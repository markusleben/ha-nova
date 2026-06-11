package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallGeminiClientUsesNamespacedFlatSkillNames(t *testing.T) {
	home := t.TempDir()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	skillsRoot := filepath.Join(sourceRoot, "skills")
	for _, skill := range []string{
		"ha-nova",
		"dashboard",
		"organize",
		"history",
		"write",
		"read",
		"helper",
		"entity-discovery",
		"onboarding",
		"service-call",
		"review",
		"fallback",
	} {
		dir := filepath.Join(skillsRoot, skill)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", skill, err)
		}
		content := "name: " + skill + "\n"
		if skill == "ha-nova" {
			content += "use ha-nova:entity-discovery\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s skill: %v", skill, err)
		}
	}

	if err := installGeminiClient(home, sourceRoot); err != nil {
		t.Fatalf("installGeminiClient() error: %v", err)
	}

	for _, skill := range []string{
		"ha-nova",
		"ha-nova-dashboard",
		"ha-nova-organize",
		"ha-nova-history",
		"ha-nova-write",
		"ha-nova-read",
		"ha-nova-helper",
		"ha-nova-entity-discovery",
		"ha-nova-onboarding",
		"ha-nova-service-call",
		"ha-nova-review",
		"ha-nova-fallback",
	} {
		if _, err := os.Stat(filepath.Join(home, ".gemini", "skills", skill, "SKILL.md")); err != nil {
			t.Fatalf("expected Gemini skill %s to exist: %v", skill, err)
		}
	}

	entityDiscovery, err := os.ReadFile(filepath.Join(home, ".gemini", "skills", "ha-nova-entity-discovery", "SKILL.md"))
	if err != nil {
		t.Fatalf("read Gemini entity-discovery skill: %v", err)
	}
	if !strings.Contains(string(entityDiscovery), "name: ha-nova-entity-discovery") {
		t.Fatalf("expected Gemini entity-discovery skill to use namespaced frontmatter name, got %q", string(entityDiscovery))
	}

	contextSkill, err := os.ReadFile(filepath.Join(home, ".gemini", "skills", "ha-nova", "SKILL.md"))
	if err != nil {
		t.Fatalf("read Gemini context skill: %v", err)
	}
	if !strings.Contains(string(contextSkill), "ha-nova:ha-nova-entity-discovery") {
		t.Fatalf("expected Gemini context skill to rewrite dispatch refs, got %q", string(contextSkill))
	}
}

// Matches the bash installer (scripts/onboarding/install-local-skills.sh):
// same-skill companion refs become bare filenames, every other `skills/...`
// or `docs/reference/...` ref becomes an absolute path into the source root
// so the installed flat skill can still reach cross-skill files like agent
// templates and the jq filter.
func TestRewriteFlatMarkdownAbsolutizesCrossSkillRefs(t *testing.T) {
	sourceRoot := t.TempDir()
	sourceDir := filepath.Join(sourceRoot, "skills", "write")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "patterns.md"), []byte("companion"), 0o644); err != nil {
		t.Fatalf("write companion: %v", err)
	}

	content := "Same-skill: `skills/write/patterns.md`. Cross-skill: `skills/review/SKILL.md`.\n" +
		"Agent template: `skills/ha-nova/agents/apply-agent.md`. Filter: `skills/ha-nova/config-body-filter.jq`.\n" +
		"Docs: `docs/reference/ha-template-reference.md`.\n"

	got := rewriteFlatMarkdown("write", content, sourceDir, sourceRoot, []string{"write", "review"})

	for _, want := range []string{
		"`patterns.md`",
		"`" + filepath.Join(sourceRoot, "skills", "review", "SKILL.md") + "`",
		"`" + filepath.Join(sourceRoot, "skills", "ha-nova", "agents", "apply-agent.md") + "`",
		"`" + filepath.Join(sourceRoot, "skills", "ha-nova", "config-body-filter.jq") + "`",
		"`" + filepath.Join(sourceRoot, "docs", "reference", "ha-template-reference.md") + "`",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected rewritten content to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "`skills/") {
		t.Fatalf("expected no relative skills/ refs to remain, got:\n%s", got)
	}
}
