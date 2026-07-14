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
    expect(reference).toContain("before a full-document save that REMOVES content");
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
    expect(yamlSkill).toContain("A user-requested `delete_file` is tokenized like any delete AND captures the auto config snapshot");
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
    expect(contextSkill).toContain("list or restore config snapshots");
  });
});
