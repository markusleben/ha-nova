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
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"strings"
	"testing"
	"time"
)

// Regression: a trailing slash on the relay URL must not produce //pair/v1/start
// (which the relay serves as 404), so the v1 request path stays well-formed.
func TestPairDeviceV1NormalizesTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusInternalServerError) // fail fast; only the path matters
	}))
	defer srv.Close()

	_, _ = pairDeviceV1(srv.Client(), srv.URL+"/", "123456",
		deviceMetadata{Name: "t", Platform: "p", Client: "c", ClientInstallID: "i"})
	if gotPath != "/pair/v1/start" {
		t.Fatalf("want /pair/v1/start, got %q", gotPath)
	}
}

// Regression: the v1 relay returns 400/VALIDATION_ERROR when no code is active
// (indistinguishable from a malformed request); the CLI must still surface the
// "click Connect a device" guidance rather than a raw status message.
func TestPairingStatusErrorMapsValidationErrorToInactive(t *testing.T) {
	err := pairingStatusError(http.StatusBadRequest, []byte(`{"ok":false,"error":{"code":"VALIDATION_ERROR"}}`))
	if !errors.Is(err, errPairingInactive) {
		t.Fatalf("v1 400/VALIDATION_ERROR should map to errPairingInactive, got %v", err)
	}
}

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

// Regression: a non-200/non-401 activation response (e.g. 500 at the device cap)
// has no transport error, so the failure must still report the HTTP status
// instead of "activation failed: %!w(<nil>)".
func TestActivateDeviceV1PreservesHTTPStatus(t *testing.T) {
	certPEM, keyPEM, pin := selfSignedECDSA(t)
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()

	err = activateDeviceV1(srv.URL, pin, "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	if err == nil {
		t.Fatal("expected an activation error for a 500 response")
	}
	if !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "%!w") {
		t.Fatalf("activation error lost the HTTP status: %v", err)
	}
}

// Regression: a relay URL with userinfo (a copied http://user:pass@host) must
// not be forwarded as a Basic auth header, nor leaked in the error text.
func TestPairPostJSONStripsURLCredentials(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{}}`))
	}))
	defer srv.Close()

	u, _ := neturl.Parse(srv.URL)
	u.User = neturl.UserPassword("user", "s3cr3t")
	var out map[string]any
	_ = pairPostJSON(srv.Client(), u.String()+"/start", nil, &out)
	if sawAuth != "" {
		t.Fatalf("URL userinfo leaked as an Authorization header: %q", sawAuth)
	}

	// The error path must not print the credentials either.
	err := pairPostJSON(srv.Client(), "http://user:s3cr3t@127.0.0.1:1/dead", nil, &out)
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "s3cr3t") {
		t.Fatalf("error leaked URL credentials: %v", err)
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
