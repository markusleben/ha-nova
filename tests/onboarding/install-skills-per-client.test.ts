/**
 * S-4: Client-specific skill installation (4 clients)
 * S-5: Multi-client ("all")
 */
import { mkdtempSync, readFileSync, readdirSync, readlinkSync, statSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { createMockBinaries, mockEnv, REPO_ROOT } from "./_helpers.js";

/** Source directory names under skills/ (short, no prefix) */
const SOURCE_SUB_SKILLS = readdirSync(join(REPO_ROOT, "skills"), { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && entry.name !== "ha-nova")
  .map((entry) => entry.name)
  .sort();

/** Gemini install directory names under ~/.gemini/skills/ (ha-nova- prefix) */
const GEMINI_SUB_SKILLS = SOURCE_SUB_SKILLS.map((s) => `ha-nova-${s}`);

const REWRITTEN_REPO_REF = /`(?:\/|[A-Za-z]:[\\/])[^`\n]*(?:\/skills\/|\/docs\/reference\/)[^`\n]*`/;

function expectRepoRefsRewritten(content: string): void {
  if (content.includes("/skills/") || content.includes("/docs/reference/")) {
    expect(content).toMatch(REWRITTEN_REPO_REF);
  }
}

function installSkills(client: string): { home: string; result: ReturnType<typeof spawnSync> } {
  const home = mkdtempSync(join(tmpdir(), `ha-nova-skill-${client}-`));
  const binDir = createMockBinaries();
  const result = spawnSync(
    "bash",
    ["scripts/onboarding/install-local-skills.sh", client],
    {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir),
    },
  );
  return { home, result };
}

describe("S-4: client-specific skill installation", () => {
  it("installs codex skills as symlink", () => {
    const { home, result } = installSkills("codex");
    expect(result.status).toBe(0);

    const codexLink = join(home, ".agents/skills/ha-nova");
    const linkTarget = readlinkSync(codexLink);
    expect(linkTarget).toBe(join(REPO_ROOT, "skills"));

    // All sub-skills readable through symlink
    for (const sub of SOURCE_SUB_SKILLS) {
      const content = readFileSync(join(codexLink, sub, "SKILL.md"), "utf8");
      expect(content).toContain(`name: ${sub}`);
    }
  });

  it("installs opencode skills as symlink", () => {
    const { home, result } = installSkills("opencode");
    expect(result.status).toBe(0);

    const link = join(home, ".config/opencode/skills/ha-nova");
    const linkTarget = readlinkSync(link);
    expect(linkTarget).toBe(join(REPO_ROOT, "skills"));

    // Context skill accessible
    const ctx = readFileSync(join(link, "ha-nova", "SKILL.md"), "utf8");
    expect(ctx).toContain("name: ha-nova");
  });

  it("installs gemini skills as flat copies", () => {
    const { home, result } = installSkills("gemini");
    expect(result.status).toBe(0);

    // Context skill
    const ctx = readFileSync(join(home, ".gemini/skills/ha-nova/SKILL.md"), "utf8");
    expect(ctx).toContain("name: ha-nova");

    // Sub-skills as separate flat directories (ha-nova- prefix for Gemini)
    for (const src of SOURCE_SUB_SKILLS) {
      const geminiDir = `ha-nova-${src}`;
      const content = readFileSync(
        join(home, ".gemini/skills", geminiDir, "SKILL.md"),
        "utf8",
      );
      expect(content).toContain(`name: ${src}`);
      // Cross-skill/docs references should no longer be relative after flat copy.
      expectRepoRefsRewritten(content);

      const companionFiles = readdirSync(join(REPO_ROOT, "skills", src))
        .filter((file) => file.endsWith(".md") && file !== "SKILL.md");

      for (const companion of companionFiles) {
        const companionContent = readFileSync(
          join(home, ".gemini/skills", geminiDir, companion),
          "utf8",
        );
        expect(companionContent.length).toBeGreaterThan(0);
        expectRepoRefsRewritten(companionContent);
      }

      if (src === "review") {
        expect(content).toContain("`checks.md`");
        expect(content).not.toContain("skills/review/checks.md");
        const checks = readFileSync(join(home, ".gemini/skills", "ha-nova-review", "checks.md"), "utf8");
        expect(checks).toContain("H-09 [MEDIUM → HIGH]");
        expect(checks).toContain("Canonical path: `checks.md`");
        expect(checks).not.toContain("skills/review/checks.md");
      }
    }
  });

  it("installs claude skills via plugin system", () => {
    const { home, result } = installSkills("claude");
    expect(result.status).toBe(0);

    // Claude uses plugin CLI — no file-based skill install
    // Plugin manifest must exist in repo
    const manifest = JSON.parse(readFileSync(join(REPO_ROOT, ".claude-plugin/plugin.json"), "utf8"));
    expect(manifest).toHaveProperty("name");
  });
});

describe("S-5: multi-client 'all' installation", () => {
  it("installs for all clients in one pass", () => {
    const { home, result } = installSkills("all");
    expect(result.status).toBe(0);

    // Codex symlink
    expect(() => readlinkSync(join(home, ".agents/skills/ha-nova"))).not.toThrow();

    // OpenCode symlink
    expect(() => readlinkSync(join(home, ".config/opencode/skills/ha-nova"))).not.toThrow();

    // Gemini flat copies
    for (const sub of GEMINI_SUB_SKILLS) {
      expect(() =>
        statSync(join(home, ".gemini/skills", sub, "SKILL.md")),
      ).not.toThrow();
    }

    expect(() =>
      statSync(join(home, ".gemini/skills", "ha-nova-review", "checks.md")),
    ).not.toThrow();
  });

  it("relay CLI is installed to config dir", () => {
    const { home, result } = installSkills("all");
    expect(result.status).toBe(0);

    const relayCli = join(home, ".config/ha-nova/relay");
    const stats = statSync(relayCli);
    // eslint-disable-next-line no-bitwise
    expect((stats.mode & 0o111) !== 0).toBe(true);

    const relayWrapper = readFileSync(relayCli, "utf8");
    expect(relayWrapper).toContain('scripts/onboarding/bin/ha-nova" relay');

    const versionCheckWrapper = readFileSync(join(home, ".config/ha-nova/version-check"), "utf8");
    expect(versionCheckWrapper).toContain('scripts/onboarding/bin/ha-nova" check-update --quiet');
  });
});
