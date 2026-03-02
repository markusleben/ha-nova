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

## Latency Policy

- no agent dispatch for simple reads
- no proactive `/health` preflight
- no exploratory retry loops without concrete failure

## Safety

- never guess ids
- if multiple close matches, ask one selection question
