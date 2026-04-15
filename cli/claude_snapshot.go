package main

import (
	"fmt"
	"os"
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
	if normalizeInstallSource(detectInstallSource(paths, state)) == installSourceDev ||
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
	if err == nil && hasCurrentSource {
		snapshot.MarketplaceSource = currentSource
		snapshot.MarketplaceFound = true
	}
	snapshot.PluginFound, snapshot.UsableInstallPath = readClaudePluginInstallSnapshot(paths.Home)

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
	if strings.TrimSpace(home) == "" {
		return false, false
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return false, false
	}
	var raw map[string]any
	if err := unmarshalClaudeJSON(data, &raw); err != nil {
		return false, false
	}
	return claudePluginInstallSnapshotFromValue(raw["plugins"])
}

func claudePluginInstallSnapshotFromValue(value any) (bool, bool) {
	switch typed := value.(type) {
	case []any:
		recordFound := len(typed) > 0
		usableInstallPath := false
		for _, entry := range typed {
			_, usable := claudePluginInstallSnapshotFromValue(entry)
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
