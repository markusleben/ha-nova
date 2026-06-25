import { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, symlinkSync, writeFileSync } from "node:fs";
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

  it("refreshes Antigravity when only the legacy Gemini marker exists", { timeout: 60000 }, () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-antigravity-legacy-"));
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
    expect(result.stdout).toContain("Google Antigravity CLI: refreshed via install-local-skills.sh antigravity");
    expect(existsSync(join(home, ".gemini", "config", "skills", "ha-nova", "SKILL.md"))).toBe(true);
    expect(existsSync(join(home, ".gemini", "skills", "ha-nova-read", "SKILL.md"))).toBe(false);
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

  it("copies the current client registry next to the dev-synced CLI runtime", { timeout: 90000 }, () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-dev-sync-client-registry-"));
    const binDir = createMockBinaries();
    const runtimeDir = join(home, ".local", "share", "ha-nova");
    const pathBinDir = join(home, ".local", "bin");

    mkdirSync(join(runtimeDir, "clients"), { recursive: true });
    mkdirSync(pathBinDir, { recursive: true });
    const initialBuild = spawnSync("go", ["build", "-o", join(runtimeDir, "ha-nova"), "."], {
      cwd: join(REPO_ROOT, "cli"),
      encoding: "utf8",
      timeout: 90000,
    });
    expect(initialBuild.status).toBe(0);
    symlinkSync(join(runtimeDir, "ha-nova"), join(pathBinDir, "ha-nova"));
    writeFileSync(
      join(runtimeDir, "clients", "registry.json"),
      '{"clients":[{"id":"gemini","label":"Gemini CLI","adapter_kind":"skill_tree","supported_os":["macos"]}]}\n',
    );

    const result = spawnSync("bash", ["scripts/dev-sync.sh"], {
      cwd: REPO_ROOT,
      encoding: "utf8",
      timeout: 90000,
      env: mockEnv(home, binDir, { PATH: `${pathBinDir}:${binDir}:${process.env.PATH ?? ""}` }),
    });

    expect(result.status).toBe(0);
    expect(result.stdout).toContain("CLI: built local Go source");

    const syncedRegistry = readFileSync(join(runtimeDir, "clients", "registry.json"), "utf8");
    const syncedBundle = readFileSync(join(runtimeDir, "bundle.json"), "utf8");
    expect(syncedRegistry).toContain('"id": "antigravity"');
    expect(syncedRegistry).toContain('"label": "Google Antigravity CLI"');
    expect(syncedRegistry).not.toContain('"id":"gemini"');
    expect(syncedBundle).toContain('"version": "0.7.0"');
    expect(syncedBundle).toContain('"bundle_format_version": 1');
    expect(existsSync(join(runtimeDir, "skills", "calendar", "SKILL.md"))).toBe(true);
    expect(existsSync(join(runtimeDir, "skills", "health", "SKILL.md"))).toBe(true);
    expect(existsSync(join(runtimeDir, "docs", "reference", "skill-architecture.md"))).toBe(true);
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

  it("extracts string-form Claude marketplace sources (not just object form)", () => {
    // A marketplace source can be a plain string path (the Go reader supports it
    // and fixtures use it). When the dev-sync parser only read object-shaped
    // source.path, the rsync was skipped and a Claude restart re-staged stale dev
    // skills over the fresh sync. Exercise the real parser function in isolation.
    const binDir = createMockBinaries();
    const extractAndRun = [
      `eval "$(sed -n '/^claude_marketplace_source_dir() {/,/^}/p' scripts/dev-sync.sh)"`,
      "claude_marketplace_source_dir",
    ].join("\n");
    const sourceDir = (knownMarketplaces: unknown): string => {
      const home = mkdtempSync(join(tmpdir(), "ha-nova-mkt-src-"));
      mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
      writeFileSync(
        join(home, ".claude", "plugins", "known_marketplaces.json"),
        JSON.stringify(knownMarketplaces),
      );
      const r = spawnSync("bash", ["-c", extractAndRun], {
        cwd: REPO_ROOT,
        encoding: "utf8",
        timeout: 20000,
        env: mockEnv(home, binDir),
      });
      expect(r.status).toBe(0);
      return r.stdout.trim();
    };

    // String path source — the regression (object-only parsing returned "").
    expect(sourceDir({ "ha-nova": { source: "/Users/x/.claude/plugins/marketplaces/ha-nova" } })).toBe(
      "/Users/x/.claude/plugins/marketplaces/ha-nova",
    );
    // Object source.path still resolves.
    expect(sourceDir({ "ha-nova": { source: { path: "/Users/x/mkt" } } })).toBe("/Users/x/mkt");
    // "github" is a type marker, not a path → empty (mirrors the Go reader).
    expect(sourceDir({ "ha-nova": { source: "github", repo: "markusleben/ha-nova" } })).toBe("");
    // installLocation fallback when there is no source.
    expect(sourceDir({ "ha-nova": { installLocation: "/Users/x/loc" } })).toBe("/Users/x/loc");
  });
});
