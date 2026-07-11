# HA NOVA Skill Architecture

## Overview

HA NOVA uses a flat skill layout with one context skill and 19 independent sub-skills under `skills/`.

The repo skill tree is the single source of truth. Client installers adapt that same tree to each client's packaging rules:
- Claude: plugin marketplace payload
- Codex / OpenCode: nested skill tree
- Google Antigravity: flat copied skill directories
- Hermes: namespaced nested copied skill tree with directory names aligned to installed skill IDs

Installed skill tree:
```
skills/
  ha-nova/SKILL.md              (context skill — stable top-level entrypoint)
  ha-nova/relay-api.md          (reference doc)
  ha-nova/best-practices.md     (reference doc)
  ha-nova/payload-schemas.md    (reference doc)
  ha-nova/helper-schemas.md     (reference doc — helper type payloads)
  ha-nova/output-rules.md       (reference doc — shared user-facing output rules)
  ha-nova/config-body-filter.jq (shared jq asset — canonical REST config-body extractor)
  ha-nova/bulk-patterns.md      (reference doc — bulk selectors, workset, aggregate audit rules)
  ha-nova/template-guidelines.md (reference doc — when to use templates vs native primitives)
  ha-nova/safe-refactoring.md   (reference doc — rename, delete, orphan cleanup workflows)
  ha-nova/automation-patterns.md (reference doc — native HA constructs vs templates)
  ha-nova/write-safety.md       (reference doc — pre-write diff + durable update-revert; SSOT for write/ + helper/)
  ha-nova/agents/               (agent templates: resolve, apply)
  read/SKILL.md                         (ha-nova:read — automation/script list/get/trace)
  write/SKILL.md                        (ha-nova:write — automation/script create/update/delete)
  helper/SKILL.md                       (ha-nova:helper — helper CRUD: list/read/create/update/delete)
  dashboard/SKILL.md                    (ha-nova:dashboard — storage dashboards, Lovelace resources, card operations)
  scene/SKILL.md                        (ha-nova:scene — storage-scene list/read/create/update/delete)
  organize/SKILL.md                     (ha-nova:organize — areas/floors/labels/categories/entity+device metadata)
  history/SKILL.md                      (ha-nova:history — bounded history/logbook/statistics reads)
  health/SKILL.md                       (ha-nova:health — read-only home status, repairs, system health)
  calendar/SKILL.md                     (ha-nova:calendar — read-only calendar lists and bounded event windows)
  todo/SKILL.md                         (ha-nova:todo — to-do list items + Local To-do lifecycle)
  backup/SKILL.md                       (ha-nova:backup — backup status/create/inspect/delete; restore stays in HA UI)
  updates/SKILL.md                      (ha-nova:updates — pending updates, release notes, feature-gated installs)
  energy/SKILL.md                       (ha-nova:energy — energy analysis + gated source/device config)
  energy/energy-reference.md            (reference doc — prefs schemas, KPI formulas, analysis recipes)
  maintenance/SKILL.md                  (ha-nova:maintenance — statistics repair, recorder purge, registry cleanup)
  maintenance/maintenance-reference.md  (reference doc — issue matrix, repair payloads, orphan gates)
  review/SKILL.md                       (ha-nova:review — config quality review + collision scan)
  entity-discovery/SKILL.md             (ha-nova:entity-discovery — entity lookup)
  service-call/SKILL.md                 (ha-nova:service-call — service calls + runtime control)
  fallback/SKILL.md                     (ha-nova:fallback — mandatory fallback for relay-ready features)
  onboarding/SKILL.md                   (ha-nova:onboarding — onboarding + diagnostics)
```

## Discovery Model

The canonical skill entrypoints remain `skills/*/SKILL.md`.

- Claude loads HA NOVA through the installed plugin marketplace payload.
- Codex and OpenCode load the nested `ha-nova` skill tree directly.
- Google Antigravity receives flat copied skill directories because it only supports one skill level.
- Antigravity sub-skills are installed with namespaced identifiers such as `ha-nova-entity-discovery` so the flat folder names and activation names stay aligned.
- Hermes receives a nested copied `ha-nova` bundle under `~/.hermes/skills/ha-nova`, with namespaced sub-skill directories such as `ha-nova-entity-discovery/` whose directory names and frontmatter names stay identical.

The context skill (`ha-nova`) stays the stable entrypoint; sub-skills remain independently discoverable by description and naming.

## Repo-local Hook Note

`hooks/session-start` still exists as a repo-local development helper, but it is **not** the production installation model for Claude. Production Claude installs use a versioned local release snapshot under `~/.config/ha-nova/claude-marketplace/releases/vX.Y.Z`. The flat local staged marketplace layout stays a repo-local development tool only.

## Agent vs Inline Decision Rule

When building a new skill, decide execution model by these criteria:

**Use agents when ALL of these apply:**
- 5+ relay calls in a single operation
- Multi-step deterministic logic (resolve with fallback, write with normalization)
- Nested payload structures requiring comparison/normalization (e.g., trigger/triggers aliasing)
- Domain reload required after write

**Use inline when ANY of these apply:**
- 1-4 relay calls per operation
- Flat payloads (no nested triggers/conditions/actions)
- Direct user interaction needed between steps (preview → confirm → execute)
- No payload normalization quirks

Current mapping:

| Skill | Model | Why |
|-------|-------|-----|
| read | inline | 1-2 calls, direct output |
| write | **agents** | 5-7 calls, entity resolution fallback, singular/plural normalization, domain reload |
| helper | inline | response-driven relay flows, direct preview/confirm loop, no agent-only normalization requirement |
| dashboard | inline | read → merge → preview → full-save → readback verify, all user-facing |
| scene | inline | 2-4 calls, flat entities payload, read → merge → preview → full-save → readback verify |
| organize | inline | field-level registry mutations with direct preview/readback |
| history | inline | read-only bounded timeline lookups |
| health | inline | read-only status aggregation, best-effort diagnostics |
| calendar | inline | REST-only bounded calendar event reads |
| todo | inline | service-based item CRUD with feature gate, single-step list flow |
| backup | inline | WS status/generate/delete with initiation-vs-completion polling |
| updates | inline | entity-based overview, feature-gated install with entity-poll verification |
| energy | inline | statistics-based analysis, prefs read → merge → preview → full-save → validate verify |
| maintenance | inline | grouped issue triage, token-gated destructive repairs with per-item verification |
| review | inline | analysis is client-side, relay calls are reads only |
| entity-discovery | inline | 1-2 calls, search + return |
| service-call | inline | 2-3 calls, preview + execute |
| fallback | inline | research + web search + experimental relay calls (write-guarded) |
| onboarding | inline | diagnostics only |

**Rule of thumb:** If a `service-call` could do it, it's inline. If it needs what `write` needs (resolve + normalize + reload), use agents.

## Write Architecture

`ha-nova:write` uses a deterministic four-phase flow:

1. Resolve (Agent)
- load env
- fetch states
- resolve entities and target id
- check existence + current config
- evaluate best-practice cache status

2. Preview + Decide (Main Thread)
- build final payload
- lead the preview with a terminal-friendly Changes slot; full YAML only on `show yaml` (see `ha-nova/write-safety.md` → Pre-Write Diff)
- update: pre-write impact advisory via `search/related` at preview time (review/ Step 2)
- show compact preview blocks
- ask one decision question only if ambiguous
- confirmation tier:
  - create/update: natural confirmation bound to active preview
  - delete: tokenized `confirm:<token>`
  - pre-preview wording such as "implement the plan", "do it", or "go ahead" authorizes draft/check/preview work only; if the previewed payload, target, or manifest changes, confirmation expires

3. Apply + Verify (Agent)
- write via relay `/core`
- read-back verification
- normalized compare (`trigger(s)`, `condition(s)`, `action(s)`)
- structured error result on partial or failed verification

4. Review (inline, do NOT invoke `ha-nova:review` as separate skill)
- post-write config quality checks, collision scan, conflict analysis
- findings are advisory (write already succeeded)
- update: capture a durable revert snapshot and offer `revert` (see `ha-nova/write-safety.md` → Update-Revert); creates clean up through the normal delete flow; deletes have no HA NOVA revert

Fallback:
- if agent dispatch unavailable, execute same phases inline serially.

## Read Architecture

`ha-nova:read` is intentionally direct/low-overhead:
- no subagent dispatch for routine reads
- `/ws config/entity_registry/list_for_display` for list operations
- `/core` config reads for single-item get operations
- one blocking question only if target ambiguity remains
- multi-target scope is inventory-only; use the shared bulk selector rules from `skills/ha-nova/bulk-patterns.md`
- room/area bulk resolution is area-first: resolve the area, then use `search/related` on the area response object instead of assuming compact-registry `ai` is populated
- do not dump full YAML for many targets in one response

## Dashboard Architecture

`ha-nova:dashboard` owns safe storage-dashboard work:
- list dashboards
- read one dashboard config
- list Lovelace resources
- inspect dashboard structure (views, cards, badges, header cards)
- create a storage dashboard shell
- update dashboard metadata
- create/update/delete Lovelace resources
- add/update/move/delete cards inside existing views
- delete a storage dashboard

Write contract:
- resolve `dashboard_id`, `url_path`, and `mode` through `lovelace/dashboards/list`
- only `mode=storage` may be created/updated/deleted here
- metadata changes go through `lovelace/dashboards/create|update|delete`
- metadata update sends `dashboard_id` plus only changed metadata fields: `title`, `icon`, `show_in_sidebar`, `require_admin`
- `dashboard_id` is the mutation identifier for `update|delete`; `url_path` is the config identifier for `lovelace/config|save`
- resource CRUD goes through `lovelace/resources|create|update|delete`
- content edits always read the current full config first
- resolve the exact target card/badge/header by view + title/entity/type/position before changing it
- merge in memory
- preview the merged result
- save through `lovelace/config/save` with the full config only
- read back and verify the intended change plus unrelated-view survival

Still excluded:
- broad raw Lovelace editing without a concrete requested change
- view create/delete/reorder
- non-storage dashboard writes/deletes
- freeform new custom-card creation
- energy dashboard preferences

## Organize Architecture

`ha-nova:organize` owns metadata-first Home Assistant organization:
- areas / floors / labels / categories CRUD
- entity registry metadata updates
- entity category assignment/removal by scope
- device registry metadata updates

Mutation rules:
- exact target resolution first
- every `config/category_registry/*` call includes the exact `scope`
- rich metadata stays first-class:
  - areas: `floor_id`, `icon`, `picture`, `aliases`
  - floors: `level`, `icon`, `aliases`
  - labels: `color`, `icon`, `description`
  - categories: `icon`
- entity/device label updates may replace, add, remove, or clear labels
- field-level preview before write
- destructive area/floor/label/category delete requires impact preview + token confirmation
- read back the changed registry fields after every mutation

Still excluded:
- entity removal
- device config-entry detachment
- device category assignment
- zones / persons / tags

## History Architecture

`ha-nova:history` is a bounded read-only timeline skill:
- entity history via `/api/history/period`
- human-readable timeline via `/api/logbook`
- long-term trends via `recorder/statistics_during_period`
- summary-first answers

Rules:
- always use a bounded time window
- prefer concise summaries over raw dumps
- reject or narrow oversized requests

## Health Architecture

`ha-nova:health` is a read-only home-status skill:
- repairs/deprecation issues through `repairs/list_issues`
- integration setup/load status through `config_entries/get`
- system health through generic bounded WS event collection for `system_health/info`
- config/components through `/api/config` and `/api/components`
- unavailable/unknown and low-battery summaries through `/api/states`

Rules:
- no repair/fix/ignore actions
- no restart/reload/service calls
- check `ha-nova relay health` and skip `system_health/info` when Relay App version is below 0.2.3
- summarize by source and bind conclusions to evidence
- keep Home Status compact: overall state, source coverage, capped examples, sanitized integration reasons
- deprioritize noisy/stateless domains (`button`, `event`, `scene`, `stt`) in unavailable/unknown examples
- localize output slot headings and labels; keep HA state values literal when used as evidence

## Calendar Architecture

`ha-nova:calendar` is a REST-only read skill:
- list calendars through `/api/calendars`
- read events through `/api/calendars/{entity_id}?start=<timestamp>&end=<timestamp>`

Rules:
- default to the next 7 days
- always use a bounded event window
- resolve ambiguous calendar names before querying events
- no event create/update/delete actions

## Review Architecture

`ha-nova:review` is a self-contained read-only reviewer:
- Config quality: safety (S-01..S-03), reliability (R-01..R-28), performance (P-01..P-05), style (M-01..M-05; M-04 retired, moved to R-20), script-specific (F-01..F-08), helper-specific (H-01..H-10)
- `R-25` is pasted-YAML only (legacy template platform syntax, removed in HA 2026.6); `M-05` is a modernize advisory for pre-2024.10 automation keys
- Collision scan: `search/related` on top 3 target entities
- Conflict analysis: 3-step test (polarity → temporal → guard conditions)
- Explorative questions: standalone automation/script reviews add a gated edge-case pass for complex behavior
- Suggestion synthesis: standalone single-target review splits uncertainty into **Questions to consider** and keeps only confident recommendations in **Suggestions**
- Remove/simplify ideas pass a design-intent gate before they can become confident suggestions
- Confident suggestions are ranked by intervention depth: fix existing → simplify existing → extend existing → add new
- User-facing review text never shows internal rule codes; findings use a short descriptive title plus `Why` and `Fix`, and clean states stay generic
- `R-17` is intra-config only; collision scan stays cross-item conflict work, not overwrite/rebound detection
- `R-18` is same-mapping only; it checks storage-sensitive sibling-variable references inside one `variables:` block, not cross-scope references
- `R-19` is branch-structure reachability only; it covers direct `trigger.id` checks in a terminal bare `else` after entity-state `if` / `elif` guards, without intent inference
- `R-23` catches boolean-like templates compared to string boolean literals such as `"True"` / `"False"`
- `R-24` is a low-severity capacity-source advisory when a capacity-like variable reads `available_energy`
- Known safe/problem pattern matching from `skills/review/checks.md`
- resolved targets `== 1`: stable 8-section single-target output (`Review target`, `Findings`, `Collision check`, `Conflicts`, `Questions to consider`, `Suggestions`, `Summary`, `Instant help`)
- resolved targets `> 1`: switch to aggregate multi-target mode automatically, materialize and trim the current workset before any per-item reads, audit max 5 items in stable order, aggregate findings by pattern, and report `matched / audited / remaining`
- bulk mode disables Quick-Fix; it stays strictly read-only
- post-write review stays compact and keeps the advisory-only `Findings` / `Collision check` / `Advisory` structure

## Helper Architecture

`ha-nova:helper` now has two explicit helper families:

- **Storage-based family**
  - Types: `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`, `input_button`, `counter`, `timer`, `schedule`
  - Transport: WS (`{type}/create`, `{type}/update`, `{type}/delete`)
  - Identity: `{type}_id` from `{type}/list`, not entity_id
  - Write verify: `{type}/list`
  - Review: H-01..H-10 helper-specific checks + collision scan via `search/related`
  - No domain reload needed

- **Config-entry family**
  - Types: `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`
  - Read/list: WS `config_entries/get` + WS `config/entity_registry/list`
  - Readback: current editable options snapshot when `supports_options: true`; metadata-only fallback otherwise
  - Mutation transport: relay `/core`
  - Create: config-entry flow loop, including menu/form step iteration
  - Update: options-flow loop with required-field carry-forward from the current editable options snapshot
  - Identity: `entry_id` is canonical; linked `entity_id` values are derived only
  - Write verify: config-entry layer first for identity/existence, reopened editable options snapshot for field-level update verification
  - Review: minimal config-entry post-write contract, not H-01..H-10
  - `group` remains menu-driven; end-to-end support is proven for the `sensor` subtype, and other subtypes must stay anchored to the live step schema instead of guessed fields

Still excluded from `ha-nova:helper`:
- `trend`
- `random`
- `filter`
- `generic_thermostat`
- `switch_as_x`
- `generic_hygrostat`

## Fallback Architecture

`ha-nova:fallback` is the mandatory safety fallback for HA features without a dedicated skill:
- Covers: blueprints, zones/persons/tags, device config-entry detach, unsupported config-entry helper families
- Three-tier capability map: Covered (redirect to existing skill), Relay-Ready (experimental relay calls), External (web search)
- All inline, no agents — research + web search + experimental relay calls
- Safety: all experimental relay calls follow Write Safety by Endpoint Type guardrails (full-overwrite, field-level replace, merge, delete)

## Dev Installer Contract

The remaining shell-adjacent scripts are a development/compatibility surface, not a second product lifecycle.

Active dev helpers:
- `scripts/onboarding/install-local-skills.sh`
- `scripts/onboarding/bin/ha-nova`
- `scripts/dev-sync.sh`

Rules for this helper family:
- no end-user installer contract
- no product lifecycle logic beyond runtime discovery and forwarding
- no Git/network self-update flow
- keep behavior narrow: local skill refresh, local cache refresh, runtime forwarding, or pre-Go compatibility only

`scripts/onboarding/install-local-skills.sh` is the main repo-local installer helper.
`npm run dev:sync` / `scripts/dev-sync.sh` is the canonical repo-local refresh helper once a local install already exists.

It handles repo-local skill refreshes for development and validation:
- source skill tree: `skills/` (repo-local, flat layout)
- client-specific install strategies:
  - **Claude Code:** for repo-local development only, stages a local marketplace root under `~/.config/ha-nova/claude-marketplace`, registers it with `claude plugin marketplace add`, then installs/reinstalls `ha-nova@ha-nova`
  - **Codex CLI:** symlink on Unix, copy fallback on Windows at `~/.agents/skills/ha-nova`
  - **OpenCode:** symlink on Unix, copy fallback on Windows at `~/.config/opencode/skills/ha-nova`
  - **Google Antigravity:** Flat copy `~/.gemini/config/skills/ha-nova/SKILL.md` plus `~/.gemini/config/skills/ha-nova-*/SKILL.md` sub-skills (1-level limit), with namespaced sub-skill names matching those folder names
  - **Hermes Agent:** Namespaced nested copy under `~/.hermes/skills/ha-nova/ha-nova-*`, with sub-skill directory names and frontmatter names both using the same `ha-nova-*` identifier
- cleans up legacy flat skill directories (old `ha-nova-*` prefixed dirs)
- supports targets: `codex`, `claude`, `opencode`, `antigravity`, `hermes`, `all`; `gemini` is accepted as a legacy alias for Antigravity

The other helper roles are intentionally smaller:
- `scripts/onboarding/bin/ha-nova` forwards repo-local setup/update/check-update calls into the Go runtime
- repo/dev compatibility wrappers such as `~/.config/ha-nova/version-check` are generated from `scripts/onboarding/install-local-skills.sh` or `scripts/dev-sync.sh`, not tracked as standalone repo scripts
- `scripts/dev-sync.sh` refreshes detected local client installs and Claude cache state during development

The end-user installer contract is:
- `install.sh` / `install.ps1` bootstrap the runtime, handle legacy gating, and hand off into `ha-nova setup`
- `ha-nova setup` owns product setup, migration, and client attachment
- bundled Claude installs attach to a versioned local release snapshot under `~/.config/ha-nova/claude-marketplace/releases/vX.Y.Z`; the flat `~/.config/ha-nova/claude-marketplace` root stays repo/dev-only

## Skill Section Template (v2)

Canonical structure for all sub-skills, enforced by `tests/skills/skill-template-contract.test.ts`. Follow this when creating or auditing skills.

**Canonical H2 order** (domain-specific sections may appear in the `[domain]` slots; the canonical sections must appear in this relative order):

```
Scope → Bootstrap (once per session) → Relay Contract → [domain] → Flow → [domain]
  → Error Handling (optional, always directly before Output Format)
  → Output Format → Safety → Guardrails (optional) → References (optional)
```

**Required for ALL sub-skills:**
- **Scope** — what this skill does + inverse scope (what it does NOT do, which skill to use instead)
- **Bootstrap (once per session)** — exact heading; relay CLI verification + onboarding fallback
- **Relay Contract** — the file-based `ha-nova relay` command contract this skill uses
- **Flow** — step-by-step operations with relay commands
- **Output Format** — first line starts with ``Apply `skills/ha-nova/output-rules.md` `` ; then what the user receives
- **Safety** — risk mitigations, confirmation rules, relay-only constraint

**Required for config-persisting skills** (write, helper):
- **Post-Write Review** — mandatory inline review phase after every create/update/delete (a Flow phase, not a separate H2)
- **References** — links to schema docs, relay API, review checks

**Optional:**
- **Error Handling** — error classification + remediation (recommended for external calls); when present it sits directly before Output Format
- **Guardrails** — hard limits and constraints (e.g. "never use raw `get_states`")
- **Latency Policy** — when to optimize for speed

**Declared deviations** (the only allowed ones):
- `onboarding` — heading `## Bootstrap` (it repairs the relay; "once per session" would be wrong) and no `Relay Contract` section (the diagnostics skill's whole body is remediation commands)
- `fallback` — heading `## Bootstrap (only before Relay-Ready calls)`

**Forbidden heading variants** (normalized in 2026-07, must not return): `## Output Rules`, `## Safety Baseline` (sub-skills; the context skill keeps its own), `## Safety Guardrails`, `## Agent Flow`.

**Terminology rule:** prose says "App(s)"; `add-on` / `addon` may appear only inside inline code or fenced blocks as literal API identifiers (`include_all_addons`, `failed_addons`, `<addon_slug>`, ...) or as backticked search-query strings.

**Portability rule:** if a referenced shared file is unavailable in an install, do not guess its content — ask the user to re-run `ha-nova setup`. Exception: the existing `config-body-filter.jq` "recreate exactly" blocks (a one-line filter, self-recreation is strictly better).

**The context skill (`ha-nova/SKILL.md`) is exempt** from the sub-skill template: it is a router, not an operation skill. Its required anchors stay pinned by `tests/skills/ha-nova-contract.test.ts`.

## Post-Write Review Standard

Unified spec for post-write review. Both `write` and `helper` skills reference this.

After any mutation (automation, script, or helper):
1. Re-read written config via relay
2. Enter via `skills/review/SKILL.md` Step 1 and load the detailed checks from `skills/review/checks.md`:
   - **Automations:** S + R + P + M checks. If actions reference helpers, also H checks on those helpers.
   - **Scripts:** S + R + P + M + F checks. If actions reference helpers, also H checks.
   - **Helpers:**
     - storage-based family: H checks only
     - config-entry family: minimal config-entry review contract + collision scan on linked entities
   - Traverse all `variables:` blocks, not just the top-level block.
   - Storage-sensitive checks such as `R-18` may still be reported from the persisted read-back config even when the rest of the config matches the draft. Do not suppress them purely as pre-write dedup.
   - If persisted `R-18` remains after a write, add a manual next step to inspect traces after the next real run. Do not auto-trigger the config or auto-read traces from post-write review.
   - All other checks, including `R-19`, `R-23`, and `R-24`, follow normal pre-write/post-write dedup. The explicit persisted-repeat exception stays unique to `R-18`.
   Focus on 🔴 findings. Report 🟠🟡 findings as advisory.
3. Collision scan: `search/related` for top target entities, max 3 related configs (standalone review uses max 5)
4. Output format — apply `skills/ha-nova/output-rules.md`. Use semantic slots, not literal Markdown headings, in terminal-like clients. Report only what has substance; the scans still run, only their empty output is suppressed:
   - **Findings**: 🔴🟠🟡 findings with short descriptive titles plus `Why` / `Fix` — only when there are real issues.
   - **Collision check**: only when related items exist (list them + the conflict verdict).
   - **Advisory**: 🟠🟡 findings — only when non-empty.
   - Omit any section with nothing to report — never print an empty "none" bucket. When all are empty, collapse to one scope-honest confirmation line (write-safety → Verification Honesty; never a bare "verified").
   - Do not emit `Questions to consider`, `Suggestions`, or `Instant help` in post-write mode.

## Adding a New Skill — Checklist

When creating a new skill under `skills/{name}/SKILL.md`:

1. Skill file follows Skill Section Template (see above)
2. `skills/ha-nova/SKILL.md` — add to Dispatch table + add disambiguation examples
3. `skills/{name}/SKILL.md` — reference `skills/ha-nova/output-rules.md` in its output section
4. `skills/ha-nova/SKILL.md` — add domain to Response Format if needed
5. `skills/review/SKILL.md` — keep entrypoint/flow aligned; add or update detailed rules in `skills/review/checks.md`
6. `docs/reference/skill-architecture.md` — add to skill tree + add Architecture section
7. `docs/reference/skill-architecture.md` — add to Agent vs Inline table
8. `scripts/onboarding/install-local-skills.sh` — verify dynamic discovery picks up new skill
9. `README.md` / `PROJECT.md` — add skill to overview table/list
10. `version.json` — bump patch version
11. For file-based clients, re-run `npm run dev:install:<client>-skill` and start a new session. Use `npm run dev:sync` only when you need the Claude cache sync helper or already have a repo-local install to refresh.

## Review Check Single Source of Truth

`skills/review/SKILL.md` is the stable review entrypoint.
`skills/review/checks.md` is the authoritative source for the detailed review catalog (S/R/P/M/F/H).
There is deliberately no review agent template: `write` and `helper` run their post-write review inline against `skills/review/SKILL.md` and `skills/review/checks.md` instead of duplicating checks in a separate template.
When adding or modifying checks, update `skills/review/checks.md` first and keep `skills/review/SKILL.md` aligned as the facade/workflow file.

## Review Check Taxonomy

Review checks use the format `{CATEGORY}-{NN}`:
- `S` = Safety
- `R` = Reliability
- `P` = Performance
- `M` = Style
- `F` = Script-specific
- `H` = Helper-specific

`NN` is the running rule number inside that family. Severity is separate from the code.

Examples:
- `R-10` = the 10th reliability rule
- `H-09` = the 9th helper-specific rule

These codes are contributor-facing/internal only. User-facing output must use localized descriptive titles instead of exposing codes.

## Safety Baseline

Global safety expectations:
- no guessed ids
- preview before any write
- delete requires tokenized confirmation
- pre-preview approval is never write confirmation; live writes require confirmation after the concrete preview/diff/payload/manifest is shown
- multi-target writes require a grouped manifest only where the owning skill already supports multi-target writes; otherwise process targets sequentially
- structured failure output: what failed / why / next step
- diagnostics only after real capability failure
- claim-evidence binding: verify data-target match before presenting conclusions (see context skill)

## Operational Goal

Minimize context and maintenance overhead while preserving strict write safety:
- flat skill layout for direct discovery
- context skill remains the stable top-level entrypoint
- centralized relay contract
- explicit phase boundaries
- deterministic preview/confirm/apply behavior
