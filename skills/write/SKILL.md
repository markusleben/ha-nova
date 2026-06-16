---
name: write
description: Use when creating, updating, or deleting Home Assistant automations or scripts through HA NOVA Relay. Self-contained — resolves entities and reviews internally.
---

# HA NOVA Write


## Scope

Mutations only:
- domains: `automation`, `script`
- operations: `create`, `update`, `delete`

## Bootstrap (once per session)

Verify relay CLI:

```text
ha-nova relay health
```

If this fails, run onboarding: `ha-nova setup`.

## Flow

### Phase 1: Resolve (Agent)

1. Read `skills/ha-nova/agents/resolve-agent.md`.
2. Fill template placeholders.
3. Dispatch agent. Extract `target_id`, `current_config`, `bp_status`, `suggested_enhancements`.
   - update/delete: resolve `entity_id -> unique_id` via registry first
4. On ambiguity/no-match: use a single blocking question for the exact entity_id when needed. Do not add a second ambiguity question. If the requested change depends on an invalid Home Assistant premise, correct it before continuing.
5. `create` IDs: automations=Unix timestamp, scripts=slug.

### Phase 2: Preview + Confirm (Main Thread)

1. Build config. For update: full-replacement merge (base=current, overlay=user changes).
   - Do not rewrite unrelated structure, aliases, or formatting for a narrow requested change.
2. BP gate (internal — see `skills/ha-nova/write-safety.md`): fresh/stale+simple->continue, stale+complex->block.
3. Suggestions + Pre-Write Checks (skip for `delete`):
   - **3a) Suggestions**: Show `suggested_enhancements` from resolve-agent (max 4, numbered; present as a menu where the client supports one). User accepts by number or "skip" → merge accepted into config BEFORE preview. Skip when `SUGGESTED_ENHANCEMENTS: none` or already present on `update`.
   - **3b) Static Checks**: Use `skills/review/SKILL.md` Step 1 plus `skills/review/checks.md`. Run S/R/P/M checks analytically on the draft YAML — no relay calls (scripts: F-01..F-08; helper references: H-01..H-08. Defer H-09/H-10 to Phase 4 for live evidence).
     Use exactly one explicit pre-write verdict line before apply:
     - clean draft → localized equivalent of "Pre-write check: no issues worth flagging before save."
     - any flagged draft → localized equivalent of "Pre-write check: this draft may not behave as intended."
     🔴 findings → inline warning + fix. 🟠🟡 findings → advisory below preview. Keep wording code-free.
     If R-18 matches, warn that a REST/UI write can break dependent variables in that block. Advisory only: do not block the write and do not require extra confirmation.
     After an R-18 warning, tell the user to inspect traces after the next real run. Do not auto-trigger the config or auto-read traces here.
     If R-19 matches, warn with: final else branch is only reached when the earlier entity-state branches are false. Move the `trigger.id` check into an explicit `elif`. Or refactor to `choose` + `condition: trigger`. Advisory only: do not block the write and do not require extra confirmation.
     Track findings by check type for dedup in Phase 4, except for the R-18 follow-up below.
   - **3c) Pre-Write Impact (update only)**: run the `review/` Step 2 `search/related` scan at preview; show affected automations/scripts as advisory (never block). Skip `create`/`delete`. Phase 4's scan still runs.
4. Preview (see `skills/ha-nova/write-safety.md` for the fixed shape):
   - update: **run** `ha-nova diff`, print its stdout **verbatim** as `## Changes` — never write it yourself (see `skills/ha-nova/write-safety.md`). create: compact summary. Offer `show yaml`.
   - Delete preview MUST include the consumer-check result before confirmation: either the affected consumers or an explicit no-consumer result.
5. Confirmation: create/update=natural, delete=tokenized `confirm:<token>` (exact token only; see context skill → Safety Baseline + Interactive Choices). create/update may use a menu; **delete is the typed token, never a menu**.

### Phase 3: Apply + Verify (Agent)

1. Read `skills/ha-nova/agents/apply-agent.md`.
2. Fill template with confirmed payload.
3. Dispatch agent. Expect: success, write_status, verification.
4. Report user-facing result. No raw curl/JSON in output.
   - Do not report destructive success until verification proves the target is gone.

Fallback: If agent dispatch unavailable, execute inline.

### Phase 4: Post-Write Review (MANDATORY)

Do NOT invoke `ha-nova:review` separately.

1. Re-read by `target_id` (do NOT re-resolve by slug):
   - automation: `ha-nova relay core --method GET --path /api/config/automation/config/<target_id> --jq-file <filter-file> --out <result-file>`
   - script: `/api/config/script/config/<target_id>`
   - `<filter-file>`:
     ```jq
     if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
     ```
   - for create/update, reload the domain, resolve the actual `entity_id` from entity registry by matching `unique_id == <target_id>`, then read `/api/states/{entity_id}` to confirm runtime presence
   - if the actual `entity_id` differs, report it and point to `skills/ha-nova/safe-refactoring.md`; do not silently assume the requested slug won
2. S/R/P/M/F checks (narrowed):
   - Compare read-back vs draft on core fields; ignore metadata (`id`,`unique_id`,`created_at`,`modified_at`,`editor`,`enabled`).
   - HA may normalize keys during write (`trigger`→`triggers`, `action`→`actions`, `condition`→`conditions`). Account for plural aliasing when comparing — these are not real diffs.
   - Core fields differ (beyond aliasing) → full checks from `review/SKILL.md` Step 1. If they match, skip the normal subset as "covered in pre-write review," but still re-run the storage-sensitive R-18 subset against the persisted read-back config.
   - **Dedup**: findings from Phase 2 Step 3b that the user saw MUST NOT repeat. Track by check type, not code.
    - Exception: if R-18 still matches on the persisted read-back config, report it again as a persisted runtime risk.
    - `R-19` follows normal dedup: if already shown pre-write, do not repeat unless it becomes a new finding category.
    - If persisted R-18 remains, the next step is to inspect traces after the next real run. Do not auto-trigger or auto-read traces.
    - If actions reference helpers: always run H-01..H-10.
3. Collision scan: `{"type":"search/related","item_type":"entity","item_id":"<entity_id>"}` via `ha-nova relay ws --data-file <payload-file>`; read max 3 related configs.
4. Post-Write Review output (localized; see `skills/ha-nova/SKILL.md` → Output Localization) — report only what has substance; the scans still run, only their empty output is suppressed:
   - **Findings**: real issues only. **Collision check**: only when related items exist (list them + the verdict). **Advisory**: only when non-empty. Omit any section with nothing to report — never print an empty "none" bucket.
   - If nothing is worth reporting, collapse to one localized confirmation line (e.g. "Verified — no issues or conflicts").
   - Never emit `Questions to consider`, `Suggestions`, or `Instant help` post-write; never repeat an item across **Findings** and **Advisory**.
5. Update-Revert (update only): after a verified update, **run `ha-nova snapshot save`** and offer `revert`; on revert use `ha-nova snapshot show/verify`, never from memory (see `skills/ha-nova/write-safety.md`). `create`/`delete` → HA Backups.

## Output Format

See `skills/ha-nova/SKILL.md` → Response Format.

## Safety

- Preview before every write
- No guessing entity_ids; resolve or ask
- Delete requires tokenized confirmation
- Agents must use Relay only; no MCP, no direct HA API
- Every write MUST end with a `## Post-Write Review` section. Skipping it is a skill violation.

## Guardrails

- Never use raw `get_states` — use targeted registry/config reads
- Max 3 related configs in collision scan

## References

- Refs: `skills/ha-nova/relay-api.md`, `skills/ha-nova/payload-schemas.md`, `skills/ha-nova/best-practices.md`, `skills/ha-nova/automation-patterns.md`, `skills/ha-nova/template-guidelines.md`, `skills/ha-nova/safe-refactoring.md`, `skills/ha-nova/write-safety.md`
- Agent refs: `skills/ha-nova/agents/resolve-agent.md`, `skills/ha-nova/agents/apply-agent.md`
- Review refs: `skills/review/SKILL.md`, `skills/review/checks.md`, `docs/reference/skill-architecture.md`
