import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";

import { describe, expect, it } from "vitest";

// 0.30 skills sweep: the CLI decides how a change is confirmed. After 0.29
// (calm edits, renames, window and phone confirmation) several files still
// told the agent "update = natural, delete = one-time code", which brings back
// an ask the CLI removed, or a "type the code" while a window is asking.

const SKILL_FILES = ((): string[] => {
  const files: string[] = [];
  const walk = (dir: string) => {
    for (const entry of readdirSync(dir)) {
      const p = join(dir, entry);
      if (statSync(p).isDirectory()) walk(p);
      else if (entry.endsWith(".md")) files.push(p);
    }
  };
  walk("skills");
  return [...files, "docs/reference/skill-architecture.md"];
})();

const flat = (path: string): string => readFileSync(path, "utf8").replace(/\s+/g, " ");

const STALE = [
  "create/update=natural",
  "one-time code only",
  "Preview every write",
  "Deletes still require the one-time code",
  "writes take a natural",
  "take natural confirmation",
  "Nothing changes before the user confirms",
  "The CLI checks every change.",
  "Canonical set: unlocking",
  "take the typed tier — also when",
  // The best-practice freshness gate (web search plus a cache stamp) is gone.
  "bp_status",
  "BP cache",
  "BP gate",
  "automation-bp-snapshot",
  // Typed verbs answer an unreachable Relay with exit 1 RELAY_ERROR.
  "Verify relay CLI",
];

describe("the CLI decides how a change is confirmed", () => {
  it("leaves no stale confirmation, best-practice gate, or probe phrase in the skills", () => {
    const hits: string[] = [];
    for (const file of SKILL_FILES) {
      const text = flat(file);
      for (const phrase of STALE) {
        if (text.includes(phrase)) hits.push(file + ": " + phrase);
      }
    }
    expect(hits).toEqual([]);
  });

  it("states the one rule where confirmation comes up", () => {
    const rule = "`plan.confirm` none → apply; natural → show the preview and wait for the user's reply; code → follow `say`/`next_step` (window, phone, form, or chat code)";
    for (const file of [
      "skills/ha-nova/index.md",
      "skills/ha-nova/SKILL.md",
      "skills/write/SKILL.md",
      "skills/dashboard/SKILL.md",
    ]) {
      expect(flat(file), file).toContain(rule);
    }
    expect(flat("skills/ha-nova/index.md")).toContain("The CLI rates every action; relay its `say`.");
  });

  it("keeps one example list of what a person confirms, for explaining only", () => {
    const answer = flat("skills/ha-nova/capability-answer.md");
    expect(answer).toContain("This is the one example list of what a person confirms; it explains, never decides: the CLI rates every action.");
    expect(answer).toContain("Locking, closing, and arming need no extra step.");
    expect(flat("skills/service-call/SKILL.md")).not.toContain("garage door, gate, or entry-door cover");
  });

  it("drafts only what was asked on a plan that runs without an ask", () => {
    // Test bed 10.11.: "25 → 27 degrees" also rewrote the alias and the
    // description, and plan.confirm none saved it with no preview.
    expect(flat("skills/write/SKILL.md")).toContain(
      "2. Scope: draft only what was asked; never rewrite the name, alias, or description unasked, even one quoting the old value (`../ha-nova/smallest-solution.md` rule 7).",
    );
    // Every plan-writing skill drafts through the smallest-solution contract.
    expect(flat("skills/ha-nova/smallest-solution.md")).toContain(
      "7. Change only what the user asked for: never rewrite a name, alias, or description unasked (a plan with `plan.confirm: none` saves with no preview); offer other edits as a question.",
    );
  });

  it("picks the answer language in the always-loaded output rules", () => {
    expect(flat("skills/ha-nova/output-rules.md")).toContain("Answer in the language of the user's sentence, not of a name in it.");
    // The injected index keeps the short form for the fast call path, where no skill loads.
    expect(flat("skills/ha-nova/index.md")).toContain("Answer in the language of the user's sentence.");
  });

  it("routes adding a device and integration options to integration-setup", () => {
    const index = flat("skills/ha-nova/index.md");
    expect(index).toContain("`ha-nova:integration-setup` — add a new device (discovered, cloud, or Zigbee/Z-Wave/Matter steps)");
    expect(index).toContain("`ha-nova:integration-setup` — integrations: add, options (Configure), reconfigure, subentries, reauthentication");
    const setup = flat("skills/integration-setup/SKILL.md");
    expect(setup).toContain("### Add a Device");
    expect(setup).toContain("Settings > Connectivity > Zigbee, Z-Wave, or Matter > Add device");
  });

  it("reads the test-run file only after the user accepts the test offer", () => {
    const write = flat("skills/write/SKILL.md");
    expect(write).toContain("- `../ha-nova/test-run.md` — only after the user accepts the Phase 5 test offer");
    expect(write).toContain("\"Want a test preview? I'll show which devices would change before anything runs.\"");
  });
});
