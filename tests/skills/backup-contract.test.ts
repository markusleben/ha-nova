import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";


// The -- RELAY-READY sections live in fallback's split file, which fallback
// loads. A negative assertion must cover both, or it cannot fail.
const relayReadySplit = readFileSync("skills/fallback/relay-ready.md", "utf-8");

const backupSkill = readFileSync("skills/backup/SKILL.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const fallbackSkill = readFileSync("skills/fallback/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");

describe("backup contract", () => {
  it("never restores — restore reboots the system and stays in the HA UI", () => {
    expect(backupSkill).toContain("a restore stops and reboots Home Assistant; never call it from here");
    expect(backupSkill).toContain("Restore is never executed here");
    expect(contextSkill).toContain('"Restore a backup"** → `ha-nova:backup` explains why restore must run in the HA UI');
  });

  it("treats generate as initiation, not completion", () => {
    // backup/generate returns when the job STARTS; success is only provable
    // by polling backup/info until idle plus a new backup entry.
    expect(backupSkill).toContain("Both generate commands return when the job is INITIATED, not when it finishes");
    expect(backupSkill).toContain('Poll `backup/info`');
    expect(backupSkill).toContain("never claim success from initiation alone");
    expect(backupSkill).toContain("initiation is not success");
    // Concurrent-generate guard.
    expect(backupSkill).toContain("a backup is already running — poll instead of starting another");
    expect(backupSkill).toContain("do not fire a second generate");
  });

  it("prefers the user's automatic settings and discovers the local agent at runtime", () => {
    expect(backupSkill).toContain('{"type":"backup/generate_with_automatic_settings"}');
    // Core vs Supervised differ; hardcoding either breaks the other.
    expect(backupSkill).toContain("Supervised installs register `hassio.local`, Core installs `backup.local` — never hardcode");
    expect(backupSkill).toContain('add `"include_all_addons":true` ONLY for `hassio.local`');
    expect(backupSkill).toContain("never invent one");
  });

  it("guards deletion and surfaces silent agent failures", () => {
    expect(backupSkill).toContain("deletion removes it from ALL listed locations, irreversibly");
    expect(backupSkill).toContain("Never delete the only backup or the newest completed backup — manual or automatic — refuse outright");
    expect(backupSkill).toContain("this skill does not carry a bypass");
    expect(backupSkill).toContain("`confirm:<token>`");
    expect(backupSkill).toContain("proceed only when the user types it back exactly");
    // A failing backup location is silent data-loss risk.
    expect(backupSkill).toContain("`agent_errors`");
    expect(backupSkill).toContain("say plainly when automatic backups are NOT configured");
    // Partial success (failed agents/add-ons) is never reported as plain success,
    // and partial add-on backups never masquerade as the newest full backup.
    expect(backupSkill).toContain("`failed_agent_ids`, `failed_addons`, and `failed_folders`");
    expect(backupSkill).toContain("report partial success");
    expect(backupSkill).toContain("also report the newest FULL backup");
    expect(backupSkill).toContain("Require `state: idle` first");
  });

  it("wires the safety-backup flow into the write-safety recovery story with proportionality", () => {
    expect(backupSkill).toContain("### Safety backup before risky changes");
    expect(backupSkill).toContain("proceed with the risky change only after the backup completed");
    expect(writeSafety).toContain("offer to create one first via `ha-nova:backup`");
    expect(contextSkill).toContain('"Make a backup before we change this"** → `ha-nova:backup`');
    // Full backups are expensive (GBs, tens of minutes) — never for small edits,
    // and a recent existing full backup beats creating another.
    expect(backupSkill).toContain("never suggest one for routine small edits");
    expect(backupSkill).toContain("Check Status FIRST");
    expect(backupSkill).toContain("say so instead of creating another");
    expect(backupSkill).toContain("estimate size and duration from the newest full backup");
    expect(writeSafety).toContain("Never suggest a backup for routine small edits");
    expect(writeSafety).toContain("checks for a recent full backup before creating");
  });

  it("is wired into dispatch, capability map, and architecture doc — roadmap entry retired", () => {
    expect(contextSkill).toContain("| check backup status, create a backup (also as a safety net before risky changes), inspect a backup's contents, delete backups | `ha-nova:backup` |");
    expect(fallbackSkill).toContain("| Backups (status, create, inspect, delete) | Covered | backup |");
    expect(fallbackSkill + relayReadySplit).not.toContain("Configuration Backups -- ROADMAP");
    expect(architectureDoc).toContain("backup/SKILL.md");
  });

  it("teaches file-based relay payloads and localized output slots", () => {
    expect(backupSkill).toMatch(/--(data-file|body-file|out)\b/);
    expect(backupSkill).toContain("skills/ha-nova/output-rules.md");
    expect(backupSkill).toContain("Use stable localized slot labels in this order; omit empty slots.");
  });
});
