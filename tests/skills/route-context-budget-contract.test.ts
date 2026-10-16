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
const ROUTE_BUDGETS: Record<string, { files: string[]; limit: number; verbRoute?: boolean; noRouter?: boolean; skill?: string; extra?: string[] }> = {
  // measured 32941 with the retired 10780-word template snapshot (router
  // 5619 + bootstrap 1176 + output rules 2636 + review 4829 + checks 7901);
  // the official templating page is fetched only when a template check
  // needs a signature checks.md lacks. Measured after the cut: 22211.
  review: {
    // The ha-nova:device dispatch row and its one example (measured 22249);
    // home-map's whole-home row and "review my whole setup" example (measured 22264).
    limit: 22265, // cards: `say` closes the card; bootstrap chaining, narrower router description (measured 22205)
    files: [
      "skills/ha-nova/SKILL.md",
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/ha-nova/output-cards.md",
      "skills/review/SKILL.md",
      "skills/review/checks.md",
      "skills/review/checks-extended.md",
    ],
  },
  // Typed-verbs PR6: writes run through `ha-nova find|get` and
  // `ha-nova plan write|delete|revert` + `apply`, so the agent templates and
  // relay-api.md left the mandatory list, and the router is no longer read
  // for a single-item write (installed releases inject nothing; the write
  // skill carries its own create-summary shape). The card file stays: write
  // renders Preview/Delete/Result/Test Plan Cards. checks.md is the core
  // catalog; checks-extended.md (evidence boundaries, other families) is read
  // only before flagging a check that names a boundary. Measured before PR6:
  // 31945 (with the router and the agents). Measured now: 15353 (multi-step row: 21121).
  write: {
    // output-rules.md gained Confirmation Reasons (2026-10-15 agent scenarios: a
    // skill path leaked into 15/39 replies when agents justified a code);
    // the duplicate bootstrap sentence was dropped. Measured 15498.
    // Inline code-step sentences in write/SKILL.md (a pointer alone still let
    // agents cite the skill file): 39 words over the 15500 spec target,
    // accepted because the leak is user-visible (measured 15537).
    // PR review: the path ban narrowed to internal files (user-relevant
    // paths stay visible) and the entity_id rule for unnamed candidates
    // (measured 15566).
    // The CLI's `say` replaced the inline reason and "yes" sentences (measured 15553).
    // Batch review: `say` closes the write card; literal names/state values (measured 15571).
    limit: 15570, // spec target <= 15500; delete reads only write-safety, bootstrap chaining (measured 15569)
    noRouter: true,
    files: [
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/ha-nova/output-cards.md",
      "skills/write/SKILL.md",
      "skills/ha-nova/payload-schemas.md",
      "skills/ha-nova/best-practices.md",
      "skills/ha-nova/write-safety.md",
      "skills/ha-nova/smallest-solution.md",
      "skills/review/checks.md",
    ],
  },
  // The same route for a multi-step or multi-target write: the full context
  // skill joins (Decision Memory, Work Ledger, Interactive Choices). Kept as
  // its own honest row.
  "write-multi-step": {
    // The ha-nova:device dispatch row and its one example (measured 21385);
    // home-map's routing rows (measured 21400).
    limit: 21401, // Confirmation Reasons + PR-review narrowing; `say` replaced the inline sentences and closes the card; bootstrap chaining (measured 21345)
    skill: "write",
    files: [
      "skills/ha-nova/SKILL.md",
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/ha-nova/output-cards.md",
      "skills/write/SKILL.md",
      "skills/ha-nova/payload-schemas.md",
      "skills/ha-nova/best-practices.md",
      "skills/ha-nova/write-safety.md",
      "skills/ha-nova/smallest-solution.md",
      "skills/review/checks.md",
    ],
  },
  // Typed-verbs PR4: the service-call route runs entity calls through
  // `ha-nova call`, so it needs neither the full router nor the card file
  // (its Output Format renders previews inline; output-rules.md exempts verb
  // routes from output-cards.md). Installed releases inject nothing, so the
  // dev-only index (hooks/session-start) is not counted. Common paths read
  // the bootstrap, the output-rules core, and the skill. On demand only:
  // domain-fields.md (when a field, unit, or feature bit is uncertain, and
  // for relative asks and input_number changes), raw-routes.md (response
  // services, App/host control, events, webhooks), indirect-actuation.md.
  // Measured 2672 (bootstrap 309 + output-rules 668 + skill 1695).
  "service-call": {
    limit: 2872, // spec target <= 3000; Confirmation Reasons + PR-review narrowing; `say` replaced the inline sentences; find/get syntax inline (measured 2871)
    verbRoute: true,
    files: [
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/service-call/SKILL.md",
    ],
  },
  // The same route when the call carries data whose field, unit, or feature
  // bit the agent is unsure of (common for brightness, temperature, modes):
  // domain-fields.md joins. Kept as its own honest row.
  "service-call-with-fields": {
    limit: 3890, // Confirmation Reasons + PR-review narrowing; `say` replaced the inline sentences; find/get syntax inline (measured 3889)
    verbRoute: true,
    skill: "service-call",
    extra: ["skills/service-call/domain-fields.md"],
    files: [
      "skills/ha-nova/session-bootstrap.md",
      "skills/ha-nova/output-rules.md",
      "skills/service-call/SKILL.md",
      "skills/service-call/domain-fields.md",
    ],
  },
};

// Verb routes skip the router and the card file.
const VERB_ROUTE_MANDATORY = [
  "skills/ha-nova/session-bootstrap.md",
  "skills/ha-nova/output-rules.md",
];

// Write and review render Cards, which live in the on-demand part of the
// output rules; output-rules.md tells every card-rendering route to read it.
const CARDS = "skills/ha-nova/output-cards.md";

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
        [...block.matchAll(/`((?:skills|docs|\.\.)\/[^`]+\.md)`/g)].map((match) => (match[1] ?? "").replace(/^\.\.\//, "skills/")),
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
    expect(readFileSync("skills/ha-nova/output-rules.md", "utf8")).toContain(`read\n\`${CARDS.replace(/^skills\//, "../")}\``);
    for (const [name, { files, verbRoute, noRouter, skill, extra = [] }] of Object.entries(ROUTE_BUDGETS)) {
      const route = skill ?? name;
      const text = readFileSync(`skills/${route}/SKILL.md`, "utf8");
      expect(text).toContain("Read and follow `../ha-nova/session-bootstrap.md`");
      const mandatory = verbRoute
        ? VERB_ROUTE_MANDATORY
        : noRouter
          ? [...VERB_ROUTE_MANDATORY, CARDS]
          : [...ROUTER_MANDATORY, CARDS];
      const expected = new Set([...mandatory, `skills/${route}/SKILL.md`, ...declaredAlwaysLoads(route), ...extra]);
      if (route === "review") {
        // The checks catalog is read on every review (Verify-before-flag rule).
        expect(text).toContain("`../review/checks.md`");
        expected.add("skills/review/checks.md");
        // Reviews flag findings routinely, so the evidence boundaries and the
        // scene/dashboard/cross-item/TS families are read every time.
        expect(text).toContain("`../review/checks-extended.md`");
        expected.add("skills/review/checks-extended.md");
      }
      expect([...files].sort(), `${name} route file list must mirror its declarations`).toEqual(
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
