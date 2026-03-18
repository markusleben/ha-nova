package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudePluginUsesRemoteMarketplaceByDefaultForInstalledBundle(t *testing.T) {
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
	if !strings.Contains(log, "plugin marketplace add https://github.com/markusleben/ha-nova") {
		t.Fatalf("expected marketplace add to use GitHub marketplace by default, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected plugin install command, got:\n%s", log)
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
	if !strings.Contains(log, "plugin update ha-nova@ha-nova") {
		t.Fatalf("expected update command for existing plugin, got:\n%s", log)
	}
	if strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("did not expect install command for existing plugin, got:\n%s", log)
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
	if _, err := os.Stat(cacheRoot); !os.IsNotExist(err) {
		t.Fatalf("expected stale local cache root to be removed, got err=%v", err)
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
	if _, err := os.Stat(cacheRoot); !os.IsNotExist(err) {
		t.Fatalf("expected current Claude cache root to be removed, got err=%v", err)
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
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	script := `#!/usr/bin/env bash
printf '%s\n' "$*" >> ` + shellQuote(logPath) + `
if [[ "$*" == "plugin remove ha-nova@ha-nova" ]]; then
  if [[ ! -d ` + shellQuote(cacheRoot) + ` ]]; then
    echo 'cache missing before remove' >&2
    exit 1
  fi
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceRoot := paths.InstallRoot
	writeClaudeMarketplaceFixture(t, sourceRoot)
	if err := installClaudePlugin(paths, sourceRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	if _, err := os.Stat(cacheRoot); !os.IsNotExist(err) {
		t.Fatalf("expected cache root removed after local refresh, got err=%v", err)
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
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	script := `#!/usr/bin/env bash
printf '%s\n' "$*" >> ` + shellQuote(logPath) + `
if [[ "$*" == "plugin update ha-nova@ha-nova" ]]; then
  echo 'Plugin "ha-nova@ha-nova" not found in installed plugins' >&2
  exit 1
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

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
	if !strings.Contains(log, "plugin update ha-nova@ha-nova") {
		t.Fatalf("expected stale-state update attempt, got:\n%s", log)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected fallback install after stale update failure, got:\n%s", log)
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

func installClaudeMock(t *testing.T, home, logPath string) string {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	script := "#!/usr/bin/env bash\nprintf '%s\\n' \"$*\" >> " + shellQuote(logPath) + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	return binDir
}

func writeClaudeMarketplaceFixture(t *testing.T, sourceRoot string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(sourceRoot, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
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

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
