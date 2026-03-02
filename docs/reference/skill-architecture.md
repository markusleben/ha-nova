# HA NOVA Skill Architecture

## Overview

HA NOVA uses a compact, installable 5-skill topology.

Installed skills:
- `ha-nova` (router + safety baseline + runtime prerequisites)
- `ha-nova-write` (automation/script create/update/delete)
- `ha-nova-read` (automation/script list/get)
- `ha-nova-entity-discovery` (state/entity shortlist from `/ws get_states`)
- `ha-nova-onboarding` (onboarding + diagnostics)

Reference files (repo-local, loaded by skills as needed):
- `skills/ha-nova/relay-api.md`
- `skills/ha-nova/best-practices.md`
- `skills/ha-nova/agents/resolve-agent.md`
- `skills/ha-nova/agents/apply-agent.md`

## Routing Model

`ha-nova` routes by intent:
- write intent (`create|update|delete` automation/script) -> `ha-nova-write`
- read intent (`list|get` automation/script) -> `ha-nova-read`
- entity lookup intent -> `ha-nova-entity-discovery`
- onboarding/connectivity/auth diagnostics -> `ha-nova-onboarding`

Routing is skill-name based (not filesystem path based), so behavior is stable across:
- `~/.agents/skills`
- `~/.claude/skills`
- `~/.config/opencode/skills`

## Write Architecture

`ha-nova-write` uses a deterministic three-phase flow:

1. Resolve (Agent)
- load env
- fetch states
- resolve entities and target id
- check existence + current config
- evaluate best-practice snapshot status

2. Preview + Decide (Main Thread)
- build final payload
- show compact preview blocks
- ask one decision question only if ambiguous
- confirmation tier:
  - create/update: natural confirmation bound to active preview
  - delete: tokenized `confirm:<token>`

3. Apply + Verify (Agent)
- write via relay `/core`
- read-back verification
- normalized compare (`trigger(s)`, `condition(s)`, `action(s)`)
- structured error result on partial or failed verification

Fallback:
- if agent dispatch unavailable, execute same phases inline serially.

## Read Architecture

`ha-nova-read` is intentionally direct/low-overhead:
- no subagent dispatch for routine reads
- `/ws get_states` for list operations
- `/core` config reads for single-item get operations
- one blocking question only if target ambiguity remains

## Installer Contract

`scripts/onboarding/install-local-skills.sh`:
- installs exactly the 5 active skills above
- archives legacy skill directories to timestamped backups
- does not destructively delete unknown legacy content

Legacy names that are archived during install:
- `ha-nova-automation-create`
- `ha-nova-automation-update`
- `ha-nova-automation-delete`
- `ha-nova-script-create`
- `ha-nova-script-update`
- `ha-nova-script-delete`
- `ha-nova-resolve-targets`
- `ha-nova-onboarding-diagnostics`

## Safety Baseline

Global safety expectations:
- no guessed ids
- preview before any write
- delete requires tokenized confirmation
- structured failure output: what failed / why / next step
- diagnostics only after real capability failure

## Operational Goal

Minimize context and maintenance overhead while preserving strict write safety:
- fewer installed skills
- centralized relay contract
- explicit phase boundaries
- deterministic preview/confirm/apply behavior
