package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseDeviceCredential(t *testing.T) {
	good := "hanova-dev-v1." + strings.Repeat("A", 22) + "." + strings.Repeat("B", 43)
	p := parseDeviceCredential(good)
	if p == nil || p.deviceID != strings.Repeat("A", 22) || p.secret != strings.Repeat("B", 43) {
		t.Fatalf("valid credential did not parse: %+v", p)
	}
	if deviceIDOf(good) != strings.Repeat("A", 22) {
		t.Fatalf("deviceIDOf mismatch")
	}
	for _, bad := range []string{
		"", "x", "hanova-dev-v1.short.secret", "wrong." + strings.Repeat("A", 22) + "." + strings.Repeat("B", 43),
		"hanova-dev-v1." + strings.Repeat("A", 22) + "." + strings.Repeat("B", 43) + ".extra",
		"hanova-dev-v1." + strings.Repeat("!", 22) + "." + strings.Repeat("B", 43),
	} {
		if parseDeviceCredential(bad) != nil {
			t.Fatalf("malformed credential accepted: %q", bad)
		}
	}
}

func TestPairAEADRoundtripAndDirectionBinding(t *testing.T) {
	sessionKey := make([]byte, 64)
	_, _ = rand.Read(sessionKey)
	hsid := make([]byte, 16)
	_, _ = rand.Read(hsid)
	s2c := derivePairKey(sessionKey, hsid, "s2c")
	c2s := derivePairKey(sessionKey, hsid, "c2s")

	msg := []byte(`{"credential":"x"}`)
	frame := pairSeal(s2c, hsid, "s2c", msg)
	got, ok := pairOpen(s2c, hsid, "s2c", frame)
	if !ok || string(got) != string(msg) {
		t.Fatalf("roundtrip failed ok=%v", ok)
	}
	// A frame sealed for s2c must not open as c2s (no cross-direction replay).
	if _, ok := pairOpen(c2s, hsid, "c2s", frame); ok {
		t.Fatalf("cross-direction frame opened")
	}
	// Tamper.
	frame[len(frame)-1] ^= 1
	if _, ok := pairOpen(s2c, hsid, "s2c", frame); ok {
		t.Fatalf("tampered frame opened")
	}
}

func TestSpkiPinnedClient(t *testing.T) {
	certPEM, keyPEM, pin := selfSignedECDSA(t)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	// Correct pin succeeds.
	if resp, err := spkiPinnedClient(pin).Get(srv.URL); err != nil {
		t.Fatalf("correct pin rejected: %v", err)
	} else {
		resp.Body.Close()
	}
	// Wrong pin fails.
	if _, err := spkiPinnedClient("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA").Get(srv.URL); err == nil {
		t.Fatalf("wrong pin accepted")
	}
}

func selfSignedECDSA(t *testing.T) (certPEM, keyPEM []byte, pin string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "nova-relay"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	pin = pairB64.EncodeToString(sum[:])
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, pin
}
