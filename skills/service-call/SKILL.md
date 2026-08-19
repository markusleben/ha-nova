---
name: service-call
description: Use when the user wants to control something in Home Assistant right now — turn lights, switches, covers, climate, vacuums, or any device on, off, up, down, open, or closed; run a scene, script, or automation; call any service by name; fire custom events or known webhooks; operate alarm panels and locks — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Service Call


## Scope

Direct device/service control:
- call any HA service (`light.turn_on`, `climate.set_temperature`, etc.)
- list available services
- target by entity_id, area_id, or device_id
- fire a named custom event or trigger a known JSON webhook
- control alarm panels and locks with capability and secret-code gates

No config mutations (use `ha-nova:write` for automation/script changes).

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

Use file-based payloads for service writes:
- `ha-nova relay core --method POST --path /api/services/... --body-file <payload-file>`
- `ha-nova relay core --method GET --path /api/events`
- `ha-nova relay core --method POST --path /api/events/<event_type> --body-file <payload-file>`
- `ha-nova relay ws --data-file <payload-file> --out <result-file>` for internal `webhook/list` metadata; both files stay in client-private scratch storage and the response never goes to stdout
- `ha-nova relay core --method POST --path /api/webhook/<webhook_id> --body-file <payload-file>`
- `ha-nova relay core --method GET --path /api/states/<entity_id>`
- `--out <result-file>` when the response is large

## Owning-Skill Deferrals (Critical)

"Call any service" never overrides a stricter owning skill. When the request matches a row below, STOP and continue in that skill — it carries gates this flow lacks (feature checks, backup offers, typed codes, restore duties):

| Service(s) | Owning skill |
|---|---|
| A call bounded by a duration ("for 30 minutes", "for an hour", "until 18:00") whose service has NO duration field | `ha-nova:write` — the turn-on is a service call, the turn-off needs an automation, and splitting them loses the pairing |
| The same request where the SERVICE already takes the duration — `timer.start`/`timer.change` (`duration`), `siren.turn_on` (`duration`) | stays here: Home Assistant bounds it itself; no counter-action or persistent automation is needed |
| `mqtt.publish` | `ha-nova:mqtt` |
| `update.install` / `update.skip` / `update.clear_skipped` | `ha-nova:updates` |
| `camera.*` (snapshot, record, power, `play_stream`, motion detection) | `ha-nova:camera` |
| `media_player.*` / `tts.*` | `ha-nova:media` |
| `notify.*` / `persistent_notification.*` | `ha-nova:notify` |
| `logger.set_level` | `ha-nova:diagnose` |
| `recorder.purge` / `recorder.purge_entities` | `ha-nova:maintenance` |
| `calendar.create_event` and other calendar mutations | `ha-nova:calendar` |
| `todo.add_item` / `todo.update_item` / `todo.remove_item` / `todo.remove_completed_items` | `ha-nova:todo` |
| `backup.create` / `backup.create_automatic` | `ha-nova:backup` |
| `conversation.process` (executes what it understands) | `ha-nova:assist` |
| `hassio.addon_start\|stop\|restart`, `hassio.host_reboot\|shutdown` | stays here, disruptive tier — refuse the App hosting this Relay |
| any other `hassio.*` (`restore_full`, `restore_partial`, `addon_update`, ...) | not covered here — `ha-nova:backup` owns restores, `ha-nova:updates` owns App updates; anything else STOPS at `ha-nova:fallback` |

Read-only response services stay here (`calendar.get_events`, `todo.get_items`, `weather.get_forecasts`) — only the mutating siblings defer.

`hassio.addon_start|stop|restart` and `hassio.host_reboot|shutdown` are ordinary Home Assistant services on this transport, so they run from here under the disruptive tier; `ha-nova:fallback` covers App *management* (install, configure, store), not these. The `hassio` domain is NOT a wildcard: `restore_full`/`restore_partial` reboot Home Assistant and belong to `ha-nova:backup`, which refuses restores outright; `addon_update` belongs to `ha-nova:updates`. Never widen this row to a service it does not name. Refuse outright any call targeting the App that runs this Relay — stopping it kills the connection mid-call, so nothing can be verified or undone.

These five do not fit the entity Flow below, so do not force them through it:

- The target is an App SLUG, not an entity: `{"addon": "core_mosquitto"}`, and a display name is not a slug. The Supervisor API is NOT reachable from here — the relay passes only `/api/...` and Home Assistant answers its `/api/hassio/...` proxy with `403` — so resolve the slug from what IS readable: every installed App has an `update.*` entity on the `hassio` platform whose `title` is the display name. Take the slug from that entity's REGISTRY record, not from its state: WS `{"type":"config/entity_registry/get","entity_id":"update.<app>_update"}` returns `unique_id` `<slug>_version_latest` — strip the suffix. The `entity_picture` attribute contains the slug too and must NOT be used: it is a mutable state attribute a user can customize, and a stale one would send `addon_stop` to a different App. If the title matches more
than one `update.*` entity — an official App and a fork can share a display
name — do not pick one: list the candidates with their slugs and ask. Starting
or stopping the wrong App is not recoverable by trying again. Show both name
and slug in the preview. If no matching `update` entity exists, stop and ask for the slug rather than guessing it.
- **App state cannot be verified from here.** There is no readable `started`/`stopped` for an App on this transport, so steps 4 and 7 have nothing to read: report the call as issued, say plainly that the result is not observable through the Relay, and point at the Apps page. Never infer success from the service call returning.
- `host_reboot` and `host_shutdown` have no target at all and take the whole transport down with them. Nothing can be verified afterwards from here — say that BEFORE asking, get the disruptive-tier confirmation, then report the call as issued and the connection as expected to drop. Never report success: you will not be there to see it. `host_shutdown` additionally needs physical access to come back, so say so.

Runtime calls that stay here: `scene.turn_on` / `scene.apply` (`ha-nova:scene` owns scene CRUD, not activation), `automation.trigger`, direct `script.*` (see Automation And Script Runtime Calls), custom events, known JSON webhooks, and `lock`/`alarm_control_panel`/`cover` control under the gates below.

## Response services

Some services return data (`weather.get_forecasts`, `calendar.get_events`, `todo.get_items`, ...) and REQUIRE `?return_response`; without it HA returns 400 "requires responses". Path: `/api/services/<domain>/<service>?return_response`; data: `.data.body.service_response`. These reads need no write confirmation only when the shared gate's consumer scan stays ordinary; a matching `call_service` event listener can make the nominal read indirect actuation. A response-capable ACTION service (for example direct `script.<script_id>`) still follows the full preview/confirmation flow — the parameter adds the response, never downgrades the action.

### Weather forecasts

`weather.get_forecasts` is the response service for forecasts — there is no weather skill because this IS the whole API:
`POST /api/services/weather/get_forecasts?return_response` with `{"entity_id":"weather.<id>","type":"daily"}` (`hourly` / `twice_daily` also exist). The forecast list arrives under `.data.body.service_response["weather.<id>"].forecast` — bracket notation, the key contains a dot. It needs no confirmation only after the shared consumer scan stays ordinary.

## Flow

1. Resolve target entity (use entity discovery if name is ambiguous).
2. If service is unclear, list available services for the domain:
   - `ha-nova relay core --method GET --path /api/services`
   - Filter by relevant domain.
3. If the user names a room/area but the intended scope could be narrower, ask one clarifying question before using `area_id`.
   - Do not ask a second blocking ambiguity question in the same turn.
   - If entity resolution already consumed the one blocking question, default to the narrower confirmed target or stop and explain the ambiguity.
   - Direct `floor_id` and `label_id` selectors are unsupported; ask for an entity, area, or device instead. Expand an `area_id` or `device_id` to the concrete member entities the service's domain applies to BEFORE the preview, and list them there. Areas expand via WS `search/related` on the resolved area (the canonical relation source, `skills/ha-nova/bulk-patterns.md`) — registry `area_id` alone misses entities that inherit the area from their device. Execute with that expanded `entity_id` list instead of the broad target — it freezes the previewed membership into the payload, so a member added or removed after the preview is neither silently actuated nor silently skipped; the preview and verification bind to exactly that list.
4. Preview the service call with stable localized slots:
   - Before preview: read current state via `ha-nova relay core --method GET --path /api/states/{entity_id}`.
   - Capability gate: if the call depends on a device capability (position, color_temp, hvac modes, sound mode, ...), check the target's attributes/`supported_features` in that state read first; if the device cannot do it, say so instead of calling (pattern: `ha-nova:media`).
   - Indirect actuation gate — decided by the TARGET, not the service name: any call whose target is in `scene`, `script`, `automation`, or a legacy `group` (which forwards to its members) enters the gate — including `scene.apply`, which names its entities in an `entities` map instead of a target and would otherwise match no condition at all, including the generic aliases (`homeassistant.turn_on`/`turn_off`/`toggle` on `script.open_door` reaches it too), and so do the targetless reloads of trigger and state sources (`schedule`, `input_*`, `counter`, `timer`, `template`, `rest`, `command_line`), and writes to a trigger source (`input_button.press` and `button.press` — a Template button runs a stored action, so it is an indirect run, not a toggle — plus writes to any storage helper: `input_*`, `counter`, `timer`, `schedule`) actuate entities the request never names. Entering the gate is not the same as being a run: the gate decides that. `homeassistant.turn_off` on a script or automation never starts its stored members, but still scans consumers. Everything that MAY start a script (`turn_on`, either `toggle`, the direct service) expands and classifies regardless of the observed state: a `mode: single` script that is running at preview time can finish while the confirmation waits, and then the call that "runs nothing" runs everything. Expand and classify the members per `skills/ha-nova/indirect-actuation.md` BEFORE previewing, list them in the preview, and let them set the tier per Safety. Ordinary device control expands no stored members but still runs the gate's CONSUMER scan: a state trigger accepts any entity, so a light another automation answers with `lock.unlock` grants access like a helper toggle. Scan one `search/related` result plus matching `call_service` event consumers; only zero hits across both stays ordinary. Two targets go further — `pl: "template"` runs its author's stored sequence whatever the domain, and `timer` needs the event scan.
   - If service changes an attribute present in the service call parameters (brightness, temperature, position, hvac_mode, etc.) OR inherently changes entity state (toggle, turn_on, turn_off, press, lock, unlock, open, close), show state delta before the call details:
     ```
     **State delta:**
     brightness: 100% → 40%
     ```
   - Two or more changing values (or batch targets): the `Field | Before | After` mini table (`output-rules.md` -> Cards) under the same `State delta:` label; one value keeps the arrow one-liner.
   - Attribute display rules:
     - **Brightness**: HA uses 0-255 internally; ALWAYS show delta in % (never raw). Light off (brightness null/absent) counts as 0%: `brightness: 0% → 40%`.
     - **Temperature**: Show with unit: `22.5°C → 19°C`; `temperature` = setpoint (what we change), `current_temperature` = sensor reading (NOT for delta).
     - **Cover position**: `position: 100% (open) → 30%`.
     - **Relative asks** ("a bit brighter", "one degree warmer"): prefer the service's own step parameter where one exists — `light.turn_on` takes `brightness_step_pct` (-100..100), covers have NO step service — `open_cover`/`close_cover` drive to the endpoint, so a relative cover ask reads `current_position` and calls `set_cover_position` with the bounded delta, media through `volume_up`/`volume_down`. Where none exists, read the current value and apply the delta — and re-read it
at apply time, because a delta computed from a stale base moves the device to
the wrong absolute value if someone else touched it during the confirmation.
A changed base re-previews — and if that read failed or the attribute is absent (a cover without `current_position`, a device that reports no level), STOP the relative operation and say so. This is the one place the "a failed state read does not block the call" rule does not apply: an absolute call still has a complete payload without the read, a relative one would have to invent the number it is relative to. Offer the absolute action instead (`open_cover`/`close_cover`, a named level). A follow-up nudge ("brighter still") keeps the last confirmed target unless the user names a new one.
     - **State / mode**: parameterless state-changing services (toggle, turn_on, turn_off, press, lock, unlock): `on → off`; mode changes: `hvac_mode: heat → cool`.
   - Entity `unavailable` → delta `unavailable → {target}` + warning: "Device is offline or unreachable."
   - Entity `unknown` → delta `unknown → {target}` + info: "State not yet known; the call may still work."
   - State read failed → preview without delta, do not block.
   - Show: service (`domain.service`), target (`entity_id`), data fields.
   - Include an explicit not-executed-yet line before confirmation.
   - Show an Options block with the execute/apply choice and `cancel`. Do not offer `show yaml` unless the user asks for raw payload details. Exception: in a grouped change set the single final action block uses the grouped keywords `apply · show yaml · cancel` (`skills/ha-nova/grouped-change-set.md`), replacing this per-call menu.
   - Ask for natural confirmation bound to this exact preview (see context skill → Active Preview Confirmation). Earlier planning consent is draft-only.
   - **Unless Safety put this call on the typed tier.** Then the menu offers no `apply`/`execute` word at all and the only accepted answer is the exact `confirm:<token>` — `yes`, `apply`, and the grouped keywords are invalid, including when the tier came from an EXPANDED member rather than the named service. A gate that assigns a tier and then renders the ordinary menu has assigned nothing.
5. Execute:
   - snapshot pending reauth flows first (Error Handling → Generic 500 with a reauth side effect)
   - `ha-nova relay core --method POST --path /api/services/{domain}/{service} --body-file <payload-file>`
6. Verify result — match the check to what the call promises:
   - Default: read the entity state back (`ha-nova relay core --method GET --path /api/states/{entity_id}`) and confirm the expected change.
   - User-assisted observation: a physical action by the user (pressing the device's own button, operating it by hand) never verifies a service call this skill sent — it is separate evidence about the device or input. When such an observation is needed, follow context skill → User-Assisted Readiness: read the baseline state and timestamp BEFORE the instruction, confirm ready, give one exact "act now" instruction, then re-read, compare, and attribute the result to the user's action, not to the call.
   - Transitions: covers report `opening`/`closing`, lights fade over `transition`, climate ramps — when the read-back shows a transitional or unchanged value on such a device, wait a few seconds (up to the transition length) and re-read once before calling it a discrepancy.
   - Timestamp targets: `scene.turn_on`, `button.press`, and `input_button.press` record the action as the target's state timestamp — verify that it advanced; scene member entities only as secondary evidence.
   - Stateless targets: `scene.apply` and direct `script.*` runs do not reflect the call in the target's own state. Verify the promise instead — a script via `last_triggered` or acted-on member entities, `scene.apply` via the applied member states — and say what was (not) verifiable rather than reporting a false discrepancy.
   - Area/device targets: verify the member list expanded and previewed in step 3, not a single entity.
   - Report: service called, verified state (or the honest verification limit), any errors.
   - After a VERIFIED grouped batch that set several entities at once, offer once to keep it: as a scene (`ha-nova:scene`, create-from-current-state) or a script (`ha-nova:write`). A movie-night set of five calls the user repeats weekly should become one activation. After a single decline, stay silent about it for the session.

## Service Data Fields

Per-domain field names, feature bits, and verification quirks live in
`skills/service-call/domain-fields.md` — read the section for the domain you
are calling. It covers light, climate, cover (including tilt), fan, vacuum
(including area cleaning), humidifier, water heater, and siren.

Two rules apply to every domain:
- `/api/services` gives field NAMES; the valid VALUES for effects, modes,
  tones, and fan speeds come from the target's own attribute list in the
  pre-preview state read. Never carry a value over from another device.
- A parameter the device does not support is usually dropped silently rather
  than rejected, so check the feature bit before promising the user the effect.

## Helper Service Patterns

Common service calls for helper entities:

- **input_boolean:** `input_boolean.turn_on`, `input_boolean.turn_off`, `input_boolean.toggle`
- **input_number:** `input_number.set_value` (`value`), `input_number.increment`, `input_number.decrement`
- **input_text:** `input_text.set_value` (`value`)
- **input_select:** `input_select.select_option` (`option`), `input_select.select_first`, `input_select.select_last`, `input_select.select_next`, `input_select.select_previous`
- **input_datetime:** `input_datetime.set_datetime` (`date`, `time`, or `datetime`)
- **input_button:** `input_button.press`
- **counter:** `counter.increment`, `counter.decrement`, `counter.reset`, `counter.set_value` (`value`)
- **timer:** `timer.start` (optional `duration`), `timer.pause`, `timer.cancel`, `timer.finish`, `timer.change` (`duration`)
- **schedule:** `schedule.reload` (reloads all schedule entities from config)

Example:
```json
{"method":"POST","path":"/api/services/input_number/set_value","body":{"entity_id":"input_number.target_temperature","value":22.5}}
```

Before `input_number.set_value`/`increment`/`decrement` — and before a `scene.apply` whose entities map assigns such a helper — resolve the helper's direct consumers (`search/related`): when an automation/script compares a physical-process signal against this helper — through `numeric_state` or a direct template comparison, the value change moves that threshold — run the calibration preflight per `skills/ha-nova/threshold-calibration.md` and carry its findings into the preview.

For helper CRUD (create/update/delete helpers themselves), use `ha-nova:helper` instead.

## Automation And Script Runtime Calls

`automation.trigger`, direct script execution (`script.<script_id>`), and `script.turn_on` are live runtime actions. They may run arbitrary action sequences and can affect physical devices even when the user's intent is "just test it".

Rules:
- Never call them automatically from read, review, write, or post-write verification.
- Use this service-call flow only after a concrete preview shows the exact service, target, payload, whether `skip_condition` is set, and the members the run actuates (Flow step 4 → indirect actuation gate).
- Treat `skip_condition: true` as higher risk because it bypasses automation conditions.
- Ask for confirmation bound to that exact runtime-call preview before execution.
- After execution, verify only the target automation/script state and any user-approved helper/state assertions; do not infer device safety from a successful service response alone.
- Post-write test runs: plan structure, real-path recipes, and the post-run follow-up live in `skills/ha-nova/test-run.md`.
- When a Test Plan Card already showed the concrete preview (service, target, payload, `skip_condition`), the user's option choice on that card IS the bound confirmation — do not ask again. The one exception: if the indirect actuation gate put the run on the typed tier, the card choice never replaces `confirm:<token>`.

## Custom Events And Webhooks

Both paths are runtime actions that can start every matching automation. Never use either endpoint to probe or discover whether a listener exists.

### Custom events

1. Require the exact user-defined `event_type` and a JSON-object payload. Never normalize or invent the name, and never fire core lifecycle/state events through this flow.
2. Read `GET /api/events` for the total listener count. Scan readable automation configs for current event triggers (`trigger: event`) and legacy triggers (`platform: event`) with the same static `event_type`; apply any literal `event_data` filters to classify known matches. Templated event types and non-automation listeners (Node-RED, AppDaemon) are not safely enumerable — disclose that limit, and treat their presence the way the shared gate does: an unenumerable listener cannot be shown to be harmless, so the fire takes the typed `confirm:<token>` rather than natural confirmation (`skills/ha-nova/indirect-actuation.md`). This scan is the shared event-consumer pattern of `skills/ha-nova/consumer-discovery-preflight.md`.
3. Inspect known matching automations for high-consequence actions. Preview the exact event type, payload fields, known matching automations, total listener count, unclassified-listener warning, and risk tier. Use natural bound confirmation only when EVERY listener was enumerable and none of them reaches the high-consequence tier. Any unenumerable listener, or a known high-consequence match, requires `confirm:<token>` — unknown impact is not low impact, and step 2 already established that an opaque listener cannot be shown to be harmless.
4. Execute `POST /api/events/<event_type>` with the JSON object. A success response proves only that Home Assistant accepted the bus fire.
5. For known matching automations, compare `last_triggered` or a new trace with the pre-call baseline using up to three reads over ten seconds. Never claim that every listener completed, and never repeat an event automatically after any timeout or transport error.

### Webhooks

1. Resolve the target automation by exact identity, then extract its static `webhook_id` internally from the stored trigger config. Scan all readable automation configs for that exact ID because multiple triggers can share one webhook. A templated ID is not safe to resolve here.
2. Call WS `webhook/list` internally with `--out <result-file>` in client-private scratch storage; never allow its secret-bearing response on stdout. Read the saved result internally, confirm that the ID is registered and `POST` is allowed, and retain only redacted metadata such as `local_only` for preview. The current Relay sends JSON; if the automation expects form/query data or another method, stop and use an explicitly authorized local caller outside this JSON-only flow.
3. Treat the webhook ID as an authentication secret: never ask the user to paste it, echo it, put it in a preview/result, or persist it outside client-private scratch storage. Only the internal request path may contain it.
4. Preview the JSON payload fields, every known matching automation, local-only status, and risk tier — never the ID. Shared IDs mean every match runs. Use the same bound/high-consequence confirmation rule as custom events.
5. Execute one `POST /api/webhook/<webhook_id>`. Home Assistant intentionally returns HTTP 200 for unknown IDs, blocked remote calls, and handler errors, so status alone is not verification.
6. Compare every known match's `last_triggered` or trace baseline with up to three reads over ten seconds. If no fresh run appears, report the outcome as unverified; never retry automatically and never weaken `local_only` to make the call work.

## Alarm Panels And Locks

Read the exact entity state immediately before preview. Never include a `code` field in a Relay payload and never ask for, accept, repeat, or store a PIN/code in chat.

Alarm panel services and feature bits:

| Service | Required `supported_features` bit | Expected terminal state |
|---|---:|---|
| `alarm_arm_home` | 1 | `armed_home` |
| `alarm_arm_away` | 2 | `armed_away` |
| `alarm_arm_night` | 4 | `armed_night` |
| `alarm_trigger` | 8 | `triggered` |
| `alarm_arm_custom_bypass` | 16 | `armed_custom_bypass` |
| `alarm_arm_vacation` | 32 | `armed_vacation` |
| `alarm_disarm` | none | `disarmed` |

For an arm action, hand off to the Home Assistant UI whenever `code_arm_required` is true, even when `code_format` is absent. For disarm/trigger, hand off when `code_format` indicates a code. `alarm_disarm` takes the typed high-consequence confirmation; `alarm_trigger` is disruptive, so warn explicitly and require bound confirmation even when no code is needed.

`lock.lock` and `lock.unlock` have no feature bit. `lock.open` requires `supported_features & 1`. If `code_format` is present, finish the action in the Home Assistant UI. `lock.unlock` and `lock.open` take the typed high-consequence confirmation; `lock.lock` uses normal bound confirmation.

Verify terminal states transition-aware: alarm panels can pass through `arming`, `pending`, or `disarming`; locks can pass through `locking`, `unlocking`, or `opening`. A service response is not proof of the physical result. Report `jammed`, `unavailable`, timeouts, or an unchanged terminal state as a discrepancy; never auto-retry a security action.

## Error Handling

Full relay/upstream error taxonomy (codes, HTTP-status split, retry rules): `skills/ha-nova/relay-api.md` → Error Handling.

Service-call specifics:
- `404/NOT_FOUND` or upstream `.data.status` 404: entity or service does not exist — re-resolve before retrying
- `502/UPSTREAM_*` transport errors: HA may already have accepted the action — verify entity state first (see `relay-api.md` → Timeout and Retry Guidance); retry once only when verification shows no state change, otherwise report the result
- State verification failure (state didn't change): report discrepancy, do not retry automatically

### Generic 500 with a reauth side effect

A service backed by invalid credentials can answer only a generic upstream 500
while Home Assistant simultaneously opens a reauthentication flow for the
config entry. Reconcile the two instead of reporting the bare failure:

1. Right before executing a confirmed call, snapshot pending reauth flows: WS
   `{"type":"config_entries/flow/progress"}`, keep only entries with
   `context.source == "reauth"` (client-private scratch storage). The snapshot
   is best-effort: if this read fails, proceed with the confirmed call and
   report a later 500 without reauth correlation — optional evidence never
   blocks an approved action.
2. On a generic upstream 500 (`.data.status` 500), re-read the same list —
   reauth flows open asynchronously, so wait a few seconds and re-read once
   more before concluding none appeared. A flow counts as NEW only
   when it is absent from the snapshot — a pre-existing flow is never reported
   as this call's side effect.
3. Match the new flow to the failed call: a match on `context.entry_id` —
   the target entity's registry `config_entry_id` — is decisive alone;
   otherwise its `handler` must equal the target's integration domain — the
   registry row's `platform`, never the service or entity_id prefix — AND the
   target must be that domain's only config entry; several entries sharing
   the domain leave the flow unattributable (step 4's no-match branch). A same-domain
   system-log entry inside the call window is corroboration, never a match by
   itself. A flow for another domain or entry does not match.
4. On a match, report both facts — the call failed AND Home Assistant started
   reauthentication for that integration — and hand the reauth to
   `ha-nova:integration-setup`. No match, or ambiguous evidence: report the
   failure and hand off to `ha-nova:diagnose` instead of guessing.
5. Never retry the service call automatically after a 500, and never surface
   credentials or key fragments from flows or logs.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Previews are the runtime-action Preview Card (`apply · cancel`); results are the Result Card. Report the executed service, its target, and the verified state change (or the discrepancy); summarize response-service data as the Report shape (output-rules.md) instead of dumping it.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

- No typed confirmation code needed for ordinary service calls; confirmation is still bound to the active preview.
- **High-consequence runtime actions take the typed `confirm:<token>`** like a destructive write: unlocking or opening a lock, disarming an alarm panel, opening a garage door, gate, or entry-door cover. Check `device_class` and what the entity controls — a garage door exposed as `cover.*` belongs here, a living-room blind does not. These actions grant physical access; calling the opposite service afterwards does not undo the exposure window.
- The tier follows the performed action, not the called service: when the indirect actuation gate expanded a scene, automation, or script and any member grants physical access or is physically irreversible, the whole run takes the typed `confirm:<token>` — the same rule `ha-nova:scene` applies to its apply-test. A member that only locks, closes, or arms grants nothing and stays ordinary.
- For potentially disruptive services (`homeassistant.restart`, `homeassistant.stop`, `hassio.host_reboot`, `hassio.host_shutdown`, `hassio.addon_start`, `hassio.addon_stop`, `hassio.addon_restart`, `siren.turn_on`), warn and ask for explicit post-preview confirmation. Restarting an App is not harmless: an MQTT or Z-Wave App takes every device it serves offline while it comes back. Disruptive is not the high-consequence tier: it interrupts, it neither grants physical access nor makes anything physically irreversible.

## Guardrails

- One entity at a time unless user explicitly requests batch (array `entity_id` supported).
- For batch service calls, show a grouped manifest first and bind confirmation to that exact manifest — the non-destructive grouped change set contract (`skills/ha-nova/grouped-change-set.md`); actuating high-consequence calls stay excluded.
- Verify per Flow step 6 — transition- and stateless-aware, never a naive immediate re-read.
- If state didn't change as expected after those checks, report discrepancy.
