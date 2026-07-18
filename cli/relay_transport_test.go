package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// F1 regression: a paired config whose device credential cannot be resolved
// must FAIL — never downgrade to the plain port with the caller's credential
// (which, in paired flows, IS the device credential).
func TestFunctionalEndpointPairedConfigNeverDowngradesToPlainHTTP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir()) // no device credential stored
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	cfg := runtimeConfig{
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}
	base, client, credential, err := functionalEndpoint(cfg, "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")
	if err == nil {
		t.Fatalf("expected an error, got base=%q credential set=%v", base, credential != "")
	}
	if base != "" || client != nil || credential != "" {
		t.Fatalf("failure must not leak endpoint values: base=%q credential=%q", base, credential)
	}
	if !strings.Contains(err.Error(), "secure relay endpoint unavailable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// F3 regression: relayFunctionalTransport itself — not only functionalEndpoint —
// must fail closed for a paired config whose device credential is missing. Direct
// callers (health, relay, setup verify) must never downgrade to the plain port.
func TestRelayFunctionalTransportPairedConfigFailsClosedWithoutCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir()) // no device credential stored
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	cfg := runtimeConfig{
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}
	base, client, token, deviceMode, err := relayFunctionalTransport(cfg)
	if err == nil {
		t.Fatalf("expected an error for a paired config with no credential, got base=%q", base)
	}
	if deviceMode || base != "" || client != nil || token != "" {
		t.Fatalf("failure must not leak transport values: base=%q tokenSet=%v deviceMode=%v", base, token != "", deviceMode)
	}
}

// F-notice regression: the update/floor notice must not downgrade a paired
// config to the legacy plain, unpinned port when the device credential is
// missing — it stays silent (best-effort), respecting fail-closed.
func TestRelayNoticeTransportDoesNotDowngradePairedToLegacy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir()) // no device credential stored
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	if err := writeRelayAuthToken("leftover-legacy"); err != nil { // a leftover legacy token
		t.Fatalf("writeRelayAuthToken: %v", err)
	}
	cfg := runtimeConfig{
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}
	if _, _, _, ok := relayNoticeTransport(cfg); ok {
		t.Fatal("paired config with no device credential must not downgrade to the legacy plain port")
	}
}

func TestFunctionalEndpointLegacyConfigKeepsCallerToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	cfg := runtimeConfig{RelayBaseURL: "http://192.168.1.5:8791"}
	base, client, credential, err := functionalEndpoint(cfg, "legacy-token")
	if err != nil {
		t.Fatalf("legacy config must not fail: %v", err)
	}
	if base != cfg.RelayBaseURL || client != httpClient || credential != "legacy-token" {
		t.Fatalf("legacy resolution wrong: base=%q credential=%q", base, credential)
	}
}

// F2 regression: paired installs (which store no legacy token at all) still get
// the relay-outdated floor warning in update/check-update.
func TestRelayFloorNoticeUsesDeviceTransportForPairedInstalls(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	// min_relay_version resolves from the repo's version.json.
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	var sawAuth string
	secure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		sawAuth = r.Header.Get("Authorization")
		// A version far below any plausible min_relay_version floor.
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.0.1"}`))
	}))
	t.Cleanup(secure.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: "http://192.168.1.5:8791",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatalf("mkdir version dir: %v", err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.18.1","min_relay_version":"0.4.0"}`), 0o644); err != nil {
		t.Fatalf("write version.json: %v", err)
	}

	original := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return secure.URL, httpClient, "device-cred", true, nil
	}
	t.Cleanup(func() { relayFunctionalTransportForDoctor = original })

	notice := relayFloorNotice(paths)
	if notice.empty() {
		t.Fatal("expected a floor notice for a paired install running an ancient relay")
	}
	if sawAuth != "Bearer device-cred" {
		t.Fatalf("floor probe used Authorization %q, want the device credential over the device transport", sawAuth)
	}
}
