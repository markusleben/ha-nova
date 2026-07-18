package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunDoctorTreatsWSPingSuccessAsReady(t *testing.T) {
	paths, cfg := doctorTestSetup(t)

	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode != 0 {
		t.Fatalf("runDoctor() exit = %d, want 0\n%s", exitCode, output)
	}
	for _, want := range []string{
		"Relay health reachable",
		"Relay /ws ping succeeded",
		"Connected to Home Assistant",
		"Doctor checks passed",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
	_ = cfg
}

func TestRunDoctorMentionsUpstreamAuthCause(t *testing.T) {
	paths, _ := doctorTestSetup(t)

	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusBadGateway, Body: []byte("LLAT is required")}, nil
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to fail when ws ping proves an upstream auth issue:\n%s", output)
	}
	if !strings.Contains(output, "Relay upstream authentication was rejected; update/restart the App, or replace HA_LLAT for standalone Container/Core") {
		t.Fatalf("expected upstream auth guidance in doctor output:\n%s", output)
	}
}

func TestRunDoctorDoesNotClaimConnectedWhenHAProbeFails(t *testing.T) {
	paths, cfg := doctorTestSetup(t)

	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	if err := saveConfig(paths, runtimeConfig{
		HAHost:       "192.168.1.250",
		HAURL:        "http://192.168.1.250:8123",
		RelayBaseURL: cfg.RelayBaseURL,
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to fail when direct HA probe fails:\n%s", output)
	}
	if strings.Contains(output, "Connected to Home Assistant") {
		t.Fatalf("doctor should not claim connected when HA probe failed:\n%s", output)
	}
}

func TestRunDoctorReportsSecureStoreUnavailableInsteadOfMissingToken(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")

	blockedPath := filepath.Join(home, "blocked-token")
	if err := os.MkdirAll(blockedPath, 0o755); err != nil {
		t.Fatalf("mkdir blocked token path: %v", err)
	}
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", blockedPath)

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(haServer.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:       normalizeHostInput(haServer.URL),
		HAURL:        haServer.URL,
		RelayBaseURL: "http://relay.test:8791",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to fail when secure storage is unavailable:\n%s", output)
	}
	if !strings.Contains(output, "relay auth token unavailable:") {
		t.Fatalf("expected secure-storage unavailable wording:\n%s", output)
	}
	if strings.Contains(output, "relay auth token missing; run: ha-nova setup") {
		t.Fatalf("doctor should not collapse secure-store failures into missing-token wording:\n%s", output)
	}
}

func TestRunDoctorShowsRepairHintForDetachedConfiguredClaude(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": false})

	paths, _ := doctorTestSetup(t)
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"claude"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to degrade for detached Claude:\n%s", output)
	}
	if !strings.Contains(output, "Claude Code is not attached correctly") {
		t.Fatalf("expected detached Claude warning:\n%s", output)
	}
	if !strings.Contains(output, "Repair: run `ha-nova setup claude`.") {
		t.Fatalf("expected concrete Claude repair hint:\n%s", output)
	}
}

func TestRunDoctorShowsDevSyncHintForDetachedConfiguredClaudeOnDevInstall(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})
	withClientAttachmentPresence(t, map[string]bool{"claude": false})

	paths, _ := doctorTestSetup(t)
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"claude"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to degrade for detached Claude on dev install:\n%s", output)
	}
	if !strings.Contains(output, "Repair: run `npm run dev:sync` or `ha-nova setup claude`.") {
		t.Fatalf("expected repo/dev Claude repair hint:\n%s", output)
	}
}

func TestRunDoctorShowsRepairHintWhenClaudeMarketplaceMissing(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"claude": true})

	paths, _ := doctorTestSetup(t)
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"claude"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	installPath := filepath.Join(paths.Home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.3.2")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	pluginsDir := filepath.Join(paths.Home, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error: %v", err)
	}
	installed := `{"plugins":{"ha-nova@ha-nova":[{"scope":"user","installPath":"` + installPath + `","version":"0.3.2"}]}}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(installed), 0o644); err != nil {
		t.Fatalf("WriteFile(installed_plugins.json) error: %v", err)
	}
	marketplaces := `{"claude-plugins-official":{"source":{"source":"github","repo":"anthropics/claude-plugins-official"}}}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "known_marketplaces.json"), []byte(marketplaces), 0o644); err != nil {
		t.Fatalf("WriteFile(known_marketplaces.json) error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to degrade when Claude marketplace is missing:\n%s", output)
	}
	if !strings.Contains(output, "Claude Code is not attached correctly") {
		t.Fatalf("expected detached Claude warning for missing marketplace:\n%s", output)
	}
	if !strings.Contains(output, "Repair: run `ha-nova setup claude`.") {
		t.Fatalf("expected concrete Claude repair hint for missing marketplace:\n%s", output)
	}
}

func doctorTestSetup(t *testing.T) (runtimePaths, runtimeConfig) {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".config", "ha-nova", ".test-relay-auth-token"))

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(haServer.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	cfg := runtimeConfig{
		HAHost:       normalizeHostInput(haServer.URL),
		HAURL:        haServer.URL,
		RelayBaseURL: "http://relay.test:8791",
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
	return paths, cfg
}

func captureCommandOutput(t *testing.T, fn func() int) (int, string) {
	t.Helper()

	originalStdout := os.Stdout
	originalStderr := os.Stderr
	defer func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
	}()

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

	exitCode := fn()

	_ = stdoutWriter.Close()
	_ = stderrWriter.Close()

	return exitCode, (<-stdoutDone) + (<-stderrDone)
}

func TestRunDoctorShowsInteractiveRecoveryHintForRecoverableLinuxSecureStorage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(haServer.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := saveConfig(paths, runtimeConfig{
		HAHost:       normalizeHostInput(haServer.URL),
		HAURL:        haServer.URL,
		RelayBaseURL: "http://relay.test:8791",
	}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}

	originalRead := readRelayAuthTokenForDoctor
	originalSupport := detectPlatformSecureStorageRecoverySupportForSetup
	defer func() {
		readRelayAuthTokenForDoctor = originalRead
		detectPlatformSecureStorageRecoverySupportForSetup = originalSupport
	}()
	readRelayAuthTokenForDoctor = func() (string, error) {
		return "", desktopKeyringLockedError("default Secret Service collection is locked")
	}
	detectPlatformSecureStorageRecoverySupportForSetup = func() (bool, error) {
		return true, nil
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to fail when secure storage needs recovery:\n%s", output)
	}
	if !strings.Contains(output, "Recovery: run `ha-nova setup` interactively to unlock local secure storage on this Linux machine.") {
		t.Fatalf("expected interactive recovery hint:\n%s", output)
	}
}

func TestRunDoctorReportsConfiguredServiceTokenFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("service token files are not supported on native Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	haServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(haServer.Close)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	cfg := runtimeConfig{
		HAHost:         normalizeHostInput(haServer.URL),
		HAURL:          haServer.URL,
		RelayBaseURL:   "http://relay.test:8791",
		RelayTokenFile: defaultRelayAuthTokenFile(paths),
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthTokenFile(cfg.RelayTokenFile, "test-relay-token"); err != nil {
		t.Fatalf("writeRelayAuthTokenFile() error = %v", err)
	}

	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode != 0 {
		t.Fatalf("runDoctor() exit = %d, want 0\n%s", exitCode, output)
	}
	if !strings.Contains(output, "Relay auth token present in service token file") {
		t.Fatalf("expected service-token-file wording:\n%s", output)
	}
}

func TestRunDoctorShowsRepairHintForDetachedConfiguredHermes(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})

	paths, _ := doctorTestSetup(t)
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	state := loadStateOrDefault(paths)
	state.InstalledClients = []string{"hermes"}
	if err := saveState(paths, state); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to degrade for detached Hermes:\n%s", output)
	}
	if !strings.Contains(output, "Hermes Agent configured, but HA NOVA is not attached") {
		t.Fatalf("expected detached Hermes warning:\n%s", output)
	}
	if !strings.Contains(output, "Repair: run `ha-nova setup hermes`.") {
		t.Fatalf("expected concrete Hermes repair hint:\n%s", output)
	}
}

func TestRunDoctorShowsRepairHintForLegacyHermesBundleWithoutState(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"hermes": true})

	paths, _ := doctorTestSetup(t)
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()
	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":true},"version":"0.1.12"}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	hermesRoot := filepath.Join(paths.Home, ".hermes", "skills", "ha-nova")
	if err := os.MkdirAll(filepath.Join(hermesRoot, "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir Hermes context skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hermesRoot, "ha-nova", "SKILL.md"), []byte("name: ha-nova"), 0o644); err != nil {
		t.Fatalf("write Hermes context skill: %v", err)
	}
	for _, skillDir := range hermesLegacyRequiredSkillDirs[1:] {
		if err := os.MkdirAll(filepath.Join(hermesRoot, skillDir), 0o755); err != nil {
			t.Fatalf("mkdir legacy Hermes skill %s: %v", skillDir, err)
		}
		if err := os.WriteFile(filepath.Join(hermesRoot, skillDir, "SKILL.md"), []byte("name: "+skillDir), 0o644); err != nil {
			t.Fatalf("write legacy Hermes skill %s: %v", skillDir, err)
		}
	}

	exitCode, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, nil)
	})
	if exitCode == 0 {
		t.Fatalf("expected doctor to degrade for legacy Hermes bundle:\n%s", output)
	}
	if !strings.Contains(output, "Hermes Agent configured, but HA NOVA is not attached") {
		t.Fatalf("expected legacy Hermes warning:\n%s", output)
	}
	if !strings.Contains(output, "Repair: run `ha-nova setup hermes`.") {
		t.Fatalf("expected legacy Hermes repair hint:\n%s", output)
	}
}
