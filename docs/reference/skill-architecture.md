# HA NOVA Skill Architecture

## Overview

HA NOVA uses a flat skill layout with one context skill and 6 independent sub-skills under `skills/`.

Claude Code discovers all skills via `skills/{name}/SKILL.md` (1 level deep). The context skill is auto-loaded via a SessionStart hook.

Installed skill tree:
```
skills/
  ha-nova/SKILL.md              (context skill — auto-loaded via SessionStart hook)
  ha-nova/relay-api.md          (reference doc)
  ha-nova/best-practices.md     (reference doc)
  ha-nova/payload-schemas.md    (reference doc)
  ha-nova/agents/               (agent templates: resolve, apply, review)
  read/SKILL.md                 (ha-nova:read — automation/script list/get/trace)
  write/SKILL.md                (ha-nova:write — automation/script create/update/delete)
  review/SKILL.md               (ha-nova:review — config quality review + collision scan)
  entity-discovery/SKILL.md     (ha-nova:entity-discovery — entity lookup)
  service-call/SKILL.md         (ha-nova:service-call — service calls + runtime control)
  onboarding/SKILL.md           (ha-nova:onboarding — onboarding + diagnostics)
```

## Discovery Model

Claude Code scans `skills/*/SKILL.md` (1 level) and matches skills by their `description` frontmatter.

The context skill (`ha-nova:ha-nova`) is auto-loaded into every session via a SessionStart hook (`hooks/session-start`), providing:
- Safety baseline
- Response format
- Runtime prerequisites
- Quoting rules
- Latency policy

Sub-skills are discovered independently by Claude Code based on their descriptions. No router dispatch needed.

## SessionStart Hook

`hooks/hooks.json` registers a SessionStart hook that:
1. Reads `skills/ha-nova/SKILL.md`
2. Returns JSON with `additional_context` containing the full context skill content
3. Fires on: startup, resume, clear, compact

This follows the same pattern as the superpowers plugin.

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
- source skill tree: `skills/` (repo-local, flat layout)
- client-specific install strategies:
  - **Claude Code:** Skipped — uses plugin system (`.claude-plugin/plugin.json`) + SessionStart hook
  - **Codex CLI:** Symlink `~/.agents/skills/ha-nova` → `${REPO_ROOT}/skills`
  - **OpenCode:** Symlink `~/.config/opencode/skills/ha-nova` → `${REPO_ROOT}/skills`
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
- flat skill layout for direct discovery
- context skill auto-loaded via hook
- centralized relay contract
- explicit phase boundaries
- deterministic preview/confirm/apply behavior
