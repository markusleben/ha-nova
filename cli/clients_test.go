package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudePluginUsesVersionedLocalMarketplaceByDefaultForInstalledBundle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	if !strings.Contains(log, "plugin validate ") {
		t.Fatalf("expected staged marketplace validation before registering the release snapshot, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add "+expectedRoot) {
		t.Fatalf("expected marketplace add to use the exact local release snapshot, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected plugin install command, got:\n%s", log)
	}
	if strings.Contains(log, "github.com/markusleben/ha-nova.git") {
		t.Fatalf("did not expect bundle install to use a GitHub source, got:\n%s", log)
	}
}

func TestInstallClaudePluginFailsWhenClaudeCLIMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	err = installClaudePlugin(paths, sourceRoot)
	if err == nil || !strings.Contains(err.Error(), "Claude CLI not found in PATH") {
		t.Fatalf("expected missing Claude CLI error, got %v", err)
	}
}

func TestInstallClaudePluginStagesInstalledBundleMarketplaceWhenLocalOverrideEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "1")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := os.WriteFile(filepath.Join(sourceRoot, "ha-nova"), []byte("binary"), 0o755); err != nil {
		t.Fatalf("write bundled binary fixture: %v", err)
	}
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	marketplaceRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	if !strings.Contains(log, "plugin validate "+marketplaceRoot) {
		t.Fatalf("expected staged marketplace validation before add, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add "+marketplaceRoot) {
		t.Fatalf("expected local override to use staged marketplace root, got:\n%s", log)
	}
	marketplaceData, err := os.ReadFile(filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatalf("read rewritten marketplace: %v", err)
	}
	marketplace := string(marketplaceData)
	if !strings.Contains(marketplace, `"source": "./ha-nova"`) {
		t.Fatalf("expected rewritten marketplace to use staged local source, got:\n%s", marketplace)
	}
	if strings.Contains(marketplace, "github.com/markusleben/ha-nova.git") {
		t.Fatalf("expected rewritten marketplace to stop pointing at GitHub:\n%s", marketplace)
	}
	if _, err := os.Stat(filepath.Join(marketplaceRoot, "ha-nova", "ha-nova")); !os.IsNotExist(err) {
		t.Fatalf("expected staged Claude plugin payload to exclude bundled ha-nova binary, got err=%v", err)
	}
}

func TestInstallClaudePluginStagesDevMarketplaceOutsideRepoRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := filepath.Join(t.TempDir(), "repo")
	t.Setenv("HA_NOVA_DEV_ROOT", sourceRoot)
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	marketplaceRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(logData), "plugin marketplace add "+marketplaceRoot) {
		t.Fatalf("expected dev install to add staged marketplace root, got:\n%s", string(logData))
	}
	if !strings.Contains(string(logData), "plugin validate "+marketplaceRoot) {
		t.Fatalf("expected dev install to validate staged marketplace root, got:\n%s", string(logData))
	}

	marketplaceData, err := os.ReadFile(filepath.Join(marketplaceRoot, ".claude-plugin", "marketplace.json"))
	if err != nil {
		t.Fatalf("read staged marketplace: %v", err)
	}
	marketplace := string(marketplaceData)
	if !strings.Contains(marketplace, `"source": "./ha-nova"`) {
		t.Fatalf("expected staged marketplace to use staged local source, got:\n%s", marketplace)
	}

	info, err := os.Lstat(filepath.Join(marketplaceRoot, "ha-nova"))
	if err != nil {
		t.Fatalf("expected staged local plugin root: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 && !info.IsDir() {
		t.Fatalf("expected staged local plugin root to be directory or symlink, got mode %v", info.Mode())
	}
}

func TestInstallClaudePluginUpdatesExistingPlugin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	if !strings.Contains(log, "plugin marketplace add "+expectedRoot) {
		t.Fatalf("expected local release marketplace registration for existing plugin, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin remove ha-nova@ha-nova") {
		t.Fatalf("expected local release sync to reset plugin first, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected local release sync to use a fresh install, got:\n%s", log)
	}
	if strings.Contains(log, "plugin update ha-nova@ha-nova") {
		t.Fatalf("did not expect local release sync to use plugin update, got:\n%s", log)
	}
}

func TestInstallClaudePluginFailsWhenMarketplaceDisappearsAfterInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_TEST_CLAUDE_DROP_MARKETPLACE_ON_INSTALL", "1")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)

	err = installClaudePlugin(paths, sourceRoot)
	if err == nil || !strings.Contains(err.Error(), "Claude marketplace ha-nova not found after sync") {
		t.Fatalf("expected marketplace verification failure, got %v", err)
	}
}

func TestInstallClaudePluginLocalModeReinstallsAndClearsCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "1")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	cacheRoot := filepath.Join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.1.12")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatalf("mkdir cache root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheRoot, "stale.txt"), []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale cache marker: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin remove ha-nova@ha-nova") {
		t.Fatalf("expected local mode to remove stale plugin first, got:\n%s", log)
	}
	if strings.Contains(log, "plugin update ha-nova@ha-nova") {
		t.Fatalf("did not expect local mode to use update, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected local mode to install fresh plugin, got:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(cacheRoot, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected stale cache marker to be removed, got err=%v", err)
	}
	if !claudePluginInstalled(home) {
		t.Fatal("expected Claude plugin to be reinstalled after local refresh")
	}
}

func TestInstallClaudePluginLocalModeClearsCurrentClaudeCacheRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "1")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		[]byte(`{"plugins":["ha-nova@ha-nova"]}`),
		0o644,
	); err != nil {
		t.Fatalf("write installed plugins: %v", err)
	}

	cacheRoot := filepath.Join(home, ".claude", "plugins", "cache", "ha-nova")
	if err := os.MkdirAll(filepath.Join(cacheRoot, "skills", "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir current cache root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cacheRoot, "ha-nova"), []byte("binary"), 0o644); err != nil {
		t.Fatalf("write current cache binary: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin remove ha-nova@ha-nova") {
		t.Fatalf("expected local mode to remove stale plugin first, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected local mode to install fresh plugin, got:\n%s", log)
	}
	if _, err := os.Stat(filepath.Join(cacheRoot, "skills")); !os.IsNotExist(err) {
		t.Fatalf("expected stale cache tree to be replaced, got err=%v", err)
	}
	info, err := os.Stat(filepath.Join(cacheRoot, "ha-nova"))
	if err != nil {
		t.Fatalf("expected refreshed Claude cache structure: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected refreshed cache entry to be a directory, got mode %v", info.Mode())
	}
}

func TestInstallClaudePluginLocalModeAcceptsBOMPrefixedInstalledPluginsRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "1")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"plugins":["ha-nova@ha-nova"]}`)...)
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"), data, 0o644); err != nil {
		t.Fatalf("write installed plugins: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin remove ha-nova@ha-nova") {
		t.Fatalf("expected local mode to remove stale plugin first, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected local mode to install fresh plugin, got:\n%s", log)
	}
}

func TestInstallClaudePluginLocalModeRemovesPluginBeforeDeletingCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "1")

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	cacheRoot := filepath.Join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.1.12")
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		t.Fatalf("mkdir cache root: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("HA_NOVA_TEST_CLAUDE_ASSERT_CACHE_PRESENT_BEFORE_REMOVE", cacheRoot)
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	if !claudePluginInstalled(home) {
		t.Fatal("expected Claude plugin to be reinstalled after local refresh")
	}
}

func TestInstallClaudePluginFallsBackToInstallWhenUpdateStateIsStale(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("HA_NOVA_TEST_CLAUDE_UPDATE_STALE", "1")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected stale update fallback to install fresh plugin, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add "+expectedRoot) {
		t.Fatalf("expected stale update fallback to keep the local release snapshot source, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin remove ha-nova@ha-nova") {
		t.Fatalf("expected stale update fallback to reset the plugin first, got:\n%s", log)
	}
}

func TestInstallClaudePluginFallbackInstallRestoresStateWhenMarketplaceDisappears(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("HA_NOVA_TEST_CLAUDE_UPDATE_STALE", "1")
	t.Setenv("HA_NOVA_TEST_CLAUDE_DROP_MARKETPLACE_ON_INSTALL", "1")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)

	err = installClaudePlugin(paths, sourceRoot)
	if err == nil || !strings.Contains(err.Error(), "Claude marketplace ha-nova not found after sync") {
		t.Fatalf("expected fallback install verification failure, got %v", err)
	}

	source, hasSource, readErr := readClaudeMarketplaceSource(home)
	if readErr != nil {
		t.Fatalf("readClaudeMarketplaceSource() error: %v", readErr)
	}
	if hasSource {
		t.Fatalf("expected rollback to restore pre-install empty marketplace state, got %+v", source)
	}
}

func TestInstallClaudePluginFailsWhenPluginVerificationFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	script := `#!/usr/bin/env bash
printf '%s\n' "$*" >> ` + shellQuote(logPath) + `
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	err = installClaudePlugin(paths, sourceRoot)
	if err == nil || !strings.Contains(err.Error(), "not found after sync") {
		t.Fatalf("expected plugin verification failure, got %v", err)
	}
}

func TestRemoveInstalledClientsSkipsMissingClaudePluginQuietly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := removeInstalledClients(paths, installState{InstalledClients: []string{"claude"}}); err != nil {
		t.Fatalf("removeInstalledClients() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected marketplace cleanup even when plugin is already absent, got:\n%s", log)
	}
	if strings.Contains(log, "plugin remove ha-nova@ha-nova") {
		t.Fatalf("did not expect plugin removal call when plugin record is absent, got:\n%s", log)
	}
}

func TestRemoveInstalledClientsFailsWhenClaudeRemovalFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	script := "#!/usr/bin/env bash\necho 'hard failure' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	err = removeInstalledClients(paths, installState{InstalledClients: []string{"claude"}})
	if err == nil || !strings.Contains(err.Error(), "claude plugin removal failed") {
		t.Fatalf("expected hard Claude removal failure, got %v", err)
	}
}

func TestRemoveInstalledClientsTreatsAlreadyMissingClaudePluginAsRemoved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	script := "#!/usr/bin/env bash\necho 'Plugin \"ha-nova@ha-nova\" not found in installed plugins' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := removeInstalledClients(paths, installState{InstalledClients: []string{"claude"}}); err != nil {
		t.Fatalf("expected already-missing plugin to be treated as removed, got %v", err)
	}
}

func TestRemoveInstalledClientsSkipsStaleClaudePluginStateWhenCLIIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), []byte(`{"ha-nova":{"source":"`+filepath.Join(home, ".config", "ha-nova", "claude-marketplace")+`"}}`), 0o644); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	t.Setenv("PATH", t.TempDir())
	if err := removeInstalledClients(paths, installState{InstalledClients: []string{"claude"}}); err != nil {
		t.Fatalf("expected uninstall to ignore stale Claude plugin state when Claude CLI is missing, got %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		t.Fatalf("read installed plugins after cleanup: %v", err)
	}
	if strings.Contains(string(data), "ha-nova@ha-nova") {
		t.Fatalf("expected stale Claude plugin record to be removed, got %s", string(data))
	}
	marketplaces, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"))
	if err != nil {
		t.Fatalf("read known marketplaces after cleanup: %v", err)
	}
	if strings.Contains(string(marketplaces), "ha-nova") {
		t.Fatalf("expected stale Claude marketplace record to be removed, got %s", string(marketplaces))
	}
}

func TestRemoveInstalledClientsRemovesBrokenClaudeRecordWhenPluginInstallPathIsGone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)
	if err := os.RemoveAll(filepath.Join(home, ".claude", "plugins", "cache", "ha-nova")); err != nil {
		t.Fatalf("remove install path: %v", err)
	}
	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMock(t, home, logPath)+string(os.PathListSeparator)+os.Getenv("PATH"))

	if err := removeInstalledClients(paths, installState{InstalledClients: []string{"claude"}}); err != nil {
		t.Fatalf("expected stale broken Claude plugin record to be cleaned, got %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if strings.Contains(string(logData), "plugin remove ha-nova@ha-nova") {
		t.Fatalf("did not expect plugin remove for already-broken Claude record, got:\n%s", string(logData))
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		t.Fatalf("read installed plugins after cleanup: %v", err)
	}
	if strings.Contains(string(data), "ha-nova@ha-nova") {
		t.Fatalf("expected broken Claude plugin record to be removed, got %s", string(data))
	}
}

func TestRemoveInstalledClientsRemovesHermesSkillTree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	hermesRoot := filepath.Join(home, ".hermes", "skills", "ha-nova")
	if err := os.MkdirAll(filepath.Join(hermesRoot, "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir Hermes context skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hermesRoot, "ha-nova", "SKILL.md"), []byte("name: ha-nova"), 0o644); err != nil {
		t.Fatalf("write Hermes context skill: %v", err)
	}

	if err := removeInstalledClients(paths, installState{InstalledClients: []string{"hermes"}}); err != nil {
		t.Fatalf("removeInstalledClients() error: %v", err)
	}
	if _, err := os.Stat(hermesRoot); !os.IsNotExist(err) {
		t.Fatalf("expected Hermes skill tree to be removed, got err=%v", err)
	}
}

func TestRemoveInstalledClientsRemovesHermesSkillTreeWhenStateMissesHermes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	hermesRoot := filepath.Join(home, ".hermes", "skills", "ha-nova")
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

	if err := removeInstalledClients(paths, installState{InstalledClients: []string{"codex"}}); err != nil {
		t.Fatalf("removeInstalledClients() error: %v", err)
	}
	if _, err := os.Stat(hermesRoot); !os.IsNotExist(err) {
		t.Fatalf("expected legacy Hermes skill tree to be removed, got err=%v", err)
	}
}

func installClaudeMock(t *testing.T, home, logPath string) string {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	script := `#!/usr/bin/env bash
set -euo pipefail
cmd="$*"
printf '%s\n' "$cmd" >> ` + shellQuote(logPath) + `
plugins_root="` + filepath.Join(home, ".claude", "plugins") + `"
known_file="${plugins_root}/known_marketplaces.json"
installed_file="${plugins_root}/installed_plugins.json"
cache_root="${plugins_root}/cache/ha-nova/ha-nova/0.1.12"
mkdir -p "${plugins_root}"

write_installed() {
  mkdir -p "${cache_root}"
  cat > "${installed_file}" <<JSON
{
  "version": 2,
  "plugins": {
    "ha-nova@ha-nova": [
      {
        "scope": "user",
        "installPath": "${cache_root}",
        "version": "0.1.12"
      }
    ]
  }
}
JSON
}

remove_installed() {
  rm -f "${installed_file}"
  rm -rf "${plugins_root}/cache/ha-nova"
}

case "$cmd" in
  "plugin validate "*)
    if [[ "${HA_NOVA_TEST_CLAUDE_VALIDATE_FAIL:-0}" == "1" ]]; then
      echo "validate failed" >&2
      exit 1
    fi
    ;;
  "plugin marketplace add "*)
    source_value="${cmd#plugin marketplace add }"
    cat > "${known_file}" <<JSON
{"ha-nova":{"source":"${source_value}"}}
JSON
    ;;
  "plugin marketplace update ha-nova")
    if [[ ! -f "${known_file}" ]]; then
      echo 'marketplace "ha-nova" not found' >&2
      exit 1
    fi
    ;;
  "plugin marketplace remove ha-nova")
    rm -f "${known_file}"
    ;;
  "plugin install ha-nova@ha-nova")
    if [[ "${HA_NOVA_TEST_CLAUDE_INSTALL_FAIL:-0}" == "1" ]]; then
      echo "install failed" >&2
      exit 1
    fi
    write_installed
    if [[ "${HA_NOVA_TEST_CLAUDE_DROP_MARKETPLACE_ON_INSTALL:-0}" == "1" ]]; then
      rm -f "${known_file}"
    fi
    ;;
 "plugin update ha-nova@ha-nova")
    if [[ "${HA_NOVA_TEST_CLAUDE_UPDATE_STALE:-0}" == "1" ]]; then
      echo 'Plugin "ha-nova@ha-nova" not found in installed plugins' >&2
      exit 1
    fi
    if [[ "${HA_NOVA_TEST_CLAUDE_UPDATE_FAIL:-0}" == "1" ]]; then
      echo "update failed" >&2
      exit 1
    fi
    if [[ ! -f "${installed_file}" ]]; then
      echo 'Plugin "ha-nova@ha-nova" not found in installed plugins' >&2
      exit 1
    fi
    write_installed
    ;;
  "plugin remove ha-nova@ha-nova")
    if [[ ! -f "${installed_file}" ]]; then
      echo 'Plugin "ha-nova@ha-nova" not found in installed plugins' >&2
      exit 1
    fi
    if [[ -n "${HA_NOVA_TEST_CLAUDE_ASSERT_CACHE_PRESENT_BEFORE_REMOVE:-}" ]] && [[ ! -d "${HA_NOVA_TEST_CLAUDE_ASSERT_CACHE_PRESENT_BEFORE_REMOVE}" ]]; then
      echo 'cache missing before remove' >&2
      exit 1
    fi
    remove_installed
    ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	return binDir
}

func writeClaudeMarketplaceFixture(t *testing.T, sourceRoot string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(sourceRoot, "clients"), 0o755); err != nil {
		t.Fatalf("mkdir clients dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sourceRoot, "skills", "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(sourceRoot, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, publicBinaryName()), []byte("bundle"), 0o755); err != nil {
		t.Fatalf("write bundle binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "bundle.json"), []byte(`{"version":"0.1.12"}`), 0o644); err != nil {
		t.Fatalf("write bundle.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "clients", "registry.json"), []byte(`{"clients":[{"id":"claude","label":"Claude Code","adapter_kind":"plugin_marketplace","supported_os":["macos","linux","windows"]}]}`), 0o644); err != nil {
		t.Fatalf("write clients registry: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "skills", "ha-nova", "SKILL.md"), []byte("name: ha-nova"), 0o644); err != nil {
		t.Fatalf("write skill fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, ".claude-plugin", "plugin.json"), []byte(`{
  "name":"ha-nova",
  "version":"0.1.12"
}`), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, ".claude-plugin", "marketplace.json"), []byte(`{
  "name":"ha-nova",
  "plugins":[
    {
      "name":"ha-nova",
      "source":"./",
      "version":"0.1.12"
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write marketplace.json: %v", err)
	}
}

func writeInstalledClaudePluginFixture(t *testing.T, home string) {
	t.Helper()

	installPath := filepath.Join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.1.12")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir install path: %v", err)
	}
	path := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	content := fmt.Sprintf(`{
  "version": 2,
  "plugins": {
    "ha-nova@ha-nova": [
      {
        "scope": "user",
        "installPath": %q,
        "version": "0.1.12"
      }
    ]
  }
}`, installPath)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write installed plugins: %v", err)
	}
}

func writeClaudeMarketplaceRegistrationFixture(t *testing.T, home, source string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins path: %v", err)
	}
	path := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	content := fmt.Sprintf(`{
  "ha-nova": {
    "source": {
      "source": "directory",
      "path": %q
    }
  }
}`, source)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func TestClientUnsupportedOSReasonRoutesHermesOnWindowsToWSL2(t *testing.T) {
	got := clientUnsupportedOSReason("hermes", "windows")
	if !strings.Contains(got, "WSL2") {
		t.Fatalf("expected WSL2 routing hint for hermes on windows, got %q", got)
	}

	if got := clientUnsupportedOSReason("opencode", "windows"); got != "not supported on windows" {
		t.Fatalf("expected generic unsupported message for opencode, got %q", got)
	}
}
