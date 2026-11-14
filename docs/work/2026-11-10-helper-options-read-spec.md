# The tier engine reads a UI thermostat helper's heater

Status: active

Date: 2026-11-10. Issue MAR-131. Owner: tier engine (`cli/tier_callset.go`, new `cli/tier_helper_options.go`), relay session (`cli/relay_session.go`). Target: 0.30 (release/0.30). High-risk: a new Home Assistant dialog read in the typed-tier path.

## Today

A call's service-call set (MAR-127, `cli/tier_callset.go`) decides which `call_service` listeners count for a plan. Two rules make it unknown, so every such listener counts:

- `callingPlatforms`: a call that reaches a `generic_thermostat` or `generic_hygrostat` entity.
- `reactors`: a home where either integration is loaded (`/api/config` components), because they call services on their own when their sensor changes.

So in a home with one generic thermostat, every plan counts every service-call listener. That is cause 1 of the typed light call in the 0.29 real-home run (cause 2, an unattributed listener, stays).

Both helpers call exactly two services, on one entity stored in their options (Home Assistant 2026.11.1):

- `generic_thermostat/climate.py:681,710`: `homeassistant.turn_on` / `turn_off` on `heater`.
- `generic_hygrostat/humidifier.py:579,584`: the same on `humidifier`.

Home Assistant's generic reads (`config_entries/get`, `GET /api/config/config_entries/entry`) carry no options, and neither helper has diagnostics.

## The read

Starting the entry's options flow returns the current options as suggested values, and aborting it saves nothing:

1. `POST /api/config/config_entries/options/flow` `{"handler": "<entry_id>"}` (admin; the Relay's token is one). Both helpers are `SchemaConfigFlowHandler`s: the `init` step is a pure schema form whose `suggested_value`s are the stored options (`helpers/schema_config_entry_flow.py`, `_show_next_step`). Answer: `{"type":"form","flow_id":…,"handler":"<entry_id>","step_id":"init","data_schema":[…{"name":"heater","selector":{"entity":…},"description":{"suggested_value":"switch.x"}}…]}`.
2. `DELETE /api/config/config_entries/options/flow/<flow_id>` aborts it. Options are saved and the entry reloads only on `create_entry`, which an abort never reaches. Starting the flow fires no event, creates no notification, and runs no callback (neither helper has a schema callback on `init`; `validate_user_input` runs only on a submit; both are built in with no requirements to install).

**Allowlist, fixed in code:** `generic_thermostat` → field `heater`, `generic_hygrostat` → field `humidifier`. No other integration's options flow is ever started: others may do I/O or register previews when their flow starts (Astra, HANO-32).

## Design

1. **One pseudo-endpoint in the relay session** (`helper_options`, payload exactly `{"entry_id": "<id>"}`), allowed by `checkReadOnlyRequest` and nothing else. It goes through `relaySession.read`, so memo, the read budget, `reads()`, and the cut-plan replay treat it like any read (one flow per entry per session; a replay reads it again). Its sender:
   - refuses before any POST unless `config_entries/get` lists the entry, its `domain` is on the allowlist, `supports_options` is true, and `manifest/list` says that domain is built in (a custom integration can take the domain; Astra). The check lives here, not in the caller, so no path, a replayed one included, can start another integration's flow.
   - sends the POST once (no transport retry, so no second flow); whatever comes back, if it holds a `flow_id` it sends the DELETE on a context detached from the session budget (5 s of its own), so an expired budget or a parse error never skips the abort.
   - returns the field's value only when all of these hold, else an error: the POST answered 200, `type` is `form`, `handler` is the entry, `step_id` is `init`, the field is in `data_schema` with an `entity` selector and a `suggested_value` that is one literal entity id, and the DELETE answered 200.
2. **Lazy, and only for the reactor rule.** `reactorCalls` (run only by `callServiceListeners`, so only when the home has a `call_service` listener) bounds the two helpers instead of making the set unknown, after the other reactors had their say (`alert`, `flux`, `device_sun_light_trigger` still make it unknown, and then nothing is read):
   - every entity of either integration, from `entity/source` (added to both read lists: read-only, `{entity_id: {domain}}`, and the only list with entities that have no registry row);
   - each needs a full-registry row with a `config_entry_id` (a YAML helper has none: unknown, as today), and nothing may react to the helper's own state (the same check as for its heater below), before any flow starts;
   - each entry's read, one after the other; any failure: unknown;
   - each stored entity must be a `switch` or `fan` (the forms' own selector), not a `group` or a `switch_as_x` wrapper or a switch on a device with one (both device-less counts, as in `switchAsX`), and nothing may react to its state: `search/related` for it and its device lists no script (a wait on its state) and no group, no automation there has triggers that match it or its device (the thermostat sits on the heater's device; an automation that only sets it doesn't count), and no automation listens to `state_changed` events, which `search/related` doesn't index (Fable: a consumer of the heater's state calls services the set would not hold). Else unknown.
   - then per stored entity: `homeassistant.turn_on`, `homeassistant.turn_off`, and `calledOn(entity)` (its domain's services; its own platform's calls).
   A call that reaches a helper entity adds its own domain and no longer makes the set unknown: the reactor rule covers what it calls, because a helper entity that runs is a loaded integration. The raw-write listener scan (`rawListeners`) never runs `callServiceListeners`, so it never starts a flow.
3. **Fail closed.** Any failed or odd read, a failed abort, a timeout, a YAML helper, a heater something listens to, or an odd heater leaves the set unknown: every listener counts, which is today's behavior. Nothing here lowers a tier by itself; it only lets `mayFire` rule a listener out.
4. **Drift.** A plan's apply recomputes with live reads; a heater change that changes the rated plan (its consumers) changes its hash. A swap to another plain switch with the same reach rates the same.

## Review (before build)

- Astra (gpt-6-astra, medium): agrees with corrections. Taken: built-in check before the POST; group heaters unknown; the drift promise narrowed.
- Fable (fable-5-2, medium): agrees with corrections. Taken: a heater something listens to stays unknown; switch/fan, no group, no UUID; `entity/source` on the raw read list too; docs and tests listed below; no "helpers seen" flag.
- KISS (sonnet-5-5): agrees with corrections. Taken: no entry cap, reads one after the other. Not taken: dropping `entity/source` (a YAML helper without a registry row would be missed, not unknown).

## Review (before PR)

- Fail-open (Astra): `state_changed` event automations and scripts waiting on the heater were missed. Fixed: either leaves the set unknown.
- Sibling paths (Fable): a device-less `switch_as_x` was missed (fixed, as in `switchAsX`); any automation on the heater's device made the set unknown (fixed: only matching triggers count); the bed's second-thermostat check passed vacuously (fixed: a fan call with a control plan before, while no `switch_as_x` exists).
- Local Codex review: an automation on the thermostat's own state was missed (fixed: the helper must be quiet too, checked before any flow starts); reads went on once the set was unknown (fixed: they stop).

## Coordination

- **Gate (HANO-24, layer 3+).** The gate worker rates with the same engine on its own session through the Relay, so its options read is itself a gated request. The gate rates an options-flow start like the entry's reload (`rawListened` → the consumers of the helper's entities), without a service-call scan, so the rating can't start a nested flow. If that rating is typed (an automation on the thermostat's state unlocks), the read gets 428 and the set stays unknown, in the CLI and the gate alike. It needed from HANO-24 that the worker serves a nested `/check` while one is open (built: `tests/runtime/gate-worker.test.ts`). As built in 0.30 the gate answered both requests `kind: write`, so the Relay refused them and the set stayed unknown on a gated App; 0.31 answers them `kind: probe` and marks the start's own check so it never starts a flow, whatever its scan reaches (`docs/work/2026-11-12-gate-helper-probe-spec.md`, MAR-153).
- **F1 (MAR-142, HANO-27)** changes the read budget in `tier_scan.go`/`tier_ahead.go`. This change stays in its own file plus small edits in `tier_callset.go` and `relay_session.go`; merged with F1, it reads where the heater and helper sit as the consumer scan does.
- No plan-file field and no MCP tool text changes.

## Tests

- Go (`cli/tier_helper_options_test.go`, fake relay): no POST for another domain's entry, a missing entry, `supports_options: false`, a custom integration on the domain, or a replayed read of such an entry; the abort is sent after a good read, a bad shape, a non-200, a missing field, a missing `suggested_value`, a UUID or templated value; no read without a `call_service` listener or when the set is already unknown (a reload, `alert` loaded); a YAML helper, one of two reads failing, a failed abort, and a POST timeout stay unknown; a group, `switch_as_x`, template, or referenced heater stays unknown; a bounded heater rules out a cover listener and keeps a `homeassistant.turn_on` one; `rawListeners` on an options-flow start sends no POST.
- Test bed (`scripts/e2e/testbed/calls.mjs`, step h): a UI generic thermostat whose heater is a demo fan nothing listens to: a light call stays ordinary (the cover watchdog doesn't count); planning leaves its stored entry (`.storage/core.config_entries` and `modified_at`) unchanged and no `state_changed` for it (no reload); oracle: setting it to heat turns the fan on, and the `homeassistant.turn_on` listener that sets off is a consumer; a second thermostat whose heater is the relay switch an automation follows makes a fan call count the climate listener, which a control plan just before doesn't.

## Known limits

- A POST that times out may leave an unaborted flow in Home Assistant's memory until it restarts. For these two helpers it holds nothing and changes nothing.
- YAML thermostats and hygrostats stay unknown.
- A device-less heater in a home with any device-less `switch_as_x` stays unknown: the wrapper can't be attributed.
- The heater's own state listeners are not scanned when a plan targets the thermostat (as today); while something listens to the heater the set stays unknown.
- A heater or helper listened to only through a template is not seen by `search/related`. Area, floor, and label triggers are (F1, MAR-142): an automation found where it sits counts when a trigger targets that place, as in the consumer scan; any script found there counts.
- Planning in a chat app below Manage everything also starts and aborts these flows: fixed in code, never chosen by the model.
- Presets are a second step and are not read; they call nothing.
- Every plan in such a home counts the heater's switch listeners, since the helper may react to anything the plan changes (the reactor rule, as today).
