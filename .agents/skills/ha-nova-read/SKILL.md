---
name: ha-nova-read
description: Use when listing or reading Home Assistant automation and script configs through HA NOVA Relay.
---

# HA NOVA Read

<!-- ha-nova-managed-install repo_root: __HA_NOVA_REPO_ROOT__ -->

## Scope

Read operations only:
- `automation.list`
- `automation.read`
- `script.list`
- `script.read`
- `automation.trace`
- `script.trace`

No writes.

## Bootstrap (once per session)

Verify relay CLI: `~/.config/ha-nova/relay health`
If this fails: `npm run onboarding:macos`

## Flow

1. For list operations:
   - `~/.config/ha-nova/relay ws -d '{"type":"get_states"}'`
   - filter by domain prefix (`automation.` / `script.`)
2. For single-item reads:
   - `~/.config/ha-nova/relay core -d '{"method":"GET","path":"/api/config/automation/config/{id}"}'`
   - for script reads use `/api/config/script/config/{id}`
3. If id is ambiguous, ask one clarifying question.
4. Return compact domain-focused output.

## Trace Debugging

For trace queries ("why didn't automation X fire?", "show me the last runs"):

1. Resolve automation/script ID (use entity discovery if name is ambiguous).
2. List recent traces:
   - `~/.config/ha-nova/relay ws -d '{"type":"trace/list","domain":"automation","item_id":"{id}"}'`
   - for scripts: `"domain":"script"`
3. For detailed trace (specific run):
   - `~/.config/ha-nova/relay ws -d '{"type":"trace/get","domain":"automation","item_id":"{id}","run_id":"{run_id}"}'`
4. Analyze trace and present in user-friendly format:
   - When it ran (timestamp)
   - Trigger: what fired (or didn't)
   - Conditions: which passed/failed
   - Actions: what executed, any errors
   - Result: success/error/aborted
5. If no traces found: tell user the automation may not have triggered recently, or tracing may be disabled.

## Latency Policy

- no agent dispatch for simple reads
- no proactive `/health` preflight
- no exploratory retry loops without concrete failure

## Safety

- never guess ids
- if multiple close matches, ask one selection question
