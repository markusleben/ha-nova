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
	source          claudeMarketplaceSource
	hasSource       bool
	pluginInstalled bool
}

type claudeMarketplaceSource struct {
	command    string
	compareKey string
}

func resolveClaudeMarketplaceSource(paths runtimePaths, sourceRoot string) (string, error) {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL")), "1") {
		return prepareClaudeMarketplaceRoot(paths, sourceRoot)
	}

	switch detectInstallSource(paths, loadStateOrDefault(paths)) {
	case installSourceDev:
		return prepareClaudeMarketplaceRoot(paths, sourceRoot)
	case installSourceBundle:
		if shippedClaudeMarketplacePresentOnDisk(sourceRoot) {
			return prepareClaudeMarketplaceRoot(paths, sourceRoot)
		}
		if bundleInstallPresentOnDisk(sourceRoot) {
			return "", fmt.Errorf("installed Claude payload missing from shipped bundle runtime")
		}
		return defaultClaudeMarketplaceURL, nil
	default:
		return defaultClaudeMarketplaceURL, nil
	}
}

func useLocalClaudeMarketplace(paths runtimePaths, sourceRoot string) bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("HA_NOVA_CLAUDE_MARKETPLACE_LOCAL")), "1") {
		return true
	}
	switch detectInstallSource(paths, loadStateOrDefault(paths)) {
	case installSourceDev:
		return true
	case installSourceBundle:
		return shippedClaudeMarketplacePresentOnDisk(sourceRoot)
	default:
		return false
	}
}

func shippedClaudeMarketplacePresentOnDisk(sourceRoot string) bool {
	sourceRoot = strings.TrimSpace(sourceRoot)
	if sourceRoot == "" {
		return false
	}
	requiredFiles := []string{
		filepath.Join(sourceRoot, "bundle.json"),
		filepath.Join(sourceRoot, "clients", "registry.json"),
		filepath.Join(sourceRoot, ".claude-plugin", "marketplace.json"),
		filepath.Join(sourceRoot, ".claude-plugin", "plugin.json"),
	}
	for _, path := range requiredFiles {
		if _, err := os.Stat(path); err != nil {
			return false
		}
	}
	requiredDirs := []string{
		filepath.Join(sourceRoot, "skills"),
	}
	for _, path := range requiredDirs {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

func prepareClaudeMarketplaceRoot(paths runtimePaths, sourceRoot string) (string, error) {
	absSourceRoot, err := filepath.Abs(sourceRoot)
	if err != nil {
		return "", err
	}

	targetRoot := filepath.Join(paths.ConfigDir, "claude-marketplace")
	localSource := "./ha-nova"
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		return "", err
	}
	stageRoot, err := os.MkdirTemp(paths.ConfigDir, "claude-marketplace-stage-")
	if err != nil {
		return "", err
	}
	stageActive := true
	defer func() {
		if stageActive {
			_ = os.RemoveAll(stageRoot)
		}
	}()

	if err := stageClaudeMarketplacePluginRoot(filepath.Join(stageRoot, "ha-nova"), absSourceRoot); err != nil {
		return "", err
	}

	marketplaceData, err := rewriteClaudeMarketplaceManifest(filepath.Join(absSourceRoot, ".claude-plugin", "marketplace.json"), localSource)
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Join(stageRoot, ".claude-plugin"), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(stageRoot, ".claude-plugin", "marketplace.json"), marketplaceData, 0o644); err != nil {
		return "", err
	}

	if err := validateClaudeMarketplaceRoot(stageRoot); err != nil {
		return "", err
	}
	if err := replaceClaudeMarketplaceRoot(targetRoot, stageRoot); err != nil {
		return "", err
	}
	stageActive = false
	return targetRoot, nil
}

func validateClaudeMarketplaceRoot(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return fmt.Errorf("Claude marketplace root missing")
	}
	if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "marketplace.json")); err != nil {
		return fmt.Errorf("Claude marketplace manifest missing: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ha-nova")); err != nil {
		return fmt.Errorf("Claude marketplace plugin payload missing: %w", err)
	}
	cmd := exec.Command("claude", "plugin", "validate", root)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("claude plugin command failed: %s (%s)", strings.Join(cmd.Args[1:], " "), strings.TrimSpace(string(output)))
	}
	return nil
}

func replaceClaudeMarketplaceRoot(targetRoot, stagedRoot string) error {
	targetRoot = filepath.Clean(targetRoot)
	stagedRoot = filepath.Clean(stagedRoot)
	backupRoot := targetRoot + ".backup"

	if err := os.RemoveAll(backupRoot); err != nil {
		return err
	}

	hadExisting := false
	if _, err := os.Lstat(targetRoot); err == nil {
		hadExisting = true
		if err := os.Rename(targetRoot, backupRoot); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.Rename(stagedRoot, targetRoot); err != nil {
		if hadExisting {
			_ = os.Rename(backupRoot, targetRoot)
		}
		return err
	}
	return nil
}

func cleanupClaudeMarketplaceBackup(targetRoot string) error {
	backupRoot := filepath.Clean(targetRoot) + ".backup"
	if err := os.RemoveAll(backupRoot); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func restoreClaudeMarketplaceBackup(targetRoot string) error {
	targetRoot = filepath.Clean(targetRoot)
	backupRoot := targetRoot + ".backup"
	if _, err := os.Lstat(backupRoot); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(targetRoot); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(backupRoot, targetRoot)
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
		if err := replaceClaudeMarketplaceRegistration(restore.source.command); err != nil {
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
	if err := unmarshalClaudeJSON(data, &manifest); err != nil {
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

func readClaudeMarketplaceSource(home string) (claudeMarketplaceSource, bool, error) {
	path := filepath.Join(home, ".claude", "plugins", "known_marketplaces.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return claudeMarketplaceSource{}, false, nil
		}
		return claudeMarketplaceSource{}, false, err
	}

	var raw any
	if err := unmarshalClaudeJSON(data, &raw); err != nil {
		return claudeMarketplaceSource{}, false, err
	}
	return claudeMarketplaceSourceFromValue(raw)
}

func claudeMarketplaceSourceFromValue(value any) (claudeMarketplaceSource, bool, error) {
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
	return claudeMarketplaceSource{}, false, nil
}

func claudeMarketplaceSourceFromEntry(value any) (claudeMarketplaceSource, bool, error) {
	entry, ok := value.(map[string]any)
	if !ok {
		return claudeMarketplaceSource{}, true, nil
	}
	return claudeMarketplaceSourceFromRaw(entry["source"]), true, nil
}

func claudeMarketplaceSourceFromRaw(value any) claudeMarketplaceSource {
	switch typed := value.(type) {
	case string:
		source := strings.TrimSpace(typed)
		if strings.EqualFold(source, "github") {
			return claudeMarketplaceSource{}
		}
		return newClaudeMarketplaceSource(source, claudeMarketplaceCompareKey(source))
	case map[string]any:
		if url, ok := typed["url"].(string); ok && strings.TrimSpace(url) != "" {
			url = strings.TrimSpace(url)
			return newClaudeMarketplaceSource(urlSourceCommand(url, strings.TrimSpace(stringValue(typed["ref"]))), githubCompareKey(url, strings.TrimSpace(stringValue(typed["ref"]))))
		}
		if path, ok := typed["path"].(string); ok && strings.TrimSpace(path) != "" {
			path = strings.TrimSpace(path)
			return newClaudeMarketplaceSource(path, path)
		}
		if repo, ok := typed["repo"].(string); ok && strings.TrimSpace(repo) != "" {
			repo = strings.TrimSpace(repo)
			if sourceKind, _ := typed["source"].(string); strings.EqualFold(strings.TrimSpace(sourceKind), "github") {
				repoURL := "https://github.com/" + strings.TrimPrefix(strings.TrimSuffix(repo, ".git"), "/")
				return newClaudeMarketplaceSource(githubSourceCommand(repo, strings.TrimSpace(stringValue(typed["ref"]))), githubCompareKey(repoURL, strings.TrimSpace(stringValue(typed["ref"]))))
			}
			return newClaudeMarketplaceSource(repo, repo)
		}
		if source, ok := typed["source"].(string); ok {
			source = strings.TrimSpace(source)
			if source != "" && !strings.EqualFold(source, "github") {
				return newClaudeMarketplaceSource(source, source)
			}
		}
	}
	return claudeMarketplaceSource{}
}

func sameClaudeMarketplaceSource(left claudeMarketplaceSource, right string) bool {
	leftKey := strings.TrimSpace(left.compareKey)
	rightKey := strings.TrimSpace(claudeMarketplaceCompareKey(right))
	if leftKey == "" {
		leftKey = strings.TrimSpace(left.command)
	}
	if rightKey == "" {
		rightKey = strings.TrimSpace(right)
	}
	if leftKey == rightKey {
		return true
	}
	if !strings.Contains(leftKey, "://") && !strings.Contains(rightKey, "://") {
		return filepath.Clean(leftKey) == filepath.Clean(rightKey)
	}
	return false
}

func newClaudeMarketplaceSource(command, compareKey string) claudeMarketplaceSource {
	command = strings.TrimSpace(command)
	compareKey = strings.TrimSpace(compareKey)
	if compareKey == "" {
		compareKey = command
	}
	return claudeMarketplaceSource{
		command:    command,
		compareKey: compareKey,
	}
}

func claudeMarketplaceCompareKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if normalized := githubCompareKey(value, ""); normalized != "" {
		return normalized
	}
	return value
}

func githubCompareKey(source, ref string) string {
	repo := normalizeGitHubRepo(source)
	if repo == "" {
		return ""
	}
	if ref != "" {
		return "github:" + repo + "@ref=" + ref
	}
	return "github:" + repo
}

func githubSourceCommand(repo, ref string) string {
	repo = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(repo, ".git"), "/"))
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return repo
	}
	return repo + "#" + ref
}

func urlSourceCommand(rawURL, ref string) string {
	rawURL = strings.TrimSpace(rawURL)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return rawURL
	}
	return rawURL + "#" + ref
}

func normalizeGitHubRepo(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.TrimSuffix(value, ".git")
	value = strings.TrimSuffix(value, "/")
	switch {
	case strings.HasPrefix(value, "https://github.com/"):
		return strings.ToLower(strings.TrimPrefix(value, "https://github.com/"))
	case strings.HasPrefix(value, "http://github.com/"):
		return strings.ToLower(strings.TrimPrefix(value, "http://github.com/"))
	}
	return ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
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
