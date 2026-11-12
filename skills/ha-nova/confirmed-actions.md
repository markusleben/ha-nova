<!-- Generated from cli/confirmed_actions.go. Do not edit by hand: go test -run TestConfirmedActionsDoc -update ./cli rewrites it. -->

# Actions A Person Confirms Themselves

Generated from `cli/confirmed_actions.go`; do not edit by hand. **The CLI decides, not this list.** Whether a plan needs the person's own confirmation is the plan's `tier` and `plan.confirm`, and how it asks is its `say` and `next_step`: follow those. This list only helps you explain what such a confirmation is about. An action missing here can still need one, a listed one can be ordinary in a given home, and a plain "yes" in chat never stands in for one.

The person confirms in a window on their computer, on their linked phone, or by typing the four-digit code from HA NOVA, as the plan's `say` asks.

The examples read as single actions, but running or saving an automation, script, or scene that reaches a listed action is rated the same way, and so is a read HA NOVA needs for its check that fails. If the person asks which actions need this, answer in plain words in their language; never show the reason codes or this table.

## Locks, doors, gates, and alarms

| Example | Reason |
|---|---|
| Unlock or open a door lock | `ACCESS_LOCK` |
| Disarm an alarm panel | `ACCESS_ALARM` |
| Open the garage door or a gate | `ACCESS_COVER` |
| Open a cover that has no “Show as” class, so it could be a door | `COVER_CLASS_UNKNOWN` |
| Run an integration's service on a device that also has a lock | `DEVICE_ACCESS` |
| Press a button on a device that also has a door lock | `DEVICE_ACCESS_SIBLING` |
| Switch the relay behind a lock or door made with Switch as X | `SWITCH_AS_X_ACCESS` |
| Activate a scene HA NOVA can't read from an integration that also controls locks | `INTEGRATION_SCENE_ACCESS` |
| Give a lock or alarm panel a new ID, area, or label | `RAW_TYPED` (access_entity) |
| Move a device with a lock or door to another area | `RAW_TYPED` (access_device) |
| Change the label or floor of an area with a lock or door | `RAW_TYPED` (access_area) |
| Make a hidden lock or door visible or enabled again | `RAW_TYPED` (access_shown) |
| Give a cover that may be a door, gate, or garage a new ID, area, or label | `RAW_TYPED` (access_entity_cover) |
| Move a device with a possible door, gate, or garage cover to another area | `RAW_TYPED` (access_device_cover) |
| Change the label or floor of an area with a possible door, gate, or garage cover | `RAW_TYPED` (access_area_cover) |
| Make a hidden possible door, gate, or garage cover visible or enabled again | `RAW_TYPED` (access_shown_cover) |
| Change which devices a person follows | `RAW_TYPED` (person_follows) |

## Deleting and removing

| Example | Reason |
|---|---|
| Delete an automation, script, or dashboard | `CONFIG_DELETE` |
| Press a factory-reset or wipe button on a device | `DESTRUCTIVE_BUTTON` |
| Delete a helper, scene, area, label, or other item, or clear statistics | `RAW_TYPED` (delete) |
| Empty a list of the Energy dashboard or drop several of its entries | `RAW_TYPED` (energy_emptied) |
| Keep fewer automatic backups, so older ones are deleted | `RAW_TYPED` (backup_retention) |
| Remove an integration and its devices | `RAW_TYPED` (integration_remove) |
| Delete a file in the config folder | `RAW_TYPED` (file_delete) |
| Delete a saved config copy | `RAW_TYPED` (copy_delete) |
| Remove an App | `RAW_TYPED` (app_uninstall) |
| Uninstall something from the Marketplace | `RAW_TYPED` (marketplace_remove) |

## Installs, reloads, and code that runs

| Example | Reason |
|---|---|
| Install an update from HACS or the Marketplace | `HACS_INSTALL` |
| Publish an MQTT message, purge the recorder, start or stop an App, or reboot the host | `OWNED_TYPED_MEMBER` |
| Run a shell command, Python script, or REST command, or use an entity that runs a command or request | `UNREAD_COMMAND` |
| Call a service defined in an ESPHome device's own YAML | `USER_AUTHORED_SERVICE` |
| Restart Home Assistant, or reload anything beyond one automation or script | `RELOAD_UNENUMERATED` |
| Save an automation that watches service calls or phone notifications | `CONFIRMATION_WATCH` |
| Download a HACS repository | `RAW_TYPED` (hacs_install) |
| Add a dashboard resource (code every browser runs) | `RAW_TYPED` (resource_create) |
| Change a dashboard resource | `RAW_TYPED` (resource_update) |
| Write a file in the config folder | `RAW_TYPED` (file_write) |
| Put a deleted image or video back in the www folder | `RAW_TYPED` (file_restore) |
| Accept the Marketplace warning for HA NOVA's Home Assistant user | `RAW_TYPED` (marketplace_warning) |
| Install or update something from the Marketplace | `RAW_TYPED` (marketplace_install) |
| Install an App | `RAW_TYPED` (app_install) |
| Change an App's settings | `RAW_TYPED` (app_settings) |

## Accounts, backups, and system settings

| Example | Reason |
|---|---|
| Create a Home Assistant account | `RAW_TYPED` (account_create) |
| Change what an account may do, or whether it can sign in | `RAW_TYPED` (account_access) |
| Change where automatic backups go | `RAW_TYPED` (backup_locations) |
| Set up or change a backup location | `RAW_TYPED` (backup_agent) |
| Change the time zone or the unit system | `RAW_TYPED` (core_clock) |
| Change Home Assistant's language | `RAW_TYPED` (core_language) |
| Turn an experimental feature on or off | `RAW_TYPED` (labs) |
| Import long-term statistics | `RAW_TYPED` (statistics_import) |
| A step the skill itself marks as needing the person's confirmation | `RAW_TYPED` (skill_typed) |

## What HA NOVA can't fully check

| Example | Reason |
|---|---|
| Switch a template entity whose action can't be read | `TEMPLATE_ACTION_UNREAD` |
| Switch or press an entity defined in YAML without a unique ID | `UNREGISTERED_ACTUATOR` |
| Press an ESPHome or MQTT button | `USER_AUTHORED_BUTTON` |
| Run a step whose service name is a template | `TEMPLATED_ACTION` |
| Run a step whose target is a template, “all”, or can't be resolved | `UNRESOLVED_TARGET` |
| Run a stored step that targets a floor or label | `STORED_SELECTOR` |
| Run an automation whose blueprint can't be resolved | `BLUEPRINT_UNRESOLVED` |
| Run a YAML automation without an ID that can't be read | `YAML_NO_ID` |
| Run an automation or script whose configuration can't be read | `UNREAD_CONFIG` |
| Act where something else may listen that HA NOVA can't list | `UNENUMERABLE_LISTENER` |
| Act when a read during the check failed | `SCAN_FAILED` |
| Call a service when Home Assistant's list of services can't be read | `SERVICE_LIST_UNREAD` |
| Stop or restart an App when HA NOVA can't read enough to rule out its own | `HASSIO_NOT_SUPPORTED` |
| Act when the check hit a size or time limit | `LIMIT_CUT` |
