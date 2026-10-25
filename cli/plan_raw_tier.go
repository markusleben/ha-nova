package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
)

// The confirmation tier of every raw plan item, in one place: the rungs the
// skills use for the same request on a computer (parity spec → Writes).
// plan_raw_tier_test.go plans every allowlisted write against it.
//
//	typed:   every delete or removal (WS */delete and */remove,
//	         recorder/clear_statistics, DELETE …/config_entries/entry/… and
//	         …/scene/config/…, files delete_file, backups delete and prune),
//	         a new account, HACS downloads and dashboard resources (code that
//	         runs in Home Assistant or every signed-in browser), and an energy
//	         save that empties or replaces a list
//	preview: everything else, a flow cancel included

const (
	reasonRawWrite = "RAW_WRITE" // a previewed raw item; the preview line says it
	reasonRawTyped = "RAW_TYPED" // Detail says why it needs the user
)

const rawWhyDelete = "This deletes something in Home Assistant"

// rawTypedWS: the WS writes typed besides the deletes and removals.
var rawTypedWS = map[string]string{
	"recorder/clear_statistics": rawWhyDelete,
	"config/auth/create":        "This creates a Home Assistant account",
	"hacs/repository/download":  "This installs code from HACS that runs in Home Assistant",
	"lovelace/resources/create": "This adds code that runs in every browser signed in to Home Assistant",
	"lovelace/resources/update": "This changes code that runs in every browser signed in to Home Assistant",
}

var rawSceneConfigPath = regexp.MustCompile(`^/api/config/scene/config/[A-Za-z0-9_.-]+$`)

// rawItemTier: the item's tier and, for a typed one, why (one sentence).
func rawItemTier(n *rawNamer, endpoint string, payload map[string]any) (tierLevel, string) {
	action, _ := payload["action"].(string)
	switch endpoint {
	case "ws":
		wsType, _ := payload["type"].(string)
		switch {
		case strings.HasSuffix(wsType, "/delete") || strings.HasSuffix(wsType, "/remove"):
			return tierTyped, rawWhyDelete
		case rawTypedWS[wsType] != "":
			return tierTyped, rawTypedWS[wsType]
		case wsType == "energy/save_prefs" && n.energyEmptied(payload):
			return tierTyped, "This empties or replaces a list of the Energy dashboard"
		}
	case "core":
		target, _ := payload["path"].(string)
		target, _, _ = strings.Cut(target, "#")
		target, _, _ = strings.Cut(target, "?")
		switch {
		case payload["method"] != http.MethodDelete:
		case rawEntryDeletePath.MatchString(target):
			return tierTyped, "This removes an integration and its devices from Home Assistant"
		case rawSceneConfigPath.MatchString(target):
			return tierTyped, rawWhyDelete
		}
	case "files":
		if action == "delete_file" {
			return tierTyped, "This deletes a file in Home Assistant's config folder"
		}
	case "backups":
		if action == "delete" || action == "prune" {
			return tierTyped, "This deletes saved config copies"
		}
	}
	return tierPreview, ""
}

// energyEmptied: the save leaves a list empty that has entries now, or keeps
// none of them (save_prefs replaces each list it names). A failed read, or a
// value that is not a list, counts as emptied: fail closed.
func (n *rawNamer) energyEmptied(payload map[string]any) bool {
	prefs, ok := n.energyPrefs()
	for key, value := range payload {
		if key == "type" {
			continue
		}
		next, isList := value.([]any)
		if !ok || !isList {
			return true
		}
		before, _ := prefs[key].([]any)
		kept := false
		for _, old := range before {
			for _, now := range next {
				kept = kept || jsonEqual(old, now)
			}
		}
		if len(before) > 0 && !kept {
			return true
		}
	}
	return false
}

// energyPrefs reads energy/get_prefs once; never configured (not_found)
// reads as no lists.
func (n *rawNamer) energyPrefs() (map[string]any, bool) {
	if n.prefsRead {
		return n.prefs, n.prefsOK
	}
	n.prefsRead, n.prefs = true, map[string]any{}
	data, err := rawRead(n.s, "ws", map[string]any{"type": "energy/get_prefs"})
	var call *relayCallError
	switch {
	case err == nil:
		n.prefsOK = json.Unmarshal(data, &n.prefs) == nil && n.prefs != nil
	case errors.As(err, &call) && strings.Contains(strings.ToLower(call.Error()), "not_found"):
		n.prefsOK = true
	}
	return n.prefs, n.prefsOK
}

// basis: the energy lists a save replaces, so a source another client added
// since the preview is drift, never silently dropped
// (skills/energy/SKILL.md → Config edit); nil without an energy save.
func (n *rawNamer) basis() map[string]any {
	if !n.prefsRead {
		return nil
	}
	if !n.prefsOK {
		return map[string]any{"energy": "unreadable"}
	}
	return map[string]any{"energy": n.prefs}
}
