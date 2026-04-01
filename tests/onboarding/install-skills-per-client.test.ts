/**
 * S-4: Client-specific skill installation (4 clients)
 * S-5: Multi-client ("all")
 */
import { existsSync, mkdtempSync, mkdirSync, readFileSync, readdirSync, readlinkSync, statSync, writeFileSync } from "node:fs";
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

function installSkills(
  client: string,
  extraEnv: Record<string, string> = {},
  timeout = 120000,
): { home: string; result: ReturnType<typeof spawnSync> } {
  const home = mkdtempSync(join(tmpdir(), `ha-nova-skill-${client}-`));
  const claudeLogFile = join(home, "claude.log");
  const binDir = createMockBinaries({ claudeLogFile });
  const result = spawnSync(
    "bash",
    ["scripts/onboarding/install-local-skills.sh", client],
    {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout,
      env: mockEnv(home, binDir, extraEnv),
    },
  );
  return { home, result };
}

describe("S-4: client-specific skill installation", () => {
  it("cleans legacy flat skills during codex install", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-codex-legacy-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });

    mkdirSync(join(home, ".agents", "skills", "ha-nova-read"), { recursive: true });
    writeFileSync(join(home, ".agents", "skills", "ha-nova-read", "SKILL.md"), "legacy\n");
    mkdirSync(join(home, ".agents", "skills", "ha-nova"), { recursive: true });
    writeFileSync(join(home, ".agents", "skills", "ha-nova", "stale.txt"), "stale\n");

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "codex"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 60000,
        env: mockEnv(home, binDir),
      },
    );

    expect(result.status).toBe(0);
    expect(existsSync(join(home, ".agents", "skills", "ha-nova-read"))).toBe(false);
    expect(readlinkSync(join(home, ".agents", "skills", "ha-nova"))).toBe(join(REPO_ROOT, "skills"));
  });

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

  it("installs gemini skills as flat copies", { timeout: 120000 }, () => {
    const { home, result } = installSkills("gemini");
    expect(result.status).toBe(0);

    // Context skill
    const ctx = readFileSync(join(home, ".gemini/skills/ha-nova/SKILL.md"), "utf8");
    expect(ctx).toContain("name: ha-nova");
    expect(ctx).toContain("ha-nova:ha-nova-entity-discovery");

    // Sub-skills as separate flat directories (ha-nova- prefix for Gemini)
    for (const src of SOURCE_SUB_SKILLS) {
      const geminiDir = `ha-nova-${src}`;
      const content = readFileSync(
        join(home, ".gemini/skills", geminiDir, "SKILL.md"),
        "utf8",
      );
      expect(content).toContain(`name: ha-nova-${src}`);
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

    const manifest = JSON.parse(readFileSync(join(REPO_ROOT, ".claude-plugin/plugin.json"), "utf8"));
    expect(manifest).toHaveProperty("name");

    const marketplaceRoot = join(home, ".config/ha-nova/claude-marketplace");
    const marketplace = JSON.parse(
      readFileSync(join(marketplaceRoot, ".claude-plugin/marketplace.json"), "utf8"),
    );
    expect(marketplace.plugins[0].source).toBe("./ha-nova");
    expect(JSON.stringify(marketplace)).not.toContain("github.com/markusleben/ha-nova.git");
    expect(existsSync(join(marketplaceRoot, "ha-nova"))).toBe(true);

    const claudeLog = readFileSync(join(home, "claude.log"), "utf8");
    expect(claudeLog).toContain("plugin marketplace remove ha-nova");
    expect(claudeLog).toContain(`plugin marketplace add ${marketplaceRoot}`);
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
  });

  it("fails loudly when Claude CLI is missing", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-claude-missing-"));
    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "claude"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: {
          ...mockEnv(home, "", {}),
          PATH: "/usr/bin:/bin",
        },
      },
    );

    expect(result.status).not.toBe(0);
    expect(result.stderr + result.stdout).toContain("[claude] Claude CLI not found in PATH");
  });

  it("installs Claude without Node.js when no prior local Claude state exists", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-claude-node-optional-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    writeFileSync(join(binDir, "node"), "#!/usr/bin/env bash\nexit 127\n", { mode: 0o755 });

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "claude"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: {
          ...mockEnv(home, binDir),
          PATH: `${binDir}:${process.env.PATH ?? ""}`,
        },
      },
    );

    expect(result.status).toBe(0);
    const claudeLog = readFileSync(claudeLogFile, "utf8");
    expect(claudeLog).toContain("plugin marketplace add");
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
  });

  it("installs Claude without Node.js when unrelated Claude plugin metadata exists", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-claude-node-unrelated-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    writeFileSync(join(binDir, "node"), "#!/usr/bin/env bash\nexit 127\n", { mode: 0o755 });
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      '{"plugins":["other-plugin"]}',
    );
    writeFileSync(
      join(home, ".claude", "plugins", "known_marketplaces.json"),
      '{"other-plugin":{"name":"other-plugin","source":{"url":"https://example.invalid/other"}}}',
    );

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "claude"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: {
          ...mockEnv(home, binDir),
          PATH: `${binDir}:${process.env.PATH ?? ""}`,
        },
      },
    );

    expect(result.status).toBe(0);
    const claudeLog = readFileSync(claudeLogFile, "utf8");
    expect(claudeLog).toContain("plugin marketplace add");
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
  });

  it("fails loudly when Node.js is missing for the Claude helper path", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-claude-node-missing-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    writeFileSync(join(binDir, "node"), "#!/usr/bin/env bash\nexit 127\n", { mode: 0o755 });
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      '{"plugins":["ha-nova@ha-nova"]}',
    );

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "claude"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: {
          ...mockEnv(home, binDir),
          PATH: `${binDir}:${process.env.PATH ?? ""}`,
        },
      },
    );

    expect(result.status).not.toBe(0);
    expect(result.stderr + result.stdout).toContain("[claude] Node.js not found in PATH");
  });

  it("fails loudly when Claude install cannot restore a previous plugin without a marketplace source", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-claude-missing-marketplace-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      JSON.stringify({
        "ha-nova@ha-nova": {
          installPath: "/tmp/ha-nova/0.3.1",
          version: "0.3.1",
        },
      }),
    );
    writeFileSync(
      join(binDir, "claude"),
      `#!/usr/bin/env bash
printf '%s\\n' "$*" >> "${claudeLogFile}"
if [[ "$1" == "plugin" && "$2" == "install" ]]; then
  exit 1
fi
exit 0
`,
      { mode: 0o755 },
    );

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "claude"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      },
    );

    expect(result.status).not.toBe(0);
    expect(result.stderr + result.stdout).toContain("previous Claude state could not be restored automatically");
    expect(readFileSync(claudeLogFile, "utf8")).toContain("plugin install ha-nova@ha-nova");
  });

  it("fails loudly when repo version.json is missing", () => {
    const fakeRoot = mkdtempSync(join(tmpdir(), "ha-nova-skill-fixture-"));
    const binDir = createMockBinaries();

    mkdirSync(join(fakeRoot, "scripts", "onboarding", "lib"), { recursive: true });
    mkdirSync(join(fakeRoot, "scripts", "onboarding", "bin"), { recursive: true });
    mkdirSync(join(fakeRoot, "skills", "ha-nova"), { recursive: true });

    for (const rel of [
      "scripts/onboarding/install-local-skills.sh",
      "scripts/onboarding/lib/install-local-skills-common.sh",
      "scripts/onboarding/lib/install-local-skills-claude.sh",
      "scripts/onboarding/lib/install-local-skills-gemini.sh",
      "scripts/onboarding/lib/install-local-skills-repo-dev.sh",
    ]) {
      writeFileSync(join(fakeRoot, rel), readFileSync(join(REPO_ROOT, rel), "utf8"), { mode: 0o755 });
    }
    writeFileSync(join(fakeRoot, "scripts", "onboarding", "bin", "ha-nova"), "#!/usr/bin/env bash\nexit 0\n", { mode: 0o755 });

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "codex"],
      {
        cwd: fakeRoot,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(fakeRoot, binDir),
      },
    );

    expect(result.status).not.toBe(0);
    expect(result.stderr + result.stdout).toContain("Missing repo version file");
  });

  it("reinstalls an existing Claude plugin for local repo validation", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-claude-update-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      '{"plugins":["ha-nova@ha-nova"]}',
    );

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "claude"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      },
    );
    expect(result.status).toBe(0);

    const claudeLog = readFileSync(claudeLogFile, "utf8");
    expect(claudeLog).toContain("plugin marketplace remove ha-nova");
    expect(claudeLog).toContain("plugin marketplace add");
    expect(claudeLog).toContain("plugin remove ha-nova@ha-nova");
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
    expect(claudeLog).not.toContain("plugin update ha-nova@ha-nova");
  });

  it("reinstalls Claude locally instead of reusing a stale cached plugin payload", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-claude-local-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    mkdirSync(join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.1.12"), { recursive: true });
    writeFileSync(join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.1.12", "stale.txt"), "stale");
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      '{"plugins":["ha-nova@ha-nova"]}',
    );

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "claude"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      },
    );
    expect(result.status).toBe(0);

    const claudeLog = readFileSync(claudeLogFile, "utf8");
    expect(claudeLog).toContain("plugin remove ha-nova@ha-nova");
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
    expect(claudeLog).not.toContain("plugin update ha-nova@ha-nova");
    expect(existsSync(join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.1.12"))).toBe(false);
  });

  it("reinstalls Claude locally when the current cache layout lives directly under cache/ha-nova", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-skill-claude-current-cache-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    mkdirSync(join(home, ".claude", "plugins", "cache", "ha-nova", "skills"), { recursive: true });
    writeFileSync(join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova"), "binary");
    writeFileSync(join(home, ".claude", "plugins", "cache", "ha-nova", "version.json"), '{"version":"0.1.12"}');
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      '{"plugins":["ha-nova@ha-nova"]}',
    );

    const result = spawnSync(
      "bash",
      ["scripts/onboarding/install-local-skills.sh", "claude"],
      {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      },
    );
    expect(result.status).toBe(0);

    const claudeLog = readFileSync(claudeLogFile, "utf8");
    expect(claudeLog).toContain("plugin remove ha-nova@ha-nova");
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
    expect(claudeLog).not.toContain("plugin update ha-nova@ha-nova");
    expect(existsSync(join(home, ".claude", "plugins", "cache", "ha-nova"))).toBe(false);
  });
});

describe("S-5: multi-client 'all' installation", () => {
  it("installs for all clients in one pass", { timeout: 120000 }, () => {
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

  it("relay CLI is installed to config dir", { timeout: 120000 }, () => {
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
