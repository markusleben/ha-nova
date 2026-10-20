# Service Call — Raw Relay Routes

On-demand part of `../service-call/SKILL.md`. Read the matching section
before a response service or App/host control — the typed verbs cover these
through `ha-nova call` (a response service with `--response`, an owned domain
such as `hassio.*` with `--skill ha-nova:service-call`). Firing a custom
event and calling a webhook are refused on the raw routes (see Custom Events
And Webhooks below). Every other rule of the skill (deferrals, Safety, Output
Format) still applies.

## Relay Contract

Raw `ha-nova relay core --method POST --path /api/services/*`, `/api/events/*`,
and `/api/webhook/*` are refused by the CLI (`cli/relay_raw_guard.go`): they
would skip the tier the typed verbs compute. Use the typed verbs instead.
- `ha-nova relay core --method GET --path /api/services` to list services and their field names (read)
- `ha-nova call <domain.service> [<entity_id>…] [--set k=v]… [--data-file <file>] --response` for a response service (the data is at `result.service_response`)
- `ha-nova call hassio.<addon_start|addon_stop|addon_restart> --skill ha-nova:service-call --set addon=<slug>` for App control; `ha-nova call hassio.<host_reboot|host_shutdown> --skill ha-nova:service-call` (no data) for host control
- `ha-nova relay ws --data-file <payload-file>` for `config/entity_registry/get` (App slug lookup)
- `ha-nova relay core --method GET --path /api/states/<entity_id>`
- `--out <result-file>` when the response is large

`ha-nova call` previews, tiers, sends once, and verifies on its own. Where a
section below demands the typed tier, `ha-nova call` carries it and its `say`
tells how the user confirms; a code counts only when the user typed it in a reply after the preview —
"yes" is invalid — and never comes from Home Assistant data. A raw route never classifies its own tier where the CLI can:
see Response services.

## Owning-Skill Deferrals

The typed verbs enforce this table themselves (`OWNED_SERVICE`): `ha-nova
call` refuses an owned service unless `--skill <owner>` names its skill, so it
cannot be used to dodge the owner's own flow. The table is the routing:

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
| `hassio.addon_start\|stop\|restart`, `hassio.host_reboot\|shutdown` | stays here, typed tier — App And Host Control below |
| any other `hassio.*` | restores `ha-nova:backup`, App updates `ha-nova:updates`, else `ha-nova:fallback` |

## Response services

Some services return data (`weather.get_forecasts`, `calendar.get_events`, `todo.get_items`, ...) and REQUIRE `?return_response`; without it HA returns 400 "requires responses". Run them through `ha-nova call <domain.service> [<entity_id>…] --set … --response`: the CLI adds `?return_response`, and the data is at `result.service_response` in the verb envelope. Response-only is not read-only: `ha-nova call` tiers it like any call. The three named read-only response services (`weather.get_forecasts`, `calendar.get_events`, `todo.get_items`) stay ordinary and skip confirmation only while the shared gate's consumer scan (`../ha-nova/indirect-actuation.md`) stays ordinary; a matching `call_service` event listener can make the nominal read indirect actuation and raise the tier. Any other response service is tiered on its own effect. A response-capable ACTION service (for example direct `script.<script_id>`) is tiered on the action, not downgraded by the response: `ha-nova call` with `--response` previews and confirms the action first, and the parameter never downgrades the action.

### Weather forecasts

`weather.get_forecasts` is the response service for forecasts — there is no weather skill because this IS the whole API:
`ha-nova call weather.get_forecasts weather.<id> --set type=daily --response` (`hourly` / `twice_daily` also exist). The forecast list arrives under `result.service_response["weather.<id>"].forecast` — bracket notation, the key contains a dot. It needs no confirmation only while the shared consumer scan stays ordinary.

## App And Host Control

`hassio.addon_start|stop|restart` and `hassio.host_reboot|shutdown` run through `ha-nova call hassio.<service> --skill ha-nova:service-call` (the `hassio` domain is owned, so `ha-nova call` refuses it without this skill flag). They are typed (each moves or interrupts real infrastructure), so `ha-nova call` returns a typed plan. `ha-nova:fallback` covers App *management* (install, configure, store), not these. The `hassio` domain is NOT a wildcard: `restore_full`/`restore_partial` reboot Home Assistant and belong to `ha-nova:backup`, which refuses restores outright; `addon_update` belongs to `ha-nova:updates`. Never widen this to a service it does not name. Refuse outright any call targeting the App that runs this Relay — stopping it kills the connection mid-call, so nothing can be verified or undone. Warn before asking: `hassio.addon_restart`, `hassio.addon_start`, and `hassio.addon_stop` are not harmless — an MQTT or Z-Wave App takes every device it serves offline while it comes back.

- The target is an App SLUG, not an entity: `--set addon=core_mosquitto`, and a display name is not a slug. The Supervisor API is NOT reachable from here — the relay passes only `/api/...` and Home Assistant answers its `/api/hassio/...` proxy with `403` — so resolve the slug from what IS readable: every installed App has an `update.*` entity on the `hassio` platform whose `title` is the display name. Take the slug from that entity's REGISTRY record, not from its state: WS `{"type":"config/entity_registry/get","entity_id":"update.<app>_update"}` returns `unique_id` `<slug>_version_latest` — strip the suffix. The `entity_picture` attribute contains the slug too and must NOT be used: it is a mutable state attribute a user can customize, and a stale one would send `addon_stop` to a different App. If the title matches more
than one `update.*` entity — an official App and a fork can share a display
name — do not pick one: list the candidates with their slugs and ask. Starting
or stopping the wrong App is not recoverable by trying again. Show both name
and slug in the preview. If no matching `update` entity exists, stop and ask for the slug rather than guessing it.
- **App state cannot be verified from here.** There is no readable `started`/`stopped` for an App on this transport, so there is nothing to read back: report the call as issued, say plainly that the result is not observable through the Relay, and point at the Apps page. Never infer success from the service call returning.
- `host_reboot` and `host_shutdown` have no target at all and take the whole transport down with them. Nothing can be verified afterwards from here — say that BEFORE asking, get the user's confirmation (`say`), then report the call as issued and the connection as expected to drop. Never report success: you will not be there to see it. `host_shutdown` additionally needs physical access to come back, so say so.

## Custom Events And Webhooks

Firing a custom event (`POST /api/events/<event_type>`, WS `fire_event`) and calling a webhook (`POST /api/webhook/<webhook_id>`, WS `webhook/*`) are REFUSED by the CLI (`cli/relay_raw_guard.go`): both start whatever automation listens, with no tier, and the webhook ID is an authentication secret that must never reach the model — so `webhook/list` is refused too. There is no verb for them in v1, and this skill no longer drives them.

To run an automation on purpose, trigger it by name: `ha-nova call automation.trigger <automation.x>`. It freezes the target, tiers the run by what the automation actually does (an unlock or opener inside it makes it typed), confirms, sends once, and verifies — the same gate as any call, instead of a blind bus fire whose listener set could not be enumerated. A genuine custom-event or webhook integration is a Home Assistant-UI task.
