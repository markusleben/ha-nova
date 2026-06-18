package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupHealableInstall builds a non-dev bundle install at the install root with
// the running binary resolving there (not a backup, not a dev root), so
// detectInstallSource classifies it as a real bundle and resolveSourceRoot
// returns the install root. Clients are forced "runtime detected".
func setupHealableInstall(t *testing.T) runtimePaths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_DEV_ROOT", "")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	writeMinimalBundleTree(t, paths.InstallRoot, "0.6.1", true)
	mockInstallSourceExe(t, filepath.Join(paths.InstallRoot, publicBinaryName()))

	originalRuntimeDetected := clientRuntimeDetectedForStatus
	clientRuntimeDetectedForStatus = func(string) bool { return true }
	t.Cleanup(func() { clientRuntimeDetectedForStatus = originalRuntimeDetected })

	return paths
}

func TestAllTrackedClientsSynced(t *testing.T) {
	cases := []struct {
		name    string
		tracked []string
		synced  []string
		want    bool
	}{
		{"all tracked synced", []string{"codex", "hermes"}, []string{"codex", "hermes"}, true},
		{"subset leaves a tracked client unsynced", []string{"codex", "hermes"}, []string{"codex"}, false},
		{"fresh install (nothing tracked)", nil, []string{"codex"}, true},
		{"synced superset still covers tracked", []string{"codex"}, []string{"codex", "hermes"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allTrackedClientsSynced(tc.tracked, tc.synced); got != tc.want {
				t.Fatalf("allTrackedClientsSynced(%v, %v) = %v, want %v", tc.tracked, tc.synced, got, tc.want)
			}
		})
	}
}

func TestEnsureClientsVerifiedSelfHealsOnceThenNoOp(t *testing.T) {
	paths := setupHealableInstall(t)
	codexLink := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")

	// Buggy-update state: version recorded, but the client marker lags (a pre-0.6.1
	// binary never wrote it).
	if err := saveState(paths, installState{
		SchemaVersion:    stateSchemaVersion,
		Version:          "0.6.1",
		InstalledClients: []string{"codex"},
	}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	ensureClientsVerifiedForCurrentVersion(paths)

	if _, err := os.Lstat(codexLink); err != nil {
		t.Fatalf("expected Codex to be re-synced by the self-heal: %v", err)
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if state.ClientsVerifiedVersion != "0.6.1" {
		t.Fatalf("expected marker stamped to 0.6.1, got %q", state.ClientsVerifiedVersion)
	}

	// Second call must be a no-op: remove the attachment and confirm the matching
	// marker short-circuits before any re-sync.
	if err := os.RemoveAll(codexLink); err != nil {
		t.Fatalf("remove codex link: %v", err)
	}
	ensureClientsVerifiedForCurrentVersion(paths)
	if _, err := os.Lstat(codexLink); !os.IsNotExist(err) {
		t.Fatalf("expected no re-sync once the marker matches (err=%v)", err)
	}
}

// TestPostUpdateSyncLeavesMarkerUnstampedWhenClientSkipped guards Codex's second
// P1: a tracked client whose runtime is absent in this environment is skipped, and
// the marker must stay unstamped so the self-heal repairs it once the runtime
// reappears (rather than short-circuiting forever on a premature marker).
func TestPostUpdateSyncLeavesMarkerUnstampedWhenClientSkipped(t *testing.T) {
	paths := setupHealableInstall(t)
	// Override the runtime probe so the tracked client is skipped, not synced.
	clientRuntimeDetectedForStatus = func(string) bool { return false }

	if err := saveState(paths, installState{
		SchemaVersion:    stateSchemaVersion,
		Version:          "0.6.1",
		InstalledClients: []string{"codex"},
	}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	// A skip is not a failure, so postUpdateSync returns nil.
	if err := postUpdateSync(paths); err != nil {
		t.Fatalf("postUpdateSync() error: %v", err)
	}

	state, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if state.ClientsVerifiedVersion != "" {
		t.Fatalf("a skipped (runtime-absent) tracked client must leave the marker unstamped, got %q", state.ClientsVerifiedVersion)
	}
	if state.Version != "0.6.1" {
		t.Fatalf("state.Version should still advance to the running version, got %q", state.Version)
	}
}

func TestEnsureClientsVerifiedSkipsDevBuild(t *testing.T) {
	paths := setupHealableInstall(t)
	codexLink := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")

	originalChannel := BuildChannel
	BuildChannel = "dev"
	t.Cleanup(func() { BuildChannel = originalChannel })

	if err := saveState(paths, installState{
		SchemaVersion:    stateSchemaVersion,
		Version:          "0.6.1",
		InstalledClients: []string{"codex"},
	}); err != nil {
		t.Fatalf("saveState() error: %v", err)
	}

	ensureClientsVerifiedForCurrentVersion(paths)

	if _, err := os.Lstat(codexLink); !os.IsNotExist(err) {
		t.Fatalf("dev build must never self-heal clients (err=%v)", err)
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if state.ClientsVerifiedVersion != "" {
		t.Fatalf("dev build must not stamp the marker, got %q", state.ClientsVerifiedVersion)
	}
}

func TestEnsureClientsVerifiedSkipsPreSetupInstall(t *testing.T) {
	paths := setupHealableInstall(t)
	codexLink := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")

	// No state file at all (pre-setup): nothing to heal, and we must not create
	// state before the user has set up.
	ensureClientsVerifiedForCurrentVersion(paths)

	if _, err := os.Lstat(codexLink); !os.IsNotExist(err) {
		t.Fatalf("pre-setup install must not sync clients (err=%v)", err)
	}
	if _, err := os.Stat(paths.StateFile); !os.IsNotExist(err) {
		t.Fatalf("pre-setup self-heal must not create state (err=%v)", err)
	}
}

// TestCheckUpdateHumanPathHealsAndJSONStaysClean proves the real delivery path:
// `check-update` (human/quiet — what every client runs on first skill use)
// triggers the self-heal, while `check-update --json` (machine-read) must NOT
// interleave self-heal output on stdout.
func TestCheckUpdateHumanPathHealsAndJSONStaysClean(t *testing.T) {
	paths := setupHealableInstall(t)
	codexLink := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")

	// Fresh release cache so the update check is offline-stable (current == latest
	// == up to date).
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{Version: "0.6.1", HTMLURL: "https://example.invalid/releases/v0.6.1"}, 0o644); err != nil {
		t.Fatalf("write update cache: %v", err)
	}

	saveHealState := func() {
		if err := saveState(paths, installState{
			SchemaVersion:    stateSchemaVersion,
			Version:          "0.6.1",
			InstalledClients: []string{"codex"},
		}); err != nil {
			t.Fatalf("saveState() error: %v", err)
		}
	}

	// --json: marker stale, but the machine path must skip the heal so stdout stays
	// pure JSON.
	saveHealState()
	output := captureStdout(t, func() {
		if exitCode := runCheckUpdate(paths, []string{"--json"}); exitCode != 0 {
			t.Fatalf("runCheckUpdate(--json) exit = %d, want 0", exitCode)
		}
	})
	if strings.Contains(output, "Client synced") {
		t.Fatalf("--json output must not interleave self-heal lines, got:\n%s", output)
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "{") {
		t.Fatalf("--json output must be pure JSON, got:\n%s", output)
	}
	if _, err := os.Lstat(codexLink); !os.IsNotExist(err) {
		t.Fatalf("--json path must not self-heal (err=%v)", err)
	}

	// --quiet (human path): heals once and stamps the marker.
	saveHealState()
	if exitCode := runCheckUpdate(paths, []string{"--quiet"}); exitCode != 0 {
		t.Fatalf("runCheckUpdate(--quiet) exit = %d, want 0", exitCode)
	}
	if _, err := os.Lstat(codexLink); err != nil {
		t.Fatalf("expected check-update human path to self-heal Codex: %v", err)
	}
	state, err := loadState(paths)
	if err != nil {
		t.Fatalf("loadState() error: %v", err)
	}
	if state.ClientsVerifiedVersion != "0.6.1" {
		t.Fatalf("expected marker stamped to 0.6.1 after check-update, got %q", state.ClientsVerifiedVersion)
	}
}
