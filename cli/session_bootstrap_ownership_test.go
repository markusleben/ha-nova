package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRepairPlanRefusesNonTreeSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated Windows privileges")
	}
	for _, client := range []string{"hermes", "antigravity"} {
		t.Run(client, func(t *testing.T) {
			paths := setupHealableInstall(t)
			target := filepath.Join(paths.Home, ".hermes", "skills", "ha-nova")
			if client == "antigravity" {
				target = filepath.Join(antigravitySkillsRoot(paths.Home), "ha-nova")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatalf("mkdir target parent: %v", err)
			}
			if err := os.Symlink(filepath.Join(paths.Home, "missing"), target); err != nil {
				t.Fatalf("seed broken symlink: %v", err)
			}
			if err := repairPlanTargetsSafe(paths, paths.InstallRoot, []string{client}); err == nil {
				t.Fatalf("%s symlink target must be refused", client)
			}
			info, err := os.Lstat(target)
			if err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("refused target changed: info=%v err=%v", info, err)
			}
		})
	}
}

func TestRepairPlanRefusesForeignAndBrokenTreeSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated Windows privileges")
	}
	for _, client := range []string{"codex", "opencode"} {
		for _, targetKind := range []string{"foreign", "broken"} {
			t.Run(client+"_"+targetKind, func(t *testing.T) {
				paths := setupHealableInstall(t)
				root := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
				if client == "opencode" {
					root = filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova")
				}
				target := filepath.Join(paths.Home, "missing-"+client)
				if targetKind == "foreign" {
					target = filepath.Join(paths.Home, "foreign-"+client)
					writeBundleTestFile(t, filepath.Join(target, "ha-nova", "SKILL.md"), "name: unrelated\n", 0o600)
				}
				if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
					t.Fatalf("mkdir parent: %v", err)
				}
				if err := os.Symlink(target, root); err != nil {
					t.Fatalf("seed symlink: %v", err)
				}
				if err := repairPlanTargetsSafe(paths, paths.InstallRoot, []string{client}); err == nil {
					t.Fatal("foreign or broken tree symlink must be refused")
				}
				info, err := os.Lstat(root)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("refused tree symlink changed: info=%v err=%v", info, err)
				}
			})
		}
	}
}

func TestRepairPlanAcceptsOnlyExactTransientInstallBackupSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated Windows privileges")
	}
	for _, tc := range []struct {
		name string
		root func(runtimePaths) string
		ok   bool
	}{
		{
			name: "recognized sibling backup",
			root: func(paths runtimePaths) string {
				return filepath.Join(filepath.Dir(paths.InstallRoot), installBackupPrefixOld+"123", "skills")
			},
			ok: true,
		},
		{
			name: "foreign path containing backup prefix",
			root: func(paths runtimePaths) string {
				return filepath.Join(paths.Home, "private", installBackupPrefixOld+"123", "skills")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupHealableInstall(t)
			link := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatalf("mkdir link parent: %v", err)
			}
			if err := os.Symlink(tc.root(paths), link); err != nil {
				t.Fatalf("seed backup link: %v", err)
			}
			err := repairPlanTargetsSafe(paths, paths.InstallRoot, []string{"codex"})
			if (err == nil) != tc.ok {
				t.Fatalf("safe=%v, want %v (err=%v)", err == nil, tc.ok, err)
			}
		})
	}
}

func TestSessionBootstrapRepairReplacesExactDanglingUpdateBackupSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires elevated Windows privileges")
	}
	for _, client := range []string{"codex", "opencode"} {
		t.Run(client, func(t *testing.T) {
			paths := setupHealableInstall(t)
			root := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
			if client == "opencode" {
				root = filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova")
			}
			dangling := filepath.Join(filepath.Dir(paths.InstallRoot), installBackupPrefixOld+"123", "skills")
			if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
				t.Fatalf("mkdir client root: %v", err)
			}
			if err := os.Symlink(dangling, root); err != nil {
				t.Fatalf("seed dangling update-backup symlink: %v", err)
			}

			if !repairMissingSessionBootstrap(paths) {
				t.Fatal("exact dangling update-backup link was not repaired")
			}
			if !fileExists(filepath.Join(root, "ha-nova", "session-bootstrap.md")) {
				t.Fatal("actual repair did not install the session bootstrap")
			}
			target, err := os.Readlink(root)
			if err != nil {
				t.Fatalf("repaired tree is not a symlink: %v", err)
			}
			if filepath.Clean(target) != filepath.Join(paths.InstallRoot, "skills") {
				t.Fatalf("repaired symlink target = %q", target)
			}
		})
	}
}

func TestAntigravityInstallPreservesForeignRetiredSkill(t *testing.T) {
	paths := setupHealableInstall(t)
	retired := filepath.Join(antigravitySkillsRoot(paths.Home), "ha-nova-guide")
	writeBundleTestFile(t, filepath.Join(retired, "SKILL.md"), "name: unrelated\n", 0o644)
	writeBundleTestFile(t, filepath.Join(retired, "private.txt"), "keep\n", 0o600)

	if err := installAntigravityClient(paths.Home, paths.InstallRoot); err != nil {
		t.Fatalf("install Antigravity: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(retired, "private.txt"))
	if err != nil || string(content) != "keep\n" {
		t.Fatalf("foreign retired skill changed: content=%q err=%v", content, err)
	}
}

func TestPostUpdateSyncRefusesForeignAntigravitySibling(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := installAntigravityClient(paths.Home, paths.InstallRoot); err != nil {
		t.Fatalf("seed Antigravity: %v", err)
	}
	retired := filepath.Join(antigravitySkillsRoot(paths.Home), "ha-nova-guide")
	writeBundleTestFile(t, filepath.Join(retired, "SKILL.md"), "name: unrelated\n", 0o644)
	writeBundleTestFile(t, filepath.Join(retired, "private.txt"), "keep\n", 0o600)
	if err := saveState(paths, installState{
		SchemaVersion:    stateSchemaVersion,
		Version:          "0.6.0",
		InstalledClients: []string{"antigravity"},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	err := postUpdateSync(paths)
	if err == nil || !strings.Contains(err.Error(), "failed clients: antigravity") {
		t.Fatalf("foreign sibling must fail client sync, got %v", err)
	}
	if content, readErr := os.ReadFile(filepath.Join(retired, "private.txt")); readErr != nil || string(content) != "keep\n" {
		t.Fatalf("foreign sibling changed: content=%q err=%v", content, readErr)
	}
}

func TestSessionBootstrapRepairPreservesLegacyAntigravityRoots(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := installAntigravityClient(paths.Home, paths.InstallRoot); err != nil {
		t.Fatalf("seed Antigravity: %v", err)
	}
	bootstrap := filepath.Join(antigravitySkillsRoot(paths.Home), "ha-nova", "session-bootstrap.md")
	if err := os.Remove(bootstrap); err != nil {
		t.Fatalf("remove bootstrap: %v", err)
	}
	sentinels := []string{
		filepath.Join(legacyGeminiSkillsRoot(paths.Home), "ha-nova-read", "private.txt"),
		filepath.Join(legacyCodexGeminiSkillsRoot(paths.Home), "ha-nova-read", "private.txt"),
	}
	for _, sentinel := range sentinels {
		writeBundleTestFile(t, sentinel, "keep\n", 0o600)
	}

	if !repairMissingSessionBootstrap(paths) {
		t.Fatal("old Antigravity copy should produce a pending first-use carrier")
	}
	for _, sentinel := range sentinels {
		if content, err := os.ReadFile(sentinel); err != nil || string(content) != "keep\n" {
			t.Fatalf("legacy sentinel changed at %s: content=%q err=%v", sentinel, content, err)
		}
	}
}

func TestUnreadableHermesLegacyContextDoesNotBlockOpenCodeRepair(t *testing.T) {
	paths := setupHealableInstall(t)
	openCodeRoot := filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova")
	if _, err := installTreeClient(filepath.Dir(openCodeRoot), filepath.Join(paths.InstallRoot, "skills"), false); err != nil {
		t.Fatalf("seed OpenCode tree: %v", err)
	}
	if err := os.Remove(filepath.Join(openCodeRoot, "ha-nova", "session-bootstrap.md")); err != nil {
		t.Fatalf("remove OpenCode bootstrap: %v", err)
	}
	if err := installHermesClient(paths.Home, paths.InstallRoot); err != nil {
		t.Fatalf("seed namespaced Hermes tree: %v", err)
	}
	legacyContext := filepath.Join(paths.Home, ".hermes", "skills", "ha-nova", "SKILL.md")
	if err := os.Mkdir(legacyContext, 0o700); err != nil {
		t.Fatalf("seed unreadable Hermes legacy context: %v", err)
	}

	if !repairMissingSessionBootstrap(paths) {
		t.Fatal("unrelated Hermes legacy context blocked OpenCode repair")
	}
	if !fileExists(filepath.Join(openCodeRoot, "ha-nova", "session-bootstrap.md")) {
		t.Fatal("OpenCode session bootstrap was not restored")
	}
	info, err := os.Stat(legacyContext)
	if err != nil || !info.IsDir() {
		t.Fatalf("Hermes legacy context changed: info=%v err=%v", info, err)
	}
}

func TestAutoRepairRefusesForeignFileClientTarget(t *testing.T) {
	paths := setupHealableInstall(t)
	foreignRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	writeBundleTestFile(t, filepath.Join(foreignRoot, "ha-nova", "SKILL.md"), "name: unrelated\n", 0o644)
	writeBundleTestFile(t, filepath.Join(foreignRoot, "private.txt"), "keep\n", 0o600)

	outcome := attemptClientAutoRepair(paths, clientStatus{
		ID: "codex", Label: "Codex", RuntimeDetected: true,
	})
	if outcome.Err == nil || outcome.Repaired {
		t.Fatalf("foreign auto-repair target must be refused: %+v", outcome)
	}
	if content, err := os.ReadFile(filepath.Join(foreignRoot, "private.txt")); err != nil || string(content) != "keep\n" {
		t.Fatalf("foreign auto-repair target changed: content=%q err=%v", content, err)
	}
}

func TestAntigravityAutoRepairPreservesLegacyRoots(t *testing.T) {
	paths := setupHealableInstall(t)
	sentinels := []string{
		filepath.Join(legacyGeminiSkillsRoot(paths.Home), "ha-nova-read", "private.txt"),
		filepath.Join(legacyCodexGeminiSkillsRoot(paths.Home), "ha-nova-read", "private.txt"),
	}
	for _, sentinel := range sentinels {
		writeBundleTestFile(t, sentinel, "keep\n", 0o600)
	}

	outcome := attemptClientAutoRepair(paths, clientStatus{
		ID: "antigravity", Label: "Antigravity", RuntimeDetected: true,
	})
	if outcome.Err != nil || !outcome.Repaired {
		t.Fatalf("Antigravity auto-repair failed: %+v", outcome)
	}
	for _, sentinel := range sentinels {
		if content, err := os.ReadFile(sentinel); err != nil || string(content) != "keep\n" {
			t.Fatalf("legacy sentinel changed at %s: content=%q err=%v", sentinel, content, err)
		}
	}
}
