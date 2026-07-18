package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// stubDoctorTransport routes doctor's functional checks at a plain test server
// as if it were the pinned secure endpoint; the real TLS+pin path is covered by
// the pairing client e2e tests.
func stubDoctorTransport(t *testing.T, base, credential string) {
	t.Helper()
	original := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return base, httpClient, credential, true, nil
	}
	t.Cleanup(func() { relayFunctionalTransportForDoctor = original })
}

func TestRunDoctorDeviceModeChecksSecureEndpoint(t *testing.T) {
	paths, _ := doctorTestSetup(t)

	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	secure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+credential {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(secure.Close)
	stubDoctorTransport(t, secure.URL, credential)

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode != 0 {
		t.Fatalf("doctor exit = %d, want 0:\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Device credential present (paired securely)") {
		t.Fatalf("expected device credential line:\n%s", output)
	}
	if strings.Contains(output, "Relay auth token present") {
		t.Fatalf("device mode must not claim a relay auth token:\n%s", output)
	}
	if !strings.Contains(output, "Relay health reachable: "+secure.URL+"/health") {
		t.Fatalf("expected health over the secure endpoint:\n%s", output)
	}
}

func TestRunDoctorDeviceModeRevokedGuidesRepair(t *testing.T) {
	paths, _ := doctorTestSetup(t)

	secure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(secure.Close)
	stubDoctorTransport(t, secure.URL, "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB")

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to fail on a revoked device credential:\n%s", output)
	}
	if !strings.Contains(output, "This device's pairing was not accepted (revoked or unknown). Pair again: run 'ha-nova setup'.") {
		t.Fatalf("expected re-pair guidance:\n%s", output)
	}
}

func TestRunDoctorPairOnlySetupWithoutHAURLStillJudgesConnection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// `ha-nova pair --relay-url ...` saves no HA address. Doctor must not fail
	// on the empty URL — the relay's WS state carries the verdict.
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: "http://192.168.1.5:18791"}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	secure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(secure.Close)
	stubDoctorTransport(t, secure.URL, credential)

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode != 0 {
		t.Fatalf("doctor exit = %d, want 0:\n%s", exitCode, output)
	}
	if !strings.Contains(output, "No Home Assistant address saved yet") {
		t.Fatalf("expected the missing-address warning:\n%s", output)
	}
	if !strings.Contains(output, "Connected to Home Assistant") {
		t.Fatalf("expected the WS verdict despite the missing address:\n%s", output)
	}
}

func TestRunDoctorPairedButMissingCredentialGuidesRepair(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// Paired config, but the device credential is gone and no legacy token
	// exists: doctor must say exactly that instead of a generic token error.
	if err := saveConfig(paths, runtimeConfig{
		HAHost:             "192.168.1.5",
		HAURL:              "http://192.168.1.5:8123",
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, []string{"--quiet"})
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to fail on a missing device credential:\n%s", output)
	}
	if !strings.Contains(output, "This device was paired, but its device credential is missing from secure storage.") {
		t.Fatalf("expected paired-but-missing guidance:\n%s", output)
	}
}

func TestRunDoctorPairedMissingCredentialNotMaskedByLegacyToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir()) // no device credential stored

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	// A leftover legacy token must NOT mask a paired config's broken device
	// transport: doctor reports the device problem, not "token present".
	if err := writeRelayAuthToken("leftover-legacy-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:             "192.168.1.5",
		HAURL:              "http://192.168.1.5:8123",
		RelayBaseURL:       "http://192.168.1.5:8791",
		RelaySecureBaseURL: "https://192.168.1.5:8792",
		RelaySpkiPin:       "pin",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, []string{"--quiet"})
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to fail on a missing device credential:\n%s", output)
	}
	if !strings.Contains(output, "This device was paired, but its device credential is missing from secure storage.") {
		t.Fatalf("expected paired-but-missing guidance, not a legacy fallback:\n%s", output)
	}
	if strings.Contains(output, "Relay auth token present") {
		t.Fatalf("a leftover legacy token must not mask the paired device problem:\n%s", output)
	}
}

func TestRunDoctorLegacyHintsAtPairingCapableRelay(t *testing.T) {
	paths, _ := doctorTestSetup(t)

	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	originalProbe := probePairingV1ForDoctor
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
		probePairingV1ForDoctor = originalProbe
	}()
	fetchRelayHealthForReadiness = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK}, nil
	}
	probePairingV1ForDoctor = func(string) bool { return true }

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode != 0 {
		t.Fatalf("doctor exit = %d, want 0:\n%s", exitCode, output)
	}
	if !strings.Contains(output, "This relay supports passwordless device pairing.") {
		t.Fatalf("expected pairing upgrade hint for a working legacy install:\n%s", output)
	}

	// --quiet is a machine contract: the advisory hint must stay out.
	exitCode, output = captureCommandOutput(t, func() int {
		return runDoctor(paths, []string{"--quiet"})
	})
	if exitCode != 0 {
		t.Fatalf("doctor --quiet exit = %d, want 0:\n%s", exitCode, output)
	}
	if strings.Contains(output, "passwordless device pairing") {
		t.Fatalf("--quiet must suppress the advisory pairing hint:\n%s", output)
	}
}
