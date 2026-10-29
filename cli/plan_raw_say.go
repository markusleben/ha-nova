package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
)

// The plain line of each raw plan item (en "Rename the device “Hall lamp”",
// de „Das Gerät „Flurlampe“ umbenennen“), which say, the window, the phone,
// and the confirmation form show (confirm_say.go hands raw plans to
// rawSayWhat, rawGoal, and rawAsk): a verb id (rawVerb) and its object,
// both catalog texts in the plan's language, joined by raw.goal. Names are
// read where one read gets them (plan_raw_names.go); an id is shown only
// when no name can be read. A field that may hold a password, code, key, or
// token shows no value (rawSecretField).

// rawCollection: a storage collection's create, update, or delete, and the
// field that names its item.
func rawCollection(wsType string) (collection, op, idKey string) {
	cut := strings.LastIndex(wsType, "/")
	if cut < 0 || !slices.Contains(rawWSCollections, wsType[:cut]) {
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

// rawItemLine words one item as its verb id and its object in lang.
func rawItemLine(n *rawNamer, lang, endpoint string, payload map[string]any) (verb, object string) {
	action, _ := payload["action"].(string)
	target, _ := payload["path"].(string)
	switch endpoint {
	case "files":
		file := confirmT(lang, "raw.object.file", "path", sayText(target, true))
		if action == "delete_file" {
			return "delete", file
		}
		return "write", file
	case "backups":
		switch action {
		case "save":
			category, _ := payload["category"].(string)
			name, _ := payload["name"].(string)
			return "save", confirmT(lang, "raw.object.copy_new", "name", sayText(category+"/"+name, true))
		case "delete":
			file, _ := payload["file"].(string)
			return "delete", confirmT(lang, "raw.object.copy", "file", sayText(file, true))
		}
		return "delete", rawPruneLine(lang, payload)
	case "core":
		return rawCoreLine(n, lang, payload)
	}
	wsType, _ := payload["type"].(string)
	collection, op, idKey := rawCollection(wsType)
	switch {
	case wsType == "config/entity_registry/update":
		return "change", withFields(lang, confirmT(lang, "raw.object.entity", "entity", n.entity(payload["entity_id"])), n.fields(lang, payload, "entity_id"))
	case wsType == "config/entity_registry/remove":
		return "remove", confirmT(lang, "raw.object.entity_registry", "entity", n.entity(payload["entity_id"]))
	case wsType == "config/device_registry/update":
		return "change", withFields(lang, confirmT(lang, "raw.object.device", "device", n.device(payload["device_id"])), n.fields(lang, payload, "device_id"))
	case op == "create":
		name, _ := payload["name"].(string)
		item := strings.TrimSpace(confirmT(lang, "raw.object.item", "noun", rawNoun(lang, collection), "name", sayText(name, true)))
		return "create", withFields(lang, item, n.fields(lang, payload, "name"))
	case op == "update":
		item := confirmT(lang, "raw.object.item", "noun", rawNoun(lang, collection), "name", n.collectionItem(collection, idKey, payload))
		return "change", withFields(lang, item, n.fields(lang, payload, idKey))
	case op == "delete":
		return "delete", confirmT(lang, "raw.object.item", "noun", rawNoun(lang, collection), "name", n.collectionItem(collection, idKey, payload))
	case wsType == "calendar/event/delete":
		return "delete", confirmT(lang, "raw.object.event", "event", n.calendarEvent(lang, payload), "calendar", n.entity(payload["entity_id"]))
	case wsType == "energy/save_prefs":
		return "change", withFields(lang, confirmT(lang, "raw.object.energy"), n.energyChange(lang, payload))
	case wsType == "config_entries/disable" && payload["disabled_by"] == nil:
		return "enable", confirmT(lang, "raw.object.integration", "entry", n.entry(payload["entry_id"]))
	case wsType == "config_entries/disable":
		return "disable", confirmT(lang, "raw.object.integration_all", "entry", n.entry(payload["entry_id"]))
	case wsType == "config_entries/update":
		line, rest := confirmT(lang, "raw.object.integration", "entry", n.entry(payload["entry_id"])), maps.Clone(payload)
		if value, set := rest["pref_disable_polling"].(bool); set {
			line = map[bool]string{true: confirmT(lang, "raw.pref.polling_off", "object", line), false: confirmT(lang, "raw.pref.polling_on", "object", line)}[value]
		}
		if value, set := rest["pref_disable_new_entities"].(bool); set {
			line = map[bool]string{true: confirmT(lang, "raw.pref.new_entities_off", "object", line), false: confirmT(lang, "raw.pref.new_entities_on", "object", line)}[value]
		}
		delete(rest, "pref_disable_polling")
		delete(rest, "pref_disable_new_entities")
		line = withFields(lang, line, n.fields(lang, rest, "entry_id"))
		if _, polling := payload["pref_disable_polling"]; polling {
			line = confirmT(lang, "raw.note.polling", "object", line)
		}
		return "change", line
	case wsType == "config_entries/ignore_flow":
		title, _ := payload["title"].(string)
		return "ignore", confirmT(lang, "raw.object.discovered", "title", sayText(title, true))
	case wsType == "config_entries/subentries/update" || wsType == "config_entries/subentries/delete":
		name := n.named(map[string]any{"type": "config_entries/subentries/list", "entry_id": payload["entry_id"]}, payload["subentry_id"], []string{"subentry_id"}, []string{"title"})
		if wsType == "config_entries/subentries/delete" {
			return "delete", confirmT(lang, "raw.object.subentry_all", "subentry", name, "entry", n.entry(payload["entry_id"]))
		}
		line := confirmT(lang, "raw.object.subentry", "subentry", name, "entry", n.entry(payload["entry_id"]))
		return "rename", withFields(lang, line, n.fields(lang, map[string]any{"title": payload["title"]}, ""))
	case wsType == "config/auth/update":
		line := confirmT(lang, "raw.ws.account", "targets", n.targets(lang, map[string]any{"user_id": payload["user_id"]}))
		if groups, set := payload["group_ids"].([]any); set {
			line = map[bool]string{true: confirmT(lang, "raw.account.admin", "object", line), false: confirmT(lang, "raw.account.not_admin", "object", line)}[slices.Contains(groups, any("system-admin"))]
		}
		if active, set := payload["is_active"].(bool); set {
			line = map[bool]string{true: confirmT(lang, "raw.account.active", "object", line), false: confirmT(lang, "raw.account.inactive", "object", line)}[active]
		}
		rest := maps.Clone(payload)
		delete(rest, "group_ids")
		delete(rest, "is_active")
		return "change", withFields(lang, line, n.fields(lang, rest, "user_id"))
	case wsType == "config/core/update":
		return "change", withFields(lang, confirmT(lang, "raw.object.core"), n.fields(lang, payload, ""))
	case wsType == "repairs/ignore_issue":
		issue, _ := payload["issue_id"].(string)
		domain, _ := payload["domain"].(string)
		verb := map[bool]string{true: "ignore", false: "show_again"}[payload["ignore"] == true]
		return verb, confirmT(lang, "raw.object.repair_issue", "issue", sayText(issue, true), "domain", sayText(domain, true))
	case wsType == "homeassistant/expose_new_entities/set":
		assistant, _ := payload["assistant"].(string)
		verb := map[bool]string{true: "expose", false: "stop_exposing"}[payload["expose_new"] == true]
		return verb, confirmT(lang, "raw.object.expose_new", "assistant", sayText(assistant, true))
	case wsType == "backup/config/update":
		line := withFields(lang, confirmT(lang, "raw.object.backup_settings"), n.fields(lang, flatFields("", payload), ""))
		if rawRetains(payload) {
			line = confirmT(lang, "raw.note.retention", "object", line)
		}
		return "change", line
	case wsType == "labs/update":
		feature, _ := payload["preview_feature"].(string)
		domain, _ := payload["domain"].(string)
		if payload["enabled"] == true {
			return "turn_on", confirmT(lang, "raw.object.labs_on", "feature", sayText(feature, true), "domain", sayText(domain, true))
		}
		return "turn_off", confirmT(lang, "raw.object.labs_off", "feature", sayText(feature, true), "domain", sayText(domain, true))
	case wsType == "recorder/import_statistics":
		metadata, _ := payload["metadata"].(map[string]any)
		rows, _ := payload["stats"].([]any)
		id, _ := metadata["statistic_id"].(string)
		if len(rows) == 0 {
			return "import", confirmT(lang, "raw.object.import", "statistic", sayText(id, true))
		}
		firstRow, _ := rows[0].(map[string]any)
		lastRow, _ := rows[len(rows)-1].(map[string]any)
		first, _ := firstRow["start"].(string)
		last, _ := lastRow["start"].(string)
		return "import", confirmT(lang, "raw.object.import_span", "count", len(rows), "statistic", sayText(id, true), "first", sayText(first, true), "last", sayText(last, true))
	case wsType == "analytics/preferences":
		preferences, _ := payload["preferences"].(map[string]any)
		return "change", withFields(lang, confirmT(lang, "raw.object.sharing"), n.fields(lang, preferences, ""))
	}
	targets := n.targets(lang, payload)
	if phrase, known := rawWSPhrases[wsType]; known {
		verb, object = phrase(lang, targets)
		return verb, strings.TrimSpace(object)
	}
	return "send", strings.TrimSpace(confirmT(lang, "raw.ws.unknown", "type", sayText(wsType, true), "targets", targets)) // unreachable for allowlisted types (plan_raw_tier_test.go)
}

func rawCoreLine(n *rawNamer, lang string, payload map[string]any) (verb, object string) {
	method, _ := payload["method"].(string)
	target, _ := payload["path"].(string)
	target, _, _ = strings.Cut(target, "?")
	body, _ := payload["body"].(map[string]any)
	flow := strings.Contains(target, "/flow")
	switch {
	case method == http.MethodGet && strings.HasPrefix(target, "/api/repairs/"):
		return "continue", confirmT(lang, "raw.object.repair_continue")
	case method == http.MethodGet:
		return "continue", confirmT(lang, "raw.object.setup_continue")
	case target == "/api/template":
		return "render", confirmT(lang, "raw.object.template")
	case target == "/api/config/core/check_config":
		return "check", confirmT(lang, "raw.object.config_check")
	case method == http.MethodDelete && strings.HasPrefix(target, "/api/repairs/issues/fix/"):
		return "cancel", confirmT(lang, "raw.object.repair_unfinished")
	case method == http.MethodDelete && flow:
		return "cancel", confirmT(lang, "raw.object.setup_unfinished")
	case method == http.MethodDelete && rawEntryDeletePath.MatchString(target):
		return "remove", confirmT(lang, "raw.object.integration", "entry", n.entry(target[strings.LastIndex(target, "/")+1:]))
	case strings.HasPrefix(target, "/api/config/scene/config/"):
		id := target[strings.LastIndex(target, "/")+1:]
		name, _ := body["name"].(string)
		if method == http.MethodDelete {
			return "delete", confirmT(lang, "raw.object.scene", "scene", n.scene(id))
		}
		return "save", confirmT(lang, "raw.object.scene_save", "scene", sayText(firstNonEmpty(name, id), true))
	case target == "/api/repairs/issues/fix":
		return "start", confirmT(lang, "raw.object.repair_fix", "handler", sayText(fmt.Sprint(body["handler"]), true), "issue", sayText(fmt.Sprint(body["issue_id"]), true))
	case strings.HasPrefix(target, "/api/repairs/issues/fix/"):
		return "submit", withFields(lang, confirmT(lang, "raw.object.repair_step"), flowFields(lang, body))
	case strings.HasSuffix(target, "/subentries/flow"):
		handler, _ := body["handler"].([]any)
		if len(handler) == 2 {
			if body["subentry_id"] != nil {
				name := n.named(map[string]any{"type": "config_entries/subentries/list", "entry_id": handler[0]}, body["subentry_id"], []string{"subentry_id"}, []string{"title"})
				return "reconfigure", confirmT(lang, "raw.object.subentry", "subentry", name, "entry", n.entry(handler[0]))
			}
			return "add", withFields(lang, confirmT(lang, "raw.object.subentry_new", "entry", n.entry(handler[0])), flowFields(lang, map[string]any{"subentry_type": handler[1]}))
		}
	case strings.Contains(target, "/subentries/flow/"):
		return "submit", withFields(lang, confirmT(lang, "raw.object.subentry_step"), flowFields(lang, body))
	case strings.HasSuffix(target, "/options/flow"):
		return "open", confirmT(lang, "raw.object.options", "entry", n.entry(body["handler"]))
	case strings.HasSuffix(target, "/flow") && body["entry_id"] != nil:
		return "start", confirmT(lang, "raw.object.reconfigure", "entry", n.entry(body["entry_id"]))
	case strings.HasSuffix(target, "/flow"):
		handler, _ := body["handler"].(string)
		return "start", confirmT(lang, "raw.object.setup_new", "handler", sayText(handler, true))
	}
	step := confirmT(lang, "raw.object.setup_step")
	if !strings.Contains(target, "/options/") {
		flow := n.names(map[string]any{"type": "config_entries/flow/progress"}, []string{"flow_id"}, []string{"handler"})[target[strings.LastIndex(target, "/")+1:]]
		if flow != "" {
			step = confirmT(lang, "raw.object.setup_step_of", "handler", sayText(flow, true))
		}
	}
	return "submit", withFields(lang, step, flowFields(lang, body))
}

// rawPruneLine: a snapshot prune in plain words (Relay defaults: 30 days,
// 100 files, named copies kept).
func rawPruneLine(lang string, payload map[string]any) string {
	days, files := "30", "100"
	if v, set := payload["max_age_days"]; set {
		days = sayText(fmt.Sprint(v), false)
	}
	if v, set := payload["max_files"]; set {
		files = sayText(fmt.Sprint(v), false)
	}
	category, _ := payload["category"].(string)
	switch named := payload["keep_named"] == false; {
	case category != "" && named:
		return confirmT(lang, "raw.object.prune_in_named", "days", days, "files", files, "category", sayText(category, true))
	case category != "":
		return confirmT(lang, "raw.object.prune_in", "days", days, "files", files, "category", sayText(category, true))
	case named:
		return confirmT(lang, "raw.object.prune_named", "days", days, "files", files)
	}
	return confirmT(lang, "raw.object.prune", "days", days, "files", files)
}

// rawNames: an item's object as the title's names. A list of targets
// (listed) that ends the object stays names, so a long title counts them;
// the words before the list go with its first name. Any other object is
// one name, cut if long.
func rawNames(lang, object string, listed []string) []string {
	lead, isList := strings.CutSuffix(object, sayList(lang, listed))
	if len(listed) < 2 || !isList {
		return []string{object}
	}
	return append([]string{lead + listed[0]}, listed[1:]...)
}

// withFields: object with what its change sets (en "the entity “Desk”:
// name “Desk lamp”", de „die Entität „Desk“ (Name „Schreibtisch“)“).
func withFields(lang, object string, fields []string) string {
	if len(fields) == 0 {
		return object
	}
	return confirmT(lang, "raw.with_fields", "object", object, "fields", strings.Join(fields, ", "))
}

// fields words what an update sets, skipping the id that names the item
// (name “Desk”, area “Office”, disabled). A field that may hold a secret
// shows no value.
func (n *rawNamer) fields(lang string, payload map[string]any, idKey string) []string {
	var parts []string
	for _, key := range sortedKeys(payload) {
		value := payload[key]
		text, isText := value.(string)
		label := sayText(strings.ReplaceAll(key, "_", " "), false)
		switch {
		case key == "type" || key == idKey:
		case rawSecretField(strings.ToLower(key)):
			parts = append(parts, confirmT(lang, "raw.field.set", "field", sayText(key, false)))
		case key == "area_id" && value == nil:
			parts = append(parts, confirmT(lang, "raw.field.no_area"))
		case key == "area_id":
			parts = append(parts, confirmT(lang, "raw.field.area", "area", n.area(value)))
		case key == "disabled_by" && value == nil:
			parts = append(parts, confirmT(lang, "raw.field.enabled"))
		case key == "disabled_by":
			parts = append(parts, confirmT(lang, "raw.field.disabled"))
		case key == "hidden_by" && value == nil:
			parts = append(parts, confirmT(lang, "raw.field.shown"))
		case key == "hidden_by":
			parts = append(parts, confirmT(lang, "raw.field.hidden"))
		case (key == "name" || key == "name_by_user") && value == nil:
			parts = append(parts, confirmT(lang, "raw.field.own_name"))
		case key == "name" || key == "name_by_user":
			parts = append(parts, confirmT(lang, "raw.field.name", "name", sayText(text, true)))
		case key == "new_entity_id":
			parts = append(parts, confirmT(lang, "raw.field.entity_id", "id", sayText(text, true)))
		case isText:
			parts = append(parts, confirmT(lang, "raw.field.value", "field", label, "value", sayText(text, true)))
		case value == nil:
			parts = append(parts, confirmT(lang, "raw.field.cleared", "field", label))
		case isObject(value):
			parts = append(parts, confirmT(lang, "raw.field.set", "field", label))
		default:
			raw, _ := json.Marshal(value)
			parts = append(parts, confirmT(lang, "raw.field.value", "field", label, "value", sayText(string(raw), false)))
		}
	}
	return parts
}

func isObject(v any) bool {
	_, object := v.(map[string]any)
	return object
}

// flowFields words a flow step's form: a value only for a top-level text,
// number, or true/false whose name is clearly no secret; anything nested,
// and anything in doubt, by its name only.
func flowFields(lang string, body map[string]any) []string {
	var parts []string
	for _, key := range sortedKeys(body) {
		label := sayText(strings.ReplaceAll(key, "_", " "), false)
		switch value := body[key].(type) {
		case string, bool, json.Number, float64:
			if text := sayText(fmt.Sprint(value), false); !rawSecretField(strings.ToLower(key)) && text != "" {
				parts = append(parts, confirmT(lang, "raw.field.value", "field", label, "value", sayText(text, true)))
				continue
			}
		}
		parts = append(parts, confirmT(lang, "raw.field.set", "field", label))
	}
	return parts
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

// flatFields spells nested settings as one level ("schedule recurrence"), so
// fields shows their values instead of "schedule set".
func flatFields(prefix string, payload map[string]any) map[string]any {
	flat := map[string]any{}
	for key, value := range payload {
		if key == "type" && prefix == "" {
			continue
		}
		if nested, ok := value.(map[string]any); ok && len(nested) > 0 {
			maps.Copy(flat, flatFields(prefix+key+" ", nested))
			continue
		}
		flat[prefix+key] = value
	}
	return flat
}

// energyChange: "energy sources 3 → 2 entries" for each list it saves.
func (n *rawNamer) energyChange(lang string, payload map[string]any) []string {
	prefs, ok := n.energyPrefs()
	var parts []string
	for _, key := range sortedKeys(payload) {
		next, isList := payload[key].([]any)
		if key == "type" || !isList {
			continue
		}
		before, _ := prefs[key].([]any)
		field := sayText(strings.ReplaceAll(key, "_", " "), false)
		if !ok {
			parts = append(parts, confirmT(lang, "raw.energy.unknown", "field", field, "count", len(next)))
			continue
		}
		parts = append(parts, confirmT(lang, "raw.energy.count", "field", field, "before", len(before), "count", len(next)))
	}
	return parts
}
