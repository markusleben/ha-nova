package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

// Tests across this package build Claude state under temp HOMEs; a
// CLAUDE_CONFIG_DIR inherited from the invoking shell (e.g. a Claude Code
// session) would redirect every reader away from those fixtures. The PATH
// stub additionally guarantees no test ever executes a developer's real
// `claude` binary (a real invocation once left backup residue inside the
// repo): it answers every call with a "not found" failure — the same
// tolerance class as claude erroring on absent plugins/marketplaces — so
// fallback paths behave like on claude-less CI. Tests that need claude
// behavior prepend their own mock in front of it.
func TestMain(m *testing.M) {
	// Route ALL go-keyring access to an in-memory mock for the whole package, so
	// no test ever touches the developer's real OS keyring (on macOS a raw
	// keyring.Set/Get pops the native "Schlüsselbund" unlock dialog and can hang
	// CI/headless runs). Tests that need a keyring FAILURE install their own
	// override on top (e.g. keyring_linux_test.go stubs keyringGetWithService, or
	// device-storage tests stub deviceStorageKeyringCanary).
	keyring.MockInit()
	// Same protection for the device-credential storage probe: on Linux its
	// keyring canary preflights the REAL DBus Secret Service before go-keyring is
	// even consulted, so MockInit alone does not isolate it. A package-wide test
	// secret dir short-circuits the probe and every device-slot read/write into
	// per-file storage. Tests that exercise the real selection logic clear this
	// env and stub the canaries instead (device_credential_storage_test.go).
	testSecretDirRoot := ""
	if os.Getenv("HA_NOVA_TEST_SECRET_DIR") == "" {
		if secretDir, err := os.MkdirTemp("", "ha-nova-test-secrets"); err == nil {
			os.Setenv("HA_NOVA_TEST_SECRET_DIR", secretDir)
			testSecretDirRoot = secretDir
		}
	}
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	// Same protection class for the update-nudge background refresh: under
	// `go test`, os.Executable() is the generated test binary, so the real
	// spawn would detach a recursive test-suite run whenever any test walks a
	// relay command into a cache miss. Nudge tests that assert spawn behavior
	// install their own recorder on top of this no-op.
	spawnDetachedUpdateRefresh = func() {}
	stubDir, err := os.MkdirTemp("", "claude-stub")
	if err == nil {
		script := "#!/usr/bin/env bash\necho \"Error: not found\" >&2\nexit 1\n"
		if writeErr := os.WriteFile(filepath.Join(stubDir, "claude"), []byte(script), 0o755); writeErr == nil {
			os.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		}
	}
	code := m.Run()
	if stubDir != "" {
		os.RemoveAll(stubDir)
	}
	if testSecretDirRoot != "" {
		os.RemoveAll(testSecretDirRoot)
	}
	os.Exit(code)
}

func TestClaudeConfigRootDefaultsToDotClaude(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home := t.TempDir()
	if got, want := claudeConfigRoot(home), filepath.Join(home, ".claude"); got != want {
		t.Fatalf("claudeConfigRoot() = %q, want %q", got, want)
	}
	if claudeConfigRootRedirected(home) {
		t.Fatalf("claudeConfigRootRedirected() = true without CLAUDE_CONFIG_DIR")
	}
}

func TestClaudeConfigRootHonorsClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	redirected := filepath.Join(t.TempDir(), "claude-profile")
	t.Setenv("CLAUDE_CONFIG_DIR", redirected)
	if got := claudeConfigRoot(home); got != redirected {
		t.Fatalf("claudeConfigRoot() = %q, want %q", got, redirected)
	}
	if !claudeConfigRootRedirected(home) {
		t.Fatalf("claudeConfigRootRedirected() = false with CLAUDE_CONFIG_DIR set")
	}
}

func TestClaudePluginInstallStateFollowsClaudeConfigDir(t *testing.T) {
	// Regression: `ha-nova update` inside a Claude Code session spawns
	// `claude plugin install`, which writes plugin state to CLAUDE_CONFIG_DIR;
	// the verifier must read the same root or every sync aborts + rolls back.
	home := t.TempDir()
	redirected := filepath.Join(t.TempDir(), "claude-profile")
	t.Setenv("CLAUDE_CONFIG_DIR", redirected)

	pluginsDir := filepath.Join(redirected, "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	installPath := filepath.Join(redirected, "plugins", "cache", "ha-nova", "ha-nova", "0.11.0")
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		t.Fatalf("mkdir installPath: %v", err)
	}
	state := `{"plugins":{"ha-nova@ha-nova":[{"installPath":` + jsonQuote(installPath) + `,"scope":"user","version":"0.11.0"}]},"version":2}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(state), 0o644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}

	found, usable := readClaudePluginInstallSnapshot(home)
	if !found || !usable {
		t.Fatalf("readClaudePluginInstallSnapshot() = (%v, %v), want (true, true) via CLAUDE_CONFIG_DIR", found, usable)
	}

	// Nothing under the default root: without the redirect the plugin is absent.
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	found, _ = readClaudePluginInstallSnapshot(home)
	if found {
		t.Fatalf("readClaudePluginInstallSnapshot() found plugin under default root unexpectedly")
	}
}

func jsonQuote(s string) string {
	quoted, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(quoted)
}
