import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const flat = (file: string) => readFileSync(file, "utf8").replace(/\s+/g, " ");

describe("rename scheme (#979)", () => {
  const refactoring = flat("skills/ha-nova/safe-refactoring.md");
  it("infers a convention only from agreeing evidence", () => {
    expect(refactoring).toContain("compare independent devices in the same domain, area, and floor");
    expect(refactoring).toContain("never several entities of one device");
    expect(refactoring).toContain("Infer a pattern only from at least two examples that agree");
    expect(refactoring).toContain("Never take a household convention from one example");
  });
  it("renames in one plan with one ask and names every consumer", () => {
    expect(refactoring).toContain("One pass is one raw plan: one preview, one confirmation.");
    expect(refactoring).toContain("Never one ask per row.");
    expect(refactoring).toContain("Plan it in the same turn as the Report, before any ask: the copy is the plan's first item, never a separate `relay backups` save.");
    expect(refactoring).toContain("then each consumer by name and what happens to it");
    expect(refactoring).toContain("the rename's confirmation never covers them");
    expect(refactoring).toContain("A yes to that offer covers the offered automations and scripts the reply accepts (a plain yes: all of them): draft those and show their preview in that same turn, never a which-ones question when the reply leaves no choice open");
    expect(refactoring).toContain("Keep that tier: never split the pass to get a lighter one.");
    expect(refactoring).toContain("never offer `--no-snapshot` for this pass");
  });
  it("binds each row to its registry entry, apply and undo alike", () => {
    expect(refactoring).toContain("Each rename is bound to the entry the preview read");
    expect(refactoring).toContain("its `id` included");
    expect(refactoring).toContain("only one whose registry `id` matches the snapshot row and whose touched fields still hold what the pass set goes back");
    expect(refactoring).toContain("Once it is applied, plan only the re-points this pass made back");
    expect(refactoring).toContain("as one `plan batch` (revert plans can't be batched, and applied one by one each drifts on the others), one ask");
  });
  it("scans consumers per changed id and keeps registry-id hits as unchanged", () => {
    expect(refactoring).toContain("`ha-nova get <old id> --references` once per changed entity id; its hits cover the whole device");
    expect(refactoring).toContain("keep only those whose `needle` is exactly that old id, saved as the holder list");
    expect(refactoring).toContain("A hit whose `needle` is that row's registry id keeps working: list it as unchanged");
    expect(refactoring).toContain("Name each holder by its `name` (friendly name or dashboard title), never by its id alone.");
    expect(refactoring).toContain("no holder of the saved list may still name it");
  });
  it("never claims a rename is complete from an empty scan", () => {
    expect(refactoring).toContain("never call a rename safe because the scan is empty");
    expect(refactoring).not.toContain("Check that no configs still reference the old ID.");
    expect(flat("skills/organize/SKILL.md")).toContain("Two or more renames: `../ha-nova/safe-refactoring.md` → Rename Scheme, not this. `new_entity_id` and `disabled_by` are NOT low-risk metadata:");
  });
});
