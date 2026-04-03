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
2. BP gate: fresh->continue, stale+simple->warn, stale+complex->block.
3. Suggestions + Pre-Write Checks (skip for `delete`):
   - **3a) Suggestions**: Show `suggested_enhancements` from resolve-agent (max 4, numbered). User accepts by number or "skip" → merge accepted into config BEFORE preview. Skip when `SUGGESTED_ENHANCEMENTS: none` or already present on `update`.
   - **3b) Static Checks**: Use `skills/review/SKILL.md` Step 1 plus `skills/review/checks.md`. Run S/R/P/M checks analytically on the draft YAML — no relay calls (scripts: F-01..F-08; helper references: H-01..H-08. Defer H-09/H-10 to Phase 4 for live evidence).
     Use exactly one explicit pre-write verdict line before apply:
     - clean draft → localized equivalent of "Pre-write check: no issues worth flagging before save."
     - any flagged draft → localized equivalent of "Pre-write check: this draft may not behave as intended."
     🔴 findings → inline warning + fix. 🟠🟡 findings → advisory below preview. Keep wording code-free.
     If R-18 matches, warn that a REST/UI write can break dependent variables in that block. Advisory only: do not block the write and do not require extra confirmation.
     After an R-18 warning, tell the user to inspect traces after the next real run. Do not auto-trigger the config or auto-read traces here.
     If R-19 matches, warn with: final else branch is only reached when the earlier entity-state branches are false. Move the `trigger.id` check into an explicit `elif`. Or refactor to `choose` + `condition: trigger`. Advisory only: do not block the write and do not require extra confirmation.
     Track findings by check type for dedup in Phase 4, except for the R-18 follow-up below.
4. Preview: summary + full YAML config.
   - Delete preview MUST include the consumer-check result before confirmation: either the affected consumers or an explicit no-consumer result.
5. Confirmation: create/update=natural, delete=tokenized `confirm:<token>` (exact token only; see context skill → Safety Baseline).

### Phase 3: Apply + Verify (Agent)

1. Read `skills/ha-nova/agents/apply-agent.md`.
2. Fill template with confirmed payload.
3. Dispatch agent. Expect: success, write_status, verification.
4. Report user-facing result. No raw curl/JSON in output.
   - Do not report destructive success until verification proves the target is gone.

Fallback: If agent dispatch unavailable, execute inline.

### Phase 4: Post-Write Review (MANDATORY)

Do NOT invoke `ha-nova:review` separately.

1. Re-read config using the `target_id` (do NOT re-resolve by slug):
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
    - `R-19` follows normal dedup. If the user already saw the pre-write warning, do not repeat it after the write unless it becomes a new distinct finding category.
    - If persisted R-18 remains, add a manual next step to inspect traces after the next real run. Do not auto-trigger or auto-read traces.
    - If actions reference helpers: always run H-01..H-10.
3. Run collision scan: create `<payload-file>` with `{"type":"search/related","item_type":"entity","item_id":"<entity_id>"}`, run `ha-nova relay ws --data-file <payload-file>`, read max 3 related configs.
4. Response MUST include localized Post-Write Review headings (see `skills/ha-nova/SKILL.md` → Output Localization):
   - **Findings**
   - **Collision check**
   - **Advisory**
   - if the collision scan found no related items, use the localized equivalent of "No related items found."
   - if related items were checked and no collision risk remains, use the localized equivalent of "No conflicts found."
   - if **Advisory** is empty, use the localized equivalent of "No additional advisories."
   - Do not emit `Questions to consider`, `Suggestions`, or `Instant help` in post-write mode.
   - Do not repeat the same advisory item in both **Findings** and **Advisory**.
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

- Refs: `skills/ha-nova/relay-api.md`, `skills/ha-nova/payload-schemas.md`, `skills/ha-nova/best-practices.md`, `skills/ha-nova/automation-patterns.md`, `skills/ha-nova/template-guidelines.md`, `skills/ha-nova/safe-refactoring.md`
- Agent refs: `skills/ha-nova/agents/resolve-agent.md`, `skills/ha-nova/agents/apply-agent.md`
- Review refs: `skills/review/SKILL.md`, `skills/review/checks.md`, `docs/reference/skill-architecture.md`
