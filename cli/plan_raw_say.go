package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// The plain line of each raw plan item ("Rename the device “Hall lamp”"),
// which say, the window, the phone, and the confirmation form show
// (confirm_say.go hands raw plans to rawSayWhat, rawGoal, and rawAsk).
// Names are read where one read gets them (plan_raw_names.go); an id is
// shown only when no name can be read. A field that may hold a password,
// code, key, or token shows no value (rawSecretField).

// rawCollectionNouns: what each storage collection holds, in plain words.
var rawCollectionNouns = map[string]string{"input_boolean": "toggle helper", "input_number": "number helper", "input_text": "text helper",
	"input_select": "dropdown helper", "input_datetime": "date and time helper", "input_button": "button helper", "counter": "counter",
	"timer": "timer", "schedule": "schedule", "person": "person", "zone": "zone", "tag": "tag", "config/area_registry": "area",
	"config/floor_registry": "floor", "config/label_registry": "label", "config/category_registry": "category"}

// rawCollection: a storage collection's create, update, or delete, and the
// field that names its item.
func rawCollection(wsType string) (collection, op, idKey string) {
	cut := strings.LastIndex(wsType, "/")
	if cut < 0 || rawCollectionNouns[wsType[:cut]] == "" {
		return "", "", ""
	}
	collection, op = wsType[:cut], wsType[cut+1:]
	return collection, op, strings.TrimSuffix(strings.TrimPrefix(collection, "config/"), "_registry") + "_id"
}

func (n *rawNamer) collectionItem(collection, idKey string, payload map[string]any) string {
	list := map[string]any{"type": collection + "/list"}
	if scope, set := payload["scope"]; set { // categories are listed per scope
		list["scope"] = scope
	}
	return n.named(list, payload[idKey], []string{"id", idKey}, []string{"name"})
}

// rawWSPhrases: the plain line of every other allowlisted WS request; its
// named targets follow (rawTargets).
var rawWSPhrases = map[string]string{
	"lovelace/dashboards/update": "Change the settings of the dashboard", "lovelace/resources/create": "Add the dashboard resource",
	"lovelace/resources/update": "Change the dashboard resource", "lovelace/resources/delete": "Remove the dashboard resource",
	"recorder/update_statistics_metadata": "Rename the statistic", "recorder/clear_statistics": "Delete the long-term statistics of",
	"recorder/adjust_sum_statistics": "Correct the sum of the statistic", "backup/generate": "Create a backup",
	"backup/generate_with_automatic_settings": "Create a backup with the saved settings", "backup/delete": "Delete the backup",
	"hacs/repositories/remove": "Remove from HACS's list of custom repositories", "hacs/repository/download": "Install from HACS",
	"hacs/repository/remove": "Uninstall from HACS", "hacs/repository/version": "Pick the version HACS offers of",
	"hacs/repository/beta": "Show or hide beta releases in HACS of", "hacs/repository/refresh": "Read again from GitHub",
	"hacs/repository/ignore": "Hide the HACS update of", "hacs/critical/acknowledge": "Acknowledge the HACS notice about",
	"config/auth/create": "Create the Home Assistant account", "config/auth/delete": "Delete the Home Assistant account",
	"assist_pipeline/pipeline/create": "Create the voice assistant", "assist_pipeline/pipeline/update": "Change the voice assistant",
	"assist_pipeline/pipeline/delete": "Delete the voice assistant", "assist_pipeline/pipeline/set_preferred": "Make this the preferred voice assistant:",
	"homeassistant/expose_entity": "Change which voice assistants see", "calendar/event/create": "Add an event to the calendar",
	"calendar/event/update": "Change an event in the calendar", "calendar/event/delete": "Delete an event from the calendar",
	"todo/item/move": "Reorder an item in the to-do list", "blueprint/save": "Save the blueprint",
	"blueprint/import": "Fetch a blueprint from", "camera/stream": "Get a live stream link for", "media_source/resolve_media": "Get a playable link for",
}

// rawItemLine words one item as its verb and the rest of the line.
func rawItemLine(n *rawNamer, endpoint string, payload map[string]any) (verb, object string) {
	action, _ := payload["action"].(string)
	target, _ := payload["path"].(string)
	switch endpoint {
	case "files":
		if action == "delete_file" {
			return "Delete", "the file " + sayText(target, true) + " in the config folder"
		}
		return "Write", "the file " + sayText(target, true) + " in the config folder"
	case "backups":
		switch action {
		case "save":
			category, _ := payload["category"].(string)
			name, _ := payload["name"].(string)
			return "Save", "a config copy " + sayText(category+"/"+name, true)
		case "delete":
			file, _ := payload["file"].(string)
			return "Delete", "the config copy " + sayText(file, true)
		}
		return "Delete", rawPruneLine(payload)
	case "core":
		return rawCoreLine(n, payload)
	}
	wsType, _ := payload["type"].(string)
	collection, op, idKey := rawCollection(wsType)
	switch {
	case wsType == "config/entity_registry/update":
		return "Change", "the entity " + n.entity(payload["entity_id"]) + n.fields(payload, "entity_id")
	case wsType == "config/entity_registry/remove":
		return "Remove", n.entity(payload["entity_id"]) + " from the entity registry"
	case wsType == "config/device_registry/update":
		return "Change", "the device " + n.device(payload["device_id"]) + n.fields(payload, "device_id")
	case wsType == "config/device_registry/remove":
		return "Delete", "the device " + n.device(payload["device_id"]) + n.deviceEntities(payload["device_id"])
	case op == "create":
		name, _ := payload["name"].(string)
		return "Create", strings.TrimSpace("the "+rawCollectionNouns[collection]+" "+sayText(name, true)) + n.fields(payload, "name")
	case op == "update":
		return "Change", "the " + rawCollectionNouns[collection] + " " + n.collectionItem(collection, idKey, payload) + n.fields(payload, idKey)
	case op == "delete":
		return "Delete", "the " + rawCollectionNouns[collection] + " " + n.collectionItem(collection, idKey, payload)
	case wsType == "calendar/event/delete":
		return "Delete", n.calendarEvent(payload) + " from the calendar " + n.entity(payload["entity_id"])
	case wsType == "energy/save_prefs":
		return "Change", "the Energy dashboard" + n.energyChange(payload)
	}
	phrase, known := rawWSPhrases[wsType]
	if !known { // unreachable for allowlisted types (plan_raw_tier_test.go)
		phrase = "Send " + sayText(wsType, true)
	}
	verb, rest, _ := strings.Cut(phrase, " ")
	return verb, strings.TrimSpace(rest + n.targets(payload))
}

func rawCoreLine(n *rawNamer, payload map[string]any) (verb, object string) {
	method, _ := payload["method"].(string)
	target, _ := payload["path"].(string)
	target, _, _ = strings.Cut(target, "?")
	body, _ := payload["body"].(map[string]any)
	flow := strings.Contains(target, "/flow")
	switch {
	case method == http.MethodGet && strings.HasPrefix(target, "/api/repairs/"):
		return "Continue", "the repair in progress (this can finish it)"
	case method == http.MethodGet:
		return "Continue", "the setup in progress (this can finish it)"
	case target == "/api/template":
		return "Render", "a template (this changes nothing)"
	case target == "/api/config/core/check_config":
		return "Check", "the configuration (this changes nothing)"
	case method == http.MethodDelete && flow:
		return "Cancel", "the unfinished setup"
	case method == http.MethodDelete && rawEntryDeletePath.MatchString(target):
		return "Remove", "the integration " + n.entry(target[strings.LastIndex(target, "/")+1:])
	case strings.HasPrefix(target, "/api/config/scene/config/"):
		id := target[strings.LastIndex(target, "/")+1:]
		name, _ := body["name"].(string)
		if method == http.MethodDelete {
			return "Delete", "the scene " + n.scene(id)
		}
		return "Save", "the scene " + sayText(firstNonEmpty(name, id), true) + " (saving never activates it)"
	case strings.HasSuffix(target, "/options/flow"):
		return "Open", "the options of the integration " + n.entry(body["handler"])
	case strings.HasSuffix(target, "/flow") && body["entry_id"] != nil:
		return "Start", "reconfiguring the integration " + n.entry(body["entry_id"])
	case strings.HasSuffix(target, "/flow"):
		handler, _ := body["handler"].(string)
		return "Start", "setting up " + sayText(handler, true)
	}
	setup := "the setup in progress"
	if !strings.Contains(target, "/options/") {
		flow := n.names(map[string]any{"type": "config_entries/flow/progress"}, []string{"flow_id"}, []string{"handler"})[target[strings.LastIndex(target, "/")+1:]]
		if flow != "" {
			setup = "the setup of " + sayText(flow, true) + " in progress"
		}
	}
	return "Submit", "the next step of " + setup + flowFields(body)
}

// rawPruneLine: a snapshot prune in plain words (Relay defaults: 30 days,
// 100 files, named copies kept).
func rawPruneLine(payload map[string]any) string {
	days, files := "30", "100"
	if v, set := payload["max_age_days"]; set {
		days = sayText(fmt.Sprint(v), false)
	}
	if v, set := payload["max_files"]; set {
		files = sayText(fmt.Sprint(v), false)
	}
	line := "automatic config copies older than " + days + " days or beyond the newest " + files
	if category, _ := payload["category"].(string); category != "" {
		line += " in " + sayText(category, true)
	}
	if payload["keep_named"] == false {
		return line + ", named copies too"
	}
	return line + "; named copies are kept"
}

// targets: the named things a request acts on (never an area it sets).
func (n *rawNamer) targets(payload map[string]any) string {
	var names []string
	for _, key := range []string{"entity_id", "entity_ids", "device_id", "statistic_id", "statistic_ids", "pipeline_id", "user_id",
		"backup_id", "repository", "resource_id", "dashboard_id", "url", "path", "name", "summary", "media_content_id"} {
		values, isList := payload[key].([]any)
		if !isList {
			values = []any{payload[key]}
		}
		for _, value := range values {
			text, _ := value.(string)
			switch {
			case text == "":
			case key == "entity_id" || key == "entity_ids":
				names = append(names, n.entity(text))
			case key == "device_id":
				names = append(names, n.device(text))
			case rawTargetLists[key].request != "":
				list := rawTargetLists[key]
				names = append(names, n.named(map[string]any{"type": list.request}, text, list.ids, list.names))
			default:
				names = append(names, sayText(text, true))
			}
		}
	}
	if version, _ := payload["version"].(string); version != "" {
		names = append(names, "version "+sayText(version, true))
	}
	if len(names) == 0 {
		return ""
	}
	return " " + sayList(names)
}

// fields words what an update sets, skipping the id that names the item:
// ": name “Desk”, area “Office”, disabled". A field that may hold a secret
// shows no value.
func (n *rawNamer) fields(payload map[string]any, idKey string) string {
	var parts []string
	for _, key := range sortedKeys(payload) {
		value := payload[key]
		text, isText := value.(string)
		lower := strings.ToLower(key)
		switch {
		case key == "type" || key == idKey:
		case rawSecretField(lower):
			parts = append(parts, sayText(key, false)+" set")
		case key == "area_id":
			parts = append(parts, "area "+n.area(value))
		case key == "disabled_by" && value == nil:
			parts = append(parts, "enabled")
		case key == "disabled_by":
			parts = append(parts, "disabled")
		case key == "hidden_by" && value == nil:
			parts = append(parts, "shown")
		case key == "hidden_by":
			parts = append(parts, "hidden")
		case (key == "name" || key == "name_by_user") && value == nil:
			parts = append(parts, "its own name again")
		case key == "name" || key == "name_by_user":
			parts = append(parts, "name "+sayText(text, true))
		case key == "new_entity_id":
			parts = append(parts, "entity ID "+sayText(text, true))
		case isText:
			parts = append(parts, sayText(strings.ReplaceAll(key, "_", " "), false)+" "+sayText(text, true))
		case value == nil:
			parts = append(parts, sayText(strings.ReplaceAll(key, "_", " "), false)+" cleared")
		case isObject(value):
			parts = append(parts, sayText(strings.ReplaceAll(key, "_", " "), false)+" set")
		default:
			raw, _ := json.Marshal(value)
			parts = append(parts, sayText(strings.ReplaceAll(key, "_", " "), false)+" "+sayText(string(raw), false))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, ", ")
}

func isObject(v any) bool {
	_, object := v.(map[string]any)
	return object
}

// flowFields words a flow step's form: a value only for a top-level text,
// number, or true/false whose name is clearly no secret; anything nested,
// and anything in doubt, by its name only.
func flowFields(body map[string]any) string {
	var parts []string
	for _, key := range sortedKeys(body) {
		label := sayText(strings.ReplaceAll(key, "_", " "), false)
		switch value := body[key].(type) {
		case string, bool, json.Number, float64:
			if text := sayText(fmt.Sprint(value), false); !rawSecretField(strings.ToLower(key)) && text != "" {
				parts = append(parts, label+" "+sayText(text, true))
				continue
			}
		}
		parts = append(parts, label+" set")
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, ", ")
}

// rawSecretField: a field name that may hold a password, code, key,
// token, or certificate; its value is never shown.
func rawSecretField(lower string) bool {
	for _, word := range []string{"pass", "pwd", "psk", "otp", "secret", "token", "key", "code", "credential", "community", "auth", "pin", "cert", "private"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

// energyChange: ": energy sources 3 → 2 entries" for each list it saves.
func (n *rawNamer) energyChange(payload map[string]any) string {
	prefs, ok := n.energyPrefs()
	var parts []string
	for _, key := range sortedKeys(payload) {
		next, isList := payload[key].([]any)
		if key == "type" || !isList {
			continue
		}
		before, _ := prefs[key].([]any)
		count := fmt.Sprintf("%d → %d entries", len(before), len(next))
		if !ok {
			count = fmt.Sprintf("%d entries (now: unknown)", len(next))
		}
		parts = append(parts, sayText(strings.ReplaceAll(key, "_", " "), false)+" "+count)
	}
	if len(parts) == 0 {
		return ""
	}
	return ": " + strings.Join(parts, ", ")
}
