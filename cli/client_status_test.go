package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func claudeRegistryEntry() clientRegistryEntry {
	return clientRegistryEntry{ID: "claude", Label: "Claude Code", AdapterKind: "plugin_marketplace", SupportedOS: []string{"macos", "linux", "windows"}}
}

// Claude Desktop brings no claude command: setup says Claude Code is still
// needed instead of a bare "install Claude Code first".
func TestClaudeStatusReasonNamesClaudeDesktopWithoutClaudeCode(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	installClaudeListJSONMock(t, `[]`) // never the real claude
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	origRuntime, origDesktop := clientRuntimeDetectedForStatus, claudeDesktopInstalled
	t.Cleanup(func() { clientRuntimeDetectedForStatus, claudeDesktopInstalled = origRuntime, origDesktop })
	clientRuntimeDetectedForStatus = func(string) bool { return false }

	desktopHome := ""
	claudeDesktopInstalled = func(h string) bool { desktopHome = h; return true }
	status := evaluateClientStatus(paths, installState{}, claudeRegistryEntry())
	if desktopHome != home || !strings.Contains(status.Reason, "Claude Desktop is installed") || !strings.Contains(status.Reason, "install Claude Code first") {
		t.Fatalf("reason with Claude Desktop = %q (checked home %q), want Claude Desktop named and Claude Code to install", status.Reason, desktopHome)
	}

	claudeDesktopInstalled = func(string) bool { return false }
	if got := evaluateClientStatus(paths, installState{}, claudeRegistryEntry()).Reason; got != "install Claude Code first" {
		t.Fatalf("reason without Claude Desktop = %q", got)
	}

	clientRuntimeDetectedForStatus = func(string) bool { return true }
	claudeDesktopInstalled = func(string) bool { return true }
	if got := evaluateClientStatus(paths, installState{}, claudeRegistryEntry()).Reason; got != "" {
		t.Fatalf("reason with Claude Code here = %q, want none", got)
	}
}

// On Windows, installers put claude.exe where setup's PATH, read when it
// started, doesn't reach: Claude Code's own installer in
// %USERPROFILE%\.local\bin (PATH unchanged); WinGet a link in Links
// (Developer Mode or elevated), else the package folder (both on PATH only for
// new processes). Setup finds each, so Check again sees an install made while
// it runs, and every claude call uses that file.
func TestClaudeFoundWhereWindowsInstallersPutIt(t *testing.T) {
	previousOS, previousDesktop := commandOffPathOS, claudeDesktopPlatformOS
	t.Cleanup(func() { commandOffPathOS, claudeDesktopPlatformOS = previousOS, previousDesktop })
	claudeDesktopPlatformOS = "windows"
	profile := func(t *testing.T) string {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
		t.Setenv("PATH", t.TempDir()) // never the real claude
		return home
	}
	put := func(t *testing.T, path string) string {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil { // no mode bits, as on Windows
			t.Fatal(err)
		}
		return path
	}
	winget := filepath.Join("AppData", "Local", "Microsoft", "WinGet")
	for _, place := range []struct{ name, dir string }{
		{"claude installer", filepath.Join(".local", "bin")},
		{"winget link", filepath.Join(winget, "Links")},
		{"winget package folder", filepath.Join(winget, "Packages", "Anthropic.ClaudeCode_Microsoft.Winget.Source_8wekyb3d8bbwe")},
	} {
		t.Run(place.name, func(t *testing.T) {
			commandOffPathOS = "windows"
			want := put(t, filepath.Join(profile(t), place.dir, "claude.exe"))
			if got, err := claudeRuntimePath(); err != nil || got != want || !clientRuntimeDetected("claude") || !commandRuntimeDetected("claude") {
				t.Fatalf("claudeRuntimePath() = %q, %v; want %q", got, err, want)
			}
		})
	}

	notFound := func(t *testing.T) {
		t.Helper()
		if got := commandOffPath("claude"); got != "" || clientRuntimeDetected("claude") || commandRuntimeDetected("claude") {
			t.Fatalf("claude counted: %q", got)
		}
	}
	t.Run("no LOCALAPPDATA", func(t *testing.T) {
		commandOffPathOS = "windows"
		home := profile(t)
		put(t, filepath.Join(home, winget, "Links", "claude.exe"))
		t.Setenv("LOCALAPPDATA", "")
		notFound(t)
	})
	t.Run("linux", func(t *testing.T) {
		commandOffPathOS = "linux"
		home := profile(t)
		put(t, filepath.Join(home, ".local", "bin", "claude")) // not executable
		put(t, filepath.Join(home, winget, "Links", "claude"))
		notFound(t)
	})
}

func TestDoctorNamesAnOutdatedClaudePlugin(t *testing.T) {
	outdated := doctorClientNotLoaded(clientStatus{Label: "Claude Code", PluginOutdated: true})
	if outdated != "Claude Code still uses an older version of HA NOVA" {
		t.Fatalf("outdated line = %q", outdated)
	}
	if got := doctorClientNotLoaded(clientStatus{Label: "Codex CLI"}); got != "Codex CLI doesn't load HA NOVA right now" {
		t.Fatalf("not loaded line = %q", got)
	}
}
