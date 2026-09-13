import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// Route context ratchet (#521): the per-skill budgets above measure files,
// not what a route actually loads. A review or write session must read the
// router (skills/ha-nova/SKILL.md), session-bootstrap.md, output-rules.md,
// the route's SKILL.md (+ folded files), and every file its "always load"
// list names before the first response. This table pins that mandatory
// transitive load with the same naive counter; an explicit list beats a
// Markdown link traversal because "always" vs "on demand" is prose, not
// syntax. Ratchet values are the measured totals rounded up; lowering them
// is the point of any context-reduction change.
const ROUTE_BUDGETS: Record<string, { files: string[]; limit: number }> = {
  // measured 32941 (router 5619 + bootstrap 1176 + output rules 2636 +
  // review 4829 + checks 7901 + template reference 10780)
  review: {
    limit: 33000,
    files: [
      "skills/ha-nova/SKILL.md",
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/review/SKILL.md",
      "skills/review/checks.md",
      "docs/reference/ha-template-reference.md",
    ],
  },
  // measured 31945 (router trio 9431 + write 2712 + relay-api 4217 +
  // payload-schemas 1002 + best-practices 1212 + write-safety 3330 +
  // smallest-solution 327 + resolve-agent 924 + apply-agent 889 +
  // checks 7901). one-shot-automations.md is on demand and not counted.
  write: {
    limit: 32000,
    files: [
      "skills/ha-nova/SKILL.md",
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/write/SKILL.md",
      "skills/ha-nova/relay-api.md",
      "skills/ha-nova/payload-schemas.md",
      "skills/ha-nova/best-practices.md",
      "skills/ha-nova/write-safety.md",
      "skills/ha-nova/smallest-solution.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/agents/apply-agent.md",
      "skills/review/checks.md",
    ],
  },
};

describe("route context ratchet (#521)", () => {
  it("keeps each route's mandatory context under its ratchet (#521)", () => {
    for (const [route, { files, limit }] of Object.entries(ROUTE_BUDGETS)) {
      const total = files
        .map((file) => readFileSync(file, "utf8").trim().split(/\s+/).length)
        .reduce((sum, count) => sum + count, 0);
      expect(
        total,
        `${route} route loads ${total} words of mandatory context (limit ${limit})`,
      ).toBeLessThan(limit);
    }
  });
});
