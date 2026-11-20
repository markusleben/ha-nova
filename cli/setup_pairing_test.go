package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInteractiveSetupReachesPairingWhenLegacyKeyringUnavailable(t *testing.T) {
	// Regression: on a headless box (container/SSH, no Secret Service) the legacy
	// relay-token keyring preflight fails, but secure device pairing brings its
	// own file-backed storage. The wizard must reach the pairing stage instead of
	// exiting at the token-storage gate.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	originalPreflight := relayAuthTokenSetupPreflightForSetup
	t.Cleanup(func() { relayAuthTokenSetupPreflightForSetup = originalPreflight })
	relayAuthTokenSetupPreflightForSetup = func() error {
		return desktopKeyringSessionUnavailableError("no session bus in this shell")
	}

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pair/v1/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"data":{"relay_version":"0.7.0","protocol_version":"v1","available":true}}`))
		case "/health":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	// Reach the six-digit code prompt, then cancel (avoids driving full OPAQUE).
	input := joinSetupInputs([]string{"", "", "", "", "exit"})
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "codex",
			normalizeHostInput(haServer.URL), haServer.URL, relayServer.URL, "", false)
		return exitCode
	})
	output := stdout + stderr
	if exitCode == 1 {
		t.Fatalf("wizard exited at the token-storage gate on a headless box (want it to reach pairing):\n%s", output)
	}
	if strings.Contains(output, "save relay token") {
		t.Fatalf("wizard surfaced the fatal legacy-token save error instead of pairing:\n%s", output)
	}
	for _, want := range []string{"saves its key in a private file", "Connect this computer"} {
		if !strings.Contains(output, want) {
			t.Fatalf("wizard output missing %q (did not reach the pairing path):\n%s", want, output)
		}
	}
}

func TestInteractiveSetupStaysFatalOnHeadlessStandaloneRelay(t *testing.T) {
	// A standalone Relay has no /pair/v1/info and connects with its shared token
	// only, which needs the (unavailable) keyring. The wizard must NOT clear the
	// token-storage error here: it fails before asking for anything.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	originalPreflight := relayAuthTokenSetupPreflightForSetup
	t.Cleanup(func() { relayAuthTokenSetupPreflightForSetup = originalPreflight })
	relayAuthTokenSetupPreflightForSetup = func() error {
		return desktopKeyringSessionUnavailableError("no session bus in this shell")
	}

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveStandaloneTokenGate(w, r) {
			return
		}
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, "exit\n", func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "codex",
			normalizeHostInput(haServer.URL), haServer.URL, relayServer.URL, "", false)
		return exitCode
	})
	output := stdout + stderr
	if exitCode != 1 {
		t.Fatalf("wizard exit = %d, want 1 (standalone relay + no keyring must fail safe)\n%s", exitCode, output)
	}
	// It must fail with clear guidance BEFORE prompting for a code (the "exit"
	// input is never consumed), so no one-time code is ever spent.
	if !strings.Contains(output, "standalone container and connects only with its token") {
		t.Fatalf("expected the token-only/no-keyring guidance before the code prompt:\n%s", output)
	}
	if strings.Contains(output, "Six-digit code") {
		t.Fatalf("wizard prompted for a code despite unusable storage (code could be consumed):\n%s", output)
	}
}

func TestInteractiveSetupStandaloneRelayStoresItsTokenAndVerifies(t *testing.T) {
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

	const pairedToken = "relay-token-never-render"
	var pairCalls atomic.Int32
	var healthCalls atomic.Int32
	var wsCalls, firstLookCalls atomic.Int32
	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if serveStandaloneTokenGate(w, request) {
			return
		}
		switch request.URL.Path {
		case "/pair":
			pairCalls.Add(1) // the code exchange is gone: a standalone Relay never sees it
			http.NotFound(w, request)
		case "/health":
			healthCalls.Add(1)
			assertPairedBearer(t, request, pairedToken)
			_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
		case "/ws":
			var payload struct {
				Type string `json:"type"`
			}
			_ = json.NewDecoder(request.Body).Decode(&payload)
			if payload.Type != "ping" {
				// The first look's registry reads: this fake answers them
				// with nonsense, so the first look is skipped silently.
				firstLookCalls.Add(1)
				if err := checkReadOnlyRequest("ws", map[string]any{"type": payload.Type}); err != nil {
					t.Errorf("first look sent a non-read request: %v", err)
				}
			} else {
				wsCalls.Add(1)
			}
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
	input := joinSetupInputs([]string{"1", pairedToken})
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
	for _, want := range []string{"standalone container", "NOVA App answers", "Setup complete!"} {
		if !strings.Contains(output, want) {
			t.Fatalf("wizard output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, pairedToken) || strings.Contains(output, "Six-digit code") {
		t.Fatalf("wizard output leaked the token or asked for a code:\n%s", output)
	}
	if strings.Contains(output, "Make a new key") {
		t.Fatalf("a standalone container's token is the one it was started with; no new key:\n%s", output)
	}
	if pairCalls.Load() != 0 || healthCalls.Load() == 0 {
		t.Fatalf("calls pair/health = %d/%d, want 0/>0", pairCalls.Load(), healthCalls.Load())
	}
	storedToken, err := readRelayAuthToken()
	if err != nil || storedToken != pairedToken {
		t.Fatalf("stored token = %q, err = %v", storedToken, err)
	}
	config, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(config), pairedToken) {
		t.Fatalf("normal config persisted the relay token: %s", config)
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
		if serveAppPairInfo(w, request) {
			return
		}
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
	if strings.Contains(output, "Open NOVA and pair this device again") {
		t.Fatalf("manual credential failure incorrectly forced pairing repair:\n%s", output)
	}
	if strings.Count(output, "Set up the shared key") < 2 {
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
		RelayBaseURL:       "http://127.0.0.1:1",
		RelaySecureBaseURL: secureServer.URL,
		RelaySpkiPin:       "pin",
		ClientInstallID:    "inst-rerun",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	installCloudSetupTestSeams(
		t,
		successfulCloudCoordinatorForTest(),
		true,
		true,
	)

	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return secureServer.URL, httpClient, credential, true, nil
	}
	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceOK }
	defer func() {
		relayFunctionalTransportForDoctor = originalTransport
		verifyDeviceHealth = originalVerify
	}()

	configSnapshot, err := readSetupConfigSnapshot(paths)
	if err != nil {
		t.Fatalf("readSetupConfigSnapshot() error = %v", err)
	}
	lifecycleMarker := [][]byte{
		captureInstallLifecycleGeneration(paths),
		captureUninstallMarker(paths),
		configSnapshot,
	}
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, "\ny\nn\n", func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "codex", "", "", "", "", false, lifecycleMarker...)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	output := stdout + stderr
	// The precheck must reflect the device transport, and the run must verify
	// the existing pairing instead of demanding a fresh code.
	for _, want := range []string{
		"Connection works",
		"Setup complete!",
		"At home:        Ready, used first",
		"Away from home: Ready — Home Assistant Link",
		"Which one:      Automatic. Home Assistant Link only when home can't be reached",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	completionIndex := strings.Index(output, "Setup complete!")
	cloudPromptIndex := strings.Index(
		output,
		"Add Home Assistant Link fallback now? [y/N]",
	)
	if cloudPromptIndex == -1 || completionIndex <= cloudPromptIndex {
		t.Fatalf("setup must finalize before reporting completion:\n%s", output)
	}
	saved, err := loadConfig(paths)
	if err != nil || !saved.Cloud.ready() ||
		saved.RoutePolicy != routePolicyAutomatic {
		t.Fatalf("saved Cloud config = %+v, err = %v", saved.Cloud, err)
	}
	for _, forbidden := range []string{"Six-digit code", "NOVA App doesn't answer", "No key saved yet"} {
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
		RelayBaseURL:       "http://127.0.0.1:1",
		RelaySecureBaseURL: "https://127.0.0.1:2",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return "https://127.0.0.1:2", httpClient, credential, true, nil
	}
	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceDown }
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
	if !strings.Contains(output, "Connect this computer") {
		t.Fatalf("'back' must route to the pairing stage:\n%s", output)
	}

	// Lane E3: a pairing removed on the NOVA page goes straight to pairing,
	// with one plain line instead of "the App may still be starting".
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceRevoked }
	stdout, stderr = captureInteractiveSetupIO(t, "exit\n", func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "codex", "", "", "", "", false)
		return exitCode
	})
	output = stdout + stderr
	if exitCode != 0 || !strings.Contains(output, setupT("setup.verify.device_revoked")) || !strings.Contains(output, "Connect this computer") ||
		strings.Contains(output, "may still be starting") || strings.Contains(output, "Retry now") {
		t.Fatalf("revoked: exit = %d\n%s", exitCode, output)
	}

	// Revoked while the retry prompt was open: the same line and route.
	answers := []deviceHealth{deviceDown, deviceRevoked}
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { h := answers[0]; answers = answers[1:]; return h }
	stdout, stderr = captureInteractiveSetupIO(t, "\nexit\n", func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "codex", "", "", "", "", false)
		return exitCode
	})
	output = stdout + stderr
	if exitCode != 0 || !strings.Contains(output, setupT("setup.verify.device_revoked")) || !strings.Contains(output, "Connect this computer") {
		t.Fatalf("revoked on retry: exit = %d\n%s", exitCode, output)
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
		if serveAppPairInfo(w, r) {
			return
		}
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
		RelaySecureBaseURL: "https://127.0.0.1:2",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return "https://127.0.0.1:2", httpClient, credential, true, nil
	}
	// The pairing is dead: device verify always fails.
	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceDown }
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
		if serveAppPairInfo(w, r) {
			return
		}
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
		RelaySecureBaseURL: "https://127.0.0.1:2",
		RelaySpkiPin:       "pin",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	originalTransport := relayFunctionalTransportForDoctor
	relayFunctionalTransportForDoctor = func(runtimeConfig) (string, *http.Client, string, bool, error) {
		return "https://127.0.0.1:2", httpClient, credential, true, nil
	}
	originalVerify := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceDown }
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
