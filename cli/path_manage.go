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
const pathBlockFooter = "# End HA NOVA"
const pathExportLine = `export PATH="$HOME/.local/bin:$PATH"`

func ensurePublicBinary(paths runtimePaths, sourceBinary string) error {
	if filepath.Clean(paths.PublicBinary) == filepath.Clean(sourceBinary) {
		return nil
	}
	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(paths.PublicBinary)
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
	if pathContainsEntry(userPath, paths.BinDir) {
		return state, nil
	}

	current, _ := readWindowsUserPath()
	if pathContainsEntry(current, paths.BinDir) {
		return state, nil
	}
	parts := splitPATH(current)
	parts = append([]string{paths.BinDir}, parts...)
	if err := setWindowsUserPath(strings.Join(parts, ";")); err != nil {
		return state, err
	}
	state.PathManaged = true
	return state, nil
}

func ensureUnixPath(paths runtimePaths, state installState) (installState, error) {
	rcFile := detectShellRC()
	block := fmt.Sprintf("\n%s\n%s\n%s\n", pathBlockHeader, pathExportLine, pathBlockFooter)

	if err := os.MkdirAll(filepath.Dir(rcFile), 0o755); err != nil {
		return state, err
	}
	data, _ := os.ReadFile(rcFile)
	if !strings.Contains(string(data), pathBlockHeader) && !strings.Contains(string(data), pathExportLine) {
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
	_, _ = removeManagedPathWithReport(paths, state)
}

func removeManagedPathWithReport(paths runtimePaths, state installState) (string, error) {
	switch runtime.GOOS {
	case "windows":
		if !state.PathManaged {
			return "", nil
		}
		current, err := readWindowsUserPath()
		if err != nil {
			return "", err
		}
		parts := splitPATH(current)
		filtered := make([]string, 0, len(parts))
		for _, part := range parts {
			if !strings.EqualFold(part, paths.BinDir) {
				filtered = append(filtered, part)
			}
		}
		if err := setWindowsUserPath(strings.Join(filtered, ";")); err != nil {
			return "", err
		}
		return "HA NOVA PATH entry from Windows user PATH", nil
	default:
		if !state.PathManaged {
			return "", nil
		}
		target := state.PathTarget
		if target == "" {
			target = detectShellRC()
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return "", err
		}
		original := string(data)
		lines := strings.Split(string(data), "\n")
		filtered := make([]string, 0, len(lines))
		removed := false
		for i := 0; i < len(lines); i++ {
			line := lines[i]
			if strings.TrimSpace(line) == pathBlockHeader {
				removed = true
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == pathExportLine {
					i++
				}
				if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == pathBlockFooter {
					i++
				}
				continue
			}
			filtered = append(filtered, line)
		}
		if !removed {
			return "", nil
		}
		updated := strings.Join(filtered, "\n")
		if updated == original {
			return "", nil
		}
		if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
			return "", err
		}
		return `HA NOVA PATH entry from ` + target, nil
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
	separator := string(os.PathListSeparator)
	if strings.Contains(value, ";") {
		separator = ";"
	}
	parts := strings.Split(value, separator)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func pathContainsEntry(value, target string) bool {
	for _, part := range splitPATH(value) {
		if strings.EqualFold(part, target) {
			return true
		}
	}
	return false
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
