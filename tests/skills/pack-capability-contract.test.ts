// tests/skills/pack-capability-contract.test.ts
//
// Capability unlocks from packs #519, #520, #531 (2026-08-20): HA/relay
// support existed, only skill text was missing. Each describe pins one item
// so the unlock cannot silently regress into a refusal — and pins the safety
// boundary the unlock deliberately keeps.
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { resolve } from "path";

const read = (p: string): string =>
  readFileSync(resolve(__dirname, "../../", p), "utf-8");
const flat = (text: string): string => text.replace(/\s+/g, " ");

describe("calendar recurring-event create (#519 C1-05)", () => {
  const calendar = flat(read("skills/calendar/SKILL.md"));

  it("no longer frames recurrence creation as an HA limitation", () => {
    expect(calendar).not.toContain("recurring-event creation (finish in the Home Assistant UI");
    expect(flat(read("docs/reference/skill-architecture.md"))).not.toContain(
      "recurring creation stays in the Home Assistant UI",
    );
    expect(flat(read("skills/ha-nova/relay-api.md"))).not.toContain(
      "use the Home Assistant UI for recurring creation",
    );
  });

  it("routes recurring creates through the WS create sibling with rrule", () => {
    expect(calendar).toContain('"type":"calendar/event/create"');
    expect(calendar).toContain("RFC 5545 `rrule`");
    // The REST service really has no recurrence field — the honest split stays.
    expect(calendar).toContain("the REST service has no recurrence field");
    expect(calendar).toContain("non-recurring creates keep the REST service");
  });

  it("verifies a recurring create by re-reading the window and reporting a series", () => {
    expect(calendar).toContain("new instances sharing one `uid`");
    expect(calendar).toContain("report them as ONE series");
    expect(calendar).toContain("instances beyond the window were not verified");
  });
});

describe("camera before/after actuation frames (#519 C1-12)", () => {
  const camera = flat(read("skills/camera/SKILL.md"));

  it("names exactly one exception to the repeated-frames ban", () => {
    expect(camera).toContain("Do not fetch frames repeatedly to simulate a live view. ONE named exception");
    expect(camera).toContain("bound to ONE named actuation");
    expect(camera).toContain("2-3 frames");
    expect(camera).toContain("each timestamped and attributed to its phase");
  });

  it("keeps the live-view ban itself", () => {
    expect(camera).toContain("still never a live view");
    expect(camera).toContain("No continuous polling");
  });
});

describe("device-level diagnostics (#519 C1-13)", () => {
  const diagnose = flat(read("skills/diagnose/SKILL.md"));

  it("adds the device diagnostics path beside the config-entry one", () => {
    expect(diagnose).toContain("/api/diagnostics/config_entry/<entry_id>/device/<device_id>");
    // The config-entry path stays.
    expect(diagnose).toContain("GET /api/diagnostics/config_entry/<entry_id>`");
  });

  it("says redaction is integration-controlled", () => {
    expect(diagnose).toContain("redaction is integration-controlled");
    expect(diagnose).toContain("potentially sensitive");
  });
});

describe("strategy dashboard config (#519 C1-03 increment 1)", () => {
  const dashboard = flat(read("skills/dashboard/SKILL.md"));

  it("allows a strategy object as the ENTIRE config of a created shell", () => {
    expect(dashboard).toContain('`{"strategy":{"type":"areas"}}`');
    expect(dashboard).toContain('`{"strategy":{"type":"original-states"}}`');
    expect(dashboard).toContain("as the ENTIRE config");
    expect(dashboard).toContain("Home Assistant generates all views and cards");
  });

  it("skips the card allowlist only because no cards are authored", () => {
    expect(dashboard).toContain("the card allowlist is not consulted because no cards are authored");
    expect(dashboard).toContain("Freeform card authoring beyond the allowlist stays refused");
  });

  it("treats strategizing a populated dashboard as destructive", () => {
    expect(dashboard).toContain("switching a populated dashboard TO a strategy discards its stored views");
    // View authoring (increment 2) is NOT unlocked by this item.
    expect(dashboard).toContain("view create/delete/reorder");
  });
});

describe("helper families generic_thermostat + switch_as_x (#531 P4-05)", () => {
  const helper = flat(read("skills/helper/SKILL.md"));
  const flowSchemas = flat(read("skills/ha-nova/helper-flow-schemas.md"));
  const fallbackAll = flat(
    read("skills/fallback/SKILL.md") + "\n" + read("skills/fallback/relay-ready.md"),
  );

  it("promotes both domains into helper's supported config-entry family", () => {
    expect(helper).toContain("CRUD support for 12 domains:");
    expect(helper).toContain("`generic_thermostat`, `switch_as_x`");
    expect(fallbackAll).toContain("Helpers (9 storage + 12 config-entry) | Covered | helper");
    // Delete allowlist follows the family.
    expect(helper).toContain(
      "allowed here: `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`, `generic_thermostat`, `switch_as_x`",
    );
  });

  it("anchors the promoted domains to the live form, honestly uninventoried", () => {
    expect(helper).toContain("promoted without an observed field inventory");
    expect(flowSchemas).toContain("## generic_thermostat");
    expect(flowSchemas).toContain("## switch_as_x");
    expect(flowSchemas).toContain("hides the original switch entity");
    expect(flowSchemas).toContain("The target domain is create-only");
    // Live-schema preflight is the contract that makes an uninventoried flow safe.
    expect(helper).toContain("every field comes from the live form per `skills/ha-nova/live-schema-preflight.md`");
  });

  it("removes both domains from every fallback-owned list", () => {
    expect(helper).not.toContain("`trend`, `random`, `filter`, `generic_thermostat`");
    // Line-level not-contains live in helper-contract.test.ts; pin the
    // remaining fallback family here so the two lists cannot drift apart.
    expect(fallbackAll).toContain(
      "**Supported types in this fallback section:** `trend`, `random`, `filter`, `generic_hygrostat`",
    );
  });
});

describe("assist pipeline create (#531 P4-06)", () => {
  const assist = flat(read("skills/assist/SKILL.md"));

  it("creates pipelines with clone-preferred-with-a-different-engine as the named default", () => {
    expect(assist).toContain("WS `assist_pipeline/pipeline/create`");
    expect(assist).toContain("clone-preferred-with-a-different-engine");
    expect(assist).toContain("swap only the engine(s) the user asked for");
    expect(assist).toContain("labeling cloned vs changed");
  });

  it("verifies by re-reading the pipeline list and never flips preferred implicitly", () => {
    expect(assist).toContain("verify by re-reading the pipeline list");
    expect(assist).toContain("Creating never changes the preferred pipeline");
  });
});

describe("assist custom sentences ownership (#519 C1-08)", () => {
  const assist = flat(read("skills/assist/SKILL.md"));
  const fallback = flat(read("skills/fallback/SKILL.md"));
  const relayReady = flat(read("skills/fallback/relay-ready.md"));

  it("gives assist the owning workflow and points at fallback's file mechanics", () => {
    expect(assist).toContain("## Custom Sentences");
    expect(assist).toContain("This skill owns the workflow");
    expect(assist).toContain("`skills/fallback/relay-ready.md` → Assist Custom Sentences");
    expect(assist).toContain("do not duplicate it here");
    expect(relayReady).toContain("Owned by `ha-nova:assist` → Custom Sentences");
  });

  it("keeps the ordered safety chain: check_config, right reload, live test, split rollback", () => {
    expect(assist).toContain("`POST /api/config/core/check_config` FIRST");
    expect(assist).toContain("`conversation.reload` reloads the sentence matcher");
    expect(assist).toContain("only the FIRST-ever top-level `intent_script:` block takes a Home Assistant restart");
    expect(assist).toContain("never claim success without this test");
    expect(assist).toContain("drops its stale handler with `intent_script.reload`");
  });

  it("routes the capability-map row to assist as owner", () => {
    expect(fallback).toContain(
      "| Assist custom sentences / intent scripts | Covered | assist",
    );
  });

  it("advertises custom sentences on the discovery surfaces", () => {
    // Review batch: description and dispatch table are the routing surfaces —
    // a lane only reachable after selection is a lane discovery never routes to.
    expect(assist).toContain("and teaching Assist custom sentences");
    expect(flat(read("skills/ha-nova/SKILL.md"))).toContain(
      "| teach Assist a new phrase / custom sentence | `ha-nova:assist` |",
    );
  });
});

describe("integration options/reconfigure/reload scope (#520 C1-04)", () => {
  const setup = flat(read("skills/integration-setup/SKILL.md"));
  const fallback = flat(read("skills/fallback/SKILL.md"));

  it("owns options flows under the live-schema preflight contract", () => {
    expect(setup).toContain("### Options, Reconfigure, and Reload");
    expect(setup).toContain('POST `{"handler":"<entry_id>"}` to `/api/config/config_entries/options/flow`');
    expect(setup).toContain("preserve unrelated existing options");
    expect(setup).toContain("never submit a field the live step does not expose");
    expect(setup).toContain("Verify by reopening the options flow and reading the changed fields back");
    // Review batch: the verification flow is agent-started and transient.
    expect(setup).toContain("Then DELETE the reopened verification flow");
  });

  it("guards reconfigure against silently starting an add flow", () => {
    expect(setup).toContain('POST `{"handler":"<domain>","entry_id":"<entry_id>"}`');
    // Hedged wording: unsupported reconfigure is detected live, not asserted.
    expect(setup).toContain("treat any non-reconfigure form (a plain add form included) as unsupported: DELETE the flow and stop");
    expect(setup).toContain("the same `entry_id` persists and the before/after `config_entries/get` diff shows NO new entry");
  });

  it("generalizes reload with the disruption disclosure", () => {
    expect(setup).toContain("reload re-runs setup and briefly drops the entry's entities");
    expect(setup).toContain("report the entry's ACTUAL state");
    // The exclusion line shrinks but keeps its remaining boundary.
    expect(setup).toContain("integration subentry, enable/disable, or delete operations");
  });

  it("moves the fallback rows with the scope", () => {
    expect(fallback).toContain("Integration entry options / reconfigure (standard config-entry flows) | Covered | integration-setup");
    expect(fallback).toContain("Integration entry enable/disable | External");
    expect(fallback).not.toContain("enable/disable, options, reconfigure");
  });

  it("routes the new lanes from the dispatch table", () => {
    expect(flat(read("skills/ha-nova/SKILL.md"))).toContain(
      "or change an integration's options, reconfigure it, or reload its entry | `ha-nova:integration-setup` |",
    );
  });
  it("selects reconfigure mode via entry_id at flow start", () => {
    const isk = flat(read("skills/integration-setup/SKILL.md"));
    expect(isk).toContain("`entry_id` selects reconfigure mode (a body `source` key is ignored by the API)");
    expect(isk).toContain("without `entry_id` Home Assistant starts a normal ADD flow");
  });
  it("hands secret steps of options/reconfigure flows to the entry's own UI", () => {
    expect(flat(read("skills/integration-setup/SKILL.md"))).toContain(
      "For an agent-started OPTIONS or RECONFIGURE flow, DELETE the transient flow",
    );
  });
});
