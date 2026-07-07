package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSetupSecureStorageRecoveryFlowStopsOnFatalError(t *testing.T) {
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) { return true, nil }
	runPlatformSecureStorageRecoveryForSetup = func(platformSecureStorageRecoveryAction, []byte) error {
		return errors.New("local secure storage backend disappeared")
	}
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		return []byte("linux-local-keyring"), nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	state := setupSecureStorageRecoveryState{}
	_, err := runSetupSecureStorageRecoveryFlow(bufio.NewReader(strings.NewReader("\n")), io.Discard, desktopKeyringLockedError("default Secret Service collection is locked"), &state, setupSecureStorageRecoveryInitialAttempt)
	if err == nil || err.Error() != "local secure storage backend disappeared" {
		t.Fatalf("expected fatal recovery error, got %v", err)
	}
	if !state.initialAttempted {
		t.Fatal("expected initial recovery attempt to be recorded")
	}
}

func TestRunSetupSecureStorageRecoveryFlowRetriesOnlyOnRetryableError(t *testing.T) {
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) { return true, nil }
	runCalls := 0
	runPlatformSecureStorageRecoveryForSetup = func(platformSecureStorageRecoveryAction, []byte) error {
		runCalls++
		if runCalls == 1 {
			return localSecureStoragePasswordRejectedError()
		}
		return nil
	}
	readCalls := 0
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		readCalls++
		if readCalls == 1 {
			return []byte("wrong-password"), nil
		}
		return []byte("correct-password"), nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	var out bytes.Buffer
	state := setupSecureStorageRecoveryState{}
	result, err := runSetupSecureStorageRecoveryFlow(bufio.NewReader(strings.NewReader("\n\n")), &out, desktopKeyringLockedError("default Secret Service collection is locked"), &state, setupSecureStorageRecoveryInitialAttempt)
	if err != nil {
		t.Fatalf("expected retryable recovery flow to succeed, got %v", err)
	}
	if result != setupSecureStorageRecoveryRecovered {
		t.Fatalf("expected recovered result, got %q", result)
	}
	if !strings.Contains(out.String(), "local Linux keyring password was rejected") {
		t.Fatalf("expected explicit wrong-password wording, got:\n%s", out.String())
	}
	if runCalls != 2 {
		t.Fatalf("expected two recovery attempts, got %d", runCalls)
	}
}

func TestRunSetupSecureStorageRecoveryFlowRecomputesPlanAfterRetryableKeyringError(t *testing.T) {
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalInfer := inferPlatformSecureStorageRecoveryActionForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		inferPlatformSecureStorageRecoveryActionForSetup = originalInfer
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) { return true, nil }
	inferPlatformSecureStorageRecoveryActionForSetup = func(err error) (platformSecureStorageRecoveryAction, error) {
		switch {
		case isDesktopKeyringInitializationRequiredError(err):
			return platformSecureStorageRecoveryInitialize, nil
		case isDesktopKeyringLockedError(err):
			return platformSecureStorageRecoveryUnlock, nil
		default:
			return "", nil
		}
	}

	var actions []platformSecureStorageRecoveryAction
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, _ []byte) error {
		actions = append(actions, action)
		if len(actions) == 1 {
			return desktopKeyringLockedError("default Secret Service collection is locked")
		}
		return nil
	}
	secretReads := 0
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		secretReads++
		switch secretReads {
		case 1, 2:
			return []byte("new-keyring-password"), nil
		default:
			return []byte("existing-keyring-password"), nil
		}
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	var out bytes.Buffer
	state := setupSecureStorageRecoveryState{}
	result, err := runSetupSecureStorageRecoveryFlow(
		bufio.NewReader(strings.NewReader("\n\n")),
		&out,
		desktopKeyringInitializationRequiredError("no default Secret Service collection configured"),
		&state,
		setupSecureStorageRecoveryInitialAttempt,
	)
	if err != nil {
		t.Fatalf("expected recomputed recovery flow to succeed, got %v", err)
	}
	if result != setupSecureStorageRecoveryRecovered {
		t.Fatalf("expected recovered result, got %q", result)
	}
	if len(actions) != 2 || actions[0] != platformSecureStorageRecoveryInitialize || actions[1] != platformSecureStorageRecoveryUnlock {
		t.Fatalf("expected initialize then unlock actions, got %v", actions)
	}
	if !strings.Contains(out.String(), "Local secure storage is locked") {
		t.Fatalf("expected retry to show the recomputed unlock copy, got:\n%s", out.String())
	}
}

func TestRunSetupSecureStorageRecoveryFlowBackDoesNotConsumeAttempt(t *testing.T) {
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) { return true, nil }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	state := setupSecureStorageRecoveryState{}
	_, err := runSetupSecureStorageRecoveryFlow(bufio.NewReader(strings.NewReader("back\n")), io.Discard, desktopKeyringLockedError("default Secret Service collection is locked"), &state, setupSecureStorageRecoveryInitialAttempt)
	if err != errSetupBack {
		t.Fatalf("expected back navigation, got %v", err)
	}
	if state.initialAttempted {
		t.Fatal("expected back navigation not to consume the initial recovery attempt")
	}
}

func TestRunSetupSecureStorageRecoveryFlowInitializationConfirmsPassword(t *testing.T) {
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) { return true, nil }
	runCalls := 0
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, secret []byte) error {
		runCalls++
		if action != platformSecureStorageRecoveryInitialize {
			t.Fatalf("unexpected recovery action %q", action)
		}
		if string(secret) != "correct-password" {
			t.Fatalf("unexpected recovery secret %q", string(secret))
		}
		return nil
	}
	secretReads := 0
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		secretReads++
		switch secretReads {
		case 1:
			return []byte("first-password"), nil
		case 2:
			return []byte("different-password"), nil
		default:
			return []byte("correct-password"), nil
		}
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	var out bytes.Buffer
	state := setupSecureStorageRecoveryState{}
	result, err := runSetupSecureStorageRecoveryFlow(
		bufio.NewReader(strings.NewReader("\n\n")),
		&out,
		desktopKeyringInitializationRequiredError("no default Secret Service collection configured"),
		&state,
		setupSecureStorageRecoveryInitialAttempt,
	)
	if err != nil {
		t.Fatalf("expected initialization recovery flow to succeed, got %v", err)
	}
	if result != setupSecureStorageRecoveryRecovered {
		t.Fatalf("expected recovered result, got %q", result)
	}
	if !strings.Contains(out.String(), "Passwords did not match.") {
		t.Fatalf("expected mismatch guidance, got:\n%s", out.String())
	}
	if runCalls != 1 {
		t.Fatalf("expected one successful recovery call after confirmation, got %d", runCalls)
	}
}

func TestInteractiveSetupRecoveryBackDoesNotBypassGate(t *testing.T) {
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
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		return []byte("linux-local-keyring"), nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", "back", "4", "", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
		setupWizardLLATPrompts(),
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
	if strings.Count(output, "Local secure storage needs setup") != 2 {
		t.Fatalf("expected recovery page twice after backing out once:\n%s", output)
	}
	if recoveryCalls != 1 {
		t.Fatalf("expected one successful recovery call after returning, got %d", recoveryCalls)
	}
}

func TestInteractiveSetupRecoversWhenSavedTokenReadNeedsRecovery(t *testing.T) {
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
	originalReadToken := readRelayAuthTokenForSetup
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
		readRelayAuthTokenForSetup = originalReadToken
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	relayAuthTokenSetupPreflightForSetup = func() error { return nil }
	readCalls := 0
	readRelayAuthTokenForSetup = func() (string, error) {
		readCalls++
		if readCalls == 1 {
			return "", desktopKeyringLockedError("default Secret Service collection is locked")
		}
		return "", missingRelayAuthTokenError(relayAuthTokenServiceName())
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, secret []byte) error {
		if action != platformSecureStorageRecoveryUnlock {
			t.Fatalf("unexpected recovery action %q", action)
		}
		if string(secret) != "linux-local-keyring" {
			t.Fatalf("unexpected recovery secret %q", string(secret))
		}
		return nil
	}
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
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
		setupWizardLLATPrompts(),
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
	recoveryIdx := strings.Index(output, "Local secure storage is locked")
	hostIdx := strings.Index(output, "Home Assistant address")
	if recoveryIdx == -1 || hostIdx == -1 || recoveryIdx > hostIdx {
		t.Fatalf("expected saved-token recovery before host prompt:\n%s", output)
	}
}

func TestInteractiveSetupReusesSavedTokenAfterReadRecovery(t *testing.T) {
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
	originalReadToken := readRelayAuthTokenForSetup
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
		readRelayAuthTokenForSetup = originalReadToken
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	relayAuthTokenSetupPreflightForSetup = func() error { return nil }
	readCalls := 0
	readRelayAuthTokenForSetup = func() (string, error) {
		readCalls++
		if readCalls == 1 {
			return "", desktopKeyringLockedError("default Secret Service collection is locked")
		}
		return "saved-relay-token", nil
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, secret []byte) error {
		if action != platformSecureStorageRecoveryUnlock {
			t.Fatalf("unexpected recovery action %q", action)
		}
		if string(secret) != "linux-local-keyring" {
			t.Fatalf("unexpected recovery secret %q", string(secret))
		}
		return nil
	}
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
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
		setupWizardLLATPrompts(),
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
	if !strings.Contains(output, "Existing relay token found") {
		t.Fatalf("expected saved token reuse after recovery:\n%s", output)
	}
	if strings.Contains(output, "Here is your relay token (generated automatically)") {
		t.Fatalf("did not expect a new relay token to be generated after recovery:\n%s", output)
	}
}

func TestInteractiveSetupDeclinedReadRecoveryMentionsSavedTokenAccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	originalPreflight := relayAuthTokenSetupPreflightForSetup
	originalReadToken := readRelayAuthTokenForSetup
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
		readRelayAuthTokenForSetup = originalReadToken
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	relayAuthTokenSetupPreflightForSetup = func() error { return nil }
	readRelayAuthTokenForSetup = func() (string, error) {
		return "", desktopKeyringLockedError("default Secret Service collection is locked")
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, joinSetupInputs([]string{"4", "n"}), func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "", "", "", "http://relay.test:8791", "", false)
		return exitCode
	})
	if exitCode != 1 {
		t.Fatalf("interactiveSetup() exit = %d, want 1\nstdout:\n%s\nstderr:\n%s", exitCode, stdout, stderr)
	}

	output := stdout + stderr
	if !strings.Contains(output, "cannot access saved relay token") {
		t.Fatalf("expected saved-token access wording when recovery is declined:\n%s", output)
	}
}

func TestInteractiveSetupRetriesSaveTimeRecoveryInline(t *testing.T) {
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
	originalWrite := writeRelayAuthTokenForSetupPersistence
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
		writeRelayAuthTokenForSetupPersistence = originalWrite
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	relayAuthTokenSetupPreflightForSetup = func() error { return nil }
	writeCalls := 0
	writeRelayAuthTokenForSetupPersistence = func(token string) error {
		writeCalls++
		if writeCalls == 1 {
			return desktopKeyringLockedError("default Secret Service collection is locked")
		}
		return originalWrite(token)
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}
	recoveryCalls := 0
	runPlatformSecureStorageRecoveryForSetup = func(action platformSecureStorageRecoveryAction, secret []byte) error {
		recoveryCalls++
		if action != platformSecureStorageRecoveryUnlock {
			t.Fatalf("unexpected recovery action %q", action)
		}
		if string(secret) != "linux-local-keyring" {
			t.Fatalf("unexpected recovery secret %q", string(secret))
		}
		return nil
	}
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		return []byte("linux-local-keyring"), nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
		setupWizardLLATPrompts(),
		[]string{""},
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
	if !strings.Contains(output, "Verifying connection") || !strings.Contains(output, "Local secure storage is locked") {
		t.Fatalf("expected verify stage followed by inline recovery:\n%s", output)
	}
	if recoveryCalls != 1 {
		t.Fatalf("expected one save-time recovery call, got %d", recoveryCalls)
	}
	if writeCalls != 2 {
		t.Fatalf("expected one failed save plus one retry, got %d writes", writeCalls)
	}
}

func TestInteractiveSetupRetriesSaveTimeInitializationRecoveryInline(t *testing.T) {
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
	originalWrite := writeRelayAuthTokenForSetupPersistence
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	originalRunRecovery := runPlatformSecureStorageRecoveryForSetup
	originalReadSecret := readSetupSecretInputForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInputTTY := uiInputSupportsTTY
	defer func() {
		relayAuthTokenSetupPreflightForSetup = originalPreflight
		writeRelayAuthTokenForSetupPersistence = originalWrite
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
		runPlatformSecureStorageRecoveryForSetup = originalRunRecovery
		readSetupSecretInputForSetup = originalReadSecret
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInputTTY
	}()

	relayAuthTokenSetupPreflightForSetup = func() error { return nil }
	writeCalls := 0
	writeRelayAuthTokenForSetupPersistence = func(token string) error {
		writeCalls++
		if writeCalls == 1 {
			return desktopKeyringInitializationRequiredError("no default Secret Service collection configured")
		}
		return originalWrite(token)
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
	readSetupSecretInputForSetup = func(int) ([]byte, error) {
		return []byte("linux-local-keyring"), nil
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	input := joinSetupInputs(
		[]string{"4", haServer.URL},
		setupWizardRelayInstallPrompts(),
		setupWizardGenerateRelayTokenPrompts(),
		setupWizardLLATPrompts(),
		[]string{""},
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
	if !strings.Contains(output, "Verifying connection") || !strings.Contains(output, "Local secure storage needs setup") {
		t.Fatalf("expected verify stage followed by inline initialization recovery:\n%s", output)
	}
	if !strings.Contains(output, "Set up local secure storage now") {
		t.Fatalf("expected initialization recovery prompt:\n%s", output)
	}
	if !strings.Contains(output, "Setup complete!") {
		t.Fatalf("expected setup to complete after inline initialization recovery:\n%s", output)
	}
	if recoveryCalls != 1 {
		t.Fatalf("expected one save-time initialization recovery call, got %d", recoveryCalls)
	}
	if writeCalls != 2 {
		t.Fatalf("expected one failed save plus one retry, got %d writes", writeCalls)
	}
}
