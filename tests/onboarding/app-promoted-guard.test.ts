import { execFileSync, spawnSync } from "node:child_process";
import { copyFileSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

import { afterEach, beforeEach, describe, expect, it } from "vitest";

const scripts = ["verify-app-promoted.sh", "app-image-lib.sh"];

describe("verify-app-promoted.sh (final tags need an App bump after App source changes)", () => {
  let repo: string;

  const git = (...args: string[]) =>
    execFileSync("git", ["-C", repo, ...args], { encoding: "utf8" }).trim();
  const write = (path: string, content: string) => {
    mkdirSync(dirname(join(repo, path)), { recursive: true });
    writeFileSync(join(repo, path), content);
  };
  const commit = (message: string, files: Record<string, string>) => {
    for (const [path, content] of Object.entries(files)) write(path, content);
    git("add", "-A");
    git("commit", "-q", "-m", message);
  };
  const appVersion = (version: string) => ({ "nova/config.yaml": `name: NOVA Relay\nversion: "${version}"\n` });
  const guard = (tag: string, rev = "HEAD") =>
    spawnSync("bash", [join(repo, "scripts/release/verify-app-promoted.sh"), tag, rev], { encoding: "utf8" });

  beforeEach(() => {
    repo = mkdtempSync(join(tmpdir(), "ha-nova-app-promoted-"));
    git("init", "-q");
    git("config", "user.email", "test@example.invalid");
    git("config", "user.name", "test");
    git("config", "commit.gpgsign", "false");
    for (const name of scripts) {
      mkdirSync(join(repo, "scripts/release"), { recursive: true });
      copyFileSync(join("scripts/release", name), join(repo, "scripts/release", name));
    }
    commit("app 1.0.0", {
      ...appVersion("1.0.0"),
      "nova/src/server.ts": "export const a = 1;\n",
      "nova/version.json": '{\n  "skill_version": "0.1.0"\n}\n',
      "version.json": '{"skill_version":"0.1.0"}\n',
      "cli/main.go": "package main\n",
      "skills/ha-nova/SKILL.md": "# skill\n",
    });
  });

  afterEach(() => {
    rmSync(repo, { recursive: true, force: true });
  });

  it("refuses a final tag when App code changed without an App bump", () => {
    commit("relay change", { "nova/src/server.ts": "export const a = 2;\n" });
    const result = guard("v0.2.0");
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("App sources changed since App 1.0.0");
    expect(result.stderr).toContain("nova/src/server.ts");
  });

  it("counts App dependencies and the bridge staging script as App sources", () => {
    commit("deps", { "nova/package-lock.json": "{}\n" });
    expect(guard("v0.2.0").stderr).toContain("nova/package-lock.json");
    commit("bump past it", appVersion("1.0.1"));
    commit("staging", { "scripts/release/stage-app-bridge.sh": "echo\n" });
    expect(guard("v0.2.0").stderr).toContain("scripts/release/stage-app-bridge.sh");
  });

  it("counts App metadata beyond the version line, and translations", () => {
    commit("ports", { "nova/config.yaml": 'name: NOVA Relay\nversion: "1.0.0"\nports:\n  8791/tcp: 8791\n' });
    const result = guard("v0.2.0");
    expect(result.status).toBe(1);
    expect(result.stderr).toContain("nova/config.yaml");
    commit("bump past it", { "nova/config.yaml": 'name: NOVA Relay\nversion: "1.0.1"\nports:\n  8791/tcp: 8791\n' });
    expect(guard("v0.2.0").status).toBe(0);
    commit("translations", { "nova/translations/en.yaml": "configuration: {}\n" });
    expect(guard("v0.2.0").stderr).toContain("nova/translations/en.yaml");
    commit("bump again", { "nova/config.yaml": 'name: NOVA Relay\nversion: "1.0.2"\nports:\n  8791/tcp: 8791\n' });
    commit("cloud flag", { "nova/version.json": '{\n  "skill_version": "0.1.0",\n  "cloud_remote_enabled": true\n}\n' });
    expect(guard("v0.2.0").stderr).toContain("nova/version.json");
  });

  it("lets nova/version.json change its skill_version without an App bump", () => {
    commit("multi-line metadata", { "nova/version.json": '{\n  "skill_version": "0.1.0",\n  "cloud_remote_enabled": true\n}\n', "nova/config.yaml": 'name: NOVA Relay\nversion: "1.0.1"\n' });
    commit("next release", { "nova/version.json": '{\n  "skill_version": "0.2.0",\n  "cloud_remote_enabled": true\n}\n' });
    expect(guard("v0.2.0").status).toBe(0);
  });

  it("passes once the promote commit bumps the App version", () => {
    commit("relay change", { "nova/src/server.ts": "export const a = 2;\n" });
    commit("promote", appVersion("1.1.0"));
    const result = guard("v0.2.0");
    expect(result.status).toBe(0);
    expect(result.stdout).toContain("ships App 1.1.0");
  });

  it("passes a CLI, skills, or release-metadata-only final", () => {
    commit("cli and skills", {
      "cli/main.go": "package main // v2\n",
      "skills/ha-nova/SKILL.md": "# skill v2\n",
      "version.json": '{"skill_version":"0.2.0"}\n',
      "nova/version.json": '{\n  "skill_version": "0.2.0"\n}\n',
    });
    expect(guard("v0.2.0").status).toBe(0);
  });

  it("skips rc tags, which run on the landing commit before the bump", () => {
    commit("relay change", { "nova/src/server.ts": "export const a = 2;\n" });
    const result = guard("v0.2.0-rc1");
    expect(result.status).toBe(0);
    expect(result.stdout).toContain("skip");
  });

  it("refuses malformed tags and shallow history", () => {
    expect(guard("v0.2").status).toBe(1);
    expect(guard("v0.2.0-rc0").status).toBe(1);
    const shallow = mkdtempSync(join(tmpdir(), "ha-nova-app-promoted-shallow-"));
    try {
      commit("second", { "cli/main.go": "package main // 2\n" });
      execFileSync("git", ["clone", "-q", "--depth", "1", `file://${repo}`, shallow]);
      const result = spawnSync("bash", [join(shallow, "scripts/release/verify-app-promoted.sh"), "v0.2.0", "HEAD"], { encoding: "utf8" });
      expect(result.status).toBe(1);
      expect(result.stderr).toContain("full git history");
    } finally {
      rmSync(shallow, { recursive: true, force: true });
    }
  });
});
