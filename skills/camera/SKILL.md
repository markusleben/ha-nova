---
name: camera
description: Use when working with Home Assistant cameras — taking a snapshot for the agent to look at, getting a stream URL, or recording — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Camera

## Scope

Camera access:
- fetch a still image so the agent (or the user) can actually look at it
- get a stream URL for live viewing in a browser
- trigger Home Assistant's own snapshot/record services (files land on the HA host)

Not in scope: creating automations around cameras (`ha-nova:write`), person/motion detection setup, or any image analysis claim beyond what the agent can see in the fetched frame.

## Bootstrap (once per session)

Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

Requires **Relay 0.3.0 or newer** (binary responses). Check `ha-nova relay health` -> `version`; if it is older, tell the user to update the NOVA Relay — an older relay would corrupt the image bytes.

## Relay Contract

- `ha-nova relay core --method GET --path /api/camera_proxy/<entity_id> --out-binary <image-file>` — the ONLY correct way to fetch a frame: the relay returns the image base64-marked and `--out-binary` decodes it to real bytes. Never use `--out` for an image (that writes the JSON envelope) and never pipe it through `--jq`.
- `ha-nova relay ws --data-file <payload-file>` for WS commands
- `ha-nova relay core --method POST --path /api/services/camera/<service> --body-file <payload-file>` for camera services

## Flow

1. Resolve the camera yourself (one skill per intent): WS `{"type":"config/entity_registry/list_for_display"}`, filter `ei` starting with `camera.`; for room phrasing resolve the area and use `search/related` with `item_type: "area"`. Ambiguity: present candidates (max 5), ask once.
2. Read its state (`/api/states/<entity_id>`): `state` (`idle`/`recording`/`streaming`/`unavailable`), `attributes.supported_features` (1 = ON_OFF, 2 = STREAM), `entity_picture`, `frontend_stream_type`. An `unavailable` camera cannot deliver a frame — say so instead of retrying.
3. **Snapshot for the agent to see**: fetch the current frame into a client-private scratch file with `--out-binary`, then open that file with the client's own image-reading tool. Only describe what is actually visible in the frame; never infer content you did not see.
4. **Stream URL** (needs STREAM, bit 2): WS `{"type":"camera/stream","entity_id":"camera.<id>"}` returns an HLS URL under `.data.url`. It is relative to the Home Assistant base URL and short-lived — give it to the user, do not try to consume it here.
5. **Snapshot/record to the HA host** (mutating): services `camera.snapshot` (`filename`) and `camera.record` (`filename`, `duration`, `lookback`). The path must be inside HA's `allowlist_external_dirs`, or the call fails — say this before the call rather than after. Preview the exact filename and confirm. These write files on the Home Assistant server, not on this machine.
6. `camera.turn_on` / `camera.turn_off` need ON_OFF (bit 1); verify by re-reading the state.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. Camera specifics:
- `--out-binary` refusing with "no base64 marker": the response was not an image — usually a 404 (wrong entity) or an error envelope. Re-run without `--out-binary` to read the actual error.
- Frames larger than the relay's 8 MiB binary ceiling fail loudly; that is a stream, not a still — use the stream URL instead.
- `camera/stream` errors when the camera has no STREAM feature: check bit 2 first.
- `camera.snapshot` failing with a path error means the target directory is not in `allowlist_external_dirs` — that is an HA configuration change the user must make in `configuration.yaml`.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output.

For a snapshot, describe what is visible in the frame and name the camera plus the time it was taken. For a stream, give the URL and say it is short-lived. Never claim to have seen something the frame does not show.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed token `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- A camera frame is private data: keep it in client-private scratch storage, never write it into the project workspace, and never send it anywhere outside this conversation.
- `camera.snapshot` / `camera.record` write files on the Home Assistant server — preview the exact path and confirm; they can overwrite an existing file.
- Do not fetch frames repeatedly to simulate a live view.

## Guardrails

- One camera per operation.
- Never use `--out` or `--jq` for an image; only `--out-binary`.
- Do not claim image content beyond what is visible in the fetched frame.
- No continuous polling; a stream URL exists for that.
