import {
  constants,
  existsSync,
  readFileSync,
  readdirSync,
  statSync,
} from "node:fs";

import { describe, expect, it } from "vitest";


// The -- RELAY-READY sections live in fallback's split file, which fallback
// loads. A negative assertion must cover both, or it cannot fail.
const relayReadySplit = readFileSync("skills/fallback/relay-ready.md", "utf-8");

describe("ha-nova contract", () => {
  it("provides context skill with skill discovery table", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("ha-nova:write");
    expect(context).toContain("ha-nova:read");
    expect(context).toContain("ha-nova:helper");
    expect(context).toContain("ha-nova:dashboard");
    expect(context).toContain("ha-nova:organize");
    expect(context).toContain("ha-nova:history");
    expect(context).toContain("ha-nova:health");
    expect(context).toContain("ha-nova:calendar");
    expect(context).toContain("ha-nova:service-call");
    expect(context).toContain("ha-nova:entity-discovery");
    expect(context).toContain("ha-nova:onboarding");
    expect(context).toContain("ha-nova:review");
    expect(context).toContain("ha-nova:fallback");
    expect(context).toContain("Sub-skills are discovered independently");
    expect(context).not.toContain(".agents/skills/");
    expect(context).not.toContain("core/intents.md");
    expect(context).not.toContain("Lazy Discovery Protocol");
    expect(context).not.toContain("Orchestration Hard Gate");
  });

  it("keeps the output-contract files on the write-flow latency allowlist (#494)", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    // The Latency Policy allowlist must include the files that define the
    // output contract — excluding them silently degrades preview Cards and
    // permits hand-written diffs.
    const latencySection = context.split("keep main-thread file reads minimal:")[1] ?? "";
    const allowlist = latencySection.split("- No proactive doctor")[0];
    expect(allowlist).toContain("`skills/ha-nova/output-rules.md`");
    expect(allowlist).toContain("`skills/ha-nova/write-safety.md`");
  });

  it("routes dashboard, organization, and history intents to dedicated skills", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain(
      "| list, show, read dashboards, Lovelace resources, or dashboard structure | `ha-nova:dashboard` |",
    );
    expect(context).toContain(
      "| create, update, delete storage dashboards / Lovelace configs / Lovelace resources / dashboard cards | `ha-nova:dashboard` |",
    );
    expect(context).toContain(
      "| organize areas, floors, labels, categories, devices, entities | `ha-nova:organize` |",
    );
    expect(context).toContain(
      "| assign or remove entity categories | `ha-nova:organize` |",
    );
    expect(context).toContain(
      "| show history, logbook timelines, or long-term statistics | `ha-nova:history` |",
    );
    expect(context).toContain(
      "| check home status, repairs, system health, integration issues, unavailable entities, or low batteries | `ha-nova:health`",
    );
    expect(context).toContain(
      "| list calendars; read, create, update, or delete calendar events | `ha-nova:calendar` |",
    );
    expect(context).toContain(
      '"Show my main dashboard"** → `ha-nova:dashboard`',
    );
    expect(context).toContain(
      '"Create a dashboard called Test Board"** → `ha-nova:dashboard`',
    );
    expect(context).toContain(
      '"Delete the Test dashboard"** → `ha-nova:dashboard`',
    );
    expect(context).toContain(
      '"Add a markdown card to my dashboard"** → `ha-nova:dashboard`',
    );
    expect(context).toContain(
      '"List my Lovelace resources"** → `ha-nova:dashboard`',
    );
    expect(context).toContain(
      '"Move this sensor to Area Alpha"** → `ha-nova:organize`',
    );
    expect(context).toContain(
      '"Put this sensor in category Category Alpha"** → `ha-nova:organize`',
    );
    expect(context).toContain(
      '"Add an alias to this area"** → `ha-nova:organize`',
    );
    expect(context).toContain(
      '"Show history for sensor X"** → `ha-nova:history`',
    );
    expect(context).toContain(
      '"Show temperature trends for the last month"** → `ha-nova:history`',
    );
    expect(context).toContain(
      '"Are there any repair issues?"** → `ha-nova:health`',
    );
    expect(context).toContain(
      '"Why are devices unavailable?"** → `ha-nova:health`',
    );
    expect(context).toContain('"Show my calendars"** → `ha-nova:calendar`');
    expect(context).toContain(
      '"What\'s on my calendar this week?"** → `ha-nova:calendar`',
    );
    expect(context).toContain(
      '"Remove this entity from Home Assistant"** → `ha-nova:maintenance` (dead registry entries only; live entities get disabled via `ha-nova:organize`)',
    );
    expect(context).toContain(
      '"Detach this config entry from the device"** → `ha-nova:fallback`',
    );
    expect(context).not.toContain(
      '"Show history for sensor X"** → `ha-nova:fallback`',
    );
    expect(context).not.toContain(
      '"Modify my dashboard"** → `ha-nova:fallback`',
    );
    expect(context).not.toContain("energy, calendars, zones/persons/tags");
  });

  it("documents storage-only dashboard lifecycle, resources, and category ownership in dedicated skills", () => {
    const dashboard = readFileSync("skills/dashboard/SKILL.md", "utf8");
    const organize = readFileSync("skills/organize/SKILL.md", "utf8");

    expect(dashboard).toContain("create a new storage dashboard shell");
    expect(dashboard).toContain("delete an existing storage dashboard");
    expect(dashboard).toContain("list Lovelace resources");
    expect(dashboard).toContain("inspect the current dashboard structure");
    expect(dashboard).toContain(
      "create, update, and delete Lovelace resources",
    );
    expect(dashboard).toContain(
      "add, update, move, and delete cards inside existing views",
    );
    expect(dashboard).toContain(
      "only write/delete when the dashboard `mode` is `storage`",
    );
    expect(dashboard).toContain(
      "`dashboard_id` for `lovelace/dashboards/update|delete`",
    );
    expect(dashboard).toContain("`url_path` for `lovelace/config|save`");
    expect(dashboard).toContain("`lovelace/dashboards/create`");
    expect(dashboard).toContain("`lovelace/dashboards/update`");
    expect(dashboard).toContain("`lovelace/dashboards/delete`");
    expect(dashboard).toContain("`lovelace/resources`");
    expect(dashboard).toContain("`lovelace/resources/create`");
    expect(dashboard).toContain("`lovelace/resources/update`");
    expect(dashboard).toContain("`lovelace/resources/delete`");
    expect(dashboard).toContain(
      "only send changed metadata fields supported there: `title`, `icon`, `show_in_sidebar`, `require_admin`",
    );
    expect(dashboard).toContain(
      "do not resend `url_path`, `mode`, or unrelated config fields in the update payload",
    );
    expect(dashboard).toContain(
      "new cards may be created only from this built-in allowlist",
    );
    expect(dashboard).toContain(
      "existing custom cards may only be moved, deleted, or shallow-updated when the exact field already exists",
    );
    expect(dashboard).toContain(
      "Never probe a different dashboard's config just to infer behavior for the target dashboard.",
    );
    expect(dashboard).toContain(
      "Dashboard/resource/card delete uses exact confirmation code only",
    );
    expect(dashboard).toContain(
      "persisted card removal is destructive and requires exact confirmation code `confirm:<token>`",
    );
    expect(dashboard).toContain("Active Preview Confirmation");
    expect(dashboard).toContain(
      "Never use `lovelace/config/delete` as the dashboard delete path.",
    );
    expect(dashboard).not.toContain(
      "For create/delete or unrelated dashboard-adjacent admin work, hand off to `ha-nova:fallback`.",
    );

    expect(organize).toContain("- categories: list/create/update/delete");
    expect(organize).toContain(
      "- entity metadata updates: rename, move to area, assign/clear/add/remove labels, assign/remove categories, disable, hide, aliases",
    );
    expect(organize).toContain(
      "- device metadata updates: rename, move to area, assign/clear/add/remove labels, disable",
    );
    expect(organize).toContain(
      "area: `name`, `floor_id`, `icon`, `picture`, `aliases`",
    );
    expect(organize).toContain("floor: `name`, `level`, `icon`, `aliases`");
    expect(organize).toContain("label: `name`, `color`, `icon`, `description`");
    expect(organize).toContain("category: `name`, `icon`, exact `scope`");
    expect(organize).toContain(
      "every category registry call must include the exact `scope`",
    );
    expect(organize).toContain(
      "do not call `config/category_registry/list|create|update|delete` without `scope`",
    );
    expect(organize).toContain(
      'category assignment uses `categories: {"<scope>":"<category_id>"}`',
    );
    expect(organize).toContain(
      'category removal for one scope uses `categories: {"<scope>": null}`',
    );
    expect(organize).toContain(
      "do not send `categories: {}` when the goal is to clear one existing scoped category",
    );
    expect(organize).toContain("replace all labels");
    expect(organize).toContain("add labels");
    expect(organize).toContain("remove labels");
    expect(organize).toContain("clear labels");
    expect(organize).toContain(
      "- entity category assignment/removal per scope",
    );
    expect(organize).toContain(
      "`config/category_registry/list|create|update|delete`",
    );
    expect(organize).toContain(
      "category assignment/removal is entity-only in this skill",
    );
    expect(organize).toContain("Device categories do not exist");
    expect(organize).toContain(
      "Delete uses the typed confirmation code only, even for cleanup of items created earlier in the same session.",
    );
    expect(organize).toContain("Active Preview Confirmation");
    expect(organize).toContain("One category scope at a time.");
  });

  it("keeps sequential intent re-dispatch guidance in the context skill", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("re-evaluate intent once before continuing");
    expect(context).toContain(
      "config change on automation/script → `ha-nova:write`",
    );
    expect(context).toContain("helper change → `ha-nova:helper`");
    expect(context).toContain(
      "automation/script: `entity_id`, `unique_id`, current config",
    );
    expect(context).toContain(
      "storage-based family: `entity_id`, helper type, internal helper id when already known",
    );
    expect(context).toContain(
      "config-entry family: `entry_id`, domain, title, linked entities when already known",
    );
    expect(context).toContain("one skill at a time, never parallel");
  });

  it("uses App terminology in active fallback skill surfaces", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");

    expect(context).toContain('"How do I manage Apps?"');
    expect(context).not.toContain('"How do I manage add-ons?"');
    expect(fallback).toContain("Apps / Supervisor");
    expect(fallback).toContain("Settings > Apps");
    expect(fallback).not.toContain("Settings > Add-ons");
  });

  it("keeps relay-bootstrap runtime prerequisite and safety baseline in context skill", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("Runtime Prerequisite");
    expect(context).toContain("Relay-only auth model");
    expect(context).toContain("ha-nova relay health");
    expect(context).toContain("ha-nova setup");
    expect(context).not.toContain("git rev-parse");
    expect(context).not.toContain('eval "$(bash');
    expect(context).toContain("Quoting Reliability (Critical)");
    expect(context).toContain("--data-file");
    expect(context).toContain("--body-file");
    expect(context).toContain("--out");
    expect(context).toContain("ha-nova relay jq --file <result-file> length");
    expect(context).toContain("never chain commands with `&&` or `||`");
    expect(context).toContain("Never call external `jq`");
    expect(context).not.toContain("ask user to switch shell");
    expect(context).not.toContain(
      "If shell is not bash-compatible, stop and ask user to switch shell.",
    );
    expect(context).toContain("Safety Baseline");
    expect(context).toContain("confirm:<token>");
    expect(context).toContain("Do not ask user to paste tokens in chat.");
  });

  it("routes 'which build is loaded?' through the CLI version self-report", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("Build Self-Report");
    expect(context).toContain("`ha-nova version`");
    expect(context).toContain("local DEV build");
    // Must not regress to guessing the build from version.json / check-update.
    expect(context).toContain("source of truth");
  });

  it("defines structured summary + YAML response format", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");

    expect(context).toContain("Response Format");
    expect(context).toContain("Automation` or `Script");
    expect(context).toContain("Entities");
    expect(context).toContain("Triggers");
    expect(context).toContain("Actions");
    expect(context).toContain("full YAML config");
    expect(context).toContain("Next step");
  });

  it("documents the review confidence split in the shared output rules", () => {
    const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");

    expect(outputRules).toContain("Questions to consider");
    expect(outputRules).toContain("Suggestions");
  });

  it("keeps output localization rules code-free and mode-scoped", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const outputRules = readFileSync("skills/ha-nova/output-rules.md", "utf8");

    expect(context).toContain("skills/ha-nova/output-rules.md");
    expect(context).toContain("internal-code hiding");
    expect(outputRules).toContain("Localize section headings and labels");
    expect(outputRules).toContain("Keep Home Assistant state values");
    expect(outputRules).toContain("Never show internal check codes");
    expect(outputRules).toContain(
      "findings, summaries, clean states, pre-write verdicts",
    );
    expect(outputRules).toContain(
      "debugging help, brainstorming, and casual Q&A",
    );
    expect(outputRules).toContain("Summarize long inventories by counts");
    expect(outputRules).toContain(
      "If raw automation IDs, helper IDs, config IDs, or entity IDs",
    );
    // Standalone/bulk keep their full shape; post-write omits empty sections (less noise).
    expect(outputRules).toContain(
      "Standalone and bulk review keep their full shape",
    );
    expect(outputRules).toContain('"no issues found" result is useful');
    expect(outputRules).toContain('Do not print empty "none" buckets');
  });

  it("requires every skill entrypoint to reference the shared output rules", () => {
    const skillDirs = readdirSync("skills", { withFileTypes: true })
      .filter((entry) => entry.isDirectory())
      .map((entry) => entry.name);

    for (const skillDir of skillDirs) {
      const skillPath = `skills/${skillDir}/SKILL.md`;
      if (!existsSync(skillPath)) {
        continue;
      }

      const skill = readFileSync(skillPath, "utf8");
      expect(
        skill,
        `${skillPath} must reference shared output rules`,
      ).toContain("skills/ha-nova/output-rules.md");
    }
  });

  it("keeps new reference files present", () => {
    const files = [
      "skills/ha-nova/relay-api.md",
      "skills/ha-nova/best-practices.md",
      "skills/ha-nova/output-rules.md",
      "skills/ha-nova/config-body-filter.jq",
      "skills/ha-nova/bulk-patterns.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/agents/apply-agent.md",
      "skills/review/checks.md",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected file to exist: ${file}`).toBe(true);
    }
  });

  it("documents relay API contract centrally", () => {
    const relayApi = readFileSync("skills/ha-nova/relay-api.md", "utf8");
    const apiMatrix = readFileSync("docs/reference/ha-api-matrix.md", "utf8");
    const architecture = readFileSync(
      "docs/reference/skill-architecture.md",
      "utf8",
    );

    expect(relayApi).toContain("GET /health");
    expect(relayApi).toContain("POST /ws");
    expect(relayApi).toContain("POST /core");
    expect(relayApi).toContain("--data-file");
    expect(relayApi).toContain("--body-file");
    // One inline-vs-file contract (issue #587): the wording is shared verbatim
    // with the ws/core --help notes (cli/relay.go relayProxyHelpNotes, pinned
    // by TestRelayHelpContractMatchesSkillDoc on the Go side).
    expect(relayApi).toContain(
      "acceptable only for tiny, unambiguously read-only diagnostics",
    );
    expect(relayApi).toContain(
      "Mutations, complex bodies, reusable payloads, and cross-platform examples use `--data-file` (ws) / `--body-file` (core)",
    );
    // The former false absolutism must not come back: relay ws supports -d/--data.
    expect(relayApi).not.toContain("MUST use `--data-file`");
    expect(relayApi).toContain("--out");
    expect(relayApi).toContain("--jq-file <filter-file>");
    expect(relayApi).toContain("Supported flags are `-r`, `-e`, `-c`");
    expect(relayApi).toContain("JSON output is already compact");
    expect(relayApi).toContain("small single-input JSON filter");
    expect(relayApi).toContain("Do not use jq `input`/`inputs`");
    expect(relayApi).toContain("input_filename");
    expect(relayApi).not.toContain("relay jq -n");
    expect(relayApi).toContain("native JSON parser");
    expect(relayApi).toContain("Do not pass other GNU jq flags");
    expect(relayApi).not.toContain("ha-nova relay ws -d '{");
    expect(relayApi).toContain('{ "ok": true, "data": ... }');
    expect(relayApi).toContain("/api/config/automation/config/{id}");
    expect(relayApi).toContain("/api/config/script/config/{id}");
    expect(relayApi).toContain("UPSTREAM_WS_ERROR");
    expect(relayApi).toContain("Config writes/deletes");
    expect(relayApi).toContain("Post-Write Verification");
    expect(relayApi).toContain("Timeout and Retry Guidance");
    expect(relayApi).toContain("Safe Bulk Patterns");
    expect(relayApi).toContain("Do not rely on external `jq` pipes");
    expect(relayApi).toContain('`search/related` for `item_type:"area"`');
    expect(relayApi).toContain(
      "automation shortlist -> `(.data.automation // [])[]`",
    );
    expect(relayApi).toContain(
      "Compact entity registry (`config/entity_registry/list_for_display`): `data.entities[]`",
    );
    expect(relayApi).toContain(
      "Full entity registry (`config/entity_registry/list`): `data[]`",
    );
    expect(relayApi).toContain(
      "If the exact automation/script `entity_id` is known, use `config/entity_registry/get` directly.",
    );
    expect(relayApi).toContain(
      "Use `config/entity_registry/list_for_display` only for search or disambiguation by name.",
    );
    expect(relayApi).toContain(
      "Area registry (`config/area_registry/list`): `data[]` with canonical `area_id`",
    );
    expect(relayApi).toContain(
      "Do not invent a `--jq` flag for `ha-nova relay jq`",
    );
    expect(relayApi).not.toContain(
      "Full entity registry (`config/entity_registry/list`): `data.entities[]`",
    );
    expect(apiMatrix).toContain("Config Entry Flow");
    expect(apiMatrix).toContain(
      "WS | `config_entries/get` | Retrieve config-entry metadata",
    );
    expect(apiMatrix).toContain("`POST /api/config/config_entries/flow`");
    expect(apiMatrix).toContain(
      "`POST /api/config/config_entries/flow/{flow_id}`",
    );
    expect(apiMatrix).toContain(
      "`POST /api/config/config_entries/options/flow`",
    );
    expect(apiMatrix).toContain(
      "`POST /api/config/config_entries/options/flow/{flow_id}`",
    );
    expect(apiMatrix).toContain(
      "`DELETE /api/config/config_entries/entry/{entry_id}`",
    );
    expect(apiMatrix).toContain(
      "raw WS `config_entries/flow` did not succeed in this session",
    );
    expect(apiMatrix).toContain("Helper-owned config-entry domains");
    expect(apiMatrix).toContain("live-proven end-to-end subtype is `sensor`");
    expect(apiMatrix).toContain("`config/device_registry/remove_config_entry`");
    expect(apiMatrix).toContain("`lovelace/dashboards/list`");
    expect(apiMatrix).toContain("`lovelace/dashboards/create`");
    expect(apiMatrix).toContain(
      "`lovelace/dashboards/update` | Update dashboard metadata by `dashboard_id`",
    );
    expect(apiMatrix).toContain("`lovelace/dashboards/delete`");
    expect(apiMatrix).toContain(
      "`lovelace/config/delete` | Delete the selected dashboard config object",
    );
    expect(apiMatrix).toContain(
      "`lovelace/resources/create` | Create UI resource (`res_type`, `url`)",
    );
    expect(apiMatrix).toContain(
      "`lovelace/resources/update` | Update UI resource by `resource_id`",
    );
    expect(apiMatrix).toContain(
      "`lovelace/resources/delete` | Delete UI resource by `resource_id`",
    );
    expect(apiMatrix).toContain(
      "`recorder/statistics_during_period` | Bounded long-term statistics for eligible entities",
    );
    expect(apiMatrix).toContain(
      "`system_health/info` | System health finite event response (Skill opts into Relay `collect_events` until `finish`)",
    );
    expect(apiMatrix).toContain(
      'set one scope: `{"categories":{"<scope>":"<category_id>"}}`',
    );
    expect(apiMatrix).toContain(
      'clear one scope: `{"categories":{"<scope>":null}}`',
    );
    expect(apiMatrix).toContain(
      'do not rely on `{"categories":{}}` to clear an existing scoped category',
    );
    expect(apiMatrix).toContain(
      "every `config/category_registry/*` call must include `scope`",
    );
    expect(apiMatrix).not.toContain(
      "`lovelace/config/delete` | Delete dashboard",
    );
    expect(apiMatrix).toContain(
      "| `{type}/list` | input_boolean, input_number, input_text, input_datetime, input_select, input_button, counter, timer, schedule |",
    );
    expect(apiMatrix).not.toContain("schedule, zone, person, tag");
    expect(architecture).toContain("config-body-filter.jq");
    expect(architecture).toContain("dashboard/SKILL.md");
    expect(architecture).toContain("organize/SKILL.md");
    expect(architecture).toContain("history/SKILL.md");
    expect(architecture).toContain("health/SKILL.md");
    expect(architecture).toContain("calendar/SKILL.md");
    expect(architecture).toContain(
      "`ha-nova:dashboard` owns safe storage-dashboard work",
    );
    expect(architecture).toContain("list Lovelace resources");
    expect(architecture).toContain("create/update/delete Lovelace resources");
    expect(architecture).toContain(
      "add/update/move/delete cards inside existing views",
    );
    expect(architecture).toContain("create a storage dashboard shell");
    expect(architecture).toContain(
      "`dashboard_id` is the mutation identifier for `update|delete`",
    );
    expect(architecture).toContain(
      "metadata update sends `dashboard_id` plus only changed metadata fields",
    );
    expect(architecture).toContain(
      "`ha-nova:organize` owns metadata-first Home Assistant organization",
    );
    expect(architecture).toContain("areas / floors / labels / categories CRUD");
    expect(architecture).toContain(
      "entity category assignment/removal by scope",
    );
    expect(architecture).toContain(
      "areas: `floor_id`, `icon`, `picture`, `aliases`",
    );
    expect(architecture).toContain(
      "entity/device label updates may replace, add, remove, or clear labels",
    );
    expect(architecture).toContain(
      "every `config/category_registry/*` call includes the exact `scope`",
    );
    expect(architecture).toContain(
      "`ha-nova:history` is a bounded read-only timeline skill",
    );
    expect(architecture).toContain(
      "`ha-nova:health` is a read-only home-status skill",
    );
    expect(architecture).toContain(
      "`ha-nova:calendar` owns bounded calendar reads and single-event writes",
    );
    expect(architecture).toContain(
      "long-term trends via `recorder/statistics_during_period`",
    );
    expect(architecture).toContain("room/area bulk resolution is area-first");
    expect(architecture).toContain(
      "materialize and trim the current workset before any per-item reads",
    );
  });

  it("keeps the architecture exclusion boundary aligned with helper-owned config-entry domains", () => {
    const architecture = readFileSync(
      "docs/reference/skill-architecture.md",
      "utf8",
    );

    expect(architecture).toContain("Still excluded from `ha-nova:helper`:");
    expect(architecture).toContain("- `trend`");
    expect(architecture).toContain("- `random`");
    expect(architecture).toContain("- `filter`");
    expect(architecture).toContain("- `generic_thermostat`");
    expect(architecture).toContain("- `switch_as_x`");
    expect(architecture).toContain("- `generic_hygrostat`");
    expect(architecture).not.toContain(
      "Still excluded from `ha-nova:helper`:\n- `template`",
    );
    expect(architecture).not.toContain(
      "Still excluded from `ha-nova:helper`:\n- `group`",
    );
    expect(architecture).not.toContain(
      "Still excluded from `ha-nova:helper`:\n- `statistics`",
    );
    expect(architecture).not.toContain(
      "Still excluded from `ha-nova:helper`:\n- `history_stats`",
    );
  });

  it("documents tiered best-practice gate", () => {
    const bp = readFileSync("skills/ha-nova/best-practices.md", "utf8");

    expect(bp).toContain("Tiered policy for automation writes");
    expect(bp).toContain("Simple automation");
    expect(bp).toContain("Complex automation");
    expect(bp).toContain("hard gate");
    expect(bp).toContain("Enforcement Checklist");
  });

  it("gates input-device remaps on the capability preflight", () => {
    const preflight = readFileSync(
      "skills/ha-nova/input-capability-preflight.md",
      "utf8",
    );
    const write = readFileSync("skills/write/SKILL.md", "utf8");
    const bp = readFileSync("skills/ha-nova/best-practices.md", "utf8");
    const relayApi = readFileSync("skills/ha-nova/relay-api.md", "utf8");

    // Write flow blocks remap drafts until the gesture is more than assumed.
    expect(write).toContain(
      "run the capability preflight per `skills/ha-nova/input-capability-preflight.md` before drafting",
    );
    expect(write).toContain(
      "the write stays blocked while the chosen gesture is only assumed or its evidence conflicts",
    );

    // Evidence classes and the never-supported rule.
    expect(preflight).toContain("**advertised**");
    expect(preflight).toContain("**observed**");
    expect(preflight).toContain("**assumed**");
    expect(preflight).toContain(
      "An assumed gesture is never presented as a working option",
    );

    // Evidence matrix: all four mixed-evidence rows with their consequences.
    expect(preflight).toContain("| advertised + observed agree | Verified |");
    expect(preflight).toContain(
      "| metadata-only (advertised, never observed) | Likely |",
    );
    expect(preflight).toContain(
      "| observation-only (observed, not in metadata) | Likely |",
    );
    expect(preflight).toContain(
      "| conflicting (one source contradicts the other) | Uncertain |",
    );
    expect(preflight).toContain("| assumed | Uncertain |");

    // Normalization: same-path comparison only, never cross-integration.
    expect(preflight).toContain("case-insensitively with separators stripped");
    expect(preflight).toContain("a Z2M `action: single` is not evidence");
    expect(preflight).toContain(
      "the active integration path may expose fewer actions",
    );

    // Live observation routes through the readiness sequence.
    expect(preflight).toContain("User-Assisted Readiness");

    // Worked example: single/double advertised, hold missing → blocked.
    expect(preflight).toContain(
      "advertises `single` and `double` but no `hold`",
    );
    expect(preflight).toContain("Never create the hold automation on spec");

    // Discovery endpoints are documented, with the metadata-is-not-proof caveat.
    expect(relayApi).toContain('"type":"device_automation/trigger/list"');
    expect(relayApi).toContain(
      '"type":"device_automation/trigger/capabilities"',
    );
    expect(relayApi).toContain(
      "Advertised metadata is not proof an action fires",
    );

    // Zigbee button authoring points at the preflight first.
    expect(bp).toContain("skills/ha-nova/input-capability-preflight.md");
  });

  it("discovers consumers before an input is repurposed", () => {
    const discovery = readFileSync(
      "skills/ha-nova/consumer-discovery-preflight.md",
      "utf8",
    );
    const capability = readFileSync(
      "skills/ha-nova/input-capability-preflight.md",
      "utf8",
    );
    const write = readFileSync("skills/write/SKILL.md", "utf8");
    const serviceCall = readFileSync("skills/service-call/SKILL.md", "utf8");
    const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");

    // Write flow routes repurpose/cleanup through the discovery preflight.
    expect(write).toContain(
      "additionally runs `skills/ha-nova/consumer-discovery-preflight.md`",
    );
    expect(write).toContain(
      'incomplete coverage is disclosed, never claimed as "unused"',
    );

    // Result schema fields.
    expect(discovery).toContain("**source family**");
    expect(discovery).toContain("**reference**");
    expect(discovery).toContain("**matched action**");
    expect(discovery).toContain("**confidence**");

    // Standard families: automations/scripts, event consumers, blueprints.
    expect(discovery).toContain("**Automations & scripts**");
    expect(discovery).toContain("**Event-type consumers**");
    expect(discovery).toContain("**Blueprint-backed automations**");
    expect(discovery).toContain(
      "`blueprint/list` returns only metadata, no triggers",
    );
    expect(discovery).toContain('"type":"blueprint/substitute"');
    expect(discovery).toContain("never as cleared");

    // Adapter contract: documented shape, zero registered, honest reporting.
    expect(discovery).toContain("## Extension Adapter Contract");
    expect(discovery).toContain("No adapters are registered yet");
    expect(discovery).toContain("**not checkable**");
    expect(discovery).toContain("never parsed heuristically and never mutated");

    // Coverage honesty: both lists, no "unused" claim under partial coverage.
    expect(discovery).toContain("families checked");
    expect(discovery).toContain("families not checkable");
    expect(discovery).toContain("never claim the input is unused");
    expect(discovery).toContain("no consumers found in the checked");

    // The relay stays a generic transport; discovery is read-only.
    expect(discovery).toContain(
      "contains no extension-specific consumer logic",
    );
    expect(discovery).toContain("Discovery is read-only");

    // Cross-links: companion pair + shared event-scan + External anchor.
    expect(capability).toContain(
      "skills/ha-nova/consumer-discovery-preflight.md",
    );
    expect(serviceCall).toContain(
      "the shared event-consumer pattern of `skills/ha-nova/consumer-discovery-preflight.md`",
    );
    expect(fallback).toContain(
      "only through a documented adapter (`skills/ha-nova/consumer-discovery-preflight.md`",
    );
  });

  it("keeps agent templates parameterized and structured", () => {
    const resolve = readFileSync(
      "skills/ha-nova/agents/resolve-agent.md",
      "utf8",
    );
    const apply = readFileSync("skills/ha-nova/agents/apply-agent.md", "utf8");

    expect(resolve).toContain("{DOMAIN}");
    expect(resolve).toContain("{OPERATION}");
    expect(resolve).toContain("{USER_INTENT}");
    expect(resolve).toContain("ha-nova relay ws");
    expect(resolve).toContain("ha-nova relay core");
    expect(resolve).not.toContain("{RELAY_BASE_URL}");
    expect(resolve).not.toContain("{RELAY_AUTH_TOKEN}");
    expect(resolve).not.toContain("macos-onboarding.sh");
    expect(resolve).not.toContain("git rev-parse");
    expect(resolve).toContain("RESOLVED_ENTITIES:");
    expect(resolve).toContain("No entities matching '{USER_INTENT}' found");
    expect(resolve).toContain("SUGGESTED_ENHANCEMENTS:");
    expect(resolve).toContain("toggle-stop");

    expect(apply).toContain("{TARGET_ID}");
    expect(apply).toContain("{PAYLOAD}");
    expect(apply).toContain("ha-nova relay ws");
    expect(apply).toContain("ha-nova relay core");
    expect(apply).not.toContain("{RELAY_BASE_URL}");
    expect(apply).not.toContain("{RELAY_AUTH_TOKEN}");
    expect(apply).not.toContain("macos-onboarding.sh");
    expect(apply).not.toContain("git rev-parse");
    expect(apply).toContain("RESULT:");
    expect(apply).toContain("reloaded:");
    expect(apply).toContain("VERIFICATION:");
    expect(apply).toContain("trigger` + `triggers");
    expect(apply).toContain("automation/reload");
    expect(apply).toContain("script/reload");
    expect(apply).toContain(
      "`write`, `read-back`, `reload`, or `runtime-verify`",
    );
  });

  it("keeps the history skill read-only, bounded, and stats-aware", () => {
    const history = readFileSync("skills/history/SKILL.md", "utf8");

    expect(history).toContain("Read-only timeline work:");
    expect(history).toContain("long-term statistics over a bounded time range");
    expect(history).toContain("`recorder/statistics_during_period`");
    expect(history).toContain("Do not invent a `--jq` flag.");
    expect(history).toContain(
      "prefer simple reductions that do not depend on fragile timestamp parsing",
    );
    expect(history).toContain(
      "do not build complex jq expressions just to recover min/max event timestamps",
    );
    expect(history).toContain("history series: `.data.body[0]`");
    expect(history).toContain("logbook entries: `.data.body`");
    expect(history).toContain(
      "Recorder statistics response stays under WS `.data`.",
    );
    expect(history).toContain(
      "do not probe `.[0]` or `.[0][0]` against the relay envelope",
    );
    expect(history).toContain(
      "otherwise default to the last 30 days for statistics/trend questions",
    );
  });

  it("routes concrete failures to diagnose and config audits to review", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    // Regression: the problem-description catch-all sent "stopped working" to
    // review, so the trace/log root-cause workflow was unreachable unless the
    // user literally said "why".
    expect(context).toContain("**Problem-description intents**");
    expect(context).toContain('"stopped working"');
    const problemRule = context.slice(
      context.indexOf("**Problem-description intents**"),
    );
    const ruleBlock = problemRule.slice(0, problemRule.indexOf("## "));
    expect(ruleBlock).toContain("`ha-nova:diagnose`");
    expect(ruleBlock).toContain("no concrete incident");
  });

  it("keeps all HA NOVA skills in source tree", () => {
    const files = [
      "skills/ha-nova/SKILL.md",
      "skills/dashboard/SKILL.md",
      "skills/scene/SKILL.md",
      "skills/todo/SKILL.md",
      "skills/backup/SKILL.md",
      "skills/updates/SKILL.md",
      "skills/organize/SKILL.md",
      "skills/history/SKILL.md",
      "skills/write/SKILL.md",
      "skills/read/SKILL.md",
      "skills/helper/SKILL.md",
      "skills/integration-setup/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/onboarding/SKILL.md",
      "skills/service-call/SKILL.md",
      "skills/review/SKILL.md",
      "skills/review/checks.md",
      "skills/energy/SKILL.md",
      "skills/energy/energy-reference.md",
      "skills/maintenance/SKILL.md",
      "skills/maintenance/maintenance-reference.md",
      "skills/fallback/SKILL.md",
      "skills/diagnose/SKILL.md",
      "skills/media/SKILL.md",
      "skills/notify/SKILL.md",
      "skills/camera/SKILL.md",
      "skills/mqtt/SKILL.md",
      "skills/yaml-config/SKILL.md",
      "skills/assist/SKILL.md",
      "skills/admin/SKILL.md",
      "skills/external-sources/SKILL.md",
    ];

    for (const file of files) {
      expect(existsSync(file), `Expected file to exist: ${file}`).toBe(true);
      const content = readFileSync(file, "utf8");
      expect(content).not.toContain("__HA_NOVA_REPO_ROOT__");
      expect(content).not.toContain("ha-nova-managed-install");
    }
  });

  it("enforces relay CLI bootstrap across all operational subskills", () => {
    const skills = [
      "skills/dashboard/SKILL.md",
      "skills/scene/SKILL.md",
      "skills/todo/SKILL.md",
      "skills/backup/SKILL.md",
      "skills/updates/SKILL.md",
      "skills/energy/SKILL.md",
      "skills/maintenance/SKILL.md",
      "skills/organize/SKILL.md",
      "skills/history/SKILL.md",
      "skills/write/SKILL.md",
      "skills/read/SKILL.md",
      "skills/integration-setup/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/onboarding/SKILL.md",
    ];

    for (const file of skills) {
      const content = readFileSync(file, "utf8");
      expect(content, `${file} should use relay CLI`).toContain(
        "ha-nova relay",
      );
      expect(content, `${file} should not use eval bootstrap`).not.toContain(
        "macos-onboarding.sh",
      );
      expect(content, `${file} should not use git rev-parse`).not.toContain(
        "git rev-parse",
      );
      expect(
        content,
        `${file} should not reference RELAY_BASE_URL`,
      ).not.toContain("RELAY_BASE_URL");
    }
  });

  it("keeps active skills on a shell-agnostic relay contract", () => {
    const files = [
      "skills/dashboard/SKILL.md",
      "skills/scene/SKILL.md",
      "skills/todo/SKILL.md",
      "skills/backup/SKILL.md",
      "skills/updates/SKILL.md",
      "skills/energy/SKILL.md",
      "skills/energy/energy-reference.md",
      "skills/maintenance/SKILL.md",
      "skills/maintenance/maintenance-reference.md",
      "skills/organize/SKILL.md",
      "skills/history/SKILL.md",
      "skills/read/SKILL.md",
      "skills/review/SKILL.md",
      "skills/helper/SKILL.md",
      "skills/integration-setup/SKILL.md",
      "skills/entity-discovery/SKILL.md",
      "skills/fallback/SKILL.md",
      "skills/service-call/SKILL.md",
      "skills/ha-nova/safe-refactoring.md",
      "skills/ha-nova/relay-api.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/agents/apply-agent.md",
      "skills/write/SKILL.md",
      "skills/review/checks.md",
    ];

    for (const file of files) {
      const content = readFileSync(file, "utf8");
      expect(content, `${file} should teach file-based relay payloads`).toMatch(
        /--(data-file|body-file|out)\b/,
      );
      expect(
        content,
        `${file} should not teach inline ws/core JSON as canonical path`,
      ).not.toContain("relay ws -d '{");
      expect(content, `${file} should not teach ws --body`).not.toContain(
        "relay ws --body",
      );
      expect(
        content,
        `${file} should not teach inline core JSON as canonical path`,
      ).not.toContain("relay core -d '{");
      expect(content, `${file} should not rely on /tmp`).not.toContain("/tmp/");
      expect(
        content,
        `${file} should not rely on python post-processing`,
      ).not.toContain("python -c");
      expect(
        content,
        `${file} should not rely on node post-processing`,
      ).not.toContain("node -e");
      expect(content, `${file} should not teach mktemp`).not.toContain(
        "mktemp",
      );
      expect(
        content,
        `${file} should not teach shell piping into relay jq`,
      ).not.toContain("| ha-nova relay jq");
      expect(content, `${file} should not teach shell heredocs`).not.toContain(
        "<< 'EOF'",
      );
      expect(content, `${file} should not teach shell heredocs`).not.toContain(
        '<< "EOF"',
      );
      expect(
        content,
        `${file} should not teach glob-sensitive inline jq selectors`,
      ).not.toContain("--jq .data[]");
    }

    const entityDiscovery = readFileSync(
      "skills/entity-discovery/SKILL.md",
      "utf8",
    );
    expect(entityDiscovery).toContain(
      "/api/config/automation/config/{unique_id}",
    );
    expect(entityDiscovery).toContain("--out <result-file>");
    expect(entityDiscovery).toContain("never chain commands with `&&` or `||`");
    expect(entityDiscovery).toContain("Never call external `jq`");

    const review = readFileSync("skills/review/SKILL.md", "utf8");
    expect(review).toContain("ha-nova relay health");
  });

  it("moves complex relay filtering to jq files and native file tools", () => {
    const jqFileExamples = [
      "skills/entity-discovery/SKILL.md",
      "skills/read/SKILL.md",
      "skills/helper/SKILL.md",
      "skills/review/SKILL.md",
      "skills/write/SKILL.md",
      "skills/ha-nova/agents/resolve-agent.md",
      "skills/ha-nova/safe-refactoring.md",
      "skills/review/checks.md",
    ];

    for (const file of jqFileExamples) {
      const content = readFileSync(file, "utf8");
      expect(
        content,
        `${file} should teach --jq-file for complex filters`,
      ).toContain("--jq-file");
      expect(
        content,
        `${file} should not teach inline body extraction filters`,
      ).not.toContain("--jq 'if .ok then .data.body");
    }

    const updateGuide = readFileSync("docs/reference/update-guide.md", "utf8");
    // version.json lives in the install root (cli/paths.go VersionFile), not in ~/.config/ha-nova.
    expect(updateGuide).toContain(
      "`ha-nova version` (reads `version.json` from the install root)",
    );
    expect(updateGuide).not.toContain("~/.config/ha-nova/version.json");
    expect(updateGuide).not.toContain("cat ~/.config/ha-nova/version.json");
    expect(updateGuide).toContain(
      "Other clients use the same shared first-use skill contract and CLI updater path",
    );
    expect(updateGuide).toContain(
      "| Google Antigravity | Flat-copy | Rebuild namespaced flat markdown copies from the active HA NOVA install |",
    );
    expect(updateGuide).not.toContain("| Gemini | Flat-copy |");

    const bestPractices = readFileSync(
      "skills/ha-nova/best-practices.md",
      "utf8",
    );
    expect(bestPractices).not.toContain(
      'cat > "${HOME}/.cache/ha-nova/automation-bp-snapshot.json"',
    );
    expect(bestPractices).toContain("native file-writing tool");

    const relayApi = readFileSync("skills/ha-nova/relay-api.md", "utf8");
    expect(relayApi).toContain(
      "Do not allocate scratch directories or files from visible shell commands",
    );
    expect(relayApi).toContain(
      "set the tool working directory to the scratch directory outside the command text",
    );

    const fallback = readFileSync("skills/fallback/SKILL.md", "utf8");
    expect(fallback).toContain(
      "| System Health / Repairs | Covered | health |",
    );
    expect(fallback).toContain(
      "| Calendar Events (read / create / update / delete) | Covered | calendar |",
    );
    expect(fallback).toContain(
      "| Custom events / known JSON webhooks | Covered | service-call |",
    );
    expect(fallback).toContain(
      "| Alarm / lock runtime control | Covered | service-call |",
    );
    expect(fallback + relayReadySplit).not.toContain(
      "### System Health / Repairs -- RELAY-READY",
    );
    expect(fallback + relayReadySplit).not.toContain("### Calendar Queries -- RELAY-READY");
    expect(fallback + relayReadySplit).not.toContain("### Events / Webhooks -- RELAY-READY");
    expect(fallback).not.toContain("<calendar-events-path>");
    expect(fallback).not.toContain("--path '/api/calendars/");
    expect(fallback).toContain(
      "| Dashboard / Lovelace (storage lifecycle, cards, resources) | Covered | dashboard |",
    );
    expect(fallback).toContain("| History Queries | Covered | history |");
    expect(fallback).toContain(
      "| Statistics / Trend Queries | Covered | history |",
    );
    expect(fallback).toContain("| Area / Floor CRUD | Covered | organize |");
    expect(fallback).toContain(
      "| Label CRUD / Rich label metadata | Covered | organize |",
    );
    expect(fallback).toContain(
      "| Category CRUD / Entity category assignment | Covered | organize |",
    );
    expect(fallback).toContain(
      "| Statistics repair / Purge / Entity registry remove | Covered | maintenance |",
    );
    expect(fallback).toContain(
      "| Device config-entry detach | External | -- (Home Assistant UI; HA 2026.8+ removes the device) |",
    );
  });

  it("keeps migration and search-related guidance in the shared refactoring doc", () => {
    const refactorGuide = readFileSync(
      "skills/ha-nova/safe-refactoring.md",
      "utf8",
    );

    expect(refactorGuide).toContain("Safe Migration Pattern");
    expect(refactorGuide).toContain(
      "Review -> Write/Helper -> Verify -> Review",
    );
    expect(refactorGuide).toContain("search/related Signal Strength");
    expect(refactorGuide).toContain("Helpers: strong signal");
    expect(refactorGuide).toContain("Scenes: weak signal");
  });

  it("avoids jq regex-dot escaping in helper-domain examples", () => {
    const files = [
      "skills/helper/SKILL.md",
      "skills/review/SKILL.md",
      "skills/ha-nova/safe-refactoring.md",
    ];

    for (const file of files) {
      const content = readFileSync(file, "utf8");
      expect(
        content,
        `${file} should avoid helper-domain regex escapes`,
      ).not.toContain(
        'test("^(input_boolean|input_number|input_text|input_select|input_datetime|input_button|counter|timer|schedule)\\\\.")',
      );
      expect(
        content,
        `${file} should use split-domain helper filtering`,
      ).toContain('split(".")[0]');
    }
  });

  it("keeps relay Go binary source present", () => {
    expect(existsSync("cli/main.go")).toBe(true);
    expect(existsSync("cli/relay.go")).toBe(true);
  });

  it("provides Claude Code plugin manifest", () => {
    const plugin = JSON.parse(
      readFileSync(".claude-plugin/plugin.json", "utf8"),
    );
    expect(plugin.name).toBe("ha-nova");
    expect(plugin.description).toBeTruthy();
  });

  it("keeps all version files in sync with version.json", () => {
    const versionJson = JSON.parse(readFileSync("version.json", "utf8"));
    const expected = versionJson.skill_version;
    expect(expected).toMatch(/^\d+\.\d+\.\d+$/);

    const plugin = JSON.parse(
      readFileSync(".claude-plugin/plugin.json", "utf8"),
    );
    expect(plugin.version).toBe(expected);

    const pkg = JSON.parse(readFileSync("package.json", "utf8"));
    expect(pkg.version).toBe(expected);

    const marketplace = JSON.parse(
      readFileSync(".claude-plugin/marketplace.json", "utf8"),
    );
    expect(marketplace.metadata.version).toBe(expected);
    expect(marketplace.metadata.description).toBeTruthy();
    expect(marketplace.plugins[0].source).toBe("./");
    expect(marketplace.plugins[0].version).toBe(expected);
  });

  it("tells non-Claude clients to run a quiet update check on first skill use", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    const content = readFileSync("skills/ha-nova/session-bootstrap.md", "utf8");
    expect(context).toContain("../ha-nova/session-bootstrap.md");
    const bootstrapHeading = context.indexOf("## Session Bootstrap");
    const prerequisiteHeading = context.indexOf("## Runtime Prerequisite");
    expect(bootstrapHeading).toBeGreaterThan(-1);
    expect(prerequisiteHeading).toBeGreaterThan(bootstrapHeading);
    expect(content).toContain("ha-nova check-update --quiet");
    expect(content).toContain(
      "before the first Home Assistant task in a session",
    );
    expect(content).toContain("first Home Assistant or HA NOVA task");
    expect(content).toContain("before any relay command");
    expect(content).toContain("one check for every server profile");
    expect(content).toContain("HA_NOVA_NO_CENSUS=1");
    expect(content).toMatch(
      /switching\s+servers cannot duplicate the install-wide pending census notice/,
    );
  });

  it("surfaces the update as a once-per-session callout after the requested result", () => {
    const content = readFileSync("skills/ha-nova/session-bootstrap.md", "utf8");
    expect(content).toContain("## HA NOVA Update Callout");
    expect(content).toContain("exactly once");
    expect(content).toContain("After the requested result");
    expect(content).toContain("up to three supplied highlight lines");
    expect(content).toContain("supplied release URL");
    expect(content).toMatch(/Never update without\s+consent/);
  });

  it("surfaces census consent as one standalone explicit action choice", () => {
    const content = readFileSync("skills/ha-nova/session-bootstrap.md", "utf8");
    expect(content).toContain("## Census Consent Choice");
    expect(content).toContain("CENSUS ASK PENDING");
    expect(content).toContain("exactly once per");
    expect(content).toContain("standalone");
    expect(content).toContain("Interactive Choices");
    expect(content).toContain("native selectable menu");
    expect(content).toContain("numbered fallback");
    expect(content).toContain("Never stack this choice");
    expect(content).toContain("deferred machine notice does not count");
    expect(content).toContain("ha-nova census notice-presented");
    expect(content).toContain("CENSUS NOTICE PRESENT");
    expect(content).toContain("CENSUS NOTICE SKIP");
    expect(content).toContain("cns-choice-");
    expect(content).toContain("the only active choice");
    expect(content).toContain("must close it");
    expect(content).toContain("Cloudflare is the hosting provider");
    expect(content).toContain("no sooner than seven days later");
    expect(content).not.toContain("one attempt per ISO week");
    expect(content).toContain("message content (JSON)");
    expect(content).toContain("private maintainer");
    expect(content).toMatch(
      /by\s+contributing, the user helps the maintainer get a rough picture/,
    );
    expect(content).toContain("how operating systems are distributed");
    expect(content).toMatch(
      /prioritize compatibility\s+work, tests, bug fixes, and new features/,
    );
    expect(content).toMatch(/not a roadmap\s+vote or feature\s+promise/);
    expect(content).toContain("without guilt, pressure, or recommending opt-in");
    expect(content).toContain(
      "ingest code does not read or store the source IP",
    );
    expect(content).toContain("at most five short lines");
    expect(content).toContain("Use those five lines for");
    expect(content).toContain("purpose and planning value; cadence");
    expect(content).toContain(
      "Cloudflare processing, HA NOVA source-IP non-reading/non-storage",
    );
    expect(content).toContain("three actions immediately after");
    expect(content).toContain("1. **Yes — contribute**");
    expect(content).toContain("2. **No — do not contribute**");
    expect(content).toContain("3. **Show exact data**");
    expect(content).toContain("ha-nova census choose <choice-id> yes");
    expect(content).toContain("ha-nova census choose <choice-id> no");
    expect(content).toContain("same choice ID");
    expect(content).toContain("old UI action");
    expect(content).toContain("run `ha-nova census status`");
    expect(content).toContain("literal JSON");
    expect(content).toContain("verbatim without omitting or renaming fields");
    expect(content).toContain("change no consent state");
    expect(content).toMatch(/render the\s+same three choices again/);
    expect(content).toContain("immediate re-render");
    expect(content).toContain("part of the same");
    expect(content).toContain(
      "Otherwise, do not surface the prompt again unsolicited",
    );
    expect(content).toContain("use the privacy-safe No choice");
    expect(content).toContain("choice is stale");
    expect(content).toContain("never retry with");
    expect(content).toContain("If `ha-nova census status` fails");
    expect(content).toMatch(/consent\s+is unchanged/);
    expect(content).toContain("ambiguous answer runs nothing");
    expect(content).toContain("Never infer opt-in");
  });

  it("never describes the census HTTPS request as anonymous on active surfaces", () => {
    const surfaces = [
      "README.md",
      "PRIVACY.md",
      "census-worker/README.md",
      "census-worker/src/census.ts",
      "docs/reference/census.md",
      "docs/reference/safety.md",
      "docs/work/2026-07-31-launch-posts-v0.22.md",
      "skills/ha-nova/session-bootstrap.md",
      "cli/census.go",
      "cli/census_ask.go",
      "cli/census_command.go",
    ];

    for (const path of surfaces) {
      const content = readFileSync(path, "utf8");
      expect(content, path).not.toMatch(/\banonymous\b/i);
      expect(content, path).not.toMatch(
        /\b(?:no|without)\s+(?:source[- ]?)?ip(?:\s+address)?\s+(?:is\s+)?(?:sent|transmitted)\b/i,
      );
      expect(content, path).not.toMatch(
        /\b(?:does not|doesn't|never)\s+(?:send|transmit)\s+(?:an?\s+|the\s+)?(?:source[- ]?)?ip\b/i,
      );
    }
  });

  it("routes Relay App updates through a previewed guided install", () => {
    const content = readFileSync("skills/ha-nova/session-bootstrap.md", "utf8");
    expect(content).toContain("## Relay Update Callout");
    expect(content).toContain("ha-nova:updates");
    expect(content).toContain("show its App-update preview");
    expect(content).toContain(
      "confirmation before installing with a partial backup",
    );
    expect(content).toContain("standalone Container/Core Relay");
    expect(content).toContain("manual image pull and container recreation");
  });

  it("provides SessionStart hook for context skill auto-loading", () => {
    const hooksJson = JSON.parse(readFileSync("hooks/hooks.json", "utf8"));
    expect(hooksJson.hooks.SessionStart).toBeDefined();
    expect(hooksJson.hooks.SessionStart[0].matcher).toBe(
      "startup|resume|clear|compact",
    );

    const hookScript = "hooks/session-start";
    expect(existsSync(hookScript)).toBe(true);
    const mode = statSync(hookScript).mode;
    expect(mode & constants.S_IXUSR).toBeGreaterThan(0);
    const content = readFileSync(hookScript, "utf8");
    expect(content).toContain("skills/ha-nova/SKILL.md");
    expect(content).toContain("additional_context");
  });

  it("defines a portable menu-or-numbered convention for choices, with typed deletes", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    expect(context).toContain("Interactive Choices");
    // Progressive enhancement: native menu where available, numbered fallback otherwise.
    expect(context).toContain("AskUserQuestion");
    expect(context).toContain("numbered list");
    // Destructive confirmation must never become a one-click menu.
    expect(context).toContain("Destructive confirmation is never a menu");
    expect(context).toContain("confirm:<token>");
    // Firewall against a self-written "always use a menu" memory overriding deletes.
    expect(context).toContain("NEVER extends to deletes");
  });

  it("resolves bare numbers to named effects and separates effect classes (#394)", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    expect(context).toContain(
      "Every option label opens with its concrete effect",
    );
    expect(context).toContain(
      "Never merge a wording change, a static check, a listen window, and a live execution into one option",
    );
    expect(context).toContain(
      "the next response names the chosen effect before acting",
    );
    expect(context).toContain(
      "This applies to any numbered selection, including config-entry flow menus.",
    );
    expect(context).toContain(
      "Never act on a number the response did not translate back into its effect.",
    );
    // Flow menus resolve numeric picks before submitting the step.
    const integrationSetup = readFileSync(
      "skills/integration-setup/SKILL.md",
      "utf8",
    );
    expect(integrationSetup).toContain(
      "name the selected option before submitting it",
    );
  });

  it("arms capture before instructing physical user actions (#394)", () => {
    const context = readFileSync("skills/ha-nova/SKILL.md", "utf8");
    expect(context).toContain("## User-Assisted Readiness");
    expect(context).toContain("never give the instruction first");
    expect(context).toContain("confirm in one line that capture is armed");
    expect(context).toContain(
      "report the observed result — or honestly that nothing was captured",
    );
    expect(context).toContain("the user then acts at their own pace");
    expect(context).toContain('say "act now" in the same message');
    expect(context).toContain("re-arm and retry once, not a device verdict");
  });
});
