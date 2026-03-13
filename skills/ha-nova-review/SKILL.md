---
name: ha-nova-review
description: Use when analyzing, reviewing, auditing, or checking Home Assistant automations, scripts, or helpers for errors, best-practice violations, and conflicts. Do not invoke `ha-nova:ha-nova-read` separately — this skill handles discovery and reading internally.
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
     | ~/.config/ha-nova/relay jq -r '.data.entities[] | select(.ei | startswith("automation.")) | "\(.ei) | \(.en // "unnamed")"' \
     | grep -i '<search_term>'
   ```
   For scripts: `select(.ei | startswith("script."))`.
   For helpers: `select(.ei | test("^(input_boolean|input_number|input_text|input_select|input_datetime|input_button|counter|timer|schedule)\\."))`.
2. If multiple matches: present top candidates (max 5) and ask one clarifying question. Never guess.
3. Resolve `unique_id` (config key) — the entity_id slug and config key differ for UI-created items (see `relay-api.md` → ID Types):
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/get","entity_id":"automation.<slug>"}' \
     | ~/.config/ha-nova/relay jq -r '.data.unique_id'
   ```
   For scripts: use `"entity_id":"script.<slug>"`.
4. Read config via REST using the resolved `unique_id` (save to temp file — configs can be 10-30 KB, shell output truncates):
   ```bash
   # Automation:
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/automation/config/<unique_id>"}' \
     | ~/.config/ha-nova/relay jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-target.json
   # Script:
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/script/config/<unique_id>"}' \
     | ~/.config/ha-nova/relay jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-target.json
   # Helper (WS, not REST):
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/list"}' \
     | ~/.config/ha-nova/relay jq 'if .ok then [.data[] | select(.name | test("<search_term>";"i"))] else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-target.json
   ```
   Then read the file with the native file-reading tool for complete, untruncated access.
5. After reading the config, extract the **primary controlled entity** from the config actions (the first significant entity_id being controlled, e.g., `light.kitchen`, `climate.living_room` — NOT the automation/script entity itself). Read its current state (for Quick-Fix detection at end of review):
   ```bash
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/states/<controlled_entity_id>"}' \
     | ~/.config/ha-nova/relay jq 'if .ok then .data.body else empty end' > /tmp/ha-review-state.json
   ```
   If no controlled entity found in actions, or state read fails: continue review — Quick-Fix will be skipped.

If config is already in the thread context (e.g., user pasted YAML):
- If entity_id is known: skip Target Resolution entirely, go straight to Config Quality Review (Step 1). But still read the primary controlled entity's state (step 5 above) for Quick-Fix detection — this step is independent of Target Resolution.
- If entity_id is unknown: run Target Resolution search (above) to find entity_id. If not found, proceed with Config Quality Review only. Note in output: "Collision scan skipped — no entity_id available."

Do NOT invoke `ha-nova:ha-nova-entity-discovery` or `ha-nova:ha-nova-read` as separate skills — handle everything within this review flow.

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

Analyze config against the review catalog plus any additional issues found in the official docs. Report only violations found.

**Rule Catalog**
- Load `skills/ha-nova-review/checks.md` before evaluating findings.
- `skills/ha-nova-review/SKILL.md` is the stable review entrypoint; `skills/ha-nova-review/checks.md` is the full rule catalog.

**Check Taxonomy (internal only):**
- Format: `{CATEGORY}-{NN}` (example: `H-09`)
- Category letter = family
- Severity is separate from the code
- Codes are internal only; never show them in user-facing output

**Apply these families by domain:**
- Automation: S-01..S-03, R-01..R-16, P-01..P-05, M-01..M-04
- Script: automation families plus F-01..F-08
- Helper: H-01..H-10 only
- If an automation or script references helpers in actions or direct thresholds, also apply H-01..H-10 to those helpers

**Live helper evidence for H-09/H-10:**
- See `skills/ha-nova-review/checks.md` → Helper Threshold Evidence
- Read `/api/states/<helper_entity_id>` only when the threshold reference is direct
- If `state`, `attributes.min`, `attributes.max`, or `attributes.step` are missing or non-numeric, skip H-09/H-10
- Do not emit unrelated findings just because an H-09/H-10 signal matched

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
     | ~/.config/ha-nova/relay jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-related-N.json
   # Script:
   ~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/script/config/<unique_id>"}' \
     | ~/.config/ha-nova/relay jq 'if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end' > /tmp/ha-review-related-N.json
   ```
5. If no related items found, report "no conflicts" in the Conflicts section and skip Step 3.

### Trace Analysis (on request)

When the user reports runtime issues ("automation didn't fire", "wrong behavior last night"):
1. Follow the trace procedure in `skills/ha-nova-read/SKILL.md` → Trace Debugging
2. Cross-reference trace findings with config quality findings from Step 1
3. Verify `item_id` in every trace matches the target's `unique_id` before attributing results. see `skills/ha-nova/SKILL.md` → Claim-Evidence Binding.
4. Include trace-based findings in the Findings section with a descriptive title (e.g., `🔴 Condition blocked — condition was never met in last 3 runs`). Localize at runtime per `skills/ha-nova/SKILL.md` → Output Localization.

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

Use the known safe/problem patterns from `skills/ha-nova-review/checks.md` when deciding whether a related automation pair is truly benign or a real conflict.

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
3. Ask for natural confirmation (same tier as `ha-nova:ha-nova-service-call` — no token needed, service calls are reversible)

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
