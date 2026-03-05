import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("app deploy script contract", () => {
  it("provides executable deploy helper script", () => {
    const file = "scripts/deploy/ha-app-deploy.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  });

  it("performs clean deploy with metadata drift detection and options restore", () => {
    const content = readFileSync("scripts/deploy/ha-app-deploy.sh", "utf8");

    expect(content).toContain(".env.local");
    expect(content).toContain(".env");
    expect(content).toContain("ha store reload");
    expect(content).toContain("ha apps rebuild");
    expect(content).toContain("ha apps start");
    expect(content).toContain("docker rmi -f");
    expect(content).toContain("metadata_needs_reinstall");
    expect(content).toContain("save_app_options");
    expect(content).toContain("restore_app_options");
    expect(content).toContain("base64");
    expect(content).toContain("--raw-json");
  });
});
