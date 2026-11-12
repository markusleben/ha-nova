# The CLI and the gate rate skill-typed raw writes themselves (raw-write typed parity)

Status: active (built for 0.31, plan item 1)

Date: 2026-11-12. Owner: the raw tier (`cli/tier_raw_takeover.go`, `cli/tier_raw_listeners.go`, `cli/plan_raw_tier.go`), which `ha-nova plan raw`, `ha-nova relay`, and the App's gate (`gateVerdict` → `gateLiveTier`) share. High-risk: tiers on every raw route.

## Problem

A raw plan item may carry `"typed": true`: the skill raises it where only the skill knows the tier. The mark lives in the plan file, not in the request, so `ha-nova relay` and the App's gate never see it. On a gated App such a plan keeps the computer's own asks (`gateChannel`, `markTyped`), but the same request sent bare (any client holding a computer's key) passed the gate as a preview. 0.30 states it as a limit: "a rename that takes over an entity an unlocking automation points at passes it".

Every place a skill sets the mark (`grep -rn typed skills`):

| Skill step | Rated by the CLI itself before | 0.31 |
|---|---|---|
| `write`: `blueprint/delete` | yes (`delete`) | same |
| `integration-setup`: entry DELETE | yes (`integration_remove`) | same |
| `energy`: a save that empties a list or drops several entries | yes (`energy_emptied`) | same |
| `device` Access Check: a hand-off of a person's trackers | yes (`person_follows`) | same |
| `device` Access Check: a takeover rename (`new_entity_id`) whose id an automation or script with a typed action names, also through a group or person | no (only an access entity itself, and listeners whose triggers match, #946) | rule 1 |
| `device` Access Check: an area or label carried onto an entity, where an automation's trigger targets that place | no (area and labels counted as stateless) | rule 2 |
| `health`: a repair fix flow that deletes or removes (picked from the issue's translated text) | no (repair flows previewed) | rule 3 |
| `integration-setup`: a destructive subentry reconfigure | no (subentry flows previewed) | rule 3 |

## Change

1. **Takeover** (`rawTakeover`). `config/entity_registry/update` that sets `new_entity_id`: `search/related` for the new id (Home Assistant 2026.11 `search` lists automations, scripts, groups, and persons that name an id whether or not an entity holds it), and for each group or person it lists, theirs, following a group in a group (each holder read once, counted against the budget). Every automation listed is walked as a consumer whatever its triggers (`consumer`, `tierMaxConsumers`), every script as a run (`expandRun`, counted against the same budget); a typed reach makes the write typed, a failed read or the budget cut is typed.
2. **Place carry** (`rawCarry`). An entity or device registry update that sets `area_id` or `labels`, or an area registry update that sets `floor_id` or `labels`: the places it newly sits in, after minus before. After: the new area (an entity or device whose own area is cleared sits where its device or parent device does), that area's floor and labels, the labels it carries; for an area, its floor and labels. Before: the same from the live registries; an entity the compact registry leaves out (disabled) counts as in no place, so all of after is walked. A rooms plan (`computeRoomsPlan`) rates each device move with the same walk; a typed one asks once through the Relay for every move (`gateFirstRequests`), like a batch. Every automation `search/related` lists there whose triggers target one of them (`targetsPlace`, MAR-142) is walked as a consumer. `rawListened` now answers a place-only update (no ids), so `ha-nova relay` and the gate run this live scan too; before, `rawStateless` skipped it on every route.
3. **Flows only the integration knows** (`rawRequestTier`). Typed: a repair fix flow's start and each step (`POST /api/repairs/issues/fix[/<flow_id>]`), a subentry flow's step (`POST …/subentries/flow/<flow_id>`), and a subentry reconfigure start (its body names `subentry_id`). The gate keeps no state to tell a flow id's issue or kind, and Home Assistant runs the integration's first step at the start (`RepairsFlowManager`). A subentry add's start, and cancelling (DELETE), stay previews; the by-id GET stays refused at the gate.

The skill mark stays accepted as a floor (plan file schema); dropping it from the skills is a follow-up that needs a bed agent run.

## Cost and UX

- Every repair fix asks twice (start and step: window, phone, or the App's ask), also a harmless one; every subentry add asks once per step. Repairs and subentries are rare admin work; the alternative (rating the start by English translation text) can't bind the step.
- A takeover rename reads one `search/related` per id, group, and person, and the configs they list.
- An area, floor, or label change reads the registries and each new place's `search/related`; it asks only where an automation whose trigger targets that place has an action that needs confirmation (or one of its reads fails).
- A typed rooms plan of more than 20 devices on a gated App is refused before anything moves (`GATE_ASK_TOO_LARGE`: one ask covers 20 requests); a rollback after a failed move asks again if the Relay holds it.

## Known limits (stay)

- The id's previous device: a takeover can't see which device the id sat on before (the old entity is gone), so "a door on the old device" stays the skill's check (its mark still raises the plan).
- References `search/related` doesn't index (computed in a template).
- A new entity an integration creates under an id an automation already names (not a write HA NOVA sends).
- A trigger that targets the place only through a condition (as MAR-142).

## Tests

- Go `cli/tier_raw_takeover_test.go`: each takeover and place case typed and a control kept as a preview, on plan raw, `gateVerdict`, and `ha-nova relay` (refused vs sent); failed reads typed. `TestPlanRawTierCoversEveryAllowlistedRequest` and `TestPlanRawBatch2FlowParity` pin the flow rules; `TestRelayTypedReadsOnlyWhenTheTierNeedsIt` serves the area read a move now makes.
- Bed: none new; the gate runs the same `rawListeners` the plan tests cover, and the gate lane already proves a typed raw write is held.

## Review

- KISS (Sonnet): keep rules 1 and 3, reuse `consumer`; rule 2 via the existing place machinery or defer. Built as a small new walker beside `rawListeners` (both reuse `consumer`, `targetsPlace`, `relatedItems`' place rows).
- Astra (gpt-6-astra medium): agrees with moving these into the shared classifier and with rule 1's `search` assumption; four corrections, all taken: count every walked config against the budget; include the destination area's labels and a cleared area's device area; make `rawListened` answer place-only updates, or relay and gate skip them; type repair starts too (the integration runs code there); add the subentry reconfigure the first table missed. It names two maintainer decisions: the extra repair and subentry asks, and conservative typing of opaque subentry flows (its recommendation: conservative, as built).
