import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("app smoke scripts contract", () => {
  it("provides executable local smoke scripts", () => {
    const files = [
      "scripts/smoke/app-local-build.sh",
      "scripts/smoke/app-local-run.sh",
      "scripts/smoke/app-http-smoke.sh"
    ];

    for (const file of files) {
      const stats = statSync(file);
      expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
      expect(readFileSync(file, "utf8").startsWith("#!/usr/bin/env bash")).toBe(true);
    }
  });

  it("uses docker for build/run and curl for HTTP smoke", () => {
    const build = readFileSync("scripts/smoke/app-local-build.sh", "utf8");
    const run = readFileSync("scripts/smoke/app-local-run.sh", "utf8");
    const http = readFileSync("scripts/smoke/app-http-smoke.sh", "utf8");

    expect(build).toContain("docker build");
    expect(run).toContain("docker run");
    expect(http).toContain("curl");
    expect(http).toContain("/health");
    expect(http).toContain("/ws");
  });
});
