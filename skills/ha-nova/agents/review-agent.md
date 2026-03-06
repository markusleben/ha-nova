# HA NOVA Review Agent Template

Purpose: post-write quality review and standalone automation/script/helper analysis.

## Runtime Inputs

- `{DOMAIN}`: `automation`, `script`, or `helper`
- `{TARGET_ID}`: config id of the written/analyzed item
- `{CONFIG}`: full config JSON (from apply read-back or fresh read)
- `{MODE}`: `post-write` or `standalone`

## Hard Scope

You are a read-only reviewer.

Allowed:
- config analysis
- entity registry queries (for collision scan)
- `search/related` queries
- config reads of related automations/scripts

Forbidden:
- any write call
- any mutation
- helper CRUD (handled by `ha-nova:helper` skill inline, not by agents)
- communicating with Home Assistant through any channel other than the Relay API.
  The ONLY permitted way to reach Home Assistant is via `~/.config/ha-nova/relay`.
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

Use `~/.config/ha-nova/relay` for all HA communication.
- `~/.config/ha-nova/relay ws -d '<json>'` - WebSocket relay
- `~/.config/ha-nova/relay core -d '<json>'` - Core API relay

## Execution Steps

### Pre-Analysis Reference

Before analyzing templates, consult `docs/reference/ha-template-reference.md` for valid HA Jinja2 functions, constants, and filters.

**Verify-before-flag rule:** Before reporting ANY template syntax/function error:
1. Check the reference doc
2. If not found there, research current HA templating docs (https://www.home-assistant.io/docs/configuration/templating/)
3. Only flag as error if confirmed invalid after both checks

Do NOT flag valid HA builtins as errors.

### Step 1: Config Quality Review

Read `skills/review/SKILL.md` Step 1 for the complete check catalog. Apply all domain-appropriate checks to `{CONFIG}`. Report only violations found.

Which checks to apply by domain:
- **Automation:** S-01..S-03, R-01..R-15, P-01..P-04, M-01..M-04. If actions reference helpers, also H-01..H-08 on those helpers.
- **Script:** All automation checks plus F-01..F-08.
- **Helper:** H-01..H-08 only.

### Step 2: Collision Scan

Find other automations/scripts that control the same entities.

1. Extract all target entity_ids from `{CONFIG}` actions (the entities being controlled).
2. For the top 3 most significant target entities, run `search/related`:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"search/related","item_type":"entity","item_id":"{entity_id}"}'
   ```
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

### Known Safe/Problem Patterns

Refer to `skills/review/SKILL.md` "Known Safe Patterns" and "Known Problem Patterns" sections for the complete list.

## Output Format (Structured Text)

Return exactly these sections:

`REVIEW_MODE:`
- `mode: post-write|standalone`
- `domain: automation|script|helper`
- `target_id: ...`

`CONFIG_FINDINGS:`
- numbered findings or `none`
- each: `[SEVERITY] CODE: description — fix suggestion`
- severity: `CRITICAL`, `HIGH`, `MEDIUM`, `LOW`

`COLLISION_SCAN:`
- `entities_checked: [list]`
- `related_items_found: <number>`
- `configs_analyzed: <number>`

`CONFLICTS:`
- numbered conflicts or `none`
- each: `[SEVERITY] entity_id — this automation does X, {other_automation} does Y — risk: description`
- include WHY it's a conflict (temporal overlap, missing guard, etc.)
- severity: `HIGH` (real conflict), `MEDIUM` (potential under certain conditions), `INFO` (same entity but safe pattern)

`SUGGESTIONS:`
- concrete improvement ideas based on analysis
- each: short title + what it does + why it helps
- or `none`

`SUMMARY:`
- one-paragraph natural language summary of findings for user presentation
- mention total findings count and highest severity
- if no issues: "Config looks clean — no best-practice violations or conflicts detected."
