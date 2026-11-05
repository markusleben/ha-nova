package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Claude Code on native Windows runs without Git for Windows (it falls back
// to PowerShell), but HA NOVA's SessionStart hook (hooks/session-start) is a
// bash script, and Claude Code runs hook commands in Git Bash there, or in
// PowerShell when it can't find one (code.claude.com/docs/en/hooks). Without
// Git Bash, HA NOVA's start-up step in Claude Code never runs (#938).

// Test seams: the platform, and where git is found.
var (
	claudeGitBashPlatformOS = runtime.GOOS
	claudeGitBashLookGit    = exec.LookPath
)

// claudeGitBashMissing: on Windows, no Git Bash where Claude Code looks for
// one: CLAUDE_CODE_GIT_BASH_PATH (environment, or Claude Code's
// settings.json env), next to git on PATH, or Git for Windows' default
// folders. Elsewhere, never.
func claudeGitBashMissing(home string) bool {
	if claudeGitBashPlatformOS != "windows" {
		return false
	}
	candidates := []string{os.Getenv("CLAUDE_CODE_GIT_BASH_PATH")}
	var settings struct {
		Env map[string]string `json:"env"`
	}
	if data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); err == nil && json.Unmarshal(data, &settings) == nil {
		candidates = append(candidates, settings.Env["CLAUDE_CODE_GIT_BASH_PATH"])
	}
	if git, err := claudeGitBashLookGit("git"); err == nil {
		dir := filepath.Dir(git) // Git\cmd, Git\bin, or Git\mingw64\bin
		candidates = append(candidates, filepath.Join(dir, "bash.exe"), filepath.Join(dir, "..", "bin", "bash.exe"), filepath.Join(dir, "..", "..", "bin", "bash.exe"))
	}
	for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")} {
		if root != "" && root != "Programs" {
			candidates = append(candidates, filepath.Join(root, "Git", "bin", "bash.exe"))
		}
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); path != "" && err == nil && info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// claudeGitBashLine: what to install, when Git Bash is missing; else "".
func claudeGitBashLine() string {
	home, err := os.UserHomeDir()
	if err != nil || !claudeGitBashMissing(home) {
		return ""
	}
	return setupT("setup.claude.git_bash_missing", "url", "https://git-scm.com/downloads/win")
}
