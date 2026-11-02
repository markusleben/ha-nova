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
   - An unknown current level, or a missing or templated value, keeps the preview (fail closed).

   Everything else keeps its tier: `play_media` and its queue, `join`/`unjoin`, seek, sound mode, shuffle and repeat, turning a player on or off, and `tts.*`.
2. **Shopping list**: `todo.add_item` and `todo.update_item` (rename, complete, due date, description) are ordinary on any to-do list. `todo.remove_item`, `todo.remove_completed_items`, and list deletes keep their tier.
3. **Room lights**: `light.turn_on`, `turn_off`, or `toggle` on `--area <id>`, or on one group named by its entity_id, is classified like naming each light it reaches (no `EXPANDED_TARGET`) when all of these hold:
   - every reached entity is a `light.*` entity, and there are at most 10 (the cap on explicitly named ids, so an area or group never runs more at once than naming lights does);
   - no reached entity is without state (disabled, not loaded: `EXPANDED_TARGET_WITHOUT_STATE`) and resolution read everything;
   - no light's device, or its parent or child devices, carries a lock, alarm panel, or access cover;
   - every group on the way is a Home Assistant group (registry platform `group`): another platform's `entity_id` list (a template light) does not prove what switching it runs.

   What Home Assistant's own area targeting leaves out (hidden, configuration or diagnostic, assigned to another area) stays left out, as in the I5 exception. `--device`, `--match`, other domains, `homeassistant.*`, several named groups, and more than 10 lights keep `EXPANDED_TARGET`. The result names every light it read back.
4. **Kept previews**: an owned service that is now ordinary stays a preview (`OWNED_MEMBER`, as today) when its own class can't see everything it does:
   - the entity's device family carries a lock, alarm panel, or access cover;
   - a `universal` media player: its user-written commands may run anything instead;
   - a player grouped with others (`group_members`): the whole group follows, and its other players are not classified;
   - a stored step (a script, scene, or consumer automation) that raises or sets the volume or unmutes, also when it repeats the direct call: the plan-time level is not the level at that step.
   A write to the built-in shopping list (platform `shopping_list`) fires `shopping_list_updated`; its listeners are classified like a timer's, so one that unlocks makes the write typed.
5. **Ordinary is only the call's own class.** Members, consumers (an automation triggered by the player, the list, or a light), event listeners (`call_service`), device siblings, unregistered or template platforms, and limit cuts still raise the tier. A pause whose consumer automation unlocks a door stays typed.
6. **Unchanged**: the `--skill` routing (an owned service without its skill stays refused), chat-app levels (below Manage everything a to-do call stays refused), reason codes, and every confirmation text (`cli/confirm_strings.*.json`).

## Splitting

The new rules never make a part of a previewed request ordinary that was not already: each light, player, and list is still classified on its own (siblings, consumers, platforms), and a room is capped at the same 10 entities as named ids. `service-call/SKILL.md` keeps "never split a call to dodge it".

## Known limits

- A player that does not report `volume_level` (some step-only players) still asks for `volume_up`, `volume_set`, and unmuting.
- One `volume_up` step is assumed to be 0.1; an integration with a bigger step can end a little above 0.8.
- A room with more than 10 lights, or with a light on a door-lock device, still asks.
- A room light an integration provides that lists its lights (some Hue setups) is not a Home Assistant group: that room still asks.
- A grouped speaker (Sonos and similar) still asks for every media call; classifying its other players is a follow-up.
- A `universal` player keeps today's preview, not the typed tier an unread user-written command would get elsewhere (pre-existing).

## Tests

`cli/tier_fewer_asks.go` holds the rules. `cli/tier_fewer_asks_test.go`: each new ordinary case; each kept case (`play_media`, `join`, a big volume jump, a fractional one just past a limit, an unknown level, a player on a lock device, a `universal` or grouped player, a stored volume step, a `volume_up` the player's own automation repeats, `remove_item`, a room with a light on a lock device, a generic call on a room with a cover, a skipped or unregistered member, a template light listing lights, a device target, 11 lights); the consumer escalation (a pause, a to-do add, and a room's lights whose automation unlocks a door: typed; a shopping-list event listener that unlocks: typed). Existing tier tests change only where the decision changes (listed in the PR).
