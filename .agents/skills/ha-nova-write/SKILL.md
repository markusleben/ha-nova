---
name: ha-nova-write
description: Use when creating, updating, or deleting Home Assistant automations or scripts through HA NOVA Relay.
---

# HA NOVA Write

<!-- ha-nova-managed-install repo_root: __HA_NOVA_REPO_ROOT__ -->

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
3. Render preview: Automation|Script, Entities, Behavior, Suggested Enhancements, Next Step.
4. Confirmation: create/update=natural, delete=tokenized `confirm:<token>`.

### Phase 3: Apply + Verify (Agent)

1. Read `skills/ha-nova/agents/apply-agent.md`.
2. Fill template with confirmed payload.
3. Dispatch general-purpose agent. Expect: success, write_status, verification.
4. Report user-facing result. No raw curl/JSON in output.

Fallback: If agent dispatch unavailable, run same logic inline serially.

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
