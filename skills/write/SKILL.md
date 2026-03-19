---
name: write
description: Use when creating, updating, or deleting Home Assistant automations or scripts through HA NOVA Relay. Self-contained — resolves entities and reviews internally.
---

# HA NOVA Write


## Scope

Mutations only:
- domains: `automation`, `script`
- operations: `create`, `update`, `delete`

Not for helpers — use `ha-nova:helper` for helper CRUD (different API: WS instead of REST).

## Bootstrap (once per session)

Verify relay CLI is available:

```text
ha-nova relay health
```

If this fails, run onboarding: `ha-nova setup`.

## Flow

### Phase 1: Resolve (Agent)

1. Read `skills/ha-nova/agents/resolve-agent.md`.
2. Fill template placeholders (domain, operation, user intent).
3. Dispatch general-purpose agent. Extract: entities, target_id, target_exists, current_config, bp_status, suggested_enhancements.
   - update/delete: resolve `entity_id -> unique_id` via registry first
   - slug is naming convenience only
4. On ambiguity: ask user. On no-match: ask for exact entity_id.
   - If broad targeting is ambiguous, reuse the existing single blocking question.
   - Do not add a second ambiguity question in the same turn.
   - If the requested change depends on an invalid Home Assistant premise, correct the premise explicitly before continuing.
5. ID generation for `create`: automations=Unix timestamp, scripts=descriptive slug (`morning_routine` → `script.morning_routine`).

### Phase 2: Preview + Confirm (Main Thread)

1. Build config. For update: full-replacement merge (base=current, overlay=user changes).
   - Do not rewrite unrelated structure, aliases, or formatting for a narrow requested change.
2. BP gate: fresh->continue, stale+simple->warn, stale+complex->block until refresh.
   Load `best-practices.md` only if gate evaluation needed.
3. Suggestions + Pre-Write Checks (skip for `delete`):
   - **3a) Suggestions**: Show `suggested_enhancements` from resolve-agent (max 4, numbered). User accepts by number (all, partial like "1 and 3", or "skip") → merge accepted into config BEFORE preview.
     Skip when: `SUGGESTED_ENHANCEMENTS: none`, or already present on `update`.
   - **3b) Static Checks**: Enter via `skills/review/SKILL.md` Step 1 and load the detailed rules from `skills/review/checks.md`. Run S/R/P/M checks analytically on the draft YAML — no relay calls needed (scripts: also F-01..F-08; if actions reference helpers: also H-01..H-08. Defer H-09/H-10 to Phase 4 because they require live helper evidence).
     🔴 findings → inline warning with fix suggestion. 🟠🟡 findings → advisory below preview. Clean → skip.
     Track findings by check type for dedup in Phase 4.
4. Preview: structured summary (alias, ID, entities, triggers, conditions, actions, mode) + full YAML config.
   - Delete preview MUST include the consumer-check result before confirmation: either the affected consumers or an explicit no-consumer result.
5. Confirmation: create/update=natural, delete=tokenized `confirm:<token>` (strict: only exact token accepted, see context skill → Safety Baseline).

### Phase 3: Apply + Verify (Agent)

1. Read `skills/ha-nova/agents/apply-agent.md`.
2. Fill template with confirmed payload.
3. Dispatch general-purpose agent. Expect: success, write_status, verification.
4. Report user-facing result. No raw curl/JSON in output.
   - Do not report destructive success until verification proves the target is gone.

Fallback: If agent dispatch unavailable, execute inline serially and include domain reload.

### Phase 4: Post-Write Review (MANDATORY)

Do NOT report results to the user until this phase is complete. Run inline (do NOT invoke `ha-nova:review` as a separate skill).

Follow the Post-Write Review Standard from `docs/reference/skill-architecture.md`:

1. Re-read the written config using the `target_id` from Phase 1 (do NOT re-resolve by slug):
   - automation: `ha-nova relay core --method GET --path /api/config/automation/config/<target_id> --jq-file <filter-file> --out <result-file>`
   - Script: `/api/config/script/config/<target_id>`
   - write `<filter-file>` with:
     ```jq
     if .ok then .data.body else error("relay error: \(.error.message // "unknown")") end
     ```
   - use relay jq for counts and follow-up checks
   - for create/update, reload the domain, resolve the actual `entity_id` from entity registry by matching `unique_id == <target_id>`, then read `/api/states/{entity_id}` to confirm runtime presence
   - if the actual `entity_id` differs from expectation, report it and point to `skills/ha-nova/safe-refactoring.md`; do not silently assume the requested slug won
2. S/R/P/M/F checks (narrowed):
   - Compare read-back vs draft on core fields (automations: `alias`,`triggers`,`conditions`,`actions`,`mode`,`description`; scripts: `alias`,`sequence`,`mode`,`description`,`variables`,`fields`). Ignore metadata (`id`,`unique_id`,`created_at`,`modified_at`,`editor`,`enabled`).
   - Note: HA may normalize keys during write (`trigger`→`triggers`, `action`→`actions`, `condition`→`conditions`). Account for plural aliasing when comparing — these are not real diffs.
   - Core fields differ (beyond aliasing) → full checks from `review/SKILL.md` Step 1. Match → skip: "covered in pre-write review."
   - **Dedup**: findings from Phase 2 Step 3b that user saw MUST NOT repeat. Track by check type (not code — codes are internal), e.g. if "mode not explicit" was shown pre-write and user proceeded, do not report it again.
   - If actions reference helpers: always run H-01..H-10.
3. Run collision scan:
   - create `<payload-file>` with `{"type":"search/related","item_type":"entity","item_id":"<entity_id>"}`
   - run `ha-nova relay ws --data-file <payload-file>`
   - read max 3 related configs.
4. Response MUST include a Post-Write Review section with localized headings (see `skills/ha-nova/SKILL.md` → Output Localization):
   - **Findings**: 🔴🟠🟡 findings with descriptive titles + fix suggestions, or localized "no issues found"
   - **Collision check**: conflicts with related automations/scripts, or localized "no conflicts"
   - **Advisory**: 🟠🟡 findings, or omit if none
5. Findings are advisory — write already succeeded.

## Output Format

see `skills/ha-nova/SKILL.md` → Response Format. Automations and scripts use structured summary + YAML.

## Safety

- Preview before every write
- No guessing entity_ids; resolve or ask
- Delete requires tokenized confirmation
- Agents must use Relay only; no MCP, no direct HA API
- Every write MUST end with a `## Post-Write Review` section. Skipping it is a skill violation.

## Guardrails

- Never use raw `get_states` — use targeted registry/config reads
- Max 3 related configs in collision scan
- No agent dispatch for helper CRUD (use `ha-nova:helper` instead)

## References

- Relay API: `skills/ha-nova/relay-api.md`
- Payload Schemas: `skills/ha-nova/payload-schemas.md`
- Helper Schemas: `skills/ha-nova/helper-schemas.md` (for helper field constraints when referenced in actions)
- Best Practices: `skills/ha-nova/best-practices.md`
- Automation Patterns: `skills/ha-nova/automation-patterns.md` (native HA constructs, action flow control, targeting)
- Template Guidelines: `skills/ha-nova/template-guidelines.md` (when to use templates vs native primitives)
- Safe Refactoring: `skills/ha-nova/safe-refactoring.md` (pre-delete impact check, entity rename workflow)
- Resolve Agent: `skills/ha-nova/agents/resolve-agent.md`
- Apply Agent: `skills/ha-nova/agents/apply-agent.md`
- Review Checks: `skills/review/SKILL.md` (entrypoint) + `skills/review/checks.md` (full check catalog)
- Post-Write Review: see `docs/reference/skill-architecture.md` Post-Write Review Standard
