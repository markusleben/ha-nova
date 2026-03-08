/**
 * S-4: Client-specific skill installation (4 clients)
 * S-5: Multi-client ("all")
 */
import { mkdirSync, mkdtempSync, readFileSync, readlinkSync, statSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { REPO_ROOT } from "./_helpers.js";

const SUB_SKILLS = ["write", "read", "helper", "entity-discovery", "onboarding", "service-call", "review", "guide"];

function installSkills(client: string): { home: string; result: ReturnType<typeof spawnSync> } {
  const home = mkdtempSync(join(tmpdir(), `ha-nova-skill-${client}-`));
  const result = spawnSync(
    "bash",
    ["scripts/onboarding/install-local-skills.sh", client],
    {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: { ...process.env, HOME: home },
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
    for (const sub of SUB_SKILLS) {
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
    const ctx = readFileSync(join(home, ".agents/skills/ha-nova/SKILL.md"), "utf8");
    expect(ctx).toContain("name: ha-nova");

    // Sub-skills as separate flat directories
    for (const sub of SUB_SKILLS) {
      const content = readFileSync(
        join(home, ".agents/skills", `ha-nova-${sub}`, "SKILL.md"),
        "utf8",
      );
      expect(content).toContain(`name: ${sub}`);
      // docs/reference/ paths should be resolved to absolute
      if (content.includes("docs/reference/")) {
        expect(content).toContain(REPO_ROOT);
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
    for (const sub of SUB_SKILLS) {
      expect(() =>
        statSync(join(home, ".agents/skills", `ha-nova-${sub}`, "SKILL.md")),
      ).not.toThrow();
    }
  });

  it("relay CLI is installed to config dir", () => {
    const { home, result } = installSkills("all");
    expect(result.status).toBe(0);

    const relayCli = join(home, ".config/ha-nova/relay");
    const stats = statSync(relayCli);
    // eslint-disable-next-line no-bitwise
    expect((stats.mode & 0o111) !== 0).toBe(true);
  });
});
