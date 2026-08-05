import { execFileSync } from "node:child_process";
import { readFileSync, readdirSync } from "node:fs";
import { join } from "node:path";

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

  it("guards the production endpoint against CI runs in the product itself", () => {
    // Workflow files are frozen to uses:-only deltas while Cloud remote is
    // enabled, so the smoke isolation lives in the census client: CI runs
    // skip the ping and the shared send layer refuses the BUILT-IN endpoint
    // (stubbed test endpoints stay unaffected).
    const census = readFileSync("cli/census.go", "utf8");
    expect(census).toContain("censusBuiltinEndpointURL");
    expect(census).toContain('os.Getenv("CI") != ""');
    expect(census).toContain("censusPingSkipCI");
    expect(census).toContain(
      "refusing census %s against the production endpoint in CI",
    );
  });

  it("keeps every live E2E entry point on the census kill switch", () => {
    // Shell launchers AND Python entry points: any executable added to
    // scripts/e2e/ must export HA_NOVA_NO_CENSUS=1 before driving the built
    // binary (session-bootstrap runs `ha-nova check-update --quiet`).
    const entries = readdirSync("scripts/e2e").filter(
      (name) => name.endsWith(".sh") || name.endsWith(".py"),
    );
    expect(entries.length).toBeGreaterThan(0);
    for (const name of entries) {
      const content = readFileSync(join("scripts/e2e", name), "utf8");
      expect(content, `${name} must set HA_NOVA_NO_CENSUS`).toContain(
        "HA_NOVA_NO_CENSUS",
      );
    }
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
