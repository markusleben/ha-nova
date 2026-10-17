package main

import "regexp"

// The raw-route allowlist (docs/work/2026-10-17-confirmation-hardening-spec.md §2.1).
// A raw `ha-nova relay core|ws` write is refused unless it is listed here:
// exactly the writes the skills still make raw. None of them acts on a device
// by itself; each line says what it changes. Acting on devices runs through
// `ha-nova call`; automations, scripts, and dashboards through `ha-nova plan`.

// rawCoreWrites: method + exact path (as sent: no %-escapes, dot segments,
// or doubled slashes) of the REST writes that stay raw.
var rawCoreWrites = []struct {
	method string
	path   *regexp.Regexp
	why    string
}{
	{"POST", regexp.MustCompile(`^/api/template$`), "renders a template; a template cannot call a service"},
	{"POST", regexp.MustCompile(`^/api/config/core/check_config$`), "validates the YAML configuration; changes nothing"},
	{"POST", regexp.MustCompile(`^/api/config/config_entries/(options/)?flow$`), "starts an integration, helper, reconfigure, or options flow (integration-setup, helper)"},
	{"POST", regexp.MustCompile(`^/api/config/config_entries/(options/)?flow/[A-Za-z0-9_-]+$`), "submits one step of that flow"},
	{"DELETE", regexp.MustCompile(`^/api/config/config_entries/(options/)?flow/[A-Za-z0-9_-]+$`), "cancels an unfinished flow"},
	{"DELETE", regexp.MustCompile(`^/api/config/config_entries/entry/[A-Za-z0-9_-]+$`), "removes an integration entry (the skills preview it and take the one-time code)"},
	{"POST", regexp.MustCompile(`^/api/config/scene/config/[A-Za-z0-9_.-]+$`), "saves a scene's config; saving never activates it"},
	{"DELETE", regexp.MustCompile(`^/api/config/scene/config/[A-Za-z0-9_.-]+$`), "deletes a scene's config"},
}

const rawWSRead = "read"

// rawWSAllowed: every WS type a raw request may send, keyed by its exact
// type. Reads change nothing; each write says what it changes.
var rawWSAllowed = map[string]string{
	// Reads.
	"ping": rawWSRead, "get_states": rawWSRead, "get_config": rawWSRead, "get_panels": rawWSRead,
	"config_entries/get": rawWSRead, "config_entries/flow/progress": rawWSRead, "manifest/list": rawWSRead,
	"config/entity_registry/list": rawWSRead, "config/entity_registry/list_for_display": rawWSRead,
	"config/entity_registry/get": rawWSRead, "config/entity_registry/get_entries": rawWSRead,
	"config/device_registry/list": rawWSRead, "config/device_registry/list_composite_splits": rawWSRead,
	"config/area_registry/list": rawWSRead, "config/floor_registry/list": rawWSRead,
	"config/label_registry/list": rawWSRead, "config/category_registry/list": rawWSRead,
	"search/related": rawWSRead, "trace/list": rawWSRead, "trace/get": rawWSRead,
	"device_automation/trigger/list": rawWSRead, "device_automation/trigger/capabilities": rawWSRead,
	"blueprint/list": rawWSRead, "blueprint/substitute": rawWSRead, "blueprint/import": rawWSRead, // import fetches and previews, never saves
	"automation/config": rawWSRead, "script/config": rawWSRead,
	"lovelace/dashboards/list": rawWSRead, "lovelace/config": rawWSRead, "lovelace/resources": rawWSRead, "lovelace/info": rawWSRead,
	"frontend/get_themes": rawWSRead,
	"energy/get_prefs":    rawWSRead, "energy/validate": rawWSRead, "energy/info": rawWSRead,
	"energy/solar_forecast": rawWSRead, "energy/fossil_energy_consumption": rawWSRead,
	"recorder/info": rawWSRead, "recorder/validate_statistics": rawWSRead, "recorder/get_statistics_metadata": rawWSRead,
	"recorder/list_statistic_ids": rawWSRead, "recorder/statistics_during_period": rawWSRead,
	"backup/info": rawWSRead, "backup/agents/info": rawWSRead, "backup/details": rawWSRead, "backup/config/info": rawWSRead,
	"hacs/info": rawWSRead, "hacs/repositories/list": rawWSRead, "hacs/repository/info": rawWSRead,
	"hacs/repository/releases": rawWSRead, "hacs/repository/release_notes": rawWSRead,
	"hacs/repositories/removed": rawWSRead, "hacs/critical/list": rawWSRead,
	"person/list": rawWSRead, "zone/list": rawWSRead, "tag/list": rawWSRead,
	"config/auth/list": rawWSRead, "auth/current_user": rawWSRead,
	"assist_pipeline/pipeline/list": rawWSRead, "homeassistant/expose_entity/list": rawWSRead,
	"conversation/agent/list": rawWSRead, "tts/engine/list": rawWSRead, "stt/engine/list": rawWSRead, "wake_word/info": rawWSRead,
	"media_player/browse_media": rawWSRead, "media_player/search_media": rawWSRead,
	"media_source/browse_media": rawWSRead, "media_source/search_media": rawWSRead, "media_source/resolve_media": rawWSRead,
	"camera/stream": rawWSRead, "persistent_notification/get": rawWSRead, "update/release_notes": rawWSRead,
	"system_health/info": rawWSRead, "system_log/list": rawWSRead, "logger/log_info": rawWSRead,
	"diagnostics/list": rawWSRead, "repairs/list_issues": rawWSRead,
	"mqtt/subscribe": rawWSRead, "mqtt/device/debug_info": rawWSRead,
	"subscribe_events": rawWSRead, "subscribe_trigger": rawWSRead, // bounded by the Relay's collect_events; call_service is refused below
	"zha/devices": rawWSRead, "zwave_js/network_status": rawWSRead,
	"otbr/info": rawWSRead, "thread/list_datasets": rawWSRead, "matter/node_diagnostics": rawWSRead,

	// Writes.
	"config/entity_registry/update":           "an entity's name, area, labels, aliases, or visibility (a cover's or button's class, disabling a lock, alarm panel, or cover, and renaming a destructive button are refused)",
	"config/entity_registry/remove":           "removes an orphaned registry entry (maintenance; a lock, alarm panel, or access cover is refused)",
	"config/device_registry/update":           "a device's name, area, labels, or enabled state",
	"lovelace/dashboards/update":              "a dashboard's title, icon, or sidebar flags; its content goes through plan write",
	"lovelace/resources/create":               "registers a frontend card resource already on this Home Assistant (/hacsfiles/, /local/; other URLs are refused below)",
	"lovelace/resources/update":               "changes such a resource",
	"lovelace/resources/delete":               "removes a frontend card resource",
	"energy/save_prefs":                       "the Energy dashboard's sources",
	"recorder/update_statistics_metadata":     "renames a long-term statistic (maintenance)",
	"recorder/clear_statistics":               "deletes long-term statistics (maintenance; previewed with the one-time code)",
	"recorder/adjust_sum_statistics":          "corrects a statistic's sum (maintenance)",
	"backup/generate":                         "creates a backup",
	"backup/generate_with_automatic_settings": "creates a backup with the saved settings",
	"backup/delete":                           "deletes a backup (previewed with the one-time code)",
	"hacs/repositories/remove":                "removes a custom repository from HACS's list",
	"hacs/repository/download":                "installs or updates a HACS repository; new integration code loads only after a restart, which is typed",
	"hacs/repository/remove":                  "uninstalls a HACS repository",
	"hacs/repository/version":                 "picks the version HACS offers",
	"hacs/repository/beta":                    "shows or hides beta releases",
	"hacs/repository/refresh":                 "re-reads a repository from GitHub",
	"hacs/repository/ignore":                  "hides a repository's update",
	"hacs/critical/acknowledge":               "acknowledges a HACS critical notice",
	"config/auth/create":                      "creates an account without credentials; it cannot sign in until a person sets a password in the UI",
	"config/auth/delete":                      "deletes an account (admin; previewed with the one-time code)",
	"assist_pipeline/pipeline/create":         "creates a voice assistant",
	"assist_pipeline/pipeline/update":         "changes a voice assistant",
	"assist_pipeline/pipeline/delete":         "deletes a voice assistant",
	"assist_pipeline/pipeline/set_preferred":  "picks the preferred voice assistant",
	"homeassistant/expose_entity":             "exposes an entity to voice assistants; using it still takes a person's voice",
	"calendar/event/create":                   "adds a calendar event (recurring; one-off events go through ha-nova call)",
	"calendar/event/update":                   "changes a calendar event",
	"calendar/event/delete":                   "deletes a calendar event",
	"todo/item/move":                          "reorders a to-do item",
	"blueprint/save":                          "saves a new blueprint; overriding one in use is refused below",
}

// Storage collections the helper and admin skills edit: each holds config
// (a helper's value range, a person's trackers), never acts on a device.
var rawWSCollections = []string{"input_boolean", "input_number", "input_text", "input_select", "input_datetime",
	"input_button", "counter", "timer", "schedule", "person", "zone", "tag",
	"config/area_registry", "config/floor_registry", "config/label_registry", "config/category_registry"}

func init() {
	for _, collection := range rawWSCollections {
		rawWSAllowed[collection+"/list"] = rawWSRead
		for _, op := range []string{"create", "update", "delete"} {
			rawWSAllowed[collection+"/"+op] = "storage config (" + collection + "), no device action"
		}
	}
}
