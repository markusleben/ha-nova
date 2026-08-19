// tests/skills/recovery-workflows-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("recovery workflows (#568/#569)", () => {
  const doc = flat(read("skills/ha-nova/recovery-workflows.md"));

  // --- Liveness against persistent faults (#568) ---

  it("recognizes watchdog intent and names the edge-trigger defect", () => {
    expect(doc).toContain(
      "the user asks for a watchdog, self-healing, continuous monitoring, or automatic recovery",
    );
    // Regression family: a fault that stays continuously unhealthy after the
    // first failed recovery attempt must not strand the workflow.
    expect(doc).toContain(
      "A failed recovery with a continuously unhealthy signal never re-fires the trigger, so one failed attempt silently strands the fault",
    );
    expect(doc).toContain(
      "a `numeric_state` crossing, a `state` change to an unhealthy value, a binary health sensor turning unhealthy, a template flipping false→true",
    );
  });

  it("considers HA restarting into an existing fault", () => {
    expect(doc).toContain(
      "A Home Assistant restart while the source is already unhealthy sees no transition either",
    );
    expect(doc).toContain("Home Assistant can boot into the fault");
  });

  it("distinguishes the four design classes", () => {
    for (const cls of [
      "**One-shot transition action**",
      "**Recovery attempt with outcome verification**",
      "**Continuously live watchdog**",
      "**Failed recovery that escalates**",
    ]) {
      expect(doc).toContain(cls);
    }
  });

  it("requires post-action re-check per #567 and a persistent-fault path", () => {
    expect(doc).toContain(
      "re-checked after the recovery action per `skills/ha-nova/outcome-verification.md`",
    );
    expect(doc).toContain(
      "An explicit persistent-fault path: bounded retry, periodic re-evaluation, or failure escalation",
    );
  });

  it("evaluates multi-target liveness per target", () => {
    expect(doc).toContain(
      "Multi-target recovery evaluates liveness independently per target",
    );
  });

  it("scopes flagging to watchdog intent and names incomplete designs honestly", () => {
    expect(doc).toContain(
      "ordinary one-shot threshold automations are never flagged when no watchdog or self-healing intent exists",
    );
    expect(doc).toContain(
      'honestly named a "one-shot recovery attempt", never a "watchdog"',
    );
    expect(doc).toContain(
      "it never rewrites the automation and never executes recovery actions",
    );
  });

  // --- Bounded retry policy (#569) ---

  it("adds retries only on explicit request, with eligibility before generation", () => {
    expect(doc).toContain(
      "only when the user explicitly requests recovery, self-healing, or retry behavior — never onto ordinary service calls",
    );
    expect(doc).toContain("Evaluate eligibility BEFORE generating the workflow");
  });

  it("asks the eight design questions", () => {
    for (const q of [
      "Is the action safe to repeat?",
      "Which semantic outcome proves success?",
      "Which observed failure permits another attempt?",
      "What is the maximum number of attempts?",
      "What delay or backoff separates attempts?",
      "How are overlapping recovery runs prevented?",
      "What happens after attempts are exhausted?",
      "Must retry/cooldown state survive a Home Assistant restart?",
    ]) {
      expect(doc).toContain(q);
    }
  });

  it("never auto-retries unsafe actions or ambiguous transport failures", () => {
    expect(doc).toContain(
      "Physical-access, irreversible, non-idempotent, or otherwise unsafe actions are never auto-retried",
    );
    // Regression family: ambiguous transport failure — the first action may
    // already have been applied, so it never justifies another attempt.
    expect(doc).toContain(
      "An ambiguous transport failure never triggers a retry: the first action may already have been applied",
    );
  });

  it("keys every retry on semantic failure evidence with bounded verification", () => {
    expect(doc).toContain(
      "Every retry keys on semantic failure evidence per `skills/ha-nova/outcome-verification.md`",
    );
    expect(doc).toContain("a bare service-call error is never retry evidence");
    // The probe must survive a raised action error, or the retry and
    // exhaustion paths silently skip in exactly the covered failure case.
    expect(doc).toContain("`continue_on_error: true` (or an equivalent structure)");
    expect(doc).toContain("missing semantic evidence routes to the explicit non-retry failure path");
    expect(doc).toContain(
      "Each attempt gets its own bounded outcome-verification window",
    );
  });

  it("bounds attempts and keeps delay/backoff explicit without universal values", () => {
    expect(doc).toContain("The attempt count is finite and explicit");
    expect(doc).toContain("no universal hard-coded values exist");
  });

  it("exits on verified success and takes one exhaustion path", () => {
    // Regression families: verified success and exhausted failure.
    expect(doc).toContain("Recovery exits immediately on verified success");
    expect(doc).toContain(
      "Exhaustion takes one explicit failure path and emits at most one notification per incident",
    );
  });

  it("prevents overlap, uses cooldowns, and isolates state per target", () => {
    // Regression family: overlapping triggers.
    expect(doc).toContain(
      "The automation `mode` and condition guards prevent overlapping recovery sequences",
    );
    expect(doc).toContain("periodic re-entry uses a cooldown where necessary");
    expect(doc).toContain(
      "Multi-target workflows isolate attempts, cooldown, and results per target",
    );
  });

  it("states restart survival and the maximum real actions in the preview", () => {
    // Regression family: Home Assistant restart behavior.
    expect(doc).toContain(
      "Persistent helpers are introduced only when state must survive a Home Assistant restart",
    );
    expect(doc).toContain("restart-survival behavior is stated in the preview");
    expect(doc).toContain(
      "The preview states the maximum number of real actions that may occur",
    );
  });

  // --- Wiring ---

  it("wires the write skill into the contract", () => {
    const write = flat(read("skills/write/SKILL.md"));
    expect(write).toContain(
      "A recovery, watchdog, self-healing, or retry intent reads `skills/ha-nova/recovery-workflows.md` before drafting",
    );
    expect(write).toContain(
      "the persistent-fault path only where the intent promises",
    );
  });

  it("wires the review check catalog into the contract", () => {
    const checks = flat(read("skills/review/checks.md"));
    expect(checks).toContain(
      'R-29 [HIGH]: Edge-triggered "watchdog" without a persistent-fault path',
    );
    expect(checks).toContain(
      "R-30 [MEDIUM → HIGH]: Retry-policy violation in a recovery workflow",
    );
    expect(checks).toContain(
      "Ordinary one-shot threshold automations are never flagged when no such intent exists",
    );
    expect(checks).toContain(
      "never rewrite the automation and never execute its recovery actions",
    );
  });
  it("keeps the R-29 boundary on declared continuity, not action shape", () => {
    const checks = flat(read("skills/review/checks.md"));
    expect(checks).toContain(
      "A recovery-shaped action alone (restart, reload, reconnect) is never that evidence",
    );
  });
});
