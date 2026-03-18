package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const defaultClaudeMarketplaceURL = "https://github.com/markusleben/ha-nova"

type claudeLocalRestoreState struct {
	source          string
	hasSource       bool
	pluginInstalled bool
}

func resolveClaudeMarketplaceSource(paths runtimePaths, sourceRoot string) (string, error) {
	if !useLocalClaudeMarketplace(paths, sourceRoot) {
		return defaultClaudeMarketplaceURL, nil
	}
	return prepareClaudeMarketplaceRoot(paths, sourceRoot)
}

func useLocalClaudeMarketplace(paths runtimePaths, sourceRoot string) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL")), "1") {
		return true
	}
	return sourceRoot != paths.InstallRoot
}

func prepareClaudeMarketplaceRoot(paths runtimePaths, sourceRoot string) (string, error) {
	absSourceRoot, err := filepath.Abs(sourceRoot)
	if err != nil {
		return "", err
	}

	targetRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	localSource := "./ha-nova"
	if err := stageClaudeMarketplacePluginRoot(filepath.Join(targetRoot, "ha-nova"), absSourceRoot); err != nil {
		return "", err
	}

	marketplaceData, err := rewriteClaudeMarketplaceManifest(filepath.Join(absSourceRoot, ".claude-plugin", "marketplace.json"), localSource)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Join(targetRoot, ".claude-plugin"), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(targetRoot, ".claude-plugin", "marketplace.json"), marketplaceData, 0o644); err != nil {
		return "", err
	}
	return targetRoot, nil
}

func ensureClaudeMarketplaceRegistration(home, desiredSource string) error {
	currentSource, hasCurrentSource, err := readClaudeMarketplaceSource(home)
	if err != nil {
		return err
	}
	if hasCurrentSource && sameClaudeMarketplaceSource(currentSource, desiredSource) {
		if err := updateClaudeMarketplaceRegistration(); err != nil {
			return err
		}
		return verifyClaudeMarketplaceRegistration(home, desiredSource)
	}
	if hasCurrentSource {
		if err := replaceClaudeMarketplaceRegistration(desiredSource); err != nil {
			return err
		}
		return verifyClaudeMarketplaceRegistration(home, desiredSource)
	}
	if err := addClaudeMarketplace(desiredSource); err != nil {
		return err
	}
	return verifyClaudeMarketplaceRegistration(home, desiredSource)
}

func captureClaudeLocalRestoreState(home string) (claudeLocalRestoreState, error) {
	source, hasSource, err := readClaudeMarketplaceSource(home)
	if err != nil {
		return claudeLocalRestoreState{}, err
	}
	return claudeLocalRestoreState{
		source:          source,
		hasSource:       hasSource,
		pluginInstalled: claudePluginInstalled(home),
	}, nil
}

func restoreClaudeLocalState(home string, restore claudeLocalRestoreState) error {
	if restore.hasSource {
		if err := replaceClaudeMarketplaceRegistration(restore.source); err != nil {
			return err
		}
	} else if err := clearClaudeMarketplaceRegistration(home); err != nil {
		return err
	}

	if restore.pluginInstalled {
		cmd := exec.Command("claude", "plugin", "install", "ha-nova@ha-nova")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(cmd.Args[1:], " "), strings.TrimSpace(string(output)))
		}
		return nil
	}

	if err := resetClaudeLocalPluginState(home); err != nil {
		return err
	}
	if restore.hasSource {
		return nil
	}
	return clearClaudeMarketplaceRegistration(home)
}

func rewriteClaudeMarketplaceManifest(manifestPath, pluginSource string) ([]byte, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}

	var manifest map[string]any
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	plugins, ok := manifest["plugins"].([]any)
	if !ok || len(plugins) == 0 {
		return nil, fmt.Errorf("claude marketplace manifest missing plugins")
	}

	rewroteSource := false
	for _, raw := range plugins {
		plugin, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := plugin["name"].(string); name == "ha-nova" {
			plugin["source"] = pluginSource
			rewroteSource = true
		}
	}
	if !rewroteSource {
		return nil, fmt.Errorf("claude marketplace manifest missing ha-nova plugin")
	}

	return json.MarshalIndent(manifest, "", "  ")
}

func addClaudeMarketplace(source string) error {
	cmd := exec.Command("claude", "plugin", "marketplace", "add", source)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(cmd.Args[1:], " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func updateClaudeMarketplaceRegistration() error {
	cmd := exec.Command("claude", "plugin", "marketplace", "update", "ha-nova")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(cmd.Args[1:], " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func replaceClaudeMarketplaceRegistration(source string) error {
	removeCmd := exec.Command("claude", "plugin", "marketplace", "remove", "ha-nova")
	if output, err := removeCmd.CombinedOutput(); err != nil {
		text := strings.ToLower(strings.TrimSpace(string(output)))
		if !strings.Contains(text, "not found") && !strings.Contains(text, "not installed") && !strings.Contains(text, "unknown marketplace") {
			return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(removeCmd.Args[1:], " "), strings.TrimSpace(string(output)))
		}
	}
	return addClaudeMarketplace(source)
}

func verifyClaudeMarketplaceRegistration(home, desiredSource string) error {
	currentSource, hasCurrentSource, err := readClaudeMarketplaceSource(home)
	if err != nil {
		return err
	}
	if !hasCurrentSource {
		return fmt.Errorf("Claude marketplace ha-nova not found after sync")
	}
	if !sameClaudeMarketplaceSource(currentSource, desiredSource) {
		return fmt.Errorf("Claude marketplace ha-nova source mismatch after sync")
	}
	return nil
}

func clearClaudeMarketplaceRegistration(home string) error {
	removeCmd := exec.Command("claude", "plugin", "marketplace", "remove", "ha-nova")
	if output, err := removeCmd.CombinedOutput(); err != nil {
		text := strings.ToLower(strings.TrimSpace(string(output)))
		if !strings.Contains(text, "not found") && !strings.Contains(text, "not installed") && !strings.Contains(text, "unknown marketplace") {
			return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(removeCmd.Args[1:], " "), strings.TrimSpace(string(output)))
		}
	}
	_, err := removeClaudeMarketplaceRecord(home)
	return err
}

func readClaudeMarketplaceSource(home string) (string, bool, error) {
	path := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}

	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", false, err
	}
	return claudeMarketplaceSourceFromValue(raw)
}

func claudeMarketplaceSourceFromValue(value any) (string, bool, error) {
	switch typed := value.(type) {
	case map[string]any:
		if entry, ok := typed["ha-nova"]; ok {
			return claudeMarketplaceSourceFromEntry(entry)
		}
		for _, entry := range typed {
			if !claudeMarketplaceRecordMatches(entry) {
				continue
			}
			return claudeMarketplaceSourceFromEntry(entry)
		}
	case []any:
		for _, entry := range typed {
			if !claudeMarketplaceRecordMatches(entry) {
				continue
			}
			return claudeMarketplaceSourceFromEntry(entry)
		}
	}
	return "", false, nil
}

func claudeMarketplaceSourceFromEntry(value any) (string, bool, error) {
	entry, ok := value.(map[string]any)
	if !ok {
		return "", true, nil
	}
	source, _ := entry["source"].(string)
	return strings.TrimSpace(source), true, nil
}

func sameClaudeMarketplaceSource(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return true
	}
	if !strings.Contains(left, "://") && !strings.Contains(right, "://") {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return false
}

func stageClaudeMarketplacePluginRoot(targetRoot, sourceRoot string) error {
	if err := os.MkdirAll(filepath.Dir(targetRoot), 0o755); err != nil {
		return err
	}
	if err := os.RemoveAll(targetRoot); err != nil {
		return err
	}
	if shouldFilterClaudeMarketplacePayload(sourceRoot) {
		return copyClaudeMarketplacePluginPayload(sourceRoot, targetRoot)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(sourceRoot, targetRoot); err == nil {
			return nil
		}
	}
	return copyDir(sourceRoot, targetRoot)
}

func shouldFilterClaudeMarketplacePayload(sourceRoot string) bool {
	info, err := os.Stat(filepath.Join(sourceRoot, "ha-nova"))
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func copyClaudeMarketplacePluginPayload(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if rel == "ha-nova" && !d.IsDir() {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
