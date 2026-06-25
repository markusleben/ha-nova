package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var sharedSkillRefPattern = regexp.MustCompile("`skills/([^`]+)`")
var docsRefPattern = regexp.MustCompile("`docs/reference/([^`]+)`")

func antigravityInstalledSkillName(skillName string) string {
	if strings.TrimSpace(skillName) == "" || skillName == "ha-nova" {
		return "ha-nova"
	}
	return "ha-nova-" + skillName
}

func antigravitySkillsRoot(home string) string {
	return filepath.Join(home, ".gemini", "config", "skills")
}

func legacyGeminiSkillsRoot(home string) string {
	return filepath.Join(home, ".gemini", "skills")
}

func installAntigravityClient(home, sourceRoot string) error {
	skillsRoot := antigravitySkillsRoot(home)
	if err := os.MkdirAll(skillsRoot, 0o755); err != nil {
		return err
	}

	subSkills, err := sourceSubSkills(sourceRoot)
	if err != nil {
		return err
	}

	if err := cleanupLegacyGeminiSkills(home, subSkills); err != nil {
		return err
	}

	if err := writeFlatSkill(filepath.Join(sourceRoot, "skills"), "ha-nova", filepath.Join(skillsRoot, antigravityInstalledSkillName("ha-nova")), sourceRoot, subSkills); err != nil {
		return err
	}
	for _, skill := range subSkills {
		if err := writeFlatSkill(filepath.Join(sourceRoot, "skills"), skill, filepath.Join(skillsRoot, antigravityInstalledSkillName(skill)), sourceRoot, subSkills); err != nil {
			return err
		}
	}
	return nil
}

func sourceSubSkills(sourceRoot string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(sourceRoot, "skills"))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	subSkills := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "ha-nova" {
			continue
		}
		if _, err := os.Stat(filepath.Join(sourceRoot, "skills", entry.Name(), "SKILL.md")); err != nil {
			continue
		}
		subSkills = append(subSkills, entry.Name())
	}
	sort.Strings(subSkills)
	return subSkills, nil
}

func managedAntigravitySkillNames(subSkills []string) map[string]struct{} {
	names := map[string]struct{}{antigravityInstalledSkillName("ha-nova"): {}}
	for _, skill := range subSkills {
		names[antigravityInstalledSkillName(skill)] = struct{}{}
	}
	return names
}

func cleanupLegacyGeminiSkills(home string, subSkills []string) error {
	for skill := range managedAntigravitySkillNames(subSkills) {
		if err := os.RemoveAll(filepath.Join(legacyGeminiSkillsRoot(home), skill)); err != nil {
			return err
		}
	}
	return nil
}

func writeFlatSkill(skillsRoot, skillName, destDir, sourceRoot string, subSkills []string) error {
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
		rewritten := rewriteFlatMarkdown(skillName, string(data), sourceDir, sourceRoot, subSkills)
		if err := os.WriteFile(filepath.Join(destDir, entry.Name()), []byte(rewritten), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func rewriteFlatMarkdown(skillName, content, sourceDir, sourceRoot string, subSkills []string) string {
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
	content = rewriteAntigravitySkillDispatchRefs(content, subSkills)
	content = rewriteAntigravitySkillFrontmatter(skillName, content)
	return content
}

func rewriteAntigravitySkillDispatchRefs(content string, subSkills []string) string {
	for _, skill := range subSkills {
		content = strings.ReplaceAll(content, "ha-nova:"+skill, "ha-nova:"+antigravityInstalledSkillName(skill))
	}
	return content
}

func rewriteAntigravitySkillFrontmatter(skillName, content string) string {
	if strings.TrimSpace(skillName) == "" || skillName == "ha-nova" {
		return content
	}
	short := "name: " + skillName
	full := "name: " + antigravityInstalledSkillName(skillName)
	return strings.Replace(content, short, full, 1)
}
