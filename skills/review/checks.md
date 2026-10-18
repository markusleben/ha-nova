# HA NOVA Review Check Catalog

Canonical path: `../review/checks.md`

Core catalog (its extended part is named below): load this file before evaluating findings — from `../review/SKILL.md` for standalone reviews, or directly from `../write/SKILL.md` / `../helper/SKILL.md` post-write phases.

Split for load size: Evidence Boundaries (R-02 … R-30, P-04), the H-09/H-10
threshold evidence, and the scene, dashboard, cross-item, and template/REST/
command-line sensor families live in `../review/checks-extended.md`.
Before flagging or escalating a check that has a boundary there — R-02, R-04,
R-12, R-17, R-18, R-19, R-21, R-22, R-23, R-24, R-25, R-26, R-29, R-30, P-04,
and the H-09/H-10 threshold evidence — read that boundary; before reviewing a scene, dashboard,
bulk/cross-item set, or YAML sensor, read its family section there.

## Output Guardrail (Critical)

- Apply `../ha-nova/output-rules.md` whenever this catalog informs user-facing output.
- Check codes (`R-17`, `S-01`, `H-02`, ...) are internal reasoning artifacts, not user-facing labels.
- Instead, describe each finding in plain language: a short descriptive title plus why it matters and how to fix it.
- This guardrail applies whenever check knowledge is used, not only during formal review runs.

## Verify Before Flagging (Critical)

A finding that turns out to be valid Home Assistant costs the user more trust
than a missed one costs them behavior. Before reporting ANY issue, resolve it
against a source rather than memory, in this order:

1. `../ha-nova/template-guidelines.md` for template and Jinja questions,
   `../ha-nova/best-practices.md` and
   `../ha-nova/automation-patterns.md` for trigger/action/mode shapes,
   `../ha-nova/helper-schemas.md` and
   `../ha-nova/helper-flow-schemas.md` for helper fields.
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
4. Scene, dashboard and cross-item families split their evidence in two, and
   conflating the halves is how an unsupported finding gets shipped:
   - EXISTENCE and JOIN facts — this card references a resource that is not
     in `lovelace/resources`, this scene captures two colour attributes, this
     entity_id appears in three automations — come from the live data the run
     already read. Nothing else can prove them.
   - The BEHAVIOURAL claim attached to them comes from a named source, and
     each claim has exactly one: that mixed colour attributes reproduce wrong
     or a domain is not reproducible — `../scene/SKILL.md` → Capture
     attributes deliberately; that a built-in card field is required — the
     D-07 allowlist (`checks-extended.md`), which is the only schema pin in this repo and is
     deliberately closed (a type not on it has no known required field, so it
     produces no finding); that a save loses omitted content or a custom-card
     schema cannot be invented — `../dashboard/SKILL.md` → Critical
     behavior. Beyond those, the Lovelace and scene pages on
     home-assistant.io. Citing a section that does not carry the claim is the
     same error as citing nothing.
   State the observation from the data and the consequence from the source.
   If only the observation holds, report the observation.
5. Unresolved means unresolved: report it as a question, never as an error.

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

Orphan/no-consumer verdicts (F-09, H-07, SC-07, collision scans) fail closed: read `search/related` through `../ha-nova/search-related-consumers.jq` (recreate per `../ha-nova/relay-api.md` → Parsing rule on flat-copy installs; its automation/script/scene projection covers F-09's scene requirement) — or, through `ha-nova get <entity_id> --related`, the lists `.result.related.automation`/`.script`/`.scene` (a non-zero exit is a failed scan); a failed or unexecuted scan is inconclusive — never evidence of an orphan.

**Apply these families by domain:**
- Automation: S-01..S-03, R-01..R-31, P-01..P-05, M-01..M-05
- Script: automation families plus F-01..F-09
- Storage scene: SC-01..SC-07 (rules: `checks-extended.md`)
- Storage dashboard: D-01..D-07 (rules: `checks-extended.md`)
- Cross-item: HX-01..HX-05 — only during aggregate/bulk reviews or when the registry context is already loaded; never a license for a full-instance sweep the user did not ask for
- Template/REST/command-line sensor YAML: TS-01..TS-07 — applied by `ha-nova:yaml-config` at write time, and by review only when such YAML is in the workset
- Helper (storage-based family): H-01..H-11
- Helper (config-entry family): minimal config-entry review plus H-12/H-13/H-15 where the fields are readable — reading `source`/member/template fields requires the non-persisting options-flow readback (the `../helper/SKILL.md` pattern) or the rendered-state read; when that readback is unavailable, say the fields were not readable instead of skipping silently — and H-14 when the energy prefs are already loaded
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
- For R-18 output, include the block context plus at least one concrete variable pair. For pasted YAML or draft configs, describe it as a bug the save would keep. For HA read-back or post-write review, describe it as a persisted runtime risk.
- R-19 applies only to Jinja2 chains with `if` plus at least one `elif`, entity-state-style branch guards, and a terminal bare `else` that contains a direct `trigger.id` comparison. Skip single `if` / `else`, `trigger.id` in `elif`, non-entity-state selector trees, `else` blocks with extra explicit guards, and `choose` + `condition: trigger`.
- For R-19 output, state: final else branch is only reached when the earlier entity-state branches are false. Move the `trigger.id` check into an explicit `elif`. Or refactor to `choose` + `condition: trigger`.
- R-23 applies only to boolean-like templates compared to string boolean literals (`'True'`, `'true'`, `'False'`, `'false'`) in either comparison direction. Do not flag bare boolean checks such as `is true` or `== true`.
- R-24 is advisory only and applies only when a capacity-like variable reads an `available_energy` source. Do not hard-code integration-specific replacement entities.
- R-25 applies only to pasted or draft YAML containing `platform: template` entity definitions (domain-level blocks or bare `!include`-file lists); configs read back from HA never carry this syntax. When it fires, fetch the HA version via `/api/config` on demand and phrase the finding version-sensitively (removed as of HA 2026.6; deprecated 2025.12–2026.5; still current earlier — see the R-25 Evidence Boundary in `../review/checks-extended.md`).
- M-05 is a modernize advisory for legacy automation keys (`platform:` in triggers, singular `trigger:`/`condition:`/`action:` blocks). Mention once, never as an error, never rewrite just to modernize.

**Live helper evidence for H-09/H-10:**
- See `../review/checks-extended.md` → Helper Threshold Evidence
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
- R-18 [MEDIUM]: Same-block sibling variable dependency — within one `variables:` mapping, variable A references sibling B declared BELOW it in that same `variables:` mapping. HA renders top-down, so A renders before B exists. B above A: no finding. Apply to top-level and local `variables:` blocks only when at least one concrete fragile pair exists. Output: Application → R-18 output.
- R-19 [MEDIUM]: Unreachable `trigger.id` in bare `else` branch — a Jinja2 `if` + `elif` chain uses entity-state-style guards, and the terminal bare `else` contains a direct `trigger.id` comparison. Output and fix: Application → R-19 output.
- R-20 [MEDIUM]: `trigger_variables` using `states()`, `is_state()`, `state_attr()`, or other state-snapshot helpers — evaluated once at attach time; the captured value is stale for the lifetime of the automation and silently produces wrong values (not stylistic — real reliability hazard). Fix: use a top-level `variables:` block evaluated per run, or read state inside the action template instead. (Migrated from former M-04.)
- R-21 [HIGH]: Reverse branch without re-entry guard on capture-state flag — a complementary forward/reverse branch pair (save/restore, activate/deactivate, arm/disarm) where the forward branch saves state and sets a flag (any helper type, for example an `input_boolean`), but the reverse branch fires on its trigger alone without a condition that the flag is set. The reverse branch then also runs after cycles where the forward branch never executed and silently overwrites user-set state with stale helper values — no error, no log. Fix: guard the reverse branch on the flag being set, and clear the flag after restoring. Applies to `choose:` branches, `if/else` actions, and trigger-id-split action chains; bidirectional. See R-21 Evidence Boundary.
- R-22 [HIGH]: Restart-dependent restore from transient storage — a restore path is reachable from a Home Assistant startup trigger (`trigger: homeassistant` with `event: start`), but the state it restores was captured in a transient construct (`scene.create` runtime snapshot, automation/script `variables:`, `trigger_variables`, timer without `restore: true`). Transient constructs do not survive a restart, so the startup restore path silently does nothing or applies wrong values. Fix: persist the saved state in a helper (`input_number`, `input_text`, `input_select`, ...) or another persistent construct — see `../ha-nova/best-practices.md` → Persistence Model. See R-22 Evidence Boundary.
- R-23 [MEDIUM]: Boolean-like template compared to a string literal — a template compares a boolean-like variable or expression to `'True'`, `"True"`, `'true'`, `"true"`, `'False'`, `"False"`, `'false'`, or `"false"`, including reversed comparisons such as `'True' == avg_valid`. Home Assistant traces can expose template variables as real booleans, so string comparison can make a valid plan evaluate false. Fix: use the boolean directly (`{{ avg_valid }}`), negate it directly (`{{ not avg_valid }}`), or use boolean tests when genuinely needed. Do not flag bare boolean comparisons such as `== true`, `is true`, or `is sameas true`.
- R-24 [LOW]: Capacity-like variable reads `available_energy` — a variable named like `capacity`, `capacity_kwh`, `maximum_energy`, or similar reads an entity containing `available_energy`. Available charge may not be nominal or maximum battery capacity, so calculated targets can be wrong. Advisory only: verify whether a maximum, nominal, or capacity entity is the intended source. Do not hard-code integration-specific replacements or automatically rewrite entity IDs.
- R-25 [HIGH]: Legacy template platform syntax (removed in HA 2026.6) — pasted/draft YAML defines template entities under a domain platform key (`sensor:`, `binary_sensor:`, `cover:`, `fan:`, `light:`, `lock:`, `switch:`, `vacuum:`, `weather:`, or `alarm_control_panel:` containing `- platform: template`), or as a bare top-level `- platform: template` list (pasted contents of an `!include` file such as `sensors.yaml`; entity-shaped items only, see the Evidence Boundary). Home Assistant 2026.6 removed this syntax entirely: the entities silently stop being created — no error banner, automations depending on them stop firing. It was deprecated in 2025.12 and still current before that — always phrase per the version split in the R-25 Evidence Boundary. Fix: migrate to the modern top-level `template:` syntax (`value_template` → `state`, `friendly_name` → `name`, per-entity maps → list items). Applies only when reviewing pasted or draft YAML (see R-25 Evidence Boundary).

- R-26 [MEDIUM → HIGH]: Exact-state equality narrower than the stated intent — a condition/trigger pins one literal state (classic: `== 'not_home'`) on a domain whose runtime states exceed a simple pair (person/device_tracker report named zones as states; media_player, vacuum, climate carry multi-state enums), while the user's stated intent is the broader category ("nobody home", "the TV is off" — which `standby`/`idle` also satisfy). The config is valid, saves, reloads, and read-back matches — it just never matches legitimate runtime states (a person at zone `work` has state `work`, not `not_home`). Fix: express the category (`!= 'home'` for away-semantics, `zone.home` person count for nobody-home, negations over enumerations). Default MEDIUM; escalate to HIGH when the narrow comparison gates the automation's core purpose. See R-26 Evidence Boundary.
- R-27 [MEDIUM]: Fixed `delay:` standing in for asynchronous completion — an action starts an asynchronous operation (non-blocking `script.turn_on`, a device command that takes variable time) and a following fixed `delay:` is the only thing "guaranteeing" completion before dependent actions run. The delay documents a hope, not a fact. Fix: `wait_template`/`wait_for_trigger` on the actual completion signal, with `timeout:` (R-04) and a defined timeout path. Do not flag delays that are themselves the intent (light on for 5 minutes).
- R-28 [MEDIUM]: Startup race — a `trigger: homeassistant` / `event: start` path immediately reads integration-backed entity states in conditions or actions. Right after startup those states can be `unknown`, `unavailable`, or stale-restored before their integration first updates. Fix: guard with an availability wait (`wait_template` on `has_value(...)` with timeout) or accept-and-document the race. Helpers restore their own state and rarely need the guard; template sensors inherit the race from their integration-backed dependencies.
- R-29 [HIGH]: Edge-triggered "watchdog" without a persistent-fault path — the name, description, or user request promises watchdog/self-healing/continuous-monitoring continuity (a recovery-shaped action alone is never that evidence — an intentionally one-shot recovery is its own class), but every entry is a one-time firing — an unhealthy-state edge trigger (`numeric_state`, `state` to an unhealthy value, binary health sensor, template false→true), with or without a `homeassistant` startup trigger beside it (startup fires once per boot and keeps nothing live) — and no bounded retry, periodic re-evaluation, or failure escalation exists. A failed recovery with a continuously unhealthy signal never re-fires, and an HA restart into an existing fault sees no transition. Also flag a missing post-action health re-check (`../ha-nova/outcome-verification.md`), and evaluate multi-target recovery liveness per target. Contract: `../ha-nova/recovery-workflows.md`. See R-29 Evidence Boundary.
- R-30 [MEDIUM → HIGH]: Retry-policy violation in a recovery workflow — an unbounded or implicit attempt count (`repeat`/`until` without a finite bound, self-re-triggering loop), a retry keyed on a bare service-call error or an ambiguous transport failure instead of semantic failure evidence, a retried physical-access/irreversible/non-idempotent action, missing outcome verification between attempts (a SINGLE-attempt recovery without its post-action health probe is the same defect — one-shot never exempts the probe), no `mode`/guard against overlapping recovery runs, periodic re-entry without a cooldown where one is necessary (an interval that already exceeds the bounded execution time with a non-overlapping `mode` needs none), no explicit delay or backoff between attempts (an immediate re-attempt hammers the failing device — the owning policy requires a per-case choice), retry state shared across targets, exhaustion emitting more than one notification per incident (notification storm), or NO explicit exhaustion path at all — a finite loop that simply ends after the last failed probe silently abandons the unhealthy target (an explicitly ONE-SHOT recovery with its single action and post-action probe is exempt: reporting the probe's honest outcome IS its end state, no retry or escalation was requested). Policy: `../ha-nova/recovery-workflows.md`. Default MEDIUM; escalate to HIGH for unbounded loops, unsafe repeat actions, or ambiguous-failure retries. See R-30 Evidence Boundary.
- R-31 [MEDIUM → HIGH]: State trigger skipping an in-between state — `from:` → `to:` of lock `locked`↔`unlocked` (or → `open`), cover/valve `open`↔`closed`, alarm `disarmed` → `armed_*` or `armed_*` → `triggered`; many integrations report `unlocking`/`opening`/`arming`/`pending` between, so it may never fire. HIGH only when history shows that state. Fix: add it to `from:` (`from: [locked, unlocking]`).

## Performance (Medium)

- P-01: `trigger: template` trigger that could be a `trigger: state` trigger (see `../ha-nova/template-guidelines.md` → Decision Tree)
- P-02: `homeassistant.update_entity` inside a `repeat:` loop without meaningful delay
- P-03: Polling loop (`repeat: while:` + short `delay:`) instead of `wait_for_trigger`
- P-04 [MEDIUM → HIGH]: Template trigger using `now()` for time-sensitive logic — re-evaluates only once per minute; for sub-minute precision use `time_pattern` trigger or a dedicated sensor. Default MEDIUM. Escalate to HIGH only with concrete sub-minute precision needs (see P-04 Evidence Boundary).
- P-05 [LOW]: `trigger: device` with `device_id` where a `trigger: state` with `entity_id` would work — `device_id` is not persistent across device re-adds; exception: Zigbee2MQTT autodiscovered device triggers and ZHA button/remote events (see `../ha-nova/best-practices.md` → Zigbee Button Patterns)

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
- H-07 [LOW]: Orphaned helper — not referenced by any automation/script (cleanup hint, not a runtime hazard; check via `search/related`; for cleanup workflow see `../ha-nova/safe-refactoring.md` → Orphan Cleanup)
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
