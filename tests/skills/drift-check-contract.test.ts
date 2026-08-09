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
    expect(flat(energy)).toContain("on any foreign change STOP");
  });

  it("wires the yaml whole-file write to the clause", () => {
    expect(flat(yamlConfig)).toContain(
      "run the drift check before applying (`skills/ha-nova/write-safety.md` → Drift check before apply)",
    );
    expect(flat(yamlConfig)).toContain("compare it against the content step 1 read");
    expect(flat(yamlConfig)).toContain("`write_file` would revert them");
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
