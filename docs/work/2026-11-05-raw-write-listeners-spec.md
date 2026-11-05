# Raw writes check what listens to them

Status: active

Date: 2026-11-05. Issue #946 (found by the sibling sweep for #928, fixed in #940). Owner: tier engine (`cli/tier_raw_listeners.go`, `cli/tier_scan.go`), raw plans (`cli/plan_raw.go`), `ha-nova relay ws` (`cli/relay_raw_endpoint.go`). Target: 0.28.2. High-risk: typed tier.

## Today

`ha-nova call` classifies the automations whose triggers reference the entity it acts on (`scanConsumers`, `cli/tier_scan.go`): one that unlocks, opens a door, gate, or garage, or disarms makes the call typed. Since #940 a raw `todo/item/move` on the shopping list classifies the listeners of `shopping_list_updated` (`rawShoppingList`). Every other raw write is tiered by its payload alone (`rawRequestTier`) in `ha-nova plan raw` and `ha_plan` kind raw, and `ha-nova relay` sends it after the raw guard with no tier at all, although Home Assistant writes a new state or new attributes for the entity it changes, and a trigger on that entity sees it (hardening spec §4, "Indirect through allowlisted config").

## Design

1. **What a write changes** (`rawListened`, no reads): the entities whose state or attributes a raw write changes.

   | Write | Entities scanned |
   |---|---|
   | `calendar/event/create`, `update`, `delete` (one-off or recurring) | the calendar (calendar and state triggers) |
   | `<collection>/update` and `/delete` of `input_boolean`, `input_number`, `input_text`, `input_select`, `input_datetime`, `input_button`, `counter`, `timer`, `schedule`, `person`, `zone`, `tag` | the item's entity: the full registry row with that platform and the item id as `unique_id` |
   | `zone/create`, `person/create` naming a device tracker (already typed: `person_follows`) | `zone.home` (a smaller zone or a new person changes whom it counts) |
   | `config/core/update` setting `latitude`, `longitude`, `elevation`, `radius`, or `location_name`; setting `unit_system` or `update_units` | `zone.home` and the `sun` entities; every entity with a unit, and every climate, water heater, and weather entity (Home Assistant shows them in the system's units) |
   | `config/entity_registry/update` setting anything but `aliases`, `area_id`, `categories`, `hidden_by`, `labels` (a rename, enable, disable, name, icon, class, options); `config/entity_registry/remove` | the entity, and its new id on a rename; on an enable, every entity of its integration entry (Home Assistant reloads the entry 30 s later) |
   | `config/device_registry/update` setting anything but `area_id`, `labels` (a name, enable, disable); `config/device_registry/remove` | every entity of the device, disabled ones included; on an enable, disable, or removal its child devices' too (`deviceFamily`, as `rawDeviceFamily` refuses); on an enable, every entity of their entries |
   | `config_entries/disable`; `config_entries/update` setting anything (its update listeners run, and most reload the entry); `config_entries/subentries/update`, `delete` | every entity of the entry, disabled ones included |
   | core `POST` that starts an options, reconfigure, or subentry flow on an entry (the entry `rawAccessTarget` names) | every entity of the entry: the finished flow reloads it |
   | core `POST /api/config/scene/config/<id>` (a scene save) | every scene Home Assistant stores (platform `homeassistant`): the save reloads them all |

   When any of these is a person, a device tracker, or a zone, every person, every device tracker with a GPS position, and every zone is scanned too, read live from `/api/states` (`statesWhere`): a person follows its trackers, a zone moves people into or out of it, and a zone's state counts the people in it.

   A write Home Assistant refuses outright (no id, or an id that isn't a valid entity_id) changes nothing and is not scanned.
2. **Classified like a call's target** (`rawListeners`, next to `rawShoppingList` in `computeRawPlan`): each entity goes through `scanConsumers` at depth 0, so its consumers are walked with the same rules, limits, basis, and drift as a call's. The item's tier is the highest of its payload tier and what its listeners reach; without such a listener it keeps its current tier. Each entity's class is kept per plan, so a second item on it gets the same class.
3. **Fail closed.** A registry, state list, relation, or config that can't be read is typed (`SCAN_FAILED`; `LIMIT_CUT` past the read budget or 64 entities), and so is a helper whose entity the registry doesn't list (`UNRESOLVED_TARGET`). A disabled entity is not in the compact registry, so its device comes from the full one (`deviceOf`) and device triggers on it are found too.
4. **`ha-nova relay`** (`relayListeners`, after the raw guard): a `ws` or `core` write `rawListened` names, as the body or as a `collect_events` envelope's `message`, opens a session and runs the same check live. Typed (a listener reaches typed, or a read failed) is refused with exit 4 and nothing sent, and the message names the plan route; anything else is sent as before. The skills keep their routes.
5. **Where it shows.** The listener reasons are the engine's own (`ACCESS_LOCK` through the automation, and so on), so the plan's `say` words them with the existing confirm strings, and the consumers appear in the plan as a call's do.

6. **Reloads are scanned, not typed** (default chosen here): `ha-nova call` types every reload service (`RELOAD_UNENUMERATED`). The entry reloads a raw write causes (an enable, an entry update, a finished options flow, a scene save) are classified by what listens to the reloaded entities instead, since typing them would ask for the code at every entity enable or integration rename. What a reload runs inside the integration itself is not classified, as before.

## Tests

`cli/tier_raw_listeners_test.go`: each write class with an automation whose trigger references the changed entity and whose action unlocks a lock is typed with `ACCESS_LOCK`; the same write without it keeps its tier; a failed read is typed; a helper the registry doesn't list is typed; a write that changes no state (an area move) scans nothing; a disabled entity's device trigger is found; `relay ws` refuses the typed case (also in an envelope) and sends the rest.

## Known limits

- A **new** entity (a helper create, an integration flow) can take an id an automation already listens to. Home Assistant picks the id from the name, with a suffix on a collision; that id is not scanned.
- A flow is checked at its start, where it names its entry; its later steps are not checked again, and a flow the person opened in Home Assistant is not checked at all.
- An entity without a registry entry (no `unique_id`) is not listed for its device or entry, so it is not scanned there.
- A zone or home-location change, or a new person, scans every person, GPS tracker, and zone, so in a home with an arrival automation that unlocks or disarms it asks for the code even when nobody's zone changes.
- Other writes with small state side effects are not scanned: `energy/save_prefs` (cost sensors), `backup/generate*` and `backup/config/update` (backup sensors and events), `assist_pipeline/pipeline/*` (a satellite's pipeline select), HACS repository actions (`update` entities), and Supervisor App options (`auto_update` on the App's `update` entity).
- The bus event of a write (`entity_registry_updated`, `device_registry_updated`, `core_config_updated`) is not classified: Home Assistant's own listeners on these events make the live count that `checkEvents` compares useless, as before.
- An integration that follows the home location (a weather forecast) changes on its next refresh; only `zone.home`, the `sun` entities, people, and GPS trackers are scanned.
- A template trigger is found only when `search/related` lists it, as for a call (`TEMPLATES_NOT_CHECKABLE`).
- A big batch (many renames in one plan), or an integration, device, or flow with more than 64 entities, passes 64 scanned entities and then asks for the code (`LIMIT_CUT`); `ha-nova relay` refuses it.
