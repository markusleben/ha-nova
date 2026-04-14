import { existsSync, mkdtempSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

import { REPO_ROOT } from "./_helpers.js";

const helperPath = join(REPO_ROOT, "scripts/dev/claude-plugin-state.mjs");

function runHelper(args: string[]) {
  return spawnSync("node", [helperPath, ...args], {
    cwd: REPO_ROOT,
    encoding: "utf8",
  });
}

describe("Claude plugin state helper", () => {
  it("captures a full Claude attachment snapshot from home", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-state-"));
    const installPath = join(home, ".claude", "plugins", "cache", "ha-nova", "ha-nova", "0.4.0");
    mkdirSync(installPath, { recursive: true });
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      JSON.stringify({
        version: 2,
        plugins: {
          "ha-nova@ha-nova": [
            {
              scope: "user",
              installPath,
              version: "0.4.0",
            },
          ],
        },
      }, null, 2) + "\n",
    );
    writeFileSync(
      join(home, ".claude", "plugins", "known_marketplaces.json"),
      JSON.stringify({
        "ha-nova": {
          source: {
            source: "directory",
            path: join(home, ".config", "ha-nova", "claude-marketplace"),
          },
        },
      }, null, 2) + "\n",
    );
    writeFileSync(
      join(home, ".claude", "settings.json"),
      JSON.stringify({
        enabledPlugins: {
          "ha-nova@ha-nova": true,
        },
        extraKnownMarketplaces: {
          "ha-nova": {
            path: join(home, ".config", "ha-nova", "claude-marketplace"),
          },
        },
      }, null, 2) + "\n",
    );

    const result = runHelper(["snapshot-home", home]);
    expect(result.status).toBe(0);

    const snapshot = JSON.parse(result.stdout);
    expect(snapshot.attached).toBe(true);
    expect(snapshot.plugin.recordPresent).toBe(true);
    expect(snapshot.plugin.usableInstallPath).toBe(true);
    expect(snapshot.plugin.installPath).toBe(installPath);
    expect(snapshot.plugin.version).toBe("0.4.0");
    expect(snapshot.marketplace.present).toBe(true);
    expect(snapshot.settings.pluginEnabled).toBe(true);
    expect(snapshot.files.installedPlugins.exists).toBe(true);
    expect(snapshot.files.knownMarketplaces.exists).toBe(true);
    expect(snapshot.files.installedPlugins.sha256.length).toBe(64);
  });

  it("does not treat another plugin record as HA NOVA", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-other-plugin-"));
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      JSON.stringify({
        version: 2,
        plugins: [
          {
            name: "other-plugin",
            installPath: "/tmp/other",
            version: "1.0.0",
          },
        ],
      }, null, 2) + "\n",
    );
    writeFileSync(
      join(home, ".claude", "plugins", "known_marketplaces.json"),
      JSON.stringify({
        "other-marketplace": {
          source: {
            source: "directory",
            path: "/tmp/other-marketplace",
          },
        },
      }, null, 2) + "\n",
    );

    const inspect = runHelper([
      "inspect-installed-plugin",
      join(home, ".claude", "plugins", "installed_plugins.json"),
    ]);
    expect(inspect.status).toBe(0);
    expect(inspect.stdout).toContain("installed=0");

    const snapshotResult = runHelper(["snapshot-home", home]);
    expect(snapshotResult.status).toBe(0);

    const snapshot = JSON.parse(snapshotResult.stdout);
    expect(snapshot.attached).toBe(false);
    expect(snapshot.plugin.recordPresent).toBe(false);
    expect(snapshot.plugin.usableInstallPath).toBe(false);
    expect(snapshot.marketplace.present).toBe(false);
  });

  it("records watch events and latest snapshot", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-watch-"));
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      JSON.stringify({ version: 2, plugins: {} }, null, 2) + "\n",
    );

    const outDir = join(home, ".config", "ha-nova", "claude-drift-audit");
    const eventLog = join(outDir, "events.jsonl");
    const latest = join(outDir, "latest.json");

    const result = runHelper([
      "write-watch-event",
      home,
      join(home, ".claude", "plugins", "installed_plugins.json"),
      eventLog,
      latest,
    ]);
    expect(result.status).toBe(0);
    expect(existsSync(eventLog)).toBe(true);
    expect(existsSync(latest)).toBe(true);

    const eventLine = readFileSync(eventLog, "utf8").trim();
    const event = JSON.parse(eventLine);
    const latestSnapshot = JSON.parse(readFileSync(latest, "utf8"));

    expect(event.triggerPath).toBe(join(home, ".claude", "plugins", "installed_plugins.json"));
    expect(event.context).toBe("");
    expect(event.changes.initialized).toEqual({ from: null, to: true });
    expect(event.snapshot.home).toBe(home);
    expect(latestSnapshot.home).toBe(home);
    expect(latestSnapshot.plugin.recordPresent).toBe(false);
    expect(latestSnapshot.plugin.usableInstallPath).toBe(false);
    expect(latestSnapshot.files.installedPlugins.exists).toBe(true);
  });

  it("does not call a blank install path attached", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-blank-install-"));
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      JSON.stringify({
        version: 2,
        plugins: {
          "ha-nova@ha-nova": [
            {
              scope: "user",
              installPath: "",
              version: "0.4.0",
            },
          ],
        },
      }, null, 2) + "\n",
    );
    writeFileSync(
      join(home, ".claude", "plugins", "known_marketplaces.json"),
      JSON.stringify({
        "ha-nova": {
          source: {
            source: "directory",
            path: join(home, ".config", "ha-nova", "claude-marketplace"),
          },
        },
      }, null, 2) + "\n",
    );

    const result = runHelper(["snapshot-home", home]);
    expect(result.status).toBe(0);

    const snapshot = JSON.parse(result.stdout);
    expect(snapshot.plugin.recordPresent).toBe(true);
    expect(snapshot.plugin.usableInstallPath).toBe(false);
    expect(snapshot.plugin.installPath).toBe("");
    expect(snapshot.attached).toBe(false);
  });

  it("treats a legacy string entry as installed but not attached", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-legacy-string-"));
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      JSON.stringify({
        version: 2,
        plugins: ["ha-nova@ha-nova"],
      }, null, 2) + "\n",
    );

    const inspect = runHelper([
      "inspect-installed-plugin",
      join(home, ".claude", "plugins", "installed_plugins.json"),
    ]);
    expect(inspect.status).toBe(0);
    expect(inspect.stdout).toContain("installed=1");

    const snapshotResult = runHelper(["snapshot-home", home]);
    expect(snapshotResult.status).toBe(0);

    const snapshot = JSON.parse(snapshotResult.stdout);
    expect(snapshot.plugin.recordPresent).toBe(true);
    expect(snapshot.plugin.usableInstallPath).toBe(false);
    expect(snapshot.attached).toBe(false);
  });

  it("treats BOM-prefixed JSON as parseable", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-bom-"));
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      `\uFEFF${JSON.stringify({ version: 2, plugins: {} }, null, 2)}\n`,
    );

    const result = runHelper(["snapshot-home", home]);
    expect(result.status).toBe(0);

    const snapshot = JSON.parse(result.stdout);
    expect(snapshot.files.installedPlugins.parseable).toBe(true);
    expect(snapshot.files.installedPlugins.error).toBe("");
  });

  it("treats BOM-prefixed known marketplaces json as parseable", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-known-bom-"));
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "known_marketplaces.json"),
      `\uFEFF${JSON.stringify({
        "ha-nova": {
          source: {
            source: "directory",
            path: join(home, ".config", "ha-nova", "claude-marketplace"),
          },
        },
      }, null, 2)}\n`,
    );

    const result = runHelper(["snapshot-home", home]);
    expect(result.status).toBe(0);

    const snapshot = JSON.parse(result.stdout);
    expect(snapshot.marketplace.present).toBe(true);
    expect(snapshot.files.knownMarketplaces.parseable).toBe(true);
    expect(snapshot.files.knownMarketplaces.error).toBe("");
  });

  it("preserves pinned GitHub refs in marketplace source output", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-known-pinned-"));
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "known_marketplaces.json"),
      JSON.stringify({
        "ha-nova": {
          source: {
            source: "github",
            repo: "markusleben/ha-nova",
            ref: "v0.2.2",
          },
        },
      }, null, 2) + "\n",
    );

    const result = runHelper(["snapshot-home", home]);
    expect(result.status).toBe(0);

    const snapshot = JSON.parse(result.stdout);
    expect(snapshot.marketplace.present).toBe(true);
    expect(snapshot.marketplace.source).toBe("markusleben/ha-nova#v0.2.2");
  });

  it("keeps snapshot output when known marketplaces json is invalid", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-invalid-known-"));
    mkdirSync(join(home, ".claude", "plugins"), { recursive: true });
    writeFileSync(
      join(home, ".claude", "plugins", "installed_plugins.json"),
      JSON.stringify({ version: 2, plugins: {} }, null, 2) + "\n",
    );
    writeFileSync(
      join(home, ".claude", "plugins", "known_marketplaces.json"),
      "{\n",
    );

    const result = runHelper(["snapshot-home", home]);
    expect(result.status).toBe(0);

    const snapshot = JSON.parse(result.stdout);
    expect(snapshot.marketplace.present).toBe(false);
    expect(snapshot.marketplace.source).toBe("");
    expect(snapshot.files.knownMarketplaces.parseable).toBe(false);
    expect(snapshot.files.knownMarketplaces.error.length).toBeGreaterThan(0);
  });

  it("keeps snapshot output when a tracked registry path is a directory", () => {
    const home = mkdtempSync(join(tmpdir(), "ha-nova-claude-dir-known-"));
    mkdirSync(join(home, ".claude", "plugins", "known_marketplaces.json"), { recursive: true });

    const result = runHelper(["snapshot-home", home]);
    expect(result.status).toBe(0);

    const snapshot = JSON.parse(result.stdout);
    expect(snapshot.marketplace.present).toBe(false);
    expect(snapshot.files.knownMarketplaces.exists).toBe(true);
    expect(snapshot.files.knownMarketplaces.parseable).toBe(false);
    expect(snapshot.files.knownMarketplaces.error.length).toBeGreaterThan(0);
  });
});
