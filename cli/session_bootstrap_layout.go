package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func repairPlanTargetsSafe(paths runtimePaths, sourceRoot string, clients []string) error {
	for _, client := range clients {
		switch client {
		case "codex":
			root := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
			safe, err := managedOrMissingTree(root, filepath.Join(root, "ha-nova", "SKILL.md"), paths.InstallRoot, true)
			if err != nil || !safe {
				return fmt.Errorf("Codex target is foreign or unreadable")
			}
		case "opencode":
			root := filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova")
			safe, err := managedOrMissingTree(root, filepath.Join(root, "ha-nova", "SKILL.md"), paths.InstallRoot, true)
			if err != nil || !safe {
				return fmt.Errorf("OpenCode target is foreign or unreadable")
			}
		case "hermes":
			root := filepath.Join(paths.Home, ".hermes", "skills", "ha-nova")
			info, err := os.Lstat(root)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("Hermes target is unreadable")
			}
			namespaced, namespacedErr := skillDeclaresHANova(filepath.Join(root, "ha-nova", "SKILL.md"))
			legacy, legacyErr := skillDeclaresHANova(filepath.Join(root, "SKILL.md"))
			if namespacedErr != nil || legacyErr != nil || (!namespaced && !legacy) {
				return fmt.Errorf("Hermes target is foreign or unreadable")
			}
		case "antigravity":
			subSkills, err := sourceSubSkills(sourceRoot)
			if err != nil {
				return fmt.Errorf("Antigravity source inventory is unreadable")
			}
			installedNames := make([]string, 0, len(subSkills)+1+len(retiredAntigravitySkillNames))
			for _, skill := range append([]string{"ha-nova"}, subSkills...) {
				installedNames = append(installedNames, antigravityInstalledSkillName(skill))
			}
			installedNames = append(installedNames, retiredAntigravitySkillNames...)
			for _, installedName := range installedNames {
				dir := filepath.Join(antigravitySkillsRoot(paths.Home), installedName)
				info, existsErr := os.Lstat(dir)
				if os.IsNotExist(existsErr) {
					continue
				}
				if existsErr != nil || info.Mode()&os.ModeSymlink != 0 {
					return fmt.Errorf("Antigravity target %s is unreadable", installedName)
				}
				managed, managedErr := skillDeclaresName(filepath.Join(dir, "SKILL.md"), installedName)
				if managedErr != nil || !managed {
					return fmt.Errorf("Antigravity target %s is foreign or unreadable", installedName)
				}
			}
		default:
			return fmt.Errorf("unsupported client %q", client)
		}
	}
	return nil
}

func managedOrMissingTree(root, contextSkill, installRoot string, allowSymlink bool) (bool, error) {
	managed, err := managedSkillFingerprint(root, contextSkill, allowSymlink)
	if err != nil || managed {
		return managed, err
	}
	if allowSymlink && symlinkTargetsTransientInstallBackup(root, installRoot) {
		return true, nil
	}
	_, statErr := os.Lstat(root)
	if os.IsNotExist(statErr) {
		return true, nil
	}
	if statErr != nil {
		return false, statErr
	}
	return false, nil
}

func sessionBootstrapRepairClients(paths runtimePaths, sourceRoot string) ([]string, error) {
	subSkills, err := sourceSubSkills(sourceRoot)
	if err != nil {
		return nil, err
	}
	if len(subSkills) == 0 {
		return nil, fmt.Errorf("source contains no HA NOVA subskills")
	}
	type layout struct {
		client                  string
		fingerprint             string
		contextDir              string
		allowSymlinkFingerprint bool
		skillDir                func(string) string
	}
	treeLayout := func(root string) func(string) string {
		return func(skill string) string { return filepath.Join(root, skill) }
	}
	flatLayout := func(root string) func(string) string {
		return func(skill string) string { return filepath.Join(root, antigravityInstalledSkillName(skill)) }
	}
	hermesRoot := filepath.Join(paths.Home, ".hermes", "skills", "ha-nova")
	layouts := []layout{
		{
			client:                  "codex",
			fingerprint:             filepath.Join(paths.Home, ".agents", "skills", "ha-nova"),
			contextDir:              filepath.Join(paths.Home, ".agents", "skills", "ha-nova", "ha-nova"),
			allowSymlinkFingerprint: true,
			skillDir:                treeLayout(filepath.Join(paths.Home, ".agents", "skills", "ha-nova")),
		},
		{
			client:                  "opencode",
			fingerprint:             filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova"),
			contextDir:              filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova", "ha-nova"),
			allowSymlinkFingerprint: true,
			skillDir:                treeLayout(filepath.Join(paths.Home, ".config", "opencode", "skills", "ha-nova")),
		},
		{
			client:      "antigravity",
			fingerprint: filepath.Join(antigravitySkillsRoot(paths.Home), "ha-nova", "SKILL.md"),
			contextDir:  filepath.Join(antigravitySkillsRoot(paths.Home), "ha-nova"),
			skillDir:    flatLayout(antigravitySkillsRoot(paths.Home)),
		},
		{
			client:      "hermes",
			fingerprint: filepath.Join(hermesRoot, "ha-nova", "SKILL.md"),
			contextDir:  filepath.Join(hermesRoot, "ha-nova"),
			skillDir: func(skill string) string {
				return filepath.Join(hermesRoot, hermesInstalledSkillName(skill))
			},
		},
	}
	repair := []string{}
	for _, candidate := range layouts {
		managed, fingerprintErr := managedSkillFingerprint(
			candidate.fingerprint,
			filepath.Join(candidate.contextDir, "SKILL.md"),
			candidate.allowSymlinkFingerprint,
		)
		if !managed && candidate.allowSymlinkFingerprint {
			managed = symlinkTargetsTransientInstallBackup(candidate.fingerprint, paths.InstallRoot)
		}
		if fingerprintErr != nil {
			return nil, fmt.Errorf("inspect %s fingerprint: %w", candidate.client, fingerprintErr)
		}
		if !managed {
			legacyHermesContext := filepath.Join(hermesRoot, "SKILL.md")
			legacyManaged, legacyErr := skillDeclaresHANova(legacyHermesContext)
			if legacyErr != nil {
				return nil, fmt.Errorf("inspect Hermes legacy context: %w", legacyErr)
			}
			if candidate.client != "hermes" || !hermesLegacyBundlePresent(paths.Home) || !legacyManaged {
				continue
			}
		}
		complete, completeErr := installedLayoutHasSessionBootstrap(candidate.contextDir, candidate.skillDir, subSkills)
		if completeErr != nil {
			return nil, fmt.Errorf("inspect %s bootstrap layout: %w", candidate.client, completeErr)
		}
		if !complete {
			repair = append(repair, candidate.client)
		}
	}
	return normalizeClients(repair), nil
}

func installedLayoutHasSessionBootstrap(contextDir string, skillDir func(string) string, subSkills []string) (bool, error) {
	bootstrapExists, err := pathExistsChecked(filepath.Join(contextDir, "session-bootstrap.md"))
	if err != nil || !bootstrapExists {
		return false, err
	}
	contextHasPointer, err := markdownContainsChecked(filepath.Join(contextDir, "SKILL.md"), sessionBootstrapPointer)
	if err != nil || !contextHasPointer {
		return false, err
	}
	for _, skill := range subSkills {
		hasPointer, pointerErr := markdownContainsChecked(filepath.Join(skillDir(skill), "SKILL.md"), sessionBootstrapPointer)
		if pointerErr != nil || !hasPointer {
			return false, pointerErr
		}
	}
	return true, nil
}

// Symlinks and regular trees are ours only when their resolved canonical
// context skill declares the exact HA NOVA name. Broken or foreign links fail
// closed; explicit setup can repair them after the user resolves ownership.
func managedSkillFingerprint(fingerprint, contextSkill string, allowSymlink bool) (bool, error) {
	info, err := os.Lstat(fingerprint)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if !allowSymlink {
			return false, nil
		}
		if managed, managedErr := skillDeclaresHANova(contextSkill); managedErr != nil || managed {
			return managed, managedErr
		}
		return false, nil
	}
	return skillDeclaresHANova(contextSkill)
}

func symlinkTargetsTransientInstallBackup(link, installRoot string) bool {
	target, err := os.Readlink(link)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	target = filepath.Clean(target)
	if filepath.Base(target) != "skills" {
		return false
	}
	backupRoot := filepath.Dir(target)
	if filepath.Clean(filepath.Dir(backupRoot)) != filepath.Clean(filepath.Dir(installRoot)) {
		return false
	}
	base := filepath.Base(backupRoot)
	for _, prefix := range []string{installBackupPrefixOld, installBackupPrefixNext, installBackupPrefixFailed} {
		if strings.HasPrefix(base, prefix) && len(base) > len(prefix) {
			return true
		}
	}
	return false
}

func skillDeclaresHANova(path string) (bool, error) {
	return skillDeclaresName(path, "ha-nova")
}

func skillDeclaresName(path, expected string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "name: "+expected {
			return true, nil
		}
	}
	return false, nil
}

func pathExistsChecked(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func markdownContainsChecked(path, text string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return strings.Contains(string(data), text), nil
}

func markdownContains(path, text string) bool {
	data, err := os.ReadFile(path)
	return err == nil && strings.Contains(string(data), text)
}
