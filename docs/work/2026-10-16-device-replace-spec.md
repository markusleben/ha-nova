# Device skill: replace, onboard, retire a device — Spec (2026-10-16)

Status: active

## Problem

Replacing a device (a dead Zigbee sensor, a new phone, a Govee lamp moved to
MQTT) means finding and re-pointing every place that names the old device or
its entities. Home Assistant has no swap feature. `search/related` (what
`ha-nova get --related` returns) sees automations, scripts, scenes, classic
groups, and persons that name an entity — but not:

- dashboards (storage and YAML mode),
- energy preferences (statistic ids),
- notify service names (`notify.mobile_app_<device>`),
- templates inside automations and scripts (`states('sensor.x')`),
- device-level references (`device_id` targets, device triggers/conditions/
  actions, which hold the entity REGISTRY id, not the entity_id),
- config-entry helpers (light/switch/sensor group helpers, min_max, utility
  meter, derivative, integral, threshold),
- registry-disabled automations and scripts (not loaded, so not indexed).

Premise correction (source-checked, HA 2026.10 `components/search`): person
device trackers and classic `group.*` members ARE covered by `search/related`.

## Behavior

### 1. Verb: `ha-nova get <entity_id> --references` (read-only)

One scan over the entity's whole device, so a swap needs one call per device.

- Needles: every entity of the device (registry rows by `device_id`; the given
  entity alone when it has no device), each entity's registry id, the
  `device_id`, and — for a `mobile_app` device — its notify service, checked
  against `GET /api/services`.
- A removed entity (no state, no registry row) is still scanned for its
  entity_id string: dangling references are what a swap after a deletion needs.
  `state` is null and a note says so. Without `--references` the old behavior
  (exit 5 NOT_FOUND) stays.
- Holders scanned, read-only:
  - every automation and script: the stored config (registry `unique_id`), or
    the loaded config (WS `automation/config`/`script/config`) for YAML items;
    registry-disabled items too;
  - every Lovelace dashboard from `get_panels` (`lovelace/config`; an
    auto-generated dashboard has no config and nothing to re-point);
  - `energy/get_prefs`;
  - every state's attributes (`/api/states`): scenes, groups, group helpers,
    persons (`device_trackers`), min_max, utility meter/derivative/integral
    `source`, threshold.
- Matching: whole-id boundaries (`sensor.temp` never matches
  `sensor.temp_2` or `binary_sensor.temp`; `states.sensor.temp.state`
  matches), in string values AND object keys (scene `entities:`). Each hit:
  family, holder id, config id, `managed: yaml` where it is not UI-editable,
  needle, JSON paths, `template: true` when a matched string is a template.
- Coverage: `scanned` counts per family; `not_scanned` names every holder or
  family whose read failed (never a silent zero); `not_covered` names the
  classes no read can see (template helpers and YAML template entities,
  helpers that show no source attribute, YAML-only integrations, Node-RED /
  AppDaemon, protocol-level groups and bindings in ZHA/Zigbee2MQTT).
- `--references` includes `related` (search/related) too.
- Read allowlist (I1) grows by `get_panels`, `lovelace/config`,
  `energy/get_prefs` — all reads.

### 2. Skill `ha-nova:device`

Three flows: **swap** (main), **onboard**, **retire**.

Swap:
1. Resolve old and new device; scan both with `get --references` BEFORE the
   old device is removed (afterwards its device id is gone).
2. Map old → new entities by domain, device class, translation key, unit,
   and the original name without the device prefix. One-to-one; anything
   ambiguous is one question; an old entity without counterpart that has
   references is named, never silently dropped.
3. Choose the path (`smallest-solution.md`): the **entity_id takeover** is
   smaller when the old device is gone for good and its entities map 1:1 in
   the same domain — remove the old device first (HA UI; leftovers via
   `ha-nova:maintenance`), then rename each new entity to the old entity_id
   (registry `new_entity_id`). Every entity_id reference — dashboards,
   energy, groups, persons, templates — keeps working, and history and
   long-term statistics continue (the recorder keeps the old id's rows).
   Renaming the old entity away instead would carry its history to the
   renamed id — never used for a takeover. **Rewrite** consumers instead when
   the old device stays in use, the domain differs, or the user wants the
   new ids.
   Either way, device-level references and notify service names are
   rewritten.
4. One plan card: the mapping, every hit by family with its owning route,
   coverage, the path and why.
5. Execute:
   - automation/script rewrites: `ha-nova plan write` + `apply`, a grouped
     change set (`grouped-change-set.md`); the CLI computes each plan's tier
     from the performed actions (an automation that unlocks a door is typed,
     whichever part changed); a typed plan leaves the group. At most 5
     automation/script updates per group, because the undo store keeps the
     last 5 targets; a swap with more offers a backup first.
   - takeover renames: registry updates per `organize` (metadata snapshot,
     consumer check). The takeover's tier is the highest tier of its
     automation/script consumers — planned unchanged with `plan write`, never
     applied — and typed for an access-capable entity (lock, alarm panel,
     access cover, or any entity on such a device): a takeover silently
     re-points every unlock, disarm, or open that names the old id, and a new
     phone tracker can become the one that opens the door.
   - every other family hands off to its owning skill with exact targets and
     the mapping (scene, dashboard, energy, admin, helper, yaml-config,
     assist) — their previews, snapshots, and tiers unchanged.
6. Verify: re-scan the new device; every holder of the old scan appears in
   it or was explicitly dropped; in the rewrite path the old device's re-scan
   is empty.

Partial failure: grouped sets stop at the first failure with a ledger (never
atomic); each applied update stays revertible. A takeover whose rename fails
after the old device was removed leaves the same state as the dead device
(references to a missing id) — retry the rename; nothing half-written.

Onboard: name, area, labels (`organize`); find where same-area siblings of
the same domain are used (`get --references` on one sibling) and offer to add
the new entity there through the owning skills; identify with a short blink
(`ha-nova call`, ordinary) when the device supports it.

Retire: `get --references` on the device, then `safe-refactoring.md` →
Pre-Delete Impact Check; removal of a live device stays in the HA UI.

## Not in scope

- New write verbs; protocol re-pairing; copying ZHA/Z2M bindings or groups;
  renaming the mobile_app notify service itself; Assist exposure edits
  (handed to `ha-nova:assist`).

## Tests

- Go: needle boundaries (keys, values, templates, prefix/suffix
  collisions), device expansion, registry id + device id needles, notify
  service resolution, dangling entity scan, every holder family, failed reads
  → `not_scanned`, I1 (only allowlisted reads).
- Skill contracts: template, Safety Core, word budget, dispatch rows,
  grouped matrix row, English-only.
- E2E on the local test bed: swap a seeded device, verify the scan and the
  agent path.
