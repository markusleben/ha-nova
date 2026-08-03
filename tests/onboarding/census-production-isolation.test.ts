import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// #446: production census statistics represent voluntary real participants
// only. Tests, smokes, releases, and deployment verification must never
// mutate them; functional census checks run exclusively against the isolated
// test Worker.

describe("census production isolation (#446)", () => {
  it("static gate finds no executable path that mutates the production worker", () => {
    const out = execFileSync(
      "node",
      ["scripts/test/check-census-production-isolation.mjs"],
      { encoding: "utf8" },
    );
    expect(out).toContain("[census-production-isolation] OK");
  });

  it("provides an isolated test worker environment with its own storage", () => {
    const wrangler = readFileSync("census-worker/wrangler.toml", "utf8");
    expect(wrangler).toContain("[env.test]");
    expect(wrangler).toContain('name = "ha-nova-census-test"');
    // The functional verifier targets exactly that isolated worker.
    const functional = readFileSync(
      "scripts/release/verify-census-functional.sh",
      "utf8",
    );
    expect(functional).toContain(
      "https://ha-nova-census-test.markusleben.workers.dev",
    );
  });
});
