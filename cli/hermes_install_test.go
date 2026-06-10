package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHermesClientKeepsHermesDirectoryNamesAlignedWithFrontmatterNames(t *testing.T) {
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
			content += "use ha-nova:review\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s skill: %v", skill, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(home, ".hermes", "skills", "ha-nova", "review"), 0o755); err != nil {
		t.Fatalf("mkdir legacy Hermes review skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".hermes", "skills", "ha-nova", "review", "SKILL.md"), []byte("name: review\n"), 0o644); err != nil {
		t.Fatalf("write legacy Hermes review skill: %v", err)
	}

	if err := installHermesClient(home, sourceRoot); err != nil {
		t.Fatalf("installHermesClient() error: %v", err)
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
		if _, err := os.Stat(filepath.Join(home, ".hermes", "skills", "ha-nova", skill, "SKILL.md")); err != nil {
			t.Fatalf("expected Hermes skill %s to exist: %v", skill, err)
		}
	}

	entityDiscovery, err := os.ReadFile(filepath.Join(home, ".hermes", "skills", "ha-nova", "ha-nova-entity-discovery", "SKILL.md"))
	if err != nil {
		t.Fatalf("read Hermes entity-discovery skill: %v", err)
	}
	if !strings.Contains(string(entityDiscovery), "name: ha-nova-entity-discovery") {
		t.Fatalf("expected Hermes entity-discovery skill to use namespaced frontmatter name, got %q", string(entityDiscovery))
	}

	contextSkill, err := os.ReadFile(filepath.Join(home, ".hermes", "skills", "ha-nova", "ha-nova", "SKILL.md"))
	if err != nil {
		t.Fatalf("read Hermes context skill: %v", err)
	}
	if !strings.Contains(string(contextSkill), "use ha-nova-entity-discovery") {
		t.Fatalf("expected Hermes context skill to rewrite dispatch refs, got %q", string(contextSkill))
	}
	if !strings.Contains(string(contextSkill), "use ha-nova-review") {
		t.Fatalf("expected Hermes context skill to rewrite additional dispatch refs, got %q", string(contextSkill))
	}
	if !strings.Contains(string(contextSkill), "name: ha-nova") {
		t.Fatalf("expected Hermes context skill to keep the root skill name, got %q", string(contextSkill))
	}
	if _, err := os.Stat(filepath.Join(home, ".hermes", "skills", "ha-nova", "entity-discovery")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy bare Hermes directory to be absent, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".hermes", "skills", "ha-nova", "review")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy bare Hermes directory to be absent, got err=%v", err)
	}
}

func TestInstallHermesClientFailsWhenContextSkillIsMissing(t *testing.T) {
	home := t.TempDir()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(filepath.Join(sourceRoot, "skills", "read"), 0o755); err != nil {
		t.Fatalf("mkdir read skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceRoot, "skills", "read", "SKILL.md"), []byte("name: read\n"), 0o644); err != nil {
		t.Fatalf("write read skill: %v", err)
	}

	err := installHermesClient(home, sourceRoot)
	if err == nil || !strings.Contains(err.Error(), "missing Hermes source skill") {
		t.Fatalf("expected missing Hermes context skill error, got %v", err)
	}
}

// Mirror of TestRewriteFlatMarkdownAbsolutizesCrossSkillRefs for the Hermes
// rewriter: the Go and bash installers must produce the same ref shapes.
func TestRewriteHermesMarkdownAbsolutizesCrossSkillRefs(t *testing.T) {
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

	got := rewriteHermesMarkdown("write", content, sourceDir, sourceRoot, []string{"write", "review"})

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
