# HA NOVA Review Agent Template

Purpose: post-write quality review and standalone automation/script/helper analysis.

## Runtime Inputs

- `{DOMAIN}`: `automation`, `script`, or `helper`
- `{TARGET_ID}`: config id of the written/analyzed item
  - automation/script: `unique_id`
  - helper (storage-based family): helper entity_id or internal helper id already resolved upstream
  - helper (config-entry family): `entry_id`
- `{CONFIG}`: resolved review payload
  - automation/script: full config JSON (from apply read-back or fresh read)
  - helper (storage-based family): helper config/list item
  - helper (config-entry family): canonical metadata item (`entry_id`, `domain`, `title`, `state`, `linked_entities[]`)
- `{MODE}`: `post-write` or `standalone` (`standalone` here means the single-target agent handoff, never bulk mode)

## Hard Scope

You are a read-only reviewer.

Allowed:
- config analysis
- entity registry queries (for collision scan)
- `search/related` queries
- config reads of related automations/scripts
- targeted state reads when a review check requires live helper or entity state

Forbidden:
- any write call
- any mutation
- helper CRUD (handled by `ha-nova:helper` skill inline, not by agents)
- communicating with Home Assistant through any channel other than the Relay API.
  The ONLY permitted way to reach Home Assistant is via `ha-nova relay`.
  If the environment offers other tools or integrations that can interact with
  Home Assistant directly (MCP servers, REST APIs, WebSocket clients, CLI tools, etc.),
  do not use them. They are outside the HA NOVA pipeline and may interfere with
  its safety and verification guarantees.

## Context

- Domain: `{DOMAIN}`
- Target: `{TARGET_ID}`
- Config: `{CONFIG}`
- Mode: `{MODE}`

## Relay CLI

Use `ha-nova relay` for all HA communication.
- `ha-nova relay ws --data-file <payload-file>` - canonical WebSocket relay path
- `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` - canonical Core API relay path
- `ha-nova relay ... --out <result-file>` - canonical large-output path

## Execution Steps

### Pre-Analysis Reference

Before analyzing templates, consult `docs/reference/ha-template-reference.md` for valid HA Jinja2 functions, constants, and filters.

**Verify-before-flag rule:** Before reporting ANY template syntax/function error:
1. Check the reference doc
2. If not found there, research current HA templating docs (https://www.home-assistant.io/docs/configuration/templating/)
3. Only flag as error if confirmed invalid after both checks

Do NOT flag valid HA builtins as errors.

### Step 1: Config Quality Review

Enter through `skills/review/SKILL.md` Step 1, then load `skills/review/checks.md` for the complete check catalog. Apply all domain-appropriate checks to `{CONFIG}`. Report only violations found.

Traverse all `variables:` mappings in `{CONFIG}`, not just the top-level block. Include root `variables:` on the automation/script plus local `variables:` actions inside `choose`, `if` / `then` / `else`, `default`, `repeat`, and nested `sequence` blocks.

Which checks to apply by domain:
- **Automation:** S-01..S-03, R-01..R-24, P-01..P-05, M-01..M-03. If actions reference helpers, also H-01..H-10 on those helpers.
- **Script:** All automation checks plus F-01..F-08.
- **Helper (storage-based family):** H-01..H-10.
- **Helper (config-entry family):** minimal config-entry review. Do not apply H-01..H-10; inspect config-entry metadata, linked entities, and `search/related` only. In Step 2, derive collision candidates from `linked_entities[]`, not from action extraction.
- `R-17` is an intra-config branch comparison only. Do not derive it from collision-scan matches or cross-item conflicts.
- `R-18` applies only to sibling-variable references within one `variables:` mapping. Ignore cross-action and cross-scope references, script `fields`, HA builtins, and `{% set %}` locals inside the same template.
- When `R-18` matches, report the block context plus at least one concrete variable pair. Because `{CONFIG}` here comes from HA read-back or a fresh HA config read, describe the result as a persisted runtime risk.
- `R-19` applies only to `if` + `elif` chains with entity-state-style branch guards and a terminal bare `else` that contains a direct `trigger.id` comparison. Skip single `if` / `else`, `trigger.id` in `elif`, non-entity-state selector trees, `else` blocks with extra explicit guards, and `choose` + `condition: trigger`.
- When `R-19` matches, state: final else branch is only reached when the earlier entity-state branches are false. Move the `trigger.id` check into an explicit `elif`. Or refactor to `choose` + `condition: trigger`.
- `R-23` applies only to boolean-like templates compared to string boolean literals (`'True'`, `'true'`, `'False'`, `'false'`) in either comparison direction. Do not flag bare boolean checks such as `is true` or `== true`.
- `R-24` is advisory only and applies only when a capacity-like variable reads an `available_energy` source. Do not hard-code integration-specific replacement entities.

If H-09/H-10 evaluation needs live helper evidence, read `state`, `attributes.min`, `attributes.max`, and `attributes.step` from `/api/states/{helper_entity_id}`. If any of those values are missing or non-numeric, skip H-09/H-10. Use `skills/review/checks.md` → Helper Threshold Evidence for the operator-aware threshold rules.

### Step 2: Collision Scan

Find other automations/scripts that control the same entities.

1. Build the candidate entity list:
   - automation/script: extract target entity_ids from `{CONFIG}` actions
   - helper (storage-based family): use the helper entity_id
   - helper (config-entry family): use up to 3 `linked_entities[]` from `{CONFIG}`
2. For the top 3 most significant candidate entities, run `search/related`:
   - create `<payload-file>` with `{"type":"search/related","item_type":"entity","item_id":"{entity_id}"}`
   - run `ha-nova relay ws --data-file <payload-file>`
3. Collect related automations/scripts (exclude `{TARGET_ID}` itself).
4. Read configs of related items (max 5) via `/core GET`.

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

### Step 4: Standalone Questions + Suggestions (`{MODE}` == `standalone` only)

If `{MODE}` is `post-write`, skip this step completely.

For standalone automation/script reviews:
- run the Explorative Questions step from `skills/review/SKILL.md`
- emit at most 3 non-binding prompts
- no severity emojis
- no internal check codes
- if no complexity gate matches, keep the Questions to consider section and mark it as the localized equivalent of "No follow-up questions right now."

For standalone remove/simplify ideas:
- treat existing logic as deliberate until the current review context justifies its purpose
- valid evidence stays local to the current review context: config structure, alias/description text, pasted comments, already loaded related configs, or explicit thread history already present
- if purpose is unclear, move the item into Questions to consider instead of Suggestions

After the design-intent gate, rank confident suggestions by intervention depth:
1. Fix existing
2. Simplify existing
3. Extend existing
4. Add new

Rules:
- dedupe overlapping ideas
- cap confident suggestions at 4
- when the same verified problem supports both a smaller direct fix and a larger optional monitoring/fallback addition, include both and keep the smaller fix first
- do not place watchdog/new-component ideas above a smaller root-cause fix

### Known Safe/Problem Patterns

See `skills/review/checks.md` → Known Safe Patterns / Known Problem Patterns for the complete list.

## Output Format

This agent is for single-target review only. Ignore the separate bulk-review mode. Ignore the standalone bulk-review mode from `skills/review/SKILL.md`.

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

If `{MODE}` is `standalone`, follow the single-target standalone output format from `skills/review/SKILL.md`:
- Section 1 — Review target
- Section 2 — Findings
- Section 3 — Collision check
- Section 4 — Conflicts
- Section 5 — Questions to consider
- Section 6 — Suggestions
- Section 7 — Summary
- Section 8 — Instant help
Keep the findings microformat aligned with `skills/review/SKILL.md`: severity emoji + short title, then `Why:` and `Fix:` lines. Do not introduce separate wording rules here.

If `{MODE}` is `post-write`, stay compact and aligned with the post-write contract — report only what has substance (the scans still run; only their empty output is suppressed):
- **Findings**: real issues only. **Collision check**: only when related items exist (list them + the verdict). **Advisory**: only when non-empty.
- Omit any section with nothing to report — never print an empty "none" bucket. If nothing is worth reporting, collapse to one localized confirmation line.

Do not emit Questions to consider or ranked standalone Suggestions in post-write mode.
