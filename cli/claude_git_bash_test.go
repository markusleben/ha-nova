package main

import (
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
		{name: "Claude Code's settings.json", os: "windows", setup: func(t *testing.T, home string) {
			path := strings.ReplaceAll(bash(t, "Git", "bin", "bash.exe"), "\\", "\\\\")
			if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"env":{"CLAUDE_CODE_GIT_BASH_PATH":"`+path+`"}}`), 0o644); err != nil {
				t.Fatal(err)
			}
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
			for _, key := range []string{"CLAUDE_CODE_GIT_BASH_PATH", "ProgramFiles", "ProgramFiles(x86)", "LOCALAPPDATA"} {
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
