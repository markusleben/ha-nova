---
name: media
description: Use when controlling or browsing Home Assistant media players — play, pause, volume, source/sound mode, speaker grouping, browsing media libraries, and TTS announcements — through HA NOVA Relay.
license: MIT
compatibility: Requires the ha-nova CLI (run 'ha-nova setup' first) and the HA NOVA Relay in Home Assistant (App, or standalone container on Container/Core).
---

# HA NOVA Media

## Scope

Media player control and discovery:
- transport control (play, pause, stop, next, previous, seek), volume, mute, source and sound mode
- browsing media libraries and media sources, then playing a resolved item
- speaker grouping (join / unjoin)
- TTS announcements to a player

Not in scope: creating automations that use media players (`ha-nova:write`), camera streams (`ha-nova:camera` once shipped), generic service calls to other domains (`ha-nova:service-call`).

## Bootstrap (once per session)

Read and follow `../ha-nova/session-bootstrap.md`.
Verify relay CLI: `ha-nova relay health`
If this fails: `ha-nova setup`

## Relay Contract

- `ha-nova relay ws --data-file <payload-file>` for WS commands (browse, search)
- `ha-nova relay core --method POST --path /api/services/<domain>/<service> --body-file <payload-file>` for service calls
- `ha-nova relay core --method GET --path /api/states/<entity_id>` to read a player's state and attributes
- `--out <result-file>` for large browse responses; `--jq-file <filter-file>` for non-trivial filters

## Feature Gate (read before acting)

A media player's `attributes.supported_features` bitmask decides what it can do — never assume. Read the entity state first and check the bit before offering an action:

| Bit | Feature |
|-----|---------|
| 1 | pause |
| 2 | seek |
| 4 | volume set |
| 8 | volume mute |
| 16 | previous track |
| 32 | next track |
| 128 | turn on |
| 256 | turn off |
| 512 | play media |
| 1024 | volume step |
| 2048 | select source |
| 4096 | stop |
| 8192 | clear playlist |
| 16384 | play |
| 32768 | shuffle set |
| 65536 | select sound mode |
| 131072 | browse media |
| 262144 | repeat set |
| 524288 | grouping |
| 1048576 | announce |
| 2097152 | enqueue |
| 4194304 | search media |

If a requested feature bit is absent, say so plainly instead of calling the service and reporting a silent no-op.

## Flow

1. Resolve the player yourself — this skill owns its resolution (one skill per intent; never dispatch to another skill mid-task):
   - exact `media_player.<id>` from the user: use it directly.
   - name or room: WS `{"type":"config/entity_registry/list_for_display"}`, filter `ei` starting with `media_player.`, and match on `ei`/`en`. For room phrasing, resolve the area and use `search/related` with `item_type: "area"`.
   - several matches: present the candidates (max 5) and ask once. Never guess.
   Then read its state (`/api/states/<entity_id>`) for `state`, `supported_features`, `source_list`, `sound_mode_list`, `group_members`, and current media attributes.
2. Transport / volume / source: check the bit for the exact action first (play 16384, pause 1, **stop 4096**, next 32, previous 16, seek 2, volume set 4, **volume step 1024**, mute 8, source 2048, sound mode 65536, turn on 128 / off 256), preview the service + target, then call it: `media_player.media_play|media_pause|media_stop|media_next_track|media_previous_track|media_seek` (`seek_position` in seconds; read `media_duration`/`media_position` from the state first) `|volume_set` (`volume_level` 0.0-1.0) `|volume_mute|select_source|select_sound_mode|turn_on|turn_off`.
   - **Relative volume** ("turn it up / down a bit"): that is `media_player.volume_up` / `volume_down`, gated by VOLUME_STEP (1024) — a player can support stepping without absolute `volume_set` (4). Prefer stepping for relative requests, and only fall back to `volume_set` (read the current `volume_level`, add a delta) when the player has 4 but not 1024.
3. Browse: WS `{"type":"media_player/browse_media","entity_id":"media_player.<id>"}` for the player's own library; drill down by passing `media_content_type` AND `media_content_id` together (they are a pair — one without the other is rejected). For Home Assistant's media sources use WS `media_source/browse_media`, then WS `media_source/resolve_media` to turn a `media-source://...` id into a playable URL.
   - **Search before drilling** when the user names a title, artist, or album: WS `{"type":"media_player/search_media","entity_id":"media_player.<id>","search_query":"<text>"}` (bit 4194304) searches the player's own library — `entity_id` is required, and `media_content_type`/`media_content_id` scope the search only when supplied together; WS `media_source/search_media` with `search_query` (plus optional `media_content_id` to scope and `media_filter_classes` to narrow) searches Home Assistant's sources, and only sources whose `can_search` is true participate. Both return browse-shaped results you can hand straight to step 4. Neither is universal: when the bit is missing or a source cannot search, say so once and fall back to browsing rather than guessing a content id.
4. Play a resolved item: `media_player.play_media` with `media_content_id` + `media_content_type` from the browse/resolve step. Never invent a content id.
   - Queue placement uses the same service: `enqueue: add | next | play | replace` (bit 2097152). "Play this next" is `next`, "add to the queue" is `add`; without the bit the player only replaces what is playing — say that instead of silently replacing.
   - Playback modes are separate services: `media_player.shuffle_set` (`shuffle` boolean, bit 32768) and `media_player.repeat_set` (`repeat: off | all | one`, bit 262144 — not the grouping bit). "Repeat this song" is `one`, "repeat the album" is `all`.
5. Grouping (bit 524288 — NOT 262144, which is repeat): `media_player.join` with `group_members: [<entity_ids>]` on the master; `media_player.unjoin` to remove a player. Read `group_members` back to verify.
6. TTS announcement: `tts.speak` with `entity_id` of a TTS entity, `media_player_entity_id` of the target, and `message`. Discover engines with WS `{"type":"tts/engine/list"}`. For a legacy setup without TTS entities, fall back to the integration's own `tts.*_say` service and say which one you used.
7. Verify by re-reading the player's state (`state`, `volume_level`, `source`, `media_title`) — never report success from the service response alone. Streaming players may show `buffering` or a briefly unchanged state; wait a few seconds and re-read once before reporting a discrepancy.

## Error Handling

Full relay/upstream error taxonomy: `skills/ha-nova/relay-api.md` -> Error Handling. Media specifics:
- WS `media_player/browse_media` fails with a command error when the player does not support browsing — check bit 131072 first.
- `media_content_type` and `media_content_id` must be provided together; a lone id is rejected.
- A service call can succeed while the player ignores it (offline speaker, wrong source): always verify by re-reading state.
- `media_player/search_media` and `media_source/search_media` exist only on newer Home Assistant versions and only for some integrations or sources — feature-detect (bit 4194304, `can_search`), and degrade to browsing instead of guessing.

## Output Format

Apply `skills/ha-nova/output-rules.md` to all user-facing output. Write previews, delete confirmations, and results render as the Cards defined there.

Render the Report shape (output-rules.md): what is playing where — player, state, current media, volume, source, and group members when relevant. Browse results render the List Frame (title + type), never the raw tree.

## Safety

- Preview before write: nothing is saved until the user confirms the shown preview.
- Confirmation binds to the displayed preview and expires on any change to target, payload, endpoint, or scope (context skill → Active Preview Confirmation).
- Pre-preview phrases ("do it", "go ahead", "implement the plan") authorize drafting and preview only — never the write itself.
- Delete and destructive operations require the typed confirmation code `confirm:<token>` verbatim; "yes" or any natural-language reply is invalid.
- Never guess entity, service, or config IDs — resolve them or ask.
- Home Assistant is reached exclusively through `ha-nova relay`.
- For any HA write this skill does not cover, STOP and invoke `ha-nova:fallback` first — never probe unfamiliar write endpoints.

- Drafts follow `skills/ha-nova/smallest-solution.md`: the complete requested outcome in the simplest safe design, nothing for hypothetical future needs.

- Volume: preview the target level; treat a large jump (or anything above ~0.8) as disruptive and confirm explicitly — a loud announcement at night is a real-world side effect.
- Announcements interrupt whatever is playing; say so before sending one.
- An announcement or TTS to a grouped player plays on every member — name the full member list in the preview, not just the target.
- Grouping changes affect every listed speaker — preview the full member list.

## Guardrails

- Read `supported_features` before offering an action; never call a service the player cannot do.
- One player (or one explicit group) per operation.
- Never invent `media_content_id` values — they come from browse/resolve.
- Browse responses return ALL children of a node — there is no paging parameter. Bound the OUTPUT, not the request: show at most ~20 children per level, say how many more exist, and ask the user to narrow (drill into a folder, name a title) instead of listing on.
