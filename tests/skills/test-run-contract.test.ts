import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const testRun = readFileSync("skills/ha-nova/test-run.md", "utf8");
const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const serviceCall = readFileSync("skills/service-call/SKILL.md", "utf8");

describe("post-write test-offer contract", () => {
  it("is wired into write, write-safety, and service-call", () => {
    expect(writeSkill).toContain("### Phase 5: Test Offer (create/update only)");
    expect(writeSkill).toContain("skills/ha-nova/test-run.md");
    expect(writeSafety).toContain("skills/ha-nova/test-run.md");
    expect(serviceCall).toContain("skills/ha-nova/test-run.md");
    // Ownership split: test-run defines the plan, service-call owns execution.
    expect(testRun).toContain("Automation And Script Runtime Calls");
    expect(testRun).toContain("This file defines only how the test plan is chosen, presented, and verified");
  });

  it("keeps the offer-only invariant and the no-dry-run honesty", () => {
    expect(testRun).toContain("Never execute actions, triggers, or events before");
    expect(testRun).toContain("the user picks an option");
    expect(testRun).toContain("Home Assistant has no dry run");
    // Transparency duty: run options must name devices and end state.
    expect(testRun).toContain("names the physical devices it will");
    expect(writeSkill).toContain("Never execute anything unconsented");
    // Read-only probes are allowed for card building — and must surface
    // currently-false conditions instead of letting a real test die silently.
    expect(testRun).toContain("Side-effect-free reads");
    expect(testRun).toContain("never let a real-path test die silently at a");
  });

  it("defines the three option types with their proof limits", () => {
    expect(testRun).toContain("### Logic check (nothing switches)");
    expect(testRun).toContain("### Run actions now");
    expect(testRun).toContain("### Full real-path test");
    expect(testRun).toContain("POST /api/template");
    expect(testRun).toContain("Does not exercise the trigger");
    expect(testRun).toContain("`trigger.id`");
  });

  it("forces an explicit skip_condition and labels bypassing as higher risk", () => {
    expect(testRun).toContain("Always set `skip_condition` explicitly");
    expect(testRun).toContain("HA defaults it to `true`");
    expect(testRun).toContain("`skip_condition: true` as higher risk");
  });

  it("binds a single confirmation to the Test Plan card — no double gate", () => {
    expect(testRun).toContain("Single confirmation: the Test Plan card doubles as the runtime-call");
    expect(testRun).toContain("do not ask a second time");
    expect(testRun).toContain("Any change to the plan expires");
    expect(serviceCall).toContain("the user's option choice on that card IS the bound confirmation");
  });

  it("recommends by action risk and never sells high-consequence runs as safe", () => {
    expect(testRun).toContain("## Feasibility & Recommendation");
    expect(testRun).toContain("high consequence: locks, garage doors, alarm panels, valves");
    expect(testRun).toContain("never presented as consequence-free");
    // Live-verified on a real instance: logical-only actions can still unlock
    // a door via a co-listening automation — the risk class must escalate.
    expect(testRun).toContain("Co-listeners escalate");
    expect(testRun).toContain("inherits that risk level");
    // An unavailable target makes any run unprovable — the card must say so.
    expect(testRun).toContain("cannot prove physical behavior right\nnow");
    // Codex P2 (#334): a garage door exposed as cover.* or a heating setpoint
    // must not slip into the ordinary-device row on domain alone.
    expect(testRun).toContain("check `device_class`");
    expect(testRun).toContain("belong in the high-consequence row");
  });

  it("keeps real-path recipes honest per trigger type", () => {
    expect(testRun).toContain("## Real-Path Recipes");
    expect(testRun).toContain("ask the user to trigger the device physically");
    // Codex P2 (#334): `for:` durations and numeric thresholds need a hold
    // from the non-matching side — an early restore resets the pending trigger.
    expect(testRun).toContain("wait out the full `for:`");
    expect(testRun).toContain("an early restore resets\n  the pending trigger");
    expect(testRun).toContain("POST /api/events/<event_type>");
    expect(testRun).toContain("`ha-nova:mqtt`");
    // Time-based triggers cannot be faked; never bend the clock or the config.
    expect(testRun).toContain("cannot be");
    expect(testRun).toContain("Never change the system");
    expect(testRun).toContain("clock or temporarily edit the trigger");
    // Blast radius: fired states/events reach every listener. Live-verified:
    // search/related does NOT index event listeners — events need a config scan.
    expect(testRun).toContain("reaches every listener");
    expect(testRun).toContain("search/related");
    expect(testRun).toContain("does not index\nlisteners");
  });

  it("runs the consented post-run follow-up: trace, state verify, restore", () => {
    expect(testRun).toContain("## Post-Run Verification (automatic after any consented run)");
    // Codex P2 (#334): a logic check has no run and no trace — reading
    // `trace latest` there would report a stale run as the test result.
    expect(testRun).toContain("A logic check creates no run");
    expect(testRun).toContain("report the rendered condition/template results instead");
    // Codex P2 (#334): trace latest can serve a stale prior run — require a
    // fresh run_id before treating the trace as the test result.
    expect(testRun).toContain("Capture the latest run_id before the run");
    expect(testRun).toContain("accept it only if its run_id is new");
    expect(testRun).toContain("ha-nova trace latest <entity_id> --json");
    expect(testRun).toContain("never infer device safety from");
    expect(testRun).toContain("Leave no test residue");
    expect(testRun).toContain("one passing run proves this path once");
    // Restore covers only what the confirmed card named — no surprise writes.
    expect(testRun).toContain("Anything the card did not name needs its\n   own previewed call");
    // Codex P2 (#334): an automation enabled only for the test must not stay
    // enabled in production afterwards.
    expect(testRun).toContain("the post-run restore returns it to disabled");
    expect(testRun).toContain("enabled only for the test (back to disabled)");
    // Write skill allows the consented follow-up without weakening the default.
    expect(writeSkill).toContain(
      "Do not auto-trigger or auto-read traces outside an accepted Phase 5 test plan",
    );
  });

  it("handles the failure path honestly and routes fixes through review gates", () => {
    expect(testRun).toContain("## When the Test Fails");
    expect(testRun).toContain("the trigger did not fire");
    expect(testRun).toContain("report the exact stop point");
    expect(testRun).toContain("`ha-nova:diagnose`");
    expect(testRun).toContain("already-saved `revert`");
    expect(testRun).toContain("A failed test never auto-triggers a config change");
  });

  it("covers long-running actions and multi-target changes", () => {
    // Blocking calls on delay/wait actions would hang — trace is the truth.
    expect(testRun).toContain("Actions containing `delay` or `wait_*`");
    expect(testRun).toContain("slow service response is not a failure");
    expect(testRun).toContain("never one card per target");
    // Cards speak user language first; the technical binding stays secondary.
    expect(testRun).toContain("Options lead with what the user will experience in plain words");
  });

  it("keeps the offer compact and de-escalates after a skip", () => {
    expect(testRun).toContain("at most 3 options plus `skip`");
    expect(testRun).toContain("Session de-escalation");
    // Card stays inside the fixed emoji vocabulary (📝 = preview).
    expect(testRun).toContain("📝 Test: automation.morning_lights");
    expect(writeSkill).toContain("de-escalate to a single line");
    expect(writeSkill).toContain("Skip this phase for deletes");
  });
});
