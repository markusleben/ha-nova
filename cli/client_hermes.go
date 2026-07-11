package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hermesRequiredSkillDirs is the CURRENT full skill set: it gates the
// "attached/ready" signal, so a bundle predating a newly added skill reports
// not-ready and gets re-synced instead of routing dispatch to a missing skill.
// Extend it whenever a skill ships.
var hermesRequiredSkillDirs = []string{
	"ha-nova",
	"ha-nova-backup",
	"ha-nova-calendar",
	"ha-nova-camera",
	"ha-nova-dashboard",
	"ha-nova-diagnose",
	"ha-nova-energy",
	"ha-nova-entity-discovery",
	"ha-nova-fallback",
	"ha-nova-health",
	"ha-nova-helper",
	"ha-nova-history",
	"ha-nova-maintenance",
	"ha-nova-media",
	"ha-nova-mqtt",
	"ha-nova-notify",
	"ha-nova-onboarding",
	"ha-nova-organize",
	"ha-nova-read",
	"ha-nova-review",
	"ha-nova-scene",
	"ha-nova-service-call",
	"ha-nova-todo",
	"ha-nova-updates",
	"ha-nova-write",
}

// hermesLegacyRequiredSkillDirs is a FROZEN fingerprint of the historical
// unnamespaced bundle layout used to detect legacy installs for migration,
// repair, and uninstall cleanup. Old installs can never contain later skills —
// do NOT add new skills here or legacy detection stops firing entirely.
var hermesLegacyRequiredSkillDirs = []string{
	"ha-nova",
	"dashboard",
	"entity-discovery",
	"fallback",
	"helper",
	"history",
	"onboarding",
	"organize",
	"read",
	"review",
	"service-call",
	"write",
}

func hermesInstalledSkillName(skillName string) string {
	if strings.TrimSpace(skillName) == "" || skillName == "ha-nova" {
		return "ha-nova"
	}
	return "ha-nova-" + skillName
}

// hermesNamespacedContextPresent reports whether ANY namespaced Hermes bundle
// exists — every namespaced generation ships the context skill dir. It is the
// configured/detection fingerprint (so stale pre-<new-skill> bundles are still
// detected and resynced); hermesBundlePresent stays the strict readiness gate.
func hermesNamespacedContextPresent(home string) bool {
	return fileExists(filepath.Join(home, ".hermes", "skills", "ha-nova", "ha-nova", "SKILL.md"))
}

func hermesBundlePresent(home string) bool {
	bundleRoot := filepath.Join(home, ".hermes", "skills", "ha-nova")
	for _, skillDir := range hermesRequiredSkillDirs {
		if !fileExists(filepath.Join(bundleRoot, skillDir, "SKILL.md")) {
			return false
		}
	}
	return true
}

func hermesLegacyBundlePresent(home string) bool {
	bundleRoot := filepath.Join(home, ".hermes", "skills", "ha-nova")
	for _, skillDir := range hermesLegacyRequiredSkillDirs {
		if !fileExists(filepath.Join(bundleRoot, skillDir, "SKILL.md")) {
			return false
		}
	}
	return true
}

func installHermesClient(home, sourceRoot string) error {
	bundleRoot := filepath.Join(home, ".hermes", "skills", "ha-nova")
	if err := os.RemoveAll(bundleRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return err
	}

	subSkills, err := sourceSubSkills(sourceRoot)
	if err != nil {
		return err
	}

	if err := writeHermesSkill(filepath.Join(sourceRoot, "skills"), "ha-nova", filepath.Join(bundleRoot, "ha-nova"), sourceRoot, subSkills); err != nil {
		return err
	}
	for _, skill := range subSkills {
		if err := writeHermesSkill(filepath.Join(sourceRoot, "skills"), skill, filepath.Join(bundleRoot, hermesInstalledSkillName(skill)), sourceRoot, subSkills); err != nil {
			return err
		}
	}
	return nil
}

func writeHermesSkill(skillsRoot, skillName, destDir, sourceRoot string, subSkills []string) error {
	sourceDir := filepath.Join(skillsRoot, skillName)
	if _, err := os.Stat(filepath.Join(sourceDir, "SKILL.md")); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("missing Hermes source skill: %s", filepath.Join(sourceDir, "SKILL.md"))
		}
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
		rewritten := rewriteHermesMarkdown(skillName, string(data), sourceDir, sourceRoot, subSkills)
		if err := os.WriteFile(filepath.Join(destDir, entry.Name()), []byte(rewritten), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func rewriteHermesMarkdown(skillName, content, sourceDir, sourceRoot string, subSkills []string) string {
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
		parts := sharedSkillRefPattern.FindStringSubmatch(match)
		return fmt.Sprintf("`%s`", filepath.Join(sourceRoot, "skills", parts[1]))
	})
	content = rewriteHermesSkillDispatchRefs(content, subSkills)
	content = rewriteHermesSkillFrontmatter(skillName, content)
	return content
}

func rewriteHermesSkillDispatchRefs(content string, subSkills []string) string {
	for _, skill := range subSkills {
		content = strings.ReplaceAll(content, "ha-nova:"+skill, hermesInstalledSkillName(skill))
	}
	return content
}

func rewriteHermesSkillFrontmatter(skillName, content string) string {
	if strings.TrimSpace(skillName) == "" || skillName == "ha-nova" {
		return content
	}
	short := "name: " + skillName
	full := "name: " + hermesInstalledSkillName(skillName)
	return strings.Replace(content, short, full, 1)
}
