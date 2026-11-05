package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Git Bash counts where Claude Code finds it on Windows; elsewhere it is
// never asked for (#938).
func TestClaudeGitBashFoundWhereClaudeCodeLooks(t *testing.T) {
	previousOS, previousLook := claudeGitBashPlatformOS, claudeGitBashLookGit
	t.Cleanup(func() { claudeGitBashPlatformOS, claudeGitBashLookGit = previousOS, previousLook })
	bash := func(t *testing.T, parts ...string) string {
		t.Helper()
		path := filepath.Join(append([]string{t.TempDir()}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, tc := range []struct {
		name    string
		os      string
		setup   func(t *testing.T, home string)
		missing bool
	}{
		{name: "macOS never asks", os: "darwin", missing: false},
		{name: "Windows without Git", os: "windows", missing: true},
		{name: "CLAUDE_CODE_GIT_BASH_PATH", os: "windows", setup: func(t *testing.T, _ string) {
			t.Setenv("CLAUDE_CODE_GIT_BASH_PATH", bash(t, "custom", "bash.exe"))
		}},
		{name: "a CLAUDE_CODE_GIT_BASH_PATH that isn't there", os: "windows", missing: true, setup: func(t *testing.T, _ string) {
			t.Setenv("CLAUDE_CODE_GIT_BASH_PATH", filepath.Join(t.TempDir(), "bash.exe"))
		}},
		{name: "Claude Code's settings.json, with a BOM", os: "windows", setup: func(t *testing.T, home string) {
			writeClaudeGitBashSettings(t, filepath.Join(home, ".claude"), bash(t, "Git", "bin", "bash.exe"), true)
		}},
		{name: "the active profile's settings.json (CLAUDE_CONFIG_DIR)", os: "windows", setup: func(t *testing.T, home string) {
			profile := t.TempDir()
			t.Setenv("CLAUDE_CONFIG_DIR", profile)
			writeClaudeGitBashSettings(t, profile, bash(t, "bash.exe"), false)
		}},
		{name: "the default profile's settings.json while another profile is active", os: "windows", missing: true, setup: func(t *testing.T, home string) {
			t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())
			writeClaudeGitBashSettings(t, filepath.Join(home, ".claude"), bash(t, "bash.exe"), false)
		}},
		{name: "git on PATH (Git\\cmd)", os: "windows", setup: func(t *testing.T, _ string) {
			root := filepath.Dir(filepath.Dir(bash(t, "Git", "bin", "bash.exe")))
			claudeGitBashLookGit = func(string) (string, error) { return filepath.Join(root, "cmd", "git.exe"), nil }
		}},
		{name: "Git for Windows in Program Files", os: "windows", setup: func(t *testing.T, _ string) {
			t.Setenv("ProgramFiles", filepath.Dir(filepath.Dir(filepath.Dir(bash(t, "Git", "bin", "bash.exe")))))
		}},
		{name: "Git for Windows for this user", os: "windows", setup: func(t *testing.T, _ string) {
			t.Setenv("LOCALAPPDATA", filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(bash(t, "Programs", "Git", "bin", "bash.exe"))))))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			claudeGitBashPlatformOS = tc.os
			claudeGitBashLookGit = func(string) (string, error) { return "", errors.New("not found") }
			for _, key := range []string{"CLAUDE_CODE_GIT_BASH_PATH", "CLAUDE_CONFIG_DIR", "ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
				t.Setenv(key, "")
			}
			home := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, home)
			}
			if got := claudeGitBashMissing(home); got != tc.missing {
				t.Fatalf("missing = %v, want %v", got, tc.missing)
			}
		})
	}
}

// Missing, the setup banner and doctor say what to install.
func TestClaudeGitBashLineSaysWhatToInstall(t *testing.T) {
	previousOS, previousLook := claudeGitBashPlatformOS, claudeGitBashLookGit
	t.Cleanup(func() { claudeGitBashPlatformOS, claudeGitBashLookGit = previousOS, previousLook })
	claudeGitBashLookGit = func(string) (string, error) { return "", errors.New("not found") }
	for _, key := range []string{"CLAUDE_CODE_GIT_BASH_PATH", "ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
		t.Setenv(key, "")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	claudeGitBashPlatformOS = "windows"
	out := &strings.Builder{}
	renderSetupCompleteBanner(out, []string{"claude"}, nil)
	if !strings.Contains(out.String(), "Git for Windows is missing.") || !strings.Contains(out.String(), "https://git-scm.com/downloads/win") {
		t.Fatalf("banner:\n%s", out)
	}
	claudeGitBashPlatformOS = "darwin"
	out.Reset()
	renderSetupCompleteBanner(out, []string{"claude"}, nil)
	if strings.Contains(out.String(), "Git for Windows") {
		t.Fatalf("banner on macOS:\n%s", out)
	}
}

// writeClaudeGitBashSettings writes dir/settings.json naming bash (any
// path separator), with a leading UTF-8 BOM when bom.
func writeClaudeGitBashSettings(t *testing.T, dir, bash string, bom bool) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"env": map[string]string{"CLAUDE_CODE_GIT_BASH_PATH": bash}})
	if err != nil {
		t.Fatal(err)
	}
	if bom {
		data = append([]byte("\ufeff"), data...)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}
