import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("app MVP CRUD smoke script contract", () => {
  it("provides executable MVP CRUD smoke script", () => {
    const file = "scripts/smoke/ha-app-mvp-crud-smoke.sh";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env bash")).toBe(true);
  });

  it("runs doctor gate and validates automation CRUD via supervisor core api", () => {
    const content = readFileSync("scripts/smoke/ha-app-mvp-crud-smoke.sh", "utf8");

    expect(content).toContain("ha-nova doctor");
    expect(content).toContain("config.json");
    expect(content).toContain("http://supervisor/core/api");
    expect(content).toContain("/config/automation/config/");
    expect(content).toContain("/services/automation/reload");
    expect(content).toContain("trap cleanup EXIT");
    expect(content).toContain("CRUD_SMOKE_OK");
  });

  it("exposes npm smoke shortcut", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.["smoke:app:mvp"]).toBe(
      "bash scripts/smoke/ha-app-mvp-crud-smoke.sh"
    );
  });
});
