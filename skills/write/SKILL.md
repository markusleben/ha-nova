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

```bash
~/.config/ha-nova/relay health
```

If this fails, run onboarding: `npm run onboarding:macos`.

## Flow

### Phase 1: Resolve (Agent)

1. Read `skills/ha-nova/agents/resolve-agent.md`.
2. Fill template placeholders (domain, operation, user intent).
3. Dispatch general-purpose agent. Extract: entities, target_id, target_exists, current_config, bp_status.
4. On ambiguity: ask user. On no-match: ask for exact entity_id.
5. ID generation for `create`:
   - **Automations:** Use a Unix timestamp (e.g., `1709550000000`) as config ID. HA auto-assigns numeric IDs; any unique number works.
   - **Scripts:** Use a descriptive slug (e.g., `morning_routine`). This becomes `script.morning_routine`.

### Phase 2: Preview + Confirm (Main Thread)

1. Build config. For update: full-replacement merge (base=current, overlay=user changes).
2. BP gate: fresh->continue, stale+simple->warn, stale+complex->block until refresh.
   Load `best-practices.md` only if gate evaluation needed.
3. Render preview with structured summary + full YAML:
   ```
   **{Automation|Script}: {alias}**
   - **ID:** {id}
   - **Entities:** {all entity_ids in triggers/conditions/actions}
   - **Triggers:** {short description}
   - **Conditions:** {short description or "none"}
   - **Actions:** {short description}
   - **Mode:** {single|restart|queued|parallel}
   ```
   Then show the full YAML config that will be written:
   ```yaml
   alias: ...
   triggers: ...
   actions: ...
   ```
4. Confirmation: create/update=natural, delete=tokenized `confirm:<token>`.

### Phase 3: Apply + Verify (Agent)

1. Read `skills/ha-nova/agents/apply-agent.md`.
2. Fill template with confirmed payload.
3. Dispatch general-purpose agent. Expect: success, write_status, verification.
4. Report user-facing result. No raw curl/JSON in output.

Fallback: If agent dispatch unavailable, run same logic inline serially.

### Phase 4: Post-Write Review (MANDATORY)

Do NOT report results to the user until this phase is complete. Run inline (do NOT invoke ha-nova:review as a separate skill).

Follow the Post-Write Review Standard from `docs/reference/skill-architecture.md`:

1. Re-read the written config (resolve `unique_id` first — see `relay-api.md` → ID Types):
   ```bash
   relay ws -d '{"type":"config/entity_registry/get","entity_id":"automation.<slug>"}' | jq -r '.data.unique_id'
   relay core -d '{"method":"GET","path":"/api/config/automation/config/<unique_id>"}' | jq '.data.body'
   ```
   - Script: use `"entity_id":"script.<slug>"` and `/api/config/script/config/<unique_id>`
2. Read `skills/review/SKILL.md` Step 1 for the full check catalog. Apply domain-appropriate checks:
   - Automations: S-01..S-03, R-01..R-15, P-01..P-04, M-01..M-04
   - Scripts: all automation checks plus F-01..F-08
   - If actions reference helpers (input_boolean, input_number, counter, timer, etc.): also run H-01..H-08 on those helpers
3. Run collision scan: `search/related` for the top 3 target entities, read max 3 related configs.
4. Your response MUST include this section (missing = incomplete write):
   ```
   ## Post-Write Review
   **Config Findings:** {CRITICAL/HIGH findings with fix suggestions, or "Clean — no issues found."}
   **Collision Scan:** {conflicts or "No conflicts detected."}
   **Advisory:** {MEDIUM/LOW findings, or omit if none}
   ```
5. Findings are advisory — the write already succeeded. User can choose to update.

## Output Format

See `skills/ha-nova/SKILL.md` Response Format section. Automations and scripts use structured summary + YAML.

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
- Resolve Agent: `skills/ha-nova/agents/resolve-agent.md`
- Apply Agent: `skills/ha-nova/agents/apply-agent.md`
- Review Checks: see `skills/review/SKILL.md` for full check catalog (S/R/P/M/F/H)
- Post-Write Review: see `docs/reference/skill-architecture.md` Post-Write Review Standard
