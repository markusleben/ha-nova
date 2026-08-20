// tests/skills/grouped-extensions-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

const grouped = read("skills/ha-nova/grouped-change-set.md");
const groupedFlat = flat(grouped);
const batchSafety = read("skills/ha-nova/batch-safety.md");
const batchSafetyFlat = flat(batchSafety);

describe("dependency-bound outputs (issue #595)", () => {
  it("represents a downstream field as a typed slot, never a guessed literal", () => {
    expect(grouped).toContain("## Dependency-Bound Outputs (#595)");
    expect(groupedFlat).toContain("typed manifest slot");
    expect(groupedFlat).toContain("never a guessed literal");
    expect(groupedFlat).toContain("Guessing the future slug stays forbidden");
  });

  it("limits the exposed output to the config entry or its uniquely linked entity", () => {
    expect(groupedFlat).toContain(
      "exactly ONE narrowly defined verified output: the config entry it creates, or its uniquely linked entity ID. Nothing else.",
    );
  });

  it("shows resolver, result shape, payload template, effect, and order in the preview", () => {
    expect(groupedFlat).toContain("The preview shows the resolver");
    expect(groupedFlat).toContain("the allowed result shape");
    expect(groupedFlat).toContain("the downstream payload template with the slot marked");
    expect(groupedFlat).toContain("the semantic effect, and the deterministic order");
  });

  it("binds confirmation to operations, resolver, constraints, template, and order", () => {
    expect(groupedFlat).toContain(
      "Confirmation binds to the operations, the resolver, the constraints, the payload template, and the order; a change to any of them expires it",
    );
  });

  it("requires exactly one matching identity and only the approved slot", () => {
    expect(groupedFlat).toContain("resolve exactly one matching identity");
    expect(groupedFlat).toContain("instantiate ONLY the approved slot");
  });

  it("keeps the downstream owning skill's normal drift and impact checks", () => {
    expect(groupedFlat).toContain("verify the predecessor per its owning skill");
    expect(groupedFlat).toContain(
      "normal pre-apply drift and impact checks unchanged",
    );
  });

  it("stops before the downstream write on any ambiguity, collision, or drift", () => {
    expect(groupedFlat).toContain("STOP the group and require a fresh preview");
    expect(groupedFlat).toContain("ambiguous or missing resolution");
    expect(groupedFlat).toContain("an unexpected type or domain, a collision");
    expect(groupedFlat).toContain("any payload change beyond the approved slot, or foreign drift");
    expect(groupedFlat).toContain("The stop happens BEFORE the downstream write");
  });

  it("keeps the honest non-atomic ledger for dependency-bound sets", () => {
    expect(groupedFlat).toContain(
      "the ledger reports it per Ledger & Partial Completion — applied, failed, not attempted, never atomic",
    );
  });

  it("pins the MVP scope and both reference cases", () => {
    expect(groupedFlat).toContain("at most 10 non-destructive operations");
    expect(groupedFlat).toContain("no fallback or experimental writes");
    expect(groupedFlat).toContain(
      "a derived value fills only the explicitly previewed typed slot",
    );
    expect(groupedFlat).toContain(
      "config-entry helper create → dashboard entity reference",
    );
    expect(groupedFlat).toContain("helper create → automation/script reference");
    expect(groupedFlat).toContain(
      "Destructive or high-consequence operations never join a dependency-bound set",
    );
  });

  it("extends the capability matrix for dashboards as downstream-only", () => {
    // The matrix row is the ONLY dashboard entry; standalone grouped dashboard
    // writes stay excluded, and the generic no-row still closes the matrix.
    expect(grouped).toContain("| `dashboard` | downstream only |");
    expect(groupedFlat).toContain("never a standalone grouped family");
    expect(grouped).toContain("| all others | no |");
    const dashboard = flat(read("skills/dashboard/SKILL.md"));
    expect(dashboard).toContain(
      "ONLY as the downstream operation of a dependency-bound set",
    );
    expect(dashboard).toContain("Dependency-Bound Outputs");
  });
});

describe("cross-family destructive cleanup (issue #583)", () => {
  it("scopes the workflow to one logical target with fully enumerated operations", () => {
    expect(grouped).toContain("## Cross-Family Destructive Cleanup (#583)");
    expect(groupedFlat).toContain("exactly ONE logical cleanup target");
    expect(groupedFlat).toContain("at most 10 fully enumerated operations");
    expect(groupedFlat).toContain("no selectors expanded after confirmation");
  });

  it("requires one immutable manifest carrying per-item owning-skill safety data", () => {
    expect(groupedFlat).toContain("one immutable manifest");
    expect(groupedFlat).toContain("stable target identifier");
    expect(groupedFlat).toContain("per-item dependency/consumer impact result");
    expect(groupedFlat).toContain("per-family recovery path");
    expect(groupedFlat).toContain("deterministic execution order");
  });

  it("binds one typed code to the exact manifest and invalidates it on any drift", () => {
    expect(grouped).toContain("`confirm:cleanup-<target>-<count>-<digest>`");
    expect(groupedFlat).toContain(
      "Any change to any operation, target, payload, impact result, or order — or an expired confirmation — invalidates it",
    );
    expect(groupedFlat).toContain("a new preview with a new manifest and a new code");
  });

  it("executes sequentially through owning skills with every existing gate intact", () => {
    expect(groupedFlat).toContain("Execute sequentially through the owning skills");
    expect(groupedFlat).toContain(
      "pre-apply check, snapshot/backup gate, drift check, verification, timeout handling, and recovery rules",
    );
    expect(groupedFlat).toContain("Fail fast; one ledger of succeeded, failed, and not attempted");
  });

  it("never presents the cleanup as atomic", () => {
    expect(groupedFlat).toContain(
      "This is not an atomic transaction and must never be presented as one",
    );
  });

  it("keeps the high-consequence and lifecycle exclusions", () => {
    expect(groupedFlat).toContain("multiple independent cleanup targets");
    expect(groupedFlat).toContain("user/account and owner/relay-account operations");
    expect(groupedFlat).toContain("backup deletion");
    expect(groupedFlat).toContain("Home Assistant Core/OS/App updates");
    expect(groupedFlat).toContain("high-consequence or physically irreversible actions");
    expect(groupedFlat).toContain("MQTT command/`set` topics");
    expect(groupedFlat).toContain(
      "whole integration removal until its guarded lifecycle path exists (#520)",
    );
  });

  it("stays a separate tier: grouped sets and same-family batches are untouched", () => {
    // The non-destructive grouped invariants survive verbatim.
    expect(grouped).toContain("**No destructive operations.**");
    expect(grouped).toContain("**Cap: 10 operations.**");
    expect(groupedFlat).toContain("a grouped set never carries a destructive operation");
    expect(groupedFlat).toContain("never part of a non-destructive grouped set");
    // batch-safety keeps its invariant and points at the sole cross-family route.
    expect(batchSafetyFlat).toContain("One resource family per manifest");
    expect(batchSafetyFlat).toContain("Cross-Family Destructive Cleanup");
  });

  it("wires the helper delete flow to the cleanup route", () => {
    const helper = flat(read("skills/helper/SKILL.md"));
    expect(helper).toContain(
      "Deleting a helper together with its consumers (cross-family) follows",
    );
    expect(helper).toContain("Cross-Family Destructive Cleanup: one manifest, one code");
  });
  it("admits supported operations only, each with a canonical path (#583)", () => {
    const doc = flat(read("skills/ha-nova/grouped-change-set.md"));
    expect(doc).toContain("only, each with a canonical preview and verification path");
  });
  it("opts the owning contracts into the cross-family tier", () => {
    expect(flat(read("skills/ha-nova/SKILL.md"))).toContain(
      "and the cross-family destructive cleanup manifest: `skills/ha-nova/grouped-change-set.md`",
    );
    expect(flat(read("skills/organize/SKILL.md"))).toContain(
      "inside a cross-family destructive cleanup manifest",
    );
    expect(flat(read("skills/maintenance/SKILL.md"))).toContain(
      "may instead ride a cross-family destructive cleanup manifest",
    );
  });
  it("names the cleanup code in the canonical confirmation tiers", () => {
    expect(flat(read("skills/ha-nova/SKILL.md"))).toContain(
      "Cross-family destructive cleanup takes its own manifest-bound code `confirm:cleanup-<target>-<count>-<digest>`",
    );
  });
});
