package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestExchangeRelayPairingCodeUsesBodyWithoutBearer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/pair" {
			t.Fatalf("request = %s %s, want POST /pair", request.Method, request.URL.Path)
		}
		if authorization := request.Header.Get("Authorization"); authorization != "" {
			t.Fatalf("Authorization = %q, want empty", authorization)
		}
		var payload struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode pairing request: %v", err)
		}
		if payload.Code != "123456" {
			t.Fatalf("pairing code = %q, want normalized six digits", payload.Code)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_token":"paired-relay-token"}}`))
	}))
	defer server.Close()

	baseURLWithCredentials := strings.Replace(server.URL, "http://", "http://user:password@", 1)
	token, err := exchangeRelayPairingCode(server.Client(), baseURLWithCredentials, "123 456")
	if err != nil {
		t.Fatalf("exchangeRelayPairingCode() error = %v", err)
	}
	if token != "paired-relay-token" {
		t.Fatalf("token = %q, want paired-relay-token", token)
	}
}

func TestExchangeRelayPairingCodeKeepsFailuresSecretAndBounded(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		retryAfter  string
		body        string
		wantError   error
		wantMessage string
		wantRetry   int
	}{
		{
			name:      "rejected",
			status:    http.StatusUnauthorized,
			body:      `{"ok":false,"error":{"code":"PAIRING_FAILED","message":"must-not-leak"}}`,
			wantError: errRelayPairingRejected,
		},
		{
			name:      "legacy relay auth middleware",
			status:    http.StatusUnauthorized,
			body:      `{"ok":false,"error":{"code":"UNAUTHORIZED","message":"must-not-leak"}}`,
			wantError: errRelayPairingUnsupported,
		},
		{
			name:        "unknown unauthorized response",
			status:      http.StatusUnauthorized,
			body:        `{"error":{"message":"must-not-leak"}}`,
			wantMessage: "relay returned an invalid pairing error response",
		},
		{name: "unsupported", status: http.StatusNotFound, wantError: errRelayPairingUnsupported},
		{name: "rate limited", status: http.StatusTooManyRequests, retryAfter: "999", wantRetry: 300},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if test.retryAfter != "" {
					w.Header().Set("Retry-After", test.retryAfter)
				}
				w.WriteHeader(test.status)
				body := test.body
				if body == "" {
					body = `{"error":{"message":"must-not-leak"}}`
				}
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()

			_, err := exchangeRelayPairingCode(server.Client(), server.URL, "654321")
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("error = %v, want %v", err, test.wantError)
			}
			if test.wantMessage != "" && (err == nil || err.Error() != test.wantMessage) {
				t.Fatalf("error = %v, want message %q", err, test.wantMessage)
			}
			if strings.Contains(err.Error(), "654321") || strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("pairing error leaked secret material: %v", err)
			}
			if test.wantRetry > 0 {
				var rateLimit *relayPairingRateLimitError
				if !errors.As(err, &rateLimit) || rateLimit.retryAfterSeconds != test.wantRetry {
					t.Fatalf("rate limit error = %#v, want retry %d", err, test.wantRetry)
				}
			}
		})
	}
}

func TestExchangeRelayPairingCodeDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	_, err := exchangeRelayPairingCode(server.Client(), server.URL, "123456")
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("error = %v, want HTTP 307 without redirect", err)
	}
	if redirected.Load() != 0 {
		t.Fatalf("redirect target received %d request(s), want 0", redirected.Load())
	}
}

func TestInteractiveSetupPairsStoresTokenAndVerifiesHealthAndWS(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer haServer.Close()

	const pairedToken = "paired-token-never-render"
	var pairCalls atomic.Int32
	var healthCalls atomic.Int32
	var wsCalls atomic.Int32
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/pair":
			pairCalls.Add(1)
			if request.Header.Get("Authorization") != "" {
				t.Error("pairing request must not send Authorization")
			}
			var payload struct {
				Code string `json:"code"`
			}
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				t.Errorf("decode pairing request: %v", err)
			}
			if payload.Code != "123456" {
				t.Errorf("pairing code = %q, want 123456", payload.Code)
			}
			_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_token":"` + pairedToken + `"}}`))
		case "/health":
			healthCalls.Add(1)
			assertPairedBearer(t, request, pairedToken)
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
		case "/ws":
			wsCalls.Add(1)
			assertPairedBearer(t, request, pairedToken)
			_, _ = w.Write([]byte(`{"ok":true,"data":{"type":"pong"}}`))
		default:
			http.NotFound(w, request)
		}
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error = %v", err)
	}
	input := joinSetupInputs(setupWizardPairingPrompts("123 456"))
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(
			paths,
			runtimeConfig{},
			loadStateOrDefault(paths),
			"codex",
			normalizeHostInput(haServer.URL),
			haServer.URL,
			relayServer.URL,
			"",
			false,
		)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	output := stdout + stderr
	for _, want := range []string{"Pair this device", "This device is paired", "Relay /ws ping succeeded", "Setup complete!"} {
		if !strings.Contains(output, want) {
			t.Fatalf("wizard output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "123456") || strings.Contains(output, pairedToken) {
		t.Fatalf("wizard output leaked pairing credentials:\n%s", output)
	}
	if pairCalls.Load() != 1 || healthCalls.Load() != 1 || wsCalls.Load() != 1 {
		t.Fatalf("calls pair/health/ws = %d/%d/%d, want 1/1/1", pairCalls.Load(), healthCalls.Load(), wsCalls.Load())
	}
	storedToken, err := readRelayAuthToken()
	if err != nil || storedToken != pairedToken {
		t.Fatalf("stored token = %q, err = %v", storedToken, err)
	}
	config, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(config), "123456") || strings.Contains(string(config), pairedToken) {
		t.Fatalf("normal config persisted pairing credentials: %s", config)
	}
}

func TestInteractiveSetupManualPairingFallbackKeepsTokenRepair(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer haServer.Close()
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/health" {
			http.NotFound(w, request)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error = %v", err)
	}
	input := joinSetupInputs(
		setupWizardPasteRelayTokenPrompts("mistyped-token"),
		[]string{"1", "exit"},
	)
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(
			paths,
			runtimeConfig{},
			loadStateOrDefault(paths),
			"codex",
			normalizeHostInput(haServer.URL),
			haServer.URL,
			relayServer.URL,
			"",
			false,
		)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want cancelled 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	output := stdout + stderr
	if !strings.Contains(output, "Back to Relay token step") {
		t.Fatalf("manual credential failure did not offer token repair:\n%s", output)
	}
	if strings.Contains(output, "Open Home Base and pair this device again") {
		t.Fatalf("manual credential failure incorrectly forced pairing repair:\n%s", output)
	}
	if strings.Count(output, "Set up Relay Auth Token") < 2 {
		t.Fatalf("manual credential failure did not return to the token step:\n%s", output)
	}
}

func assertPairedBearer(t *testing.T, request *http.Request, token string) {
	t.Helper()
	if got := request.Header.Get("Authorization"); got != "Bearer "+token {
		t.Errorf("Authorization = %q, want paired bearer", got)
	}
}

func TestInteractiveSetupPairedDeviceReRunVerifiesWithoutNewCode(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer haServer.Close()
	secureServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer secureServer.Close()

	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error = %v", err)
	}
	cfg := runtimeConfig{
		HAHost:             normalizeHostInput(haServer.URL),
		HAURL:              haServer.URL,
		RelayBaseURL:       "http://192.168.1.5:18791",
		RelaySecureBaseURL: secureServer.URL,
		RelaySpkiPin:       "pin",
		ClientInstallID:    "inst-rerun",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return secureServer.URL, httpClient, credential, true, nil
	}
	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) bool { return true }
	defer func() {
		relayFunctionalTransportForDoctor = originalTransport
		verifyDeviceHealth = originalVerify
	}()

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, "\n\n\n", func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "codex", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	output := stdout + stderr
	// The precheck must reflect the device transport, and the run must verify
	// the existing pairing instead of demanding a fresh code.
	for _, want := range []string{"Secure connection verified", "Setup complete!"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	for _, forbidden := range []string{"Six-digit code", "Relay not reachable", "No auth token found"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("paired re-run must not show %q:\n%s", forbidden, output)
		}
	}
}

// F4 regression: on the device-verify retry prompt, 'back' must route to
// pairing (as the prompt advertises) and 'exit' must cancel cleanly — neither
// may abort with a silent failure exit code.
func TestInteractiveSetupDeviceVerifyRetryPromptHonorsBackAndExit(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer haServer.Close()

	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error = %v", err)
	}
	cfg := runtimeConfig{
		HAHost:             normalizeHostInput(haServer.URL),
		HAURL:              haServer.URL,
		RelayBaseURL:       "http://192.168.1.5:18791",
		RelaySecureBaseURL: "https://192.168.1.5:18792",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return "https://192.168.1.5:18792", httpClient, credential, true, nil
	}
	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) bool { return false }
	defer func() {
		relayFunctionalTransportForDoctor = originalTransport
		verifyDeviceHealth = originalVerify
	}()

	// 'exit' at the retry prompt: clean cancel, exit code 0.
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, "exit\n", func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "codex", "", "", "", "", false)
		return exitCode
	})
	output := stdout + stderr
	if exitCode != 0 {
		t.Fatalf("exit at retry prompt: exit = %d, want 0\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Setup cancelled") {
		t.Fatalf("expected a cancelled note for 'exit':\n%s", output)
	}

	// 'back' at the retry prompt: routes to the pairing stage as advertised.
	stdout, stderr = captureInteractiveSetupIO(t, "back\nexit\n", func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "codex", "", "", "", "", false)
		return exitCode
	})
	output = stdout + stderr
	if exitCode != 0 {
		t.Fatalf("back at retry prompt: exit = %d, want 0 (cancelled in pairing)\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Pair this device") {
		t.Fatalf("'back' must route to the pairing stage:\n%s", output)
	}
}

// F3 regression: escaping pairing via 'manual' clears the device-verify route —
// the wizard must verify the pasted token, not the dead pairing.
func TestInteractiveSetupManualEscapeFromPairingVerifiesTheToken(t *testing.T) {
	originalRetireRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error { return nil }
	defer func() { revokeSelfDeviceV1ForRetire = originalRetireRevoke }()

	withClientRuntimeAvailability(t, map[string]bool{"codex": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer haServer.Close()
	const pastedToken = "relay-token-from-other-device"
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+pastedToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error = %v", err)
	}
	cfg := runtimeConfig{
		HAHost:             normalizeHostInput(haServer.URL),
		HAURL:              haServer.URL,
		RelayBaseURL:       relayServer.URL,
		RelaySecureBaseURL: "https://192.168.1.5:18792",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return "https://192.168.1.5:18792", httpClient, credential, true, nil
	}
	// The pairing is dead: device verify always fails.
	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) bool { return false }
	defer func() {
		relayFunctionalTransportForDoctor = originalTransport
		verifyDeviceHealth = originalVerify
	}()

	// Retry prompt -> 'back' to pairing -> Enter (open-NOVA prompt) ->
	// 'manual' at the code prompt -> choice 1 (paste) -> token -> verify.
	input := joinSetupInputs([]string{"back", "", "manual", "1", pastedToken})
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "codex", "", "", "", "", false)
		return exitCode
	})
	output := stdout + stderr
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	// The pasted token must be what gets verified (legacy /health probe), and
	// the run must end successfully — not loop back into device verify.
	if !strings.Contains(output, "Connected to Home Assistant") {
		t.Fatalf("expected the pasted token to verify:\n%s", output)
	}
	if !strings.Contains(output, "Setup complete!") {
		t.Fatalf("expected setup to complete after the manual escape:\n%s", output)
	}
}

// S1 regression: completing setup on the token path must RETIRE a leftover
// device pairing (slots + secure config fields), or the next doctor/skill run
// resolves the dead pairing first and wedges the install.
func TestInteractiveSetupManualEscapeRetiresTheDeadPairing(t *testing.T) {
	originalRetireRevoke := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error { return nil }
	defer func() { revokeSelfDeviceV1ForRetire = originalRetireRevoke }()

	withClientRuntimeAvailability(t, map[string]bool{"codex": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer haServer.Close()
	const pastedToken = "relay-token-from-other-device"
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+pastedToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	const credential = "hanova-dev-v1.AAAAAAAAAAAAAAAAAAAAAA.BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatalf("writeDeviceCredential() error: %v", err)
	}
	if err := writePendingDeviceCredential(credential); err != nil {
		t.Fatalf("writePendingDeviceCredential() error: %v", err)
	}
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error = %v", err)
	}
	cfg := runtimeConfig{
		HAHost:             normalizeHostInput(haServer.URL),
		HAURL:              haServer.URL,
		RelayBaseURL:       relayServer.URL,
		RelaySecureBaseURL: "https://192.168.1.5:18792",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return "https://192.168.1.5:18792", httpClient, credential, true, nil
	}
	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) bool { return false }
	defer func() {
		relayFunctionalTransportForDoctor = originalTransport
		verifyDeviceHealth = originalVerify
	}()

	input := joinSetupInputs([]string{"back", "", "manual", "1", pastedToken})
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "codex", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	// The dead pairing is fully retired: no credential slots, no secure fields.
	if _, ok, _ := readDeviceCredential(); ok {
		t.Fatal("expected the dead device credential to be removed")
	}
	if _, ok, _ := readPendingDeviceCredential(); ok {
		t.Fatal("expected the pending slot to be removed")
	}
	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.RelaySecureBaseURL != "" || saved.RelaySpkiPin != "" {
		t.Fatalf("expected secure endpoint fields to be cleared, got %q/%q", saved.RelaySecureBaseURL, saved.RelaySpkiPin)
	}
}
