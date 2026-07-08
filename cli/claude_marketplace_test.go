package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudePluginMigratesFloatingGitHubMarketplaceToVersionedLocalSnapshot(t *testing.T) {
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
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected floating GitHub marketplace to be replaced with a local release snapshot:\n%s", log)
	}
	if !strings.Contains(log, "plugin validate ") {
		t.Fatalf("expected floating GitHub marketplace migration to validate a staged local release snapshot:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add "+expectedRoot) {
		t.Fatalf("expected local release snapshot after replacement:\n%s", log)
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
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	if !strings.Contains(string(logData), "plugin marketplace add "+expectedRoot) {
		t.Fatalf("expected BOM-prefixed registry to migrate to the local release snapshot, got:\n%s", string(logData))
	}
}

func TestInstallClaudePluginMigratesStructuredGitHubMarketplaceSourceToVersionedLocalSnapshot(t *testing.T) {
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
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected structured GitHub marketplace to be replaced with a local release snapshot:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add "+expectedRoot) {
		t.Fatalf("expected structured GitHub marketplace to migrate to the local release snapshot:\n%s", log)
	}
}

func TestInstallClaudePluginReplacesPinnedGitHubMarketplaceRefWithVersionedLocalSnapshot(t *testing.T) {
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
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected pinned marketplace removal before re-add:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add "+expectedRoot) {
		t.Fatalf("expected pinned marketplace to be replaced with the current local release snapshot:\n%s", log)
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
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	failCommand := "plugin marketplace add " + expectedRoot
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when local release snapshot replace fails")
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
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	failCommand := "plugin marketplace add " + expectedRoot
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, failCommand)+string(os.PathListSeparator)+os.Getenv("PATH"))

	writeClaudeMarketplaceFixture(t, paths.InstallRoot)
	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when local release snapshot replace fails")
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

func TestInstallClaudePluginFailsWithoutInstalledBundlePayload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.RemoveAll(paths.InstallRoot); err != nil {
		t.Fatalf("remove install root: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when the installed bundle payload is missing")
	}
	if !strings.Contains(err.Error(), "Claude release snapshot payload missing") {
		t.Fatalf("expected missing release snapshot payload error, got %v", err)
	}
}

func TestInstallClaudePluginFailsWithoutStageableClaudePayload(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	if err := os.MkdirAll(filepath.Join(paths.InstallRoot, ".claude-plugin"), 0o755); err != nil {
		t.Fatalf("mkdir plugin dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, publicBinaryName()), []byte("bundle"), 0o755); err != nil {
		t.Fatalf("write bundle binary: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, "bundle.json"), []byte(`{"version":"0.3.1"}`), 0o644); err != nil {
		t.Fatalf("write bundle.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, ".claude-plugin", "marketplace.json"), []byte(`{"plugins":[{"name":"ha-nova","source":"./"}]}`), 0o644); err != nil {
		t.Fatalf("write marketplace.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(paths.InstallRoot, ".claude-plugin", "plugin.json"), []byte(`{"name":"ha-nova","version":"0.3.1"}`), 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}

	logPath := filepath.Join(home, "claude.log")
	t.Setenv("PATH", installClaudeMarketplaceMock(t, logPath, "")+string(os.PathListSeparator)+os.Getenv("PATH"))

	err = installClaudePlugin(paths, paths.InstallRoot)
	if err == nil {
		t.Fatal("expected installClaudePlugin() to fail when the shipped Claude payload is incomplete")
	}
	if !strings.Contains(err.Error(), "Claude release snapshot payload missing") {
		t.Fatalf("expected incomplete payload to fail with missing release snapshot payload error, got %v", err)
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

func TestInstallClaudePluginReplacesStaleMarketplaceWithVersionedLocalSnapshot(t *testing.T) {
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
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	if !strings.Contains(log, "plugin marketplace remove ha-nova") {
		t.Fatalf("expected stale marketplace removal before local re-add:\n%s", log)
	}
	if !strings.Contains(log, "plugin marketplace add "+expectedRoot) {
		t.Fatalf("expected stale marketplace to be replaced with the local release snapshot:\n%s", log)
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
	expectedRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
	if err != nil {
		t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
	}
	failCommand := "plugin marketplace add " + expectedRoot
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
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "1")

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
	t.Setenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL", "1")

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

func TestCleanupClaudeMarketplaceDevResidue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	base := claudeMarketplaceBaseRoot(paths)
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatalf("MkdirAll(base) error: %v", err)
	}
	repo := t.TempDir()
	link := filepath.Join(base, "ha-nova")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatalf("Symlink() error: %v", err)
	}
	releaseRoot := filepath.Join(base, "releases", "v9.9.9")

	// Release registration active: the dev symlink is residue and goes away.
	cleanupClaudeMarketplaceDevResidue(paths, releaseRoot)
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("expected dev symlink removed after release registration, err=%v", err)
	}

	// Dev registration active (activeRoot == base): the symlink IS the
	// install and must survive.
	if err := os.Symlink(repo, link); err != nil {
		t.Fatalf("Symlink() error: %v", err)
	}
	cleanupClaudeMarketplaceDevResidue(paths, base)
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("expected dev symlink kept for dev registration, err=%v", err)
	}

	// A real directory at the link path is never touched.
	if err := os.Remove(link); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if err := os.MkdirAll(link, 0o755); err != nil {
		t.Fatalf("MkdirAll(dir) error: %v", err)
	}
	cleanupClaudeMarketplaceDevResidue(paths, releaseRoot)
	if info, err := os.Stat(link); err != nil || !info.IsDir() {
		t.Fatalf("expected real directory untouched, err=%v", err)
	}
}
