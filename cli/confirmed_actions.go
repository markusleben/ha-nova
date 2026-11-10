package main

import (
	"fmt"
	"strings"
)

// The one list of what the tier engine can rate typed: a plan the person must
// confirm themselves (window, linked phone, or the four-digit code). The
// engine decides; this table only explains. It feeds
// skills/ha-nova/confirmed-actions.md (renderConfirmedActionsDoc, pinned by
// TestConfirmedActionsDoc) and the MCP instructions, so no prose copy of the
// set has to be kept by hand. TestConfirmedActionsCoverEngine fails when the
// engine gains a reason code or a raw why that this table lacks.

type confirmedAction struct {
	Code    string // a tier reason code, or confirmedRawPrefix + a key of rawWhys
	Example string
}

type confirmedGroup struct {
	Title, Summary string
	Actions        []confirmedAction
}

const confirmedRawPrefix = reasonRawTyped + ":"

var confirmedActions = []confirmedGroup{
	{"Locks, doors, gates, and alarms", "locks, gates, and garage doors, alarm disarm", []confirmedAction{
		{reasonAccessLock, "Unlock or open a door lock"},
		{reasonAccessAlarm, "Disarm an alarm panel"},
		{reasonAccessCover, "Open the garage door or a gate"},
		{reasonCoverClassUnknown, "Open a cover that has no “Show as” class, so it could be a door"},
		{reasonDeviceAccess, "Run an integration's service on a device that also has a lock"},
		{reasonDeviceAccessSibling, "Press a button on a device that also has a door lock"},
		{reasonSwitchAsX, "Switch the relay behind a lock or door made with Switch as X"},
		{reasonSceneAccess, "Activate a scene HA NOVA can't read from an integration that also controls locks"},
		{confirmedRawPrefix + "access_entity", "Give a lock or alarm panel a new ID, area, or label"},
		{confirmedRawPrefix + "access_device", "Move a device with a lock or door to another area"},
		{confirmedRawPrefix + "access_area", "Change the label or floor of an area with a lock or door"},
		{confirmedRawPrefix + "access_shown", "Make a hidden lock or door visible or enabled again"},
		{confirmedRawPrefix + "access_entity_cover", "Give a cover that may be a door, gate, or garage a new ID, area, or label"},
		{confirmedRawPrefix + "access_device_cover", "Move a device with a possible door, gate, or garage cover to another area"},
		{confirmedRawPrefix + "access_area_cover", "Change the label or floor of an area with a possible door, gate, or garage cover"},
		{confirmedRawPrefix + "access_shown_cover", "Make a hidden possible door, gate, or garage cover visible or enabled again"},
		{confirmedRawPrefix + "person_follows", "Change which devices a person follows"},
	}},
	{"Deleting and removing", "deletes and removals", []confirmedAction{
		{reasonConfigDelete, "Delete an automation, script, or dashboard"},
		{reasonDestructiveButton, "Press a factory-reset or wipe button on a device"},
		{confirmedRawPrefix + "delete", "Delete a helper, scene, area, label, or other item, or clear statistics"},
		{confirmedRawPrefix + "energy_emptied", "Empty a list of the Energy dashboard or drop several of its entries"},
		{confirmedRawPrefix + "backup_retention", "Keep fewer automatic backups, so older ones are deleted"},
		{confirmedRawPrefix + "integration_remove", "Remove an integration and its devices"},
		{confirmedRawPrefix + "file_delete", "Delete a file in the config folder"},
		{confirmedRawPrefix + "copy_delete", "Delete a saved config copy"},
		{confirmedRawPrefix + "app_uninstall", "Remove an App"},
	}},
	{"Installs, reloads, and code that runs", "installs, reloads, and code that runs", []confirmedAction{
		{reasonHACSInstall, "Install an update from HACS or the Marketplace"},
		{reasonOwnedTypedMember, "Publish an MQTT message, purge the recorder, start or stop an App, or reboot the host"},
		{reasonUnreadCommand, "Run a shell command, Python script, or REST command, or use an entity that runs a command or request"},
		{reasonUserService, "Call a service defined in an ESPHome device's own YAML"},
		{reasonReload, "Restart Home Assistant, or reload anything beyond one automation or script"},
		{reasonConfirmWatch, "Save an automation that watches service calls or phone notifications"},
		{confirmedRawPrefix + "hacs_install", "Download a HACS repository"},
		{confirmedRawPrefix + "resource_create", "Add a dashboard resource (code every browser runs)"},
		{confirmedRawPrefix + "resource_update", "Change a dashboard resource"},
		{confirmedRawPrefix + "file_write", "Write a file in the config folder"},
		{confirmedRawPrefix + "app_install", "Install an App"},
		{confirmedRawPrefix + "app_settings", "Change an App's settings"},
	}},
	{"Accounts, backups, and system settings", "accounts, backups, and system settings", []confirmedAction{
		{confirmedRawPrefix + "account_create", "Create a Home Assistant account"},
		{confirmedRawPrefix + "account_access", "Change what an account may do, or whether it can sign in"},
		{confirmedRawPrefix + "backup_locations", "Change where automatic backups go"},
		{confirmedRawPrefix + "backup_agent", "Set up or change a backup location"},
		{confirmedRawPrefix + "core_clock", "Change the time zone or the unit system"},
		{confirmedRawPrefix + "core_language", "Change Home Assistant's language"},
		{confirmedRawPrefix + "labs", "Turn an experimental feature on or off"},
		{confirmedRawPrefix + "statistics_import", "Import long-term statistics"},
		{confirmedRawPrefix + "skill_typed", "A step the skill itself marks as needing the person's confirmation"},
	}},
	{"What HA NOVA can't fully check", "anything HA NOVA can't fully check", []confirmedAction{
		{reasonTemplateAction, "Switch a template entity whose action can't be read"},
		{reasonUnregisteredActuator, "Switch or press an entity defined in YAML without a unique ID"},
		{reasonUserButton, "Press an ESPHome or MQTT button"},
		{reasonTemplatedAction, "Run a step whose service name is a template"},
		{reasonUnresolvedTarget, "Run a step whose target is a template, “all”, or can't be resolved"},
		{reasonStoredSelector, "Run a stored step that targets a floor or label"},
		{reasonBlueprint, "Run an automation whose blueprint can't be resolved"},
		{reasonYAMLNoID, "Run a YAML automation without an ID that can't be read"},
		{reasonUnreadConfig, "Run an automation or script whose configuration can't be read"},
		{reasonListener, "Act where something else may listen that HA NOVA can't list"},
		{reasonScanFailed, "Act when a read during the check failed"},
		{reasonServiceListUnread, "Call a service when Home Assistant's list of services can't be read"},
		{reasonHassio, "Stop or restart an App when HA NOVA can't read enough to rule out its own"},
		{reasonLimitCut, "Act when the check hit a size or time limit"},
	}},
}

// confirmedNotTyped: reason codes and notes that never put a plan on the code
// tier by themselves (preview, refused, or informational). A code in neither
// this set nor confirmedActions fails TestConfirmedActionsCoverEngine, so a
// new reason has to be classified on purpose. A code typed in ANY path, the
// Relay gate included (gatePlanTier), belongs in confirmedActions.
var confirmedNotTyped = map[string]bool{
	reasonDisruptive: true, reasonOwnedService: true, reasonOwnedMember: true, reasonSelector: true,
	reasonCodeRequired: true, reasonFeature: true, reasonExpanded: true, reasonBatch: true, reasonStoredMembers: true,
	reasonNoRegistryRow: true, reasonSceneUnread: true, reasonListenerUnexpl: true, reasonRunListenerUnexpl: true,
	reasonResponseOnly: true, reasonTargetRequired: true, reasonServiceNotFound: true,
	reasonConfigWrite: true, reasonConfigRevert: true, reasonConfigRestore: true, reasonAreaAssign: true,
	reasonManager: true, reasonIntegrationSvc: true, reasonRawWrite: true, reasonMarketplaceWarning: true,
	noteTemplates: true, noteNoManager: true, noteSkippedTargets: true, noteFilteredTargets: true,
}

// confirmedActionsSummary: the groups' one-line summaries, for the MCP
// instructions.
func confirmedActionsSummary() string {
	parts := make([]string, len(confirmedActions))
	for i, group := range confirmedActions {
		parts[i] = group.Summary
	}
	return strings.Join(parts, "; ")
}

// renderConfirmedActionsDoc is skills/ha-nova/confirmed-actions.md.
func renderConfirmedActionsDoc() string {
	var b strings.Builder
	b.WriteString("<!-- Generated from cli/confirmed_actions.go. Do not edit by hand: go test -run TestConfirmedActionsDoc -update ./cli rewrites it. -->\n\n" +
		"# Actions A Person Confirms Themselves\n\n" +
		"Generated from `cli/confirmed_actions.go`; do not edit by hand. **The CLI decides, not this list.** " +
		"Whether a plan needs the person's own confirmation is the plan's `tier` and `plan.confirm`, and how it asks is its `say` and `next_step`: follow those. " +
		"This list only helps you explain what such a confirmation is about. " +
		"An action missing here can still need one, a listed one can be ordinary in a given home, and a plain \"yes\" in chat never stands in for one.\n\n" +
		"The person confirms in a window on their computer, on their linked phone, or by typing the four-digit code from HA NOVA, as the plan's `say` asks.\n\n" +
		"The examples read as single actions, but running or saving an automation, script, or scene that reaches a listed action is rated the same way, and so is a read HA NOVA needs for its check that fails. " +
		"If the person asks which actions need this, answer in plain words in their language; never show the reason codes or this table.\n")
	for _, group := range confirmedActions {
		fmt.Fprintf(&b, "\n## %s\n\n| Example | Reason |\n|---|---|\n", group.Title)
		for _, action := range group.Actions {
			code := "`" + action.Code + "`"
			if id, raw := strings.CutPrefix(action.Code, confirmedRawPrefix); raw {
				code = "`" + reasonRawTyped + "` (" + id + ")"
			}
			fmt.Fprintf(&b, "| %s | %s |\n", action.Example, code)
		}
	}
	return b.String()
}
