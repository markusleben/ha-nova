# Dead-reference sweep for automations and scripts (#978)

Status: active (release/0.36, HANO-84). KISS-shaped by gpt-6.2-sol (high).

## Problem
"Check all my automations for broken entities" ran through the bulk audit's Static Pass: one `ha-nova get <id> --config` per automation plus one `ha-nova get` per id it could not find in the states. 140 automations meant 140+ CLI runs, each with its own session. `ha-nova get <id> --references` answers what uses one item, not what is broken across the home.

## What the user asks
"Do my automations use things that no longer exist?", "which scripts still name the old Hue bridge?", "check all my automations for broken entities".

## What the agent does
1. One read-only call: `ha-nova get --dead-references` (a sibling of `get --dashboards`, the dashboard broken-card sweep).
2. A List Frame grouped by status with exact counts (missing, disabled, not loaded, unavailable right now), each row with the automation or script by name, where the reference sits (trigger, condition, action, template), and one suggested fix (a clear replacement, remove the reference, re-enable, check the integration).
3. Fixes the user picks go through the normal write flow: `ha-nova:write` for one, one `plan batch` for 2 to 20. The sweep writes nothing.

The bulk audit's Static Pass uses the same call instead of a `get` per id (`skills/ha-nova/bulk-patterns.md`). The review skill routes a request that is only about broken references to the sweep.

## CLI: `ha-nova get --dead-references` (read-only)
- Lists, read together and each once: `/api/states`, the entity registry, device registry, merged device ids (`list_composite_splits`, HA 2026.9+), area registry, `/api/services`. Any of them unread fails the sweep: a reference checked against an unread list would look missing.
- Holders: every automation and script with a state or a registry row. A loaded one is read as Home Assistant runs it (`automation/config`, `script/config`; blueprints already filled in), all of them at once through the MAR-180 ws batches (16 per request, single reads on an older App). One without a state (registry-disabled, not loaded) is read from its stored config; a stored blueprint instance is filled in (`blueprint/substitute`) and its rows carry `managed: blueprint`. An unreadable config is a `not_scanned` row, never clean.
- References at the positions Home Assistant reads them, literal only: a step's, trigger's, condition's, or target's `entity_id` (string, comma list, list; a 32-hex registry id in a device step resolves through the registry), `device_id` (a merged pre-2026.9 id still resolves), `area_id`, `scene` and `zone` ids, a `numeric_state`'s `above`/`below` entity, a step's `action`/`service`; the top level of a step's `data` (`entity_id`, `*_entity_id` such as `tts.speak`'s `media_player_entity_id`, `device_id`, `area_id`) and the entity keys `scene.apply`/`scene.create` set; a `state_changed` event filter's `entity_id`. Ids inside `variables`, event payloads, `data` below its top level, and script `fields` are no references. Template entity ids count anywhere, as complete literal arguments of `states()`, `is_state()`, `state_attr()`, `is_state_attr()`, `has_value()`, every argument of `expand()`, and `states.<domain>.<id>` (`states('sensor.' ~ x)` is not one). Templated ids and `all`/`none` are skipped.
- Shape: a list that comes back as `null` is unread (fails the sweep), an empty list is empty; a config without its triggers and actions (a script: its sequence) is `not_scanned`. No count cap: every holder is checked, stored configs at most 8 reads at a time.
- Status: entity `missing` (no state, no registry row), `disabled` (registry), `not_loaded` (a row without state), `unavailable` (state); device `missing` or `disabled`; area and action `missing`.
- Result: `{scanned: {automation, script}, findings: [{holder, name, config_id, managed, kind, ref, entity_id, status, paths, template}], not_scanned: [{family, id, reason}], not_covered: [...]}`.

## Shaping decisions
- Taken from Sol: loaded configs first (batched) instead of the stored-first reads `get --references` keeps; position-aware walk instead of arbitrary dotted text; floor and label targets and fuzzy replacement ranking deferred (not binding; the skill proposes a replacement with `ha-nova find` only when one clearly fits).
- Not taken: exposing it as `ha_get.dead_references` to chat apps now. Any tool-text change resets every claude.ai user's "Always allow" (`TestMCPToolTextStable`); it waits for a train that changes the tool text anyway.

## Not in scope
Dashboards (`get --dashboards`), scenes, helpers, YAML-only integrations, floor and label targets, automatic fixes.

## Proof
- Go (`cli/verb_get_dead_refs_test.go`): every kind and position (numeric_state, zone, scene.apply, `*_entity_id`, state_changed filters), registry ids, merged device ids, templates (complete literals only, every `expand()` argument), ids in variables, event payloads, and nested data skipped, disabled item from its stored config, unreadable one named; 140 loaded automations in 9 batches with the same findings as single reads and every list and config read once; each unread or `null` list (WS and REST) fails the sweep; an empty config is `not_scanned`; usage.
- Skill contracts: the Static Pass and review routing.
- End-to-end agent run on the test bed with seeded broken references.
