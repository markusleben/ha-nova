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
    // Bounded recovery retries are governed by their own contract, not the
    // verification no-repeat rule.
    expect(doc).toContain("This binds the VERIFICATION step only");
    expect(doc).toContain("repeats by its own rules, never by this step");
    for (const r of ["`accepted`", "`verified`", "`unverified`", "`failed`"]) {
      expect(doc).toContain(r);
    }
    expect(doc).toContain("Missing evidence is never inferred success");
  });

  it("selects probes from real relationships with bounded observation", () => {
    expect(doc).toContain("Friendly-name similarity alone NEVER selects a probe");
    expect(doc).toContain("Capture every probe's baseline BEFORE executing the action");
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
    expect(sc).toContain("a script via `last_triggered` or acted-on member entities");
    // Async refresh/synchronize semantics.
    expect(doc).toContain("a refresh updating a sensor's `last_updated`");
    expect(doc).toContain("an update entity leaving `in_progress`");
    // Restart probes get their own horizon; a too-short window can only be
    // unverified, never failed.
    expect(doc).toContain("inside a RESTART-LENGTH window");
    expect(doc).toContain("window too short or cut off → `unverified`, never `failed`");
    // Classification/probes/baselines belong to the PREVIEW step.
    expect(sc).toContain(
      "name the probes, expected outcome, and observation window in this preview, and capture the probe baselines before executing",
    );
    // The canonical retry rule carries the exclusion at its source.
    expect(flat(read("skills/ha-nova/relay-api.md"))).toContain(
      "never for disruptive or restart-class actions",
    );
  });

  it("wires service-call into the contract", () => {
    const sc = flat(read("skills/service-call/SKILL.md"));
    expect(sc).toContain(
      "A press with restart/reboot/reset semantics is acknowledgement only",
    );
    expect(sc).toContain("never infer a completed restart from the timestamp");
    // A scene's advanced timestamp can coexist with failed member changes.
    expect(sc).toContain("an advanced timestamp alone is `accepted`, never `verified`");
    expect(sc).toContain("`verified` requires the previewed member probes to show the promised states");
    expect(sc).toContain(
      "evidence classes and result vocabulary: `skills/ha-nova/outcome-verification.md`",
    );
    // A transport error on a restart-class action must not re-fire it via the
    // generic 502 retry-once rule.
    expect(sc).toContain(
      "Disruptive and restart-class actions are excluded: never auto-retry them after any transport error",
    );
  });
});
