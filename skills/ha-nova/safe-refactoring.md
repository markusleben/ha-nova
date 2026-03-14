# HA NOVA Safe Refactoring

Workflows for safely renaming, deleting, and cleaning up entities, automations, scripts, and helpers.

## Pre-Delete Impact Check

Before deleting any automation, script, or helper, check what depends on it.

### Step 1: Find Consumers

Use `search/related` to find all automations, scripts, and scenes that reference the entity:

```bash
ha-nova relay ws -d '{"type":"search/related","item_type":"entity","item_id":"<entity_id>"}'
```

Parse the response for `automation`, `script`, and `scene` entries in the related items.

### Step 2: Assess Impact

- **No consumers found:** Safe to delete.
- **Consumers found:** List them for the user with their aliases. Example:

  ```
  This entity is used by:
  - automation.morning_routine ("Morning Routine") — action: turns on this light
  - script.welcome_home ("Welcome Home") — action: sets brightness

  Deleting it will break these configs. Options:
  1. Update the consumers first, then delete
  2. Disable instead of delete (reversible)
  3. Cancel
  ```

### Step 3: Disable vs Delete

Prefer disabling over deleting when the user is unsure:

```bash
# Disable entity (reversible)
ha-nova relay ws -d '{"type":"config/entity_registry/update","entity_id":"<entity_id>","disabled_by":"user"}'

# Re-enable later
ha-nova relay ws -d '{"type":"config/entity_registry/update","entity_id":"<entity_id>","disabled_by":null}'
```

Only proceed with permanent deletion after tokenized confirmation (see write skill → Phase 2).

## Entity Rename Workflow

Renaming an entity can break automations and scripts that reference the old ID.

### Step 1: Find All References

```bash
ha-nova relay ws -d '{"type":"search/related","item_type":"entity","item_id":"<old_entity_id>"}'
```

### Step 2: Show Impact

List all configs that reference this entity. For each, note whether the reference is in:
- Triggers (entity_id in trigger config)
- Conditions (entity_id or template referencing entity)
- Actions (target entity_id or template)

Template references (`states('old.entity_id')`, `is_state(...)`) are NOT auto-updated by HA — they will break silently.

### Step 3: Rename

```bash
ha-nova relay ws -d '{"type":"config/entity_registry/update","entity_id":"<old_entity_id>","new_entity_id":"<new_entity_id>"}'
```

### Step 4: Update Consumers

For each consumer automation/script found in Step 1:
1. Read the config
2. Replace all occurrences of the old entity_id (in entity_id fields AND in templates)
3. Preview the updated config to the user
4. Apply via write skill flow (Phase 2+3)

### Step 5: Verify

Re-run `search/related` on the NEW entity_id to confirm all references are updated. Check that no configs still reference the old ID.

## Orphan Cleanup

Find and clean up unused helpers. Uses review check H-07.

### Find Orphaned Helpers

For each helper entity, run:

```bash
ha-nova relay ws -d '{"type":"search/related","item_type":"entity","item_id":"input_boolean.<id>"}'
```

If no automations or scripts reference it, it's an orphan candidate.

### Bulk Discovery

1. List all helpers:
   ```bash
   ha-nova relay ws -d '{"type":"config/entity_registry/list_for_display"}' \
     | ha-nova relay jq -r '.data.entities[] | select(.ei | test("^(input_boolean|input_number|input_select|input_text|input_datetime|input_button|counter|timer|schedule)\\.")) | .ei'
   ```
2. For each, run `search/related` and collect those with zero automation/script references.
3. Present the list to the user — some "orphans" may be intentionally UI-only (e.g., dashboard controls).

### Cleanup

For confirmed orphans, delete via the helper skill flow (tokenized confirmation required).

## Safety Rules

- Always run consumer check before any rename or delete
- Keep rename/delete work limited to the requested target and its directly affected consumers.
- Do not rewrite, rename, disable, or delete unrelated configs while fixing references.
- Template references (`states('...')`, `is_state('...')`) are NOT auto-updated — must be fixed manually
- Prefer disable over delete when impact is unclear
- Orphan detection is advisory — some helpers are intentionally UI-only
- All deletes require tokenized confirmation (`confirm:<token>`)
- A delete is not done until follow-up verification confirms the target is gone.
- Do not present a destructive change as complete when consumer impact is still unresolved.
