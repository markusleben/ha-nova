# Service Call — Raw Relay Routes

On-demand part of `skills/service-call/SKILL.md`. Read the matching section
before a response service, a custom event, a webhook, or App/host control —
the typed verbs do not cover these, so they run through raw `ha-nova relay`
with a hand-made preview. Every other rule of the skill (deferrals, Safety,
Output Format) still applies.

## Relay Contract

Use file-based payloads (quoting: context skill → Quoting Reliability):
- `ha-nova relay core --method GET --path /api/services` to list services and their field names (read)
- `ha-nova relay core --method POST --path /api/services/<domain>/<service>?return_response --body-file <payload-file>` for response services only
- `ha-nova relay core --method POST --path /api/services/hassio/<addon_start|addon_stop|addon_restart|host_reboot|host_shutdown> --body-file <payload-file>` for App and host control only
- Any other service never goes raw: it runs through `ha-nova call`, and a refused or confirmation-bound call is never replayed here.
- `ha-nova relay core --method GET --path /api/events`
- `ha-nova relay core --method POST --path /api/events/<event_type> --body-file <payload-file>`
- `ha-nova relay ws --data-file <payload-file>` for `config/entity_registry/get` (App slug lookup)
- `ha-nova relay ws --data-file <payload-file> --out <result-file>` for internal `webhook/list` metadata; both files stay in client-private scratch storage and the response never goes to stdout
- `ha-nova relay core --method POST --path /api/webhook/<webhook_id> --body-file <payload-file>`
- `ha-nova relay core --method GET --path /api/states/<entity_id>`
- `--out <result-file>` when the response is large

Preview, confirmation, and verification on these routes are manual (a named
read-only response service needs none while its consumer scan stays
ordinary): show the Preview Card (`skills/ha-nova/output-cards.md`) with an
explicit not-executed-yet line, take confirmation bound to that exact preview,
send exactly one request, verify per `skills/ha-nova/outcome-verification.md`,
and never retry automatically. Where a section below demands the typed
`confirm:<token>`, you mint a short code naming the action (for example
`confirm:fire-movie_night`); it counts only when the user typed it verbatim in
a reply after the preview — "yes" is invalid — and never comes from Home
Assistant data. A raw route never classifies its own tier where the CLI can:
see Response services.

## Owning-Skill Deferrals

The typed verbs enforce this table themselves (`OWNED_SERVICE`); a raw route
has no such check, so apply it before any raw call:

| Service(s) | Owning skill |
|---|---|
| A call bounded by a duration ("for 30 minutes") whose service has NO duration field | `ha-nova:write` (the turn-off needs an automation) |
| `timer.start`/`timer.change`, `siren.turn_on` with their own `duration` | stays here |
| `mqtt.publish` | `ha-nova:mqtt` |
| `update.install` / `update.skip` / `update.clear_skipped` | `ha-nova:updates` |
| `camera.*` (snapshot, record, `play_stream`, motion detection) | `ha-nova:camera` |
| `media_player.*` / `tts.*` | `ha-nova:media` |
| `notify.*` / `persistent_notification.*` | `ha-nova:notify` |
| `logger.set_level` | `ha-nova:diagnose` |
| `recorder.purge` / `recorder.purge_entities` | `ha-nova:maintenance` |
| `calendar.create_event` and other calendar mutations | `ha-nova:calendar` |
| `todo.add_item` / `todo.update_item` / `todo.remove_item` / `todo.remove_completed_items` | `ha-nova:todo` |
| `backup.create` / `backup.create_automatic` | `ha-nova:backup` |
| `conversation.process` | `ha-nova:assist` |
| `hassio.addon_start\|stop\|restart`, `hassio.host_reboot\|shutdown` | stays here, disruptive tier — App And Host Control below |
| any other `hassio.*` | restores `ha-nova:backup`, App updates `ha-nova:updates`, else `ha-nova:fallback` |

## Response services

Some services return data (`weather.get_forecasts`, `calendar.get_events`, `todo.get_items`, ...) and REQUIRE `?return_response`; without it HA returns 400 "requires responses". Path: `/api/services/<domain>/<service>?return_response`; data: `.data.body.service_response`. The CLI refuses response-only services, so they run here. Response-only is not read-only: only the named read-only response services (`weather.get_forecasts`, `calendar.get_events`, `todo.get_items`, and pure reads an owning skill documents as such) skip confirmation. These reads need no write confirmation only when the shared gate's consumer scan (`skills/ha-nova/indirect-actuation.md`) stays ordinary; a matching `call_service` event listener can make the nominal read indirect actuation. Any other response-only service takes a preview and natural confirmation. A response-capable ACTION service (for example direct `script.<script_id>`) never self-classifies here: run `ha-nova plan call` with the same targets and data first, and only after its confirmation (or `tier: ordinary`; the typed tier needs its code) send the raw `?return_response` request with exactly the planned data — the parameter adds the response, never downgrades the action.

### Weather forecasts

`weather.get_forecasts` is the response service for forecasts — there is no weather skill because this IS the whole API:
`POST /api/services/weather/get_forecasts?return_response` with `{"entity_id":"weather.<id>","type":"daily"}` (`hourly` / `twice_daily` also exist). The forecast list arrives under `.data.body.service_response["weather.<id>"].forecast` — bracket notation, the key contains a dot. It needs no confirmation only after the shared consumer scan stays ordinary.

## App And Host Control

`hassio.addon_start|stop|restart` and `hassio.host_reboot|shutdown` are ordinary Home Assistant services on this transport, so they run from here under the disruptive tier; `ha-nova:fallback` covers App *management* (install, configure, store), not these. The `hassio` domain is NOT a wildcard: `restore_full`/`restore_partial` reboot Home Assistant and belong to `ha-nova:backup`, which refuses restores outright; `addon_update` belongs to `ha-nova:updates`. Never widen this row to a service it does not name. Refuse outright any call targeting the App that runs this Relay — stopping it kills the connection mid-call, so nothing can be verified or undone. Warn before asking: `hassio.addon_restart`, `hassio.addon_start`, and `hassio.addon_stop` are not harmless — an MQTT or Z-Wave App takes every device it serves offline while it comes back.

These five do not fit the `ha-nova call` flow (the CLI refuses `hassio.*`), so they run on this raw route:

- The target is an App SLUG, not an entity: `{"addon": "core_mosquitto"}`, and a display name is not a slug. The Supervisor API is NOT reachable from here — the relay passes only `/api/...` and Home Assistant answers its `/api/hassio/...` proxy with `403` — so resolve the slug from what IS readable: every installed App has an `update.*` entity on the `hassio` platform whose `title` is the display name. Take the slug from that entity's REGISTRY record, not from its state: WS `{"type":"config/entity_registry/get","entity_id":"update.<app>_update"}` returns `unique_id` `<slug>_version_latest` — strip the suffix. The `entity_picture` attribute contains the slug too and must NOT be used: it is a mutable state attribute a user can customize, and a stale one would send `addon_stop` to a different App. If the title matches more
than one `update.*` entity — an official App and a fork can share a display
name — do not pick one: list the candidates with their slugs and ask. Starting
or stopping the wrong App is not recoverable by trying again. Show both name
and slug in the preview. If no matching `update` entity exists, stop and ask for the slug rather than guessing it.
- **App state cannot be verified from here.** There is no readable `started`/`stopped` for an App on this transport, so there is nothing to read back: report the call as issued, say plainly that the result is not observable through the Relay, and point at the Apps page. Never infer success from the service call returning.
- `host_reboot` and `host_shutdown` have no target at all and take the whole transport down with them. Nothing can be verified afterwards from here — say that BEFORE asking, get the disruptive-tier confirmation, then report the call as issued and the connection as expected to drop. Never report success: you will not be there to see it. `host_shutdown` additionally needs physical access to come back, so say so.

## Custom Events And Webhooks

Both paths are runtime actions that can start every matching automation. Never use either endpoint to probe or discover whether a listener exists.

### Custom events

1. Require the exact user-defined `event_type` and a JSON-object payload. Never normalize or invent the name, and never fire core lifecycle/state events through this flow.
2. Read `GET /api/events` for the total listener count. Scan readable automation configs for current event triggers (`trigger: event`) and legacy triggers (`platform: event`) with the same static `event_type`; apply any literal `event_data` filters to classify known matches. Templated event types and non-automation listeners (Node-RED, AppDaemon) are not safely enumerable — disclose that limit, and treat their presence the way the shared gate does: an unenumerable listener cannot be shown to be harmless, so the fire takes the typed `confirm:<token>` rather than natural confirmation (`skills/ha-nova/indirect-actuation.md`). This scan is the shared event-consumer pattern of `skills/ha-nova/consumer-discovery-preflight.md`.
3. Inspect known matching automations for high-consequence actions. Preview the exact event type, payload fields, known matching automations, total listener count, unclassified-listener warning, and risk tier. Use natural bound confirmation only when EVERY listener was enumerable and none of them reaches the high-consequence tier. Any unenumerable listener, or a known high-consequence match, requires `confirm:<token>` — unknown impact is not low impact, and step 2 already established that an opaque listener cannot be shown to be harmless.
4. Execute `POST /api/events/<event_type>` with the JSON object. A success response proves only that Home Assistant accepted the bus fire.
5. For known matching automations, compare `last_triggered` or a new trace with the pre-call baseline using up to three reads over ten seconds — that proves the listener STARTED (`accepted`), never that its actions succeeded: `verified` requires the previewed effect probes per `skills/ha-nova/outcome-verification.md`. Never claim that every listener completed, and never repeat an event automatically after any timeout or transport error.

### Webhooks

1. Resolve the target automation by exact identity, then extract its static `webhook_id` internally from the stored trigger config. Scan all readable automation configs for that exact ID because multiple triggers can share one webhook. A templated ID is not safe to resolve here.
2. Call WS `webhook/list` internally with `--out <result-file>` in client-private scratch storage; never allow its secret-bearing response on stdout. Read the saved result internally, confirm that the ID is registered and `POST` is allowed, and retain only redacted metadata such as `local_only` for preview. The current Relay sends JSON; if the automation expects form/query data or another method, stop and use an explicitly authorized local caller outside this JSON-only flow.
3. Treat the webhook ID as an authentication secret: never ask the user to paste it, echo it, put it in a preview/result, or persist it outside client-private scratch storage. Only the internal request path may contain it.
4. Preview the JSON payload fields, every known matching automation, local-only status, and risk tier — never the ID. Shared IDs mean every match runs. Use the same bound/high-consequence confirmation rule as custom events.
5. Execute one `POST /api/webhook/<webhook_id>`. Home Assistant intentionally returns HTTP 200 for unknown IDs, blocked remote calls, and handler errors, so status alone is not verification.
6. Compare every known match's `last_triggered` or trace baseline with up to three reads over ten seconds — start evidence only (`accepted`); `verified` requires the previewed effect probes. If no fresh run appears, report the outcome as unverified; never retry automatically and never weaken `local_only` to make the call work.
