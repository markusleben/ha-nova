package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const pathBlockHeader = "# Added by HA NOVA"
const pathBlockFooter = "# End HA NOVA"
const pathExportLine = `export PATH="$HOME/.local/bin:$PATH"`
const pathFishLine = `contains -- $HOME/.local/bin $PATH; or set -gx PATH $HOME/.local/bin $PATH`
const pathFishFile = "ha-nova.fish"

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
		if !state.PathManaged && strings.TrimSpace(paths.BinDir) == "" {
			return "", nil
		}
		if !state.PathManaged && strings.TrimSpace(paths.BinDir) != "" && !isWindowsInstallSourcePath(paths.BinDir) {
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
	var cleaned []string
	for _, target := range managedPathTargets(paths, state) {
		removed, err := removePathBlockFromFile(target)
		if err != nil {
			return "", err
		}
		if removed {
			cleaned = append(cleaned, target)
		}
	}
	if len(cleaned) == 0 {
		return "", nil
	}
	return "HA NOVA PATH entry from " + strings.Join(cleaned, ", "), nil
}

// managedPathTargets lists every file an installer can have written the PATH
// block to — install.sh's detect_shell_rc today (zsh, bash, fish, ~/.profile)
// plus older installers (~/.bashrc) — and a recorded target. Sweeping them all
// keeps uninstall correct when SHELL changed since the install.
func managedPathTargets(paths runtimePaths, state installState) []string {
	var targets []string
	if target := strings.TrimSpace(state.PathTarget); target != "" {
		targets = append(targets, target)
	}
	if paths.Home == "" {
		return targets
	}
	for _, name := range []string{".zshrc", ".bash_profile", ".profile", ".bashrc"} {
		targets = append(targets, filepath.Join(paths.Home, name))
	}
	configDirs := []string{filepath.Join(paths.Home, ".config")}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		configDirs = append(configDirs, xdg)
	}
	for _, dir := range configDirs {
		targets = append(targets, filepath.Join(dir, "fish", "conf.d", pathFishFile))
	}
	seen := map[string]bool{}
	unique := targets[:0]
	for _, target := range targets {
		if key := filepath.Clean(target); !seen[key] {
			seen[key] = true
			unique = append(unique, target)
		}
	}
	return unique
}

// removePathBlockFromFile strips the HA NOVA PATH block (header, payload line,
// optional footer). The fish conf.d file is HA NOVA's own and is deleted once
// nothing else is left in it.
func removePathBlockFromFile(target string) (bool, error) {
	data, err := os.ReadFile(target)
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, err
	}
	lines := strings.Split(string(data), "\n")
	filtered := make([]string, 0, len(lines))
	removed := false
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != pathBlockHeader {
			filtered = append(filtered, lines[i])
			continue
		}
		removed = true
		if i+1 < len(lines) {
			if next := strings.TrimSpace(lines[i+1]); next == pathExportLine || next == pathFishLine {
				i++
			}
		}
		if i+1 < len(lines) && strings.TrimSpace(lines[i+1]) == pathBlockFooter {
			i++
		}
	}
	if !removed {
		return false, nil
	}
	updated := strings.Join(filtered, "\n")
	if filepath.Base(target) == pathFishFile && strings.TrimSpace(updated) == "" {
		if err := os.Remove(target); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := os.WriteFile(target, []byte(updated), 0o644); err != nil {
		return false, err
	}
	return true, nil
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

func buildSetWindowsUserPathCommand(value string) *exec.Cmd {
	// The full PATH value must never travel on the command line: real user
	// PATHs can be tens of kilobytes and CreateProcess caps the command
	// line at 32K, failing with "The filename or extension is too long".
	// Hand the value over via the environment instead.
	cmd := buildWindowsHiddenPowerShellCommand(`[Environment]::SetEnvironmentVariable("Path", $env:HA_NOVA_SET_USER_PATH, "User")`)
	cmd.Env = append(os.Environ(), "HA_NOVA_SET_USER_PATH="+value)
	return cmd
}

func setWindowsUserPath(value string) error {
	return buildSetWindowsUserPathCommand(value).Run()
}

func isWindowsInstallSourcePath(path string) bool {
	clean := strings.ToLower(filepath.Clean(path))
	return strings.HasSuffix(clean, `\programs\ha-nova`) ||
		strings.Contains(clean, `\appdata\local\programs\ha-nova`)
}
