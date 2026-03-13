# HA NOVA Review Check Catalog

Canonical path: `skills/ha-nova-review/checks.md`

Load this catalog from `skills/ha-nova-review/SKILL.md` Step 1 before evaluating findings.

## Check Taxonomy (internal only)

- Format: `{CATEGORY}-{NN}` (example: `H-09`)
- Category letter = family: `S` safety, `R` reliability, `P` performance, `M` style, `F` script-specific, `H` helper-specific
- Number = running rule number within that family
- Severity is separate from the code
- Codes are for your internal reasoning only; never show them in user-facing output

## Safety (Critical)

- S-01: Hardcoded secrets (tokens, passwords, API keys, long webhook IDs as literals)
- S-02: `entity_id: all` or domain-wide targets without explicit intent
- S-03: Webhook trigger with `local_only: false` (exposes webhook to internet)

## Reliability (High)

- R-01 [HIGH]: `float`/`int` template filter without `default` argument
- R-02 [HIGH]: `trigger: state` trigger without `to:` (fires on every attribute change)
- R-03 [MEDIUM]: Physical sensor trigger on inactive/cleared state (no-motion, door closed) without `for:` debounce — immediate-response triggers (motion detected → on) are fine without `for:`
- R-04 [HIGH]: `wait_for_trigger` or `wait_template` without `timeout:`
- R-05 [MEDIUM]: `mode` not explicitly set (defaults to `single` — re-invocations dropped with warning)
- R-06 [HIGH]: `mode: single` combined with `delay:` or `wait_*` (trigger drops during wait, logged as warning)
- R-07 [HIGH]: `mode: restart` with asymmetric on/off action pairs (partial execution risk)
- R-08 [HIGH]: `mode: parallel` referencing shared mutable state (`input_number`, `counter`, `input_boolean`)
- R-09 [MEDIUM]: `choose:` without `default:` branch (silently does nothing when no condition matches)
- R-10 [HIGH]: `mode: queued` with `delay:` or `wait_*` blocks and `max:` ≤ 3 combined with ≥ 3 triggers — queue saturation risk (triggers dropped with WARNING log when queue full during delays; truly silent only if `max_exceeded: silent` is set); severity escalates if any trigger also violates R-02 (unfiltered `trigger: state` without `to:` multiplies trigger frequency)
- R-11 [HIGH]: `float(0)` or `int(0)` default on sensor values used in physical calculations (temperature, humidity, pressure) — 0 is physically wrong and produces silently incorrect results; use `float(none)` with an availability guard (`has_value()`) or a realistic fallback value
- R-12 [HIGH]: Self-trigger / feedback loop — automation triggers on an entity (e.g., `input_select`, `input_boolean`, `input_number`) that it also sets in its own actions; HA has NO built-in self-trigger protection; with `mode: queued` or `mode: parallel` this creates an infinite loop consuming queue slots; fix: remove the trigger, add a `condition` guard, or use `mode: single` as partial protection
- R-13 [MEDIUM]: Trigger without `id:` in `choose:`-based automations — makes `trigger.id` matching impossible; branches using `condition: trigger` require trigger IDs to function
- R-14 [MEDIUM]: Dead trigger — trigger has `id:` but that ID is never referenced in any `condition: trigger`, `choose:`, or template expression; likely copy-paste remnant or unfinished logic
- R-15 [MEDIUM]: Asymmetric error handling — same physical action (e.g., `cover.open_cover`, `climate.set_temperature`) appears in multiple branches but only some have retry/fallback logic; inconsistent reliability across code paths
- R-16 [HIGH]: Templated event name — `event_type:` does not evaluate templates in event triggers; the automation attaches to the literal string and silently misses the intended event. Use a fixed `event_type` and move dynamic logic into conditions or event data handling.

## Performance (Medium)

- P-01: `trigger: template` trigger that could be a `trigger: state` trigger (see `skills/ha-nova/template-guidelines.md` → Decision Tree)
- P-02: `homeassistant.update_entity` inside a `repeat:` loop without meaningful delay
- P-03: Polling loop (`repeat: while:` + short `delay:`) instead of `wait_for_trigger`
- P-04: Template trigger using `now()` for time-sensitive logic — re-evaluates only once per minute; for sub-minute precision use `time_pattern` trigger or a dedicated sensor
- P-05 [LOW]: `trigger: device` with `device_id` where a `trigger: state` with `entity_id` would work — `device_id` is not persistent across device re-adds; exception: Zigbee2MQTT autodiscovered device triggers and ZHA button/remote events (see `skills/ha-nova/best-practices.md` → Zigbee Button Patterns)

## Style (Low)

- M-01: Missing `alias:`
- M-02: Deprecated `service:` key instead of `action:`
- M-03: `entity_id:` under `data:` instead of `target: entity_id:`
- M-04: `trigger_variables` using `states()` (evaluated at attach time, will be stale)

## Script-Specific (apply ONLY when domain is `script`, skip for automations)

- F-01 [HIGH]: `fields:` entry without `selector:` (UI shows raw text box for all types)
- F-02 [HIGH]: `fields:` with `required: true` or `default:` but no `| default(...)` guard in `variables:` block — `required` and `default` are UI-only, not enforced at runtime
- F-03 [MEDIUM]: Template `{{ field_name }}` in sequence without corresponding `variables:` guard — fails silently when caller omits field
- F-04 [MEDIUM]: `mode: queued` or `mode: parallel` without explicit `max:` value
- F-05 [MEDIUM]: `mode: parallel` with actions writing to same entity (race condition)
- F-06 [MEDIUM]: `action: script.turn_on` (non-blocking) when next step depends on result — use blocking `action: script.{id}` instead
- F-07 [LOW]: Script contains `wait_for_trigger:` at top of sequence with no preceding logic — likely should be an automation
- F-08 [LOW]: Hardcoded values that vary per call-site should be `fields:` parameters (human-judgment check — flag only obvious cases like repeated entity_ids or magic numbers)

## Helper-Specific (apply when reviewing helpers or automations referencing helpers)

- H-01 [HIGH]: `input_number` without explicit `min`/`max` — HA defaults 0/100, likely wrong for physical quantities
- H-02 [MEDIUM]: `input_boolean`/`input_select` as condition guard without `homeassistant.started` initializer — state unknown after restart
- H-03 [MEDIUM]: `input_number` `mode: box` with wide range and no `step` — easy to mistype values
- H-04 [LOW]: `input_select` `initial` not set — defaults to first option, may not be intended
- H-05 [MEDIUM]: `counter` without `minimum`/`maximum` — unbounded growth risk
- H-06 [LOW]: `timer` without `duration` — must be set via service call before start
- H-07 [MEDIUM]: Orphaned helper — not referenced by any automation/script (check via `search/related`; for cleanup workflow see `skills/ha-nova/safe-refactoring.md` → Orphan Cleanup)
- H-08 [LOW]: Naming inconsistency — mixed patterns across helpers (e.g., `sleep_mode` vs `Sleep Mode` vs `sleepMode`)
- H-09 [MEDIUM → HIGH]: Threshold effectively weakened — `input_number` is used as a direct threshold and its current value sits at or near the boundary that makes the guard trivially easy to satisfy. Operator-aware: `>`/`>=` is risky near `min`; `<`/`<=` is risky near `max`. "Near" means within `1 × step`, including the exact boundary. Escalate to HIGH only with concrete loop evidence (`repeat:`, or R-10/R-12 also applies).
- H-10 [LOW]: Threshold value off the configured step grid — current `input_number` value does not land on the configured `step` lattice relative to `min`; likely set programmatically rather than through the UI. Supplementary signal for H-09, not a severity escalator by itself.

## Helper Threshold Evidence (for H-09/H-10)

- Apply only for direct threshold references, not broad heuristics:
  - `numeric_state` with helper-backed `above`/`below`
  - direct template comparisons where an explicit `input_number.<id>` appears in the compared expression
- Read live helper evidence via:
  ```bash
  ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/states/<helper_entity_id>"}' \
    | ~/.config/ha-nova/relay jq 'if .ok then .data.body else empty end'
  ```
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
