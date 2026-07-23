package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRelayCommandsRepairOldCopiedSkillsDespiteCurrentMarkerAndMissingRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Hermes is WSL-only and this fixture covers all copied layouts together")
	}
	cases := []struct {
		name      string
		args      []string
		stateMode string
		wantJSON  string
	}{
		{
			name:      "health with misleading current marker",
			args:      []string{"health"},
			stateMode: "current",
			wantJSON:  `{"ok":true,"data":{"status":"ok","version":"0.6.1"}}`,
		},
		{
			name:      "core with state missing",
			args:      []string{"core", "--method", "GET", "--path", "/api/states"},
			stateMode: "missing",
			wantJSON:  `{"ok":true,"data":{"status":200,"body":[]}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupHealableInstall(t)
			sourceRoot := paths.InstallRoot
			codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
			openCodeRoot := filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova")
			antigravityRoot := antigravitySkillsRoot(paths.Home)
			hermesRoot := filepath.Join(paths.Home, ".hermes", "skills", "ha-nova")
			if _, err := installTreeClient(filepath.Dir(codexRoot), filepath.Join(sourceRoot, "skills"), false); err != nil {
				t.Fatalf("seed copied Codex tree: %v", err)
			}
			if _, err := installTreeClient(filepath.Dir(openCodeRoot), filepath.Join(sourceRoot, "skills"), false); err != nil {
				t.Fatalf("seed copied OpenCode tree: %v", err)
			}
			if err := installAntigravityClient(paths.Home, sourceRoot); err != nil {
				t.Fatalf("seed copied Antigravity skills: %v", err)
			}
			if err := installHermesClient(paths.Home, sourceRoot); err != nil {
				t.Fatalf("seed copied Hermes skills: %v", err)
			}

			layouts := []struct {
				name      string
				bootstrap string
				context   string
				read      string
				readName  string
			}{
				{
					name:      "codex",
					bootstrap: filepath.Join(codexRoot, "ha-nova", "session-bootstrap.md"),
					context:   filepath.Join(codexRoot, "ha-nova", "SKILL.md"),
					read:      filepath.Join(codexRoot, "read", "SKILL.md"),
					readName:  "read",
				},
				{
					name:      "opencode",
					bootstrap: filepath.Join(openCodeRoot, "ha-nova", "session-bootstrap.md"),
					context:   filepath.Join(openCodeRoot, "ha-nova", "SKILL.md"),
					read:      filepath.Join(openCodeRoot, "read", "SKILL.md"),
					readName:  "read",
				},
				{
					name:      "antigravity",
					bootstrap: filepath.Join(antigravityRoot, "ha-nova", "session-bootstrap.md"),
					context:   filepath.Join(antigravityRoot, "ha-nova", "SKILL.md"),
					read:      filepath.Join(antigravityRoot, "ha-nova-read", "SKILL.md"),
					readName:  "ha-nova-read",
				},
				{
					name:      "hermes",
					bootstrap: filepath.Join(hermesRoot, "ha-nova", "session-bootstrap.md"),
					context:   filepath.Join(hermesRoot, "ha-nova", "SKILL.md"),
					read:      filepath.Join(hermesRoot, "ha-nova-read", "SKILL.md"),
					readName:  "ha-nova-read",
				},
			}
			for _, layout := range layouts {
				if err := os.Remove(layout.bootstrap); err != nil {
					t.Fatalf("remove %s bootstrap from old copy: %v", layout.name, err)
				}
				if err := os.WriteFile(layout.context, []byte("name: ha-nova\n"), 0o644); err != nil {
					t.Fatalf("seed old %s context skill: %v", layout.name, err)
				}
				if err := os.WriteFile(layout.read, []byte("name: "+layout.readName+"\n"), 0o644); err != nil {
					t.Fatalf("seed old %s read skill: %v", layout.name, err)
				}
			}

			previousRuntimeProbe := clientRuntimeDetectedForStatus
			clientRuntimeDetectedForStatus = func(string) bool { return false }
			t.Cleanup(func() { clientRuntimeDetectedForStatus = previousRuntimeProbe })
			t.Setenv("HA_NOVA_NO_CENSUS", "1")
			if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
				Version: "0.6.1",
				HTMLURL: "https://example.invalid/releases/v0.6.1",
			}, 0o644); err != nil {
				t.Fatalf("write current update cache: %v", err)
			}
			if tc.stateMode == "current" {
				if err := saveState(paths, installState{
					SchemaVersion:          stateSchemaVersion,
					Version:                "0.6.1",
					ClientsVerifiedVersion: "0.6.1",
				}); err != nil {
					t.Fatalf("save misleading current marker: %v", err)
				}
			}

			relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/health":
					fmt.Fprint(w, tc.wantJSON)
				case "/core":
					fmt.Fprint(w, tc.wantJSON)
				default:
					http.NotFound(w, r)
				}
			}))
			defer relay.Close()
			if err := saveConfig(paths, runtimeConfig{RelayBaseURL: relay.URL}); err != nil {
				t.Fatalf("save relay config: %v", err)
			}
			t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
			t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(paths.Home, ".config", "ha-nova", ".test-relay-auth-token"))
			if err := writeRelayAuthToken("test-relay-token"); err != nil {
				t.Fatalf("write relay token: %v", err)
			}

			for call := 1; call <= 2; call++ {
				exitCode, output := captureCommandOutput(t, func() int {
					return runRelayCommand(paths, tc.args)
				})
				if exitCode != 0 {
					t.Fatalf("relay call %d failed:\n%s", call, output)
				}
				if strings.TrimSpace(output) != tc.wantJSON {
					t.Fatalf("relay call %d output is not pure JSON:\ngot:  %s\nwant: %s", call, output, tc.wantJSON)
				}
				for _, layout := range layouts {
					if !fileExists(layout.bootstrap) {
						t.Fatalf("relay call %d did not restore %s session-bootstrap.md", call, layout.name)
					}
					for _, skillPath := range []string{layout.context, layout.read} {
						if !markdownContains(skillPath, sessionBootstrapPointer) {
							t.Fatalf("relay call %d did not restore pointer in %s", call, skillPath)
						}
					}
				}
			}
			if tc.stateMode == "missing" {
				if _, err := os.Stat(paths.StateFile); !os.IsNotExist(err) {
					t.Fatalf("stateless migration must not create state: %v", err)
				}
			} else {
				state, err := loadState(paths)
				if err != nil {
					t.Fatalf("load unchanged state: %v", err)
				}
				if state.ClientsVerifiedVersion != "0.6.1" {
					t.Fatalf("migration changed unrelated state: %+v", state)
				}
			}
			marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
			if !markerHasVersion(marker, "0.6.1") {
				t.Fatal("successful proxy task must finalize the carried transition")
			}
		})
	}
}

func TestSessionBootstrapRepairRefusesForeignRegularDirectory(t *testing.T) {
	paths := setupHealableInstall(t)
	foreignRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	writeBundleTestFile(t, filepath.Join(foreignRoot, "ha-nova", "SKILL.md"), "name: unrelated\n", 0o644)
	writeBundleTestFile(t, filepath.Join(foreignRoot, "private.txt"), "keep me\n", 0o600)

	repairMissingSessionBootstrap(paths)

	content, err := os.ReadFile(filepath.Join(foreignRoot, "private.txt"))
	if err != nil || string(content) != "keep me\n" {
		t.Fatalf("foreign directory was modified: content=%q err=%v", content, err)
	}
	if fileExists(filepath.Join(foreignRoot, "ha-nova", "session-bootstrap.md")) {
		t.Fatal("foreign directory must not be classified as an old HA NOVA copy")
	}
}

func TestSessionBootstrapRepairResumesPersistedPlanAfterFingerprintLoss(t *testing.T) {
	paths := setupHealableInstall(t)
	pendingPath := filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile)
	if err := writeJSONFile(pendingPath, sessionBootstrapRepairPending{
		Version: "0.6.0",
		Clients: []string{"codex"},
	}, 0o600); err != nil {
		t.Fatalf("write pending repair: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker),
		[]byte("0.6.1\n"),
		0o644,
	); err != nil {
		t.Fatalf("write misleading current marker: %v", err)
	}

	if !repairMissingSessionBootstrap(paths) {
		t.Fatal("resumed repair must leave the first-use carrier pending")
	}
	context := filepath.Join(paths.Home, ".agents", "skills", "ha-nova", "ha-nova", "SKILL.md")
	if !markdownContains(context, sessionBootstrapPointer) {
		t.Fatalf("persisted plan did not rebuild the lost client tree: %s", context)
	}
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Fatalf("successful recovery must clear pending plan: %v", err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if !markerHasCarrierPending(marker, "0.6.1") {
		t.Fatal("recovered old copy must keep its first-use carrier pending")
	}
}

func TestSessionBootstrapRepairFailsClosedOnInvalidPendingPlan(t *testing.T) {
	paths := setupHealableInstall(t)
	pendingPath := filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile)
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	if err := os.WriteFile(pendingPath, []byte(`{"version":`), 0o600); err != nil {
		t.Fatalf("write invalid pending plan: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker),
		[]byte("0.6.1\n"),
		0o644,
	); err != nil {
		t.Fatalf("write misleading current marker: %v", err)
	}

	if repairMissingSessionBootstrap(paths) {
		t.Fatal("malformed recovery plan must fail closed")
	}
	if fileExists(filepath.Join(paths.Home, ".agents", "skills", "ha-nova")) {
		t.Fatal("invalid recovery plan must not mutate a client tree")
	}
	if !fileExists(pendingPath) {
		t.Fatal("invalid recovery plan must remain for explicit recovery")
	}
}

func TestSessionBootstrapRepairRefusesForeignTreeCreatedAfterPlan(t *testing.T) {
	paths := setupHealableInstall(t)
	pendingPath := filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile)
	if err := writeJSONFile(pendingPath, sessionBootstrapRepairPending{
		Version: "0.6.1",
		Clients: []string{"codex"},
	}, 0o600); err != nil {
		t.Fatalf("write pending repair: %v", err)
	}
	foreignRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	writeBundleTestFile(t, filepath.Join(foreignRoot, "ha-nova", "SKILL.md"), "name: unrelated\n", 0o644)
	writeBundleTestFile(t, filepath.Join(foreignRoot, "private.txt"), "keep me\n", 0o600)

	if repairMissingSessionBootstrap(paths) {
		t.Fatal("persisted plan must not overwrite a newly foreign target")
	}
	content, err := os.ReadFile(filepath.Join(foreignRoot, "private.txt"))
	if err != nil || string(content) != "keep me\n" {
		t.Fatalf("foreign target changed: content=%q err=%v", content, err)
	}
	if !fileExists(pendingPath) {
		t.Fatal("refused plan must remain available for explicit recovery")
	}
}

func TestSessionBootstrapRepairMigratesOlderCarrierIntent(t *testing.T) {
	paths := setupHealableInstall(t)
	codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	if _, err := installTreeClient(filepath.Dir(codexRoot), filepath.Join(paths.InstallRoot, "skills"), false); err != nil {
		t.Fatalf("seed current Codex tree: %v", err)
	}
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if err := os.WriteFile(marker, []byte("0.6.0"+sessionBootstrapCarrierPendingSuffix+"\n"), 0o644); err != nil {
		t.Fatalf("write older carrier marker: %v", err)
	}

	if !repairMissingSessionBootstrap(paths) {
		t.Fatal("older carrier intent must survive a later binary update")
	}
	if !markerHasCarrierPending(marker, "0.6.1") {
		t.Fatal("older carrier intent was not migrated to the running version")
	}
}

func TestSessionBootstrapRepairKeepsPlanWhenCarrierMarkerCannotPersist(t *testing.T) {
	paths := setupHealableInstall(t)
	pendingPath := filepath.Join(paths.CacheDir, sessionBootstrapRepairPendingFile)
	if err := writeJSONFile(pendingPath, sessionBootstrapRepairPending{
		Version: "0.6.1",
		Clients: []string{"codex"},
	}, 0o600); err != nil {
		t.Fatalf("write pending repair: %v", err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if err := os.Mkdir(marker, 0o755); err != nil {
		t.Fatalf("make marker path unwritable as a file: %v", err)
	}

	if repairMissingSessionBootstrap(paths) {
		t.Fatal("repair without a persisted carrier marker must not report ready")
	}
	if !fileExists(pendingPath) {
		t.Fatal("recovery plan must survive carrier-marker persistence failure")
	}
	context := filepath.Join(paths.Home, ".agents", "skills", "ha-nova", "ha-nova", "SKILL.md")
	if !markdownContains(context, sessionBootstrapPointer) {
		t.Fatal("client rebuild should complete before the simulated marker failure")
	}
}
