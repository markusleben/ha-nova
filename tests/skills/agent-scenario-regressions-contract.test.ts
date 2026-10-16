import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

// 2026-10-15 end-to-end agent scenarios on the test bed: three skill-text defects that cost real
// recall or leaked internals to users. Each pin guards the sentence the agent
// actually follows.
const flat = (path: string): string => readFileSync(path, "utf8").replace(/\s+/g, " ");

describe("agent scenario regressions (2026-10-15)", () => {
  it("bulk automation audits check every matched target statically before the 5-target deep workset", () => {
    const bulk = flat("skills/ha-nova/bulk-patterns.md");
    const review = flat("skills/review/SKILL.md");
    expect(review).toContain("`ha-nova find --domain automation --limit 200 --include-disabled` (scripts: `ha-nova find --domain script --limit 200 --include-disabled`) is the shortlist");
    expect(review).not.toContain("automation|script --limit");
    expect(review).toContain("Static Pass and runs it over EVERY matched target");
    expect(bulk).toContain("## Static Pass (automation/script audits)");
    // Seeded classes the first-5 sample missed: a missing entity, an
    // impossible state, a trigger entity that never transitions.
    expect(bulk).toContain("`NOT_FOUND` is a missing entity");
    expect(bulk).toContain("`sun.sun`: `above_horizon`, `below_horizon`");
    expect(bulk).toContain("it fires only on a new transition");
    expect(bulk).toContain("never try another WS type");
    // Adversarial review: no false "missing"/"clean" verdicts.
    expect(bulk).toContain("list the target as not checked");
    expect(bulk).toContain("never \"missing entity\"");
    expect(bulk).toContain("Skip items with `attribute:`, `to: null`, templates, and any other domain");
    expect(bulk).toContain("Scripts: `off` is idle, never \"disabled\"");
  });

  it("diagnose treats a day word as the window and checks for a stuck trigger entity", () => {
    const diagnose = flat("skills/diagnose/SKILL.md");
    expect(diagnose).toContain('A day word ("today", "this morning", "last night", "yesterday") already sets the window');
    expect(diagnose).toContain("Ask one question for the time only when nothing bounds it");
    expect(diagnose).toContain("a `state` trigger fires only on a transition, so that is the root cause");
    expect(diagnose).toContain("compare each trigger's full predicate (`from`/`not_from`, `to`/`not_to`, `attribute`, `for:`)");
    expect(diagnose).toContain("Script: it runs only when called");
    expect(diagnose).not.toContain("ask one question if the incident time is unknown");
  });

  it("confirmation reasons stay plain and never cite skills, files, or paths", () => {
    const rules = flat("skills/ha-nova/output-rules.md");
    expect(rules).toContain("## Confirmation Reasons");
    // The CLI writes the reason (`say`); the agent relays it and adds none.
    expect(rules).toContain("A plan's `say` is the reason, localized (names, state values, code literal); add none of your own.");
    expect(rules).toContain('"This unlocks a door, so it needs a code."');
    expect(rules).toContain("Never mention, quote, or link HA NOVA's own skill or rule files or scratch paths — the user doesn't know they exist. Paths the user needs (the config file a change targets, a batch manifest) stay visible.");
    expect(rules).toContain("Clarifying questions name candidates by friendly name and area (add the entity_id when either is missing or both collide).");
    // The pointer alone did not stop citations at the code step: the CLI
    // now hands over the sentence (`say`), relayed where the code is
    // requested; the "yes" rejection stays inline.
    for (const file of ["skills/service-call/SKILL.md", "skills/write/SKILL.md"]) {
      const text = flat(file);
      expect(text).toMatch(/add no other reason; never cite, link, or name this skill or any HA NOVA rule file\./);
      expect(text).toContain('After a "yes", repeat `say`\'s last sentence.');
    }
  });
});
