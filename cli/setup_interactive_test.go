package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func hasSetupStep(output string, step int) bool {
	return strings.Contains(output, fmt.Sprintf("Step %d of 4", step)) ||
		strings.Contains(output, fmt.Sprintf("Step %d of 5", step)) ||
		strings.Contains(output, fmt.Sprintf("[%d/4]", step)) ||
		strings.Contains(output, fmt.Sprintf("[%d/5]", step))
}

func setupStepIndex(output string, step int) int {
	if idx := strings.Index(output, fmt.Sprintf("Step %d of 4", step)); idx != -1 {
		return idx
	}
	if idx := strings.Index(output, fmt.Sprintf("Step %d of 5", step)); idx != -1 {
		return idx
	}
	if idx := strings.Index(output, fmt.Sprintf("[%d/4]", step)); idx != -1 {
		return idx
	}
	return strings.Index(output, fmt.Sprintf("[%d/5]", step))
}

func TestInteractiveSetupFreshInstallShowsWizardAndInstallsAntigravitySkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Which AI client do you use?",
		"1) Claude Code",
		"4) Google Antigravity",
		"5) Hermes Agent",
		"Discovering Home Assistant on your network...",
		"Next, add the HA NOVA app repository to your Home Assistant.",
		"Once the repository is added:",
		`Search for "NOVA Relay"`,
		"NOVA keeps client and Home Assistant access separate",
		"Standalone Container/Core relays keep HA_LLAT in the server environment; the CLI never asks for it.",
		`Paste the token into the "Relay Auth Token" field`,
		"Press Enter after you saved the Relay Auth Token and restarted NOVA Relay",
		"Setting up HA NOVA for Google Antigravity...",
		"Setup complete!",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("wizard output missing %q:\n%s", want, output)
		}
	}
	if !strings.Contains(output, "Found Home Assistant at ") &&
		!strings.Contains(output, "Found Home Assistant via the network name ") &&
		!strings.Contains(output, "No confirmed Home Assistant found automatically; enter the Home Assistant address manually") &&
		!strings.Contains(output, "No confirmed Home Assistant found automatically; using your saved address as a starting point:") {
		t.Fatalf("wizard output missing discovery result:\n%s", output)
	}
	discoveryIdx := strings.Index(output, "Discovering Home Assistant on your network...")
	hostPromptIdx := strings.Index(output, "Home Assistant address")
	stepOneIdx := setupStepIndex(output, 1)
	if discoveryIdx == -1 || hostPromptIdx == -1 || stepOneIdx == -1 {
		t.Fatalf("missing discovery ordering markers:\n%s", output)
	}
	if !(discoveryIdx < hostPromptIdx && hostPromptIdx < stepOneIdx) {
		t.Fatalf("expected discovery and host prompt before step 1:\n%s", output)
	}
	for step := 1; step <= 4; step++ {
		if !hasSetupStep(output, step) {
			t.Fatalf("wizard output missing step %d marker:\n%s", step, output)
		}
	}
	generatedTokenMatch := regexp.MustCompile(`Here is your relay token \(generated automatically\):\s+([a-f0-9]{64})`).FindStringSubmatch(output)
	if len(generatedTokenMatch) != 2 {
		t.Fatalf("expected generated relay token in wizard output:\n%s", output)
	}
	if strings.Count(output, generatedTokenMatch[1]) != 1 {
		t.Fatalf("expected generated relay token to be displayed exactly once:\n%s", output)
	}

	if _, err := os.Stat(filepath.Join(home, ".gemini", "config", "skills", "ha-nova", "SKILL.md")); err != nil {
		t.Fatalf("expected Antigravity main skill to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "config", "skills", "ha-nova-review", "SKILL.md")); err != nil {
		t.Fatalf("expected Antigravity review skill to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ha-nova", "config.json")); err != nil {
		t.Fatalf("expected config.json to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token")); err != nil {
		t.Fatalf("expected test keyring file to exist: %v", err)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if saved.HAHost != normalizeHostInput(haServer.URL) {
		t.Fatalf("saved.HAHost = %q, want %q", saved.HAHost, normalizeHostInput(haServer.URL))
	}
	if saved.HAURL != haServer.URL {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, haServer.URL)
	}
	if saved.RelayBaseURL != relayServer.URL {
		t.Fatalf("saved.RelayBaseURL = %q, want %q", saved.RelayBaseURL, relayServer.URL)
	}
}

func TestInteractiveSetupWithoutAvailableClientReturnsGuidance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	withClientRuntimeAvailability(t, map[string]bool{})

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode := 1
	stdout, stderr := captureInteractiveSetupIO(t, "", func() int {
		clientRuntimeDetectedForStatus = func(string) bool { return false }
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"No supported AI client is ready on this machine yet.",
		"Install one supported client first, then rerun: ha-nova setup",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing-client guidance missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "[ha-nova] ERROR") {
		t.Fatalf("missing-client guidance must not render as an error:\n%s", output)
	}
}

func TestInteractiveSetupFreshInstallCanTargetHermes(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"5", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"5) Hermes Agent",
		"Setting up HA NOVA for Hermes Agent...",
		"Installed for: Hermes Agent",
		"Setup complete!",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("wizard output missing %q:\n%s", want, output)
		}
	}

	for _, skillDir := range hermesRequiredSkillDirs {
		skillPath := filepath.Join(home, ".hermes", "skills", "ha-nova", skillDir, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			t.Fatalf("expected Hermes skill %s to exist at %s: %v", skillDir, skillPath, err)
		}
	}
}

func TestInteractiveSetupOffersServiceCredentialsForHermesWhenKeyringLocked(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	originalPreflight := relayAuthTokenSetupPreflightForSetup
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
	}()
	relayAuthTokenSetupPreflightForSetup = func() error {
		if relayAuthTokenFilePathOverride != "" {
			return nil
		}
		return desktopKeyringLockedError("default keyring is locked")
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"5", "y", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Service / gateway mode is available for Hermes Agent.",
		"Choose yes for SSH, systemd, headless, or gateway sessions. Choose no for a normal desktop terminal with an unlocked keyring.",
		"Use service/gateway token file instead of desktop keyring?",
		"Setting up HA NOVA for Hermes Agent...",
		"Setup complete!",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("wizard output missing %q:\n%s", want, output)
		}
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.RelayTokenFile != defaultRelayAuthTokenFile(paths) {
		t.Fatalf("RelayTokenFile = %q, want %q", saved.RelayTokenFile, defaultRelayAuthTokenFile(paths))
	}
	token, err := readRelayAuthTokenFile(saved.RelayTokenFile)
	if err != nil {
		t.Fatalf("readRelayAuthTokenFile() error: %v", err)
	}
	if strings.TrimSpace(token) == "" {
		t.Fatal("expected service token file to contain the generated relay token")
	}
}

func TestInteractiveSetupFreshInstallCanPasteExistingRelayToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	const pastedToken = "relay-token-from-macos"
	input := joinSetupInputs(
		[]string{"4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardPasteRelayTokenPrompts(pastedToken),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	if !strings.Contains(output, "Paste existing token from another device / Home Assistant") {
		t.Fatalf("expected token reuse choice in output:\n%s", output)
	}
	if strings.Contains(output, "generated automatically") {
		t.Fatalf("did not expect generated-token flow in output:\n%s", output)
	}

	savedToken, err := readRelayAuthToken()
	if err != nil {
		t.Fatalf("readRelayAuthToken() error: %v", err)
	}
	if savedToken != pastedToken {
		t.Fatalf("saved token = %q, want %q", savedToken, pastedToken)
	}
}

func TestInteractiveSetupFreshInstallManualTokenFallbackRunsAppSetupBeforeVerify(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardPasteRelayTokenPrompts("relay-token-from-other-device"),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"If this token already works on another device",
		"Connected to Home Assistant",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected explicit-token fallback text %q in output:\n%s", want, output)
		}
	}
	// The App receives upstream access automatically: the explicit-token
	// fallback must not surface any Home Assistant access-token walkthrough.
	for _, forbidden := range []string{
		"Create a Home Assistant Access Token in Home Assistant.",
		"Press Enter to open your HA profile",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("explicit-token fallback must not show LLAT setup %q:\n%s", forbidden, output)
		}
	}
	if !hasSetupStep(output, 3) {
		t.Fatalf("expected explicit-token verification step marker in output:\n%s", output)
	}
	if strings.Contains(output, "automations\n\n\n  Existing relay token found:") {
		t.Fatalf("expected single-gap spacing before reuse-token note:\n%s", output)
	}
}

func TestInteractiveSetupFailsEarlyWhenLinuxKeyringPreflightFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	originalPreflight := relayAuthTokenSetupPreflightForSetup
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
	}()
	relayAuthTokenSetupPreflightForSetup = func() error {
		return desktopKeyringSetupRequiredError("no default Secret Service collection configured")
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return false, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	if !strings.Contains(output, "secure storage is present but not initialized on this Linux machine") {
		t.Fatalf("expected keyring guidance in output:\n%s", output)
	}
	if strings.Contains(output, "Step 1 of 5") {
		t.Fatalf("expected setup to stop before relay install walkthrough:\n%s", output)
	}
	if _, err := os.Stat(paths.ConfigFile); !isNotExist(err) {
		t.Fatalf("expected config not to be written after preflight failure, err=%v", err)
	}
	if _, err := os.Stat(paths.StateFile); !isNotExist(err) {
		t.Fatalf("expected state not to be written after preflight failure, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token")); !isNotExist(err) {
		t.Fatalf("expected token file not to be written after preflight failure, err=%v", err)
	}
}

func TestInteractiveSetupRecoversSecureStorageBeforeHostStep(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	originalPreflight := relayAuthTokenSetupPreflightForSetup
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	preflightCalls := 0
	relayAuthTokenSetupPreflightForSetup = func() error {
		preflightCalls++
		if preflightCalls == 1 {
			return desktopKeyringSetupRequiredError("no default Secret Service collection configured")
		}
		return nil
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}
	recoveryCalls := 0
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, secret []byte) error {
		recoveryCalls++
		if action != platformSecureStorageRecoveryInitialize {
			t.Fatalf("unexpected recovery action %q", action)
		}
		if string(secret) != "linux-local-keyring" {
			t.Fatalf("unexpected recovery secret %q", string(secret))
		}
		return nil
	}
	readSetupSecretInputForSetup = func(fd int) ([]byte, error) {
		if fd <= 0 {
			t.Fatalf("expected terminal fd, got %d", fd)
		}
		return []byte("linux-local-keyring"), nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", "", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	if !strings.Contains(output, "Local secure storage needs setup") {
		t.Fatalf("expected dedicated secure storage recovery page:\n%s", output)
	}
	if !strings.Contains(output, "not the Relay token or the Home Assistant token") {
		t.Fatalf("expected explicit local-password guidance:\n%s", output)
	}
	if !strings.Contains(output, "HA NOVA, NOVA Relay, and Home Assistant never receive it.") {
		t.Fatalf("expected explicit local-only keyring guidance:\n%s", output)
	}
	if !strings.Contains(output, "Set up local secure storage now") {
		t.Fatalf("expected recovery action prompt:\n%s", output)
	}
	if !strings.Contains(output, "Home Assistant address") {
		t.Fatalf("expected setup to continue to host prompt after recovery:\n%s", output)
	}
	if recoveryCalls != 1 {
		t.Fatalf("expected one recovery attempt, got %d", recoveryCalls)
	}
}

func TestInteractiveSetupWithHostAndRelayTokenFlagsSkipsLLATWalkthrough(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := ""

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "claude", normalizeHostInput(haServer.URL), haServer.URL, relayServer.URL, "relay-token-from-flag", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	if hasSetupStep(output, 1) {
		t.Fatalf("did not expect relay-install step when host + relay token flags already supplied:\n%s", output)
	}
	if strings.Contains(output, "Create a Home Assistant Access Token in Home Assistant.") {
		t.Fatalf("did not expect LLAT walkthrough when host + relay token flags already supplied:\n%s", output)
	}
}

func TestInteractiveSetupBackFromVerifyDoesNotPersistConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardPasteRelayTokenPrompts("relay-token-from-other-device"),
		[]string{"back", "exit"},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	if _, err := os.Stat(paths.ConfigFile); !os.IsNotExist(err) {
		t.Fatalf("expected config file to stay absent after backing out of verify; err=%v", err)
	}
	if _, err := os.Stat(paths.StateFile); !os.IsNotExist(err) {
		t.Fatalf("expected state file to stay absent after backing out of verify; err=%v", err)
	}
}

func TestInteractiveSetupBackFromRelayInstallLetsUserChangeHost(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	firstHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer firstHAServer.Close()

	secondHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer secondHAServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", firstHAServer.URL, "back", secondHAServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardPasteRelayTokenPrompts("relay-token-from-other-device"),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if saved.HAURL != secondHAServer.URL {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, secondHAServer.URL)
	}
}

func TestApplySetupFlagOverridesNormalizesURLShapedHost(t *testing.T) {
	originalResolve := resolveHAURLBaseForFlags
	defer func() { resolveHAURLBaseForFlags = originalResolve }()
	resolveHAURLBaseForFlags = func(input string) (string, error) {
		return strings.TrimSpace(input), nil
	}

	cfg, err := applySetupFlagOverrides(runtimeConfig{}, "http://192.168.1.5:8123", "", "")
	if err != nil {
		t.Fatalf("applySetupFlagOverrides() error: %v", err)
	}

	if cfg.HAHost != "192.168.1.5" {
		t.Fatalf("HAHost = %q, want %q", cfg.HAHost, "192.168.1.5")
	}
	if cfg.HAURL != "http://192.168.1.5:8123" {
		t.Fatalf("HAURL = %q, want %q", cfg.HAURL, "http://192.168.1.5:8123")
	}
	if cfg.RelayBaseURL != "http://192.168.1.5:8791" {
		t.Fatalf("RelayBaseURL = %q, want %q", cfg.RelayBaseURL, "http://192.168.1.5:8791")
	}
}

func TestApplySetupFlagOverridesFailsWhenHostCannotBeResolved(t *testing.T) {
	originalResolve := resolveHAURLBaseForFlags
	defer func() { resolveHAURLBaseForFlags = originalResolve }()
	resolveHAURLBaseForFlags = func(input string) (string, error) {
		return "", assertDiscoveryFailure{}
	}

	_, err := applySetupFlagOverrides(runtimeConfig{}, "homeassistant.local", "", "")
	if err == nil {
		t.Fatal("expected applySetupFlagOverrides() to fail when --host cannot be resolved")
	}
}

func TestInteractiveSetupRelayTokenFlagCanBackToHostAfterVerifyFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	firstHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer firstHAServer.Close()

	secondHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer secondHAServer.Close()

	failingRelay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer failingRelay.Close()

	originalProbe := probeRelayWSPingForSetup
	defer func() { probeRelayWSPingForSetup = originalProbe }()
	probeRelayWSPingForSetup = func(baseURL, token string) (relayWSPingResponse, error) {
		if baseURL == failingRelay.URL {
			return relayWSPingResponse{StatusCode: http.StatusBadGateway, Body: []byte("upstream unavailable")}, nil
		}
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{firstHAServer.URL},
		setupWizardRelayInstallPrompts(),
		[]string{"back", "back", secondHAServer.URL},
		[]string{"n"},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "antigravity", "", "", failingRelay.URL, "relay-token-flagged", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAURL != secondHAServer.URL {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, secondHAServer.URL)
	}
	if saved.RelayBaseURL != failingRelay.URL {
		t.Fatalf("saved.RelayBaseURL = %q, want %q", saved.RelayBaseURL, failingRelay.URL)
	}
}

func TestInteractiveSetupResumeCanChangeHostFromRepairMenuWithoutTokenRePrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	goodHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer goodHAServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	// Created after every live server: a later httptest bind in this test could
	// be handed the freed port, silently resurrecting the "dead" address.
	deadHAURL := newDeadServerURL()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       normalizeHostInput(deadHAURL),
		HAURL:        deadHAURL,
		RelayBaseURL: relayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"2", goodHAServer.URL},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "antigravity", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Using Home Assistant address: " + deadHAURL,
		"Change Home Assistant address",
		"could not be used. Enter a new one.",
		"Keeping your saved relay address: " + relayServer.URL,
		"Setup complete!",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	for _, unwanted := range []string{
		"Choose how to set up the Relay Auth Token",
		"Create a Home Assistant Access Token in Home Assistant.",
		"Discovering Home Assistant on your network",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("host change must not re-run token/discovery steps; found %q:\n%s", unwanted, output)
		}
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAURL != goodHAServer.URL {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, goodHAServer.URL)
	}
	if saved.RelayBaseURL != relayServer.URL {
		t.Fatalf("saved.RelayBaseURL = %q, want custom relay URL %q preserved across the host change", saved.RelayBaseURL, relayServer.URL)
	}
}

func TestInteractiveSetupHostChangePersistsNewHostEvenWhenUserExitsBeforeVerifySucceeds(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	goodHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer goodHAServer.Close()

	failingRelayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failingRelayServer.Close()

	deadHAURL := newDeadServerURL()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       normalizeHostInput(deadHAURL),
		HAURL:        deadHAURL,
		RelayBaseURL: failingRelayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"2", goodHAServer.URL, "exit"},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "antigravity", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "Setup cancelled") {
		t.Fatalf("expected cancellation message:\n%s", stdout+stderr)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAURL != goodHAServer.URL {
		t.Fatalf("saved.HAURL = %q, want corrected host %q persisted despite exit", saved.HAURL, goodHAServer.URL)
	}
}

func TestInteractiveSetupExitAtTokenChoiceCancelsCleanly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := strings.Join([]string{
		"4",
		haServer.URL,
		"",
		"",
		"exit",
	}, "\n") + "\n"

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0", exitCode)
	}
	output := stdout + stderr
	if !strings.Contains(output, "Setup cancelled") {
		t.Fatalf("expected cancellation message in output:\n%s", output)
	}
	if _, err := readRelayAuthToken(); err == nil {
		t.Fatal("expected cancelled setup to leave relay token untouched")
	}
}

func TestInteractiveSetupInitialClientPageAllowsRepeatedBack(t *testing.T) {
	originalDiscover := discoverReachableHAHostsForSetup
	t.Cleanup(func() { discoverReachableHAHostsForSetup = originalDiscover })
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return nil, ""
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"back", "back", "4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardPasteRelayTokenPrompts("relay-token-from-other-device"),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", relayServer.URL, "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
}

func TestInteractiveSetupAlreadyDoneUsesResumeBanner(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: relayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	state := loadStateOrDefault(paths)
	mergeStateClients(&state, []string{"claude"})
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)
	writeClaudeMarketplaceRegistrationFixture(t, home, filepath.Join(paths.ConfigDir, "claude-marketplace"))

	stdout, stderr := captureInteractiveSetupIO(t, "", func() int {
		return interactiveSetup(paths, cfg, state, "claude", "", "", "", "", false)
	})

	output := stdout + stderr
	if !strings.Contains(output, "Everything is already set up!") {
		t.Fatalf("expected resume banner in output:\n%s", output)
	}
	if strings.Contains(output, "Setup complete!") {
		t.Fatalf("did not expect fresh-setup success banner in resume output:\n%s", output)
	}
}

func TestInteractiveSetupPartialResumeSkipsTokenChoiceAndVerifiesFirstWhenWSIsPending(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: relayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"back", "exit"},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "claude", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0", exitCode)
	}

	output := stdout + stderr
	if !hasSetupStep(output, 3) {
		t.Fatalf("expected verify step on partial resume:\n%s", output)
	}
	if strings.Contains(output, "Create a Home Assistant Access Token in Home Assistant.") {
		t.Fatalf("did not expect LLAT walkthrough on partial resume:\n%s", output)
	}
	verifyIdx := setupStepIndex(output, 3)
	tokenIdx := strings.Index(output, "Choose how to set up the Relay Auth Token")
	if tokenIdx != -1 && tokenIdx < verifyIdx {
		t.Fatalf("expected partial resume to verify before reopening token step:\n%s", output)
	}
}

func TestInteractiveSetupPartialResumeTTYShowsContinuePromptBeforeClearing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	defer func() {
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
	}()
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: relayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, "\n"+"n\n", func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "claude", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1", exitCode)
	}

	output := stdout + stderr
	if !strings.Contains(output, "Press Enter to continue setup") {
		t.Fatalf("expected resume continue prompt in TTY output:\n%s", output)
	}
	if !strings.Contains(output, "Already done:") {
		t.Fatalf("expected resume summary in TTY output:\n%s", output)
	}
}

func TestInteractiveSetupRelayTokenFlagPersistsBeforeVerify(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: relayServer.URL,
	}

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, joinSetupInputs([]string{"n"}), func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "claude", "", "", "", "flag-token-from-cli", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	savedToken, err := readRelayAuthToken()
	if err != nil {
		t.Fatalf("readRelayAuthToken() error: %v", err)
	}
	if savedToken != "flag-token-from-cli" {
		t.Fatalf("saved token = %q, want %q", savedToken, "flag-token-from-cli")
	}
}

func TestInteractiveSetupCompletedResumeRejectsBrokenHostOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	originalResolve := resolveHAURLBaseForSetup
	originalFlagResolve := resolveHAURLBaseForFlags
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		resolveHAURLBaseForSetup = originalResolve
		resolveHAURLBaseForFlags = originalFlagResolve
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()

	resolveHAURLBaseForSetup = func(input string) (string, error) {
		if input == "bad-host.local" {
			return "", fmt.Errorf("unreachable: %s", input)
		}
		return "http://192.168.1.5:8123", nil
	}
	resolveHAURLBaseForFlags = resolveHAURLBaseForSetup
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: 200, Body: []byte(`{"type":"pong"}`)}, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "homeassistant.local",
		HAURL:        "http://homeassistant.local:8123",
		RelayBaseURL: "http://homeassistant.local:8791",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	state := loadStateOrDefault(paths)
	mergeStateClients(&state, []string{"claude"})
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)
	writeClaudeMarketplaceRegistrationFixture(t, home, filepath.Join(paths.ConfigDir, "claude-marketplace"))

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, "", func() int {
		exitCode = interactiveSetup(paths, cfg, state, "claude", "bad-host.local", "", "", "", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}
	output := stdout + stderr
	if strings.Contains(output, "Everything is already set up!") {
		t.Fatalf("did not expect already-done banner for broken host override:\n%s", output)
	}
	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAHost != cfg.HAHost || saved.HAURL != cfg.HAURL || saved.RelayBaseURL != cfg.RelayBaseURL {
		t.Fatalf("expected config to stay unchanged after broken override, got %+v", saved)
	}
}

func TestInteractiveSetupWSDegradedEndsIncomplete(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	originalProbeHTTP := probeHTTPForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
	}()
	probeHTTPForSetup = func(string) error { return nil }

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: relayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	exitCode := 0
	input := joinSetupInputs(
		[]string{"3", "3", "3"},
	)
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "claude", "", "", "", "", false)
		return exitCode
	})

	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1", exitCode)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Home Assistant WebSocket is not connected yet",
		"Retry now",
		"Setup incomplete",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("incomplete flow missing %q:\n%s", want, output)
		}
	}
	if !hasSetupStep(output, 3) {
		t.Fatalf("incomplete flow missing verify step marker:\n%s", output)
	}
}

func TestInteractiveSetupWSDegradedMentionsUpstreamAuthCause(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	originalProbeHTTP := probeHTTPForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
	}()
	probeHTTPForSetup = func(string) error { return nil }

	originalWSPing := probeRelayWSPingForSetup
	defer func() {
		probeRelayWSPingForSetup = originalWSPing
	}()
	probeRelayWSPingForSetup = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{
			StatusCode: http.StatusBadGateway,
			Body:       []byte("LLAT is required"),
		}, nil
	}

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: relayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	exitCode := 0
	input := joinSetupInputs(
		[]string{"n"},
	)
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "claude", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1", exitCode)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Home Assistant WebSocket is not connected yet",
		"NOVA Relay's upstream Home Assistant access was rejected.",
		"App install: update or restart NOVA Relay. Standalone Container/Core: replace HA_LLAT in the server environment.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected upstream-auth guidance %q in output:\n%s", want, output)
		}
	}
}

func TestInteractiveSetupWSDegradedUsesWSPingSuccessAsReady(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	originalProbeHTTP := probeHTTPForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
	}()
	probeHTTPForSetup = func(string) error { return nil }

	originalWSPing := probeRelayWSPingForSetup
	defer func() {
		probeRelayWSPingForSetup = originalWSPing
	}()
	probeRelayWSPingForSetup = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{
			StatusCode: http.StatusOK,
			Body:       []byte(`{"type":"pong"}`),
		}, nil
	}

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "192.168.1.5",
		HAURL:        "http://192.168.1.5:8123",
		RelayBaseURL: relayServer.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	state := loadStateOrDefault(paths)
	mergeStateClients(&state, []string{"claude"})
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	input := joinSetupInputs(nil)
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		return interactiveSetup(paths, cfg, state, "claude", "", "", "", "", false)
	})
	output := stdout + stderr
	if !strings.Contains(output, "Connected to Home Assistant") {
		t.Fatalf("expected ws-ping success to count as connected:\n%s", output)
	}
	if strings.Contains(output, "Home Assistant WebSocket is not connected yet") {
		t.Fatalf("did not expect degraded WS guidance when ws ping succeeds:\n%s", output)
	}
}

func TestApplySelectedSetupHostReplacesStaleRelayBaseURL(t *testing.T) {
	cfg := runtimeConfig{
		HAHost:       "homeassistant.local",
		HAURL:        "http://homeassistant.local:8123",
		RelayBaseURL: "http://homeassistant.local:8791",
	}

	got := applySelectedSetupHost(cfg, "192.168.1.123", "http://192.168.1.123:8123", "")

	if got.HAHost != "192.168.1.123" {
		t.Fatalf("HAHost = %q, want %q", got.HAHost, "192.168.1.123")
	}
	if got.HAURL != "http://192.168.1.123:8123" {
		t.Fatalf("HAURL = %q, want %q", got.HAURL, "http://192.168.1.123:8123")
	}
	if got.RelayBaseURL != "http://192.168.1.123:8791" {
		t.Fatalf("RelayBaseURL = %q, want %q", got.RelayBaseURL, "http://192.168.1.123:8791")
	}
}

func TestApplySelectedSetupHostKeepsExplicitRelayOverride(t *testing.T) {
	cfg := runtimeConfig{
		HAHost:       "homeassistant.local",
		HAURL:        "http://homeassistant.local:8123",
		RelayBaseURL: "http://homeassistant.local:8791",
	}

	got := applySelectedSetupHost(cfg, "192.168.1.123", "http://192.168.1.123:8123", "http://relay-box:9999")

	if got.RelayBaseURL != "http://relay-box:9999" {
		t.Fatalf("RelayBaseURL = %q, want %q", got.RelayBaseURL, "http://relay-box:9999")
	}
}

func TestInteractiveSetupCompletedResumePersistsExplicitEndpointOverrides(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "homeassistant.local",
		HAURL:        "http://homeassistant.local:8123",
		RelayBaseURL: "http://homeassistant.local:8791",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	state := loadStateOrDefault(paths)
	mergeStateClients(&state, []string{"claude"})
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode := interactiveSetup(paths, cfg, state, "claude", "192.168.1.123", haServer.URL, relayServer.URL, "", false)
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0", exitCode)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAHost != "192.168.1.123" {
		t.Fatalf("saved.HAHost = %q, want %q", saved.HAHost, "192.168.1.123")
	}
	if saved.HAURL != haServer.URL {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, haServer.URL)
	}
	if saved.RelayBaseURL != relayServer.URL {
		t.Fatalf("saved.RelayBaseURL = %q, want %q", saved.RelayBaseURL, relayServer.URL)
	}
}

func TestInteractiveSetupCompletedResumeUsesOverrideHealthInsteadOfOldHealthyState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	healthyRelay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer healthyRelay.Close()

	degradedRelay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":false}}`))
	}))
	defer degradedRelay.Close()

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer haServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "homeassistant.local",
		HAURL:        haServer.URL,
		RelayBaseURL: healthyRelay.URL,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	state := loadStateOrDefault(paths)
	mergeStateClients(&state, []string{"claude"})
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode := 0
	input := joinSetupInputs(
		[]string{"n"},
	)
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, state, "claude", "192.168.1.123", haServer.URL, degradedRelay.URL, "", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1", exitCode)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Home Assistant WebSocket is not connected yet",
		"Setup incomplete",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("override-based degraded output missing %q:\n%s", want, output)
		}
	}
	if !hasSetupStep(output, 3) {
		t.Fatalf("override-based degraded output missing verify step marker:\n%s", output)
	}
}

func TestPromptValidHAHostContinueAnywayPreservesExplicitURL(t *testing.T) {
	originalResolve := resolveHAURLBaseForSetup
	defer func() {
		resolveHAURLBaseForSetup = originalResolve
	}()
	resolveHAURLBaseForSetup = func(input string) (string, error) {
		return "", fmt.Errorf("unreachable: %s", input)
	}

	reader := bufio.NewReader(strings.NewReader("https://ha-box.local:9443/custom\nn\ny\n"))
	output := &strings.Builder{}

	host, haURL, err := promptValidHAHostFromReader(reader, output, "homeassistant.local")
	if err != nil {
		t.Fatalf("promptValidHAHostFromReader() error: %v", err)
	}
	if host != "ha-box.local" {
		t.Fatalf("host = %q, want %q", host, "ha-box.local")
	}
	if haURL != "https://ha-box.local:9443/custom" {
		t.Fatalf("haURL = %q, want %q", haURL, "https://ha-box.local:9443/custom")
	}
}

func TestInteractiveSetupContinueAnywayPersistsExplicitURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	originalResolve := resolveHAURLBaseForSetup
	originalProbe := probeHTTPForSetup
	defer func() {
		resolveHAURLBaseForSetup = originalResolve
		probeHTTPForSetup = originalProbe
	}()

	resolveHAURLBaseForSetup = func(input string) (string, error) {
		return "", fmt.Errorf("unreachable: %s", input)
	}
	probeHTTPForSetup = func(url string) error {
		return fmt.Errorf("unreachable: %s", url)
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{
			"4",
			"",
			"",
			"https://ha-box.local:9443/custom",
			"n",
			"y",
		},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
		[]string{"4"},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1", exitCode)
	}

	output := stdout + stderr
	if !strings.Contains(output, "Continue anyway (connection will be verified later)?") {
		t.Fatalf("expected continue-anyway path in output:\n%s", output)
	}
	for _, want := range []string{
		"Change Home Assistant address",
		"Stop for now (progress is saved)",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected connection repair menu with %q in output:\n%s", want, output)
		}
	}
	if strings.Contains(output, "Retry WebSocket check?") {
		t.Fatalf("did not expect WebSocket retry prompt for relay-unreachable path:\n%s", output)
	}
	if !strings.Contains(output, "relay could not be verified yet") {
		t.Fatalf("expected relay-unreachable incomplete banner in output:\n%s", output)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if saved.HAHost != "ha-box.local" {
		t.Fatalf("saved.HAHost = %q, want %q", saved.HAHost, "ha-box.local")
	}
	if saved.HAURL != "https://ha-box.local:9443/custom" {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, "https://ha-box.local:9443/custom")
	}
	if saved.RelayBaseURL != "http://ha-box.local:8791" {
		t.Fatalf("saved.RelayBaseURL = %q, want %q", saved.RelayBaseURL, "http://ha-box.local:8791")
	}
}

func TestPromptValidHAHostRetriesDifferentAddressAndUsesNewHost(t *testing.T) {
	originalResolve := resolveHAURLBaseForSetup
	defer func() {
		resolveHAURLBaseForSetup = originalResolve
	}()
	resolveHAURLBaseForSetup = func(input string) (string, error) {
		if input == "192.168.1.123" {
			return "http://192.168.1.123:8123", nil
		}
		return "", fmt.Errorf("unreachable: %s", input)
	}

	reader := bufio.NewReader(strings.NewReader("homeassistant.local\ny\n192.168.1.123\n"))
	output := &strings.Builder{}

	host, haURL, err := promptValidHAHostFromReader(reader, output, "homeassistant.local")
	if err != nil {
		t.Fatalf("promptValidHAHostFromReader() error: %v", err)
	}
	if host != "192.168.1.123" {
		t.Fatalf("host = %q, want %q", host, "192.168.1.123")
	}
	if haURL != "http://192.168.1.123:8123" {
		t.Fatalf("haURL = %q, want %q", haURL, "http://192.168.1.123:8123")
	}
	if !strings.Contains(output.String(), "Checking connection to Home Assistant...") {
		t.Fatalf("expected visible connection check in output:\n%s", output.String())
	}
}

func TestPromptValidHAHostIgnoresStaleBlankAfterDiscoveryProgress(t *testing.T) {
	originalResolve := resolveHAURLBaseForSetup
	defer func() {
		resolveHAURLBaseForSetup = originalResolve
		clearSetupNextPromptSkipsStaleBlankInput()
	}()

	resolveHAURLBaseForSetup = func(input string) (string, error) {
		switch input {
		case "ha-box.local":
			return "http://ha-box.local:8123", nil
		case "192.168.1.123":
			return "http://192.168.1.123:8123", nil
		default:
			return "", fmt.Errorf("unreachable: %s", input)
		}
	}

	armSetupNextPromptSkipsStaleBlankInput()

	reader := bufio.NewReader(strings.NewReader("\n192.168.1.123\n"))
	output := &strings.Builder{}

	host, haURL, err := promptValidHAHostFromReader(reader, output, "ha-box.local")
	if err != nil {
		t.Fatalf("promptValidHAHostFromReader() error: %v", err)
	}
	if host != "192.168.1.123" {
		t.Fatalf("host = %q, want %q", host, "192.168.1.123")
	}
	if haURL != "http://192.168.1.123:8123" {
		t.Fatalf("haURL = %q, want %q", haURL, "http://192.168.1.123:8123")
	}
}

func TestPromptValidHAHostIgnoresMultipleStaleBlanksAfterDiscoveryProgress(t *testing.T) {
	originalResolve := resolveHAURLBaseForSetup
	defer func() {
		resolveHAURLBaseForSetup = originalResolve
		clearSetupNextPromptSkipsStaleBlankInput()
	}()

	resolveHAURLBaseForSetup = func(input string) (string, error) {
		switch input {
		case "ha-box.local":
			return "http://ha-box.local:8123", nil
		case "192.168.1.123":
			return "http://192.168.1.123:8123", nil
		default:
			return "", fmt.Errorf("unreachable: %s", input)
		}
	}

	armSetupNextPromptSkipsStaleBlankInput()

	reader := bufio.NewReader(strings.NewReader("\n\n192.168.1.123\n"))
	output := &strings.Builder{}

	host, haURL, err := promptValidHAHostFromReader(reader, output, "ha-box.local")
	if err != nil {
		t.Fatalf("promptValidHAHostFromReader() error: %v", err)
	}
	if host != "192.168.1.123" {
		t.Fatalf("host = %q, want %q", host, "192.168.1.123")
	}
	if haURL != "http://192.168.1.123:8123" {
		t.Fatalf("haURL = %q, want %q", haURL, "http://192.168.1.123:8123")
	}
}

func TestInteractiveSetupCompletedResumePersistsHostOnlyOverride(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": true})

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	originalResolve := resolveHAURLBaseForSetup
	originalFlagResolve := resolveHAURLBaseForFlags
	originalProbe := probeHTTPForSetup
	originalFetch := fetchRelayHealthForSetup
	originalReadinessFetch := fetchRelayHealthForReadiness
	originalReadinessWSPing := probeRelayWSPingForReadiness
	defer func() {
		resolveHAURLBaseForSetup = originalResolve
		resolveHAURLBaseForFlags = originalFlagResolve
		probeHTTPForSetup = originalProbe
		fetchRelayHealthForSetup = originalFetch
		fetchRelayHealthForReadiness = originalReadinessFetch
		probeRelayWSPingForReadiness = originalReadinessWSPing
	}()

	resolveHAURLBaseForSetup = func(input string) (string, error) {
		if input == "192.168.1.123" {
			return "http://192.168.1.123:8123", nil
		}
		return "", fmt.Errorf("unreachable: %s", input)
	}
	resolveHAURLBaseForFlags = resolveHAURLBaseForSetup
	probeHTTPForSetup = func(url string) error {
		if url == "http://192.168.1.123:8123" {
			return nil
		}
		return fmt.Errorf("unexpected probe: %s", url)
	}
	fetchRelayHealthForSetup = func(relayBaseURL, token string) ([]byte, error) {
		if relayBaseURL != "http://192.168.1.123:8791" {
			return nil, fmt.Errorf("unexpected relay url: %s", relayBaseURL)
		}
		if token != "test-relay-token" {
			return nil, fmt.Errorf("unexpected token: %s", token)
		}
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true}}`), nil
	}
	fetchRelayHealthForReadiness = fetchRelayHealthForSetup
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: 200, Body: []byte(`{"type":"pong"}`)}, nil
	}

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       "homeassistant.local",
		HAURL:        "http://homeassistant.local:8123",
		RelayBaseURL: "http://homeassistant.local:8791",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	state := loadStateOrDefault(paths)
	mergeStateClients(&state, []string{"claude"})
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)
	writeClaudeMarketplaceRegistrationFixture(t, home, filepath.Join(paths.ConfigDir, "claude-marketplace"))

	stdout, stderr := captureInteractiveSetupIO(t, "", func() int {
		return interactiveSetup(paths, cfg, state, "claude", "192.168.1.123", "", "", "", false)
	})
	output := stdout + stderr
	if !strings.Contains(output, "Everything is already set up!") {
		t.Fatalf("expected already-done banner, got:\n%s", output)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAHost != "192.168.1.123" {
		t.Fatalf("saved.HAHost = %q, want %q", saved.HAHost, "192.168.1.123")
	}
	if saved.HAURL != "http://192.168.1.123:8123" {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, "http://192.168.1.123:8123")
	}
	if saved.RelayBaseURL != "http://192.168.1.123:8791" {
		t.Fatalf("saved.RelayBaseURL = %q, want %q", saved.RelayBaseURL, "http://192.168.1.123:8791")
	}
}

func captureInteractiveSetupIO(t *testing.T, input string, fn func() int) (string, string) {
	t.Helper()
	withAllClientRuntimesAvailable(t)
	originalDiscovery := discoverReachableHAHostsForSetup
	discoverReachableHAHostsForSetup = func(cfg runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return nil, preferredUnverifiedHAHost(cfg)
	}
	defer func() { discoverReachableHAHostsForSetup = originalDiscovery }()

	home, err := os.UserHomeDir()
	if err == nil {
		logPath := filepath.Join(t.TempDir(), "claude.log")
		t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	originalStdin := os.Stdin
	originalStdout := os.Stdout
	originalStderr := os.Stderr
	defer func() {
		os.Stdin = originalStdin
		os.Stdout = originalStdout
		os.Stderr = originalStderr
	}()

	stdinFile, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatalf("CreateTemp(stdin) error: %v", err)
	}
	if _, err := stdinFile.WriteString(input); err != nil {
		t.Fatalf("WriteString(stdin) error: %v", err)
	}
	if _, err := stdinFile.Seek(0, 0); err != nil {
		t.Fatalf("Seek(stdin) error: %v", err)
	}
	os.Stdin = stdinFile

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stdout) error: %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe(stderr) error: %v", err)
	}
	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter

	stdoutDone := make(chan string, 1)
	stderrDone := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(stdoutReader)
		stdoutDone <- string(data)
	}()
	go func() {
		data, _ := io.ReadAll(stderrReader)
		stderrDone <- string(data)
	}()

	_ = fn()

	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()
	_ = stdinFile.Close()

	return <-stdoutDone, <-stderrDone
}

func repoRootForSetupTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error: %v", err)
	}
	if filepath.Base(wd) == "cli" {
		return filepath.Dir(wd)
	}
	return wd
}

// newDeadServerURL returns an address that refuses connections. Call it only
// after every live httptest server of the test is bound: closing frees the
// port, and a later bind in the same process can be handed exactly that port —
// silently turning the "dead" address into a live server (a real ci-gate
// flake: the relay server came up on the freed port, the unreachable Home
// Assistant address answered, and the repair menu never appeared).
func newDeadServerURL() string {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()
	return url
}

func TestInteractiveSetupFreshHostChangeRoutesBackThroughInstallStepsNotStraightToVerify(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	goodHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer goodHAServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	deadHAURL := newDeadServerURL()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       normalizeHostInput(deadHAURL),
		HAURL:        deadHAURL,
		RelayBaseURL: relayServer.URL,
	}

	// Fresh run with a --relay-token flag: no saved state, no saved token.
	// First verify fails against the dead HA address; picking "Change Home
	// Assistant address" (2) must NOT jump straight back to verification —
	// the new address may be a different instance, so the wizard re-runs the
	// install/token/LLAT steps for it (Codex P2 on the fresh-flow shortcut).
	input := joinSetupInputs(
		[]string{"2", goodHAServer.URL},
		[]string{"", ""},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "antigravity", "", "", "", "fresh-flag-token", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Change Home Assistant address",
		"could not be used. Enter a new one.",
		"Install NOVA Relay in Home Assistant",
		"Setup complete!",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if got := strings.Count(output, "Set up Relay Auth Token"); got < 2 {
		t.Fatalf("fresh-flow host change must re-run the token step for the new address; token step shown %d time(s):\n%s", got, output)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAURL != goodHAServer.URL {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, goodHAServer.URL)
	}
}

func TestInteractiveSetupFlagDrivenRunSkipsIntroEvenWithoutClientTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	// `ha-nova setup --host ...` with no client target and no saved config:
	// the run is flag-driven, so the first-run intro must not render even
	// though the flag overrides are applied after the intro check.
	stdout, stderr := captureInteractiveSetupIO(t, joinSetupInputs([]string{"exit"}), func() int {
		return interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "192.168.1.5", "", "", "", false)
	})

	output := stdout + stderr
	if strings.Contains(output, "connects your AI assistant") {
		t.Fatalf("flag-driven run must skip the intro:\n%s", output)
	}
	if !strings.Contains(output, "Setup cancelled") {
		t.Fatalf("expected clean exit path in output:\n%s", output)
	}
}

func TestInteractiveSetupFlaggedFreshHostChangeReenablesInstallSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	// Both HA addresses are reachable, but neither has a relay at the derived
	// <host>:8791, so verification fails with the connection repair menu.
	wrongHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer wrongHAServer.Close()

	goodHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer goodHAServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	// `ha-nova setup antigravity --host <wrong> --relay-token <tok>`: the
	// flag-driven shortcut skips the install walkthrough. When the user
	// interactively corrects the address, that shortcut is abandoned — the
	// wizard must re-run the install steps and the token step for the new
	// address instead of bouncing token → verify forever (Codex P2 round 3).
	input := joinSetupInputs(
		[]string{"2", goodHAServer.URL},
		[]string{"", ""},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "antigravity", wrongHAServer.URL, "", "", "flagged-token", false)
		return exitCode
	})
	// The corrected address still has no relay app either, so the run ends at
	// the incomplete banner — the point is the guidance it showed on the way.
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Change Home Assistant address",
		"Install NOVA Relay in Home Assistant",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if got := strings.Count(output, "Set up Relay Auth Token"); got < 2 {
		t.Fatalf("flagged fresh host change must re-run the token step; token step shown %d time(s):\n%s", got, output)
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAURL != goodHAServer.URL {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, goodHAServer.URL)
	}
}

func TestInteractiveSetupPastedTokenFreshHostChangeRoutesThroughInstallSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	goodHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer goodHAServer.Close()

	relayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"ha_ws_connected":true}}`))
	}))
	defer relayServer.Close()

	deadHAURL := newDeadServerURL()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cfg := runtimeConfig{
		HAHost:       normalizeHostInput(deadHAURL),
		HAURL:        deadHAURL,
		RelayBaseURL: relayServer.URL,
	}

	// Fresh run, NO saved state: the user pastes an existing token ("1"),
	// verification fails against the dead address, and "Change Home Assistant
	// address" (2) must route through the install steps for the corrected
	// address — a pasted token is not resume state (Codex P2 round 4).
	input := joinSetupInputs(
		setupWizardPasteRelayTokenPrompts("pasted-relay-token"),
		[]string{"2", goodHAServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardPasteRelayTokenPrompts("pasted-relay-token"),
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "antigravity", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 0 {
		t.Fatalf("interactiveSetup() exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Change Home Assistant address",
		"Install NOVA Relay in Home Assistant",
		"Setup complete!",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}

	saved, err := loadRuntimeConfig(paths)
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error: %v", err)
	}
	if saved.HAURL != goodHAServer.URL {
		t.Fatalf("saved.HAURL = %q, want %q", saved.HAURL, goodHAServer.URL)
	}
}

func TestInteractiveSetupConnectionRepairMenuOffersFullInstallSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	goodHAServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer goodHAServer.Close()

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	// Resume state (saved token) whose relay never becomes reachable — e.g.
	// the app was uninstalled from the instance. The connection repair menu
	// must offer "Run the app install steps for this address" (3) as a
	// structural escape into the full guidance instead of a retry dead end.
	cfg := runtimeConfig{
		HAHost:       normalizeHostInput(goodHAServer.URL),
		HAURL:        goodHAServer.URL,
		RelayBaseURL: "http://127.0.0.1:1",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("saved-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"3"},
		[]string{"", ""},
		[]string{"1"},
	)

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, cfg, loadStateOrDefault(paths), "antigravity", "", "", "", "", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	for _, want := range []string{
		"Run the app install steps for this address",
		"Install NOVA Relay in Home Assistant",
		"Keep saved token",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}
