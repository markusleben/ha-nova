---
name: notify
description: Use when sending Home Assistant notifications — discovering notify targets, sending messages to phones or speakers, building mobile-app notifications, and managing persistent notifications — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Notify

## Scope

Notification delivery and management:
- discover available notify targets (entities and legacy services)
- send a notification to a phone, a notify group, or another target
- mobile-app extras: tag/replace, actionable buttons, priority/channel, click URL, images
- persistent notifications inside the Home Assistant UI (list, create, dismiss)

Not in scope: notifications INSIDE an automation or script (that is the automation's action — `ha-nova:write`), TTS announcements to speakers (`ha-nova:media`).

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

- `ha-nova relay core --method GET --path /api/services --out <result-file>` to discover notify services
- `ha-nova relay ws --data-file <payload-file>` for WS commands (entity registry, persistent notifications)
- `ha-nova relay core --method POST --path /api/services/<domain>/<service> --body-file <payload-file>` to send
- `--jq-file <filter-file>` for non-trivial filters

## Target Discovery (do this first)

Home Assistant has TWO notification surfaces, and picking the wrong one silently drops features:

| Surface | How to call | Use it for |
|---------|-------------|------------|
| Notify ENTITIES (`notify.*` entities, modern) | `notify.send_message` with `entity_id` + `message` (+ `title`) | Plain messages, and notify groups built in the UI |
| Notify SERVICES (`notify.mobile_app_<device>`, legacy platform) | `notify.mobile_app_<device>` with `message`, `title`, `data: {...}` | Everything the entity platform cannot do: actionable buttons, `tag` replace/clear, sticky/persistent alerts, Android channel/importance, notification icon, click URL, attachments |

Discovery:
1. Services: `GET /api/services`, filter the `notify` domain — the `mobile_app_*` entries are the per-device legacy targets.
2. Entities: WS `{"type":"config/entity_registry/list_for_display"}`, filter `ei` starting with `notify.`.
3. Report the real, resolved targets — never guess a device name.

**Decision rule:** plain message to a known target → notify entity (`notify.send_message`). Any mobile-app extra (buttons, tag, sticky, channel, url, image) → the legacy `notify.mobile_app_<device>` service, because the entity platform does not carry the `data` payload for those.

## Flow

1. Resolve the target with Target Discovery. If several devices match, present the candidates and ask once.
2. Build the payload:
   - entity: `{"entity_id":"notify.<target>","message":"...","title":"..."}` for `notify.send_message`
   - mobile app: `{"message":"...","title":"...","data":{...}}` for `notify.mobile_app_<device>`. Common `data` keys, and the platform split that silently drops fields when confused:
     - both platforms: `tag` (replace/clear an earlier notification), `actions` (actionable buttons), `image`/`video`/`audio`, `url` (iOS) / `clickAction` (Android).
     - **Android — direct keys inside `data`**: `channel`, `importance`, `ttl`, `priority`, `sticky`, `persistent`, `notification_icon`, `color`. Nesting these under `push` does nothing on Android.
     - **iOS — under `data.push`**: `interruption-level`, `sound`, `badge`. `push` is the iOS-only container; do not put Android channel/importance there.
     Read the target's platform from the device name or ask, and place the keys accordingly.
   - A `message: "clear_notification"` with a matching `tag` removes a previously sent notification instead of sending a new one.
3. Preview the exact payload (target + title + message + any `data`) and get confirmation — a notification is an irreversible, user-visible side effect.
4. Send, then report what was sent where. There is no delivery receipt: a successful service call means Home Assistant accepted it, not that the phone displayed it — say so honestly.
5. Actionable buttons: the button press arrives as a `mobile_app_notification_action` EVENT in Home Assistant. This skill cannot wait for it (the relay does not hold long subscriptions). To react to a press, the user needs an automation that listens for that event — hand off to `ha-nova:write` and explain the pattern instead of pretending to wait.

## Persistent Notifications (Home Assistant UI)

- list: WS `{"type":"persistent_notification/get"}`
- create: service `persistent_notification.create` with `message`, `title`, `notification_id`
- dismiss: service `persistent_notification.dismiss` with `notification_id`

These live only in the HA UI; they never reach a phone.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. Notify specifics:
- `404/NOT_FOUND` on `notify.mobile_app_<device>`: the device name is wrong or the companion app is not registered — re-discover, never guess.
- A service call returning 200 says nothing about delivery: the phone may be offline, or Android may hold the message while dozing (`priority: high` in `data` affects that).
- `notify.send_message` rejects mobile-app `data` extras — that is the entity-platform limit, not a payload bug: switch to the legacy service.
- An accepted send with silently ignored extras usually means the keys sat in the wrong place (Android keys under `push`, or iOS keys at top level) — re-check the platform split before blaming the device.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Render the Report shape (output-rules.md): the resolved target, the sent title/message, and any extras used. Report acceptance honestly (accepted by Home Assistant, not confirmed on the device). Discovery renders the List Frame, grouped by surface (entities vs mobile-app services).

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- A notification is irreversible and user-visible: always preview the exact payload and confirm before sending, even for a single short message.
- Never send test notifications unprompted, and never send to every discovered target at once.
- Repeated/looping sends are not supported here; if the user wants recurring notifications, that is an automation — hand off to `ha-nova:write`.

## Guardrails

- One target per send unless the user explicitly asks for a group; then show the full member list in the preview.
- Never invent device names, `tag` values, or action ids.
- Do not claim delivery — only acceptance.
