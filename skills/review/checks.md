# HA NOVA Review Check Catalog

Canonical path: `skills/review/checks.md`

Self-contained catalog: load this file before evaluating findings — from `skills/review/SKILL.md` for standalone reviews, or directly from `skills/write/SKILL.md` / `skills/helper/SKILL.md` post-write phases.

## Output Guardrail (Critical)

- Apply `skills/ha-nova/output-rules.md` whenever this catalog informs user-facing output.
- Check codes (`R-17`, `S-01`, `H-02`, ...) are internal reasoning artifacts, not user-facing labels.
- Instead, describe each finding in plain language: a short descriptive title plus why it matters and how to fix it.
- This guardrail applies whenever check knowledge is used, not only during formal review runs.

## Verify Before Flagging (Critical)

A finding that turns out to be valid Home Assistant costs the user more trust
than a missed one costs them behavior. Before reporting ANY issue, resolve it
against a source rather than memory, in this order:

1. `skills/ha-nova/template-guidelines.md` for template and Jinja questions,
   `skills/ha-nova/best-practices.md` and `automation-patterns.md` for
   trigger/action/mode shapes, `skills/ha-nova/helper-schemas.md` and
   `helper-flow-schemas.md` for helper fields.
2. If the local reference does not settle it, the official documentation page
   for that surface: triggers <https://www.home-assistant.io/docs/automation/trigger/>,
   modes <https://www.home-assistant.io/docs/automation/modes/>, scripts
   <https://www.home-assistant.io/docs/scripts/>, templating
   <https://www.home-assistant.io/docs/configuration/templating/>, YAML schema
   <https://www.home-assistant.io/docs/automation/yaml/>.
3. Flag it when the settling source confirms the CLAIM you are making. Most
   of this catalog is not about schema validity: R-02, R-06, P-04 and M-05
   describe configurations Home Assistant accepts happily and that still
   misbehave, deprecate, or fire at the wrong moment. For those the source
   must confirm the semantics or the risk — "valid YAML" never clears them.
   Only schema-shaped checks need the source to call the config invalid.
4. Unresolved means unresolved: report it as a question, never as an error.

This applies wherever findings are generated — the standalone review flow and
the post-write phases in write, helper, and yaml-config alike, which load this
file directly and never see the review skill's copy of the rule.

## Check Taxonomy (internal only)

- Format: `{CATEGORY}-{NN}` (example: `H-09`)
- Category letter = family: `S` safety, `R` reliability, `P` performance, `M` style, `F` script-specific, `H` helper-specific, `SC` scene, `D` dashboard, `HX` cross-item, `TS` template/REST/command-line sensors (YAML)
- Number = running rule number within that family
- Severity is separate from the code
- Code visibility is governed by the Output Guardrail above

## Application (family matrix + evidence boundaries)

Orphan/no-consumer verdicts (F-09, H-07, SC-07, collision scans) fail closed: read `search/related` through `skills/ha-nova/search-related-consumers.jq` (recreate per `skills/ha-nova/relay-api.md` → Parsing rule on flat-copy installs; its automation/script/scene projection covers F-09's scene requirement); a failed or unexecuted scan is inconclusive — never evidence of an orphan.

**Apply these families by domain:**
- Automation: S-01..S-03, R-01..R-28, P-01..P-05, M-01..M-05
- Script: automation families plus F-01..F-09
- Storage scene: SC-01..SC-07
- Storage dashboard: D-01..D-07
- Cross-item: HX-01..HX-05 — only during aggregate/bulk reviews or when the registry context is already loaded; never a license for a full-instance sweep the user did not ask for
- Template/REST/command-line sensor YAML: TS-01..TS-07 — applied by `ha-nova:yaml-config` at write time, and by review only when such YAML is in the workset
- Helper (storage-based family): H-01..H-11
- Helper (config-entry family): minimal config-entry review plus H-12/H-13/H-15 where the fields are readable — reading `source`/member/template fields requires the non-persisting options-flow readback (the `skills/helper/SKILL.md` pattern) or the rendered-state read; when that readback is unavailable, say the fields were not readable instead of skipping silently — and H-14 when the energy prefs are already loaded
  - do not apply H-01..H-11
  - confirm config-entry metadata is present
  - inspect linked entities when available
  - in Step 2, derive collision candidates from `linked_entities[]`, not from config actions
  - run `search/related` on up to 3 linked entities
  - say explicitly that config-entry helper review does not use the storage-helper H rules
  - for the `template` domain, also read the linked entity's rendered state (`unavailable`/`unknown` is inconclusive, not proof of breakage — source state or an intentional sentinel); to apply the template-level reliability checks, open the options flow for the entry first (non-persisting readback — the canonical metadata item does not carry the `state` template)
- If an automation or script references helpers in actions or direct thresholds, also apply H-01..H-11 to those helpers (H-11 is exactly the case where the consumer is in the workset)
- R-17 is an intra-config branch comparison only. Never emit it from collision scan or cross-automation conflict analysis.
- R-18 applies only to sibling-variable references within one `variables:` mapping. Never emit it for cross-action or cross-scope references, script `fields`, HA builtins, or `{% set %}` locals inside the same template.
- For R-18 output, include the block context plus at least one concrete variable pair. For pasted YAML or draft configs, describe it as future write fragility. For HA read-back or post-write review, describe it as a persisted runtime risk.
- R-19 applies only to Jinja2 chains with `if` plus at least one `elif`, entity-state-style branch guards, and a terminal bare `else` that contains a direct `trigger.id` comparison. Skip single `if` / `else`, `trigger.id` in `elif`, non-entity-state selector trees, `else` blocks with extra explicit guards, and `choose` + `condition: trigger`.
- For R-19 output, state: final else branch is only reached when the earlier entity-state branches are false. Move the `trigger.id` check into an explicit `elif`. Or refactor to `choose` + `condition: trigger`.
- R-23 applies only to boolean-like templates compared to string boolean literals (`'True'`, `'true'`, `'False'`, `'false'`) in either comparison direction. Do not flag bare boolean checks such as `is true` or `== true`.
- R-24 is advisory only and applies only when a capacity-like variable reads an `available_energy` source. Do not hard-code integration-specific replacement entities.
- R-25 applies only to pasted or draft YAML containing `platform: template` entity definitions (domain-level blocks or bare `!include`-file lists); configs read back from HA never carry this syntax. When it fires, fetch the HA version via `/api/config` on demand and phrase the finding version-sensitively (removed as of HA 2026.6; deprecated 2025.12–2026.5; still current earlier — see the R-25 Evidence Boundary in `skills/review/checks.md`).
- M-05 is a modernize advisory for legacy automation keys (`platform:` in triggers, singular `trigger:`/`condition:`/`action:` blocks). Mention once, never as an error, never rewrite just to modernize.

**Live helper evidence for H-09/H-10:**
- See `skills/review/checks.md` → Helper Threshold Evidence
- Read `/api/states/<helper_entity_id>` only when the threshold reference is direct
- If `state`, `attributes.min`, `attributes.max`, or `attributes.step` are missing or non-numeric, skip H-09/H-10
- Do not emit unrelated findings just because an H-09/H-10 signal matched

## Safety (Critical)

- S-01: Hardcoded secrets (tokens, passwords, API keys, long webhook IDs as literals)
- S-02: `entity_id: all` or domain-wide targets without explicit intent
- S-03: Webhook trigger with `local_only: false` (exposes webhook to internet)

## Reliability (High)

- R-01 [HIGH]: `float`/`int` template filter without `default` argument
- R-02 [MEDIUM → HIGH]: `trigger: state` trigger without `to:` (fires on every attribute change). Default MEDIUM. Escalate to HIGH with concrete pile-up evidence (see R-02 Evidence Boundary).
- R-03 [MEDIUM]: Physical sensor trigger on inactive/cleared state (no-motion, door closed) without `for:` debounce — immediate-response triggers (motion detected → on) are fine without `for:`
- R-04 [MEDIUM → HIGH]: `wait_for_trigger` or `wait_template` without `timeout:`. Default to MEDIUM as a defensive safety-net recommendation. Escalate to HIGH only with concrete unrecoverable-hang evidence (see R-04 Evidence Boundary).
- R-05 [MEDIUM]: `mode` not explicitly set (defaults to `single` — re-invocations dropped with warning)
- R-06 [HIGH]: `mode: single` combined with `delay:` or `wait_*` (trigger drops during wait, logged as warning)
- R-07 [HIGH]: `mode: restart` with asymmetric on/off action pairs (partial execution risk)
- R-08 [HIGH]: `mode: parallel` referencing shared mutable state (`input_number`, `counter`, `input_boolean`)
- R-09 [MEDIUM]: `choose:` without `default:` branch (silently does nothing when no condition matches)
- R-10 [HIGH]: `mode: queued` with `delay:` or `wait_*` blocks and `max:` ≤ 3 combined with ≥ 3 triggers — queue saturation risk (triggers dropped with WARNING log when queue full during delays; truly silent only if `max_exceeded: silent` is set); severity intensifies if any trigger also matches R-02 at HIGH (unfiltered `trigger: state` without `to:` plus pile-up-prone mode multiplies trigger frequency)
- R-11 [HIGH]: `float(0)` or `int(0)` default on sensor values used in physical calculations (temperature, humidity, pressure) — 0 is physically wrong and produces silently incorrect results; use `float(none)` with an availability guard (`has_value()`) or a realistic fallback value
- R-12 [MEDIUM → HIGH]: Self-trigger / feedback loop — automation triggers on an entity (e.g., `input_select`, `input_boolean`, `input_number`) that it also sets in its own actions; HA has NO built-in self-trigger protection. Default MEDIUM. Escalate to HIGH with `mode: queued`, `mode: parallel`, or `mode: restart` (queued/parallel pile up runs until `max:` is hit; restart re-enters mid-execution so the action chain never completes). Fix: remove the trigger, add a `condition` guard comparing new vs. previous value, or use `mode: single` as partial protection. See R-12 Evidence Boundary.
- R-13 [MEDIUM]: Trigger without `id:` in `choose:`-based automations — makes `trigger.id` matching impossible; branches using `condition: trigger` require trigger IDs to function
- R-14 [MEDIUM]: Dead trigger — trigger has `id:` but that ID is never referenced in any `condition: trigger`, `choose:`, or template expression; likely copy-paste remnant or unfinished logic
- R-15 [MEDIUM]: Asymmetric error handling — same physical action (e.g., `cover.open_cover`, `climate.set_temperature`) appears in multiple branches but only some have retry/fallback logic; inconsistent reliability across code paths
- R-16 [HIGH]: Templated event name — `event_type:` does not evaluate templates in event triggers; the automation attaches to the literal string and silently misses the intended event. Use a fixed `event_type` and move dynamic logic into conditions or event data handling.
- R-17 [MEDIUM → HIGH]: Intra-config overwrite/rebound risk — the same entity/helper is written in 2+ distinct control-flow branches and the write basis is mixed. Typical risk shape: one branch advances live state incrementally, another branch later recomputes or resets from snapshot/start value/timer/fallback/baseline. Default to MEDIUM. Escalate to HIGH only when a later branch can plausibly overwrite/reset value already advanced by an earlier branch.
- R-18 [HIGH]: Same-block sibling variable dependency — within one `variables:` mapping, variable A references sibling variable B from that same `variables:` mapping. HA storage/API writes may reorder mapping keys, so the saved variable order can evaluate A before B. Apply to top-level and local `variables:` blocks only when at least one concrete fragile pair exists. Report the block context plus at least one concrete pair (for example `check_flag -> reading`). For draft or pasted YAML, frame this as future write fragility. For HA read-back or post-write review, frame it as a persisted runtime risk.
- R-19 [MEDIUM]: Unreachable `trigger.id` in bare `else` branch — a Jinja2 `if` + `elif` chain uses entity-state-style guards, and the terminal bare `else` contains a direct `trigger.id` comparison. final else branch is only reached when the earlier entity-state branches are false. Move the `trigger.id` check into an explicit `elif`. Or refactor to `choose` + `condition: trigger`.
- R-20 [MEDIUM]: `trigger_variables` using `states()`, `is_state()`, `state_attr()`, or other state-snapshot helpers — evaluated once at attach time; the captured value is stale for the lifetime of the automation and silently produces wrong values (not stylistic — real reliability hazard). Fix: use a top-level `variables:` block evaluated per run, or read state inside the action template instead. (Migrated from former M-04.)
- R-21 [HIGH]: Reverse branch without re-entry guard on capture-state flag — a complementary forward/reverse branch pair (save/restore, activate/deactivate, arm/disarm) where the forward branch saves state and sets a flag (any helper type, for example an `input_boolean`), but the reverse branch fires on its trigger alone without a condition that the flag is set. The reverse branch then also runs after cycles where the forward branch never executed and silently overwrites user-set state with stale helper values — no error, no log. Fix: guard the reverse branch on the flag being set, and clear the flag after restoring. Applies to `choose:` branches, `if/else` actions, and trigger-id-split action chains; bidirectional. See R-21 Evidence Boundary.
- R-22 [HIGH]: Restart-dependent restore from transient storage — a restore path is reachable from a Home Assistant startup trigger (`trigger: homeassistant` with `event: start`), but the state it restores was captured in a transient construct (`scene.create` runtime snapshot, automation/script `variables:`, `trigger_variables`, timer without `restore: true`). Transient constructs do not survive a restart, so the startup restore path silently does nothing or applies wrong values. Fix: persist the saved state in a helper (`input_number`, `input_text`, `input_select`, ...) or another persistent construct — see `skills/ha-nova/best-practices.md` → Persistence Model. See R-22 Evidence Boundary.
- R-23 [MEDIUM]: Boolean-like template compared to a string literal — a template compares a boolean-like variable or expression to `'True'`, `"True"`, `'true'`, `"true"`, `'False'`, `"False"`, `'false'`, or `"false"`, including reversed comparisons such as `'True' == avg_valid`. Home Assistant traces can expose template variables as real booleans, so string comparison can make a valid plan evaluate false. Fix: use the boolean directly (`{{ avg_valid }}`), negate it directly (`{{ not avg_valid }}`), or use boolean tests when genuinely needed. Do not flag bare boolean comparisons such as `== true`, `is true`, or `is sameas true`.
- R-24 [LOW]: Capacity-like variable reads `available_energy` — a variable named like `capacity`, `capacity_kwh`, `maximum_energy`, or similar reads an entity containing `available_energy`. Available charge may not be nominal or maximum battery capacity, so calculated targets can be wrong. Advisory only: verify whether a maximum, nominal, or capacity entity is the intended source. Do not hard-code integration-specific replacements or automatically rewrite entity IDs.
- R-25 [HIGH]: Legacy template platform syntax (removed in HA 2026.6) — pasted/draft YAML defines template entities under a domain platform key (`sensor:`, `binary_sensor:`, `cover:`, `fan:`, `light:`, `lock:`, `switch:`, `vacuum:`, `weather:`, or `alarm_control_panel:` containing `- platform: template`), or as a bare top-level `- platform: template` list (pasted contents of an `!include` file such as `sensors.yaml`; entity-shaped items only, see the Evidence Boundary). Home Assistant 2026.6 removed this syntax entirely: the entities silently stop being created — no error banner, automations depending on them stop firing. It was deprecated in 2025.12 and still current before that — always phrase per the version split in the R-25 Evidence Boundary. Fix: migrate to the modern top-level `template:` syntax (`value_template` → `state`, `friendly_name` → `name`, per-entity maps → list items). Applies only when reviewing pasted or draft YAML (see R-25 Evidence Boundary).

- R-26 [MEDIUM → HIGH]: Exact-state equality narrower than the stated intent — a condition/trigger pins one literal state (classic: `== 'not_home'`) on a domain whose runtime states exceed a simple pair (person/device_tracker report named zones as states; media_player, vacuum, climate carry multi-state enums), while the user's stated intent is the broader category ("nobody home", "the TV is off" — which `standby`/`idle` also satisfy). The config is valid, saves, reloads, and read-back matches — it just never matches legitimate runtime states (a person at zone `work` has state `work`, not `not_home`). Fix: express the category (`!= 'home'` for away-semantics, `zone.home` person count for nobody-home, negations over enumerations). Default MEDIUM; escalate to HIGH when the narrow comparison gates the automation's core purpose. See R-26 Evidence Boundary.
- R-27 [MEDIUM]: Fixed `delay:` standing in for asynchronous completion — an action starts an asynchronous operation (non-blocking `script.turn_on`, a device command that takes variable time) and a following fixed `delay:` is the only thing "guaranteeing" completion before dependent actions run. The delay documents a hope, not a fact. Fix: `wait_template`/`wait_for_trigger` on the actual completion signal, with `timeout:` (R-04) and a defined timeout path. Do not flag delays that are themselves the intent (light on for 5 minutes).
- R-28 [MEDIUM]: Startup race — a `trigger: homeassistant` / `event: start` path immediately reads integration-backed entity states in conditions or actions. Right after startup those states can be `unknown`, `unavailable`, or stale-restored before their integration first updates. Fix: guard with an availability wait (`wait_template` on `has_value(...)` with timeout) or accept-and-document the race. Helpers restore their own state and rarely need the guard; template sensors inherit the race from their integration-backed dependencies.

## R-02 Evidence Boundary

Without `to:`, a `trigger: state` fires on every attribute change of the entity. The footgun is real but its blast radius depends entirely on the automation's mode:

- Default severity: **MEDIUM**. Frame as "you probably did not mean for this to fire on every attribute change". Suggested wording: *"Add `to:` so the trigger only fires when the state actually changes — without it, every attribute update (signal strength, last seen, etc.) re-runs the automation."*
- Escalate to **HIGH** only with concrete pile-up evidence:
  - `mode: queued` — every attribute change consumes a queue slot; high-frequency entities (motion sensors, signal-strength reporters, climate devices) can saturate the queue.
  - `mode: parallel` — every attribute change spawns a parallel run; combined with shared mutable state this is the same hazard as R-08.
  - Action chain has non-idempotent side effects (counter increments, notifications, service calls that change physical state) — every spurious fire is a real-world side effect.
- Do not escalate purely on `mode: single` or `mode: restart`. With `single`, surplus fires drop with a warning (log noise, not breakage). With `restart`, the action is bounded.
- Skip when an explicit `not_to:`/`not_from:` filter is present and adequately scopes the trigger.

## R-04 Evidence Boundary

The pattern (a `wait_*` action without `timeout:`) is a defensive lint, not always an active bug. Severity must reflect whether the worst-case hang is recoverable.

- Default severity: **MEDIUM**. The wait pattern is suboptimal but most setups recover via re-trigger or natural state changes. Frame as a safety-net recommendation, not an alarm. Suggested wording shape: *"Add `timeout:` as a safety net — `mode: restart` already protects most cases, but a stuck sensor would still hang the chain."*
- Escalate to **HIGH** only with at least one concrete unrecoverable-hang signal:
  - `mode: single` — re-triggers are dropped while the wait is active, so a stuck condition has no recovery path. (This may also match R-06; keep R-04 distinct only when the wait itself is the diagnostic anchor.)
  - The wait sits inside a `repeat:` loop with no other timeout/break — every iteration can hang independently and queue up.
  - The awaited entity has a known-flaky platform signature (battery-powered Zigbee/BLE sensors with no `availability:`, deprecated integrations, MQTT topics with no LWT) — a stuck `on` state is the realistic failure mode, not a hypothetical one.
  - The action chain following the wait is on a critical path (e.g., off-actions, alarm disarm, security gating) where a permanent hang is materially harmful.
- Do not escalate to HIGH purely because of `mode: restart`. Restart is a real mitigation: any new trigger event resets the wait. The only restart case worth HIGH is when the trigger is so rare that "next event" is unrealistic (e.g., daily-only triggers).
- Suggested fix tone:
  - MEDIUM: *"Add `timeout:` (e.g., `'00:10:00'`) with `continue_on_timeout: true` as a defensive guard."*
  - HIGH: *"Add `timeout:` immediately — without it the action chain has no exit and `mode: single` provides no recovery."*

## R-12 Evidence Boundary

Self-trigger / feedback loop = the automation triggers on an entity that it also writes in its own actions. HA has no built-in self-trigger protection; whether this becomes an infinite loop depends on `mode:`.

- Default severity: **MEDIUM**. With `mode: single`, the second self-write while a run is active is dropped (HA logs a warning). The loop is bounded but every run produces warning-spam and the intended logic likely still misfires once. Suggested wording: *"This automation triggers on an entity it also writes — with `mode: single` the loop is bounded but every run logs a self-trigger warning. Add a `condition:` guard comparing new vs. previous value, or remove the self-targeting trigger."*
- Escalate to **HIGH** with concrete unbounded-loop evidence:
  - `mode: queued` — every self-write enqueues another run until `max:` is hit. With default `max: 10` this is ten queued runs per write, plus dropped events afterward.
  - `mode: parallel` — every self-write spawns a parallel run; combined with shared state this is catastrophic.
  - `mode: restart` — every self-write restarts the current run mid-execution; the loop never terminates, the action chain never completes.
- Skip when a `condition:` guard provably breaks the cycle (e.g., compare `trigger.to_state.state != trigger.from_state.state`, or check an idempotent set with `is_state(...)` before writing).
- Skip when the trigger and action target the same entity but with structurally different values (e.g., trigger on `state == on`, action sets `state == off`).
- This rule must verify trigger/action overlap independently. Do not infer from H-09 co-match alone.

## R-17 Evidence Boundary

- Apply only within one automation or one script. Never use collision-scan results to trigger R-17.
- First confirm same target entity/helper is written in 2+ distinct control-flow paths such as `choose`, `default`, `if`/`then`/`else`, timeout, recovery, or fallback branches.
- Then compare write-basis classes:
  - `live/incremental`: increment/decrement, add/subtract from current state, or otherwise advance existing value
  - `recompute/reset`: set from snapshot, start value, timer duration, fallback constant, or baseline rebuild
- Skip when all writes use the same basis class, when the writes are idempotent duplicates, or when fixed preset branches are the intended behavior.
- Do not use R-17 for generic repeated writes, cross-automation conflicts, or existing `mode: parallel` race cases already covered by R-08 / F-05.

## R-18 Evidence Boundary

- Apply only within one `variables:` mapping. Never derive R-18 from references that cross action boundaries, sequence steps, or outer scopes.
- Traverse all `variables:` mappings in the config, not just the top-level block:
  - root `variables:` on the automation or script
  - local `variables:` actions inside `choose`, `if` / `then` / `else`, `default`, `repeat`, and nested `sequence` blocks
- First confirm the referenced name is a sibling variable declared in that same mapping.
- Skip HA builtins and runtime vars such as `trigger`, `this`, `wait`, `repeat`, `states()`, `is_state()`, and similar documented helpers.
- Skip `{% set %}` locals inside one template. Those are internal to the template and do not depend on sibling key order in the outer `variables:` mapping.
- Skip script `fields` references and values inherited from previous `variables` actions or outer scopes.
- Skip self-references such as `count: "{{ count + 1 }}"`; those depend on outer scope or previous run context, not a sibling key.
- Use conservative matching. Do not infer a dependency from broad substring overlap or from names that only appear inside string literals/comments.
- Preferred fixes:
  - use a self-contained template with internal `{% set %}` statements
  - split the dependency across ordered `variables` actions when later actions need the derived value

## R-23 Evidence Boundary

- Apply only inside template expressions, not ordinary text, aliases, comments, or example prose.
- Match equality and inequality comparisons in either direction:
  - boolean-like expression compared to string literal
  - string literal compared to boolean-like expression
- Treat these as boolean-like:
  - variable names ending in `_valid`, `_enabled`, `_fresh`, `_ready`, `_ok`, `_active`
  - variable names starting with `is_`, `has_`, `should_`, `can_`
  - expressions using `is_number(...)`, `has_value(...)`, comparisons, `and`, `or`, or `not`
- Do not flag literal string-state checks such as `states('sensor.x') == 'on'`.
- Do not flag real boolean comparisons or tests: `== true`, `== false`, `is true`, `is false`, `is sameas true`, `is sameas false`.
- Suggested fix:
  - use the boolean value directly (`{{ plan_inputs_valid }}`)
  - use direct negation for false checks (`{{ not plan_inputs_valid }}`)

## R-24 Evidence Boundary

- Apply only when both conditions hold:
  1. the variable name is capacity-like (`capacity`, `capacity_kwh`, `battery_capacity`, `maximum_energy`, `max_energy`, or close variants)
  2. the source entity or source expression contains `available_energy`
- Keep severity LOW and wording advisory. This is domain sanity checking, not a schema or syntax error.
- Do not assume a specific integration, manufacturer, or replacement entity. Suggest verification of maximum/nominal/capacity source only.

## R-19 Evidence Boundary

- Apply only to Jinja2 `if` / `elif` / `else` chains with at least one `elif`. Skip single `if` / `else` binaries.
- First confirm the `if` / `elif` guards are entity-state-style conditions:
  - direct `is_state(...)`
  - direct `states(...)`
  - or local variables clearly derived from those calls
- Then confirm the terminal `else` is a bare catch-all and contains a direct `trigger.id` comparison.
- Skip `trigger.id` checks that already live in an explicit `elif`.
- Skip non-entity-state selector trees such as mode, numeric-range, or time-range dispatch.
- Skip `else` bodies that add explicit extra state guards alongside the `trigger.id` comparison.
- Skip the preferred `choose` + `condition: trigger` routing pattern entirely.
- Keep the warning branch-structure-specific. Do not infer trigger intent from aliases, names, or external semantics.
- Preferred fixes:
  - move the `trigger.id` check into an explicit `elif`
  - or refactor to `choose` + `condition: trigger`

## R-21 Evidence Boundary

- Apply only when all three hold:
  1. two branches in one automation or script form a complementary forward/reverse pair acting on the same target state
  2. the forward branch writes a dedicated state flag (any helper type)
  3. the reverse branch contains no guard on that flag — neither a `condition:` entry nor a template check
- Skip when no capture flag exists at all — mixed write bases without a flag are R-17 territory, not R-21.
- Treat a guard on the saved snapshot itself (for example "only restore when the snapshot helper is not `unknown`") as an equivalent re-entry guard ONLY when the restore path also clears/resets that snapshot after restoring. A persistent snapshot that is never cleared can still hold a value from an earlier cycle, so the guard does not prevent stale restores — flag it.
- Skip pairs where the reverse action is idempotent and stateless (for example plain `light.turn_off` without restoring saved values) — the hazard is restoring stale captured state, not symmetric toggling.
- Never derive the pair from cross-automation analysis; both branches must live in one config item.

## R-22 Evidence Boundary

- Apply only when both hold:
  1. a trigger or branch explicitly handles HA startup (`trigger: homeassistant` with `event: start`, or a trigger id routed into a startup branch)
  2. that startup path reads state captured by a construct from the transient list in `skills/ha-nova/best-practices.md` → Persistence Model
- Without a startup trigger, transient snapshots are fine for short-lived save/restore cycles — do not flag them.
- `scene.create` alone is not a finding; the finding is the combination with a restart-recovery expectation.
- When the storage construct is ambiguous (for example a custom integration entity), ask instead of flagging.

## R-25 Evidence Boundary

- Apply only when reviewing pasted or draft YAML that contains a domain-level list item with `platform: template`, or a bare top-level list whose items carry `platform: template` — pasted contents of an `!include` file (for example `sensors.yaml`) have no wrapping domain key but define the same removed entities. Stored automations, scripts, and helpers read back from HA never carry this syntax — do not fetch or scan `configuration.yaml` to hunt for it.
- Bare-list items count only when they are entity-shaped, meaning any of:
  - the item carries a per-entity map: `sensors:`, `binary_sensors:`, `switches:`, `covers:`, `fans:`, `lights:`, `locks:`, `vacuums:`, or `panels:` (legacy alarm_control_panel)
  - the item carries legacy weather template fields directly (for example `condition_template:`, `temperature_template:`)
  - the user names an entity include (such as `sensors.yaml` or `weather.yaml`)
  A bare `- platform: template` item whose only template field is a direct `value_template:` (no per-entity map, no weather fields) is a legacy template trigger from a trigger include — that is M-05 territory, never R-25.
- Version-sensitive phrasing: fetch the HA version via `ha-nova relay core --method GET --path /api/config` only when this check actually fires (never as a routine per-review call), then phrase the finding accordingly:
  - HA >= 2026.6: removed — the entities are silently gone (keep HIGH).
  - HA 2025.12 to 2026.5: deprecated, still functional — downgrade the finding to MEDIUM and recommend migrating before upgrading.
  - HA < 2025.12: the syntax is still current on that version (deprecation started in 2025.12) — keep MEDIUM, do not call it deprecated; say it will stop working once the instance upgrades to 2026.6+ and recommend migrating ahead of that upgrade.
  - Version unavailable: state the outcomes conditionally.
- Do not confuse with the config-entry `template` helper domain (`ha-nova:helper`) or with `trigger: template` triggers — this check is only about `platform: template` entity definitions.
- When R-25 pulls a pasted template block into review scope, apply the template-level reliability checks (R-01, R-11, R-23) to its templates as well — those defects survive the migration to modern syntax.
- Migration hint shape (do not rewrite automatically; show the target shape):
  ```yaml
  template:
    - sensor:
        - name: "My Sensor"
          state: "{{ ... }}"
  ```

## R-26 Evidence Boundary

- Flag only when the stated or obvious intent is a CATEGORY and the comparison
  pins one literal that under-covers it. `== 'work'` for "when Markus is at
  work" is exactly right — never flag intent-matching literals.
- The criterion is under-coverage of the intent, not open vs closed state
  sets: two-state domains (binary_sensor on/off) and comparisons that cover
  the intended category exactly are fine.
- The classic instance: person/device_tracker away-semantics via
  `== 'not_home'` — any named zone (work, school, gym) silently fails the
  comparison although the person is away. Home-semantics via `== 'home'` is
  normally correct — only a user-defined zone placed inside the home radius
  can shadow it; do not flag it by default.
- This is a static check: it cannot prove which zones exist. Phrase it as "this
  comparison misses valid states like named zones" and show the category-safe
  form; never claim the automation is currently broken.

## Performance (Medium)

- P-01: `trigger: template` trigger that could be a `trigger: state` trigger (see `skills/ha-nova/template-guidelines.md` → Decision Tree)
- P-02: `homeassistant.update_entity` inside a `repeat:` loop without meaningful delay
- P-03: Polling loop (`repeat: while:` + short `delay:`) instead of `wait_for_trigger`
- P-04 [MEDIUM → HIGH]: Template trigger using `now()` for time-sensitive logic — re-evaluates only once per minute; for sub-minute precision use `time_pattern` trigger or a dedicated sensor. Default MEDIUM. Escalate to HIGH only with concrete sub-minute precision needs (see P-04 Evidence Boundary).
- P-05 [LOW]: `trigger: device` with `device_id` where a `trigger: state` with `entity_id` would work — `device_id` is not persistent across device re-adds; exception: Zigbee2MQTT autodiscovered device triggers and ZHA button/remote events (see `skills/ha-nova/best-practices.md` → Zigbee Button Patterns)

## Style (Low)

- M-01: Missing `alias:`
- M-02: Deprecated `service:` key instead of `action:`
- M-03: `entity_id:` under `data:` instead of `target: entity_id:`
- M-04: *retired — moved to Reliability as R-20*
- M-05 [LOW]: Legacy automation syntax keys — `platform:` inside a trigger item (renamed to `trigger:` in HA 2024.10), or singular top-level `trigger:`/`condition:`/`action:` blocks (renamed to `triggers:`/`conditions:`/`actions:`). Both forms still work and auto-migrate when edited in the HA UI; this is a modernize advisory only. Mention it once per reviewed target (in bulk mode fold repeats into the Repeated Patterns section), never as an error, and never rewrite a config just to modernize the keys.

## Script-Specific (apply ONLY when domain is `script`, skip for automations)

- F-01 [HIGH]: `fields:` entry without `selector:` (UI shows raw text box for all types)
- F-02 [HIGH]: `fields:` with `required: true` or `default:` but no `| default(...)` guard in `variables:` block — `required` and `default` are UI-only, not enforced at runtime
- F-03 [MEDIUM]: Template `{{ field_name }}` in sequence without corresponding `variables:` guard — fails silently when caller omits field
- F-04 [MEDIUM]: `mode: queued` or `mode: parallel` without explicit `max:` value
- F-05 [HIGH]: `mode: parallel` with actions writing to same entity (race condition) — same hazard as R-08; severity raised for parity. Use `mode: queued` or sequence-level locks if parallel concurrency is required.
- F-06 [MEDIUM]: `action: script.turn_on` (non-blocking) when next step depends on result — use blocking `action: script.{id}` instead
- F-07 [LOW]: Script contains `wait_for_trigger:` at top of sequence with no preceding logic — likely should be an automation
- F-08 [LOW]: Hardcoded values that vary per call-site should be `fields:` parameters (human-judgment check — flag only obvious cases like repeated entity_ids or magic numbers)
- F-09 [LOW]: Orphaned script — not invoked by any automation, script, or scene (`search/related`; cleanup hint like H-07, never a runtime hazard). `search/related` does NOT index dashboards: either scan the storage dashboards for `script.*` card actions, or say dashboard usage cannot be ruled out

## Helper-Specific (apply when reviewing helpers or automations referencing helpers)

- H-01 [HIGH]: `input_number` without explicit `min`/`max` — HA defaults 0/100, likely wrong for physical quantities
- H-02 [MEDIUM]: `input_boolean`/`input_select` as condition guard without `homeassistant.started` initializer — state unknown after restart
- H-03 [MEDIUM]: `input_number` `mode: box` with wide range and no `step` — easy to mistype values
- H-04 [LOW]: `input_select` `initial` not set — defaults to first option, may not be intended
- H-05 [MEDIUM]: `counter` without `minimum`/`maximum` — unbounded growth risk
- H-06 [LOW]: `timer` without `duration` — must be set via service call before start
- H-07 [LOW]: Orphaned helper — not referenced by any automation/script (cleanup hint, not a runtime hazard; check via `search/related`; for cleanup workflow see `skills/ha-nova/safe-refactoring.md` → Orphan Cleanup)
- H-08 [LOW]: Naming inconsistency — mixed patterns across helpers (e.g., `sleep_mode` vs `Sleep Mode` vs `sleepMode`)
- H-09 [MEDIUM → HIGH]: Threshold effectively weakened — `input_number` is used as a direct threshold and its current value sits at or near the boundary that makes the guard trivially easy to satisfy. Operator-aware: `>`/`>=` is risky near `min`; `<`/`<=` is risky near `max`. "Near" means within `1 × step`, including the exact boundary. Escalate to HIGH only with concrete loop evidence (`repeat:`, or R-10/R-12 matched at HIGH also applies).
- H-10 [LOW]: Threshold value off the configured step grid — current `input_number` value does not land on the configured `step` lattice relative to `min`; likely set programmatically rather than through the UI. Supplementary signal for H-09, not a severity escalator by itself.
- H-11 [LOW]: Unit mismatch — a helper's `unit_of_measurement` disagrees with the unit the consuming template/automation treats it as; flag only when both units are actually visible in the workset
- H-12 [MEDIUM]: Config-entry helper source entity absent — `source`/`entity_id` of a `utility_meter`/`derivative`/`integration`/`min_max`/`threshold`/`statistics`/`history_stats` entry resolves in NEITHER the entity registry NOR `/api/states`
- H-13 [MEDIUM]: `group` helper with a dead or duplicate member entity
- H-14 [LOW]: `utility_meter` cycle/tariff disagrees with the energy-dashboard tariff configuration — only when the energy prefs are ALREADY loaded in this review; never fetch them just for this check
- H-15 [MEDIUM]: `template` config-entry helper renders `unavailable`/`unknown` because a referenced entity id resolves to nothing — pair the rendered-state read with an entity-id resolution before flagging

H-12/H-13/H-15 dead-source/member findings require absence from BOTH the
entity registry AND `/api/states` — YAML/manual entities live in states
without a registry record (same boundary as SC-01/D-01/HX).

## Scene-Specific (apply when reviewing storage scenes)

- SC-01 [HIGH]: Dead entity reference — a key under `entities:` resolves in NEITHER the entity registry NOR `/api/states`; the scene applies partially and silently
- SC-02 [MEDIUM]: Mixed color attributes on one light — more than one of `color_temp_kelvin`/`hs_color`/`rgb_color`/`xy_color`/`rgbw_color`/`rgbww_color` captured; reproduction depends on the active color mode and is unreliable
- SC-03 [MEDIUM]: Light group captured instead of member lights — group reproduce-state is a known trouble spot; suggest capturing the members
- SC-04 [LOW]: Read-only domain captured (`sensor`, `binary_sensor`, ...) — a scene cannot reproduce it
- SC-05 [LOW]: Measurement/diagnostic attribute captured (battery, rssi, ...) instead of writable target attributes
- SC-06 [MEDIUM]: Captured color attribute outside the entity's `supported_color_modes` — the device cannot reproduce it
- SC-07 [LOW]: Orphaned scene — no automation/script references it (`search/related`), no storage-dashboard card action calls it, and its state timestamp shows no recent activation; cleanup hint, never a defect

## SC Evidence Boundaries

- SC-01 requires absence from BOTH the entity registry AND `/api/states` — YAML-defined entities live in states without a registry entry, and an `unavailable` entity is offline, not deleted.
- SC-02/SC-06 need the live entity's `supported_color_modes`; never flag from the scene config alone.
- SC-07: `search/related` does not index dashboards. Scan card actions across ALL storage dashboards before emitting the hint; if the scan is incomplete or YAML-mode dashboards are present, say dashboard usage cannot be ruled out and skip the cleanup hint. Scene state `unknown` means "never activated", which strengthens the evidence but proves nothing broken.

## Dashboard-Specific (apply when reviewing storage dashboards)

- D-01 [HIGH]: Broken card entity reference — an `entity`/`entities[]` id absent from BOTH the registry AND `/api/states`; the card renders permanently unavailable
- D-02 [HIGH]: `custom:` card with no matching entry in `lovelace/resources` — the card cannot render at all
- D-03 [MEDIUM]: Duplicate view `path` within one dashboard — routing collision
- D-04 [LOW]: Empty view, or a view whose only card is broken
- D-05 [MEDIUM]: Orphaned Lovelace resource — no `custom:` card, custom dashboard/view strategy, or custom view type across the storage dashboards references it (cleanup hint; say that YAML-mode dashboards are invisible to this scan and cannot be ruled out)
- D-06 [LOW]: Card references a registry-disabled entity (`disabled_by` is non-null) — it does not produce a state, so the card stays unavailable
- D-07 [MEDIUM]: Built-in card missing its required field — authoritative minimal schema: `entity`/`tile`/`gauge`/`sensor` require `entity`; `entities`/`history-graph` require a non-empty `entities` list; `markdown` requires `content`; `button` has no required field. Judge ONLY these allowlisted types; never infer custom-card schemas

## D Evidence Boundaries

- D-01 requires absence from BOTH the registry AND `/api/states` (YAML/manual entities have states but no registry entry); D-06 requires non-null `disabled_by` on an entity that DOES have a registry entry — never flag `hidden_by` alone.
- All dashboard D/HX scans traverse the full dashboard object recursively. Follow nested `cards[]`, singular `card`, `elements[]`, `badges[]`, `sections[]`, and header-card structures; inspect every nested card `type`, entity reference, and `tap_action`/`hold_action`/`double_tap_action` service target before applying D-01, D-02, D-05, or HX-05. A top-level-only scan is invalid.
- D-02/D-05 join `lovelace/config` across ALL storage dashboards with `lovelace/resources` — scan card `type`, view `type`, and dashboard/view `strategy.type`; partial joins produce false orphans, so skip D-05 when not all dashboards were read.
- D-07 checks exactly the minimal schema spelled out in the rule — no field beyond the one listed is ever required, and `custom:` cards are never judged.

## Cross-Item (aggregate/bulk reviews with registry context)

- HX-01 [HIGH]: Automation/script action targets a `scene.*`/`script.*` entity that no longer exists
- HX-02 [HIGH]: Automation/script references an `input_*`/`counter`/`timer`/`schedule` helper absent from the registry
- HX-03 [MEDIUM]: `target.area_id`/`floor_id`/`label_id`/`device_id` no longer present in its registry
- HX-04 [MEDIUM]: Trigger/condition references a `zone.*`/`person.*` entity that was deleted
- HX-05 [LOW]: Dashboard card action calls a scene/script that no longer exists

## HX Evidence Boundaries

- HX fires only when the referenced item is confirmed ABSENT from its authoritative registry AND (for entity-shaped references) from `/api/states` — YAML-defined items have states without registry entries; a failed state read or `unavailable` is not deleted.
- Scope stays the review workset the user asked for; the registry lists may be read once to resolve references, but HX never expands the workset itself.

## Template/REST/Command-line Sensors (TS — YAML files)

Applied by `ha-nova:yaml-config` before writing sensor YAML; review applies them only when such YAML sits in the workset (pasted, or read through opt-in file access).

- TS-01 [HIGH]: Template references an entity id absent from both registry and states — the sensor goes silently `unavailable`
- TS-02 [HIGH]: `float`/`int` filter without `default` in a state/value template (the R-01 hazard; YAML sensor files never reach the automation reviewer)
- TS-03 [MEDIUM]: `rest`/`command_line` sensor without an `availability` template — a down source propagates `unknown` into consumers
- TS-04 [MEDIUM]: Aggressive `scan_interval` (sub-10 s) against a remote or expensive source
- TS-05 [HIGH]: `command_line` command interpolates a secret or an unvalidated template input (the S-01 hazard on a shell boundary)
- TS-06 [LOW]: `unit_of_measurement`/`device_class`/`state_class` combination inconsistent — breaks long-term statistics and energy
- TS-07 [MEDIUM]: Duplicate `unique_id` or duplicate sensor name across the template blocks of the file

## P-04 Evidence Boundary

`now()` in a template trigger is re-evaluated only when another tracked entity in the template changes, or once per minute as a fallback. For sub-minute precision the trigger is materially broken — it cannot fire at the resolution the surrounding logic implies.

- Default severity: **MEDIUM**. Suggested wording: *"This template trigger uses `now()`, which only re-evaluates when a tracked entity changes or once per minute. For sub-minute precision use a `time_pattern` trigger or a dedicated time sensor."*
- Escalate to **HIGH** with concrete sub-minute precision evidence:
  - A sibling `for:` or `timeout:` shorter than 60 seconds — the trigger cannot resolve before the deadline.
  - The template compares `now()` to a timestamp delta below 60 seconds (e.g., `(now() - states(...).last_changed).total_seconds() < 30`).
  - The automation gates safety/security actions (alarm arming, lock state, presence-based access) where minute-level drift is materially harmful.
  - The automation is a watchdog whose stated purpose (in `alias:`/`description:` or referenced helpers) is to detect stale data — once-per-minute evaluation can mask the very stalled-data condition the watchdog is designed to detect.
- Skip entirely when `time_pattern` or a dedicated time sensor (`sensor.time`, `sensor.date_time`) is already present in the same trigger block.
- Do not escalate purely on the presence of `now()`. The minute-cadence is acceptable for hour-/day-scale logic.

## Helper Threshold Evidence (for H-09/H-10)

- Apply only for direct threshold references, not broad heuristics:
  - `numeric_state` with helper-backed `above`/`below`
  - direct template comparisons where an explicit `input_number.<id>` appears in the compared expression
- Read live helper evidence via:
  - `ha-nova relay core --method GET --path /api/states/<helper_entity_id> --jq-file <state-filter-file> --out <state-file>`
  - Write `<state-filter-file>` with `if .ok then .data.body else empty end`
- Use `state` plus `attributes.min`, `attributes.max`, and `attributes.step`. If any of these are missing or non-numeric, skip H-09/H-10.
- For H-10, check the step lattice relative to `min`, not `value % step`. Use a small float tolerance when deciding whether `(value - min) / step` is effectively an integer.
- `choose:` alone is not enough for HIGH severity. Escalate only when the weakened threshold also participates in concrete loop-capable control flow (`repeat:` or already-matched R-10/R-12).
- Do not emit R-10 just because H-09 matched. Report R-10 only when its own queue-saturation criteria are independently satisfied.

## Known Safe Patterns (do NOT warn)

- Motion on → light on + No motion (with `for:`) → light off = complementary pair
- Goodnight routine → all off + Motion → specific light on = intentional override
- Sunrise → open + Sunset → close = mutually exclusive time windows
- Automation A and B target same entity but with non-overlapping value ranges (e.g., brightness 0-50 vs 51-100)

## Known Problem Patterns (DO warn)

- **Flip-Flop:** Automation A turns entity on (schedule/event), Automation B turns it off (timer/no-motion), triggers can overlap with no guard → entity bounces
- **Cascade:** Automation A changes entity X, entity X is template dependency, Automation B triggers on X → unintended chain reaction
- **Race Condition:** Two automations with `delay:` targeting same entity, both can fire before other's delay expires
- **Stale Helper:** `input_boolean` used as condition guard, no `homeassistant.started` initializer → wrong state after restart
- **Startup Flash:** Template sensor trigger without `unknown`/`unavailable` from_state guard → fires on HA restart
- **Self-Trigger Loop:** Automation triggers on entity X and sets entity X in actions → re-triggers itself; with `mode: queued`/`parallel` this creates infinite loop consuming queue slots until `max:` is hit (see also R-12)
