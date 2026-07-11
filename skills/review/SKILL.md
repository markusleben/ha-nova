---
name: review
description: Use when analyzing, reviewing, auditing, or checking Home Assistant automations, scripts, or helpers for errors, best-practice violations, and conflicts. Do not invoke `ha-nova:read` separately — this skill handles discovery and reading internally.
---

# HA NOVA Review


## Scope

Read-only quality review for automations, scripts, and helpers:
- Config quality checks (safety, reliability, performance, style)
- Collision scan (other automations targeting same entities)
- Conflict analysis (real conflicts vs safe patterns)
- Explorative questions for complex automation/script behavior
- Suggestion synthesis that separates open questions from confident recommendations
- Quick-Fix: if an acute state problem is detected, offer a single corrective service call
- Bulk review: aggregate the same checks across a deterministic multi-target workset

Read-only analysis. Exception: after explicit user confirmation, one Quick-Fix service call may be executed to correct an acute state problem detected during review.
- No `POST`, `PUT`, `PATCH`, or `DELETE` config writes through the relay.
- If the user wants to change an automation or script, hand off to `ha-nova:write`.
- If the user wants to change a helper, hand off to `ha-nova:helper`.
- The Quick-Fix service call in Step 4 is the only write exception in this skill.
- Bulk review is stricter: no Quick-Fix, no service calls, no write exception.

## Bootstrap (once per session)

Preflight: `ha-nova relay health` (once per session, skip if already verified). If this fails: `ha-nova setup`.

## Relay Contract

Relay CLI: `ha-nova relay`
- `ha-nova relay ws --data-file <payload-file>` — canonical WebSocket path
- `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` — canonical REST path
- `ha-nova relay ... --jq-file <filter-file>` — canonical complex filter path
- `ha-nova relay ... --out <result-file>` — canonical large-output path

## Target Resolution

If user provides an exact automation/script `entity_id` (e.g., `automation.main_lights`), skip search and go directly to config read.

For helpers, resolve the family first:
- storage-based family: entity_id domain is one of `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`, `input_button`, `counter`, `timer`, `schedule`
- supported config-entry family: domain is one of `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`
- config-entry helper review remains minimal, but target resolution must still normalize to a real `entry_id`

If the target config is not already in the thread context, resolve it yourself:
1. If the user asks for a bulk audit by `prefix`, `domain`, `area`, or `label`, build a shortlist first using `skills/ha-nova/bulk-patterns.md`.
   - use `config/entity_registry/list_for_display` for direct `prefix` / `domain` resolution
   - escalate to `config/entity_registry/list` and `config/area_registry/list` only when richer area/label evidence is required
   - for room/area phrasing, resolve the area and use `search/related` with `item_type:"area"` before any registry-first fallback
   - treat area-related results as a keyed object (`automation`, `script`, `entity`, `device`, ...), not as a flat array
   - use the canonical area projection by target family:
     - automation review -> `.data.automation`
     - script review -> `.data.script`
     - helper-in-area is not a first-class bulk selector contract
     - `.data.entity` is only a fallback seed for automation/script derivation when the target-family arrays are absent
   - use direct registry `area_id` only as supplemental evidence when it is actually populated
   - dedupe the shortlist on canonical `entity_id`, then sort deterministically and persist the matched shortlist
   - derive the current review set before any per-item reads:
     - exact single target -> current review set = that one target
     - more than one resolved target -> current review set = the resolved targets in deterministic order, trimmed to the first 5 only when more than 5 targets match
   - carry the exact matched count plus current review-set size into Bulk Mode Gate
   - do not resolve unique_ids, read configs, read states, or run collision scans for matched-but-non-audited remainder targets outside the current review set
   - a narrow collision explanation may inspect one extra target outside the matched remainder when needed to classify a cluster; keep that read explicit, read-only, outside the audited-item count, and clearly marked as related/collision evidence in the transcript
   - prefer explicit command/file naming such as `related-config`; if the marker must live in command transcript output, emit a line like `COLLISION_EVIDENCE=<reason>`
   - never build a full matched-set config cache or evidence snapshot; if caching helps, cache only the current review set
   - once the shortlist or workset is saved, keep that file immutable; use dedicated filenames for later registry, config, and collision outputs
   - if the user intentionally requested a bulk selector, do not ask a clarifying question just because multiple matches remain
2. Search by name using entity registry (compact fields: `ei`=entity_id, `en`=name/alias):
   Skip this step when step 1 already produced a resolved current review set.
   Create `<payload-file>` with `{"type":"config/entity_registry/list_for_display"}`.
   Then run:
   ```text
   ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
   ```
   Write `<filter-file>` with:
   ```jq
   .data.entities[] | select(.ei | startswith("automation.")) | "\(.ei) | \(.en // "unnamed")"
   ```
   Filter the resulting text with the client's native search/filter tool, not shell-specific pipelines.
   For scripts: `select(.ei | startswith("script."))`.
   For helpers: `(.ei | split(".")[0]) as $domain | select(["input_boolean","input_number","input_text","input_select","input_datetime","input_button","counter","timer","schedule"] | index($domain))`.
3. If the helper might be from the config-entry family, also read:
   ```text
   ha-nova relay ws --data-file <payload-file> --out <entries-file>
   ha-nova relay ws --data-file <payload-file> --out <registry-file>
   ```
   with:
   - `{"type":"config_entries/get"}`
   - `{"type":"config/entity_registry/list"}`
   Resolve the target by one of:
   - exact `entry_id`
   - config-entry `title` within the supported helper domains
   - linked `entity_id` by matching entity-registry `config_entry_id`
   Build the canonical metadata item:
   - `entry_id`
   - `domain`
   - `title`
   - `state`
   - `linked_entities[]`
4. If multiple matches remain outside an intentional bulk-selector flow: present top candidates (max 5) and ask one clarifying question. Never guess.
5. For automation/script targets, resolve `unique_id` (config key) — the entity_id slug and config key differ for UI-created items (see `relay-api.md` → ID Types):
   Create `<payload-file>` with the final `config/entity_registry/get` request in one write step, then run:
   ```text
   ha-nova relay ws --data-file <payload-file> --out <registry-file>
   ha-nova relay jq -r --file <registry-file> '.data.unique_id'
   ```
   The jq filter quoting above is a POSIX example. On Windows/PowerShell pass the same filter with native argument quoting.
   Use that saved-result + `-r` form for the scalar. Do not create a separate jq file for `.data.unique_id`, do not strip quotes with shell substitutions afterward, and do not write placeholder payload templates that are rewritten later with `perl -0pi`, `sed -i`, or similar commands.
   For scripts: use `"entity_id":"script.<slug>"`.
   Skip this step for config-entry helpers — `entry_id` is already the canonical identity.
6. Read the target:
   ```text
   # Automation:
   ha-nova relay core --method GET --path /api/config/automation/config/<unique_id> --jq-file <config-filter-file> --out <target-file>
   # Script:
   ha-nova relay core --method GET --path /api/config/script/config/<unique_id> --jq-file <config-filter-file> --out <target-file>
   # Helper (storage-based family, WS list + filter):
   ha-nova relay ws --data-file <payload-file> --jq-file <helper-filter-file> --out <target-file>
   ```
   Preferred: copy `skills/ha-nova/config-body-filter.jq` to `<config-filter-file>` and use that copied file directly.
   If the canonical file is unavailable (flat-copy installs), recreate it with exactly:
   ```jq
   if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
   ```
   When writing this jq file, paste that jq program body exactly as shown. Do not add extra shell-escape backslashes around the jq interpolation or run a probe variant first.
   POSIX shell example only:
   ```sh
   cat <<'EOF' > "$config_filter_file"
   if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
   EOF
   ```
   On Windows/PowerShell, use the native file-writing equivalent or copy the canonical file while preserving the exact jq file body. The jq file body must contain exactly that one jq expression and nothing else. Do not add extra single quotes inside the error string. If you print the file for confirmation, do not compare it against a shell-escaped string, do not store the jq program in a shell variable, and do not wrap it in an `if [ "$line" != ... ]` guard. If the printed contents differ, overwrite the same `config_filter_file` with the exact canonical line before the first config read; do not create probe variants or alternate filenames, and do not patch the file afterward with in-place rewrite commands.
   Write `<helper-filter-file>` with:
   ```jq
   if .ok then [.data[] | select(.name | test("<search_term>";"i"))] else error("relay error: \(.error.message // "unknown")") end
   ```
   For config-entry helpers, persist the canonical metadata item from step 3 to `<target-file>` instead of attempting `{type}/list`.
   Then read the file with the native file-reading tool for complete, untruncated access.
7. After reading the config for an automation or script, extract the **primary controlled entity** from the config actions (the first significant entity_id being controlled, e.g., `light.main_light`, `climate.main_zone` — NOT the automation/script entity itself). Read its current state (for Quick-Fix detection at end of review):
   ```text
   ha-nova relay core --method GET --path /api/states/<controlled_entity_id> --jq-file <state-filter-file> --out <state-file>
   ```
   Write `<state-filter-file>` with:
   ```jq
   if .ok then .data.body else empty end
   ```
   Skip this step when the current review set contains more than one target.
   If no controlled entity found in actions, or state read fails: continue review — Quick-Fix will be skipped.
   For standalone config-entry helper review, skip this step entirely. There is no config body with actions to analyze for a primary controlled entity.

If config is already in the thread context (e.g., user pasted YAML):
- If entity_id is known for an automation or script: skip Target Resolution entirely, go straight to Config Quality Review (Step 1). But still read the primary controlled entity's state (step 7 above) for Quick-Fix detection when the current review set contains exactly one target — this step is independent of Target Resolution.
- If the target already in context is a config-entry helper metadata item: skip Target Resolution entirely and go straight to the config-entry helper review lane in Step 1. Do not attempt primary-controlled-entity state reads or Quick-Fix detection from that path.
- If entity_id is unknown: run Target Resolution search (above) to find entity_id. If not found, proceed with Config Quality Review only. Note in output: "Collision scan skipped — no entity_id available."

Do NOT invoke `ha-nova:entity-discovery` or `ha-nova:read` as separate skills — handle everything within this review flow.

## Bulk Mode Gate

After target resolution:

- resolved targets `== 1`: stay in normal single-target review mode
- resolved targets `> 1`: enter aggregate multi-target review mode automatically

Multi-target rules:
- multi-target review starts only after the current review set is trimmed
- Quick-Fix is single-target only; skip Step 4 and its prerequisite state-read step whenever the current review set contains more than one target

Bulk mode rules:
- use `skills/ha-nova/bulk-patterns.md` for selector semantics, stable ordering, and workset limits
- audit only the current workset (max 5 targets)
- stop after that one workset; do not start a second batch in the same standalone request
- resolve `unique_id`, config, state, and related-item evidence per target inside the current workset only; no prefetch for the remaining matched targets
- run the same Step 1 / Step 2 / Step 3 checks per item
- dedupe official-doc verification by pattern across the workset; do not refetch the same doc page per item
- cap related-config deep reads across the whole workset, not per item
- aggregate findings by repeated pattern, but preserve affected item lists
- skip Steps 4-6 entirely; bulk mode does not offer Quick-Fix, exploratory questions, or single-target suggestion synthesis
- persist collision candidate sets to files when needed; do not embed JSON arrays in shell variables for later command generation
- when multiple Relay probes are needed inside one temp directory, keep shared temp files serial or use dedicated payload filenames per probe
- write each Relay payload file as the final JSON body for that request; do not create placeholder templates and patch them later
- if more targets remain after the current workset, report `matched N / audited M / remaining R` and wait for an explicit follow-up request before continuing

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

Traverse all `variables:` mappings in the config, not just the top-level block. Include root `variables:` on the automation/script plus local `variables:` actions inside `choose`, `if` / `then` / `else`, `default`, `repeat`, and nested `sequence` blocks.

**Rule Catalog**
- Load `skills/review/checks.md` before evaluating findings.
- `skills/review/SKILL.md` is the stable review entrypoint; `skills/review/checks.md` is the full rule catalog.

**Check Taxonomy (internal only):**
- Format: `{CATEGORY}-{NN}` (example: `H-09`)
- Category letter = family
- Severity is separate from the code
- Codes are internal only; NEVER show them in any user-facing message (reports, chat replies, follow-up questions) — describe findings in plain language instead

**Apply these families by domain:**
- Automation: S-01..S-03, R-01..R-28, P-01..P-05, M-01..M-05
- Script: automation families plus F-01..F-08
- Helper (storage-based family): H-01..H-10
- Helper (config-entry family): minimal config-entry review
  - do not apply H-01..H-10
  - confirm config-entry metadata is present
  - inspect linked entities when available
  - in Step 2, derive collision candidates from `linked_entities[]`, not from config actions
  - run `search/related` on up to 3 linked entities
  - say explicitly that config-entry helper review does not use the storage-helper H rules
  - for the `template` domain, also read the linked entity's rendered state (`unavailable`/`unknown` is inconclusive, not proof of breakage — source state or an intentional sentinel); to apply the template-level reliability checks, open the options flow for the entry first (non-persisting readback — the canonical metadata item does not carry the `state` template)
- If an automation or script references helpers in actions or direct thresholds, also apply H-01..H-10 to those helpers
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

### Step 2: Collision Scan

Find other automations/scripts that control the same entities.

For updates, this same scan is also run pre-write as an advisory impact preview (see `ha-nova:write` Phase 2 Step 3c, Pre-Write Impact, and the `ha-nova:helper` update flows). The post-write run here stays mandatory and authoritative against the persisted config.

Branch by target family:
- automation/script/storage-based helper: use action-derived target entities as before
- config-entry helper metadata item: use `linked_entities[]` from the canonical metadata item; do not attempt action extraction

1. Build the candidate entity list:
   - automation/script: extract all target entity_ids from config actions (the entities being controlled)
   - helper (storage-based family): use the helper entity_id
   - helper (config-entry family): use up to 3 `linked_entities[]` from the canonical metadata item
2. For the top 3 most significant candidate entities, run `search/related`:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```
3. Collect related automations/scripts (exclude current target).
4. Read configs of related items (max 5 for a single standalone target; keep a tighter shared budget across a bulk workset; post-write scans inside `write`/`helper` deliberately use a tighter budget of max 3 related configs). Resolve `unique_id` first for automation/script targets (see Target Resolution step 5), then:
   ```text
   # Automation:
   ha-nova relay core --method GET --path /api/config/automation/config/<unique_id> --jq-file <config-filter-file> --out <related-file>
   # Script:
   ha-nova relay core --method GET --path /api/config/script/config/<unique_id> --jq-file <config-filter-file> --out <related-file>
   ```
5. If no related items found, report "no conflicts" in the Conflicts section and skip Step 3.

### Trace Analysis (on request)

When the user reports runtime issues ("automation didn't fire", "wrong behavior last night"):
1. Follow the trace procedure in `skills/read/SKILL.md` → Trace Debugging
2. Prefer the normalized CLI helper fields from `ha-nova trace latest/list/get --json`; they are enough for run selection, result status, timestamp, item binding, and most review findings.
3. Inspect raw trace internals only when step-level evidence is required. Raw trace nodes can be arrays of event records; type-check before reading `path`, `result`, `changed_variables`, or `error`, and avoid large jq projections as the standard path.
4. Cross-reference trace findings with config quality findings from Step 1
5. Verify `item_id` in every trace matches the target's `unique_id` before attributing results. see `skills/ha-nova/SKILL.md` → Claim-Evidence Binding.
6. Include trace-based findings in the Findings section with a descriptive title (e.g., `🔴 Condition blocked — condition was never met in last 3 runs`). Localize at runtime per `skills/ha-nova/output-rules.md`.

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

Use the known safe/problem patterns from `skills/review/checks.md` when deciding whether a related automation pair is truly benign or a real conflict.

### Step 4: Quick-Fix Detection (single-target only)

Skip this step entirely in bulk mode.

After completing Steps 1-3, check if the current entity state (from the earlier `<state-file>` read) shows an acute, fixable problem.

**Qualifies as Quick-Fix:**
- Entity state contradicts automation intent under current conditions (e.g., light `on` when automation should have turned it `off`, climate mode wrong)
- Entity is in error/degraded state that a service call can reset (e.g., `unavailable` cover that needs `cover.stop_cover`)
- Helper value is desynchronized from what automation logic expects (e.g., `input_select` stuck on wrong option)

**Does NOT qualify:**
- State is simply "not what user wants" without clear automation-intent evidence — that's a service-call request, not a review finding
- Fix requires config change (that's a Suggestions item)
- Multiple equally valid corrections exist (ambiguous — note in Questions to consider instead)
- State read failed or entity unavailable — skip, note in Instant Help section: localized equivalent of "Skipped: current state unavailable."

**If qualified:**
1. Show current state vs expected state
2. Show exact service call that would fix it
3. Ask for natural confirmation bound to this exact service-call preview (same tier as `ha-nova:service-call`; see context skill → Active Preview Confirmation; no token needed for ordinary service calls)

**On confirmation:**
Execute via Relay:
```text
ha-nova relay core --method POST --path /api/services/{domain}/{service} --body-file <payload-file>
```
Then verify state changed:
```text
ha-nova relay core --method GET --path /api/states/{entity_id}
```
Report result (new state or failure).

### Step 5: Explorative Questions (standalone automation/script only)

Skip this step in bulk mode and for helper reviews.

Run this step only when at least one complexity gate matches:
- state-machine pattern: explicit state tracking plus threshold comparisons on the same signal across different branches
- loop-capable control flow: `repeat:` present, or `choose:` with 3+ branches
- periodic trigger (`time_pattern`) combined with actuator control such as `switch`, `light`, `cover`, `climate`, or `valve`

Ask only non-binding "what happens if...?" questions that go beyond the fixed checklist:
- boundary values and exact equality
- cycles and anti-cycling gaps
- sustained conditions that stay true for hours
- time extremes such as HA restart mid-cycle
- branch interplay and helper/variable overwrites
- failure and recovery when one step in a sequence fails
- `queued` / `parallel` behavior under rapid repeat triggers

Rules:
- return max 3 prompts
- no severity emojis
- no internal check codes
- do not restate an existing formal finding unless there is remaining uncertainty that the finding alone does not cover
- if no complexity gate matches, keep the Questions to consider section and mark it as the localized equivalent of "No follow-up questions right now."

### Step 6: Suggestion Synthesis (standalone single-target only)

Skip this step in bulk mode.

Derive candidate suggestions only from verified evidence already present in the current review context:
- matched findings from Step 1
- verified conflicts from Step 3
- current config structure
- already loaded related-config evidence
- alias/description text, pasted comments, or explicit history already present in the thread

Do not invent intent or removal justification from appearance alone.

**Design-intent gate for remove/simplify ideas:**
- treat existing logic as deliberate until proven otherwise
- removal/simplification is allowed only when the current context justifies the purpose of that logic and shows it is obsolete, contradictory, or unnecessarily complex
- if the purpose cannot be established, downgrade the idea into Questions to consider instead of presenting it as a confident suggestion

After the gate, rank confident suggestions by intervention depth:
1. Fix existing
2. Simplify existing
3. Extend existing
4. Add new

Rules:
- dedupe overlapping ideas
- cap the Suggestions section at 4 items
- when the same verified problem supports both a smaller direct fix and a larger optional monitoring/fallback addition, include both as confident suggestions and keep the smaller fix first
- do not place a new watchdog, monitor, or helper above a smaller root-cause fix
- do not suggest large refactors when a smaller change can eliminate the current verified problem
- if no confident recommendation remains after gating, output the localized equivalent of "No confident suggestions."

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Exception: if a maintainer-provided release-validation or machine-check prompt explicitly pins exact section titles or machine markers, follow that override exactly so automated validation can compare the fixed headings. This exception does not allow internal check codes in normal user-facing prose.

### Standard mode

For resolved targets `== 1`, keep this 8-section output in the same order every time:

**Section 1 — Review target:**
- domain (automation / script / helper) and target entity_id

**Section 2 — Findings:**
- numbered list or "no issues found"
- each finding uses:
  - `🔴|🟠|🟡 Short descriptive title`
  - `Why: ...`
  - `Fix: ...`
- 🔴 = high/critical, 🟠 = medium, 🟡 = low/info
- title must describe WHAT the issue is in one short phrase, NOT an internal code
- if clean: localized equivalent of "No issues found in this review."

**Section 3 — Collision check:**
- list the checked entity names
- short result: how many related automations/scripts found
- if no related items were found: localized equivalent of "No related items found."

**Section 4 — Conflicts:**
- numbered conflicts or localized equivalent of "No conflicts found."
- each: entity_id, what this automation does vs what the other does, risk description
- 🔴 = real conflict, 🟠 = potential, 🟡 = info (safe pattern)

**Section 5 — Questions to consider:**
- non-binding questions and edge-case prompts only
- use this section for exploratory prompts and intent-uncertain remove/simplify follow-ups
- no severity emojis
- or localized equivalent of "No follow-up questions right now."

**Section 6 — Suggestions:**
- confident improvement ideas only
- each: short title + what it does + why it helps
- rank by intervention depth: Fix existing → Simplify existing → Extend existing → Add new
- do not place intent-uncertain removals/simplifications here
- or localized equivalent of "No confident suggestions."

**Section 7 — Summary:**
- one-paragraph natural language summary
- mention total findings count and highest severity emoji
- if clean: localized equivalent of "No issues found in this review."

**Section 8 — Instant help:**
- if no acute state problem: localized equivalent of "No quick fix suggested."
- if state read failed: localized equivalent of "Skipped: current state unavailable."
- if fixable problem detected: current state, expected state, proposed service call, confirmation prompt

### Aggregate multi-target mode

For resolved targets `> 1`, return exactly these 6 sections:

**Section 1 — Scope:**
- filter used
- matched count
- audited count
- remaining count when applicable

**Section 2 — Summary:**
- one-paragraph aggregate summary
- count findings by highest severity
- say how many items were clean

**Section 3 — High-Risk Findings:**
- deduped 🔴 findings grouped by pattern
- each finding lists the affected targets

**Section 4 — Repeated Patterns:**
- recurring 🟠 / 🟡 findings grouped by pattern
- each pattern lists the affected targets

**Section 5 — Items Checked:**
- compact per-item table: target, overall status, top finding or clean result

**Section 6 — Collisions by Cluster:**
- group conflicts by shared controlled entity or linked helper
- separate true conflicts from safe/redundant clusters
- if clean: localized "no conflicts"

## Safety

- Read-only analysis: no config writes through the relay (see Scope for the single Quick-Fix exception).
- The Quick-Fix service call requires confirmation bound to its exact preview; bulk mode disables it entirely.
- Never guess entity or config IDs; resolve or ask.

## Guardrails

- Read-only analysis (exception: Quick-Fix service call after user confirmation)
- Quick-Fix: max 1 service call per review, only after explicit user confirmation, only simple state corrections (no config mutations)
- Any multi-target review: no Quick-Fix, no service calls, no write exception
- Only communicate with HA through `ha-nova relay`
- Never guess entity IDs
- Limit collision scan to top 3 target entities, max 5 related configs for a single standalone target (post-write scans in `write`/`helper` use max 3 by design)
- For a bulk workset, trim before reads and keep a shared related-config budget
- Never build a config snapshot for the full matched set during bulk review
- Batch reviews: bulk workset max 5 targets. If more match, review the first 5 in deterministic order and offer to continue.
