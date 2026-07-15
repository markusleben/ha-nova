# HA NOVA Relay API Contract

Single source of truth for Relay calls used by HA NOVA skills.

## Auth & Transport

Transport, auth headers, and the base URL are handled internally by the `ha-nova relay` CLI wrapper — the relay token lives in the OS credential store. Never set, request, export, or ask the user for `RELAY_AUTH_TOKEN` or `RELAY_BASE_URL`. If a relay call fails, run `ha-nova relay health`; if that fails, have the user run `ha-nova setup`.

Underlying HTTP contract (reference only, not for direct use): `Authorization: Bearer <relay token>` and `Content-Type: application/json` against the relay base URL.

## Bounded Event Collection (envelope)

Window mode (`on_limit`) and binary responses are supported by every relay at or above the skills' enforced floor — `min_relay_version` in `version.json`, currently **Relay 0.4.0**. The CLI checks that floor on every relay call and prints a relay-outdated warning when the installed relay is older; surface that warning and offer the update instead of version-gating manually. (Below the floor, `on_limit` is silently ignored and a timeout fails the call instead of returning partial events.)


Some WS commands answer with events instead of a single response. Wrap them:

```json
{
  "message": { "type": "system_health/info" },
  "collect_events": { "until_type": "finish", "max_events": 100, "timeout_ms": 10000 }
}
```

The events come back as `.data.events`.

`on_limit` controls what happens when `max_events` or `timeout_ms` is hit first:
- omit it (or `"error"`): the call fails — use this when a finish event is expected.
- `"on_limit": "return"`: window mode — the relay returns what it saw and sets `.data.truncated: true`. Use this to sniff a stream that never finishes (for example `mqtt/subscribe`).

Subscription commands are permitted **only inside this envelope** (the relay unsubscribes and bounds the window). A bare subscription without the envelope is rejected with `UNSUPPORTED_WS_TYPE`.

## Binary Responses

`/core` returns binary upstream bodies (camera frames, downloads) base64-encoded with a marker:

```json
{ "ok": true, "data": { "status": 200, "body": "<base64>", "body_encoding": "base64", "content_type": "image/jpeg" } }
```

Write the raw bytes with `ha-nova relay core --method GET --path <path> --out-binary <file>` — it decodes the marker for you and refuses a body that is not marked binary (use `--out` for those). Binary bodies have a smaller ceiling (8 MiB) than text/JSON responses. JSON and text (including `/api/error_log`) keep their existing shape.

## Endpoints

- `GET /health`
- `POST /ws`
- `POST /core`

## Relay CLI Wrapper

For agent-dispatched flows, use the CLI wrapper instead of raw curl:

1. Write request JSON with the client's native file-writing tool.
   - POSIX heredocs are examples only; on Windows/PowerShell use the native file-writing equivalent while preserving the same JSON and jq file contents.
   - Write the final request body directly. Do not create placeholder payload templates such as `REPLACE_ENTITY_ID` and patch them later with `perl -0pi`, `sed -i`, or similar in-place rewrite commands.
2. Use file-based relay flags as the default contract:
   - `ha-nova relay ws --data-file <payload-file>`
   - `ha-nova relay core --method <METHOD> --path <PATH> --body-file <payload-file>`
   - `ha-nova relay ... --out <result-file>`
   - `ha-nova relay ... --jq-file <filter-file>` for non-trivial filters
   - `ha-nova relay ... --jq .field` only for short selectors without shell-special characters
3. Use client-private scratch storage outside the project workspace for payload/result files. Do not allocate scratch directories or files from visible shell commands just to hold relay JSON. Scratch files are internal execution artifacts; do not create them under the repo working tree and do not present them as user-facing edits. If command text is visible to the user, set the tool working directory to the scratch directory outside the command text, then run relay commands with local filenames (`payload.json`, `result.json`, `filter.jq`) instead of absolute scratch paths.

`ha-nova relay jq` is a small single-input JSON filter, not full GNU jq CLI compatibility. Supported flags are `-r`, `-e`, `-c` (accepted as compact-output compatibility; JSON output is already compact), `--file <input-file>`, and `--jq-file <filter-file>`. Do not pass other GNU jq flags. Do not use jq `input`/`inputs`, `input_filename`, or multi-file programs; compare two saved JSON files with the client's native JSON parser instead.

Examples:

```text
ha-nova relay ws --data-file <payload-file> --jq-file <filter-file>
ha-nova relay core --method GET --path /api/config/automation/config/<id> --out <result-file>
ha-nova relay core --method POST --path /api/services/light/turn_on --body-file <payload-file>
```

The wrapper handles auth (OS credential store), headers, timeouts, and base URL internally.
Inline `--body` is not supported for WebSocket relay calls; WS request bodies MUST use `--data-file`.
Inline `--body` may be used only for tiny `ha-nova relay core` diagnostics when quoting is already known-good; it is not the canonical cross-platform path.
Relay API examples are not write authorization. Any live write still needs the owning skill's active-preview confirmation flow before execution.

## Standard Envelope

- Success: `{ "ok": true, "data": ... }`
- Error: `{ "ok": false, "error": { "code": "...", "message": "..." } }`

Parsing varies by endpoint:
- `/ws` responses: upstream payload is in `.data` directly; the exact shape depends on the WS message type
- `/core` responses: upstream payload is in `.data.body` (with `.data.status` for HTTP status)

## ID Types & Resolution

HA uses different identifiers depending on the API. Skills MUST use the correct type.

| ID Type | Example | Used By |
|---------|---------|---------|
| `entity_id` | `automation.main_lights` | Entity registry, states, `search/related`, service calls |
| `unique_id` (config key) | `1766434159701` (UI) or `motion_main` (YAML) | REST config API, `trace/list`, `trace/get` |

**The entity_id slug and the unique_id are NOT the same** for UI-created items. HA generates a numeric `unique_id` for items created through the UI. YAML-defined items typically use the slug as both.

### Standard Resolution: entity_id → unique_id

When you have an entity_id and need the config key for REST or trace APIs:

If the exact automation/script `entity_id` is known, use `config/entity_registry/get` directly. Do not call `config/entity_registry/list` just to find one known entity. Use `config/entity_registry/list_for_display` only for search or disambiguation by name.

Create `<payload-file>` with:

```json
{"type":"config/entity_registry/get","entity_id":"automation.{slug}"}
```

Then run:

```text
ha-nova relay ws --data-file <payload-file> --out <registry-file>
ha-nova relay jq -r --file <registry-file> '.data.unique_id'
```

The jq filter quoting above is a POSIX example. On Windows/PowerShell pass the same filter with native argument quoting.

Use the resolved `unique_id` with:
- Config reads: `GET /api/config/automation/config/{unique_id}`
- Config writes/deletes: `POST|DELETE /api/config/automation/config/{unique_id}`
- Trace list: `trace/list` with `"item_id":"{unique_id}"`
- Trace get: `trace/get` with `"item_id":"{unique_id}"`

**Do NOT use the entity_id slug** as the authoritative id for existing-item config reads, writes, deletes, or traces.

## Post-Write Verification (automation/script)

For create/update verification:
1. read back config via `GET /api/config/{domain}/config/{unique_id}`
2. reload the domain service
3. query entity registry and match `unique_id` to the actual `entity_id`
4. read `/api/states/{entity_id}` to confirm runtime presence

If the actual `entity_id` differs from expectation, surface the real slug and offer a rename/refactor follow-up instead of silently assuming the requested slug won.

## /ws Contract

Request examples:
- `{ "type": "ping" }`
- `{ "type": "config/entity_registry/list_for_display" }` (compact entity listing; preferred over `get_states`)
- `{ "type": "get_states" }` (full state dump; avoid for listings — use only for single-entity state when needed)

Expected success body:
- `ok=true`
- Compact entity registry (`config/entity_registry/list_for_display`): `data.entities[]` with abbreviated keys (`ei`=entity_id, `en`=name, `ai`=area_id)
- Full entity registry (`config/entity_registry/list`): `data[]`
- Area registry (`config/area_registry/list`): `data[]` with canonical `area_id`; do not expect a generic `id`
- Recorder statistics (`recorder/statistics_during_period`): `data.<statistic_id>[]`
- `search/related` for `item_type:"area"`: `data` is a keyed object such as `automation[]`, `script[]`, `entity[]`, `device[]`
- `search/related` for `item_type:"entity"`: `data` is a related-item collection; filter by the requested target family before counting or follow-up reads
- `get_states`: `data` is an array of full state objects (thousands of entries — avoid for discovery)

Parsing rule:
- Compact entity registry: `.data.entities[]` — filter with jq `select(.ei | startswith("automation."))`.
- Full entity registry: `(.data // [])[]`.
- `search/related` area projection:
  - automation shortlist -> `(.data.automation // [])[]`
  - script shortlist -> `(.data.script // [])[]`
  - entity shortlist -> `(.data.entity // [])[]`
  - `.data.entity` is only a fallback seed when automation/script arrays are absent
- `get_states`: treat as `(.data // [])[]`, filter only object entries with string `entity_id`.

## /core Contract

Request envelope:

```json
{
  "method": "GET|POST|DELETE",
  "path": "/api/...",
  "body": {}
}
```

Response envelope:

```json
{
  "ok": true,
  "data": {
    "status": 200,
    "body": {}
  }
}
```

Parsing rule:
- Success flag: `.ok`
- Upstream status: `.data.status`
- Upstream payload: `.data.body`
- Preferred config-body jq file: `skills/ha-nova/config-body-filter.jq`

## Frequent HA API Paths

Automation config:
- `GET /api/config/automation/config/{id}`
- `POST /api/config/automation/config/{id}`
- `DELETE /api/config/automation/config/{id}`

Script config:
- `GET /api/config/script/config/{id}`
- `POST /api/config/script/config/{id}`
- `DELETE /api/config/script/config/{id}`

State/config helpers:
- `GET /api/states`
- `GET /api/states/{entity_id}`
- `POST /api/services/automation/reload`
- `POST /api/services/script/reload`

Dashboard / Lovelace WS:
- `lovelace/dashboards/list`
- `lovelace/dashboards/create`
- `lovelace/dashboards/update`
- `lovelace/dashboards/delete`
- `lovelace/config`
- `lovelace/config/save`
- `lovelace/resources`
- `lovelace/resources/create`
- `lovelace/resources/update`
- `lovelace/resources/delete`

Recorder statistics WS:
- `recorder/statistics_during_period`

## Helper CRUD (via /ws)

Supported types: `input_boolean`, `input_number`, `input_text`, `input_select`,
`input_datetime`, `input_button`, `counter`, `timer`, `schedule`

```
List:   {"type": "{type}/list"}
Create: {"type": "{type}/create", "name": "...", ...type-specific}
Update: {"type": "{type}/update", "{type}_id": "...", ...fields}
Delete: {"type": "{type}/delete", "{type}_id": "..."}
```

Important: `{type}_id` is the internal `id` from the list response, NOT the entity_id.

CLI examples:
```text
ha-nova relay ws --data-file <payload-file>
ha-nova relay ws --data-file <payload-file> --out <result-file>
```

No domain reload needed — storage-based helpers take effect immediately.

See `skills/ha-nova/helper-schemas.md` for type-specific fields and constraints.

## Config-Entry Helpers

Supported helper domains:

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

`group` is menu-driven; the live-proven end-to-end subtype is `sensor`, and other subtypes must stay anchored to the live step schema instead of guessed fields.

Canonical identity: `entry_id`

List/read source:

```json
{"type":"config_entries/get"}
{"type":"config/entity_registry/list"}
```

Current editable options readback:

```json
{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"<entry_id>","show_advanced_options":false}}
```

Create flow:

```json
{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"min_max"}}
{"method":"POST","path":"/api/config/config_entries/flow/{flow_id}","body":{"name":"..."}}
```

Use separate payload files for those two calls:

- flow start payload = handler-start body only
- capture `flow_id` from the flow-start response before calling `/flow/{flow_id}`
- flow submit payload = step form fields only

Update flow:

```json
{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"<entry_id>","show_advanced_options":false}}
{"method":"POST","path":"/api/config/config_entries/options/flow/{flow_id}","body":{"field":"value"}}
```

Use the live current options step as the update merge base:

- read current field values from `description.suggested_value`
- if a requested field is exposed but lacks `description.suggested_value`, fail loud instead of guessing the current value
- carry forward unchanged required fields
- do not submit read-only fields
- if the entry exposes no options flow on the running HA version, fail loud as unsupported update

Delete:

```json
{"method":"DELETE","path":"/api/config/config_entries/entry/{entry_id}"}
```

Verification rules:

- create/delete success is decided at the config-entry layer first
- create success = `entry_id` from the terminal flow result confirmed in the after-read, or a constrained before/after `config_entries/get` diff by `entry_id` if the flow omits it
- the before/after fallback requires a pre-create `config_entries/get` baseline
- the before/after fallback passes only when exactly one new `entry_id` appeared and that new entry matches the requested domain/title
- if the fallback diff is empty, plural, or metadata-inconsistent, fail loud as ambiguous create verification
- update success = the same `entry_id` still exists in `config_entries/get` and a reopened options flow shows the requested changed editable fields in `description.suggested_value`
- if a requested changed field is exposed in the verification step but lacks `description.suggested_value`, fail loud as unverifiable update on this HA version
- use `config_entries/get` as the source of truth for identity/existence
- resolve linked entities from `config/entity_registry/list` by matching `config_entry_id`
- linked entity appearance/disappearance is secondary evidence only

Observed locally on a real HA instance on 2026-03-19:

- the original 9 domains completed real create/update/delete loops through relay `/core` (2026-03-19); `template` completed a live create/options-flow/delete loop on 2026-07-09
- raw WS `config_entries/flow` did not succeed in this session
- field-level update verification required reopening the options flow

See `skills/ha-nova/helper-flow-schemas.md` for the observed field sets and domain-specific notes.

## Integration Config Flows

`ha-nova:integration-setup` uses the generic config-flow surface:

```json
{"method":"GET","path":"/api/config/config_entries/flow_handlers"}
{"type":"manifest/list"}
{"type":"config_entries/flow/progress"}
{"type":"config_entries/get"}
{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"hue"}}
{"method":"GET","path":"/api/config/config_entries/flow/{flow_id}"}
{"method":"POST","path":"/api/config/config_entries/flow/{flow_id}","body":{"step_field":"value"}}
{"method":"DELETE","path":"/api/config/config_entries/flow/{flow_id}"}
```

Rules:

- available handler domains come from `flow_handlers`; join them to `manifest/list` by `domain` for display-name resolution and never guess between matches
- pending reauthentication comes from `config_entries/flow/progress` with `context.source == "reauth"` and a matching `context.entry_id`; never create a replacement reauth flow
- each response's `type`, `data_schema`, `menu_options`, `flow_id`, and `step_id` define the next action
- form submit bodies contain only fields exposed by the current live step
- `config_entries/flow/progress` omits flows whose `context.source` is `user`; a relay-started add flow cannot rely on appearing as an in-progress UI card
- a credential-bearing, external/OAuth, or progress step from a relay-started add flow is canceled and restarted in the HA UI; the Relay also does not provide the frontend-origin header used to construct OAuth redirects
- pre-existing reauth flows are preserved and continue through their matching Home Assistant UI card when one of those UI-only steps is reached
- add verification uses terminal `result.entry_id`, or a constrained before/after `config_entries/get` diff when it is absent
- successful reauth uses terminal abort reason `reauth_successful`, the same surviving `entry_id`, and absence of the completed pending flow

## Domain Payload Rules

Automation fields: `alias`, `triggers`, `conditions`, `actions`, `mode`
Script fields: `alias`, `sequence`, `mode`, `description`, `fields`, `variables`

Method: create/update = `POST`, delete = `DELETE`

## Service Calls (via /core)

List all available services:
```json
{"method":"GET","path":"/api/services"}
```

Call a service:
```json
{"method":"POST","path":"/api/services/light/turn_on","body":{"entity_id":"light.main_light","brightness":128}}
```

Call with response data:
```json
{"method":"POST","path":"/api/services/weather/get_forecasts?return_response","body":{"entity_id":"weather.home","type":"daily"}}
```

Supported target fields: `entity_id` (string or array), `area_id`, `device_id`.

## Runtime Events And Webhooks

Fire a custom event with an event-data JSON object:

```json
{"method":"POST","path":"/api/events/example_event","body":{"source":"ha_nova"}}
```

Inspect registered webhook metadata through WS without exposing the returned IDs:

```json
{"type":"webhook/list"}
```

Trigger a known JSON webhook only after the owning skill resolves the exact ID internally:

```json
{"method":"POST","path":"/api/webhook/<webhook_id>","body":{"example":"value"}}
```

Rules:

- `/api/events/{event_type}` requires an exact custom event name and an object body; a successful response proves bus acceptance, not listener completion
- automation webhook triggers default to POST/PUT and `local_only: true`; inspect the registered `allowed_methods` and locality before calling
- multiple automation triggers can share one webhook ID and all will run
- webhook IDs are authentication secrets; keep them in client-private scratch storage and out of previews, results, and logs
- Home Assistant intentionally answers unknown IDs, blocked non-local requests, and handler failures with HTTP 200; verify matched automation runs instead
- events and webhooks may already have fired when transport evidence is ambiguous; never retry automatically

## Calendar Event Writes

`ha-nova:calendar` creates events through the service API and updates/deletes them through WS:

```json
{"method":"POST","path":"/api/services/calendar/create_event","body":{"entity_id":"calendar.example","summary":"Event","start_date_time":"2026-07-15T14:00:00+02:00","end_date_time":"2026-07-15T15:00:00+02:00"}}
{"type":"calendar/event/update","entity_id":"calendar.example","uid":"<uid>","event":{"summary":"Event","dtstart":"2026-07-15T14:00:00+02:00","dtend":"2026-07-15T15:00:00+02:00"}}
{"type":"calendar/event/delete","entity_id":"calendar.example","uid":"<uid>"}
```

Rules:

- read the calendar entity state first; feature bits are create `1`, delete `2`, update `4`
- REST event reads return `uid` and optional `recurrence_id`; update/delete require exact identity
- update's `event` is a full replacement object with `summary`, `dtstart`, `dtend`, and any retained optional `description`, `location`, or `rrule`
- recurring instances add `recurrence_id` and `recurrence_range`: `""` for one occurrence, `THISANDFUTURE` for that occurrence and later ones
- the create service has no recurrence field; use the Home Assistant UI for recurring creation

## Registry Queries (via /ws)

List areas:
```json
{"type":"config/area_registry/list"}
```

List devices:
```json
{"type":"config/device_registry/list"}
```

List entity registry (includes area/device assignment):
```json
{"type":"config/entity_registry/list"}
```

## Trace Queries (via /ws)

**`item_id` must be the `unique_id` (config key), NOT the entity_id slug.** See [ID Types & Resolution](#id-types--resolution).

List traces for an automation:
```json
{"type":"trace/list","domain":"automation","item_id":"{unique_id}"}
```

Get detailed trace:
```json
{"type":"trace/get","domain":"automation","item_id":"{unique_id}","run_id":"{run_id}"}
```

Trace response includes: `trace.trigger`, `trace.condition`, `trace.action` nodes with `result`, `timestamp`, and `changed_variables`.

`run_id` must come from a `trace/list` item. Do not pass a list index as `run_id`. If no traces are returned, explain that Home Assistant keeps only recent traces and YAML automations/scripts need an `id` to expose traces.

## Error Handling

Relay errors and upstream HA errors arrive differently. Agents must distinguish them.

### Relay Errors (HTTP-level)

These are top-level HTTP errors produced by the relay itself. The response has `ok: false` and an HTTP status >= 400.

- `400 / INVALID_JSON`: request body is not valid JSON
- `400 / VALIDATION_ERROR`: missing required fields (e.g. ws `type`, core `method`/`path`)
- `400 / CORE_PATH_INVALID`: core API path failed validation
- `401 / UNAUTHORIZED`: relay auth token missing or invalid
- `404 / NOT_FOUND`: unknown relay route
- `500 / INTERNAL_ERROR`: unexpected relay server error
- `502 / UPSTREAM_WS_ERROR`: relay could not reach HA websocket
- `502 / UPSTREAM_WS_TIMEOUT`: WS request to HA timed out
- `502 / UPSTREAM_WS_COMMAND_ERROR`: HA answered the WS command with a structured error; the message contains HA's own error code and text (e.g. `HA rejected 'x': unknown_command: ...`). The connection stays healthy — treat this as a command problem, not a connectivity problem. Legacy relays below the enforced floor report these as generic `UPSTREAM_WS_ERROR`.
- `502 / UPSTREAM_HTTP_ERROR`: core HTTP request to HA failed
- `502 / UPSTREAM_HTTP_TIMEOUT`: core HTTP request to HA timed out

Check: HTTP status >= 400, or envelope `.ok == false`.

### Upstream HA Errors (inside success envelope)

When the relay successfully proxies to HA but HA itself returns an error, the response is `{ok: true, data: {status: <HA status>, body: ...}}`. Common upstream status codes:

- `409`: target state changed during write (conflict)
- `422`: HA rejected payload semantics (unprocessable entity)
- `404`: HA resource not found at the requested API path
- `405`: HA does not support the HTTP method for that path

Check: envelope `.ok == true`, then inspect `.data.status` for non-2xx values.

## Timeout and Retry Guidance

The CLI has hardcoded timeouts (not user-configurable):
- **Connect timeout:** 5 seconds
- **HTTP request timeout:** 15 seconds

The relay server has its own internal upstream timeout of 10 seconds per WS/HTTP request to HA.

On `502 / UPSTREAM_*_TIMEOUT` or CLI-level timeout:
- verify state/config first before retrying
- retry exactly once only when verification shows no state change
- if config read-back succeeded but reload timed out, treat it as partial verification and confirm registry/state before retrying

## Safe Bulk Patterns

For bulk inspection or review preparation:
1. save discovery output with `--out <result-file>`
2. keep JSON filtering in `--jq-file` or `ha-nova relay jq --file <result-file>`
3. iterate over the saved shortlist with native file/loop tools
4. follow selector semantics, stable ordering, and workset limits from `skills/ha-nova/bulk-patterns.md`

Do not rely on external `jq` pipes as the canonical path.

## `relay jq` Usage

- Usage: `ha-nova relay jq [--file <result-file>] [-e] [-r] [--jq-file <filter-file>] '<filter>'`
- The jq filter is positional unless `--jq-file` is used.
- The single-quoted positional filter shown here is a POSIX example. On Windows/PowerShell pass the same filter with native argument quoting.
- Do not invent a `--jq` flag for `ha-nova relay jq`; that flag belongs to `ha-nova relay ws` / `ha-nova relay core`.

## Runtime Env

- Use `ha-nova relay` for all HA communication (auth + base URL resolved inside the CLI).
- Bootstrap check: `ha-nova relay health`
