---
name: ha-nova
description: Use when the user wants Home Assistant operations through HA NOVA (App + Relay) with macOS Keychain-backed local auth.
---

# HA NOVA Skill


## Mission

Operate Home Assistant through HA NOVA with a minimal user-facing flow:
- App + Relay first
- preview before write
- one blocking question only when required
- compact result output

## Runtime Prerequisite (macOS)

Before HA operations in this session:

1. Verify relay CLI: `~/.config/ha-nova/relay health`
2. If this fails, ask user to run: `npm run onboarding:macos`
3. Do not run diagnostics proactively; diagnose only after real failure.
4. Relay-only auth model: do not request or persist LLAT client-side.
   - LLAT belongs in App option `ha_llat`

Do not ask user to paste tokens in chat.

## Quoting Reliability (Critical)

Quoting is shell-dependent (bash/zsh vs PowerShell), not primarily OS-dependent.

Rules:
- Keep command examples copy-pastable as shown.
- Avoid unnecessary escaping in bash/zsh snippets.
- If shell is not bash-compatible, stop and ask user to switch shell.

## Safety Baseline

- Never guess entity IDs, service names, or config IDs.
- Preview every write payload.
- Confirmation tiers:
  - `create`/`update`: natural confirmation bound to active preview.
  - `delete`/destructive: token confirmation `confirm:<token>`.
- Ask exactly one blocking question only if ambiguity remains.
- Failure format must include:
  - what failed
  - why it failed
  - next concrete step

## Response Format

Render structured summary + YAML for both reads and writes:
1. `Automation` or `Script` (name + ID)
2. `Entities` (all entity_ids in triggers/conditions/actions)
3. `Triggers`, `Conditions`, `Actions` (short descriptions)
4. `Mode` (single/restart/queued/parallel)
5. full YAML config block
6. `Next Step` (for writes: confirmation; for reads: done)

Keep orchestration details internal on normal success paths.

## Routing Table

- Write intent (`create|update|delete` for automation or script):
  - use skill `ha-nova:write`
- Read intent (`list|get|trace` for automation or script):
  - use skill `ha-nova:read`
- Service call intent (turn on, turn off, toggle, set, call service):
  - use skill `ha-nova:service-call`
- Automation/script runtime control (enable, disable, trigger):
  - use skill `ha-nova:service-call` (services: `automation.turn_on`, `automation.turn_off`, `automation.trigger`)
- Review / analyze intent (`review|analyze|check|audit` for automation or script):
  - use skill `ha-nova:review`
- Entity discovery / target lookup:
  - use skill `ha-nova:entity-discovery`
- Onboarding / connectivity / auth diagnostics:
  - use skill `ha-nova:onboarding`

## Latency Policy

- Prefer one-shot reads over multi-step probing.
- For first read/list, try Relay `/ws` directly.
- For write flows, keep main-thread file reads minimal:
  - router skill
  - `skills/ha-nova/relay-api.md`
  - one agent template per phase
- No proactive doctor in success path.
- Re-read full state snapshot only with explicit reason.
