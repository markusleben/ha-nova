package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// Regression pack (live 2026-07-20, headless agent VM): a present-but-locked
// desktop keyring — gnome-keyring autostarts as a user unit, but nobody ever
// logs in graphically — blocked device pairing with no discoverable way out,
// even though `.hermes/INSTALL.md` documents `setup --service` as exactly that
// way out. These tests pin the explicit file-backend opt-ins.

// resetKeyringDeviceSlots empties the mock-keyring device slots NOW and again
// at cleanup — the package-wide keyring mock is shared state, so a credential
// leaked by any earlier test would make these migration-aware tests see a
// pairing that never existed for them (and vice versa).
func resetKeyringDeviceSlots(t *testing.T) {
	t.Helper()
	clear := func() {
		user := secretUser()
		_ = keyring.Delete(deviceCredentialService, user)
		_ = keyring.Delete(deviceCredentialPendingService, user)
	}
	clear()
	t.Cleanup(clear)
}

func TestProbeLockedKeyringErrorNamesTheExplicitFileOptIns(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"locked", desktopKeyringLockedError("default Secret Service collection is locked")},
		{"uninitialized", desktopKeyringInitializationRequiredError("no default collection exists")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withDeviceStorageTestHome(t)
			stubStorageCanaries(t, tc.err)

			_, err := probeDeviceCredentialStorage()
			if err == nil {
				t.Fatal("expected the locked/uninitialized keyring to stay a hard error")
			}
			if !isDesktopKeyringLockedError(err) && !isDesktopKeyringInitializationRequiredError(err) {
				t.Fatalf("error lost its keyring-state identity: %v", err)
			}
			for _, want := range []string{"--service", "ha-nova pair --credential-store=file"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error must name %q, got %q", want, err)
				}
			}
			if deviceCredentialFileModeForced {
				t.Fatal("a locked keyring must never silently force file mode")
			}
		})
	}
}

func TestRunPairCommandCredentialStoreFileBypassesLockedKeyring(t *testing.T) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	stubStorageCanaries(t, desktopKeyringLockedError("default Secret Service collection is locked"))
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.json")
	paths := runtimePaths{ConfigDir: dir, ConfigFile: configFile}
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: "http://ha:8791"}); err != nil {
		t.Fatal(err)
	}

	paired := false
	orig := runSecurePairingForPairCmd
	runSecurePairingForPairCmd = func(bootstrapURL, code string, cfg *runtimeConfig, saveCfg func(*runtimeConfig) error, info pairingClientInfo) (string, error) {
		paired = true
		return "dev-1", nil
	}
	defer func() { runSecurePairingForPairCmd = orig }()

	// Without the opt-in the locked keyring stays a hard, code-preserving error.
	if rc := runPairCommand(paths, []string{"--code", "123456"}); rc == 0 {
		t.Fatal("locked keyring without the opt-in must fail")
	}
	if paired {
		t.Fatal("pairing must not start when the storage probe fails")
	}

	// The explicit opt-in routes the same machine to the file backend and pairs.
	if rc := runPairCommand(paths, []string{"--code", "123456", "--credential-store=file"}); rc != 0 {
		t.Fatalf("rc=%d, want 0 with --credential-store=file", rc)
	}
	if !paired {
		t.Fatal("pairing must run under the file opt-in")
	}
	// The pairing hook is stubbed, so no credential was promoted — the marker
	// must not exist yet (it persists only at promotion).
	if deviceFileBackendMarkerExists() {
		t.Fatal("marker must not persist before a credential is promoted")
	}
}

func TestRunPairCommandRejectsInvalidCredentialStore(t *testing.T) {
	withDeviceStorageTestHome(t)
	for _, args := range [][]string{
		{"--credential-store", "keyring"},
		{"--credential-store=banana"},
		{"--credential-store="},
		{"--credential-store"},
	} {
		if rc := runPairCommand(runtimePaths{}, args); rc != 1 {
			t.Fatalf("args %v: rc=%d, want 1", args, rc)
		}
		if deviceCredentialFileModeForced {
			t.Fatalf("args %v: an invalid value must not force file mode", args)
		}
	}
}

func TestInteractiveServiceSetupReachesPairingWithLockedKeyring(t *testing.T) {
	// `setup --service` documents that the device credential lands in a protected
	// file. With the keyring present but locked, the wizard must therefore reach
	// the pairing stage in file mode instead of dying at the storage probe.
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	stubStorageCanaries(t, desktopKeyringLockedError("default Secret Service collection is locked"))
	t.Setenv("HA_NOVA_NO_BROWSER", "1")
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))

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

	// Reach the six-digit code prompt, then cancel (never drives real OPAQUE).
	input := joinSetupInputs([]string{"", "", "", "", "exit"})
	exitCode := 0
	stdout, stderr := captureInteractiveSetupIO(t, input, func() int {
		exitCode = interactiveSetup(paths, runtimeConfig{}, loadStateOrDefault(paths), "hermes",
			normalizeHostInput(haServer.URL), haServer.URL, relayServer.URL, "", true)
		return exitCode
	})
	output := stdout + stderr
	if exitCode == 1 {
		t.Fatalf("service setup died at the storage probe with a locked keyring (want: pairing stage):\n%s", output)
	}
	if strings.Contains(output, "cannot store the device credential") {
		t.Fatalf("service setup surfaced the locked-keyring storage error:\n%s", output)
	}
	if !strings.Contains(output, "Pair this device") {
		t.Fatalf("service setup did not reach the pairing stage:\n%s", output)
	}
}

func TestMinRelayVersionParityAcrossVersionFiles(t *testing.T) {
	// min_relay_version lives in TWO version.json files with no release-script
	// guard; a drifted pair would show users an inconsistent floor.
	root, err := readVersionJSON(filepath.Join("..", "version.json"))
	if err != nil {
		t.Fatalf("read root version.json: %v", err)
	}
	nova, err := readVersionJSON(filepath.Join("..", "nova", "version.json"))
	if err != nil {
		t.Fatalf("read nova/version.json: %v", err)
	}
	if root.MinRelayVersion == "" || root.MinRelayVersion != nova.MinRelayVersion {
		t.Fatalf("min_relay_version drift: root %q vs nova %q", root.MinRelayVersion, nova.MinRelayVersion)
	}
	if root.SkillVersion == "" || root.SkillVersion != nova.SkillVersion {
		t.Fatalf("skill_version drift: root %q vs nova %q", root.SkillVersion, nova.SkillVersion)
	}
}
