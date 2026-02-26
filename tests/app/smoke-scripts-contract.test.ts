import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("app smoke scripts contract", () => {
  it("provides executable local smoke scripts", () => {
    const files = [
      "scripts/smoke/app-local-build.sh",
      "scripts/smoke/app-local-run.sh",
      "scripts/smoke/app-http-smoke.sh",
      "scripts/smoke/ha-app-e2e.mjs"
    ];

    for (const file of files) {
      const stats = statSync(file);
      expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
      const content = readFileSync(file, "utf8");
      if (file.endsWith(".sh")) {
        expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
      } else {
        expect(content.startsWith("#!/usr/bin/env node")).toBe(true);
      }
    }
  });

  it("uses docker for build/run, curl for local HTTP smoke, and supervisor API for live e2e", () => {
    const build = readFileSync("scripts/smoke/app-local-build.sh", "utf8");
    const run = readFileSync("scripts/smoke/app-local-run.sh", "utf8");
    const http = readFileSync("scripts/smoke/app-http-smoke.sh", "utf8");
    const liveE2e = readFileSync("scripts/smoke/ha-app-e2e.mjs", "utf8");

    expect(build).toContain("docker build");
    expect(run).toContain("docker run");
    expect(http).toContain("curl");
    expect(http).toContain("/health");
    expect(http).toContain("/ws");
    expect(liveE2e).toContain("/addons/");
    expect(liveE2e).toContain("/options/validate");
    expect(liveE2e).toContain("/restart");
  });
});
