---
name: ha-nova-helper
description: Use when creating, updating, deleting, or listing Home Assistant helpers (input_boolean, input_number, input_text, input_select, input_datetime, input_button, counter, timer, schedule) through HA NOVA Relay.
---

# HA NOVA Helper


## Scope

CRUD for storage-based helpers:
- types: `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`, `input_button`, `counter`, `timer`, `schedule`
- operations: `list`, `read`, `create`, `update`, `delete`

Excluded: config-entry flow helpers (template, group, utility_meter, etc.) — these require a multi-step config flow, not WS CRUD.

No config mutations on automations/scripts (use `ha-nova:ha-nova-write` for those).

## Bootstrap (once per session)

Verify relay CLI: `~/.config/ha-nova/relay health`
If this fails: `npm run onboarding:macos`

## Flow

### Listing helpers

Use the compact entity registry (abbreviated keys: `ei`=entity_id, `en`=name, `ai`=area_id):

```bash
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
  | jq '[.data.entities[] | select(.ei | test("^(input_boolean|input_number|input_text|input_select|input_datetime|input_button|counter|timer|schedule)\\.")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:30]'
```

If user filters by type, narrow the `test()` regex to that single domain.

### Keyword search

```bash
~/.config/ha-nova/relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
  | jq '[.data.entities[] | select(.ei | test("^(input_boolean|input_number|input_text|input_select|input_datetime|input_button|counter|timer|schedule)\\.")) | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:20]'
```

If 0 results: try synonyms or shorter stems. Never dump entire domains.

### Reading a single helper

1. Determine type from entity_id domain prefix.
2. Fetch full config via type-specific list:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/list"}' | jq '[.data[] | select(.name | test("KEYWORD";"i"))]'
   ```
3. No single-item read endpoint — always `{type}/list` + filter.

### Creating a helper

1. Validate intent against `skills/ha-nova/helper-schemas.md` for required/optional fields.
2. Use-case defaults (create only, skip on update/delete):
   - Infer use-case from helper name + type using general HA knowledge.
   - Consult `skills/ha-nova/helper-schemas.md` → Suggested Defaults for principles and field name reminders.
   - If sensible defaults can be inferred: show max 4 as numbered list. Group related fields into one item (e.g. min/max/step together).
     ```
     Suggested defaults for "{name}" ({type}):
     1. min: 16, max: 30, step: 0.5
     2. unit_of_measurement: "°C"
     3. mode: slider
     4. icon: mdi:thermometer
     Accept all, pick by number (e.g. "1 and 3"), or "skip".
     ```
   - User accepts all, picks by number, or says "skip".
   - Accepted → merge into payload BEFORE preview.
   - No useful defaults inferable → silently skip.
3. Preview the payload:
   ```
   **Create Helper: {name}** ({type})
   - {all fields being set}
   ```
   ```yaml
   # WS payload
   type: {type}/create
   name: ...
   ```
4. Ask for natural confirmation.
5. Execute:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/create","name":"...","...":"..."}'
   ```
6. Verify — list back and confirm new item exists:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/list"}' | jq '[.data[] | select(.name == "{name}")]'
   ```
7. No domain reload needed — immediate effect.
8. Run post-write review (see below).

### Updating a helper

1. Resolve target:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/list"}' | jq '.data[]'
   ```
   Match by `name` or `id`. If multiple matches: present candidates (max 5), ask one question.
2. Extract `id` field from list response (this is the `{type}_id` for the update command).
3. Preview: show current vs proposed values.
4. Ask for natural confirmation.
5. Execute:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/update","{type}_id":"{id}","...":"..."}'
   ```
6. Verify — list back and confirm fields match:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/list"}' | jq '[.data[] | select(.id == "{id}")]'
   ```
7. Run post-write review (see below).

### Deleting a helper

1. Resolve target (same as update step 1-2).
2. Preview:
   ```
   **Delete Helper: {name}** ({type})
   - **ID:** {id}
   ```
3. Token confirmation: `confirm:<token>` (strict: only exact token accepted, see context skill → Safety Baseline).
4. Execute:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/delete","{type}_id":"{id}"}'
   ```
5. Verify — confirm item is absent:
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"{type}/list"}' | jq '[.data[] | select(.id == "{id}")]'
   ```
   `passed=true` only when result is empty.

### Post-write review (MANDATORY)

Do NOT report results to user until complete. Run after every create/update/delete:

1. Enter via `skills/ha-nova-review/SKILL.md` Step 1. Apply H-01..H-08 directly to the written helper config. Only evaluate H-09/H-10 if the collision scan finds a referencing automation/script with a direct helper-backed threshold and you also read live helper state per `skills/ha-nova-review/checks.md`.
2. Collision scan: `search/related` for helper entity, check referencing automations/scripts (max 3).
   ```bash
   ~/.config/ha-nova/relay ws -d '{"type":"search/related","item_type":"entity","item_id":"{entity_id}"}'
   ```
3. Response MUST include a Post-Write Review section with localized headings (see `skills/ha-nova/SKILL.md` → Output Localization):
   - **Findings**: 🔴🟠🟡 findings with descriptive titles + fix suggestions, or localized "no issues found"
   - **Collision check**: referencing automations/scripts, or localized "no references found"
   - **Advisory**: 🟠🟡 findings, or omit if none

Findings are advisory — write already succeeded. User can choose to update.

## Output Format

After reading a helper config, present a structured summary:

```
**Helper: {name}** ({type})
- **Entity ID:** {entity_id}
- **Unique ID:** {id}
- **Icon:** {icon}
- {type-specific fields (min/max, options, duration, etc.)}
```

For list operations, use a compact table:

```
| Entity ID | Name | Type | Area |
|-----------|------|------|------|
```

Never show raw JSON to the user.

## Safety

- Preview before every write
- No guessing entity_ids or unique_ids; resolve or ask
- Delete requires tokenized confirmation
- All HA communication through `~/.config/ha-nova/relay` only
- Every write MUST end with `## Post-Write Review`. Skipping it is a skill violation.

## Guardrails

- Limit list results to 30
- Max 5 candidates on ambiguity
- Max 3 related configs in collision scan
- Never use raw `get_states`

## References

- Relay API: `skills/ha-nova/relay-api.md` (see "Helper CRUD" section)
- Helper Schemas: `skills/ha-nova/helper-schemas.md`
- Review Checks: `skills/ha-nova-review/SKILL.md` (entrypoint) + `skills/ha-nova-review/checks.md` (H-01..H-10 catalog)
