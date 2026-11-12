import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// System check-up (docs/work/2026-11-12-system-checkup-spec.md): the CLI
// computes every verdict; the skill words them, never applies a fix itself,
// and never claims a backup restores.
const checkup = readFileSync("skills/health/checkup.md", "utf8");
const health = readFileSync("skills/health/SKILL.md", "utf8");
const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const go = ["cli/verb_map_checkup.go", "cli/verb_map_checkup_home.go"].map((f) => readFileSync(f, "utf8")).join("\n");

describe("system check-up contract", () => {
  it("is reached from the health skill and the router", () => {
    expect(health).toContain("0. \"Is my HA OK\", slow, or backup works: §checkup.md§ first.".replaceAll("§", "\x60"));
    expect(router).toContain("**\"Is everything OK?\" / \"Why so slow?\" / \"Would my backup work?\"** → §ha-nova:health§".replaceAll("§", "\x60"));
  });

  it("reads once through the CLI and never judges by itself", () => {
    expect(checkup).toContain("§ha-nova map --section checkup§ (chat apps: §ha_map§ with §section: checkup§)".replaceAll("§", "\x60"));
    expect(checkup).toContain("Never compute a verdict yourself");
    expect(checkup).toContain("\"nothing found in what HA NOVA checked\", never \"everything is fine\"");
  });

  it("fixes only through the repair flow's confirmation", () => {
    expect(checkup).toContain("Fix a Repair flow (its preview and confirmation; nothing is applied before the user confirms)");
  });

  it("never claims a backup restores", () => {
    expect(checkup).toContain("HA NOVA did not test a restore");
    expect(checkup).toContain("keep the emergency kit outside Home Assistant");
    expect(checkup).toContain("never on this home");
  });

  it("words every code the CLI can return", () => {
    const codes = [...new Set([...go.matchAll(/c\.find\("([A-Z_]+)"/g)].map((m) => m[1]))].sort();
    expect(codes.length).toBeGreaterThan(10);
    for (const code of codes) {
      expect(checkup, code).toContain("\x60" + code + "\x60");
    }
  });
});
