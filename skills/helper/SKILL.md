---
name: helper
description: Use when creating, updating, deleting, or listing Home Assistant helpers (storage-based helpers plus the supported config-entry helper family) through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Helper

## Scope

`ha-nova:helper` has two helper families:

- **Storage-based family** — full CRUD for:
  - `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`, `input_button`, `counter`, `timer`, `schedule`
- **Config-entry family** — CRUD support for 10 domains:
  - `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`
  - `group` note: handled through the live menu-driven flow; end-to-end support is verified for the `sensor` subtype, and other subtypes must stay anchored to the live step schema instead of guessed fields
  - `template` note: same menu-driven flow (17 entity types); end-to-end support is verified for the `sensor` subtype. The `state` field is a Jinja template — author it per `skills/ha-nova/template-guidelines.md` and apply the template-level reliability checks (missing `float`/`int` defaults, boolean-string comparisons) BEFORE submitting; a broken template renders the entity `unavailable`, so post-write verification must read the rendered state, not just entry existence

Not handled here:

- other config-entry helper families:
  - `trend`, `random`, `filter`, `generic_thermostat`, `switch_as_x`, `generic_hygrostat`
- `local_todo` list config entries (use `ha-nova:todo`)
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
- **Config-entry family:** WS `config_entries/get` + WS entity-registry joins for list/read; relay `/core` config-entry flow, options-flow, and delete writes

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
   - If sensible defaults can be inferred: render them as the Suggestion Block (output-rules.md), max 4 as numbered list. Group related fields into one item.
     ```
     💡 Suggested defaults for "{name}" ({type}):
     1. min: 16, max: 30, step: 0.5
     2. unit_of_measurement: "°C"
     3. mode: slider
     4. icon: mdi:thermometer
     Accept all, pick by number (e.g. "1 and 3"), or "skip".
     ```
   - User accepts all, picks by number, or says "skip".
   - Accepted → merge into payload BEFORE preview.
   - No useful defaults inferable → silently skip.
3. Preview the payload with the shared write-preview shape: compact summary, pre-write check when applicable, explicit not-saved-yet line, and Options block (`apply`, `show yaml`, `cancel`).
4. Ask for natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation).
   - for unobserved `group` or `template` subtypes, this first confirmation authorizes only the non-persisting menu-step submit, not the final subtype-specific payload
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
3. Preview current vs proposed in the Changes slot with `ha-nova diff` (see `skills/ha-nova/write-safety.md` → Pre-Write Diff). Then run a pre-write impact check — `search/related` on this helper entity (max 3 related configs) — and surface affected automations/scripts as an advisory. Advisory only; never block. Include an explicit not-saved-yet line and Options block (`apply`, `show yaml`, `cancel`).
4. Ask for natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation).
5. Execute:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```
6. Verify by re-reading the same list item.
7. Run storage-family post-write review (see below).
8. Update-Revert: after the verified update, capture the snapshot and offer `revert` — see `skills/ha-nova/write-safety.md` → Update-Revert. Storage-family restore rebuilds a schema-valid `{type}/update` from `before_config` (typed `{type}_id` from its `id` + writable fields only — never the raw list item, which lacks `{type}_id` and carries read-only `id`/`entity_id`); `expected_after` is the post-update read-back.

#### Deleting a helper

1. Resolve target from `{type}/list`; extract its `id` (the `{type}_id` used for the delete call — internal, not shown to the user).
2. Preview with stable localized slots (see `skills/ha-nova/write-safety.md` → Output hygiene — no raw internal id):
   - name
   - type
   - entity_id
3. Token confirmation: `confirm:<token>` (strict: only exact token accepted; see context skill → Safety Baseline).
4. Execute:
   ```text
   ha-nova relay ws --data-file <payload-file>
   ```
5. Verify absence from `{type}/list`.

### Family 2: Config-entry helpers

Canonical config-entry helper item:

- `entry_id`
- `domain`
- `title`
- `state`
- `linked_entities[]`
- `supports_options`

`entry_id` is the canonical identity for config-entry helper writes.
If the user gives only a linked `entity_id`, resolve it back to `config_entry_id` through the full entity registry before continuing.

#### Supported domains

- `utility_meter`
- `derivative`
- `integration`
- `min_max`
- `threshold`
- `tod`
- `statistics`
- `group`
- `history_stats`
- `template`

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
3. Filter config entries to the ten supported domains.
4. Join linked entities by matching `config_entry_id`.
5. Present the List Frame table (output-rules.md): title, domain, `entry_id`,
   state. Options-flow support and linked entities stay in the per-helper
   detail read; when several entries share a domain or similar titles, add a
   short disambiguation line under the table with each candidate's linked
   entities (compact comma-separated summary).

#### Keyword search

Search against:

- config-entry `title`
- `domain`
- linked `entity_id`
- linked original/display names when available

If multiple matches remain, present max 5 candidates and ask one blocking question.

#### Reading a single helper

1. Resolve by one of:
   - `entry_id`
   - config-entry title
   - linked `entity_id`
   - if multiple candidates remain after resolution, stop and ask one blocking question
   - never guess between duplicate titles or ambiguous linked-entity matches
2. Re-read `config_entries/get`.
3. Re-read full entity registry and attach `linked_entities[]`.
4. If `supports_options: true`, start an options flow:
   ```text
   ha-nova relay core --method POST --path /api/config/config_entries/options/flow --body-file <start-payload-file>
   ```
   with `<start-payload-file>` containing `{"handler":"<entry_id>","show_advanced_options":false}`.
5. Treat the returned current step as the current editable options snapshot:
   - record `step_id`
   - summarize each exposed field from `description.suggested_value` when present
   - if an exposed field has no `description.suggested_value`, mark its value as unavailable instead of guessing
   - ignore hidden fields that are not exposed in the current step
6. If the options flow is unavailable even though the domain is supported:
   - still show canonical metadata
   - mark update as unsupported on this HA version
7. Present:

```text
**Helper: {title}** (config-entry `{domain}`)
- **Entry ID:** {entry_id}
- **Config-entry state:** {state}
- **Linked entities:** {linked_entities summary}
- **Supports options-flow editing:** {yes/no}
- **Current flow step:** {step_id or "metadata-only fallback"}
- **Current editable fields:** {field summary from the current options step when available}
```

#### Creating a helper

1. Confirm the requested domain is supported in `skills/ha-nova/helper-flow-schemas.md`.
   - treat that file as observed field inventory, not a full validation schema
   - if required field semantics remain uncertain, fail loud and ask one blocking question
2. Prepare the full create plan using `skills/ha-nova/helper-flow-schemas.md`:
   - for one-step domains, the plan is one submit body
   - for `group` or `template` with subtype `sensor`, include the required `next_step_id` menu choice and the observed final form
   - for any other `group` or `template` subtype, plan only the menu choice before the flow starts; inspect the live subtype form before promising the final field set
   - for `statistics` and `history_stats`, prepare every later step body before preview
   - for `template`, resolve every entity ID referenced inside the `state` template before preview — check the entity registry, then fall back to `/api/states/<entity_id>` (the registry only tracks entities with a `unique_id`; YAML/manual entities are valid references but absent from it). Only a reference missing from BOTH is a blocking question, not a submit
3. Preview with the shared write-preview shape:
   - title/name
   - domain
   - known step plan
   - all fields already known at this point
   - for unobserved `group` or `template` subtypes, say that the final subtype form will be previewed after the menu step returns live fields
   - include an explicit not-saved-yet line and Options block (`apply`, `show yaml`, `cancel`)
4. Ask for natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation).
5. Capture a pre-create baseline:
   ```text
   ha-nova relay ws --data-file <entries-request-file> --out <entries-before-file>
   ```
   with `<entries-request-file>` containing `{"type":"config_entries/get"}`.
6. Start the flow:
   ```text
   ha-nova relay core --method POST --path /api/config/config_entries/flow --body-file <start-payload-file>
   ```
   `<start-payload-file>` must contain the handler-start body only.
7. Read the start response and extract `flow_id` before continuing.
   - persist it in a variable or note file
   - fail loud if the start response did not return `flow_id`
8. Iterate the flow until terminal success:
   - if the current response is a menu step, submit only the selected `next_step_id`
   - if that menu step leads to an unobserved `group` or `template` subtype form, stop and preview the live subtype fields before the terminal submit
   - after that live subtype preview, ask for a second natural confirmation before sending the terminal subtype-specific payload
   - if the current response is a form step, submit only the fields exposed for that step
   - the submit body for a form step must contain form fields only
   - if a required field is still unresolved and there is no safe value, stop and ask one blocking question
   - if HA returns a form with validation errors, fail loud instead of guessing
9. Verify success at the config-entry layer first:
   - re-read `config_entries/get` into `<entries-after-file>`
   - if the terminal flow result includes `entry_id`, `passed=true` only when that same `entry_id` is present in `<entries-after-file>`
   - if the terminal flow result omits `entry_id`, diff `config_entries/get` before vs after by `entry_id`
   - in the diff fallback, collect the new `entry_id` values that were absent before and present after
   - in the diff fallback, `passed=true` only when exactly one new `entry_id` appeared and its metadata is consistent with the requested create
   - if the diff fallback yields zero or multiple new `entry_id` values, or the new entry metadata is inconsistent with the request, fail loud as ambiguous create verification
   - `domain`/`title` are fallback tie-breakers only; they never override a terminal-flow `entry_id`
10. Resolve `linked_entities[]` through the entity registry as secondary evidence only.
11. If the created entry exposes `supports_options: true`, reopen the options flow and store the current editable options snapshot for the post-write response.
12. Run config-entry-family post-write review (see below).

#### Updating a helper

1. Resolve the canonical config-entry helper item:
   - `entry_id`
   - `domain`
   - `title`
   - `linked_entities[]`
   - `supports_options`
   - if multiple candidates remain after resolution, stop and ask one blocking question
   - never guess between duplicate titles or ambiguous linked-entity matches
2. If `supports_options` is false for this entry:
   - fail loud with `update unsupported for this helper on this HA version`
   - do not recreate
   - do not guess a direct patch body
3. Start the options flow:
   ```text
   ha-nova relay core --method POST --path /api/config/config_entries/options/flow --body-file <start-payload-file>
   ```
   with `<start-payload-file>` containing `{"handler":"<entry_id>","show_advanced_options":false}`.
4. Read the start response and extract `flow_id` before continuing.
   - if the start response explicitly shows that options editing is unsupported for this entry on this HA version, fail loud with `update unsupported for this helper on this HA version`
   - if the start call fails for another reason, surface that relay/HA error directly instead of relabeling it as unsupported
   - persist it in a variable or note file
   - fail loud if the start response did not return `flow_id`
5. Capture the current editable options snapshot from the returned form:
   - use `description.suggested_value` as the current value source
   - if a requested field is exposed but lacks `description.suggested_value`, fail loud instead of guessing its current value
   - do not submit read-only fields
   - treat the current step as the authoritative mutable field set
6. Build the update body by merging requested changes over the current options snapshot:
   - carry forward unchanged required fields
   - if the user requests a field the current step does not expose, fail loud as unsupported update for that field on this HA version
   - do not silently ignore non-exposed requested fields
   - do not invent values for fields the current step does not expose
   - for `history_stats`, preserve HA's two-key window invariant across `start`, `end`, and `duration`
   - for `history_stats`, if the requested change switches to a different valid window pair, drop the old third key explicitly so the submit body still contains exactly two of `start`, `end`, and `duration`
   - for `template`, when the change touches the `state` template, resolve every entity ID referenced in the NEW template before submitting (entity registry, then `/api/states/<entity_id>` fallback — YAML/manual entities are absent from the registry but valid) — a typo like `is_state('binary_sensor.typo','on')` renders a clean boolean, so the post-write rendered-state read cannot catch it; a reference missing from both is a blocking question, not a submit
7. Preview current vs proposed in the Changes slot with `ha-nova diff` (see `skills/ha-nova/write-safety.md` → Pre-Write Diff). Include an explicit not-saved-yet line and Options block (`apply`, `show yaml`, `cancel`).
8. Ask for natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation).
9. Submit the current step:
   ```text
   ha-nova relay core --method POST --path /api/config/config_entries/options/flow/{flow_id} --body-file <submit-payload-file>
   ```
10. If HA returns another form step, repeat the same merge-and-submit rule until terminal `create_entry` or explicit failure.
11. Verify success:
   - re-read `config_entries/get`
   - `passed=true` only when the same `entry_id` still exists
   - reopen the options flow
   - `passed=true` only when the changed fields now appear in `description.suggested_value` as requested
   - if a requested changed field is exposed in the verification step but lacks `description.suggested_value`, fail loud as unverifiable update on this HA version
12. Resolve `linked_entities[]` again as secondary evidence only.
13. Run config-entry-family post-write review (see below).

#### Deleting a helper

1. Resolve target to the canonical config-entry helper item:
   - `entry_id`
   - `domain`
   - `title`
   - `linked_entities[]` when available
   - if multiple candidates remain after resolution, stop and ask one blocking question
   - never guess between duplicate titles or ambiguous linked-entity matches
2. Enforce the helper-domain allowlist before any delete:
   - allowed here: `utility_meter`, `derivative`, `integration`, `min_max`, `threshold`, `tod`, `statistics`, `group`, `history_stats`, `template`
   - if the resolved `domain` is outside that allowlist, stop
   - do not call `DELETE /api/config/config_entries/entry/{entry_id}` for out-of-scope domains
   - hand off to `ha-nova:fallback` for any other config-entry domain
3. Preview with stable localized slots:
   - title
   - domain
   - linked entities if known
   - `entry_id` only when needed to disambiguate duplicate titles/domains
   - explicit not-deleted-yet line before the confirmation token
4. Run a pre-delete dependency check:
   - if linked entities are known, run `search/related` against up to 3 linked entities before confirmation
   - summarize any related automations/scripts in the preview
   - if linked entities are unknown, say that dependency check coverage is limited
5. Token confirmation: `confirm:<token>` (strict exact-token rule). This still applies to cleanup and helpers created earlier in the same session. Multi-helper deletes within ONE family follow `skills/ha-nova/batch-safety.md`; storage and config-entry families never mix.
6. Execute:
   ```text
   ha-nova relay core --method DELETE --path /api/config/config_entries/entry/{entry_id}
   ```
7. Verify success at the config-entry layer first:
   - re-read `config_entries/get`
   - `passed=true` only when the `entry_id` is absent
8. Entity disappearance is secondary evidence only — do not fail the delete just because registry/state cleanup lags.
9. Run config-entry-family post-write review (see below).

### Post-write review (MANDATORY)

Do NOT report results to user until complete.

#### Storage-based family

1. Apply `skills/review/checks.md` → Application (storage family: H-01..H-10).
2. Apply H-01..H-08 directly to the written helper config.
3. Only evaluate H-09/H-10 if the collision scan finds a referencing automation/script with a direct helper-backed threshold and you also read live helper state per `skills/review/checks.md`.
4. Collision scan: `search/related` for the helper entity, max 3 related automations/scripts.

#### Config-entry family

Do not pretend H-01..H-10 apply here.
Instead, run the minimal config-entry post-write contract:

1. **Verification**
   - create: config entry now exists and the requested `entry_id`/diff verification passed
   - update: the same `entry_id` still exists and the reopened options-flow snapshot reflects the requested field changes
   - for `template` creates and `state`-changing updates, additionally read the linked entity via `GET /api/states/<entity_id>`. A clean numeric/string render confirms the template works. Treat `unavailable`/`unknown` as INCONCLUSIVE, not proof of breakage — a source entity can be legitimately `unknown`, or the template may intentionally return a sentinel; only call it a template defect when the options-flow template or an HA error proves a failure. Either way the config-entry write itself still counts as passed
   - delete: config entry is absent
2. **Current editable snapshot**
   - if an options flow is available, summarize only the editable fields exposed by the final current step readback
3. **Linked entities**
   - read linked entities from entity registry when available
   - treat them as secondary evidence only
4. **Collision check**
   - if linked entities were found, run `search/related` against up to 3 linked entities
5. **Advisory**
   - say that storage-helper H-01..H-10 checks do not apply to this family
   - config-entry updates are not auto-revertible (options-flow writes are multi-step); for undo, point the user to Home Assistant Backups

Report only what has substance (same rule as the write flow — see `skills/write/SKILL.md` Phase 4): keep **Verification** (and the editable snapshot when present), but omit an empty **Collision check** or **Advisory** — never an empty "none" bucket. When the write is clean, the verification plus a single scope-honest confirmation line suffices (`skills/ha-nova/write-safety.md` → Verification Honesty). Multi-target logical changes: plan first per write-safety → Multi-Target Changes.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output. Write previews, delete prompts, and results render as the Cards defined there. The bold labels in both read templates below are semantic slots — localize them at runtime, never print them as literal English headings.

### Storage-based family

After reading a helper config, present:

```text
**Helper: {name}** ({type})
- **Entity ID:** {entity_id}
- **Unique ID:** {id}
- **Icon:** {icon}
- {type-specific fields}
```

For list operations, render the List Frame (output-rules.md):

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
- **Supports options-flow editing:** {yes/no}
- **Current flow step:** {step_id or "metadata-only fallback"}
- **Current editable fields:** {field summary from the current options step when available}
```

For list operations, render the List Frame (output-rules.md; options-flow support and linked entities stay in the per-helper detail read above):

```text
| Title | Domain | Entry ID | State |
|-------|--------|----------|-------|
```

Never show raw JSON to the user.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- No guessing entity IDs, linked entities, or config entry IDs; resolve or ask
- `entry_id` is the canonical write identity for the config-entry family
- Destructive cleanup still requires `confirm:<token>`, even for helpers created earlier in the same session.
- Every write MUST end with the Post-Write Review slot; use terminal-friendly labels where Markdown headings add noise.

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
- Review Checks: `skills/review/checks.md` (self-contained catalog + Application)
- On demand: `skills/ha-nova/update-revert.md` — when the user asks to revert, undo, or restore a verified update
