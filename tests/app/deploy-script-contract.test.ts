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

  it("supports fast and clean deploy modes and rebuild workflow", () => {
    const content = readFileSync("scripts/deploy/ha-app-deploy.sh", "utf8");

    expect(content).toContain("--mode fast|clean");
    expect(content).toContain("ha apps reload");
    expect(content).toContain("ha apps rebuild");
    expect(content).toContain("ha apps start");
    expect(content).toContain("docker rmi -f");
  });
});
