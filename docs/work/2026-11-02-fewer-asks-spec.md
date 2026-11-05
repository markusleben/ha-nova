# Fewer asks for everyday things: media, the shopping list, and room lights

Status: active

Date: 2026-11-02. Owner: tier engine (`cli/tier_fewer_asks.go`, called from `cli/tier_rules.go`, `cli/tier_engine.go`, `cli/plan_call.go`); skills `media`, `todo`, `service-call`. Target: 0.28.0 (App 0.13.10). Changes the shipped tier contract of the typed-verbs spec (`docs/work/2026-10-14-typed-verbs-spec.md`, I5 and §4) and `docs/reference/safety.md`.

## Today

- Every owned service runs from its skill as a preview (`OWNED_MEMBER`, `classifyAction`): pausing music, a little louder, or adding milk to the shopping list each ask first.
- An area or a group that reaches several entities is a preview (`EXPANDED_TARGET`, I5). Only an area or device with exactly one entity of the service's domain counts as naming it.

## Decision (Markus, 2026-11-02; binding)

"Medien, Einkaufsliste und Raum-Licht schon in 0.28.0 ohne Nachfrage": media controls, the shopping list, and a room's lights run without asking, starting with 0.28.0. Only things that are not really critical lose the question.

## Design

1. **Media** (from `ha-nova:media`, on a media player whose state was read): `media_play`, `media_pause`, `media_play_pause`, `media_stop`, `media_next_track`, `media_previous_track`, `select_source`, and muting are ordinary. Volume is ordinary when the change is moderate; the jump rule of `skills/media/SKILL.md` (Safety) moves into code:
   - `volume_down`: always.
   - `volume_set`: the new level is at most 0.8 and at most 0.3 above the current `volume_level`.
   - `volume_up`: the current level plus one step (0.1, Home Assistant's default) stays at most 0.8.
   - Unmuting (`is_volume_muted: false`): the current level is at most 0.8.
   - An unknown current level (none, outside 0..1, or only assumed: `assumed_state`), or a missing or templated value, keeps the preview (fail closed).

   Everything else keeps its tier: `play_media` and its queue, `join`/`unjoin`, seek, sound mode, shuffle and repeat, turning a player on or off, and `tts.*`.
2. **Shopping list**: `todo.add_item` and `todo.update_item` (rename, complete, due date, description) are ordinary on any to-do list. `todo.remove_item`, `todo.remove_completed_items`, and list deletes keep their tier.
3. **Room lights**: `light.turn_on`, `turn_off`, or `toggle` on `--area <id>`, or on one group named by its entity_id, is classified like naming each light it reaches (no `EXPANDED_TARGET`) when all of these hold:
   - every reached entity is a `light.*` entity, and there are at most 30 (`tierMaxRoomLights`, Markus 2026-11-02; explicitly named ids keep their cap of 10, `EXPLICIT_BATCH`);
   - no reached entity is without state (disabled, not loaded: `EXPANDED_TARGET_WITHOUT_STATE`) and resolution read everything;
   - no light's device, or its parent or child devices, carries a lock, alarm panel, or access cover;
   - every group on the way is a Home Assistant group (registry platform `group`) or a Hue grouped light (platform `hue` with `is_hue_group`), and each light it lists is still classified: another platform's `entity_id` list (a template light) does not prove what switching it runs.

   What Home Assistant's own area targeting leaves out (hidden, configuration or diagnostic, assigned to another area) stays left out, as in the I5 exception. `--device`, `--match`, other domains, `homeassistant.*`, several named groups, and more than 30 lights keep `EXPANDED_TARGET`. The result names every light it read back.
4. **Kept previews**: an owned service that is now ordinary stays a preview (`OWNED_MEMBER`, as today) when its own class can't see everything it does:
   - the entity's device family carries a lock, alarm panel, or access cover;
   - a `universal` or `media_player_template` player: its user-written commands may run anything instead;
   - a player whose `group_members` names another player, or is not a list: the whole group follows, and its other players are not classified;
   - a stored step (a script, scene, or consumer automation) that raises or sets the volume or unmutes, also when it repeats the direct call: the plan-time level is not the level at that step.
   A write to the built-in shopping list (a `todo.*` call on platform `shopping_list`, any `shopping_list.*` service, direct or stored, or a raw `todo/item/move` on it inside a raw plan, `ha_plan` kind raw or `ha-nova plan raw`) fires `shopping_list_updated`; its listeners are classified like a timer's, so one that unlocks makes the write typed. With no readable registry, every raw move is typed. The listener configs a raw plan walked are in its basis, so a listener changed before `apply` is drift (I3).
5. **Ordinary is only the call's own class.** Members, consumers (an automation triggered by the player, the list, or a light), event listeners (`call_service`), device siblings, unregistered or template platforms, and limit cuts still raise the tier. A pause whose consumer automation unlocks a door stays typed.
6. **Tool text**: the `area` argument of `ha_call` and `--area` in the CLI help now say that `light.turn_on|turn_off|toggle` reaching only a room's lights (up to 30) is not previewed, and `ha-nova call --help` lists a room's lights among the calls that run now. The hash in `TestMCPToolTextStable` changes, so chat apps ask once more to always allow `ha_call` (Markus, 2026-11-02).
7. **Unchanged**: the `--skill` routing (an owned service without its skill stays refused), chat-app levels (below Manage everything a to-do call stays refused), reason codes, and every confirmation text (`cli/confirm_strings.*.json`).

## Splitting

The new rules never make a part of a previewed request ordinary that was not already: each light, player, and list is still classified on its own (siblings, consumers, platforms). A room runs up to 30 lights, named ids up to 10: three calls of 10 named lights run what one room call would, with the same checks. Volume: the cap holds per call, so repeated small steps end at most at 0.8; anything louder previews however it is split. `service-call/SKILL.md` keeps "never split a call to dodge it", and `skills/media/SKILL.md` keeps "never split one into small steps".

## Known limits

- A player that does not report `volume_level` (some step-only players), reports it outside 0..1, or only assumes it still asks for `volume_up`, `volume_set`, and unmuting.
- One `volume_up` step is assumed to be 0.1; an integration with a bigger step can end a little above 0.8.
- A room with more than 30 lights, or with a light on a device (or its parent or child device) that has a lock, still asks.
- A large room can reach an engine limit; the plan then needs a code (`LIMIT_CUT`), as the same room's preview did before: more than 64 acted entities (30 lights plus more than 34 other entities their consumer automations switch), or the 30 s read budget. Measured on the fake with a delay per read: 30 lights without consumers took 1.9 s at 100 ms and 4.8 s at 250 ms; 30 lights that each start their own automation took 14 s at 100 ms and hit the budget at 250 ms (24 such lights: 28 s).
- A Hue room or zone (a v2 grouped light) is expanded and never sent itself: only the frozen light ids are, so a bulb without a Home Assistant entity stays as it was. A v1 Hue group (`is_hue_group` without an `entity_id` list) and ZHA, Zigbee2MQTT, and deCONZ group lights list no members, so they act like one named light, the same as naming one today.
- `ha-nova relay ws` with `todo/item/move` (the reorder `skills/todo/SKILL.md` uses on a computer) passes only the raw guard and is sent with no plan, tier, or listener scan (`cli/relay.go`), so a `shopping_list_updated` listener that unlocks is not seen there. The bus event of any direct raw write has the same gap (pre-existing since #258/#718; follow-up in 0.28.1). Closed for the move in 0.28.1 (#928): `ha-nova relay` refuses it, and the skill plans it; what listens to other raw writes stays unchecked on both raw routes. Closed for the entities a raw write changes in 0.28.2 (#946, `docs/work/2026-11-05-raw-write-listeners-spec.md`); a write's own bus event stays unchecked.
- A grouped speaker whose `group_members` names another player still asks for every media call; classifying its other players is a follow-up. Speaker groups that don't publish `group_members` (Google Cast groups, Alexa groups, players that drive another player such as Music Assistant or Spotify) run at once while the other speakers' automations are not checked; before this change their preview did not name such an effect either.
- A `universal` or `media_player_template` player keeps today's preview, not the typed tier an unread user-written command would get elsewhere (pre-existing).
- A media or to-do call that runs at once ends with "Sent … I can't check the result" (exit 0), as after a confirmed preview before; read-back probes come in 0.28.1.
- A kept preview on a grouped player names `OWNED_MEMBER` with an empty reason clause; naming the group needs a new confirmation text.
- `volume_set` from 0.9 down to 0.85 previews (0.85 is above the cap), though it lowers the volume.

## Tests

`cli/tier_fewer_asks.go` holds the rules. `cli/tier_fewer_asks_test.go`: each new ordinary case; each kept case (`play_media`, `join`, a big volume jump, a fractional one just past a limit, an unknown level, a player on a lock device, a `universal` or grouped player, a `media_player_template` player, a speaker listing another player (one listing only itself runs), a level outside 0..1 or assumed, unmuting with no level, a `group_members` that is no list, a stored volume step or unmute, a `volume_up` the player's own automation repeats, `remove_item`, a room with a light on a lock device or on its parent or child device, a generic call on a room with a cover, a skipped or unregistered member, a template light, or a Hue light that is no Hue group, listing lights, a device target, 31 lights); exactly 30 lights, also with a consumer automation each, run at once without a cut; a Hue room by name and inside its area; the consumer escalation (a pause, a to-do add, and a room's lights whose automation unlocks a door: typed; a shopping-list event listener that unlocks: typed for `todo.add_item`, `todo.update_item`, a `shopping_list.*` service, direct or stored, and a raw `todo/item/move`, `TestShoppingListMoveWakesItsListeners`, also per item with two lists, a failed event read, and no registry). `cli/mcp_serve_levels_test.go`: at Use the home, a moderate volume change and a room's lights run with no plan. Existing tier tests change only where the decision changes (listed in the PR).
