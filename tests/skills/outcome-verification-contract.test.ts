// tests/skills/outcome-verification-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("semantic outcome verification (#567/#566)", () => {
  const doc = flat(read("skills/ha-nova/outcome-verification.md"));

  it("defines the five evidence classes and the four results", () => {
    for (const cls of [
      "**Command acknowledgement only**",
      "**Direct target-state change**",
      "**Indirect effect on related entities**",
      "**Asynchronous completion signal**",
      "**No independently observable outcome**",
    ]) {
      expect(doc).toContain(cls);
    }
    expect(doc).toContain("Report one of exactly four outcomes, never a blend");
    // A multi-probe promise verifies only when every previewed probe holds.
    expect(doc).toContain("EVERY probe the previewed promise names showed its effect");
    expect(doc).toContain("One good probe never verifies a multi-probe promise");
    // The safety union never turns an untaken branch into a failed probe.
    expect(doc).toContain("verification binds to the branch that actually ran");
    expect(doc).toContain('"not applicable (branch not taken)", never `failed`');
    // Without trace or branch-unique effects, the branch is unknowable.
    expect(doc).toContain("the branch is unknowable: report the per-branch readings honestly with overall `unverified`");
    // Bounded recovery retries are governed by their own contract, not the
    // verification no-repeat rule.
    expect(doc).toContain("This binds the VERIFICATION step only");
    expect(doc).toContain("repeats by its own rules, never by this step");
    for (const r of ["`accepted`", "`verified`", "`unverified`", "`failed`"]) {
      expect(doc).toContain(r);
    }
    expect(doc).toContain("Missing evidence is never inferred success");
    // A transport timeout before any acknowledgement lands in unverified.
    expect(doc).toContain("even registration is unknown — say that plainly, never retry");
    // A proven upstream rejection is a real outcome, not a taxonomy hole.
    expect(doc).toContain("the command itself was DEFINITIVELY rejected");
    // Early opposite readings are in-progress, never failure.
    expect(doc).toContain("an opposite reading early in the window is in-progress, not failure");
    // Event/webhook listener starts are acceptance, not effect proof.
    expect(flat(read("skills/service-call/SKILL.md"))).toContain(
      "that proves the listener STARTED (`accepted`), never that its actions succeeded",
    );
    expect(doc).toContain("never proves rejection and never permits a retry");
  });

  it("selects probes from real relationships with bounded observation", () => {
    expect(doc).toContain("Friendly-name similarity alone NEVER selects a probe");
    expect(doc).toContain(
      "Capture every probe's baseline immediately before executing the action — after the confirmation, not at preview time",
    );
    // Already-true probes cannot distinguish a skipped run from a success.
    expect(doc).toContain("A probe whose expected value was ALREADY TRUE at that baseline proves nothing");
    expect(doc).toContain("`verified` cannot rest on already-satisfied probes alone"
    );
    expect(doc).toContain("never an indefinite wait");
    expect(doc).toContain(
      "the observation timeout, and any stability requirement",
    );
  });

  it("never repeats the action automatically", () => {
    expect(doc).toContain("NEVER repeats the original action automatically");
    expect(doc).toContain(
      "not after a transport error on a disruptive or restart-class action",
    );
  });

  it("keeps a restart button's timestamp as acceptance only", () => {
    expect(doc).toContain("the advanced timestamp is `accepted`, never restart proof");
    expect(doc).toContain(
      "still unhealthy after a window that covered the expected restart duration → `failed`",
    );
    expect(doc).toContain(
      "Ordinary buttons (no restart semantics) keep the timestamp as verification of the press itself",
    );
  });

  it("covers the scripts/scenes and async classes the issue names", () => {
    const sc = flat(read("skills/service-call/SKILL.md"));
    // Scripts and scenes: the promise lives on members, not the target.
    expect(sc).toContain(
      "Stateless targets: `scene.apply` and direct `script.*` runs do not reflect the call in the target's own state",
    );
    expect(sc).toContain("a script's or automation's `last_triggered` is acceptance evidence only");
    expect(sc).toContain("`verified` requires the previewed effect probes on the acted-on entities");
    // automation.trigger's runtime rule carries the same downgrade.
    expect(sc).toContain(
      "an advanced `last_triggered` on the automation or script is acceptance evidence only",
    );
    // Async refresh/synchronize semantics.
    expect(doc).toContain("a refresh updating a sensor's `last_updated`");
    expect(doc).toContain("an update entity leaving `in_progress`");
    // Routine polling can advance last_updated without the requested refresh.
    expect(doc).toContain("The signal must be attributable to THIS request");
    expect(doc).toContain("Attribution requires the advance to fall OUTSIDE the integration's known polling cadence");
    expect(doc).toContain("a value change alone attributes nothing either");
    // Restart probes get their own horizon; a too-short window can only be
    // unverified, never failed.
    expect(doc).toContain("inside a RESTART-LENGTH window");
    expect(doc).toContain("window too short or cut off → `unverified`, never `failed`");
    // Classification/probes/baselines belong to the PREVIEW step.
    expect(sc).toContain(
      "re-read every probe AFTER confirmation and immediately before the POST",
    );
    // update_entity's completion signal lives on the target itself — still class 4.
    expect(sc).toContain(
      "OR is asynchronous on the target itself (a refresh via `homeassistant.update_entity`",
    );
    // The canonical retry rule carries the exclusion at its source.
    expect(flat(read("skills/ha-nova/relay-api.md"))).toContain(
      "never for indirect or asynchronous runs",
    );
    expect(flat(read("skills/ha-nova/relay-api.md"))).toContain(
      "this clause narrows service-call retries only",
    );
    expect(flat(read("skills/service-call/SKILL.md"))).toContain(
      "AND whose consumer scan found nothing — a discovered consumer can already have fired on the first accepted call",
    );
  });

  it("wires service-call into the contract", () => {
    const sc = flat(read("skills/service-call/SKILL.md"));
    expect(sc).toContain(
      "A press with restart/reboot/reset semantics is acknowledgement only",
    );
    expect(sc).toContain("never infer a completed restart from the timestamp");
    // A scene's advanced timestamp can coexist with failed member changes.
    expect(sc).toContain(
      "keeps the advanced timestamp as verification of the press itself",
    );
    expect(sc).toContain(
      "any press with an expanded stored action or promised consumer effects (a Template button, an `input_button` automations answer), an advanced timestamp alone is `accepted`, never `verified`",
    );
    expect(sc).toContain("a scene verifies only when the previewed member probes show the promised states");
    expect(sc).toContain(
      "evidence classes and result vocabulary: `skills/ha-nova/outcome-verification.md`",
    );
    // A transport error on a restart-class action must not re-fire it via the
    // generic 502 retry-once rule.
    expect(sc).toContain(
      "disruptive/restart-class actions never retry after any transport error",
    );
  });
});
