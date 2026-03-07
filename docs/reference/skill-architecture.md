# HA NOVA Skill Architecture

## Overview

HA NOVA uses a flat skill layout with one context skill and 7 independent sub-skills under `skills/`.

Claude Code discovers all skills via `skills/{name}/SKILL.md` (1 level deep). The context skill is auto-loaded via a SessionStart hook.

Installed skill tree:
```
skills/
  ha-nova/SKILL.md              (context skill — auto-loaded via SessionStart hook)
  ha-nova/relay-api.md          (reference doc)
  ha-nova/best-practices.md     (reference doc)
  ha-nova/payload-schemas.md    (reference doc)
  ha-nova/helper-schemas.md     (reference doc — helper type payloads)
  ha-nova/agents/               (agent templates: resolve, apply, review)
  read/SKILL.md                 (ha-nova:read — automation/script list/get/trace)
  write/SKILL.md                (ha-nova:write — automation/script create/update/delete)
  helper/SKILL.md               (ha-nova:helper — helper CRUD: list/read/create/update/delete)
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

## Agent vs Inline Decision Rule

When building a new skill, decide execution model by these criteria:

**Use agents when ALL of these apply:**
- 5+ relay calls in a single operation
- Multi-step deterministic logic (resolve with fallback, write with normalization)
- Nested payload structures requiring comparison/normalization (e.g., trigger/triggers aliasing)
- Domain reload required after write

**Use inline when ANY of these apply:**
- 1-4 relay calls per operation
- Flat payloads (no nested triggers/conditions/actions)
- Direct user interaction needed between steps (preview → confirm → execute)
- No payload normalization quirks

Current mapping:

| Skill | Model | Why |
|-------|-------|-----|
| read | inline | 1-2 calls, direct output |
| write | **agents** | 5-7 calls, entity resolution fallback, singular/plural normalization, domain reload |
| helper | inline | 2-4 calls, flat configs, no normalization |
| review | inline | analysis is client-side, relay calls are reads only |
| entity-discovery | inline | 1-2 calls, search + return |
| service-call | inline | 2-3 calls, preview + execute |
| onboarding | inline | diagnostics only |

**Rule of thumb:** If a `service-call` could do it, it's inline. If it needs what `write` needs (resolve + normalize + reload), use agents.

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
- Config quality: safety (S-01..S-03), reliability (R-01..R-15), performance (P-01..P-04), style (M-01..M-04), script-specific (F-01..F-08), helper-specific (H-01..H-08)
- Collision scan: `search/related` on top 3 target entities
- Conflict analysis: 3-step test (polarity → temporal → guard conditions)
- Known safe/problem pattern matching

## Helper Architecture

`ha-nova:helper` handles CRUD for 9 storage-based helper types via WebSocket commands:
- Types: `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`, `input_button`, `counter`, `timer`, `schedule`
- Transport: WS (`{type}/create`, `{type}/update`, `{type}/delete`) — not REST `/core`
- Identity: `{type}_id` (internal unique_id from list), not entity_id
- All operations inline (no agents) — configs are flat, no complex resolution or normalization needed
- Write: WS `{type}/create|update|delete` + `{type}/list` verify
- Review: H-01..H-08 helper-specific checks + collision scan via `search/related`
- No domain reload needed — storage-based, immediate effect

Excluded: config-entry flow helpers (template, group, utility_meter) — different API pattern.

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

## Skill Section Template

Standard sections for all sub-skills. Follow this when creating or auditing skills.

**Required for ALL skills:**
- **Scope** — what this skill does + inverse scope (what it does NOT do, which skill to use instead)
- **Bootstrap** — relay CLI verification + onboarding fallback
- **Flow** — step-by-step operations with relay commands
- **Output Format** — what the user receives (structure, content)
- **Safety** — risk mitigations, confirmation rules, relay-only constraint
- **Guardrails** — limits, constraints, "never use raw `get_states`"

**Required for MUTATION skills** (write, helper):
- **Post-Write Review** — mandatory inline review after every create/update/delete
- **References** — links to schema docs, relay API, review checks

**Optional:**
- **Error Handling** — error classification + remediation (recommended for external calls)
- **Latency Policy** — when to optimize for speed

## Post-Write Review Standard

Unified spec for post-write review. Both `write` and `helper` skills reference this.

After any mutation (automation, script, or helper):
1. Re-read written config via relay
2. Run domain-appropriate checks from `skills/review/SKILL.md` Step 1:
   - **Automations:** S + R + P + M checks. If actions reference helpers, also H checks on those helpers.
   - **Scripts:** S + R + P + M + F checks. If actions reference helpers, also H checks.
   - **Helpers:** H checks only.
   Focus on CRITICAL/HIGH. Report MEDIUM/LOW as advisory.
3. Collision scan: `search/related` for top target entities, max 3 related configs
4. Output format (MUST appear in every post-write response):
   ```
   ## Post-Write Review
   **Config Findings:** {CRITICAL/HIGH findings with fix suggestions, or "Clean — no issues found."}
   **Collision Scan:** {conflicts or "No conflicts detected."}
   **Advisory:** {MEDIUM/LOW findings, or omit if none}
   ```

## Adding a New Skill — Checklist

When creating a new skill under `skills/{name}/SKILL.md`:

1. Skill file follows Skill Section Template (see above)
2. `skills/ha-nova/SKILL.md` — add to Dispatch table + add disambiguation examples
3. `skills/ha-nova/SKILL.md` — add domain to Response Format if needed
4. `skills/review/SKILL.md` — add domain-specific checks if applicable (new check prefix)
5. `docs/reference/skill-architecture.md` — add to skill tree + add Architecture section
6. `docs/reference/skill-architecture.md` — add to Agent vs Inline table
7. `scripts/onboarding/install-local-skills.sh` — verify dynamic discovery picks up new skill
8. `README.md` / `PROJECT.md` — add skill to overview table/list
9. `version.json` — bump patch version
10. Run `bash scripts/dev-sync.sh` + start new session to test

## Review Check Single Source of Truth

`skills/review/SKILL.md` Step 1 is the authoritative source for all review checks (S/R/P/M/F/H).
Agent templates (`review-agent.md`) reference this file instead of duplicating checks.
When adding or modifying checks, update ONLY `review/SKILL.md` — agents read from there.

## Safety Baseline

Global safety expectations:
- no guessed ids
- preview before any write
- delete requires tokenized confirmation
- structured failure output: what failed / why / next step
- diagnostics only after real capability failure
- claim-evidence binding: verify data-target match before presenting conclusions (see context skill)

## Operational Goal

Minimize context and maintenance overhead while preserving strict write safety:
- flat skill layout for direct discovery
- context skill auto-loaded via hook
- centralized relay contract
- explicit phase boundaries
- deterministic preview/confirm/apply behavior
