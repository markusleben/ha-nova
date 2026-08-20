// tests/skills/work-ledger-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const context = readFileSync(
  resolve(__dirname, "../../skills/ha-nova/SKILL.md"),
  "utf-8",
);
const flat = context.replace(/\s+/g, " ");

describe("work ledger contract (#572)", () => {
  it("stays conversation-scoped workflow state, not a task system", () => {
    expect(flat).toContain("## Work Ledger (multi-step tasks)");
    expect(flat).toContain(
      "conversation-scoped, internal, lightweight workflow state, never a persistent task system",
    );
    expect(flat).toContain("create no helpers, to-do items, or stores for it");
    expect(flat).toContain("expose no internal item identifiers");
  });

  it("tracks the primary objective separately from supporting steps", () => {
    expect(flat).toContain("**primary objective** (what the user asked to finish)");
    // Roles and lifecycle states are separate axes — a blocked primary
    // objective stays the primary objective.
    expect(flat).toContain("a blocked primary objective stays the primary objective");
    // An explicitly ordered follow-up activates when its predecessor completes.
    expect(flat).toContain("becomes the active item the moment the predecessor completes");
    expect(flat).toContain("previews and confirmations included; storage still authorizes nothing");
    expect(flat).toContain("activates after the current item finishes");
    expect(flat).toContain("**supporting step**");
    expect(flat).toContain("**deferred** (explicitly postponed)");
    expect(flat).toContain("**blocked**");
    expect(flat).toContain("**completed / cancelled / superseded**");
  });

  it("creates deferred items only from explicit scope-adding phrases", () => {
    expect(flat).toContain(
      '("afterward", "later", "also do this", "once that works") create deferred items when they clearly add scope',
    );
    expect(flat).toContain(
      "Agent suggestions become items only when the user accepts them",
    );
  });

  it("returns to the parent objective after a supporting step", () => {
    expect(flat).toContain(
      "A supporting step never replaces the objective it supports",
    );
    expect(flat).toContain("return to the still-open parent objective");
  });

  it("classifies new requests and never silently replaces on ambiguity", () => {
    expect(flat).toContain(
      "replacing, extending, reprioritizing, or independently adding",
    );
    expect(flat).toContain(
      "never silently treated as a replacement — ask, or keep both",
    );
  });

  it("lets the user manage items and keeps closed work closed", () => {
    expect(flat).toContain(
      "The user can cancel, defer, reprioritize, or supersede any item",
    );
    expect(flat).toContain("completed or cancelled work is not resurfaced");
  });

  it("survives cross-skill handoffs and conversation compaction", () => {
    expect(flat).toContain(
      "Cross-skill handoffs carry the primary objective and pending follow-ups",
    );
    expect(flat).toContain(
      "conversation compaction preserves every non-completed item — active, deferred, AND blocked",
    );
    // The existing read/review handoff prose must reference the ledger.
    expect(flat).toContain(
      "plus the primary objective and pending follow-ups (Work Ledger)",
    );
  });

  it("checks open work before completion and surfaces the remainder", () => {
    expect(flat).toContain(
      "Before declaring the workflow complete, check for explicitly requested open work",
    );
    expect(flat).toContain(
      "summarize remaining work briefly instead of hiding it",
    );
  });

  it("never turns storage into authorization", () => {
    expect(flat).toContain("Storing an item authorizes nothing");
    expect(flat).toContain(
      "deferred work is never executed merely because it is stored",
    );
    expect(flat).toContain(
      "every later mutation follows its owning skill's normal preview, confirmation, and verification flow",
    );
  });
});
