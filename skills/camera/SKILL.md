---
name: camera
description: Use when working with Home Assistant cameras — taking a snapshot for the agent to look at, getting a stream URL, recording, casting a camera stream to a TV or speaker display, or turning a camera or its motion detection on and off — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Camera

## Scope

Camera access:
- fetch a still image so the agent (or the user) can actually look at it
- get a stream URL for live viewing in a browser
- trigger Home Assistant's own snapshot/record services (files land on the HA host)
- switch a camera on or off where the entity supports it

Also here: switching a camera on or off, toggling its motion detection, and casting its stream to a media player.

Not in scope: creating automations around cameras (`ha-nova:write`), configuring the detection pipeline itself (zones, sensitivity, person models — that lives in the camera integration), or any image analysis claim beyond what the agent can see in the fetched frame.

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

Binary responses are guaranteed by the skills' enforced relay floor (`skills/ha-nova/relay-api.md` -> Bounded Event Collection). If any relay command printed a relay-outdated warning, have the user update the NOVA Relay before fetching frames — an older relay would corrupt the image bytes.

## Relay Contract

- `ha-nova relay core --method GET --path /api/camera_proxy/<entity_id> --out-binary <image-file>` — the ONLY correct way to fetch a frame: the relay returns the image base64-marked and `--out-binary` decodes it to real bytes. Never use `--out` for an image (that writes the JSON envelope) and never pipe it through `--jq`.
- `ha-nova relay ws --data-file <payload-file>` for WS commands
- `ha-nova relay core --method POST --path /api/services/camera/<service> --body-file <payload-file>` for camera services

## Flow

1. Resolve the camera yourself (one skill per intent): WS `{"type":"config/entity_registry/list_for_display"}`, filter `ei` starting with `camera.`; for room phrasing resolve the area and use `search/related` with `item_type: "area"`. Ambiguity: present candidates (max 5), ask once.
2. Read its state (`/api/states/<entity_id>`): `state` (`idle`/`recording`/`streaming`/`unavailable`), `attributes.supported_features` (1 = ON_OFF, 2 = STREAM), `entity_picture`, `frontend_stream_type`. An `unavailable` camera cannot deliver a frame — say so instead of retrying.
3. **Snapshot for the agent to see**: fetch the current frame into a client-private scratch file with `--out-binary`, then open that file with the client's own image-reading tool. Only describe what is actually visible in the frame; never infer content you did not see. Viewing the frame is a client capability — classify this check per context skill → Verification Planning (client capabilities) before promising it.
4. **Stream URL** (needs STREAM, bit 2): WS `{"type":"camera/stream","entity_id":"camera.<id>"}` returns an HLS URL under `.data.url`. It is relative to the Home Assistant base URL and short-lived — give it to the user, do not try to consume it here.
5. **Snapshot/record to the HA host** (mutating): services `camera.snapshot` (`filename`) and `camera.record` (`filename`, `duration`, `lookback`). The path must be inside HA's `allowlist_external_dirs`, or the call fails — say this before the call rather than after. Preview the exact filename and confirm. These write files on the Home Assistant server, not on this machine.
6. `camera.turn_on` / `camera.turn_off` need ON_OFF (bit 1); verify by re-reading the state. A restart/reboot ask is neither of these: find the device's own capability entity (often a disabled sibling) via `ha-nova:entity-discovery` → Device-sibling capability discovery.
7. **Cast to a screen**: `camera.play_stream` puts the live view on a TV or display. Payload: `{"entity_id":"camera.<id>","media_player":"media_player.<id>"}` — the receiver field is `media_player` (required), NOT `media_player_entity_id`, which belongs to `tts.speak`; `format` is optional and defaults to `hls`. Two entities, two gates: the camera needs STREAM (bit 2) and the receiver needs PLAY_MEDIA (bit 512), because HA forwards the stream URL to `media_player.play_media` internally. Resolve the receiver like any other target (registry lookup, ask once on ambiguity — never guess a player id) and read its state for that bit before previewing. Verify on the RECEIVER's state, not the camera's: this is the only camera action whose result shows up on another entity.
8. `camera.enable_motion_detection` / `camera.disable_motion_detection` toggle detection where the integration implements it. Verify via the `motion_detection` attribute when the camera exposes it; when it does not, say the toggle was accepted but could not be verified rather than claiming success.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. Camera specifics:
- `--out-binary` refusing with "no base64 marker": the response was not an image — usually a 404 (wrong entity) or an error envelope. Re-run without `--out-binary` to read the actual error.
- Frames larger than the relay's 8 MiB binary ceiling fail loudly; that is a stream, not a still — use the stream URL instead.
- `camera/stream` errors when the camera has no STREAM feature: check bit 2 first.
- `camera.snapshot` failing with a path error means the target directory is not in `allowlist_external_dirs` — that is an HA configuration change the user must make in `configuration.yaml`.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output. Write previews, delete confirmations, and results render as the Cards defined there.

Render the Report shape (output-rules.md). For a snapshot, describe what is visible in the frame and name the camera plus the time it was taken. For a stream, give the URL and say it is short-lived. Never claim to have seen something the frame does not show.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

- A camera frame is private data: keep it in client-private scratch storage, never write it into the project workspace, and never send it anywhere outside this conversation.
- `camera.snapshot` / `camera.record` write files on the Home Assistant server — preview the exact path and confirm; they can overwrite an existing file.
- Do not fetch frames repeatedly to simulate a live view.

## Guardrails

- One camera per operation.
- Never use `--out` or `--jq` for an image; only `--out-binary`.
- Do not claim image content beyond what is visible in the fetched frame.
- No continuous polling; a stream URL exists for that.
