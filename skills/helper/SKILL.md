---
name: helper
description: Use when creating, updating, deleting, or listing Home Assistant helpers (storage-based helpers plus the supported config-entry helper family) through HA NOVA Relay.
---

# HA NOVA Helper

## Scope

`ha-nova:helper` has two helper families:

- **Storage-based family** — full CRUD for:
  - `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`, `input_button`, `counter`, `timer`, `schedule`
- **Config-entry family (PR1 foundation)** — list, metadata-read, create, delete for:
  - `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`

Config-entry family in this slice does **not** support update yet.
If the user requests update for one of those six domains, say so explicitly and point them to the HA UI for now.

Not handled here:

- config-entry multi-step helper domains still owned by `ha-nova:fallback`:
  - `group`, `statistics`, `history_stats`
- other config-entry helper families:
  - `template`, `trend`, `random`, `filter`, `generic_thermostat`, `switch_as_x`, `generic_hygrostat`
- automations/scripts config mutations (use `ha-nova:write`)

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Write payloads with the client's native file-writing tool, then use:

- `ha-nova relay ws --data-file <payload-file>`
- `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`
- `ha-nova relay ... --out <result-file>` for larger read/verify output
- `--jq-file <filter-file>` for non-trivial filters; keep inline `--jq` for short selectors only

Family-specific transport:

- **Storage-based family:** WS CRUD + WS list
- **Config-entry family:** WS `config_entries/get` + WS entity-registry joins for list/metadata-read; relay `/core` for create/delete writes

## Flow

### Family 1: Storage-based helpers

#### Listing helpers

Use the compact entity registry (abbreviated keys: `ei` = entity_id, `en` = name, `ai` = area_id).

Create `<payload-file>` with `{"type":"config/entity_registry/list_for_display"}`, then run:

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
```

Write `<filter-file>` with:

```jq
[.data.entities[] | (.ei | split(".")[0]) as $domain | select(["input_boolean","input_number","input_text","input_select","input_datetime","input_button","counter","timer","schedule"] | index($domain)) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:30]
```

If user filters by type, narrow the domain filter to that single storage-based domain.

#### Keyword search

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
```

Write `<filter-file>` with:

```jq
[.data.entities[] | (.ei | split(".")[0]) as $domain | select(["input_boolean","input_number","input_text","input_select","input_datetime","input_button","counter","timer","schedule"] | index($domain)) | select((.ei + " " + (.en // "")) | test("KEYWORD";"i")) | {entity_id: .ei, name: .en, area_id: .ai}] | .[0:20]
```

If 0 results: try synonyms or shorter stems. Never dump entire domains.

#### Reading a single helper

1. Determine type from entity_id domain prefix.
2. Fetch full config via type-specific list:
   ```text
   ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
   ```
   Write `<filter-file>` with:
   ```jq
   [.data[] | select(.name | test("KEYWORD";"i"))]
   ```
3. No single-item read endpoint — always `{type}/list` + filter.

#### Creating a helper

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
3. Preview the payload.
4. Ask for natural confirmation.
5. Execute:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```
6. Verify — list back and confirm new item exists.
7. No domain reload needed — immediate effect.
8. Run storage-family post-write review (see below).

#### Updating a helper

1. Resolve target from `{type}/list` by `name` or internal `id`.
2. Extract `id` from the list response (this is the `{type}_id` for the update command).
3. Preview current vs proposed values.
4. Ask for natural confirmation.
5. Execute:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```
6. Verify by re-reading the same list item.
7. Run storage-family post-write review (see below).

#### Deleting a helper

1. Resolve target from `{type}/list`.
2. Preview:
   - name
   - type
   - internal `id`
3. Token confirmation: `confirm:<token>` (strict: only exact token accepted; see context skill → Safety Baseline).
4. Execute:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```
5. Verify absence from `{type}/list`.

### Family 2: Config-entry helpers (PR1 foundation)

Canonical config-entry helper item:

- `entry_id`
- `domain`
- `title`
- `state`
- `linked_entities[]`

`entry_id` is the canonical identity for write operations.
If the user gives only a linked `entity_id`, resolve it back to `config_entry_id` through the full entity registry before continuing.

#### Supported domains in this slice

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`

#### Listing helpers

1. Read all config entries:
   ```text
   ha-nova relay ws --data-file <payload-file> --out <entries-file>
   ```
   with `{"type":"config_entries/get"}`.
2. Read full entity registry:
   ```text
   ha-nova relay ws --data-file <payload-file> --out <registry-file>
   ```
   with `{"type":"config/entity_registry/list"}`.
3. Filter config entries to the six supported domains.
4. Join linked entities by matching `config_entry_id`.
5. Present a compact table with:
   - title
   - domain
   - `entry_id`
   - state
   - linked entities (compact comma-separated summary)

#### Keyword search

Search against:

- config-entry `title`
- `domain`
- linked `entity_id`
- linked original/display names when available

If multiple matches remain, present max 5 candidates and ask one blocking question.

#### Reading a single helper

Read is metadata-only in this slice.
Do not claim full domain-specific config readback from this path.

1. Resolve by one of:
   - `entry_id`
   - config-entry title
   - linked `entity_id`
2. Re-read `config_entries/get`.
3. Re-read full entity registry and attach `linked_entities[]`.
4. Present the canonical item:

```text
**Helper: {title}** (config-entry `{domain}`)
- **Entry ID:** {entry_id}
- **Config-entry state:** {state}
- **Linked entities:** {linked_entities summary}
- **Read scope:** metadata only in this slice
- **Supports update:** not in this slice
```

#### Creating a helper

1. Confirm the requested domain is supported in `skills/ha-nova/helper-flow-schemas.md`.
   - treat that file as observed field inventory, not as a full validation schema
   - if required field semantics remain uncertain, fail loud and ask one blocking question
2. For this slice, all six supported create flows were observed locally as:
   - `step_id: user`
   - `last_step: true`
3. Prepare the final field set using the observed domain-specific field inventory in `skills/ha-nova/helper-flow-schemas.md`.
4. Preview:
   - title/name
   - domain
   - all submitted fields
5. Ask for natural confirmation.
6. Capture a pre-create baseline:
   ```text
   ha-nova relay ws --data-file <entries-request-file> --out <entries-before-file>
   ```
   with `<entries-request-file>` containing `{"type":"config_entries/get"}`.
7. Start the flow:
   ```text
   ha-nova relay core --method POST --path /api/config/config_entries/flow --body-file <start-payload-file>
   ```
   `<start-payload-file>` must contain the handler-start body only.
8. Read the start response and extract `flow_id` before continuing.
   - persist it in a variable or note file
   - fail loud if the start response did not return `flow_id`
9. Submit the single observed form step:
   ```text
   ha-nova relay core --method POST --path /api/config/config_entries/flow/{flow_id} --body-file <submit-payload-file>
   ```
   `<submit-payload-file>` must contain the form fields only.
10. Verify success at the config-entry layer first:
   - re-read `config_entries/get` into `<entries-after-file>`
   - if the terminal flow result includes `entry_id`, `passed=true` only when that same `entry_id` is present in `<entries-after-file>`
   - if the terminal flow result omits `entry_id`, diff `config_entries/get` before vs after by `entry_id`
   - in the diff fallback, collect the new `entry_id` values that were absent before and present after
   - in the diff fallback, `passed=true` only when exactly one new `entry_id` appeared and its metadata is consistent with the requested create
   - if the diff fallback yields zero or multiple new `entry_id` values, or the new entry metadata is inconsistent with the request, fail loud as ambiguous create verification
   - `domain`/`title` are fallback tie-breakers only; they never override a terminal-flow `entry_id`
11. Resolve `linked_entities[]` through the entity registry as secondary evidence only.
12. Run config-entry-family post-write review (see below).

#### Updating a helper

Not supported in this PR1 slice for the config-entry family.

If the user requests update for one of the six config-entry domains:

- say that config-entry helper update is not delivered in this slice
- do not guess an options-flow payload
- do not silently fall back to delete+create
- point the user to the HA UI for now

#### Deleting a helper

1. Resolve target to `entry_id`.
2. Preview:
   - title
   - domain
   - `entry_id`
   - linked entities if known
3. Token confirmation: `confirm:<token>` (strict exact-token rule).
4. Execute:
   ```text
   ha-nova relay core --method DELETE --path /api/config/config_entries/entry/{entry_id}
   ```
5. Verify success at the config-entry layer first:
   - re-read `config_entries/get`
   - `passed=true` only when the `entry_id` is absent
6. Entity disappearance is secondary evidence only — do not fail the delete just because registry/state cleanup lags.
7. Run config-entry-family post-write review (see below).

### Post-write review (MANDATORY)

Do NOT report results to user until complete.

#### Storage-based family

1. Enter via `skills/review/SKILL.md` Step 1.
2. Apply H-01..H-08 directly to the written helper config.
3. Only evaluate H-09/H-10 if the collision scan finds a referencing automation/script with a direct helper-backed threshold and you also read live helper state per `skills/review/checks.md`.
4. Collision scan: `search/related` for the helper entity, max 3 related automations/scripts.

#### Config-entry family

Do not pretend H-01..H-10 apply here.
Instead, run the minimal config-entry post-write contract:

1. **Verification**
   - create: config entry now exists
   - delete: config entry is absent
2. **Linked entities**
   - read linked entities from entity registry when available
   - treat them as secondary evidence only
3. **Collision check**
   - if linked entities were found, run `search/related` against up to 3 linked entities
4. **Advisory**
   - say that storage-helper H-01..H-10 checks do not apply to this family in this slice

Response MUST still include a localized Post-Write Review section with:

- **Findings**
- **Collision check**
- **Advisory**

## Output Format

### Storage-based family

After reading a helper config, present:

```text
**Helper: {name}** ({type})
- **Entity ID:** {entity_id}
- **Unique ID:** {id}
- **Icon:** {icon}
- {type-specific fields}
```

For list operations, use:

```text
| Entity ID | Name | Type | Area |
|-----------|------|------|------|
```

### Config-entry family

After reading a helper config, present:

```text
**Helper: {title}** (config-entry `{domain}`)
- **Entry ID:** {entry_id}
- **Config-entry state:** {state}
- **Linked entities:** {linked_entities}
- **Read scope:** metadata only in this slice
```

For list operations, use:

```text
| Title | Domain | Entry ID | State | Linked Entities |
|-------|--------|----------|-------|-----------------|
```

Never show raw JSON to the user.

## Safety

- Preview before every write
- No guessing entity IDs, linked entities, or config entry IDs; resolve or ask
- `entry_id` is the canonical write identity for the config-entry family
- Delete requires tokenized confirmation
- All HA communication through `ha-nova relay` only
- Every write MUST end with `## Post-Write Review`

## Guardrails

- Limit list results to 30
- Max 5 candidates on ambiguity
- Max 3 related configs in collision scan
- Never use raw `get_states`
- For config-entry helpers, success/failure is config-entry-first, not entity-first

## References

- Relay API: `skills/ha-nova/relay-api.md`
- Storage helper schemas: `skills/ha-nova/helper-schemas.md`
- Config-entry helper schemas: `skills/ha-nova/helper-flow-schemas.md`
- Review Checks: `skills/review/SKILL.md` (entrypoint) + `skills/review/checks.md` (storage helper catalog)
