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
    // Firing events/webhooks is refused now; the automation.trigger path is
    // tiered and verified like any call.
    expect(flat(read("skills/service-call/raw-routes.md"))).toContain(
      "are REFUSED by the CLI",
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
    // Since PR4 `ha-nova call` verifies; scripts and scenes keep the promise
    // on their members, never on the target's own state.
    const verify = flat(read("cli/verb_verify.go"));
    expect(verify).toContain("statelessRun: a scene, script, button, or automation run does not show");
    expect(verify).toContain("too) has a probe and every probe matches; otherwise it is accepted.");
    expect(verify).toContain("an unknown service is accepted, never verified");
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
    // The canonical retry rule carries the exclusion at its source.
    expect(flat(read("skills/ha-nova/relay-api.md"))).toContain(
      "never for indirect or asynchronous runs",
    );
    expect(flat(read("skills/ha-nova/relay-api.md"))).toContain(
      "this clause narrows service-call retries only",
    );
    // The CLI sends exactly one request and never retries (I7).
    expect(flat(read("skills/service-call/SKILL.md"))).toContain("Never retry after exit 6 or 7");
  });

  it("wires service-call into the contract", () => {
    const sc = flat(read("skills/service-call/SKILL.md"));
    const verify = flat(read("cli/verb_verify.go"));
    // Every press, restart buttons included, is acknowledgement only.
    expect(verify).toContain('(domain == "button" || domain == "input_button") && name == "press"');
    expect(sc).toContain("| 6 | sent; `unverified` or `failed` | Relay `say` (none: the `error`, per Verb Results); never retry automatically. |");
    expect(sc).toContain("Never retry after exit 6 or 7; a verified result names only the proven scope.");
    // The result vocabulary lives in the CLI's result line; the skills relay it.
    expect(flat(read("skills/ha-nova/output-rules.md"))).toContain("result's `say` is the result line: relay it verbatim, only translated");
    const lines = read("cli/say_lines.go");
    for (const lead of ['"Done: "', '" I can\'t check the result."', '"Not done: "', '"Not sure: ', '"Partly done: "']) expect(lines).toContain(lead);
  });
});
