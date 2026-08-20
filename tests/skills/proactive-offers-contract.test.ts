// tests/skills/proactive-offers-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const context = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/SKILL.md"),
  "utf-8",
);
const flat = context.replace(/\s+/g, " ");

describe("proactive assistance offers contract (#565)", () => {
  it("checks for a capable skill before recommending a manual step", () => {
    expect(flat).toContain("## Proactive Assistance Offers");
    expect(flat).toContain(
      "before recommending a manual step, check whether an available HA NOVA skill can perform it",
    );
    expect(flat).toContain("say concretely what HA NOVA can do next");
  });

  it("keeps offers objective-relevant, ranked, and capped at three", () => {
    expect(flat).toContain(
      "Offers stay relevant to the active primary objective (Work Ledger) — never a capability dump",
    );
    expect(flat).toContain(
      "rank them by how directly each one unblocks the objective and show at most three",
    );
  });

  it("hands accepted offers to the owning skill with resolved targets and the objective", () => {
    expect(flat).toContain(
      "An accepted offer hands off to the owning skill with exact resolved targets",
    );
    expect(flat).toContain(
      "the primary objective and pending follow-ups travel along (Work Ledger)",
    );
  });

  it("never turns an offer into authorization and keeps safety rules intact", () => {
    expect(flat).toContain("Offering authorizes nothing");
    expect(flat).toContain("nothing executes merely because it was offered");
    expect(flat).toContain(
      "every preview, confirmation, and safety rule of the owning skill stays intact",
    );
  });

  it("never invents unsupported actions and keeps credential/UI-only steps manual", () => {
    expect(flat).toContain(
      "Never imply or invent actions no current skill supports",
    );
    expect(flat).toContain(
      "credential- or UI-only steps stay manual — say so and name the reason",
    );
  });

  it("does not re-present a declined offer in the same workflow", () => {
    expect(flat).toContain(
      "A declined offer is not re-presented in the same workflow",
    );
  });

  it("keeps blocker-resolving offers outside the unsolicited-suggestion budget", () => {
    expect(flat).toContain(
      "a blocker-resolving offer unblocks the active objective and is NOT an unsolicited improvement suggestion",
    );
    expect(flat).toContain("it does not consume the max-2 suggestion budget");
    expect(flat).toContain(
      "Genuine optional improvements keep riding the Suggestion Block",
    );
  });

  it("names the disabled-sibling-entity example that organize can enable", () => {
    expect(flat).toContain(
      "discovery finds the needed control exists as a disabled sibling entity",
    );
    expect(flat).toContain(
      "The restart control exists but is disabled. I can enable it (`ha-nova:organize`) and continue the restart workflow.",
    );
  });
});
