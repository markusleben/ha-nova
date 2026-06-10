import { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

import { createMockBinaries, mockEnv, REPO_ROOT } from "./_helpers.js";

function writeState(home: string, installedClients: string[]): void {
  mkdirSync(join(home, ".config", "ha-nova"), { recursive: true });
  writeFileSync(
    join(home, ".config", "ha-nova", "state.json"),
    JSON.stringify({
      schema_version: 1,
      version: "0.3.0",
      install_source: "dev",
      installed_clients: installedClients,
    }),
  );
}

function writeClaudePluginRecord(home: string, recordBody: string): void {
  mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
  writeFileSync(
    join(home, ".claude", "plugins", "installed_plugins.json"),
    `{\n  "ha-nova@ha-nova": ${recordBody}\n}\n`,
  );
}

function readClaudeLog(logFile: string): string {
  if (!existsSync(logFile)) {
    return "";
  }
  return readFileSync(logFile, "utf8");
}

function stageCopiedFileClient(home: string, clientRoot: string): void {
  mkdirSync(join(home, clientRoot, "ha-nova", "ha-nova"), { recursive: true });
  writeFileSync(join(home, clientRoot, "ha-nova", "ha-nova", "SKILL.md"), "name: ha-nova\n");
}

describe("dev-sync behavior", () => {
  it("refreshes copied Codex installs instead of skipping them", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-codex-copy-"));
    const binDir = createMockBinaries();

    stageCopiedFileClient(home, ".agents/skills");

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir, { HA_NOVA_FORCE_COPY_INSTALL: "1" }),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Codex: refreshed via install-local-skills.sh codex");
    expect(lstatSync(join(home, ".agents/skills", "ha-nova")).isSymbolicLink()).toBe(false);
    expect(existsSync(join(home, ".agents/skills", "ha-nova", "ha-nova", "SKILL.md"))).toBe(true);
  });

  it("refreshes non-Claude clients without Node.js when Claude has no ha-nova records", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-codex-no-node-"));
    const binDir = createMockBinaries();

    stageCopiedFileClient(home, ".agents/skills");
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      '{"plugins":["other-plugin"]}',
    );
    writeFileSync(join(binDir, "node"), "#!/usr/bin/env bash\nexit 127\n", { mode: 0o755 });

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir, { HA_NOVA_FORCE_COPY_INSTALL: "1" }),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Codex: refreshed via install-local-skills.sh codex");
  });

  it("refreshes copied OpenCode installs instead of skipping them", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-opencode-copy-"));
    const binDir = createMockBinaries();

    stageCopiedFileClient(home, ".config/opencode/skills");

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir, { HA_NOVA_FORCE_COPY_INSTALL: "1" }),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("OpenCode: refreshed via install-local-skills.sh opencode");
    expect(lstatSync(join(home, ".config/opencode/skills", "ha-nova")).isSymbolicLink()).toBe(false);
    expect(existsSync(join(home, ".config/opencode/skills", "ha-nova", "ha-nova", "SKILL.md"))).toBe(true);
  });

  it("refreshes Gemini when only the legacy marker exists", { timeout: 60000 }, () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-gemini-legacy-"));
    const binDir = createMockBinaries();

    mkdirSync(join(home, ".agents", "skills", "ha-nova-read"), { recursive: true });
    writeFileSync(join(home, ".agents", "skills", "ha-nova-read", "SKILL.md"), "name: ha-nova-read\n");

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 60000,
      env: mockEnv(home, binDir),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Gemini: refreshed via install-local-skills.sh gemini");
    expect(existsSync(join(home, ".gemini", "skills", "ha-nova", "SKILL.md"))).toBe(true);
  });

  it("refreshes shared tools when no file clients are installed", { timeout: 90000 }, () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-shared-tools-"));
    const binDir = createMockBinaries();
    const configDir = join(home, ".config", "ha-nova");

    mkdirSync(configDir, { recursive: true });
    writeFileSync(join(configDir, "relay"), "#!/usr/bin/env bash\n", { mode: 0o755 });

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 60000,
      env: mockEnv(home, binDir, { PATH: `${binDir}:/usr/bin:/bin` }),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Shared tools refreshed");
    expect(existsSync(join(configDir, "version-check"))).toBe(true);
    expect(existsSync(join(configDir, "version.json"))).toBe(true);
  });

  it("fails loudly when repo version.json is missing", () => {
    const fakeRoot = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-fixture-"));
    const binDir = createMockBinaries();

    mkdirSync(join(fakeRoot, "scripts", "onboarding", "lib"), { recursive: true });
    mkdirSync(join(fakeRoot, "scripts", "onboarding", "bin"), { recursive: true });
    mkdirSync(join(fakeRoot, "scripts", "dev"), { recursive: true });
    mkdirSync(join(fakeRoot, "skills", "ha-nova"), { recursive: true });

    writeFileSync(join(fakeRoot, "scripts", "dev-sync.sh"), readFileSync(join(REPO_ROOT, "scripts", "dev-sync.sh"), "utf8"), { mode: 0o755 });
    writeFileSync(join(fakeRoot, "scripts", "onboarding", "lib", "install-local-skills-common.sh"), readFileSync(join(REPO_ROOT, "scripts", "onboarding", "lib", "install-local-skills-common.sh"), "utf8"), { mode: 0o755 });
    writeFileSync(join(fakeRoot, "scripts", "onboarding", "lib", "install-local-skills-claude.sh"), readFileSync(join(REPO_ROOT, "scripts", "onboarding", "lib", "install-local-skills-claude.sh"), "utf8"), { mode: 0o755 });
    writeFileSync(join(fakeRoot, "scripts", "onboarding", "bin", "ha-nova"), "#!/usr/bin/env bash\nexit 0\n", { mode: 0o755 });

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: fakeRoot,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(fakeRoot, binDir),
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr + result.stdout).toContain("missing repo version file");
  });

  it("reinstalls Claude when state.json still expects it but plugin records are gone", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-claude-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });

    writeState(home, ["claude"]);

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Claude Code: configured in state.json but plugin record is missing");

    const claudeLog = readClaudeLog(claudeLogFile);
    expect(claudeLog).toContain("plugin marketplace remove ha-nova");
    expect(claudeLog).toContain("plugin marketplace add");
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
  });

  it("reinstalls Claude from stale state without Node.js when no ha-nova records exist yet", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-claude-no-node-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    writeFileSync(join(binDir, "node"), "#!/usr/bin/env bash\nexit 127\n", { mode: 0o755 });

    writeState(home, ["claude"]);

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: {
        ...mockEnv(home, binDir),
        PATH: `${binDir}:${process.env.PATH ?? ""}`,
      },
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Claude Code: configured in state.json but plugin record is missing");

    const claudeLog = readClaudeLog(claudeLogFile);
    expect(claudeLog).toContain("plugin marketplace add");
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
  });

  it("reinstalls Claude when state.json still expects it but plugin installPath is missing", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-claude-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });

    writeState(home, ["claude"]);
    writeClaudePluginRecord(home, '{\n    "version": "0.3.0"\n  }');

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Claude Code: configured in state.json but plugin installPath is missing");

    const claudeLog = readClaudeLog(claudeLogFile);
    expect(claudeLog).toContain("plugin marketplace remove ha-nova");
    expect(claudeLog).toContain("plugin marketplace add");
    expect(claudeLog).toContain("plugin install ha-nova@ha-nova");
  });

  it("does not reinstall Claude when state.json does not expect it", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-claude-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });

    writeState(home, []);

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Claude Code: ha-nova plugin not installed — skipped");
    expect(readClaudeLog(claudeLogFile)).not.toContain("plugin install ha-nova@ha-nova");
  });

  it("repairs a stale Claude installPath without reinstalling the plugin", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-claude-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    const stalePath = join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.2.0");
    const actualPath = join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.3.0");

    mkdirSync(actualPath, { recursive: true });
    writeClaudePluginRecord(
      home,
      `{\n    "installPath": "${stalePath}",\n    "version": "0.2.0"\n  }`,
    );

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: mockEnv(home, binDir),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain(`Claude Code: installPath stale (${stalePath}), found ${actualPath}`);
    expect(readClaudeLog(claudeLogFile)).not.toContain("plugin install ha-nova@ha-nova");
    expect(readFileSync(join(home, ".claude", "plugins", "installed_plugins.json"), "utf8")).toContain(
      `"installPath": "${actualPath}"`,
    );
  });

  it("fails loudly when Claude sync needs the local plugin state helper but Node.js is missing", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-claude-node-missing-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    writeFileSync(join(binDir, "node"), "#!/usr/bin/env bash\nexit 127\n", { mode: 0o755 });

    writeState(home, ["claude"]);
    writeClaudePluginRecord(home, '{\n    "installPath": "/tmp/ha-nova/0.3.1",\n    "version": "0.3.1"\n  }');

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: {
        ...mockEnv(home, binDir),
        PATH: `${binDir}:${process.env.PATH ?? ""}`,
      },
    });

    expect(result.status).not.toBe(0);
    expect(result.stderr + result.stdout).toContain("[dev:sync] ERROR: Node.js not found in PATH");
  });

  it("skips stale Claude marketplace-only metadata without requiring Node.js", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-claude-marketplace-only-"));
    const claudeLogFile = join(home, "claude.log");
    const binDir = createMockBinaries({ claudeLogFile });
    writeFileSync(join(binDir, "node"), "#!/usr/bin/env bash\nexit 127\n", { mode: 0o755 });
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "known_marketplaces.json"),
      '{"ha-nova":{"source":"https://github.com/markusleben/ha-nova"}}',
    );

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 20000,
      env: {
        ...mockEnv(home, binDir),
        PATH: `${binDir}:${process.env.PATH ?? ""}`,
      },
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("Claude Code: ha-nova plugin not installed");
  });
});
