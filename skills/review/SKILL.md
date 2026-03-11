---
name: review
description: Use when analyzing, reviewing, auditing, or checking Home Assistant automations, scripts, or helpers for errors, best-practice violations, and conflicts. Do not invoke ha-nova:read separately — this skill handles discovery and reading internally.
---

# HA NOVA Review


## Scope

Read-only quality review for automations, scripts, and helpers:
- Config quality checks (safety, reliability, performance, style)
- Collision scan (other automations targeting same entities)
- Conflict analysis (real conflicts vs safe patterns)
- Quick-Fix: if an acute state problem is detected, offer a single corrective service call

Read-only analysis. Exception: after explicit user confirmation, one Quick-Fix service call may be executed to correct an acute state problem detected during review.

## Bootstrap

Relay CLI: `~/.config/ha-nova/relay`
- Preflight: `relay health` (once per session, skip if already verified)
- `relay ws -d '<json>'` — WebSocket API
- `relay core -d '<json>'` — REST API

### Target Resolution

If user provides an exact entity_id (e.g., `automation.kitchen_lights`), skip search and go directly to config read.

If the target config is not already in the thread context, resolve it yourself:
1. Search by name using entity registry (compact fields: `ei`=entity_id, `en`=name/alias):
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
     | jq -r '.data.entities[] | select(.ei | startswith("automation.")) | "\(.ei) | \(.en // "unnamed")"' \
     | grep -i '<search_term>'
   ```
   For scripts: `select(.ei | startswith("script."))`.
   For helpers: `select(.ei | test("^(input_boolean|input_number|input_text|input_select|input_datetime|input_button|counter|timer|schedule)\\."))`.
2. If multiple matches: present top candidates (max 5) and ask one clarifying question. Never guess.
3. Resolve `unique_id` (config key) — the entity_id slug and config key differ for UI-created items (see `relay-api.md` → ID Types):
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/get","entity_id":"automation.<slug>"}' \
     | jq -r '.data.unique_id'
   ```
   For scripts: use `"entity_id":"script.<slug>"`.
4. Read config via REST using the resolved `unique_id` (save to temp file — configs can be 10-30 KB, shell output truncates):
   ```bash
   # Automation:
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/automation/config/<unique_id>"}' \
     | jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-target.json
   # Script:
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/script/config/<unique_id>"}' \
     | jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-target.json
   # Helper (WS, not REST):
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/list"}' \
     | jq 'if .ok then [.data[] | select(.name | test("<search_term>";"i"))] else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-target.json
   ```
   Then read the file with the native file-reading tool for complete, untruncated access.
5. Read current state of the primary target entity (for Quick-Fix detection at end of review):
   ```bash
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/states/<entity_id>"}' \
     | jq 'if .ok then .data.body else empty end' > /tmp/ha-review-state.json
   ```
   If state read fails, continue review — Quick-Fix will be skipped.

If config is already in the thread context (e.g., user pasted YAML):
- If entity_id is known: skip Target Resolution entirely, go straight to Config Quality Review (Step 1).
- If entity_id is unknown: run Target Resolution search (above) to find entity_id. If not found, proceed with Config Quality Review only. Note in output: "Collision scan skipped — no entity_id available."

Do NOT invoke ha-nova:entity-discovery or ha-nova:read as separate skills — handle everything within this review flow.

## Flow

### Pre-Analysis Reference

Before analyzing, consult these sources:

**Local reference (always):**
- `docs/reference/ha-template-reference.md` — valid Jinja2 functions, constants, filters

**Official HA docs (fetch selectively based on config content — do NOT fetch all for every review):**
- Trigger issues → https://www.home-assistant.io/docs/automation/trigger/
- Mode issues → https://www.home-assistant.io/docs/automation/modes/
- Action/script issues → https://www.home-assistant.io/docs/scripts/
- Template issues → https://www.home-assistant.io/docs/configuration/templating/
- Schema questions → https://www.home-assistant.io/docs/automation/yaml/

Only fetch pages relevant to the triggers, actions, and templates found in the config. Cross-check against documented gotchas and constraints — this catches issues beyond the hardcoded checks below.

**Verify-before-flag rule:** Before reporting ANY issue:
1. Check local reference doc
2. If not found, check the official HA docs above
3. Only flag as error if confirmed invalid after both checks

Do NOT flag valid HA builtins or documented behavior as errors.

### Step 1: Config Quality Review

Analyze config against these checks AND any additional issues found in the official docs. Report only violations found.

**Safety (Critical):**
- S-01: Hardcoded secrets (tokens, passwords, API keys, long webhook IDs as literals)
- S-02: `entity_id: all` or domain-wide targets without explicit intent
- S-03: Webhook trigger with `local_only: false` (exposes webhook to internet)

**Reliability (High):**
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

**Performance (Medium):**
- P-01: `trigger: template` trigger that could be a `trigger: state` trigger (see `skills/ha-nova/template-guidelines.md` → Decision Tree)
- P-02: `homeassistant.update_entity` inside a `repeat:` loop without meaningful delay
- P-03: Polling loop (`repeat: while:` + short `delay:`) instead of `wait_for_trigger`
- P-04: Template trigger using `now()` for time-sensitive logic — re-evaluates only once per minute; for sub-minute precision use `time_pattern` trigger or a dedicated sensor

**Style (Low):**
- M-01: Missing `alias:`
- M-02: Deprecated `service:` key instead of `action:`
- M-03: `entity_id:` under `data:` instead of `target: entity_id:`
- M-04: `trigger_variables` using `states()` (evaluated at attach time, will be stale)

**Script-Specific (apply ONLY when domain is `script`, skip for automations):**
- F-01 [HIGH]: `fields:` entry without `selector:` (UI shows raw text box for all types)
- F-02 [HIGH]: `fields:` with `required: true` or `default:` but no `| default(...)` guard in `variables:` block — `required` and `default` are UI-only, not enforced at runtime
- F-03 [MEDIUM]: Template `{{ field_name }}` in sequence without corresponding `variables:` guard — fails silently when caller omits field
- F-04 [MEDIUM]: `mode: queued` or `mode: parallel` without explicit `max:` value
- F-05 [MEDIUM]: `mode: parallel` with actions writing to same entity (race condition)
- F-06 [MEDIUM]: `action: script.turn_on` (non-blocking) when next step depends on result — use blocking `action: script.{id}` instead
- F-07 [LOW]: Script contains `wait_for_trigger:` at top of sequence with no preceding logic — likely should be an automation
- F-08 [LOW]: Hardcoded values that vary per call-site should be `fields:` parameters (human-judgment check — flag only obvious cases like repeated entity_ids or magic numbers)

**Helper-Specific (apply when reviewing helpers or automations referencing helpers):**
- H-01 [HIGH]: `input_number` without explicit `min`/`max` — HA defaults 0/100, likely wrong for physical quantities
- H-02 [MEDIUM]: `input_boolean`/`input_select` as condition guard without `homeassistant.started` initializer — state unknown after restart
- H-03 [MEDIUM]: `input_number` `mode: box` with wide range and no `step` — easy to mistype values
- H-04 [LOW]: `input_select` `initial` not set — defaults to first option, may not be intended
- H-05 [MEDIUM]: `counter` without `minimum`/`maximum` — unbounded growth risk
- H-06 [LOW]: `timer` without `duration` — must be set via service call before start
- H-07 [MEDIUM]: Orphaned helper — not referenced by any automation/script (check via `search/related`; for cleanup workflow see `skills/ha-nova/safe-refactoring.md` → Orphan Cleanup)
- H-08 [LOW]: Naming inconsistency — mixed patterns across helpers (e.g., `sleep_mode` vs `Sleep Mode` vs `sleepMode`)

### Step 2: Collision Scan

Find other automations/scripts that control the same entities.

1. Extract all target entity_ids from config actions (the entities being controlled).
2. For the top 3 most significant target entities, run `search/related`:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"search/related","item_type":"entity","item_id":"{entity_id}"}'
   ```
3. Collect related automations/scripts (exclude current target).
4. Read configs of related items (max 5). Resolve `unique_id` first (see Target Resolution step 3), then:
   ```bash
   # Automation:
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/automation/config/<unique_id>"}' \
     | jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-related-N.json
   # Script:
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/script/config/<unique_id>"}' \
     | jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-related-N.json
   ```
5. If no related items found, report "no conflicts" in the Conflicts section and skip Step 3.

### Trace Analysis (on request)

When the user reports runtime issues ("automation didn't fire", "wrong behavior last night"):
1. Follow the trace procedure in `skills/read/SKILL.md` → Trace Debugging
2. Cross-reference trace findings with config quality findings from Step 1
3. Verify `item_id` in every trace matches the target's `unique_id` before attributing results. see `skills/ha-nova/SKILL.md` → Claim-Evidence Binding.
4. Include trace-based findings in the Findings section with a descriptive title (e.g., `🔴 Bedingung blockiert — Condition wurde in den letzten 3 Runs nie erfüllt`)

### Step 3: Conflict Analysis

For each related automation/script, apply the 3-step conflict test:

**Step 3a — Action Polarity:**
- Same action on same entity → not a conflict (possibly redundant, note only)
- Opposite actions (on/off, open/close, different values) → proceed to 3b

**Step 3b — Trigger Temporal Relationship:**
- Mutually exclusive triggers (sunrise/sunset, non-overlapping time windows) → **no conflict, skip**
- Sequential triggers (event start → event end/timeout) → **complementary pair, skip**
- Concurrent triggers (can fire in same time window) → proceed to 3c

**Step 3c — Guard Conditions:**
- Mutually exclusive conditions (e.g., `sleep_mode: on` vs `off`) → **no conflict, skip**
- No mutual exclusion → **real conflict risk, report**

### Known Safe Patterns (do NOT warn)

- Motion on → light on + No motion (with `for:`) → light off = complementary pair
- Goodnight routine → all off + Motion → specific light on = intentional override
- Sunrise → open + Sunset → close = mutually exclusive time windows
- Automation A and B target same entity but with non-overlapping value ranges (e.g., brightness 0-50 vs 51-100)

### Known Problem Patterns (DO warn)

- **Flip-Flop:** Automation A turns entity on (schedule/event), Automation B turns it off (timer/no-motion), triggers can overlap with no guard → entity bounces
- **Cascade:** Automation A changes entity X, entity X is template dependency, Automation B triggers on X → unintended chain reaction
- **Race Condition:** Two automations with `delay:` targeting same entity, both can fire before other's delay expires
- **Stale Helper:** `input_boolean` used as condition guard, no `homeassistant.started` initializer → wrong state after restart
- **Startup Flash:** Template sensor trigger without `unknown`/`unavailable` from_state guard → fires on HA restart
- **Self-Trigger Loop:** Automation triggers on entity X and sets entity X in actions → re-triggers itself; with `mode: queued`/`parallel` this creates infinite loop consuming queue slots until `max:` is hit (see also R-12)

### Step 4: Quick-Fix Detection

After completing Steps 1-3, check if the current entity state (from `/tmp/ha-review-state.json`) shows an acute, fixable problem.

**Qualifies as Quick-Fix:**
- Entity state contradicts automation intent under current conditions (e.g., light `on` when automation should have turned it `off`, climate mode wrong)
- Entity is in error/degraded state that a service call can reset (e.g., `unavailable` cover that needs `cover.stop_cover`)
- Helper value is desynchronized from what automation logic expects (e.g., `input_select` stuck on wrong option)

**Does NOT qualify:**
- State is simply "not what user wants" without clear automation-intent evidence — that's a service-call request, not a review finding
- Fix requires config change (that's a Suggestions item)
- Multiple equally valid corrections exist (ambiguous — note in Suggestions section instead)
- State read failed or entity unavailable — skip, note in Instant Help section: localized "skipped (state unavailable)"

**If qualified:**
1. Show current state vs expected state
2. Show exact service call that would fix it
3. Ask for natural confirmation (same tier as `ha-nova:service-call` — no token needed, service calls are reversible)

**On confirmation:**
Execute via Relay:
```bash
~/.config/ha-nova/relay core -d '{"method":"POST","path":"/api/services/{domain}/{service}","body":{"entity_id":"{entity_id}",{...service_data}}}'
```
Then verify state changed:
```bash
~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/states/{entity_id}"}'
```
Report result (new state or failure).

## Output Format

Return exactly these 7 sections, in this order, every time. Localize all headings to the user's language (see `skills/ha-nova/SKILL.md` → Output Localization).

**Section 1 — Review target:**
- domain (automation / script / helper) and target entity_id

**Section 2 — Findings:**
- numbered list or "no issues found"
- each: `🔴|🟠|🟡 Descriptive title — explanation + fix suggestion`
- 🔴 = high/critical, 🟠 = medium, 🟡 = low/info
- title must describe WHAT the issue is (2-5 words), NOT an internal code

**Section 3 — Collision check:**
- list the checked entity names
- short result: how many related automations/scripts found

**Section 4 — Conflicts:**
- numbered conflicts or "none"
- each: entity_id, what this automation does vs what the other does, risk description
- 🔴 = real conflict, 🟠 = potential, 🟡 = info (safe pattern)

**Section 5 — Suggestions:**
- concrete improvement ideas
- each: short title + what it does + why it helps
- or "none"

**Section 6 — Summary:**
- one-paragraph natural language summary
- mention total findings count and highest severity emoji
- if clean: localized equivalent of "Config looks clean — no issues detected."

**Section 7 — Instant help:**
- if no acute state problem: localized "not needed"
- if state read failed: localized "skipped (state unavailable)"
- if fixable problem detected: current state, expected state, proposed service call, confirmation prompt

## Guardrails

- Read-only analysis (exception: Quick-Fix service call after user confirmation)
- Quick-Fix: max 1 service call per review, only after explicit user confirmation, only simple state corrections (no config mutations)
- Only communicate with HA through `~/.config/ha-nova/relay`
- Never guess entity IDs
- Limit collision scan to top 3 target entities, max 5 related configs
- Batch reviews: max 3 automations/scripts per request. If user asks for more, review first 3 and offer to continue.
