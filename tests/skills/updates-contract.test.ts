import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

const updatesSkill = readFileSync("skills/updates/SKILL.md", "utf8");
const contextSkill = readFileSync("skills/ha-nova/SKILL.md", "utf8");
const fallbackSkill = readFileSync("skills/fallback/SKILL.md", "utf8");
const writeSafety = readFileSync("skills/ha-nova/write-safety.md", "utf8");
const architectureDoc = readFileSync("docs/reference/skill-architecture.md", "utf8");

describe("updates contract", () => {
  it("gates every service on the update feature bitmask", () => {
    expect(updatesSkill).toContain("## Feature Gate (critical)");
    expect(updatesSkill).toContain("| 8 | backup before update (add-on/core mechanism) |");
    expect(updatesSkill).toContain("| 16 | release notes |");
    expect(updatesSkill).toContain("Never call a service the mask does not allow");
    // Release notes fail with not_supported without bit 16 (live-verified).
    expect(updatesSkill).toContain("`not_supported`");
    expect(updatesSkill).toContain("`release_summary`/`release_url`");
  });

  it("treats installs as far-reaching with kind-specific safety gates", () => {
    // Core/OS updates restart HA and are not downgradable — safety backup first.
    expect(updatesSkill).toContain("offer a full safety backup first via `ha-nova:backup`");
    expect(updatesSkill).toContain("HA restarts during the update");
    expect(updatesSkill).toContain('include `"backup": true`');
    // Updating the NOVA Relay add-on kills the transport mid-flight.
    expect(updatesSkill).toContain("Updating the NOVA Relay add-on restarts the relay itself");
    expect(updatesSkill).toContain("verify afterwards via `ha-nova relay health`");
    // Firmware caution.
    expect(updatesSkill).toContain("must not be interrupted");
  });

  it("verifies installs by entity re-read, never by service success", () => {
    expect(updatesSkill).toContain("The call returns before the update finishes");
    expect(updatesSkill).toContain("`installed_version` equals the target");
    expect(updatesSkill).toContain("never claim success from the service call alone");
    expect(updatesSkill).toContain("a running update (`in_progress`) blocks a second install");
  });

  it("survives the restart window it warns about (red-team blockers)", () => {
    // Core/OS: relay errors during the poll are the restart, not failure.
    expect(updatesSkill).toContain("ARE the expected restart window, not failure");
    expect(updatesSkill).toContain("never route to `ha-nova setup` mid-update");
    // HACS integrations: installed != active until HA restarts.
    expect(updatesSkill).toContain("loads only after a Home Assistant restart");
    expect(updatesSkill).toContain("installed but not yet active");
    // Long-running hand-off and firmware patience.
    expect(updatesSkill).toContain("update continues in the background");
    expect(updatesSkill).toContain("30+ minutes and a temporarily `unavailable` device are normal");
    // Batch ordering keeps the batch alive across restarts.
    expect(updatesSkill).toContain("Order add-ons and firmware before core/OS");
    // Breaking changes first for core.
    expect(updatesSkill).toContain("Surface breaking-changes sections first");
  });

  it("keeps installs consent-bound — one per confirmation, batches need a confirmed plan", () => {
    expect(updatesSkill).toContain("One update per confirmation");
    expect(updatesSkill).toContain("confirm the batch explicitly, then install sequentially, verifying each before the next");
    expect(updatesSkill).toContain("Never install anything the user did not ask about or confirm");
    expect(updatesSkill).toContain("say so instead of installing them unprompted; an explicit, confirmed user request may still install");
  });

  it("groups the overview and never dumps device-firmware fleets", () => {
    expect(updatesSkill).toContain("Home Assistant stack: core, operating system, supervisor");
    expect(updatesSkill).toContain("never dump 200 firmware rows");
    // platform: hassio covers stack AND add-ons — unique_id is the live-verified
    // second discriminator (home_assistant_{core,os,supervisor}_version_latest).
    expect(updatesSkill).toContain("`platform: hassio` splits by `unique_id`");
    expect(updatesSkill).toContain("`home_assistant_{core,os,supervisor}_version_latest` = HA stack");
    // backup:true is never unconditional for core/OS (would bypass the offer).
    expect(updatesSkill).toContain('`"backup": true` for add-on updates');
    expect(updatesSkill).toContain("ONLY when the confirmed preview explicitly included that built-in backup");
    // Skip is reversible (live-verified skip -> clear_skipped roundtrip).
    expect(updatesSkill).toContain("`/api/services/update/skip`");
    expect(updatesSkill).toContain("`/api/services/update/clear_skipped`");
  });

  it("is wired into dispatch, capability map, availability table, and architecture doc", () => {
    expect(contextSkill).toContain("| check pending updates, read release notes, install updates, skip/unskip versions | `ha-nova:updates` |");
    expect(contextSkill).toContain('"Update Home Assistant"** → `ha-nova:updates` (offers a safety backup first)');
    expect(fallbackSkill).toContain("| Updates (pending, release notes, install, skip) | Covered | updates |");
    expect(writeSafety).toContain("| `updates` | preview + entity re-read verify | no (updates not downgradable) | HA Backups (offered pre-install for core/OS) |");
    expect(architectureDoc).toContain("updates/SKILL.md");
  });

  it("teaches file-based relay payloads and localized output slots", () => {
    expect(updatesSkill).toMatch(/--(data-file|body-file|out)\b/);
    expect(updatesSkill).toContain("skills/ha-nova/output-rules.md");
    expect(updatesSkill).toContain("Use stable localized slot labels in this order; omit empty slots.");
  });
});
