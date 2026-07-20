import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const maintenanceSkill = readFileSync("skills/maintenance/SKILL.md", "utf8");
const maintenanceReference = readFileSync("skills/maintenance/maintenance-reference.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const fallbackSkill = readFileSync("skills/fallback/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");

describe("maintenance contract", () => {
  it("covers the complete validate_statistics issue vocabulary with safe defaults", () => {
    for (const issueType of [
      "no_state",
      "entity_not_recorded",
      "entity_no_longer_recorded",
      "state_class_removed",
      "units_changed",
      "mean_type_changed",
    ]) {
      expect(maintenanceReference).toContain(`\`${issueType}\``);
    }
    // Non-destructive defaults where they exist.
    expect(maintenanceReference).toContain("report only; fix is `recorder:` YAML config");
    expect(maintenanceReference).toContain("default KEEP (data preserved); clear only on explicit request");
    expect(maintenanceSkill).toContain("default is the non-destructive relabel");
    expect(maintenanceSkill).toContain("`unit_class` REQUIRED");
  });

  it("treats clear_statistics as the point of no return", () => {
    expect(maintenanceSkill).toContain("`recorder/clear_statistics` is IRREVERSIBLE");
    // The anti-Spook gate: never clear an energy-dashboard statistic silently.
    expect(maintenanceSkill).toContain("`energy/get_prefs` cross-check");
    expect(maintenanceSkill).toContain("Typed confirmation code");
    // Red-team blocker: the confirmation code must bind to the fully enumerated set, not a capped sample.
    expect(maintenanceSkill).toContain("a capped sample never authorizes the uncapped set");
    expect(maintenanceSkill).toContain(
      "it binds to one issue group and the manifest's full enumerated ID set",
    );
    // Non-energy dashboards can also reference statistics.
    expect(maintenanceSkill).toContain("scan of storage dashboards for the IDs");
    // Live-verified: the WS call times out at ~10s while the job completes.
    expect(maintenanceSkill).toContain("a WS timeout after ~10 s is NOT failure");
    expect(maintenanceReference).toContain("verify by re-running `validate_statistics`, never blind-retry");
  });

  it("keeps relabel honest and refuses live-entity removal (red-team P1s)", () => {
    // Relabel is corruption when the sensor changed quantity, not label.
    expect(maintenanceSkill).toContain("if the sensor changed what it measures, relabel corrupts data");
    // Live entities never get removed; disable is the alternative.
    expect(maintenanceSkill).toContain("Live/healthy entities: refuse removal — offer disable via `ha-nova:organize`");
    // Purge verification must prove deletion, not just recorder health.
    expect(maintenanceSkill).toContain("`recorder/info` alone cannot prove deletion");
    // No restart/reload side channel to create a restart boundary.
    expect(maintenanceSkill).toContain("never restart or reload HA from this skill");
    // Codex P2: every write is confirmation-gated, not only the destructive ones.
    expect(maintenanceSkill).toContain("Every write: preview → confirmation → per-item verification");
  });

  it("teaches the reversible spike-repair protocol with the paired cost fix", () => {
    expect(maintenanceSkill).toContain("`adjustment = corrected − original`");
    expect(maintenanceSkill).toContain("Reversible via the inverse call");
    expect(maintenanceSkill).toContain("linked cost statistic");
    expect(maintenanceReference).toContain("AND every later bucket");
    expect(maintenanceReference).toContain("`utility_meter.calibrate`");
    expect(maintenanceReference).toContain("5-minute resolution exists only for ~10 days");
    // Live-verified: adjustments land via the recorder queue, not synchronously.
    expect(maintenanceReference).toContain("applied asynchronously via the recorder queue");
  });

  it("states the purge invariants users always get wrong", () => {
    expect(maintenanceSkill).toContain("Long-term statistics are NEVER purged");
    expect(maintenanceSkill).toContain("statistics are NOT touched");
    expect(maintenanceSkill).toContain("fire-and-forget");
    expect(maintenanceReference).toContain("`keep_days: 0` (default) deletes ALL state history for the match");
    // Repack risk gates.
    expect(maintenanceReference).toContain("~2× database size free disk");
    expect(maintenanceReference).toContain("recommend a recent backup first");
    // No DB-size promises through the relay (live-verified null).
    expect(maintenanceReference).toContain("do not promise a size number");
  });

  it("guards orphan removal behind the full gate checklist (red-team blockers)", () => {
    expect(maintenanceReference).toContain("## Orphan-Removal Gate Checklist (ALL must pass)");
    expect(maintenanceReference).toContain("`restored` is a state attribute");
    expect(maintenanceReference).toContain("never `setup_retry`");
    expect(maintenanceSkill).toContain("not part of a whole-integration outage");
    // YAML/template entities have no config entry — the Spook failure class.
    expect(maintenanceSkill).toContain("`config_entry_id: null`");
    expect(maintenanceSkill).toContain("persists across a restart");
    // Reference-check with its honest blind spot.
    expect(maintenanceSkill).toContain("`search/related`");
    expect(maintenanceSkill).toContain("YAML dashboards and templates are not covered");
    // Statistics survive registry removal and become no_state.
    expect(maintenanceSkill).toContain("they then report as `no_state`");
    expect(maintenanceReference).toContain("restorable ~30 days");
    // Devices are report-only.
    expect(maintenanceSkill).toContain("device deletion — no generic API");
  });

  it("labels long-unavailable timestamps by confidence and distrusts last_changed", () => {
    expect(maintenanceSkill).toContain("`last_changed` resets on restart");
    expect(maintenanceReference).toContain("never report it as the outage start");
    expect(maintenanceReference).toContain("years back — numeric sensors only");
  });

  it("never removes merely-offline entities", () => {
    expect(maintenanceSkill).toContain("Never remove an entity that is merely offline");
    expect(maintenanceReference).toContain("`unavailable` without `restored` = integration loaded, device offline → NEVER remove");
  });

  it("is wired into dispatch, capability map, availability table, and architecture doc", () => {
    expect(contextSkill).toContain(
      "| repair statistics (orphans, unit mismatches, sum spikes), purge recorder history, clean up dead registry entries | `ha-nova:maintenance` |",
    );
    expect(contextSkill).toContain('"Clean up orphaned statistics"** → `ha-nova:maintenance`');
    expect(fallbackSkill).toContain("| Statistics repair / Purge / Entity registry remove | Covered | maintenance |");
    expect(fallbackSkill).not.toContain("Entity Removal / Device Detach -- RELAY-READY");
    expect(writeSafety).toContain(
      "| `maintenance` | grouped preview + re-validate verify | spike adjust only (inverse call); clears/purges/removals irreversible | HA Backups (offered pre-bulk) |",
    );
    expect(architectureDoc).toContain("maintenance/SKILL.md");
    // Neighbor skills route to maintenance.
    const energySkill = readFileSync("skills/energy/SKILL.md", "utf8");
    const healthSkill = readFileSync("skills/health/SKILL.md", "utf8");
    expect(energySkill).toContain("statistics repair (sum spikes, orphaned statistics): `ha-nova:maintenance`");
    expect(healthSkill).toContain("statistics repair, purge, registry cleanup (`ha-nova:maintenance`)");
  });

  it("teaches file-based relay payloads and localized output slots", () => {
    expect(maintenanceSkill).toMatch(/--(data-file|body-file|out)\b/);
    expect(maintenanceSkill).toContain("skills/ha-nova/output-rules.md");
    expect(maintenanceSkill).toContain("Use stable localized slot labels in this order; omit empty slots.");
  });
});
