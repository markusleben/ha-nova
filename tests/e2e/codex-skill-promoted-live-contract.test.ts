import { constants, readFileSync, statSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("codex promoted skill live e2e contract", () => {
  it("ships an executable promoted-skill live harness", () => {
    const file = "scripts/e2e/codex-ha-nova-promoted-live-e2e.py";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env python3")).toBe(true);
    expect(content).toContain(
      'SCENARIO_ORDER = (\n    "dashboard_storage_lifecycle",\n    "dashboard_card_flow",\n    "dashboard_resource_flow",\n    "dashboard_delete_token",\n    "dashboard_delete_reject_natural",\n    "organize_category_flow",\n    "organize_floor_area_flow",\n    "organize_label_entity_flow",\n    "organize_category_delete_token",\n    "history_timeline",\n    "history_statistics",\n)'
    );
    expect(content).toContain('OUTPUT_DIR = Path(os.environ.get("OUTPUT_DIR"');
    expect(content).toContain("ACTIVE_OUTPUT_DIR = OUTPUT_DIR.resolve()");
    expect(content).toContain('SCENARIO_TIMEOUT_SEC = int(os.environ.get("PROMOTED_E2E_SCENARIO_TIMEOUT_SEC", "420"))');
    expect(content).toContain("codex");
    expect(content).toContain("ha-nova");
    expect(content).toContain("--ephemeral");
    expect(content).toContain("--json");
    expect(content).toContain("danger-full-access");
    expect(content).toContain('"stdin": subprocess.DEVNULL');
    expect(content).toContain('require_cmd("trash")');
    expect(content).toContain("NOVA_PROMOTED_SKILL_RESULT");
    expect(content).toContain('base_prompt(ROOT / "skills" / "dashboard" / "SKILL.md", "dashboard"');
    expect(content).toContain('base_prompt(ROOT / "skills" / "organize" / "SKILL.md", "organize"');
    expect(content).toContain('base_prompt(ROOT / "skills" / "history" / "SKILL.md", "history"');
    expect(content).toContain("Do not open installed skill copies under `~/.local/share/ha-nova/skills`");
    expect(content).toContain("Do not browse the web and do not use external docs or research tools.");
    expect(content).toContain("Do not run project helper scripts.");
    expect(content).toContain("Do not search the repo for implementation hints beyond those three allowed files.");
    expect(content).toContain("Do not wrap relay commands in ad-hoc debug shells, loops, or extra shell variables.");
    expect(content).toContain("Do not emit progress updates, meta narration, or tool transcript fragments.");
    expect(content).toContain("storage-dashboard create + metadata-update + config-save proof");
    expect(content).toContain("inspect the dashboard structure first");
    expect(content).toContain("move the existing card titled");
    expect(content).toContain("Keep the final view with exactly these two cards in this order");
    expect(content).toContain("List the current Lovelace resources first.");
    expect(content).toContain("Then create a new Lovelace resource");
    expect(content).toContain("update the same resource");
    expect(content).toContain("If the first config read on the fresh dashboard fails because no config exists yet");
    expect(content).toContain("do not resend `url_path` or `mode`");
    expect(content).toContain("Do not probe any other dashboard's config to infer behavior for this target.");
    expect(content).toContain("The delete preview was already shown in the previous turn");
    expect(content).toContain("The user's current reply is only `yes`.");
    expect(content).toContain("Use Relay WebSocket calls only for this dashboard flow.");
    expect(content).toContain("Create a category in scope");
    expect(content).toContain("Create and verify richer floor and area metadata in one careful flow.");
    expect(content).toContain("Create and verify richer label and entity metadata in one careful flow.");
    expect(content).toContain("add the created label");
    expect(content).toContain("clear aliases back to an empty list");
    expect(content).toContain("every category registry call in this scenario must include the provided scope");
    expect(content).toContain("verify after each change");
    expect(content).toContain("use `categories` with that exact scope set to `null`");
    expect(content).toContain("Keep this flow read-only and bounded. Do not omit `end_time`.");
    expect(content).toContain("Use Relay core GET only; do not use Relay WebSocket in this history flow.");
    expect(content).toContain("using statistics, not a wide raw history scan");
    expect(content).toContain("Use the recorder statistics WebSocket command only for the statistics read.");
    expect(content).toContain("do not invent a `--jq` flag");
    expect(content).toContain("do not run raw `tonumber` reductions across the whole series");
    expect(content).toContain("Do not add fragile timestamp-gap parsing.");
    expect(content).toContain("Do not build complex jq expressions just to recover min/max event timestamps;");
    expect(content).toContain("use `.data.body[0]` for the history series and `.data.body` for logbook entries");
    expect(content).toContain("Do not probe `.[0]` or `.[0][0]`.");
    expect(content).toContain("lovelace/dashboards/create");
    expect(content).toContain("lovelace/dashboards/update");
    expect(content).toContain("lovelace/dashboards/delete");
    expect(content).toContain("lovelace/config/save");
    expect(content).toContain("lovelace/resources/create");
    expect(content).toContain("lovelace/resources/update");
    expect(content).toContain("config/category_registry/create");
    expect(content).toContain("config/category_registry/update");
    expect(content).toContain("config/category_registry/delete");
    expect(content).toContain("config/area_registry/create");
    expect(content).toContain("config/floor_registry/create");
    expect(content).toContain("config/label_registry/create");
    expect(content).toContain("config/entity_registry/update");
    expect(content).toContain("config/entity_registry/get");
    expect(content).toContain("recorder/statistics_during_period");
    expect(content).toContain("/api/history/period/");
    expect(content).toContain("/api/logbook/");
    expect(content).toContain("dashboard_view_title_mismatch");
    expect(content).toContain("dashboard_card_order_mismatch");
    expect(content).toContain("dashboard_updated_resource_missing");
    expect(content).toContain("dashboard_delete_should_not_run");
    expect(content).toContain("final_floor_missing");
    expect(content).toContain("final_area_missing");
    expect(content).toContain("final_label_missing");
    expect(content).toContain("organize_floor_area_flow");
    expect(content).toContain("organize_label_entity_flow");
    expect(content).toContain('if "organize_category_flow" in requested or "organize_category_delete_token" in requested:');
    expect(content).toContain('entity[-_]get');
    expect(content).toContain("entity_category_scope_not_cleared_after_delete");
    expect(content).toContain("bounded_history_call_missing");
    expect(content).toContain("bounded_logbook_call_missing");
    expect(content).toContain("statistics_ws_call_missing");
    expect(content).toContain("unexpected_mutating_core_usage");
    expect(content).toContain("helper_script_usage_detected");
    expect(content).toContain("proactive_doctor_or_ready_detected");
    expect(content).toContain("health_preflight_before_action");
    expect(content).toContain("installed_skill_copy_accessed");
    expect(content).toContain("forbidden_dashboard_config_delete");
    expect(content).toContain("RESULTS_FILE");
    expect(content).toContain("SUMMARY_FILE");
    expect(content).toContain("cleanup_stale_promoted_dashboards()");
    expect(content).toContain("cleanup_stale_promoted_resources()");
    expect(content).toContain("cleanup_stale_promoted_categories()");
    expect(content).toContain("cleanup_stale_promoted_organize_metadata()");
    expect(content).toContain("cleanup_promoted_output_dirs()");
    expect(content).toContain("cleanup_entity_category_scope(scope)");
    expect(content).toContain("if output_dir.resolve() != ACTIVE_OUTPUT_DIR");
    expect(content).not.toContain("sensor.wetterstation_actual_temperature");
    expect(content).toContain("NEUTRAL_HISTORY_ENTITY_RE = re.compile(");
    expect(content).toContain("neutral_candidates = [");
    expect(content).toContain("if entity_id and NEUTRAL_HISTORY_ENTITY_RE.search(entity_id)");
    expect(content).toContain("][:40]");
    expect(content).toContain("neutral_candidates = [entity_id for entity_id in candidates if NEUTRAL_HISTORY_ENTITY_RE.search(entity_id)]");
    expect(content).not.toContain("person.markus");
  });

  it("ships an executable promoted-skill suite harness", () => {
    const file = "scripts/e2e/codex-ha-nova-promoted-live-suite.py";
    const stats = statSync(file);
    const content = readFileSync(file, "utf8");

    expect((stats.mode & constants.S_IXUSR) !== 0).toBe(true);
    expect(content.startsWith("#!/usr/bin/env python3")).toBe(true);
    expect(content).toContain('SCENARIO_SCRIPT = ROOT / "scripts" / "e2e" / "codex-ha-nova-promoted-live-e2e.py"');
    expect(content).toContain("SMOKE_SCENARIOS = (");
    expect(content).toContain('SUITE_SCENARIO_TIMEOUT_SEC = int(os.environ.get("PROMOTED_SUITE_SCENARIO_TIMEOUT_SEC", "540"))');
    expect(content).toContain('SUITE_CLEANUP_TIMEOUT_SEC = int(os.environ.get("PROMOTED_SUITE_CLEANUP_TIMEOUT_SEC", "120"))');
    expect(content).toContain("stop_process_group(");
    expect(content).toContain('"start_new_session"] = True');
    expect(content).toContain('run_python(["--cleanup-only"], timeout_sec=SUITE_CLEANUP_TIMEOUT_SEC)');
    expect(content).toContain("collect_residue()");
    expect(content).toContain("PROMOTED_SUITE_KEEP_OUTPUT");
    expect(content).toContain('if not parsed.get("ok"):');
    expect(content).toContain('raise subprocess.CalledProcessError(1, "ha-nova relay ws", output=raw)');
  });

  it("exposes an npm script for the promoted live harness", () => {
    const pkg = JSON.parse(readFileSync("package.json", "utf8")) as {
      scripts?: Record<string, string>;
    };

    expect(pkg.scripts?.["e2e:skill:codex:promoted"]).toBe(
      "node scripts/e2e/run-python-script.mjs scripts/e2e/codex-ha-nova-promoted-live-suite.py"
    );
    expect(pkg.scripts?.["e2e:skill:codex:promoted:scenario"]).toBe(
      "node scripts/e2e/run-python-script.mjs scripts/e2e/codex-ha-nova-promoted-live-e2e.py"
    );
    expect(pkg.scripts?.["e2e:skill:codex:promoted:smoke"]).toBe(
      "node scripts/e2e/run-python-script.mjs scripts/e2e/codex-ha-nova-promoted-live-suite.py --smoke"
    );
  });
});
