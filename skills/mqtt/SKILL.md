---
name: mqtt
description: Use when debugging or working with MQTT in Home Assistant — listening to topics to see what devices actually publish, inspecting MQTT device discovery, or publishing a message — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA MQTT

## Scope

MQTT work through Home Assistant's own MQTT integration:
- listen to a topic for a bounded window to see what is actually being published (the "is my device even talking?" question)
- inspect MQTT discovery/debug info for a device
- publish a message to a topic

Not in scope: the MQTT broker's own configuration, Zigbee2MQTT's web UI, or creating MQTT-based automations (`ha-nova:write`).

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

Bounded subscription windows are guaranteed by the skills' enforced relay floor (`skills/ha-nova/relay-api.md` -> Bounded Event Collection). If a relay-outdated warning appeared, have the user update the NOVA Relay instead of working around it — an older relay silently ignores `on_limit`, which turns a normal quiet window into a hard error.

Requires the `mqtt` integration: check `GET /api/components` for `mqtt`. If it is absent, say so — this skill cannot add it.

## Relay Contract

- `ha-nova relay ws --data-file <payload-file>` for WS commands
- `ha-nova relay core --method POST --path /api/services/mqtt/publish --body-file <payload-file>` to publish
- `--out <result-file>` for large windows; `--jq-file <filter-file>` for filtering

## Listening (bounded window)

MQTT topics never emit a "finish" event, so a listen is a WINDOW, not a request. Use the envelope in window mode:

```json
{
  "message": { "type": "mqtt/subscribe", "topic": "zigbee2mqtt/#" },
  "collect_events": { "max_events": 50, "timeout_ms": 8000, "on_limit": "return" }
}
```

- The relay unsubscribes when the window closes; nothing keeps running.
- `.data.events` holds what was seen. `.data.truncated: true` means the window ended at a LIMIT — either `max_events` or `timeout_ms`, the relay does not distinguish them — not that the stream stopped. Compare the event count with `max_events` to tell them apart: at the cap it hit the event limit (there may be more traffic); below it the window simply ran out of time, which is the normal ending for a quiet topic.
- An EMPTY result is a real answer ("nothing published on that topic in 8 seconds"), not a failure — report it as such. That is often the actual diagnosis.
- **Retained messages are NOT live traffic.** The broker replays every retained message to a new subscriber immediately, so a message can arrive in the first milliseconds of the window even though the device published it days ago and is now silent. Each event carries a `retain` flag: treat `retain: true` as broker state, not as evidence that the device is sending. For the question "is my device actually publishing?", only `retain: false` messages count — and if all you saw were retained ones, say exactly that (the device is silent; the broker is holding its last value).
- Keep windows short (<= 10 s, the relay's ceiling) and topics as narrow as possible; `#` on the root is noise, not evidence.

## Flow

1. Clarify the topic. Prefer the narrowest one that answers the question (`zigbee2mqtt/<device>` beats `zigbee2mqtt/#`).
2. Listen in a bounded window (above). Summarize: which topics appeared, how many messages, and — separately — how many were live (`retain: false`) versus retained replays. Quote at most a few payloads.
3. Device discovery/debug: WS `{"type":"mqtt/device/debug_info","device_id":"<device_id>"}` shows the subscribed topics and discovery payloads for one MQTT device (resolve `device_id` from the device registry, never guess it).
4. Publishing (mutating): service `mqtt.publish` with `topic`, `payload`, `qos`, `retain`. There is no `payload_template` field in the current schema — render any template yourself and send the finished string as `payload`. (`evaluate_payload: true` only tells HA to evaluate a Python-literal payload for raw bytes; it is not a template switch.) Preview the exact topic + payload and confirm.
5. Verify a command/`set` publish: when the topic belongs to an HA-known device, read the affected entity's state back after a moment — a broker accept is not device confirmation; if HA shows no corresponding change, say so. For topics HA does not mirror, state plainly that HA-side verification is not possible.

## Publishing Safety (read before any publish)

- A **retained** message (`retain: true`) persists on the broker and is re-delivered to every future subscriber — including Home Assistant's discovery layer. A wrong retained payload on a `homeassistant/...` discovery topic can create or destroy entities and keeps doing so after a restart. Retained publishes therefore require the typed `confirm:<token>`, not natural confirmation.
- Publishing to a device's `set`/command topic actuates real hardware. Preview it as an action, not as a message.
- Clearing a retained message means publishing an EMPTY payload to the same topic with `retain: true` — say this explicitly when a retained message is the problem.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. MQTT specifics:
- `UNSUPPORTED_WS_TYPE` on `mqtt/subscribe`: check YOUR request first. A BARE `mqtt/subscribe` is rejected by every relay version — that is by design, and it means the subscription was not wrapped in a `collect_events` envelope. Fix the request. Only if the request WAS enveloped does this error mean the relay predates bounded windows; confirm with `ha-nova relay health` before telling the user to update the App.
- `400 VALIDATION_ERROR` mentioning `on_limit`: never expected — an older relay ignores the field silently rather than rejecting it, so this points at a malformed envelope.
- `UPSTREAM_WS_TIMEOUT` in window mode means the subscription never even established (broker down, integration not loaded) — that is different from an empty window, and worth saying plainly.
- `mqtt/device/debug_info` needs a device_id from the device registry; an entity_id is rejected.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

Render the Report shape (output-rules.md). For a listen: the topic, the window length, how many messages arrived (live versus retained — never merge those two), the distinct topics seen, and a few representative payloads — never the raw dump. Say explicitly when nothing arrived, and when everything that arrived was a retained replay. For a publish: the topic, payload, and whether it was retained.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Retained publishes and command/`set` topics take the typed `confirm:<token>` — they change device state or broker state persistently, which is a stricter tier than an ordinary service call.
- Listening is read-only, but keep windows short and topics narrow: a broad subscription on a busy broker is noise, and the relay caps it anyway.
- Never publish to `homeassistant/...` discovery topics unless the user explicitly asks and understands that it can create or delete entities.

## Guardrails

- One topic per window; one publish per confirmation — except a manifest-bound retained-discovery cleanup batch per `skills/ha-nova/batch-safety.md`: one resolved device per manifest, topics taken only from `mqtt/device/debug_info`, never guessed or globbed; command/`set` topics always stay single-target.
- Windows are bounded by the relay (max 100 events / 10 s) — never claim continuous monitoring.
- Never guess a `device_id` or invent a payload schema; read what the device actually publishes first.
