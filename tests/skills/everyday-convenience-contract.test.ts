// tests/skills/everyday-convenience-contract.test.ts
//
// #527: the intents a household actually speaks. These were not missing
// capabilities — Home Assistant could do all of them — they were missing
// flows, so the agent either improvised or spent turns the user did not owe.
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("one-shot and temporary automations (#527)", () => {
  const patterns = read("skills/ha-nova/automation-patterns.md");

  it("gives the self-disabling pattern a home and says why it beats a delay", () => {
    expect(patterns).toContain("## One-Shot And Temporary Automations");
    expect(patterns).toContain("action: automation.turn_off");
    expect(patterns).toContain("{{ this.entity_id }}");
    // A long delay does not survive a restart; disabling does.
    expect(flat(patterns)).toContain(
      "which survives a restart (a long `delay` or `wait_for_trigger` does not)",
    );
  });

  it("keeps the user in control of the leftover", () => {
    const p = flat(patterns);
    // Not a label: labels live in the entity registry that organize owns, and
    // the write flow never touches it, so a promised label is never applied.
    expect(p).toContain("start the `alias` with `One-shot:`");
    expect(p).toContain("the write flow does not touch");
    expect(p).toContain("it stays in the automation list until deleted");
    expect(p).toContain("offer to delete it — do not delete anything unprompted");
    // "every Monday" is an ordinary automation, not this pattern.
    expect(p).toContain("A recurring request (\"every Monday\") is NOT this pattern");
    // A one-shot whose trigger never fires must still expire, or it goes off
    // tomorrow — the exact surprise the pattern exists to prevent.
    expect(p).toContain("A deadline-bound one-shot needs a second way out");
    expect(p).toContain("The disable runs on both paths");
  });

  it("routes the intent from the write flow", () => {
    expect(flat(read("skills/write/SKILL.md"))).toContain(
      "One-Shot And Temporary Automations",
    );
    expect(flat(read("skills/write/SKILL.md"))).toContain(
      "skips unsolicited improvement offers",
    );
  });
});

describe("state-snapshot questions have an owner (#527)", () => {
  const discovery = read("skills/entity-discovery/SKILL.md");

  it("answers 'is anything open / who is home / what is on' as a read", () => {
    expect(discovery).toContain("## State Snapshot Queries");
    expect(flat(discovery)).toContain(
      "`ha-nova:health` answers what is broken, not what is on",
    );
    expect(discovery).toContain("/api/states");
    expect(discovery).toContain('`device_class` `window`/`door`/`garage_door`');
    // A motorized window or garage door is a cover; checking only
    // binary_sensor answers "is anything open?" wrong.
    expect(flat(discovery)).toContain('AND `cover.*` with those device classes in state `open`/`opening`');
    // person STATE reads land here; person CRUD stays with admin.
    expect(flat(discovery)).toContain("this skill owns person STATE reads");
  });

  it("distinguishes a summary from the domain dump it still bans", () => {
    expect(flat(discovery)).toContain("Answer count-first");
    expect(flat(discovery)).toContain("This is a summary, not the banned domain dump");
    expect(discovery).toContain("Never** dump entire domains");
  });

  it("reaches the aliases a household actually curated", () => {
    const d = flat(discovery);
    expect(d).toContain("escalate ONCE to the full registry");
    // A wrong-but-nonempty match must not skip the alias lookup.
    expect(d).toContain("no results, or only matches the user rejects");
    expect(d).toContain("`list_for_display` does not carry them");
    expect(d).toContain("offer once to store it as an alias via `ha-nova:organize`");
  });
});

describe("flows that cost the user extra turns (#527)", () => {
  it("lets one confirmation cover a shopping list, not four", () => {
    const todo = flat(read("skills/todo/SKILL.md"));
    expect(todo).toContain("may confirm as a single grouped change set");
    expect(todo).toContain("Four items should not cost four rounds");
    // One confirmation, but still one read-back per operation: the grouped
    // contract's ledger is fail-fast and a trailing read would record a
    // silently ignored write as applied.
    expect(todo).toContain("read back after EACH applied operation");
    expect(todo).toContain("must stop the batch there");
    // Destructive list deletes keep their own contract.
    expect(todo).toContain("List deletes keep `batch-safety.md` unchanged");
    // The canonical matrix has to agree, or the contract the skill points at
    // classifies it as unsupported.
    const matrix = read("skills/ha-nova/grouped-change-set.md");
    const row = matrix.split("\n").find((l) => l.startsWith("| `todo`"));
    expect(row, "grouped-change-set matrix has no todo row").toBeTruthy();
    expect(row).toContain("item operations on ONE list");
  });

  it("resolves who is home before previewing a presence-conditional send", () => {
    const notify = flat(read("skills/notify/SKILL.md"));
    expect(notify).toContain('### Household routing ("tell whoever is home")');
    expect(notify).toContain("read `person.*` states");
    // Tracker ids are not notify service names.
    expect(notify).toContain("Never derive a service name from `person.device_trackers`");
    // The user confirms recipients, not a rule.
    expect(notify).toContain("Preview the resolved recipient list, not the rule");
    expect(notify).toContain("Nobody home is a real answer");
  });

  it("prefers step parameters for relative asks and keeps the last target", () => {
    const sc = flat(read("skills/service-call/SKILL.md"));
    expect(sc).toContain("`brightness_step_pct`");
    // Covers have no step service: open/close drive to the endpoint.
    expect(sc).toContain("covers have NO step service");
    expect(sc).toContain("`set_cover_position` with the bounded delta");
    expect(sc).toContain(
      'A follow-up nudge ("noch heller") keeps the last confirmed target',
    );
  });

  it("allows the whole-home timeline the endpoint already supports", () => {
    const history = flat(read("skills/history/SKILL.md"));
    expect(history).toContain("omitting `entity` from the logbook path");
    expect(history).toContain("windows up to 24 hours, summary-first");
    // The logbook path takes `entity`; filter_entity_id is the history
    // endpoint's parameter and silently widens the query to the whole home.
    expect(history).toContain("pass the ids as a comma-separated `entity` value");
    expect(history).toContain("`filter_entity_id` belongs to the history endpoint");
  });

  it("offers to keep a repeated batch as a scene", () => {
    const sc = flat(read("skills/service-call/SKILL.md"));
    expect(sc).toContain("After a VERIFIED grouped batch");
    expect(sc).toContain("create-from-current-state");
    // One offer, then silence — this is a suggestion, not a nag.
    expect(sc).toContain("After a single decline, stay silent about it for the session");
  });

  it("makes the one-shot disable survive a failing action", () => {
    const p = flat(read("skills/ha-nova/automation-patterns.md"));
    // HA aborts the sequence on an error, so a trailing disable never runs and
    // the one-shot stays armed for the next matching transition.
    expect(p).toContain("Disable FIRST, act second");
    expect(p).toContain("a disable placed last never runs if the notification");
    // Verified against the live instance: automation.turn_off defaults
    // stop_actions to true, so self-disabling cancels its own remaining steps.
    expect(p).toContain("`stop_actions: false` is not optional");
    expect(p).toContain("cancels its own remaining steps");
  });

  it("routes duration-bound requests to write, not service-call", () => {
    expect(flat(read("skills/ha-nova/SKILL.md"))).toContain(
      "do something FOR a duration",
    );
    expect(flat(read("skills/ha-nova/automation-patterns.md"))).toContain(
      "is a WRITE, not a service call",
    );
  });

  it("stops a relative move that has no value to be relative to", () => {
    const sc = flat(read("skills/service-call/SKILL.md"));
    expect(sc).toContain("STOP the relative operation and say so");
    expect(sc).toContain("would have to invent the number it is relative to");
  });
});
