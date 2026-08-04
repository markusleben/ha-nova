import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const reference = readFileSync("skills/ha-nova/config-snapshots.md", "utf8");
const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");
const sceneSkill = readFileSync("skills/scene/SKILL.md", "utf8");
const dashboardSkill = readFileSync("skills/dashboard/SKILL.md", "utf8");
const helperSkill = readFileSync("skills/helper/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");

describe("config snapshots contract", () => {
  it("keeps the shared reference honest about capture, restore, and limits", () => {
    // Auto-snapshots are prunable; named ones survive — the naming rule IS the retention policy.
    expect(reference).toContain("auto-snapshots MUST use the `auto-` prefix");
    // HA ids carry underscores/dots — the store only accepts hyphen slugs.
    expect(reference).toContain("the item's id made relay-safe");
    // Capture must never override the user's confirmed operation.
    expect(reference).toContain("STOP and tell the user there will be no snapshot");
    expect(reference).toContain("The already-typed\nconfirmation stays valid");
    // The capture trigger is destructive ops, not routine edits.
    expect(reference).toContain("Capture triggers, per family");
    expect(reference).toContain("every YAML file overwrite or delete");
    // Restore is a normal write, not a blind put.
    expect(reference).toContain("never delete+recreate");
    expect(reference).toContain("a restore is a normal write, never a blind put");
    // Honesty labels: item vs graph, and the excluded families.
    expect(reference).toContain("A snapshot restores the ITEM, never the reference graph");
    expect(reference).toContain("recreate mints a NEW id");
    expect(reference).toContain("config-entry\nhelpers, areas/floors/labels/categories, users, recorder data");
    // Graceful degradation on relays without the store.
    expect(reference).toContain("`404/NOT_FOUND`\nfrom `/backups` means the relay predates the snapshot store");
    // revert stays the quick-undo layer; snapshots never masquerade as full backups.
    expect(reference).toContain("Offer\n  `revert` first when it applies");
    expect(reference).toContain('never call it a backup');
  });

  it("wires every advertised category", () => {
    expect(reference).toContain("All wired for auto-capture");
  });

  it("wires the second-step families too", () => {
    const energySkill = readFileSync("skills/energy/SKILL.md", "utf8");
    const yamlSkill = readFileSync("skills/yaml-config/SKILL.md", "utf8");
    const organizeSkill = readFileSync("skills/organize/SKILL.md", "utf8");
    expect(energySkill).toContain("Before any save that REMOVES entries (a single source/device removal included)");
    expect(yamlSkill).toContain("data = `{path: <exact logical path>, content: <current file content>}`");
    expect(yamlSkill).toContain("A user-requested `delete_file` is code-gated like any delete AND captures the auto config snapshot");
    expect(organizeSkill).toContain("the DEVICE registry record for a device-level `disabled_by`");
  });

  it("wires auto-capture before destructive ops in every FULL family", () => {
    expect(writeSkill).toContain("capture the auto config snapshot of the current read-back first");
    expect(sceneSkill).toContain("Capture the auto config snapshot of the current config first");
    expect(sceneSkill).toContain("When the confirmed update REMOVES scene members, capture the auto config snapshot first");
    expect(dashboardSkill).toContain("capture the auto config snapshot first — data = `{shell:");
    expect(dashboardSkill).toContain("capture the auto config snapshot of the pre-save document when the save removes views or cards");
    expect(helperSkill).toContain("Capture the auto config snapshot of the current list item first");
  });

  it("updates the safety matrix and dispatch", () => {
    expect(writeSafety).toContain("`skills/ha-nova/config-snapshots.md` is the SSOT");
    expect(writeSafety).toContain("config snapshot (auto before delete, identity-preserving restore); HA Backups");
    expect(contextSkill).toContain("list or delete config snapshots");
    expect(contextSkill).toContain("restore a config snapshot");
  });

  it("supports exact manifest-bound batch deletion without widening prune", () => {
    expect(reference).toContain("## Batch delete (explicit opt-in)");
    expect(reference).toContain("`config-snapshots` resource family");
    expect(reference).toContain("Snapshot categories may share one manifest");
    expect(reference).toContain("Cap the manifest at **20** files");
    expect(reference).toContain("confirm:batch-delete-config-snapshots-<count>-<digest>");
    expect(reference).toContain("Run an unfiltered `list` immediately before building the manifest");
    expect(reference).toContain("never expand a category, age, prefix, wildcard, or other\n  selector after the preview");
    expect(reference).toContain('Execute one `{"action":"delete","file":"<literal-file>"}` request at a\n  time');
    expect(reference).toContain("After each response, `list` again and verify that\n  exact file is absent before continuing");
    expect(reference).toContain("succeeded, failed, and not attempted");
    expect(reference).toContain("the deleted copy itself has no rollback");
    expect(reference).toContain("Never use batch delete to emulate prune or to widen `keep_named`");
  });
});

describe("update-revert checkpoint observability (#483)", () => {
  const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
  const updateRevert = readFileSync("skills/ha-nova/update-revert.md", "utf8");
  const writeSkill = readFileSync("skills/write/SKILL.md", "utf8");

  it("carries the alias in the record and reflects the CLI receipt", () => {
    expect(writeSafety).toContain('"name":"<the item\'s alias/friendly name>"');
    expect(writeSafety).toContain("structured receipt");
    expect(writeSafety).toContain("`replaced` receipt means the target's PREVIOUS checkpoint");
    expect(writeSkill).toContain("reflect the save receipt in the result");
    expect(writeSkill).toContain("evicted targets are no longer revertible");
  });

  it("separates the three recovery layers explicitly", () => {
    expect(updateRevert).toContain("Three recovery layers, never conflated");
    expect(updateRevert).toContain("Update-revert checkpoint");
    expect(updateRevert).toContain("Config snapshot");
    expect(updateRevert).toContain("Home Assistant Backup");
  });
});
