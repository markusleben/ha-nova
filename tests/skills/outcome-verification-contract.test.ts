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
    expect(doc).toContain("recovered → `verified`; still unhealthy → `failed`");
    expect(doc).toContain(
      "Ordinary buttons (no restart semantics) keep the timestamp as verification of the press itself",
    );
  });

  it("wires service-call into the contract", () => {
    const sc = flat(read("skills/service-call/SKILL.md"));
    expect(sc).toContain(
      "A press with restart/reboot/reset semantics is acknowledgement only",
    );
    expect(sc).toContain("never infer a completed restart from the timestamp");
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
