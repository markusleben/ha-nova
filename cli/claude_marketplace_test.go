package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudePluginMigratesGitHubMarketplaceToLocalStagedSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		[]byte(`{"ha-nova":{"source":"https://github.com/markusleben/ha-nova"}}`),
		0o644,
	); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	if err := installClaudePlugin(paths, paths.InstallRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	marketplaceRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	if !strings.Contains(log, "plugin validate "+marketplaceRoot) {
		t.Fatalf("expected staged marketplace validation before migration:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected legacy GitHub marketplace removal before local re-add:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add "+marketplaceRoot) {
		t.Fatalf("expected staged local marketplace re-add:\n%s", log)
	}
	if !strings.Contains(log, "plugin install ha-nova@ha-nova") {
		t.Fatalf("expected plugin install to continue, got:\n%s", log)
	}
}

func TestInstallClaudePluginAcceptsBOMPrefixedMarketplaceRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"ha-nova":{"source":"https://github.com/markusleben/ha-nova"}}`)...)
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), data, 0o644); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	if err := installClaudePlugin(paths, paths.InstallRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	marketplaceRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	if !strings.Contains(string(logData), "plugin marketplace add "+marketplaceRoot) {
		t.Fatalf("expected BOM-prefixed registry to migrate to staged local marketplace, got:\n%s", string(logData))
	}
}

func TestInstallClaudePluginAcceptsStructuredGitHubMarketplaceSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		[]byte(`{"ha-nova":{"source":{"source":"github","repo":"markusleben/ha-nova"}}}`),
		0o644,
	); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	if err := installClaudePlugin(paths, paths.InstallRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	marketplaceRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	if !strings.Contains(log, "plugin marketplace add "+marketplaceRoot) {
		t.Fatalf("expected structured GitHub marketplace to migrate to staged local source:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected structured GitHub marketplace removal before local add:\n%s", log)
	}
	if strings.Contains(log, "plugin marketplace add https://github.com/markusleben/ha-nova") {
		t.Fatalf("did not expect structured GitHub marketplace to remain on floating GitHub:\n%s", log)
	}
}

func TestInstallClaudePluginReplacesPinnedGitHubMarketplaceRef(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		[]byte(`{"ha-nova":{"source":{"source":"github","repo":"markusleben/ha-nova","ref":"v0.2.2"}}}`),
		0o644,
	); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	if err := installClaudePlugin(paths, paths.InstallRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected pinned marketplace removal before re-add:\n%s", log)
	}
	marketplaceRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	if !strings.Contains(log, "plugin marketplace add "+marketplaceRoot) {
		t.Fatalf("expected pinned marketplace to be replaced with staged local source:\n%s", log)
	}
	if strings.Contains(log, "plugin marketplace update ha-nova") {
		t.Fatalf("did not expect pinned marketplace to be treated as a matching source:\n%s", log)
	}
}

func TestInstallClaudePluginRestoresPinnedGitHubMarketplaceRefWhenReplaceFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		[]byte(`{"ha-nova":{"source":{"source":"github","repo":"markusleben/ha-nova","ref":"v0.2.2"}}}`),
		0o644,
	); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	failCommand := "plugin marketplace add " + filepath.Join(paths.ConfigDir, "claude-marketplace")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when staged local marketplace add fails")
	}

	logData, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read log: %v", readErr)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin marketplace add markusleben/ha-nova#v0.2.2") {
		t.Fatalf("expected rollback to restore the original pinned GitHub marketplace ref:\n%s", log)
	}
}

func TestInstallClaudePluginRestoresPinnedGitURLMarketplaceRefWhenReplaceFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		[]byte(`{"ha-nova":{"source":{"source":"git","url":"https://github.com/markusleben/ha-nova.git","ref":"v0.2.2"}}}`),
		0o644,
	); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	failCommand := "plugin marketplace add " + filepath.Join(paths.ConfigDir, "claude-marketplace")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when staged local marketplace add fails")
	}

	logData, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read log: %v", readErr)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin marketplace add https://github.com/markusleben/ha-nova.git#v0.2.2") {
		t.Fatalf("expected rollback to restore the original pinned Git URL marketplace ref:\n%s", log)
	}
}

func TestSameClaudeMarketplaceSourceTreatsPinnedGitHubRefsAsDistinct(t *testing.T) {
	current := claudeMarketplaceSourceFromRaw(map[string]any{
		"source": "github",
		"repo":   "markusleben/ha-nova",
		"ref":    "v0.2.2",
	})

	if sameClaudeMarketplaceSource(current, "https://github.com/markusleben/ha-nova") {
		t.Fatal("expected pinned GitHub source to differ from the default floating GitHub source")
	}
}

func TestInstallClaudePluginReplacesStaleMarketplaceWithLocalStagedSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		[]byte(`{"ha-nova":{"source":"/tmp/old-ha-nova-marketplace"}}`),
		0o644,
	); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	if err := installClaudePlugin(paths, paths.InstallRoot); err != nil {
		t.Fatalf("installClaudePlugin() error: %v", err)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected stale marketplace removal before local re-add:\n%s", log)
	}
	marketplaceRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	if !strings.Contains(log, "plugin marketplace add "+marketplaceRoot) {
		t.Fatalf("expected staged local marketplace re-add after stale marketplace removal:\n%s", log)
	}
}

func TestInstallClaudePluginRestoresExistingMarketplaceWhenLocalReplaceFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		[]byte(`{"ha-nova":{"source":"/tmp/old-ha-nova-marketplace"}}`),
		0o644,
	); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	failCommand := "plugin marketplace add " + filepath.Join(paths.ConfigDir, "claude-marketplace")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when local marketplace add fails")
	}

	logData, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read log: %v", readErr)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected stale marketplace removal before a failed local re-add:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add /tmp/old-ha-nova-marketplace") {
		t.Fatalf("expected stale marketplace restore after local add failure:\n%s", log)
	}
}

func TestInstallClaudePluginKeepsExistingLocalMarketplaceWhenValidationFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	existingRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	if err := os.MkdirAll(filepath.Join(existingRoot, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir existing root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(existingRoot, ".claude-plugin", "marketplace.json"), []byte(`{"plugins":[{"name":"ha-nova","source":"./ha-nova"}]}`), 0o644); err != nil {
		t.Fatalf("write existing manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(existingRoot, "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir existing plugin payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(existingRoot, "ha-nova", "marker.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("write existing marker: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("HA_NOVA_TEST_CLAUDE_VALIDATE_FAIL", "1")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when staged marketplace validation fails")
	}

	marker, readErr := os.ReadFile(filepath.Join(existingRoot, "ha-nova", "marker.txt"))
	if readErr != nil {
		t.Fatalf("expected previous staged marketplace to remain intact: %v", readErr)
	}
	if string(marker) != "keep" {
		t.Fatalf("existing staged marketplace marker changed unexpectedly: %q", string(marker))
	}
}

func TestInstallClaudePluginRestoresExistingLocalMarketplaceWhenInstallFailsAfterCutover(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	existingRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	if err := os.MkdirAll(filepath.Join(existingRoot, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir existing root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(existingRoot, ".claude-plugin", "marketplace.json"), []byte(`{"plugins":[{"name":"ha-nova","source":"./ha-nova"}]}`), 0o644); err != nil {
		t.Fatalf("write existing manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(existingRoot, "ha-nova"), 0o755); err != nil {
		t.Fatalf("mkdir existing plugin payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(existingRoot, "ha-nova", "marker.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatalf("write existing marker: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), []byte(fmt.Sprintf(`{"ha-nova":{"source":{"source":"directory","path":%q}}}`, existingRoot)), 0o644); err != nil {
		t.Fatalf("write known marketplaces: %v", err)
	}
	writeInstalledClaudePluginFixture(t, home)

	logPath := filepath.Join(home, "claude.log")
	failCommand := "plugin install ha-nova@ha-nova"
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when plugin install fails after marketplace cutover")
	}

	marker, readErr := os.ReadFile(filepath.Join(existingRoot, "ha-nova", "marker.txt"))
	if readErr != nil {
		t.Fatalf("expected previous staged marketplace to be restored after install failure: %v", readErr)
	}
	if string(marker) != "keep" {
		t.Fatalf("expected previous staged marketplace marker after install failure, got %q", string(marker))
	}
}

func TestInstallClaudePluginClearsMarketplaceRegistrationWhenFreshInstallFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	failCommand := "plugin install ha-nova@ha-nova"
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when fresh plugin install fails")
	}

	knownPath := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	if _, statErr := os.Stat(knownPath); statErr == nil {
		data, readErr := os.ReadFile(knownPath)
		if readErr != nil {
			t.Fatalf("read known marketplaces: %v", readErr)
		}
		if strings.Contains(string(data), "ha-nova") {
			t.Fatalf("expected fresh install rollback to clear marketplace registration, got:\n%s", string(data))
		}
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("stat known marketplaces: %v", statErr)
	}
}

func installClaudeMarketplaceMock(t *testing.T, logPath, failCommand string) string {
	t.Helper()

	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}

	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"cmd=\"$*\"\n" +
		"plugins_root=\"${HOME}/.claude/plugins\"\n" +
		"known_file=\"${plugins_root}/known_marketplaces.json\"\n" +
		"installed_file=\"${plugins_root}/installed_plugins.json\"\n" +
		"cache_root=\"${plugins_root}/cache/ha-nova/ha-nova/0.1.12\"\n" +
		"mkdir -p \"${plugins_root}\"\n" +
		"printf '%s\\n' \"$cmd\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"$cmd\" = " + shellQuote(failCommand) + " ]; then\n" +
		"  echo 'simulated marketplace add failure' >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"case \"$cmd\" in\n" +
		"  \"plugin validate \"*)\n" +
		"    if [[ \"${HA_NOVA_TEST_CLAUDE_VALIDATE_FAIL:-0}\" == \"1\" ]]; then\n" +
		"      echo 'simulated validate failure' >&2\n" +
		"      exit 1\n" +
		"    fi\n" +
		"    ;;\n" +
		"  \"plugin marketplace add \"*)\n" +
		"    source_value=\"${cmd#plugin marketplace add }\"\n" +
		"    printf '{\"ha-nova\":{\"source\":\"%s\"}}\\n' \"$source_value\" > \"$known_file\"\n" +
		"    ;;\n" +
		"  \"plugin marketplace remove ha-nova\")\n" +
		"    rm -f \"$known_file\"\n" +
		"    ;;\n" +
		"  \"plugin install ha-nova@ha-nova\"|\"plugin update ha-nova@ha-nova\")\n" +
		"    mkdir -p \"$cache_root\"\n" +
		"    cat > \"$installed_file\" <<JSON\n" +
		"{\"version\":2,\"plugins\":{\"ha-nova@ha-nova\":[{\"scope\":\"user\",\"installPath\":\"$cache_root\",\"version\":\"0.1.12\"}]}}\n" +
		"JSON\n" +
		"    ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("write claude mock: %v", err)
	}
	return binDir
}
