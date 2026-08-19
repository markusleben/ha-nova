// tests/skills/verification-planning-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const context = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/SKILL.md"),
  "utf-8",
);
const flat = context.replace(/\s+/g, " ");

describe("verification planning contract (#597)", () => {
  it("classifies every promised check into the four capability classes before the preview", () => {
    expect(flat).toContain("## Verification Planning (client capabilities)");
    expect(flat).toContain(
      "Before showing a mutation preview, classify every check the plan promises",
    );
    expect(flat).toContain("**Relay-native**");
    expect(flat).toContain("**client-capability**");
    expect(flat).toContain("**user-assisted**");
    expect(flat).toContain("**unavailable**");
  });

  it("makes the preview state planned evidence and session-executable checks", () => {
    expect(flat).toContain(
      "The preview states the planned evidence per check and known limitations",
    );
    expect(flat).toContain(
      "which requested checks are executable in this session and which are not",
    );
  });

  it("stops before the mutation when essential evidence is unavailable", () => {
    expect(flat).toContain(
      "Unavailable evidence essential to the user's success criteria: STOP before the mutation and ask whether to continue with a named fallback",
    );
    expect(flat).toContain("never write first and downgrade afterward");
  });

  it("scopes success wording for optional unavailable checks and keeps them incomplete", () => {
    expect(flat).toContain(
      "proceed with honestly scoped success wording and keep the missing check visible as incomplete",
    );
  });

  it("never collapses an unavailable promised check into a generic success claim", () => {
    expect(flat).toContain(
      "never collapse an unavailable promised check into a generic success claim",
    );
    expect(flat).toContain(
      "give one concrete manual or later-session next step instead",
    );
  });

  it("re-evaluates volatile client capabilities at the point of use", () => {
    expect(flat).toContain(
      "re-evaluate at the point of use — a listed skill or plugin never proves its control surface is callable right now",
    );
    expect(flat).toContain(
      "A capability lost between preview and verification is handled as unavailable from that point",
    );
  });

  it("carries unfinished verification through the Work Ledger", () => {
    expect(flat).toContain(
      "Unfinished verification carries through the Work Ledger as open work, across cross-skill handoffs and compaction",
    );
  });

  it("keeps the Relay out of verification business logic", () => {
    expect(flat).toContain(
      "the Relay gains no screenshot endpoint, browser automation, or verification business logic (Relay stays dumb)",
    );
  });

  it("points the camera snapshot promise at verification planning", () => {
    const camera = readFileSync(
      resolve(__dirname, "../../skills/camera/SKILL.md"),
      "utf-8",
    ).replace(/\s+/g, " ");
    expect(camera).toContain(
      "Viewing the frame is a client capability — classify this check per context skill → Verification Planning (client capabilities)",
    );
  });
});
