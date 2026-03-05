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

### Phase 4: Post-Write Review

Run a lightweight review inline (do NOT invoke ha-nova:review as a separate skill):

1. Re-read the written config: `relay ws -d '{"type":"automation/config","entity_id":"<id>"}'`
   - Script: `relay core -d '{"method":"GET","path":"/api/config/script/config/<key>"}'`
2. Read `skills/review/SKILL.md` Step 1 (Config Quality Review) for the full R-01..R-15 check definitions. Check the written config against these — focus on CRITICAL/HIGH only.
3. Run collision scan: `search/related` for the top 2 target entities, read max 3 related configs.
4. Present findings to user:
   - CRITICAL/HIGH findings: highlight prominently, suggest fixes
   - MEDIUM/LOW findings: mention as advisory
   - No findings: "Config looks clean."
5. Findings are advisory — the write already succeeded. User can choose to update.

## Safety

- Preview before every write
- No guessing entity_ids; resolve or ask
- Delete requires tokenized confirmation
- Agents must use Relay only; no MCP, no direct HA API

## References

- Relay API: `skills/ha-nova/relay-api.md`
- Payload Schemas: `skills/ha-nova/payload-schemas.md`
- Best Practices: `skills/ha-nova/best-practices.md`
- Resolve Agent: `skills/ha-nova/agents/resolve-agent.md`
- Apply Agent: `skills/ha-nova/agents/apply-agent.md`
- Review Checks: see `skills/review/SKILL.md` for full R-01..R-15 check catalog
