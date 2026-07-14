import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

// Version-claim drift guard (masterplan-2026-h2 Wave 0). Skill docs once told
// users to manually version-gate on "Relay 0.3.0 or newer" long after the
// enforced floor (version.json → min_relay_version) had moved past it. Any
// relay version a skill doc names must therefore BE the current floor: when
// the floor moves, this test fails and forces the docs to be revisited
// instead of drifting silently.

const SKILLS_ROOT = "skills";

const ALL_SKILL_MD_FILES = ((): string[] => {
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      const p = join(dir, entry);
      if (statSync(p).isDirectory()) walk(p);
      else if (entry.endsWith(".md")) files.push(p);
    }
  };
  walk(SKILLS_ROOT);
  return files;
})();

const MIN_RELAY_VERSION: string = JSON.parse(readFileSync("version.json", "utf8"))
  .min_relay_version;

describe("relay version claims in skill docs", () => {
  it("version.json declares a min_relay_version", () => {
    expect(MIN_RELAY_VERSION).toMatch(/^\d+\.\d+\.\d+$/);
  });

  it("every relay version a skill doc names equals the enforced floor", () => {
    for (const file of ALL_SKILL_MD_FILES) {
      const lines = readFileSync(file, "utf8").split("\n");
      lines.forEach((line, idx) => {
        if (!/relay/i.test(line)) return;
        for (const match of line.matchAll(/\b(\d+\.\d+\.\d+)\b/g)) {
          expect(
            match[1],
            `${file}:${idx + 1} names relay version ${match[1]}, but the enforced floor is ${MIN_RELAY_VERSION} — update the claim or raise the floor`,
          ).toBe(MIN_RELAY_VERSION);
        }
      });
    }
  });
});
