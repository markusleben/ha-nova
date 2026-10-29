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

  it("grounds authored cards in documentation or existing usage", () => {
    expect(dashboard).toContain("with options from that card's page only");
    expect(dashboard).toContain("or copy the shape of its existing usage in the user's dashboards");
  });

  it("treats strategizing a populated dashboard as destructive", () => {
    expect(dashboard).toContain("Switching a populated dashboard TO a strategy discards its stored views");
    expect(dashboard).toContain("name that in the preview");
  });
});

describe("helper families generic_thermostat + switch_as_x (#531 P4-05)", () => {
  const helper = flat(read("skills/helper/SKILL.md"));
  const flowSchemas = flat(read("skills/ha-nova/helper-flow-schemas.md"));
  const fallbackAll = flat(
    read("skills/fallback/SKILL.md") + "\n" + read("skills/fallback/relay-ready.md"),
  );

  it("promotes both domains into helper's supported config-entry family", () => {
    expect(helper).toContain("CRUD support for 16 domains:");
    expect(helper).toContain("`generic_thermostat`, `switch_as_x`");
    expect(fallbackAll).toContain("Helpers (9 storage + 16 config-entry) | Covered | helper");
    // Delete allowlist follows the family.
    expect(helper).toContain(
      "allowed here: `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`, `generic_thermostat`, `switch_as_x`",
    );
    // The shared relay contract's allowlist must agree with the promotion.
    const relayApi = flat(read("skills/ha-nova/relay-api.md"));
    expect(relayApi).toContain("- `generic_thermostat`");
    expect(relayApi).toContain("- `switch_as_x`");
  });

  it("anchors the promoted domains to the live form, honestly uninventoried", () => {
    expect(helper).toContain("promoted without an observed field inventory");
    expect(flowSchemas).toContain("## generic_thermostat");
    expect(flowSchemas).toContain("## switch_as_x");
    expect(flowSchemas).toContain("hides the original switch entity");
    expect(flowSchemas).toContain("The target domain is create-only");
    // Live-schema preflight is the contract that makes an uninventoried flow safe.
    expect(helper).toContain("every field comes from the live form per `../ha-nova/live-schema-preflight.md`");
  });

  it("removes both domains from every fallback-owned list", () => {
    expect(helper).not.toContain("`trend`, `random`, `filter`, `generic_thermostat`");
    // Line-level not-contains live in helper-contract.test.ts; pin the
    // remaining fallback family here so the two lists cannot drift apart.
    expect(fallbackAll).not.toContain("Other Config-Entry Helpers -- RELAY-READY");
    for (const domain of ["trend", "random", "filter", "generic_hygrostat"]) {
      expect(helper).toContain(`\`${domain}\``);
    }
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
    expect(assist).toContain("`../fallback/relay-ready.md` → Assist Custom Sentences");
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
    expect(setup).toContain("### Subentries");
  });

  it("moves the fallback rows with the scope", () => {
    expect(fallback).toContain("Integration entry options / reconfigure (standard config-entry flows) | Covered | integration-setup");
    expect(fallback).toContain("Integration entry enable/disable, rename, system options, ignoring a discovery | Covered | integration-setup");
    expect(fallback).not.toContain("enable/disable, options, reconfigure");
  });

  it("routes the new lanes from the dispatch table", () => {
    expect(flat(read("skills/ha-nova/SKILL.md"))).toContain(
      "add/remove, enable/disable, rename an integration, manage subentries, ignore discovery, change options, reconfigure, reload, reauthenticate or recover invalid credentials | `ha-nova:integration-setup` |",
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

describe("HA UI parity flow ownership", () => {
  const setup = read("skills/integration-setup/SKILL.md");
  const health = read("skills/health/SKILL.md");
  const write = read("skills/write/SKILL.md");
  const fallback = read("skills/fallback/SKILL.md") + read("skills/fallback/relay-ready.md");
  it("owns repair start and live steps with destructive tier escalation", () => {
    for (const text of [
      "/api/repairs/issues/fix", "issue_id", "is_fixable", "translation",
      "typed:true", "live-schema-preflight.md", "next_flow",
    ]) {
      expect(health).toContain(text);
    }
    expect(health).not.toContain("Never call repair fix flows");
    expect(health).toContain("Never call delete issue commands");
    const preflight = read("skills/ha-nova/live-schema-preflight.md");
    for (const text of ["frontend/get_translations", '"category":"issues"', ".data.resources", "issue.translation_key || issue.issue_id", "fix_flow.step.<step_id>.description", "Missing/unclear descriptions"]) {
      expect(preflight).toContain(text);
    }
  });

  it("uses the standard subentry API and checks identity after each mutation", () => {
    for (const text of [
      "config_entries/subentries/list", "config_entries/subentries/update",
      "config_entries/subentries/delete", "/api/config/config_entries/subentries/flow",
      "supported_subentry_types", "supports_reconfigure", "config_subentry_id",
      "exactly one new", "reopened live values", "Parent and unrelated subentries must survive",
    ]) {
      expect(setup).toContain(text);
    }
    expect(setup).toContain("parent owns a lock, alarm panel, or access cover");
    expect(setup).toContain("normalized device subentry membership");
    const membership = read("skills/ha-nova/membership-resolution.md");
    expect(membership).toContain("config_entries_subentries[entry_id]");
    expect(membership).toContain("Only when absent");
    expect(membership).toContain("stop before a destructive preview with unresolved scope");
  });

  it("moves entry removal out of fallback without losing the impact scan", () => {
    for (const text of [
      "typed raw DELETE", "/api/config/config_entries/entry/<entry_id>",
      "FULL entity/device registries", "EACH owned device AND entity",
      "event-listener", "unindexed dashboard/template", "no auto-undo",
    ]) {
      expect(setup).toContain(text);
    }
    expect(fallback).not.toContain("### Integration Entry Lifecycle -- RELAY-READY");
  });

  it("moves blueprints to write and expands inputs before normal config planning", () => {
    for (const text of [
      "blueprint/import", "blueprint/save", "blueprint/substitute", "blueprint/delete",
      "raw_data", "validation_errors", "source_url", "substituted_config",
      "use_blueprint", "plan write --create", "automation_blueprint", "script_blueprint",
    ]) {
      expect(write).toContain(text);
    }
    expect(fallback).not.toContain("### Blueprints -- RELAY-READY");
  });
});

describe("HA UI parity batch 1", () => {
  it("pins each new write and its safety guidance in the owning skill", () => {
    const integration = read("skills/integration-setup/SKILL.md");
    for (const type of ["config_entries/disable", "config_entries/update", "config_entries/ignore_flow"]) {
      expect(integration).toContain(type);
    }
    expect(integration).toContain("lock, alarm panel, or access cover is refused");
    expect(integration).toContain("Polling changes reload loaded entries: preview the interruption");
    expect(integration).toContain("Same access refusals; `require_restart: true`");
    expect(integration).toContain("config_entries/get");
    const admin = read("skills/admin/SKILL.md");
    for (const pin of ["config/auth/update", "group_ids", "is_active", "system-admin", "config/core/update", "config/core/detect", "get_config", "Time zone, units, and language take the one-time code", "the owner, a system account, and the relay's own account"]) {
      expect(admin).toContain(pin);
    }
    const health = read("skills/health/SKILL.md");
    for (const pin of ["repairs/ignore_issue", "Ignoring hides, never fixes", "re-reading its `ignored` flag", "ha-nova plan raw", "ha-nova apply"]) {
      expect(health).toContain(pin);
    }
    const blueprints = read("skills/write/SKILL.md");
    for (const pin of ["blueprint/delete", "automation_blueprint", "script_blueprint", "search/related", "Home Assistant refuses a blueprint in use", "typed raw plan"]) {
      expect(blueprints).toContain(pin);
    }
    const assist = read("skills/assist/SKILL.md");
    for (const pin of ["homeassistant/expose_new_entities/get", "homeassistant/expose_new_entities/set", "expose_new", "assist_pipeline/pipeline_debug/list", "assist_pipeline/pipeline_debug/get", "pipeline_run_id"]) {
      expect(assist).toContain(pin);
    }
    expect(assist).toContain("automatic exposure with `homeassistant/expose_new_entities/get` (same `assistant`, compare `expose_new`)");
    for (const type of ["assist_pipeline/pipeline_debug/list", "assist_pipeline/pipeline_debug/get"]) {
      expect(read("skills/diagnose/SKILL.md")).toContain(type);
    }
  });
});

describe("HA UI parity batch 3a", () => {
  it("pins backup settings, Labs, analytics, and statistics import in their owning skills", () => {
    const backup = read("skills/backup/SKILL.md");
    for (const pin of ["backup/config/update", "backup/config/info", "replace whole: send every field from the read", "A retention takes the one-time code", "the CLI refuses both", "no saved key", "ha-nova plan raw"]) {
      expect(backup).toContain(pin);
    }
    expect(backup).not.toContain("changing automatic-backup settings or encryption keys — HA UI");
    const admin = read("skills/admin/SKILL.md");
    for (const pin of ["labs/list", "labs/update", "never `create_backup`", "analytics/preferences", "only changed keys", "takes the one-time code"]) {
      expect(admin).toContain(pin);
    }
    expect(read("skills/maintenance/SKILL.md")).toContain("recorder/import_statistics");
    const reference = read("skills/maintenance/maintenance-reference.md");
    for (const pin of ["## Statistics Import", "`mean_type`", "`unit_class`", "overwrites its stored rows for the same hours", "`source` is `recorder`", "statistics_unit_of_measurement", "Sums must join"]) {
      expect(reference).toContain(pin);
    }
    const fallback = read("skills/fallback/SKILL.md");
    expect(fallback).toContain("| Labs features, analytics sharing | Covered | admin |");
    expect(fallback).toContain("| Backups (status, create, inspect, delete, automatic settings) | Covered | backup |");
  });
});
