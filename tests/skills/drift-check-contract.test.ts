// tests/skills/drift-check-contract.test.ts
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const writeSafety = read("skills/ha-nova/write-safety.md");
const energy = read("skills/energy/SKILL.md");
const yamlConfig = read("skills/yaml-config/SKILL.md");
const dashboard = read("skills/dashboard/SKILL.md");
const scene = read("skills/scene/SKILL.md");
// Contract docs hard-wrap, so pin sentences, not wrap columns.
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("pre-write drift check covers every full-document write (#514)", () => {
  it("states the clause once, for every whole-document family", () => {
    const clause = flat(writeSafety);
    expect(clause).toContain("### Drift check before apply");
    expect(clause).toContain(
      "Applies to every write that replaces a whole document or whole list",
    );
    expect(clause).toContain("energy preferences, and a YAML file");
    expect(clause).toContain("no optimistic locking anywhere");
    // Post-write verification is the failure report, not the guard.
    expect(clause).toContain("Verifying afterwards is not a substitute");
  });

  it("wires energy save_prefs to the clause instead of relying on the post-save check", () => {
    expect(flat(energy)).toContain(
      "run the drift check before applying (`skills/ha-nova/write-safety.md` → Drift check before apply)",
    );
    expect(flat(energy)).toContain("re-read `get_prefs` immediately before the save");
    expect(flat(energy)).toContain("On any other foreign change STOP");
    // First-time setup legitimately has no prefs document: a second
    // ERR_NOT_FOUND is the unchanged answer, not a failure to compare.
    expect(flat(energy)).toContain("On first-time setup the basis is absence");
    expect(flat(energy)).toContain(
      "a second `ERR_NOT_FOUND` means nothing changed and the save proceeds",
    );
  });

  it("wires the yaml whole-file write to the clause, last before the write", () => {
    const y = flat(yamlConfig);
    expect(y).toContain(
      "run the drift check IMMEDIATELY before the write (`skills/ha-nova/write-safety.md` → Drift check before apply)",
    );
    // Ordering matters: a snapshot round trip after the re-read reopens the
    // very window the re-read exists to close.
    expect(y).toContain("so the snapshot round trip cannot open a new window behind it");
    expect(y).toContain("compare against the content step 1 read");
    expect(y).toContain("`write_file` would revert them");
  });

  it("handles the brand-new file, whose basis is absence rather than content", () => {
    // read_file on a missing path fails by design, so an unconditional
    // re-read would block legitimate file creation or demand that an error
    // be read as success.
    const y = flat(yamlConfig);
    expect(y).toContain("brand-new file: absence IS the basis");
    expect(y).toContain("`FILE_NOT_FOUND` is the expected answer");
    expect(y).toContain("treat it as success here, not as an error");
    expect(y).toContain("writing would overwrite a file nobody read");
    // list_dir caps at 500 entries and flags truncation, so it can report an
    // existing file as absent — an exact-path probe cannot.
    expect(y).toContain("Do not use `list_dir` for this");
    expect(y).toContain("at most 500 entries");
  });

  it("keeps the two families that already had the STOP", () => {
    // Regression guard: these were the correct precedents the gap was
    // measured against, so they must not quietly lose it.
    expect(flat(dashboard)).toContain("STOP — confirmation expired");
    expect(flat(scene)).toContain(
      "if the live scene differs from the previewed basis, STOP",
    );
  });
});
