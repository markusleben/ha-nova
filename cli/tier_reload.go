package main

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// configReloadTier rates the reload the CLI sends after each automation or
// script config write (verb_config_apply.go) like the configs it loads
// (Markus, 2026-11-07: "Wie die Automation selbst"): automation.reload of one
// id as that automation's stored config, script.reload as every stored
// script (each with a state or a registry row; a YAML or package one, which
// has no stored config, makes it typed). Each is walked as a draft
// (classifyDraft), so the plan's level is the strictest config's, and a
// config that can't be read is typed, and so is a reload met again inside
// those configs. Known limits, both met by Home Assistant's own reload after
// every config write too (config/view.py), so typing this reload would not
// close them, and only a person or a typed file write changes these files: a
// script added on disk that has never loaded is not seen, and a blueprint
// file changed since Home Assistant cached it is rated as cached. At least
// preview: a reload changes what runs. ok is false for every other reload,
// which stays typed (classifyAction).
func (e *tierEngine) configReloadTier(service string, data map[string]any) (level tierLevel, code string, ok bool) {
	id, isID := data["id"].(string)
	automation := service == "automation.reload" && len(data) == 1 && isID && strings.TrimSpace(id) != ""
	if !automation && (service != "script.reload" || len(data) != 0) {
		return 0, "", false
	}
	if e.reloading { // a reload among the configs a reload loads: never walked twice
		return tierTyped, reasonReload, true
	}
	e.reloading = true
	defer func() { e.reloading = false }()
	switch {
	case automation:
		raw, err := readStoredConfig(e.s, "automation", id)
		if err != nil {
			e.readFailed("automation", "reload", "config "+id)
			return tierTyped, reasonReload, true
		}
		e.classifyDraft("automation", id, raw, nil)
		e.reloadListeners("automation")
	default:
		e.useStateList()
		states, err := e.stateList()
		var rows []struct {
			EntityID string `json:"entity_id"`
			Platform string `json:"platform"`
		}
		if err == nil { // the full registry: the compact one leaves disabled entities out
			err = e.s.wsDecode(map[string]any{"type": "config/entity_registry/list"}, &rows, "the entity registry")
		}
		if err != nil {
			e.readFailed("script", "reload", "state list or registry")
			return tierTyped, reasonReload, true
		}
		scripts := map[string]bool{} // what has loaded, or has an id: a disabled script has no state
		for entityID := range states {
			if strings.HasPrefix(entityID, "script.") {
				scripts[entityID] = true
			}
		}
		for _, row := range rows {
			if row.Platform == "script" {
				scripts[row.EntityID] = true
			}
		}
		for _, entityID := range slices.Sorted(maps.Keys(scripts)) {
			configID, err := resolveConfigID(e.s, entityID, "script", nil)
			var raw json.RawMessage
			if err == nil {
				raw, err = readStoredConfig(e.s, "script", configID)
			}
			if err != nil { // a YAML or package script: what reload loads is unread (its loaded config is the old one)
				e.gap(reasonUnreadConfig, entityID, "reload", configID)
				return tierTyped, reasonReload, true
			}
			e.classifyDraft("script", configID, raw, nil)
		}
	}
	return tierPreview, reasonReload, true
}

// reloadListeners classifies what a config load sets off besides the config
// itself: an automation reload, even of one id, fires automation_reloaded
// (automation/services.py), and Home Assistant's own reload after a config
// write is a service call, which fires call_service (config/automation.py,
// config/script.py).
func (e *tierEngine) reloadListeners(domain string) {
	if domain == "automation" {
		e.checkEvents("automation_reloaded", "")
	}
	if e.liveEvents()["call_service"] > callServiceListenerBaseline {
		e.checkEvents("call_service", "")
	}
}
