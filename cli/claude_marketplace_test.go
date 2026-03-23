package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudePluginRefreshesMatchingMarketplaceWhenGitHubMarketplaceAlreadyConfigured(t *testing.T) {
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
	if !strings.Contains(log, "plugin marketplace update ha-nova") {
		t.Fatalf("expected marketplace update when GitHub marketplace is already configured:\n%s", log)
	}
	if strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("did not expect marketplace removal when GitHub marketplace is already configured:\n%s", log)
	}
	if strings.Contains(log, "plugin marketplace add https://github.com/markusleben/ha-nova") {
		t.Fatalf("did not expect marketplace re-add when GitHub marketplace is already configured:\n%s", log)
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
	if !strings.Contains(string(logData), "plugin marketplace update ha-nova") {
		t.Fatalf("expected marketplace update for BOM-prefixed registry, got:\n%s", string(logData))
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
	if !strings.Contains(log, "plugin marketplace update ha-nova") {
		t.Fatalf("expected marketplace update when structured GitHub marketplace is already configured:\n%s", log)
	}
	if strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("did not expect marketplace removal for structured GitHub marketplace:\n%s", log)
	}
	if strings.Contains(log, "plugin marketplace add https://github.com/markusleben/ha-nova") {
		t.Fatalf("did not expect marketplace re-add for structured GitHub marketplace:\n%s", log)
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
	if !strings.Contains(log, "plugin marketplace add https://github.com/markusleben/ha-nova") {
		t.Fatalf("expected pinned marketplace to be replaced with default GitHub source:\n%s", log)
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
	failCommand := "plugin marketplace add https://github.com/markusleben/ha-nova"
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when floating GitHub marketplace add fails")
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
	failCommand := "plugin marketplace add https://github.com/markusleben/ha-nova"
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when floating GitHub marketplace add fails")
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

func TestInstallClaudePluginReplacesStaleMarketplaceWithGitHub(t *testing.T) {
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
		t.Fatalf("expected stale marketplace removal before GitHub re-add:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add https://github.com/markusleben/ha-nova") {
		t.Fatalf("expected GitHub marketplace re-add after stale marketplace removal:\n%s", log)
	}
}

func TestInstallClaudePluginRestoresExistingMarketplaceWhenGitHubReplaceFails(t *testing.T) {
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
	failCommand := "plugin marketplace add https://github.com/markusleben/ha-nova"
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when GitHub marketplace add fails")
	}

	logData, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read log: %v", readErr)
	}
	log := string(logData)
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected stale marketplace removal before a failed GitHub re-add:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add /tmp/old-ha-nova-marketplace") {
		t.Fatalf("expected stale marketplace restore after GitHub add failure:\n%s", log)
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
