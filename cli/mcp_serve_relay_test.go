package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ha-nova serve --relay-file (mcp_serve_relay.go): the NOVA App's internal
// device credential replaces a paired server profile.

const testBridgeCredential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"

func writeRelayFile(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "relay.json")
	if err := os.WriteFile(file, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return file
}

func relayFileBody(url, pin, credential string) string {
	return `{"relay_url":"` + url + `","spki_pin":"` + pin + `","credential":"` + credential + `"}`
}

func TestLoadBridgeRelayRefusesAnythingButAPrivateExactFile(t *testing.T) {
	pin := strings.Repeat("p", 43)
	good := relayFileBody("https://127.0.0.1:8792", pin, testBridgeCredential)
	cfg, err := loadBridgeRelay(writeRelayFile(t, good, 0o600))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RelaySecureBaseURL != "https://127.0.0.1:8792" || cfg.RelaySpkiPin != pin || cfg.bridgeCredential != testBridgeCredential || cfg.RoutePolicy != routePolicyLocal {
		t.Fatalf("config = %+v", cfg)
	}
	for name, file := range map[string]string{
		"group-readable": writeRelayFile(t, good, 0o640),
		"unknown field":  writeRelayFile(t, strings.TrimSuffix(good, "}")+`,"extra":1}`, 0o600),
		"trailing data":  writeRelayFile(t, good+"{}", 0o600),
		"plain http":     writeRelayFile(t, relayFileBody("http://127.0.0.1:8791", pin, testBridgeCredential), 0o600),
		"with a path":    writeRelayFile(t, relayFileBody("https://127.0.0.1:8792/x", pin, testBridgeCredential), 0o600),
		"no pin":         writeRelayFile(t, relayFileBody("https://127.0.0.1:8792", "", testBridgeCredential), 0o600),
		"legacy token":   writeRelayFile(t, relayFileBody("https://127.0.0.1:8792", pin, "shared-token"), 0o600),
		"missing":        filepath.Join(t.TempDir(), "absent.json"),
	} {
		if _, err := loadBridgeRelay(file); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	link := filepath.Join(t.TempDir(), "link.json")
	if err := os.Symlink(writeRelayFile(t, good, 0o600), link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBridgeRelay(link); err == nil {
		t.Error("symlink: accepted")
	}
}

func TestBridgeRelayIsTheOnlyRelayAndUsesThePinnedDevicePath(t *testing.T) {
	var auth string
	relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(relay.Close)
	sum := sha256.Sum256(relay.Certificate().RawSubjectPublicKeyInfo)
	pin := base64.RawURLEncoding.EncodeToString(sum[:])

	cfg, err := loadBridgeRelay(writeRelayFile(t, relayFileBody(relay.URL, pin, testBridgeCredential), 0o600))
	if err != nil {
		t.Fatal(err)
	}
	bridgeRelay = cfg
	t.Cleanup(func() { bridgeRelay = nil })
	t.Setenv(serverSelectionEnvVar, "elsewhere") // no profile selection applies
	loaded, err := loadRuntimeConfig(runtimePaths{ConfigFile: filepath.Join(t.TempDir(), "config.json")})
	if err != nil || loaded.bridgeCredential != testBridgeCredential {
		t.Fatalf("loadRuntimeConfig = %+v, %v", loaded, err)
	}
	base, client, credential, device, err := relayFunctionalTransport(loaded)
	if err != nil || base != relay.URL || credential != testBridgeCredential || !device {
		t.Fatalf("transport = %s %s %v %v", base, credential, device, err)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/health", nil)
	req.Header.Set("Authorization", "Bearer "+credential)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if auth != "Bearer "+testBridgeCredential {
		t.Fatalf("relay saw %q", auth)
	}

	loaded.RelaySpkiPin = strings.Repeat("x", 43) // another TLS identity is refused
	_, client, _, _, _ = relayFunctionalTransport(loaded)
	if _, err := client.Get(base + "/health"); err == nil {
		t.Fatal("a wrong pin connected")
	}
}

func TestServeRefusesRelayFileWithServer(t *testing.T) {
	paths := runtimePaths{Home: t.TempDir()}
	file := writeRelayFile(t, relayFileBody("https://127.0.0.1:8792", strings.Repeat("p", 43), testBridgeCredential), 0o600)
	t.Cleanup(func() { bridgeRelay = nil })
	if code := runServeCommand(paths, []string{"--listen", "127.0.0.1:0", "--data-dir", t.TempDir(), "--secret-file", file, "--relay-file", file, "--server", "home"}); code != 1 {
		t.Fatalf("exit %d", code)
	}
}
