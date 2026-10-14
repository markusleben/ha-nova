package main

import (
	"strings"
)

// Static classification tables of the tier engine (typed-verbs spec §2.2
// step 2). Each row mirrors a prose rule; the prose stays authoritative for
// the routes that do not use the CLI.

type tierLevel int

const (
	tierOrdinary tierLevel = iota
	tierPreview
	tierTyped
	tierRefused
)

func (l tierLevel) String() string {
	return [...]string{"ordinary", "preview", "typed", "refused"}[l]
}

// Reason codes and coverage notes. Every value is pinned to
// indirect-actuation.md or the typed-verbs spec by
// TestTierReasonCodesAreDocumented; the engine emits only these constants.
const (
	reasonAccessLock        = "ACCESS_LOCK"
	reasonAccessAlarm       = "ACCESS_ALARM"
	reasonAccessCover       = "ACCESS_COVER"
	reasonCoverClassUnknown = "COVER_CLASS_UNKNOWN"
	reasonDisruptive        = "DISRUPTIVE"
	reasonOwnedService      = "OWNED_SERVICE"
	reasonOwnedMember       = "OWNED_MEMBER"
	reasonOwnedTypedMember  = "OWNED_TYPED_MEMBER"
	reasonHassio            = "HASSIO_NOT_SUPPORTED"
	reasonSelector          = "UNSUPPORTED_SELECTOR"
	reasonCodeRequired      = "CODE_REQUIRED"
	reasonFeature           = "UNSUPPORTED_FEATURE"
	reasonExpanded          = "EXPANDED_TARGET"
	reasonBatch             = "EXPLICIT_BATCH"
	reasonStoredMembers     = "STORED_MEMBERS"
	reasonNoRegistryRow     = "NO_REGISTRY_ROW"
	reasonTemplateAction    = "TEMPLATE_ACTION_UNREAD"
	reasonUserButton        = "USER_AUTHORED_BUTTON"
	reasonUnreadCommand     = "UNREAD_COMMAND"
	reasonTemplatedAction   = "TEMPLATED_ACTION"
	reasonUnresolvedTarget  = "UNRESOLVED_TARGET"
	reasonStoredSelector    = "STORED_SELECTOR"
	reasonBlueprint         = "BLUEPRINT_UNRESOLVED"
	reasonYAMLNoID          = "YAML_NO_ID"
	reasonUnreadConfig      = "UNREAD_CONFIG"
	reasonSceneAccess       = "INTEGRATION_SCENE_ACCESS"
	reasonSceneUnread       = "INTEGRATION_SCENE_UNREAD"
	reasonReload            = "RELOAD_UNENUMERATED"
	reasonListener          = "UNENUMERABLE_LISTENER"
	reasonListenerUnexpl    = "CALL_SERVICE_LISTENER_UNEXPLAINED"
	reasonManager           = "CONSUMER_MANAGER_PRESENT"
	reasonScanFailed        = "SCAN_FAILED"
	reasonLimitCut          = "LIMIT_CUT"
	reasonIntegrationSvc    = "INTEGRATION_SERVICE"
	reasonUserService       = "USER_AUTHORED_SERVICE"
	noteTemplates           = "TEMPLATES_NOT_CHECKABLE"
	noteNoManager           = "NO_LOCAL_CONSUMER_MANAGER"
	noteSkippedTargets      = "EXPANDED_TARGET_WITHOUT_STATE"
)

// Engine limits (spec §2.2 "Caching and limits"); any cut is LIMIT_CUT.
const (
	tierMaxActed       = 64
	tierMaxNodes       = 200
	tierMaxDepth       = 8
	tierMaxConsumers   = 100
	tierMaxAutomations = 2000
	tierMaxExplicit    = 10
)

// ponytail: baseline not yet measured on a live Home Assistant (spec PR2 E2E
// is the blocking check). 0 fails closed: any call_service listener forces the
// full scan, and a core listener nobody can enumerate reads as typed.
const callServiceListenerBaseline = 0

// Owning-skill deferrals (service-call/SKILL.md → Owning-Skill Deferrals).
var ownedServices = map[string]string{
	"mqtt.publish": "ha-nova:mqtt", "logger.set_level": "ha-nova:diagnose",
	"update.install": "ha-nova:updates", "update.skip": "ha-nova:updates", "update.clear_skipped": "ha-nova:updates",
	"recorder.purge": "ha-nova:maintenance", "recorder.purge_entities": "ha-nova:maintenance",
	"todo.add_item": "ha-nova:todo", "todo.update_item": "ha-nova:todo", "todo.remove_item": "ha-nova:todo", "todo.remove_completed_items": "ha-nova:todo",
	"backup.create": "ha-nova:backup", "backup.create_automatic": "ha-nova:backup",
	"conversation.process": "ha-nova:assist",
}

var ownedDomains = map[string]string{
	"camera": "ha-nova:camera", "media_player": "ha-nova:media", "tts": "ha-nova:media",
	"notify": "ha-nova:notify", "persistent_notification": "ha-nova:notify", "calendar": "ha-nova:calendar",
}

// Read-only response services stay with service-call.
var readOnlyResponseServices = map[string]bool{"calendar.get_events": true, "todo.get_items": true, "weather.get_forecasts": true}

func ownerSkill(service string) string {
	if readOnlyResponseServices[service] {
		return ""
	}
	if owner := ownedServices[service]; owner != "" {
		return owner
	}
	domain, _ := splitID(service)
	return ownedDomains[domain]
}

// hassioOwner names where a hassio.* call goes; v1 verbs never run one.
func hassioOwner(name string) string {
	switch name {
	case "restore_full", "restore_partial":
		return "ha-nova:backup"
	case "addon_update":
		return "ha-nova:updates"
	case "addon_start", "addon_stop", "addon_restart", "host_reboot", "host_shutdown":
		return "ha-nova:service-call"
	}
	return "ha-nova:fallback"
}

var disruptiveServices = map[string]bool{
	"homeassistant.restart": true, "homeassistant.stop": true, "siren.turn_on": true,
	"alarm_control_panel.alarm_trigger": true,
}

// Implementations live in files the engine cannot read (rules 2 and 3).
var unreadCommandDomains = map[string]bool{"python_script": true, "shell_command": true, "rest_command": true}

// Home Assistant's own entity and core service domains. A service of any
// other domain belongs to an integration: on an access entity it is typed,
// targetless it is at least preview (its effect set is not enumerable).
var coreDomains = map[string]bool{}

func init() {
	for _, d := range strings.Fields(`alarm_control_panel automation backup binary_sensor button calendar camera
		climate conversation counter cover date datetime device_tracker event fan group homeassistant humidifier
		image input_boolean input_button input_datetime input_number input_select input_text lawn_mower light lock
		logbook logger media_player notify number persistent_notification person recorder remote scene schedule
		script select sensor siren switch system_log text time timer todo tts update vacuum valve water_heater
		weather zone`) {
		coreDomains[d] = true
	}
}

// Alarm feature bits (service-call/SKILL.md → Alarm Panels And Locks).
var alarmFeatureBits = map[string]int{
	"alarm_arm_home": 1, "alarm_arm_away": 2, "alarm_arm_night": 4, "alarm_trigger": 8,
	"alarm_arm_custom_bypass": 16, "alarm_arm_vacation": 32,
}

var accessCoverClasses = map[string]bool{"garage": true, "gate": true, "door": true}

// accessCapable: a lock, an alarm panel, or a cover that is (or may be) an
// access cover — an unknown device_class fails closed (Q3).
func accessCapable(entityID, deviceClass string) bool {
	domain, _ := splitID(entityID)
	return domain == "lock" || domain == "alarm_control_panel" || (domain == "cover" && (deviceClass == "" || accessCoverClasses[deviceClass]))
}

// Consumer managers detected per run: config-entry domains and App slugs.
var managerEntryDomains = map[string]bool{"nodered": true, "pyscript": true}
var managerAppSlugs = []string{"nodered", "appdaemon"}

// classifyAction is the direct-action table for one effective service on
// one entity state (nil = unknown target, which fails closed for covers).
func classifyAction(service string, data map[string]any, st *entityState) (tierLevel, string) {
	domain, name := splitID(service)
	switch {
	case service == "lock.unlock" || service == "lock.open":
		return tierTyped, reasonAccessLock
	case service == "alarm_control_panel.alarm_disarm":
		return tierTyped, reasonAccessAlarm
	case domain == "cover" && coverCanOpen(name, data):
		dc := st.str("device_class")
		if dc == "" {
			return tierTyped, reasonCoverClassUnknown
		}
		if accessCoverClasses[dc] {
			return tierTyped, reasonAccessCover
		}
	case disruptiveServices[service]:
		return tierPreview, reasonDisruptive
	case unreadCommandDomains[domain]:
		return tierTyped, reasonUnreadCommand
	case name == "reload" || (domain == "homeassistant" && strings.HasPrefix(name, "reload_")):
		return tierTyped, reasonReload // no reload is provably side-effect-free in v1
	case domain == "esphome":
		return tierTyped, reasonUserService
	case service == "mqtt.publish" || domain == "hassio":
		return tierTyped, reasonOwnedTypedMember
	case ownerSkill(service) != "":
		return tierPreview, reasonOwnedMember
	}
	return tierOrdinary, ""
}

// classifyOnEntity inverts the table for access-capable targets (classify
// by target domain first): only calls that provably grant nothing stay
// below typed — every other service, integration services included, is typed.
func classifyOnEntity(entityID, service string, data map[string]any, st *entityState) (tierLevel, string) {
	dc := st.str("device_class")
	if !accessCapable(entityID, dc) {
		return classifyAction(service, data, st)
	}
	domain, name := splitID(service)
	if service == "lock.lock" || service == "cover.close_cover" || service == "homeassistant.update_entity" ||
		service == "alarm_control_panel.alarm_trigger" || (domain == "alarm_control_panel" && strings.HasPrefix(name, "alarm_arm_")) {
		return classifyAction(service, data, st)
	}
	switch entityDomain, _ := splitID(entityID); {
	case entityDomain == "lock":
		return tierTyped, reasonAccessLock
	case entityDomain == "alarm_control_panel":
		return tierTyped, reasonAccessAlarm
	case dc == "":
		return tierTyped, reasonCoverClassUnknown
	}
	return tierTyped, reasonAccessCover
}

// classifyTargetless: a call without entities; an integration's own service
// cannot be enumerated, so it never stays ordinary.
func classifyTargetless(service string, data map[string]any) (tierLevel, string) {
	level, code := classifyAction(service, data, nil)
	if domain, _ := splitID(service); level == tierOrdinary && !coreDomains[domain] {
		return tierPreview, reasonIntegrationSvc
	}
	return level, code
}

// coverCanOpen: open_cover, toggle, and set_cover_position above 0 — a
// missing or templated position fails closed (indirect-actuation.md).
func coverCanOpen(name string, data map[string]any) bool {
	switch name {
	case "open_cover", "toggle":
		return true
	case "set_cover_position":
		pos, isNumber := data["position"].(float64)
		return !isNumber || pos > 0
	}
	return false
}

// directRefusal: code-bearing locks and alarms hand off to the HA UI;
// unsupported feature bits are refused (service-call/SKILL.md).
func directRefusal(service string, st *entityState) string {
	domain, name := splitID(service)
	features := int(st.num("supported_features"))
	switch domain {
	case "lock":
		if st.str("code_format") != "" {
			return reasonCodeRequired
		}
		if name == "open" && features&1 == 0 {
			return reasonFeature
		}
	case "alarm_control_panel":
		if bit := alarmFeatureBits[name]; bit != 0 && features&bit == 0 {
			return reasonFeature
		}
		armCode := strings.HasPrefix(name, "alarm_arm_") && st.attr("code_arm_required") == true
		if armCode || ((name == "alarm_disarm" || name == "alarm_trigger") && st.str("code_format") != "") {
			return reasonCodeRequired
		}
	}
	return ""
}

// effectiveService maps the generic homeassistant.turn_on|turn_off|toggle
// onto the entity's own domain; covers take open_cover/close_cover.
func effectiveService(service, entityDomain string) string {
	domain, name := splitID(service)
	if domain != "homeassistant" || entityDomain == "" || (name != "turn_on" && name != "turn_off" && name != "toggle") {
		return service
	}
	if entityDomain == "cover" {
		switch name {
		case "turn_on":
			name = "open_cover"
		case "turn_off":
			name = "close_cover"
		}
	}
	return entityDomain + "." + name
}

// isRun reports whether the call STARTS stored members of the target.
func isRun(entityDomain, service string) bool {
	domain, name := splitID(service)
	switch entityDomain {
	case "scene":
		return (domain == "scene" && name == "turn_on") || (domain == "homeassistant" && (name == "turn_on" || name == "toggle"))
	case "script":
		return (domain == "script" || domain == "homeassistant") && (name == "turn_on" || name == "toggle")
	case "automation":
		return service == "automation.trigger"
	}
	return false
}

var scriptEntityServices = map[string]bool{"turn_on": true, "turn_off": true, "toggle": true, "reload": true}

// sceneMemberService turns a stored scene state into the equivalent service,
// so the target STATE decides (unlocked/open/disarmed grant access).
func sceneMemberService(entityID string, value any) (string, map[string]any) {
	domain, _ := splitID(entityID)
	state, _ := value.(string)
	data := map[string]any{}
	if m, isMap := value.(map[string]any); isMap {
		state, _ = m["state"].(string)
		data["position"] = m["current_position"]
	}
	switch domain {
	case "lock":
		if state == "locked" || state == "locking" {
			return "lock.lock", nil
		}
		return "lock.unlock", nil
	case "alarm_control_panel":
		switch {
		case state == "disarmed":
			return "alarm_control_panel.alarm_disarm", nil
		case state == "triggered":
			return "alarm_control_panel.alarm_trigger", nil
		case strings.HasPrefix(state, "armed_"):
			return "alarm_control_panel.alarm_arm_" + strings.TrimPrefix(state, "armed_"), nil
		}
		return "alarm_control_panel.alarm_disarm", nil // unknown target state fails closed
	case "cover":
		if pos, isNumber := data["position"].(float64); state == "closed" && (!isNumber || pos == 0) {
			return "cover.close_cover", nil
		}
		return "cover.set_cover_position", data
	}
	if state == "off" {
		return domain + ".turn_off", nil
	}
	return domain + ".turn_on", nil
}

// deviceActionService maps a device-form step (domain + type) to its service.
func deviceActionService(domain, actionType string) string {
	switch {
	case domain == "cover" && actionType == "open":
		return "cover.open_cover"
	case domain == "cover" && actionType == "close":
		return "cover.close_cover"
	case domain == "cover" && actionType == "set_position":
		return "cover.set_cover_position"
	case domain == "alarm_control_panel":
		return domain + ".alarm_" + actionType
	}
	return domain + "." + actionType
}

func splitID(id string) (string, string) {
	domain, rest, _ := strings.Cut(id, ".")
	return domain, rest
}

func isTemplated(s string) bool { return strings.Contains(s, "{{") || strings.Contains(s, "{%") }
