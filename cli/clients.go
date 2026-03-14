package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

var sameSkillRefPattern = regexp.MustCompile("`skills/([^`/]+)/([^`]+)`")
var sharedSkillRefPattern = regexp.MustCompile("`skills/([^`]+)`")
var docsRefPattern = regexp.MustCompile("`docs/reference/([^`]+)`")

var geminiSubSkills = []string{
	"ha-nova-write",
	"ha-nova-read",
	"ha-nova-helper",
	"ha-nova-entity-discovery",
	"ha-nova-onboarding",
	"ha-nova-service-call",
	"ha-nova-review",
	"ha-nova-fallback",
}

func resolveSourceRoot(paths runtimePaths) string {
	if override := strings.TrimSpace(os.Getenv("HA_NOVA_DEV_ROOT")); override != "" {
		return override
	}
	return paths.InstallRoot
}

func sourceRoot(paths runtimePaths) string {
	return resolveSourceRoot(paths)
}

func installClients(paths runtimePaths, state *installState, clients []string) error {
	sourceRoot := resolveSourceRoot(paths)
	for _, client := range normalizeClients(clients) {
		mode, err := installClient(paths, sourceRoot, client)
		if err != nil {
			return err
		}
		if state != nil {
			if state.ClientInstallModes == nil {
				state.ClientInstallModes = map[string]string{}
			}
			state.ClientInstallModes[client] = mode
			mergeStateClients(state, []string{client})
		}
	}
	return nil
}

func installClient(paths runtimePaths, sourceRoot, client string) (string, error) {
	switch client {
	case "codex":
		return installTreeClient(filepath.Join(paths.Home, ".agents", "skills"), filepath.Join(sourceRoot, "skills"), preferSymlinkForCurrentOS())
	case "opencode":
		return installTreeClient(filepath.Join(paths.Home, ".config", "opencode", "skills"), filepath.Join(sourceRoot, "skills"), preferSymlinkForCurrentOS())
	case "gemini":
		return "copy", installGeminiClient(paths.Home, sourceRoot)
	case "claude":
		return "plugin", installClaudePlugin(sourceRoot)
	default:
		return "", fmt.Errorf("unsupported client: %s", client)
	}
}

func preferSymlinkForCurrentOS() bool {
	return runtime.GOOS != "windows"
}

func installTreeClient(parentDir, sourceDir string, preferSymlink bool) (string, error) {
	targetDir := filepath.Join(parentDir, "ha-nova")
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return "", err
	}
	_ = os.RemoveAll(targetDir)

	if preferSymlink {
		if err := os.Symlink(sourceDir, targetDir); err == nil {
			return "symlink", nil
		}
	}

	if err := copyDir(sourceDir, targetDir); err != nil {
		return "", err
	}
	return "copy", nil
}

func installGeminiClient(home, sourceRoot string) error {
	skillsRoot := filepath.Join(home, ".gemini", "skills")
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return err
	}

	if err := cleanupGeminiOrphans(skillsRoot, sourceRoot); err != nil {
		return err
	}

	if err := writeFlatSkill(filepath.Join(sourceRoot, "skills"), "ha-nova", filepath.Join(skillsRoot, "ha-nova"), sourceRoot); err != nil {
		return err
	}
	for _, skill := range geminiSubSkills {
		if err := writeFlatSkill(filepath.Join(sourceRoot, "skills"), skill, filepath.Join(skillsRoot, skill), sourceRoot); err != nil {
			return err
		}
	}
	return nil
}

func cleanupGeminiOrphans(skillsRoot, sourceRoot string) error {
	valid := map[string]struct{}{"ha-nova": {}}
	matches, err := filepath.Glob(filepath.Join(sourceRoot, "skills", "*", "SKILL.md"))
	if err != nil {
		return err
	}
	for _, match := range matches {
		valid[filepath.Base(filepath.Dir(match))] = struct{}{}
	}

	entries, err := os.ReadDir(skillsRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "ha-nova") {
			continue
		}
		if _, ok := valid[entry.Name()]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(skillsRoot, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func writeFlatSkill(skillsRoot, skillName, destDir, sourceRoot string) error {
	sourceDir := filepath.Join(skillsRoot, skillName)
	if _, err := os.Stat(filepath.Join(sourceDir, "SKILL.md")); err != nil {
		return nil
	}
	if err := os.RemoveAll(destDir); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			return err
		}
		rewritten := rewriteFlatMarkdown(skillName, string(data), sourceDir, sourceRoot)
		if err := os.WriteFile(filepath.Join(destDir, entry.Name()), []byte(rewritten), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func rewriteFlatMarkdown(skillName, content, sourceDir, sourceRoot string) string {
	entries, err := os.ReadDir(sourceDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || entry.Name() == "SKILL.md" {
				continue
			}
			from := fmt.Sprintf("`skills/%s/%s`", skillName, entry.Name())
			to := fmt.Sprintf("`%s`", entry.Name())
			content = strings.ReplaceAll(content, from, to)
		}
	}

	content = docsRefPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := docsRefPattern.FindStringSubmatch(match)
		return fmt.Sprintf("`%s`", filepath.Join(sourceRoot, "docs", "reference", parts[1]))
	})
	content = sharedSkillRefPattern.ReplaceAllStringFunc(content, func(match string) string {
		if sameSkillRefPattern.MatchString(match) {
			return match
		}
		parts := sharedSkillRefPattern.FindStringSubmatch(match)
		return fmt.Sprintf("`%s`", filepath.Join(sourceRoot, "skills", parts[1]))
	})
	return content
}

func installClaudePlugin(sourceRoot string) error {
	if _, err := exec.LookPath("claude"); err != nil {
		printWarn("Claude CLI not found in PATH; install will continue without plugin registration")
		return nil
	}

	cmds := [][]string{
		{"plugin", "marketplace", "add", sourceRoot},
		{"plugin", "install", "ha-nova@ha-nova"},
	}
	for _, args := range cmds {
		cmd := exec.Command("claude", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			printWarn("Claude plugin command failed: %s (%s)", strings.Join(args, " "), strings.TrimSpace(string(output)))
		}
	}
	return nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
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

func removeInstalledClients(paths runtimePaths, state installState) error {
	clients := append([]string{}, state.InstalledClients...)
	if len(clients) == 0 {
		clients = []string{"claude", "codex", "opencode", "gemini"}
	}
	sort.Strings(clients)
	for _, client := range clients {
		switch client {
		case "claude":
			if err := removeSkillEntries(filepath.Join(paths.Home, ".claude", "skills")); err != nil {
				return err
			}
			if _, err := exec.LookPath("claude"); err != nil {
				printWarn("Claude CLI not found; plugin removal skipped")
				continue
			}
			cmd := exec.Command("claude", "plugin", "remove", "ha-nova@ha-nova")
			if output, err := cmd.CombinedOutput(); err != nil {
				printWarn("Claude plugin removal failed: %s", strings.TrimSpace(string(output)))
			}
		case "codex":
			if err := removeSkillEntries(filepath.Join(paths.Home, ".agents", "skills")); err != nil {
				return err
			}
		case "opencode":
			if err := removeSkillEntries(filepath.Join(paths.Home, ".config", "opencode", "skills")); err != nil {
				return err
			}
		case "gemini":
			if err := removeSkillEntries(filepath.Join(paths.Home, ".gemini", "skills")); err != nil {
				return err
			}
		}
	}
	return nil
}

func removeSkillEntries(skillsRoot string) error {
	matches, err := filepath.Glob(filepath.Join(skillsRoot, "ha-nova*"))
	if err != nil {
		return err
	}
	for _, match := range matches {
		if err := os.RemoveAll(match); err != nil {
			return err
		}
	}
	return nil
}

func uninstallClients(paths runtimePaths, state installState) {
	_ = removeInstalledClients(paths, state)
}

func detectLikelyClients(paths runtimePaths) []string {
	found := []string{}
	if _, err := os.Stat(filepath.Join(paths.Home, ".agents")); err == nil {
		found = append(found, "codex")
	}
	if _, err := os.Stat(filepath.Join(paths.Home, ".config", "opencode")); err == nil {
		found = append(found, "opencode")
	}
	if _, err := os.Stat(filepath.Join(paths.Home, ".gemini")); err == nil {
		found = append(found, "gemini")
	}
	if _, err := exec.LookPath("claude"); err == nil {
		found = append(found, "claude")
	}
	if len(found) == 0 {
		return []string{"claude", "codex", "opencode", "gemini"}
	}
	sort.Strings(found)
	return found
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, info.Mode())
}
