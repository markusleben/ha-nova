package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallAntigravityClientUsesNamespacedFlatSkillNames(t *testing.T) {
	home := t.TempDir()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	skillsRoot := filepath.Join(sourceRoot, "skills")
	for _, skill := range []string{
		"ha-nova",
		"calendar",
		"dashboard",
		"organize",
		"health",
		"history",
		"write",
		"read",
		"helper",
		"entity-discovery",
		"onboarding",
		"service-call",
		"review",
		"scene",
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

	if err := installAntigravityClient(home, sourceRoot); err != nil {
		t.Fatalf("installAntigravityClient() error: %v", err)
	}

	for _, skill := range []string{
		"ha-nova",
		"ha-nova-calendar",
		"ha-nova-dashboard",
		"ha-nova-organize",
		"ha-nova-health",
		"ha-nova-history",
		"ha-nova-write",
		"ha-nova-read",
		"ha-nova-helper",
		"ha-nova-entity-discovery",
		"ha-nova-onboarding",
		"ha-nova-service-call",
		"ha-nova-review",
		"ha-nova-scene",
		"ha-nova-fallback",
	} {
		if _, err := os.Stat(filepath.Join(antigravitySkillsRoot(home), skill, "SKILL.md")); err != nil {
			t.Fatalf("expected Antigravity skill %s to exist: %v", skill, err)
		}
	}

	entityDiscovery, err := os.ReadFile(filepath.Join(antigravitySkillsRoot(home), "ha-nova-entity-discovery", "SKILL.md"))
	if err != nil {
		t.Fatalf("read Antigravity entity-discovery skill: %v", err)
	}
	if !strings.Contains(string(entityDiscovery), "name: ha-nova-entity-discovery") {
		t.Fatalf("expected Antigravity entity-discovery skill to use namespaced frontmatter name, got %q", string(entityDiscovery))
	}

	contextSkill, err := os.ReadFile(filepath.Join(antigravitySkillsRoot(home), "ha-nova", "SKILL.md"))
	if err != nil {
		t.Fatalf("read Antigravity context skill: %v", err)
	}
	if !strings.Contains(string(contextSkill), "ha-nova:ha-nova-entity-discovery") {
		t.Fatalf("expected Antigravity context skill to rewrite dispatch refs, got %q", string(contextSkill))
	}
}

func TestInstallAntigravityClientRemovesRetiredCurrentSkills(t *testing.T) {
	home := t.TempDir()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	for _, skill := range []string{"ha-nova", "read"} {
		dir := filepath.Join(sourceRoot, "skills", skill)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir source skill %s: %v", skill, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("name: "+skill+"\n"), 0o644); err != nil {
			t.Fatalf("write source skill %s: %v", skill, err)
		}
	}

	retired := filepath.Join(antigravitySkillsRoot(home), "ha-nova-guide", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(retired), 0o755); err != nil {
		t.Fatalf("mkdir retired skill: %v", err)
	}
	if err := os.WriteFile(retired, []byte("name: ha-nova-guide\n"), 0o644); err != nil {
		t.Fatalf("write retired skill: %v", err)
	}

	if err := installAntigravityClient(home, sourceRoot); err != nil {
		t.Fatalf("installAntigravityClient() error: %v", err)
	}

	if _, err := os.Stat(retired); !os.IsNotExist(err) {
		t.Fatalf("expected retired Antigravity skill to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(antigravitySkillsRoot(home), "ha-nova", "SKILL.md")); err != nil {
		t.Fatalf("expected current Antigravity root skill to remain: %v", err)
	}
}

func TestInstallAntigravityClientRemovesLegacyCodexGeminiFlatSkillsOnly(t *testing.T) {
	home := t.TempDir()
	sourceRoot := filepath.Join(t.TempDir(), "source")
	for _, skill := range []string{"ha-nova", "read"} {
		dir := filepath.Join(sourceRoot, "skills", skill)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir source skill %s: %v", skill, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("name: "+skill+"\n"), 0o644); err != nil {
			t.Fatalf("write source skill %s: %v", skill, err)
		}
	}

	for _, skill := range []string{"ha-nova-read", "ha-nova-guide", "ha-nova-lab"} {
		dir := filepath.Join(legacyCodexGeminiSkillsRoot(home), skill)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir legacy Codex-scope Gemini skill %s: %v", skill, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("name: "+skill+"\n"), 0o644); err != nil {
			t.Fatalf("write legacy Codex-scope Gemini skill %s: %v", skill, err)
		}
	}
	codexRootSkill := filepath.Join(legacyCodexGeminiSkillsRoot(home), "ha-nova", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(codexRootSkill), 0o755); err != nil {
		t.Fatalf("mkdir Codex root skill: %v", err)
	}
	if err := os.WriteFile(codexRootSkill, []byte("name: ha-nova\n"), 0o644); err != nil {
		t.Fatalf("write Codex root skill: %v", err)
	}

	if err := installAntigravityClient(home, sourceRoot); err != nil {
		t.Fatalf("installAntigravityClient() error: %v", err)
	}

	for _, removed := range []string{"ha-nova-read", "ha-nova-guide"} {
		if _, err := os.Stat(filepath.Join(legacyCodexGeminiSkillsRoot(home), removed, "SKILL.md")); !os.IsNotExist(err) {
			t.Fatalf("expected legacy Codex-scope Gemini flat skill %s to be removed, stat err=%v", removed, err)
		}
	}
	for _, kept := range []string{"ha-nova", "ha-nova-lab"} {
		if _, err := os.Stat(filepath.Join(legacyCodexGeminiSkillsRoot(home), kept, "SKILL.md")); err != nil {
			t.Fatalf("expected %s to remain: %v", kept, err)
		}
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

func TestRemoveInstalledClientsForAntigravityRemovesManagedCurrentAndLegacySkillsOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	paths := runtimePaths{Home: home}

	for _, root := range []string{antigravitySkillsRoot(home), legacyGeminiSkillsRoot(home), legacyCodexGeminiSkillsRoot(home)} {
		for _, skill := range []string{"ha-nova", "ha-nova-read", "ha-nova-guide"} {
			dir := filepath.Join(root, skill)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("name: "+skill), 0o644); err != nil {
				t.Fatalf("write %s: %v", skill, err)
			}
		}
	}

	userOwned := []string{
		filepath.Join(antigravitySkillsRoot(home), "ha-novation"),
		filepath.Join(legacyGeminiSkillsRoot(home), "ha-nova-lab"),
		filepath.Join(legacyCodexGeminiSkillsRoot(home), "ha-nova-lab"),
	}
	for _, dir := range userOwned {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir user-owned skill %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("name: "+filepath.Base(dir)), 0o644); err != nil {
			t.Fatalf("write user-owned skill %s: %v", dir, err)
		}
	}

	err := removeInstalledClientsWithReport(paths, installState{InstalledClients: []string{"antigravity"}}, &uninstallReport{})
	if err != nil {
		t.Fatalf("removeInstalledClientsWithReport() error: %v", err)
	}

	for _, removed := range []string{
		filepath.Join(antigravitySkillsRoot(home), "ha-nova", "SKILL.md"),
		filepath.Join(antigravitySkillsRoot(home), "ha-nova-read", "SKILL.md"),
		filepath.Join(antigravitySkillsRoot(home), "ha-nova-guide", "SKILL.md"),
		filepath.Join(legacyGeminiSkillsRoot(home), "ha-nova", "SKILL.md"),
		filepath.Join(legacyGeminiSkillsRoot(home), "ha-nova-read", "SKILL.md"),
		filepath.Join(legacyGeminiSkillsRoot(home), "ha-nova-guide", "SKILL.md"),
		filepath.Join(legacyCodexGeminiSkillsRoot(home), "ha-nova-read", "SKILL.md"),
		filepath.Join(legacyCodexGeminiSkillsRoot(home), "ha-nova-guide", "SKILL.md"),
	} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Fatalf("expected managed Antigravity skill to be removed: %s", removed)
		}
	}
	if _, err := os.Stat(filepath.Join(legacyCodexGeminiSkillsRoot(home), "ha-nova", "SKILL.md")); err != nil {
		t.Fatalf("expected Codex root skill to remain after Antigravity uninstall: %v", err)
	}
	for _, dir := range userOwned {
		if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
			t.Fatalf("expected user-owned Antigravity skill to remain: %v", err)
		}
	}
}
