package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func installClaudePlugin(paths runtimePaths, sourceRoot string) error {
	if _, err := claudeRuntimePath(); err != nil {
		return fmt.Errorf("Claude CLI not found in PATH and no usable Desktop runtime; open Claude Desktop's Code tab or install Claude Code first")
	}

	restoreState, err := captureClaudeLocalRestoreState(paths.Home)
	if err != nil {
		return err
	}
	marketplaceRoot, err := resolveClaudeMarketplaceSource(paths, sourceRoot)
	if err != nil {
		return err
	}
	localMode := !strings.Contains(claudeMarketplaceCompareKey(marketplaceRoot), "github:")
	restoreMarketplace := func(cause error) error {
		if localMode {
			if restoreErr := restoreClaudeMarketplaceBackup(marketplaceRoot); restoreErr != nil {
				return fmt.Errorf("%w (Claude marketplace rollback failed: %v)", cause, restoreErr)
			}
		}
		if claudeStalled.Load() != nil { // its own rollback can't run either; doctor names the fix
			return cause
		}
		if restoreState.hasSource || restoreState.pluginInstalled {
			if restoreErr := restoreClaudeLocalState(paths.Home, restoreState); restoreErr != nil {
				return fmt.Errorf("%w (Claude rollback failed: %v)", cause, restoreErr)
			}
		} else if clearErr := clearClaudeMarketplaceRegistration(paths.Home); clearErr != nil {
			return fmt.Errorf("%w (Claude rollback failed: %v)", cause, clearErr)
		}
		return cause
	}
	if err := ensureClaudeMarketplaceRegistration(paths.Home, marketplaceRoot); err != nil {
		return restoreMarketplace(err)
	}

	refreshArgs := []string{"plugin", "install", "ha-nova@ha-nova"}
	alreadyInstalled := claudePluginInstalled(paths.Home)
	if localMode {
		if err := resetClaudeLocalPluginState(paths.Home); err != nil {
			return restoreMarketplace(err)
		}
		alreadyInstalled = false
	} else if alreadyInstalled {
		refreshArgs = []string{"plugin", "update", "ha-nova@ha-nova"}
	}

	if output, err := claudeOutput(refreshArgs...); err != nil {
		commandErr := fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(refreshArgs, " "), claudeFailureText(output, err))
		if localMode {
			return restoreMarketplace(commandErr)
		}
		if alreadyInstalled {
			text := strings.TrimSpace(string(output))
			if strings.Contains(strings.ToLower(text), "not found") || strings.Contains(strings.ToLower(text), "not installed") {
				if installErr := claudeStep("plugin", "install", "ha-nova@ha-nova"); installErr == nil {
					if verifyErr := verifyClaudePluginInstalled(paths, marketplaceRoot); verifyErr != nil {
						return verifyErr
					}
					cleanupClaudeMarketplaceDevResidue(paths, marketplaceRoot)
					warnClaudeProfileRedirect(paths)
					return nil
				} else {
					return restoreMarketplace(installErr)
				}
			}
		}
		return restoreMarketplace(commandErr)
	}
	if err := verifyClaudePluginInstalled(paths, marketplaceRoot); err != nil {
		return restoreMarketplace(err)
	}
	if localMode {
		if err := cleanupClaudeMarketplaceBackup(marketplaceRoot); err != nil {
			printHumanWarn("Claude marketplace backup cleanup skipped: %s", err)
		}
	}
	// Only after the sync fully verified: earlier failure paths roll back to
	// the previous (possibly dev) registration, which still needs its symlink.
	cleanupClaudeMarketplaceDevResidue(paths, marketplaceRoot)
	warnClaudeProfileRedirect(paths)
	return nil
}

func resetClaudeLocalPluginState(home string) error {
	if !claudePluginInstalled(home) {
		if err := removeClaudePluginRecord(home); err != nil {
			return err
		}
		return removeClaudePluginCache(home)
	}

	if output, err := claudeOutput("plugin", "remove", "ha-nova@ha-nova"); err != nil {
		lower := strings.ToLower(strings.TrimSpace(string(output)))
		if strings.Contains(lower, "not found") || strings.Contains(lower, "not installed") {
			if err := removeClaudePluginRecord(home); err != nil {
				return err
			}
			return removeClaudePluginCache(home)
		}
		return fmt.Errorf("claude plugin command failed: plugin remove ha-nova@ha-nova (%s)", claudeFailureText(output, err))
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
	_, installed, known := readClaudePluginState(home)
	return known && installed
}

func removeClaudeMarketplace(home string, report *uninstallReport) error {
	if _, err := claudeRuntimePath(); err != nil {
		removed, err := removeClaudeMarketplaceRecord(home)
		if err != nil {
			return err
		}
		if removed && report != nil {
			report.addRemoved("Claude marketplace ha-nova")
		}
		return nil
	}

	if output, err := claudeOutput("plugin", "marketplace", "remove", "ha-nova"); err != nil {
		lower := strings.ToLower(strings.TrimSpace(string(output)))
		if strings.Contains(lower, "not found") || strings.Contains(lower, "not installed") || strings.Contains(lower, "unknown marketplace") {
			return nil
		}
		return fmt.Errorf("claude marketplace removal failed: %s", claudeFailureText(output, err))
	}
	if report != nil {
		report.addRemoved("Claude marketplace ha-nova")
	}
	return nil
}

func removeClaudeMarketplaceRecord(home string) (bool, error) {
	path := filepath.Join(claudeConfigRoot(home), "plugins", "known_marketplaces.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return false, nil
		}
		return false, err
	}

	var raw any
	if err := unmarshalClaudeJSON(data, &raw); err != nil {
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
	return true, writeFileAtomic(path, updated, 0o644)
}

func removeClaudePluginRecord(home string) error {
	path := filepath.Join(claudeConfigRoot(home), "plugins", "installed_plugins.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}

	var raw map[string]any
	if err := unmarshalClaudeJSON(data, &raw); err != nil {
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
	return writeFileAtomic(path, updated, 0o644)
}

func unmarshalClaudeJSON(data []byte, target any) error {
	return unmarshalStrictJSON(data, "Claude JSON", target)
}

func removeClaudePluginCache(home string) error {
	cacheRoot := filepath.Join(claudeConfigRoot(home), "plugins", "cache", "ha-nova")
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
			return false
		}
		return fileExists(installPath)
	default:
		return true
	}
}

func readClaudePluginState(home string) (recordPresent bool, installed bool, known bool) {
	if strings.TrimSpace(home) == "" {
		return false, false, true
	}
	data, err := os.ReadFile(filepath.Join(claudeConfigRoot(home), "plugins", "installed_plugins.json"))
	if err != nil {
		if isNotExist(err) {
			return false, false, true
		}
		return false, false, false
	}

	var raw map[string]any
	if err := unmarshalClaudeJSON(data, &raw); err == nil {
		recordPresent = claudeInstalledPluginRecordPresent(raw["plugins"])
		installed = claudeInstalledPluginsContain(raw["plugins"])
		return recordPresent, installed, true
	}
	return false, false, false
}

func claudeInstalledPluginRecordPresent(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, entry := range typed {
			if claudeInstalledPluginRecordPresent(entry) {
				return true
			}
		}
		return false
	case map[string]any:
		if pluginValue, ok := typed["ha-nova@ha-nova"]; ok {
			return claudeInstalledPluginRecordPresent(pluginValue)
		}
		for _, key := range []string{"name", "id", "plugin"} {
			if name, ok := typed[key].(string); ok && name == "ha-nova@ha-nova" {
				return true
			}
		}
		return false
	default:
		return claudePluginRecordMatches(value)
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
