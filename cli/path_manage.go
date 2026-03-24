package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

func removeManagedPath(paths runtimePaths, state installState) {
	_, _ = removeManagedPathWithReport(paths, state)
}

func removeManagedPathWithReport(paths runtimePaths, state installState) (string, error) {
	if runtime.GOOS == "windows" && (strings.EqualFold(state.PathTarget, "user-path") || isWindowsInstallSourcePath(paths.BinDir)) {
		if !state.PathManaged && runtime.GOOS != "windows" {
			return "", nil
		}
		current, err := readWindowsUserPath()
		if err != nil {
			return "", err
		}
		if !state.PathManaged && !pathContainsEntry(current, paths.BinDir) {
			return "", nil
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
	}
	target := state.PathTarget
	if target == "" {
		target = detectShellRC()
	}
	if !state.PathManaged {
		if _, err := os.Stat(target); err != nil {
			if isNotExist(err) {
				return "", nil
			}
			return "", err
		}
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return "", err
	}
	if !state.PathManaged && !strings.Contains(string(data), pathBlockHeader) {
		return "", nil
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
	cmd := buildWindowsHiddenPowerShellCommand(`[Environment]::GetEnvironmentVariable("Path","User")`)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func setWindowsUserPath(value string) error {
	cmd := buildWindowsHiddenPowerShellCommand(`[Environment]::SetEnvironmentVariable("Path", ` + strconv.Quote(value) + `, "User")`)
	return cmd.Run()
}

func isWindowsInstallSourcePath(path string) bool {
	clean := strings.ToLower(filepath.Clean(path))
	return strings.HasSuffix(clean, `\programs\ha-nova`) ||
		strings.Contains(clean, `\appdata\local\programs\ha-nova`) ||
		isWingetManagedPath(path)
}
