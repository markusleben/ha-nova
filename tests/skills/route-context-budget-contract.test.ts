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
  // measured 32941 with the retired 10780-word template snapshot (router
  // 5619 + bootstrap 1176 + output rules 2636 + review 4829 + checks 7901);
  // the official templating page is fetched only when a template check
  // needs a signature checks.md lacks. Measured after the cut: 22211.
  review: {
    limit: 22250,
    files: [
      "skills/ha-nova/SKILL.md",
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/review/SKILL.md",
      "skills/review/checks.md",
    ],
  },
  // measured 31945 with best-practices.md always loaded (router trio 9431
  // + write 2712 + relay-api 4217 + payload-schemas 1002 + best-practices
  // 1212 + write-safety 3330 + smallest-solution 327 + resolve-agent 924 +
  // apply-agent 889 + checks 7901); best-practices.md is now on demand
  // (bp_status stale/missing/invalid or a check that defers to it) and
  // one-shot-automations.md was always on demand — neither is counted.
  write: {
    limit: 30950,
    files: [
      "skills/ha-nova/SKILL.md",
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/write/SKILL.md",
      "skills/ha-nova/relay-api.md",
      "skills/ha-nova/payload-schemas.md",
      "skills/ha-nova/write-safety.md",
      "skills/ha-nova/smallest-solution.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/agents/apply-agent.md",
      "skills/review/checks.md",
    ],
  },
};

// The three files every route reads before its own SKILL.md: the router
// itself, the session bootstrap it mandates, and the output rules.
const ROUTER_MANDATORY = [
  "skills/ha-nova/SKILL.md",
  "skills/ha-nova/session-bootstrap.md",
  "skills/ha-nova/output-rules.md",
];

// Paths a route declares as unconditional reads: the `Always load:` block
// (write) and the `**Local reference (always):**` block (review), each
// ending at the next blank line. Any other reference is on demand.
function declaredAlwaysLoads(route: string): string[] {
  const text = readFileSync(`skills/${route}/SKILL.md`, "utf8");
  const blocks = [
    ...text.matchAll(/^(?:Always load:|\*\*Local reference \(always\):\*\*)\n([\s\S]*?)\n\n/gm),
  ].map((match) => match[1] ?? "");
  return [
    ...new Set(
      blocks.flatMap((block) =>
        [...block.matchAll(/`((?:skills|docs)\/[^`]+\.md)`/g)].map((match) => match[1] ?? ""),
      ),
    ),
  ];
}

describe("route context ratchet (#521)", () => {
  it("binds each route's file list to the route's own always-load declarations", () => {
    // A new "always load" line must change this table, or the ratchet keeps
    // counting the old context and stays green through the growth it exists
    // to catch.
    // The router-wide mandatory reads are the explicit third leg of this
    // table: every imperative "read `skills/...md`" sentence in the router
    // is extracted and must equal ROUTER_MANDATORY minus the router itself
    // and the session bootstrap (mandated by each route below), so a new
    // router-level unconditional read lands here.
    const router = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const routerReads = [
      ...router.matchAll(/\b(?:[Rr]ead(?: and (?:apply|follow))?) `((?:\.\.\/|skills\/)[^`]+\.md)`/g),
    ].map((match) => (match[1] ?? "").replace(/^\.\.\//, "skills/"));
    expect([...new Set(routerReads)].sort(), "router-wide unconditional reads").toEqual(
      ROUTER_MANDATORY.filter(
        (file) => file !== "skills/ha-nova/SKILL.md" && file !== "skills/ha-nova/session-bootstrap.md",
      ).sort(),
    );
    for (const [route, { files }] of Object.entries(ROUTE_BUDGETS)) {
      const text = readFileSync(`skills/${route}/SKILL.md`, "utf8");
      expect(text).toContain("Read and follow `../ha-nova/session-bootstrap.md`.");
      const expected = new Set([...ROUTER_MANDATORY, `skills/${route}/SKILL.md`, ...declaredAlwaysLoads(route)]);
      if (route === "review") {
        // The checks catalog is read on every review (Verify-before-flag rule).
        expect(text).toContain("`skills/review/checks.md`");
        expected.add("skills/review/checks.md");
      }
      expect([...files].sort(), `${route} route file list must mirror its declarations`).toEqual(
        [...expected].sort(),
      );
    }
  });

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
