import { existsSync, mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
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

describe("dev-sync behavior", () => {
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
    expect(result.stdout).toContain("Claude Code: no installed_plugins.json found — skipped");
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
});
