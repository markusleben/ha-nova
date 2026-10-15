# HA NOVA Review Checks — Extended

Canonical path: `skills/review/checks-extended.md`

On-demand part of `skills/review/checks.md` (whose Output Guardrail, Verify
Before Flagging, and Application rules apply here too): the Evidence
Boundaries, the H-09/H-10 threshold evidence, and the scene, dashboard,
cross-item, and template/REST/command-line sensor families.

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

## R-29 Evidence Boundary

- Apply only when watchdog, self-healing, or continuous-monitoring
  CONTINUITY is declared by the user or promised by the config's own name or
  description. A recovery-shaped action alone (restart, reload, reconnect) is
  never that evidence — an intentionally one-shot recovery is its own valid
  class. Ordinary one-shot threshold automations are never flagged when no
  continuity promise exists.
- Name the incomplete design honestly a "one-shot recovery attempt", never a
  "watchdog"; report the gap and the persistent-fault options (bounded retry,
  periodic re-evaluation, failure escalation).
- Report only: never rewrite the automation and never execute its recovery
  actions to test liveness.

## R-30 Evidence Boundary

- Apply only to configs that already contain retry/recovery machinery; never
  suggest adding retries to ordinary service calls.
- An ambiguous transport failure never justifies another attempt — the first
  action may already have been applied. Flag retries keyed on it; do not flag
  their absence.
- No universal retry count, delay, or backoff exists: flag missing or
  unbounded values, never impose specific ones.
- Report only: never rewrite the workflow and never execute recovery actions.

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
