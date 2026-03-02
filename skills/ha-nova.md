---
name: ha-nova
description: Use when the user wants Home Assistant operations through HA NOVA (App + Relay) with macOS Keychain-backed local auth.
---

# HA NOVA Skill

<!-- ha-nova-managed-install repo_root: __HA_NOVA_REPO_ROOT__ -->

## Mission

Operate Home Assistant through HA NOVA with a simple user flow:
- App + Relay first
- fastest viable path first
- minimal prompts
- safe write confirmation

## Orchestration Hard Gate (First Step, Mandatory)

Before the first Relay call in any non-trivial flow, compute:
- `independent_units_count`
- `substantial_independent_units`
- `subagent_capable`
- `fan_out_required`

Decision:
- Classify each independent unit as `substantial` or `lightweight` using this rubric:
  - `substantial` if any applies:
    - needs a full-state snapshot (`get_states`) or equivalent high-volume discovery/filter pass
    - performs best-practice refresh/validation work that can block writes
    - performs an ID/existence read that changes write path decisions (for example 200/404 branch)
  - `lightweight` otherwise.
- If `substantial_independent_units >= 3` and `subagent_capable=true`, then `fan_out_required=true` and subagent fan-out must run before any Relay discovery/read call.
- If `substantial_independent_units < 3`, run native parallel calls in the main agent (no subagent boot).
- If subagents are unavailable, run native parallel calls and set `subagent_capable=false` internally.

Fail-closed rule:
- Starting Relay discovery/read calls before this gate in a `>=3-substantial-unit` flow is a protocol violation.
- Keep these values internal by default; expose only in diagnostics/failure paths or when the user explicitly asks.

## Canonical Automation DAG (Create/Update)

Use this strict hierarchy for automation create/update:
1. Phase A (parallel, independent):
   - `A1`: best-practice snapshot validate/refresh
   - `A2`: entity resolution from one shared state snapshot
   - `A3`: automation id existence check
2. Phase B (sequential, dependent):
   - `preview -> confirm:<token> -> apply -> verify`

Do not interleave Phase A and Phase B.

## Reusable Fast-Pass Blocks (MVP)

Normative fast-pass block definitions and compositions are defined only in:
- `skills/ha-nova/core/blocks.md`

Source-of-truth references:
- `skills/ha-nova/core/blocks.md`
- `skills/ha-nova/core/contracts.md`
- `skills/ha-nova/core/intents.md`
- `skills/ha-nova/core/discovery-map.md`

## Runtime Prerequisite (macOS)

Before HA operations in this session:

1. Resolve repository root:
   - `NOVA_REPO_ROOT="${NOVA_REPO_ROOT:-${HA_NOVA_REPO_ROOT:-__HA_NOVA_REPO_ROOT__}}"`
   - if missing script at `$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh`, fallback:
     - `NOVA_REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"`
   - if script is still missing, stop and ask user to:
     - clone `ha-nova`,
     - run `npm install`,
     - run `npm run install:codex-skill`,
     - restart the client.
2. Load runtime env once per shell session from local config + Keychain relay auth:
   - `eval "$(bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env)"`
3. Do not run readiness/doctor proactively before the first HA action.
4. Diagnose only on explicit capability failure:
   - examples: relay unreachable, auth rejected, invalid token, request timeout
5. If diagnosis is needed:
   - ask user to run `cd "$NOVA_REPO_ROOT" && npm run onboarding:macos`
   - then run `bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" doctor` for detailed diagnostics
   - stop until onboarding is healthy
6. Relay-only auth model:
   - do not request or store LLAT in client-side Keychain/env for end-user flow
   - LLAT is configured in App option `ha_llat`
   - if `doctor` reports `ha_ws_connected=false`, route user to App config fix (set `ha_llat`, restart App)

Do not ask user to paste tokens in chat.
Do not run proactive network preflight checks before the first read action.

## Quoting Reliability (Critical)

Quoting is shell-dependent (bash/zsh vs PowerShell), not primarily OS-dependent.
Use canonical bash-compatible quoting exactly as shown below.

- Correct (bash/zsh):
  - `eval "$(bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env)"`
- Incorrect:
  - `eval "$(bash \"$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh\" env)"`

Rules:
- Do not escape inner double quotes with backslashes in normal bash/zsh commands.
- Standardize NOVA command snippets to bash-compatible shells only:
  - macOS: zsh/bash
  - Linux: bash/zsh
  - Windows: WSL bash or Git Bash
- If shell is not bash-compatible (for example PowerShell-only), stop and ask user to run in a bash-compatible shell for deterministic behavior.

## Execution Latency Policy

- Prioritize one-shot commands over multi-step probing for read requests.
- Do not print internal progress logs in normal success paths.
- For first read/list requests, attempt Relay `/ws` directly.
- Run `doctor` only after an actual request failure, not as a startup ritual.
- For Relay `/core` responses, parse only `.ok`, `.data.status`, `.data.body`.
- Do not run ad-hoc JSON schema probing with custom jq in normal flows.
- For `/ws` `get_states`, always treat response as `{ ok, data[] }` and filter object entries only.
- For entity discovery output, return a shortlist only (default max 20) and prefer exact/high-confidence matches first.
- In one flow, fetch full `get_states` at most once and reuse it for all filters (`one-state-snapshot` rule).
- Repeating full-state reads is allowed only with a stated reason (for example stale-state suspicion after write/reload).

## Parallel Orchestration (MVP/KISS)

Apply this graph discipline:
1. Build a task graph and label each step:
   - `independent`
   - `dependent`
2. Run all `independent` tasks in parallel (Fan-out).
3. Join results once (Fan-in), then execute dependent path.
4. Use deterministic fallback: if parallel is unsupported, run the same graph sequentially without extra prompts.

Typical independent tasks:
- best-practice refresh check
- entity discovery
- existence/read checks
- diagnostics branches (connectivity/auth/shape), then choose one root cause

Never parallelize:
- `preview -> confirm:<token> -> apply`
- writes targeting the same object scope
- reload/delete with in-flight writes on the same scope

## Subagent Dispatch Protocol (Required)

Policy:
- Parallelism is mandatory when capability exists.
- Subagent fan-out is mandatory only for `>=3` substantial independent task units.
- For `<3` substantial units, use native parallel tool calls in the main agent.
- Native parallel tool calls are also the fallback when subagent capability is unavailable.

Trigger for subagent fan-out:
- Use subagents whenever there are `>=3` substantial independent units.

Minimal dispatch pattern:
1. Split work into independent task units.
2. Dispatch one subagent per unit.
3. Wait for all units.
4. Join findings once and continue with dependent steps.

Red flags (forbidden):
- sequential execution of substantial independent units when parallel support exists
- running Relay discovery/read before required subagent fan-out in `>=3-substantial-unit` flows
- dispatching subagents for confirm-gated writes
- dispatching subagents that mutate the same target concurrently

## Read-Only Fast Shortcut (Trivial Single-Unit Only)

Use this directly only when all conditions are true:
- `independent_units_count = 1`
- no write intent
- no ambiguity resolution branch required

For non-trivial reads (`independent_units_count >= 2`), this shortcut is forbidden; run the full orchestration gate and choose native-parallel vs subagent by threshold.

Trivial read shortcut command:

```bash
[[ -n "${RELAY_BASE_URL:-}" && -n "${RELAY_AUTH_TOKEN:-}" ]] || eval "$(bash "$NOVA_REPO_ROOT/scripts/onboarding/macos-onboarding.sh" env)"
DOMAIN="${DOMAIN:-light}"
LIMIT="${LIMIT:-5}"
curl -sS -X POST \
  -H "Authorization: Bearer $RELAY_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  "$RELAY_BASE_URL/ws" \
  -d '{"type":"get_states"}' | \
jq -r --arg domain "$DOMAIN." --argjson limit "$LIMIT" \
  'limit($limit; (.data // [])[] | select((.entity_id|type)=="string" and (.entity_id|startswith($domain))) | .entity_id)'
```

Output rule for this shortcut:
- return compact domain summary only (result + next step)
- keep orchestration mechanics hidden unless diagnostics are explicitly requested

## Routing

- Setup/connectivity/user first-run questions:
  - use `"$NOVA_REPO_ROOT/skills/ha-onboarding.md"`
- Entity discovery/listing:
  - use `"$NOVA_REPO_ROOT/skills/ha-entities.md"`
- Device control:
  - use `"$NOVA_REPO_ROOT/skills/ha-control.md"` + `"$NOVA_REPO_ROOT/skills/ha-safety.md"` for write intents
- Automation/Script (`lazy module loading`):
  - resolve exact intent mapping from:
    - `"$NOVA_REPO_ROOT/skills/ha-nova/core/intents.md"` (canonical)
    - `"$NOVA_REPO_ROOT/skills/ha-nova/core/discovery-map.md"` (loading protocol)
  - load only `required_companions[]` + `modules[]` for the resolved intent.
- Automation runtime control:
  - use `"$NOVA_REPO_ROOT/skills/ha-automation-control.md"` + `"$NOVA_REPO_ROOT/skills/ha-safety.md"`

## Lazy Discovery Protocol (Mandatory)

1. Load router + `core/contracts.md` first.
2. Resolve intent class (`automation|script` + `create|update|delete|read|list`).
3. Load only `required_companions[]` + `modules[]` from `core/intents.md`.
4. Use `core/discovery-map.md` rules for progressive/lazy loading behavior.
5. Load diagnostics guidance only after a real failure.
6. Do not preload all modules in normal flow.

## Automation Write Freshness Gate

- Before any automation `create`/`update` plan, require a valid best-practice refresh snapshot.
- Snapshot is valid if age <= 30 days and HA major/minor did not change.
- If snapshot is stale/missing, refresh from official Home Assistant docs/release notes and store timestamp + source list.
- If refresh fails and no valid snapshot exists, block write planning and return remediation steps.
- Persist snapshot for cross-session reuse at `${HOME}/.cache/ha-nova/automation-bp-snapshot.json`.

## Safety Baseline

- Never guess entity IDs.
- Preview every write payload.
- Require explicit tokenized confirmation before write execution (`confirm:<token>`).
- Keep terminology as App + Relay.

## Domain-First Response Contract (MVP)

Normative contract is defined only in:
- `"$NOVA_REPO_ROOT/skills/ha-nova/core/contracts.md"`

Router-level rule:
- keep orchestration mechanics internal in normal success paths.
