package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The SessionStart hook (hooks/) injects skills/ha-nova/index.md into every
// Claude Code session. CLI installs get it only if the release snapshot keeps
// it, whatever marketplace source the user had before.
func TestInstallClaudePluginSnapshotKeepsSessionHook(t *testing.T) {
	for name, known := range map[string]string{
		"plain GitHub URL":  `{"ha-nova":{"source":"https://github.com/markusleben/ha-nova"}}`,
		"structured GitHub": `{"ha-nova":{"source":{"source":"github","repo":"markusleben/ha-nova"}}}`,
		"pinned ref":        `{"ha-nova":{"source":{"source":"github","repo":"markusleben/ha-nova","ref":"v0.2.2"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			paths, err := detectPaths()
			if err != nil {
				t.Fatalf("detectPaths() error: %v", err)
			}
			if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins"), 0o755); err != nil {
				t.Fatalf("mkdir plugins: %v", err)
			}
			if err := os.WriteFile(filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"), []byte(known), 0o644); err != nil {
				t.Fatalf("write known marketplaces: %v", err)
			}
			t.Setenv("PATH", installClaudeMarketplaceMock(t, filepath.Join(home, "claude.log"), "")+string(os.PathListSeparator)+os.Getenv("PATH"))

			writeClaudeMarketplaceFixture(t, paths.InstallRoot)
			hooksDir := filepath.Join(paths.InstallRoot, "hooks")
			if err := os.MkdirAll(hooksDir, 0o755); err != nil {
				t.Fatalf("mkdir hooks: %v", err)
			}
			if err := os.WriteFile(filepath.Join(hooksDir, "hooks.json"), []byte(`{"hooks":{}}`), 0o644); err != nil {
				t.Fatalf("write hooks.json: %v", err)
			}
			if err := os.WriteFile(filepath.Join(hooksDir, "session-start"), []byte("#!/usr/bin/env bash\n"), 0o755); err != nil {
				t.Fatalf("write session-start: %v", err)
			}

			if err := installClaudePlugin(paths, paths.InstallRoot); err != nil {
				t.Fatalf("installClaudePlugin() error: %v", err)
			}
			releaseRoot, err := claudeMarketplaceReleaseRoot(paths, "0.1.12")
			if err != nil {
				t.Fatalf("claudeMarketplaceReleaseRoot() error: %v", err)
			}
			plugin := filepath.Join(releaseRoot, "ha-nova")
			if _, err := os.Stat(filepath.Join(plugin, "hooks", "hooks.json")); err != nil {
				t.Fatalf("snapshot lost hooks/hooks.json: %v", err)
			}
			info, err := os.Stat(filepath.Join(plugin, "hooks", "session-start"))
			if err != nil {
				t.Fatalf("snapshot lost hooks/session-start: %v", err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
				t.Fatalf("hooks/session-start lost its executable bit: %v", info.Mode())
			}
			if _, err := os.Stat(filepath.Join(plugin, publicBinaryName())); !os.IsNotExist(err) {
				t.Fatalf("snapshot must not carry the CLI binary: %v", err)
			}
		})
	}
}
