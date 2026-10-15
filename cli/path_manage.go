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
const pathLegacyBlockHeader = "# Added by HA NOVA installer" // pre-Go installer
const pathTargetRecordFile = "path-target"                   // written by install.sh
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
	_, _ = removeManagedPathWithReport(paths, state, nil)
}

func removeManagedPathWithReport(paths runtimePaths, state installState, report *uninstallReport) (string, error) {
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
	recorded, swept, err := managedPathTargets(paths, state)
	if err != nil {
		return "", err
	}
	var cleaned []string
	for _, target := range recorded {
		removed, err := removePathBlockFromFile(target)
		if err != nil {
			return "", err
		}
		if removed {
			cleaned = append(cleaned, target)
		}
	}
	// The sweep is best effort: an unreadable dotfile the installer may never
	// have touched must not abort the uninstall.
	for _, target := range swept {
		removed, err := removePathBlockFromFile(target)
		if err != nil {
			report.addNote(fmt.Sprintf("Could not check %s for the HA NOVA PATH entry (%v); remove the \"%s\" block there manually if present.", target, err, pathBlockHeader))
			continue
		}
		if removed {
			cleaned = append(cleaned, target)
		}
	}
	if record := pathTargetRecordPath(paths); record != "" {
		if err := os.Remove(record); err != nil && !isNotExist(err) {
			return "", err
		}
	}
	if len(cleaned) == 0 {
		return "", nil
	}
	return "HA NOVA PATH entry from " + strings.Join(cleaned, ", "), nil
}

func pathTargetRecordPath(paths runtimePaths) string {
	if paths.ConfigDir == "" {
		return ""
	}
	return filepath.Join(paths.ConfigDir, pathTargetRecordFile)
}

// managedPathTargets returns the recorded targets (state.json, and the file
// install.sh wrote — hard failures) and the best-effort sweep of every file an
// installer can have written the block to: install.sh's detect_shell_rc today
// (zsh, bash, fish, ~/.profile) plus older installers (~/.bashrc). The sweep
// keeps uninstall correct when SHELL changed since the install and for
// installs made before the target was recorded.
func managedPathTargets(paths runtimePaths, state installState) (recorded, swept []string, err error) {
	seen := map[string]bool{}
	add := func(list *[]string, target string) {
		if key := filepath.Clean(target); target != "" && !seen[key] {
			seen[key] = true
			*list = append(*list, target)
		}
	}
	add(&recorded, strings.TrimSpace(state.PathTarget))
	if record := pathTargetRecordPath(paths); record != "" {
		data, readErr := os.ReadFile(record)
		switch {
		case readErr == nil:
			if target := strings.TrimSpace(string(data)); validRecordedPathTarget(target) {
				add(&recorded, target)
			}
		case !isNotExist(readErr):
			// Keep the record for a retry instead of guessing past it.
			return nil, nil, fmt.Errorf("read recorded PATH target %s: %w", record, readErr)
		}
	}
	if paths.Home == "" {
		return recorded, swept, nil
	}
	for _, name := range []string{".zshrc", ".bash_profile", ".profile", ".bashrc"} {
		add(&swept, filepath.Join(paths.Home, name))
	}
	add(&swept, filepath.Join(paths.Home, ".config", "fish", "conf.d", pathFishFile))
	// The record can be lost (re-install with another SHELL, a relocated
	// HA_NOVA_CONFIG_DIR); install.sh writes fish's file under XDG_CONFIG_HOME.
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); filepath.IsAbs(xdg) {
		add(&swept, filepath.Join(xdg, "fish", "conf.d", pathFishFile))
	}
	return recorded, swept, nil
}

// validRecordedPathTarget only accepts an absolute path to a file install.sh
// can write, so a tampered record cannot point uninstall at arbitrary files.
func validRecordedPathTarget(target string) bool {
	if !filepath.IsAbs(target) {
		return false
	}
	switch filepath.Base(target) {
	case ".zshrc", ".bash_profile", ".profile", ".bashrc", pathFishFile:
		return true
	}
	return false
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
		if header := strings.TrimSpace(lines[i]); header != pathBlockHeader && header != pathLegacyBlockHeader {
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
