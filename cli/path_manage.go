package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const pathBlockHeader = "# Added by HA NOVA"

func ensurePublicBinary(paths runtimePaths, sourceBinary string) error {
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(paths.PublicBinary)
	if runtime.GOOS == "windows" {
		launcher := "@echo off\r\n\"" + sourceBinary + "\" %*\r\n"
		return os.WriteFile(paths.PublicBinary, []byte(launcher), 0o644)
	}
	return os.Symlink(sourceBinary, paths.PublicBinary)
}

func ensureManagedPath(paths runtimePaths, state installState) (installState, error) {
	switch runtime.GOOS {
	case "windows":
		return ensureWindowsPath(paths, state)
	default:
		return ensureUnixPath(paths, state)
	}
}

func ensureWindowsPath(paths runtimePaths, state installState) (installState, error) {
	state.PathTarget = "user-path"
	userPath := os.Getenv("PATH")
	if strings.Contains(strings.ToLower(userPath), strings.ToLower(paths.BinDir)) {
		return state, nil
	}

	current, _ := readWindowsUserPath()
	parts := splitPATH(current)
	for _, part := range parts {
		if strings.EqualFold(part, paths.BinDir) {
			return state, nil
		}
	}
	parts = append([]string{paths.BinDir}, parts...)
	if err := setWindowsUserPath(strings.Join(parts, ";")); err != nil {
		return state, err
	}
	state.PathManaged = true
	return state, nil
}

func ensureUnixPath(paths runtimePaths, state installState) (installState, error) {
	rcFile := detectShellRC()
	line := `export PATH="$HOME/.local/bin:$PATH"`
	block := fmt.Sprintf("\n%s\n%s\n", pathBlockHeader, line)

	if err := os.MkdirAll(filepath.Dir(rcFile), 0o755); err != nil {
		return state, err
	}
	data, _ := os.ReadFile(rcFile)
	if !strings.Contains(string(data), line) {
		f, err := os.OpenFile(rcFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return state, err
		}
		if _, err := f.WriteString(block); err != nil {
			f.Close()
			return state, err
		}
		_ = f.Close()
		state.PathManaged = true
	}
	state.PathTarget = rcFile
	return state, nil
}

func removeManagedPath(paths runtimePaths, state installState) {
	switch runtime.GOOS {
	case "windows":
		if !state.PathManaged {
			return
		}
		current, err := readWindowsUserPath()
		if err != nil {
			return
		}
		parts := splitPATH(current)
		filtered := make([]string, 0, len(parts))
		for _, part := range parts {
			if !strings.EqualFold(part, paths.BinDir) {
				filtered = append(filtered, part)
			}
		}
		_ = setWindowsUserPath(strings.Join(filtered, ";"))
	default:
		target := state.PathTarget
		if target == "" {
			target = detectShellRC()
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return
		}
		lines := strings.Split(string(data), "\n")
		filtered := make([]string, 0, len(lines))
		for _, line := range lines {
			if line == pathBlockHeader || strings.TrimSpace(line) == `export PATH="$HOME/.local/bin:$PATH"` {
				continue
			}
			filtered = append(filtered, line)
		}
		_ = os.WriteFile(target, []byte(strings.Join(filtered, "\n")), 0o644)
	}
}

func detectShellRC() string {
	shellName := filepath.Base(os.Getenv("SHELL"))
	home, _ := os.UserHomeDir()
	switch shellName {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "bash":
		for _, candidate := range []string{".bash_profile", ".profile", ".bashrc"} {
			path := filepath.Join(home, candidate)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
		return filepath.Join(home, ".bash_profile")
	default:
		return filepath.Join(home, ".profile")
	}
}

func splitPATH(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, string(os.PathListSeparator))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func readWindowsUserPath() (string, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `[Environment]::GetEnvironmentVariable("Path","User")`)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func setWindowsUserPath(value string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", fmt.Sprintf(`[Environment]::SetEnvironmentVariable("Path", %q, "User")`, value))
	return cmd.Run()
}
