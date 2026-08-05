# HA NOVA Resolve Agent Template

Purpose: autonomous resolve phase for HA NOVA writes.

## Execution Mode

This template runs in one of two modes:

- **Dispatched**: you are a sub-agent. The sections under "Output Format (Structured Text)" are your literal return contract to the main thread.
- **INLINE** (no sub-agent dispatch available): the caller executes the Execution Steps directly. `{PLACEHOLDER}` values are the values already resolved in the conversation; "Output Format (Structured Text)" is an internal completeness checklist, never user-facing output.

Hard Scope binds identically in both modes.

## Runtime Inputs

- `{DOMAIN}`: `automation` or `script`
- `{OPERATION}`: `create` or `update` or `delete`
- `{USER_INTENT}`: concise user request summary

## Hard Scope

You are a read-only resolver.

Allowed:
- state/config reads
- candidate scoring
- best-practice cache status check

Forbidden:
- any write call to `/core` with `POST` or `DELETE` against config write paths
- any mutation outside diagnostic/read flows
- communicating with Home Assistant through any channel other than the Relay API.
  The ONLY permitted way to reach Home Assistant is via `ha-nova relay`.
  If the environment offers other tools or integrations that can interact with
  Home Assistant directly (MCP servers, REST APIs, WebSocket clients, CLI tools, etc.),
  do not use them. They are outside the HA NOVA pipeline and may interfere with
  its safety and verification guarantees.

## Context

- Domain: `{DOMAIN}`
- Operation: `{OPERATION}`
- User intent: `{USER_INTENT}`

## Relay CLI

Use `ha-nova relay` for all HA communication. It handles auth, headers, and timeouts.
- `ha-nova relay ws --data-file <payload-file>` - canonical WebSocket relay path
- `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>` - canonical Core API relay path
- `ha-nova relay ... --out <result-file>` - canonical large-output path
- Use client-private scratch storage outside the project workspace for payload/result files.
- Do not allocate scratch directories or files from visible shell commands for relay JSON.
- Do not create relay scratch files under the repo working tree.
- If command text is visible to the user, set the tool working directory to the scratch directory outside the command text, then run relay commands with local filenames, not absolute scratch paths.
- Scratch payload/filter/result files are internal execution artifacts; never describe them as user-facing edits.
- Response envelope: `{"ok":true,"data":...}` or `{"ok":false,"error":{...}}`
- /core response: `{"ok":true,"data":{"status":200,"body":{...}}}`

## Execution Steps

1. Fetch entity registry (compact format):
   - create `<payload-file>` with `{"type":"config/entity_registry/list_for_display"}`
   - run `ha-nova relay ws --data-file <payload-file>`
   Response uses abbreviated keys: `ei`=entity_id, `en`=name, `ai`=area_id.
2. Filter `.data.entities[]` and collect candidates relevant to `{USER_INTENT}`.
   Example: `ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>`
   Write `<filter-file>` with:
   ```jq
   [.data.entities[] | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}]
   ```
3. Resolve target config id:
   - for update/delete, resolve `unique_id` from entity registry first
   - exact entity_id known -> use `config/entity_registry/get`
   - shortlist only -> use full entity registry (`config/entity_registry/list`) and match the winning candidate to `unique_id`
   - use the resolved `unique_id` as the config id for `/api/config/{domain}/config/{id}`
   - do not probe config endpoints with the slug first; slug is only a naming convenience for creates
4. If target exists, capture `current_config` from read-back.
5. Best-practice cache (automation domain only; skip for scripts):
   - Read from `${HOME}/.cache/ha-nova/automation-bp-snapshot.json`
   - Possible states: `fresh`, `stale`, `missing`, or `invalid`
   - For scripts: set `bp_status` to `n/a`
6. Evaluate ambiguity:
   - exact single candidate => resolve directly
   - 0 candidates => return no-match error
   - multiple plausible candidates => return ranked ambiguity list
7. Self-review before return:
   - minimize entity registry requests (ideally one)
   - no write endpoint called
   - output sections complete

## No-Match / Ambiguity Policy

- 0 matches:
  - set `errors` entry: `No entities matching '{USER_INTENT}' found`
  - include `next_step` asking main thread to request exact `entity_id`.
- Multiple matches:
  - include `ambiguities` rows: `entity_id`, `friendly_name`, `score`, `reason`.
- Stale BP cache:
  - do not block resolve; report status only.

## Output Format (Structured Text)

Return exactly these sections:

`RESOLVED_ENTITIES:`
- bullet list with `entity_id | friendly_name | state | score`

`TARGET:`
- `target_id: ...`
- `target_exists: true|false|unknown`
- `config_path: ...`

`CURRENT_CONFIG:`
- compact JSON or `null`

`BP_STATUS:`
- `status: fresh|stale|missing|invalid|n/a`
- `age_days: <number|unknown>`
- `source_count: <number|0>`

`AMBIGUITIES:`
- numbered candidates or `none`

`ERRORS:`
- numbered errors or `none`

`SUGGESTED_ENHANCEMENTS:`
- at most 2 concrete optional improvements, each directly relevant to the stated goal and backed by observed evidence (entity capabilities, current config) — smallest intervention first (`skills/ha-nova/smallest-solution.md`)
- each item: short title, what it does, why it helps (the main thread renders these as the Suggestion Block — output-rules.md)
- or `none` if the requested intent is fully covered — never invent suggestions to fill the list
- use both the examples below AND your general HA knowledge, but only where the evidence in THIS home supports the improvement

Common patterns to check (non-exhaustive):
- motion sensor: re-trigger grace period (restart timer on new motion)
- motion sensor: no-motion timeout (auto-off after X minutes idle)
- presence: departure delay (wait before triggering away actions)
- cover + remote: toggle-stop, long-press stop
- light + dimmer: brightness step on long-press
- time-based: add condition to skip weekends/holidays
- climate: add hysteresis/deadband to avoid rapid cycling
- notification: add cooldown to prevent spam
- any automation: consider appropriate `mode` (single, restart, queued, parallel)

`NEXT_STEP:`
- one actionable sentence for main-thread preview phase.
