package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func installClaudePlugin(paths runtimePaths, sourceRoot string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		return fmt.Errorf("Claude CLI not found in PATH; install Claude Code first")
	}

	localMode := useLocalClaudeMarketplace(paths, sourceRoot)
	restoreState, err := captureClaudeLocalRestoreState(paths.Home)
	if err != nil {
		return err
	}
	marketplaceRoot, err := resolveClaudeMarketplaceSource(paths, sourceRoot)
	if err != nil {
		return err
	}
	if err := ensureClaudeMarketplaceRegistration(paths.Home, marketplaceRoot); err != nil {
		if restoreState.hasSource || restoreState.pluginInstalled {
			if restoreErr := restoreClaudeLocalState(paths.Home, restoreState); restoreErr != nil {
				return fmt.Errorf("%w (Claude rollback failed: %v)", err, restoreErr)
			}
		}
		return err
	}

	refreshArgs := []string{"plugin", "install", "ha-nova@ha-nova"}
	alreadyInstalled := claudePluginInstalled(paths.Home)
	if localMode {
		if err := resetClaudeLocalPluginState(paths.Home); err != nil {
			if restoreErr := restoreClaudeLocalState(paths.Home, restoreState); restoreErr != nil {
				return fmt.Errorf("%w (Claude rollback failed: %v)", err, restoreErr)
			}
			return err
		}
		alreadyInstalled = false
	} else if alreadyInstalled {
		refreshArgs = []string{"plugin", "update", "ha-nova@ha-nova"}
	}

	cmd := exec.Command("claude", refreshArgs...)
	if output, err := cmd.CombinedOutput(); err != nil {
		if localMode {
			if restoreErr := restoreClaudeLocalState(paths.Home, restoreState); restoreErr != nil {
				return fmt.Errorf("claude plugin command failed: %s (%s); Claude rollback failed: %v", strings.Join(refreshArgs, " "), strings.TrimSpace(string(output)), restoreErr)
			}
		}
		if alreadyInstalled {
			text := strings.TrimSpace(string(output))
			if strings.Contains(strings.ToLower(text), "not found") || strings.Contains(strings.ToLower(text), "not installed") {
				installCmd := exec.Command("claude", "plugin", "install", "ha-nova@ha-nova")
				if installOutput, installErr := installCmd.CombinedOutput(); installErr == nil {
					return nil
				} else {
					return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(installCmd.Args[1:], " "), strings.TrimSpace(string(installOutput)))
				}
			}
		}
		return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(refreshArgs, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func resetClaudeLocalPluginState(home string) error {
	if !claudePluginInstalled(home) {
		if err := removeClaudePluginRecord(home); err != nil {
			return err
		}
		return removeClaudePluginCache(home)
	}

	cmd := exec.Command("claude", "plugin", "remove", "ha-nova@ha-nova")
	if output, err := cmd.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		lower := strings.ToLower(message)
		if strings.Contains(lower, "not found") || strings.Contains(lower, "not installed") {
			if err := removeClaudePluginRecord(home); err != nil {
				return err
			}
			return removeClaudePluginCache(home)
		}
		return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(cmd.Args[1:], " "), message)
	}
	if err := removeClaudePluginRecord(home); err != nil {
		return err
	}
	return removeClaudePluginCache(home)
}

func claudePluginInstalled(home string) bool {
	if strings.TrimSpace(home) == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return false
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err == nil {
		return claudeInstalledPluginsContain(raw["plugins"])
	}
	return strings.Contains(string(data), "ha-nova@ha-nova")
}

func removeClaudeMarketplace(home string, report *uninstallReport) error {
	if _, err := exec.LookPath("claude"); err != nil {
		removed, err := removeClaudeMarketplaceRecord(home)
		if err != nil {
			return err
		}
		if removed && report != nil {
			report.addRemoved("Claude marketplace ha-nova")
		}
		return nil
	}

	cmd := exec.Command("claude", "plugin", "marketplace", "remove", "ha-nova")
	if output, err := cmd.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		lower := strings.ToLower(message)
		if strings.Contains(lower, "not found") || strings.Contains(lower, "not installed") || strings.Contains(lower, "unknown marketplace") {
			return nil
		}
		return fmt.Errorf("claude marketplace removal failed: %s", message)
	}
	if report != nil {
		report.addRemoved("Claude marketplace ha-nova")
	}
	return nil
}

func removeClaudeMarketplaceRecord(home string) (bool, error) {
	path := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, err
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, err
	}
	filtered, removed := removeClaudeMarketplaceValue(raw)
	if !removed {
		return false, nil
	}
	updated, err := json.Marshal(filtered)
	if err != nil {
		return false, err
	}
	return true, os.WriteFile(path, updated, 0o644)
}

func removeClaudePluginRecord(home string) error {
	path := filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	filtered, removed := removeClaudeInstalledPluginValue(raw["plugins"])
	if !removed {
		return nil
	}
	raw["plugins"] = filtered

	updated, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, updated, 0o644)
}

func removeClaudePluginCache(home string) error {
	cacheRoot := filepath.Join(home, ".claude", "plugins", "cache", "ha-nova")
	if err := os.RemoveAll(cacheRoot); err != nil {
		return err
	}
	return nil
}

func claudeInstalledPluginsContain(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, plugin := range typed {
			if claudePluginRecordMatches(plugin) {
				return true
			}
		}
	case map[string]any:
		if pluginValue, ok := typed["ha-nova@ha-nova"]; ok {
			return claudePluginInstallValuePresent(pluginValue)
		}
	}
	return false
}

func removeClaudeInstalledPluginValue(value any) (any, bool) {
	switch typed := value.(type) {
	case []any:
		filtered := make([]any, 0, len(typed))
		removed := false
		for _, plugin := range typed {
			if claudePluginRecordMatches(plugin) {
				removed = true
				continue
			}
			filtered = append(filtered, plugin)
		}
		return filtered, removed
	case map[string]any:
		if _, ok := typed["ha-nova@ha-nova"]; !ok {
			return value, false
		}
		delete(typed, "ha-nova@ha-nova")
		return typed, true
	default:
		return value, false
	}
}

func claudePluginRecordMatches(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "ha-nova@ha-nova"
	case map[string]any:
		for _, key := range []string{"name", "id", "plugin"} {
			if name, ok := typed[key].(string); ok && name == "ha-nova@ha-nova" {
				return true
			}
		}
	}
	return false
}

func claudePluginInstallValuePresent(value any) bool {
	switch typed := value.(type) {
	case []any:
		if len(typed) == 0 {
			return false
		}
		for _, entry := range typed {
			if claudePluginInstallValuePresent(entry) {
				return true
			}
		}
		return false
	case map[string]any:
		installPath, _ := typed["installPath"].(string)
		if strings.TrimSpace(installPath) == "" {
			return true
		}
		return fileExists(installPath)
	default:
		return true
	}
}

func removeClaudeMarketplaceValue(value any) (any, bool) {
	switch typed := value.(type) {
	case []any:
		filtered := make([]any, 0, len(typed))
		removed := false
		for _, entry := range typed {
			if claudeMarketplaceRecordMatches(entry) {
				removed = true
				continue
			}
			filtered = append(filtered, entry)
		}
		return filtered, removed
	case map[string]any:
		if _, ok := typed["ha-nova"]; ok {
			delete(typed, "ha-nova")
			return typed, true
		}
		removed := false
		for key, entry := range typed {
			if claudeMarketplaceRecordMatches(entry) {
				delete(typed, key)
				removed = true
			}
		}
		return typed, removed
	default:
		return value, false
	}
}

func claudeMarketplaceRecordMatches(value any) bool {
	switch typed := value.(type) {
	case string:
		return typed == "ha-nova"
	case map[string]any:
		for _, key := range []string{"name", "id"} {
			if name, ok := typed[key].(string); ok && name == "ha-nova" {
				return true
			}
		}
	}
	return false
}
