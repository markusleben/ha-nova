# HA NOVA Skill Architecture

## Overview

HA NOVA uses a nested skill topology with one router and 6 sub-skills under `ha-nova/`.

Installed skill tree:
```
ha-nova/
  SKILL.md                  (router + safety baseline + runtime prerequisites)
  write/SKILL.md            (automation/script create/update/delete)
  read/SKILL.md             (automation/script list/get/trace)
  entity-discovery/SKILL.md (entity lookup via `/ws config/entity_registry/list_for_display`)
  service-call/SKILL.md     (service calls + automation/script runtime control)
  review/SKILL.md           (config quality review + collision scan + conflict analysis)
  onboarding/SKILL.md       (onboarding + diagnostics)
```

Reference files (repo-local, loaded by skills as needed):
- `skills/ha-nova/relay-api.md`
- `skills/ha-nova/best-practices.md`
- `skills/ha-nova/agents/resolve-agent.md`
- `skills/ha-nova/agents/apply-agent.md`

## Routing Model

`ha-nova` routes by intent using `ha-nova:<skill>` references:
- write intent (`create|update|delete` automation/script) -> `ha-nova:write`
- read intent (`list|get|trace` automation/script) -> `ha-nova:read`
- service call intent (turn on/off, toggle, set) -> `ha-nova:service-call`
- automation/script runtime control (enable, disable, trigger) -> `ha-nova:service-call`
- review/analyze intent (`review|analyze|check|audit`) -> `ha-nova:review`
- entity lookup intent -> `ha-nova:entity-discovery`
- onboarding/connectivity/auth diagnostics -> `ha-nova:onboarding`

Routing is skill-name based (not filesystem path based), so behavior is stable across:
- `~/.agents/skills`
- `~/.claude/skills`
- `~/.config/opencode/skills`

## Write Architecture

`ha-nova:write` uses a deterministic three-phase flow:

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

4. Review
- post-write quality review via `ha-nova:review`
- config quality checks, collision scan, conflict analysis
- findings are advisory (write already succeeded)

Fallback:
- if agent dispatch unavailable, execute same phases inline serially.

## Read Architecture

`ha-nova:read` is intentionally direct/low-overhead:
- no subagent dispatch for routine reads
- `/ws config/entity_registry/list_for_display` for list operations
- `/core` config reads for single-item get operations
- one blocking question only if target ambiguity remains

## Review Architecture

`ha-nova:review` is a self-contained read-only reviewer:
- Config quality: safety (S-01..S-03), reliability (R-01..R-11), performance (P-01..P-04), style (M-01..M-04), script-specific (F-01..F-08)
- Collision scan: `search/related` on top 3 target entities
- Conflict analysis: 3-step test (polarity → temporal → guard conditions)
- Known safe/problem pattern matching

## Installer Contract

`scripts/onboarding/install-local-skills.sh`:
- source skill tree: `skills/ha-nova/` (repo-local)
- client-specific install strategies:
  - **Claude Code:** Skipped — uses plugin system (`.claude-plugin/plugin.json`)
  - **Codex CLI:** Symlink `~/.agents/skills/ha-nova` → `${REPO_ROOT}/skills/ha-nova`
  - **OpenCode:** Symlink `~/.config/opencode/skills/ha-nova` → `${REPO_ROOT}/skills/ha-nova`
  - **Gemini CLI:** Flat copy `~/.agents/skills/ha-nova-{skill}/SKILL.md` (1-level limit)
- cleans up legacy flat skill directories (`ha-nova-write`, `ha-nova-read`, etc.)
- supports targets: `codex`, `claude`, `opencode`, `gemini`, `all`

## Safety Baseline

Global safety expectations:
- no guessed ids
- preview before any write
- delete requires tokenized confirmation
- structured failure output: what failed / why / next step
- diagnostics only after real capability failure

## Operational Goal

Minimize context and maintenance overhead while preserving strict write safety:
- nested skill tree under one parent
- centralized relay contract
- explicit phase boundaries
- deterministic preview/confirm/apply behavior
