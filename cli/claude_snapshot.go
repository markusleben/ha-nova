package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type claudeInstallSnapshot struct {
	ExpectedSource    string
	MarketplaceSource claudeMarketplaceSource
	MarketplaceFound  bool
	PluginFound       bool
	UsableInstallPath bool
	Attached          bool
	MismatchReason    string
	// StateUnreadable marks snapshots taken while a Claude state file existed
	// but could not be read or parsed — e.g. a torn write while Claude Code
	// itself updates installed_plugins.json. Such snapshots must never drive
	// destructive repair.
	StateUnreadable bool
}

func claudeMarketplaceBaseRoot(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "claude-marketplace")
}

func claudeMarketplaceDevRoot(paths runtimePaths) string {
	return claudeMarketplaceBaseRoot(paths)
}

func claudeMarketplaceReleaseRoot(paths runtimePaths, version string) (string, error) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if version == "" || version == "dev" {
		return "", fmt.Errorf("Claude release snapshot requires a stable version")
	}
	return filepath.Join(claudeMarketplaceBaseRoot(paths), "releases", "v"+version), nil
}

func claudeMarketplaceVersionForSourceRoot(paths runtimePaths, sourceRoot string) (string, error) {
	bundlePath := filepath.Join(strings.TrimSpace(sourceRoot), "bundle.json")
	if meta, err := loadBundleMetadataFile(bundlePath); err == nil && strings.TrimSpace(meta.Version) != "" {
		version := strings.TrimPrefix(strings.TrimSpace(meta.Version), "v")
		if version != "" && version != "dev" {
			return version, nil
		}
	}
	version := strings.TrimPrefix(strings.TrimSpace(localVersion(paths)), "v")
	if version == "" || version == "dev" {
		return "", fmt.Errorf("cannot determine Claude release snapshot version")
	}
	return version, nil
}

func expectedClaudeMarketplaceSource(paths runtimePaths, state installState) (string, error) {
	if BuildChannel == "dev" ||
		normalizeInstallSource(detectInstallSource(paths, state)) == installSourceDev ||
		strings.EqualFold(strings.TrimSpace(os.Getenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL")), "1") {
		return claudeMarketplaceDevRoot(paths), nil
	}
	version, err := claudeMarketplaceVersionForSourceRoot(paths, paths.InstallRoot)
	if err != nil {
		return "", err
	}
	return claudeMarketplaceReleaseRoot(paths, version)
}

func inspectClaudeInstallSnapshot(paths runtimePaths, state installState) claudeInstallSnapshot {
	snapshot := claudeInstallSnapshot{}
	expectedSource, err := expectedClaudeMarketplaceSource(paths, state)
	if err == nil {
		snapshot.ExpectedSource = expectedSource
	}
	currentSource, hasCurrentSource, err := readClaudeMarketplaceSource(paths.Home)
	if err != nil {
		snapshot.StateUnreadable = true
	} else if hasCurrentSource {
		snapshot.MarketplaceSource = currentSource
		snapshot.MarketplaceFound = true
	}
	var pluginStateUnreadable bool
	snapshot.PluginFound, snapshot.UsableInstallPath, pluginStateUnreadable = readClaudePluginPresence(paths.Home)
	if pluginStateUnreadable {
		snapshot.StateUnreadable = true
	}

	switch {
	case !snapshot.MarketplaceFound:
		snapshot.MismatchReason = "marketplace missing"
	case snapshot.ExpectedSource != "" && !sameClaudeMarketplaceSource(snapshot.MarketplaceSource, snapshot.ExpectedSource):
		snapshot.MismatchReason = "marketplace source mismatch"
	case !snapshot.PluginFound:
		snapshot.MismatchReason = "plugin missing"
	case !snapshot.UsableInstallPath:
		snapshot.MismatchReason = "plugin installPath missing"
	default:
		snapshot.Attached = true
	}
	return snapshot
}

func readClaudePluginInstallSnapshot(home string) (bool, bool) {
	found, usable, _ := readClaudePluginPresence(home)
	return found, usable
}

// readClaudePluginPresence is the attach-check reader: the state file first
// (cheap, exec-free — the healthy path never spawns a process), and only on
// a READABLE negative verdict the claude CLI's own `plugin list --json`
// answer. Claude Code has changed its state-file layout before (2.1.x
// stopped recording some installs in the file) — the CLI answer is
// schema-agnostic and prevents false "not attached" verdicts and sync
// rollbacks on such changes. Unreadable state stays unreadable (torn-write
// protection) and never triggers an exec.
func readClaudePluginPresence(home string) (found, usable, stateUnreadable bool) {
	found, usable, stateUnreadable = readClaudePluginInstallState(home)
	if stateUnreadable || (found && usable) {
		// `claude plugin disable` keeps the install record intact but loads
		// no skills — an explicit enabledPlugins=false outranks a healthy
		// install record (still exec-free: it is one more file read).
		if !stateUnreadable && found && claudePluginExplicitlyDisabled(home) {
			return false, false, false
		}
		return found, usable, stateUnreadable
	}
	if cliFound, cliUsable, ok := readClaudePluginInstallFromCLI(home); ok {
		return cliFound, cliUsable, false
	}
	return found, usable, stateUnreadable
}

func readClaudePluginInstallState(home string) (found, usable, stateUnreadable bool) {
	if strings.TrimSpace(home) == "" {
		return false, false, false
	}
	data, err := os.ReadFile(filepath.Join(claudeConfigRoot(home), "plugins", "installed_plugins.json"))
	if err != nil {
		if isNotExist(err) {
			return false, false, false
		}
		return false, false, true
	}
	var raw map[string]any
	if err := unmarshalClaudeJSON(data, &raw); err != nil {
		return false, false, true
	}
	found, usable = claudePluginInstallSnapshotFromValue(raw["plugins"])
	return found, usable, false
}

// claudePluginExplicitlyDisabled reports whether settings.json carries an
// explicit enabledPlugins["ha-nova@ha-nova"] = false. Absent file, absent
// key, or unreadable settings all count as enabled (default-on).
func claudePluginExplicitlyDisabled(home string) bool {
	data, err := os.ReadFile(filepath.Join(claudeConfigRoot(home), "settings.json"))
	if err != nil {
		return false
	}
	var raw struct {
		EnabledPlugins map[string]any `json:"enabledPlugins"`
	}
	if err := unmarshalClaudeJSON(data, &raw); err != nil {
		return false
	}
	enabled, present := raw.EnabledPlugins["ha-nova@ha-nova"]
	if !present {
		return false
	}
	value, isBool := enabled.(bool)
	return isBool && !value
}

// readClaudePluginInstallFromCLI asks `claude plugin list --json` for the
// install state of the profile belonging to the inspected home. ok=false
// means the answer is unavailable (claude missing, no --json support,
// unparseable output) — callers then fall back to the state file. ok=true
// with found=false is an authoritative "not installed".
func readClaudePluginInstallFromCLI(home string) (found, usable, ok bool) {
	// stdout goes to a real file descriptor, not a pipe: claude truncates
	// piped --json output at exactly 64 KiB (anthropics/claude-code#36685),
	// which would silently disable this confirmation for plugin-heavy users.
	outFile, err := os.CreateTemp("", "ha-nova-claude-plugin-list-*.json")
	if err != nil {
		return false, false, false
	}
	defer os.Remove(outFile.Name())
	cmd := exec.Command("claude", "plugin", "list", "--json")
	// Bind the subprocess to the inspected home: without this, claude would
	// answer for the process $HOME while the file reader inspects `home`.
	// CLAUDE_CONFIG_DIR stays inherited — both sides honor it identically.
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.Stdout = outFile
	runErr := cmd.Run()
	closeErr := outFile.Close()
	if runErr != nil || closeErr != nil {
		return false, false, false
	}
	output, err := os.ReadFile(outFile.Name())
	if err != nil {
		return false, false, false
	}
	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return false, false, false
	}
	var entries []struct {
		ID          string `json:"id"`
		InstallPath string `json:"installPath"`
		Enabled     *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(trimmed, &entries); err != nil {
		return false, false, false
	}
	for _, entry := range entries {
		if entry.ID == "ha-nova@ha-nova" {
			// A disabled plugin loads no skills — report it as not attached
			// so repair re-installs (which re-enables). Absent field = enabled.
			if entry.Enabled != nil && !*entry.Enabled {
				return false, false, true
			}
			installPath := strings.TrimSpace(entry.InstallPath)
			return true, installPath != "" && fileExists(installPath), true
		}
	}
	return false, false, true
}

func claudePluginInstallSnapshotFromValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case []any:
		recordFound := false
		usableInstallPath := false
		for _, entry := range typed {
			found, usable := claudePluginInstallSnapshotFromValue(entry)
			recordFound = recordFound || found
			usableInstallPath = usableInstallPath || usable
		}
		return recordFound, usableInstallPath
	case map[string]any:
		if pluginValue, ok := typed["ha-nova@ha-nova"]; ok {
			return true, claudePluginInstallValuePresent(pluginValue)
		}
		if !claudePluginRecordMatches(typed) {
			return false, false
		}
		installPath, _ := typed["installPath"].(string)
		installPath = strings.TrimSpace(installPath)
		if installPath == "" {
			return true, false
		}
		return true, fileExists(installPath)
	case string:
		return strings.TrimSpace(typed) == "ha-nova@ha-nova", false
	default:
		return false, false
	}
}
