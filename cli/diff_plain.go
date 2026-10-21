package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Plain preview rows: while a plan renders the diff its user confirms,
// entity and area ids show as the names people know and values read in
// plain words ("Saturday and Sunday", "on", "22:00"). A name stands in for
// an id only where no other entity has it; a shared name keeps its id
// beside it, so no row can mean two things. The plan still binds the exact
// ids (config.body, the plan hash). The comparisons (read-back, revert
// guard, snapshot verify) never set names, so they stay exact; nor does
// `ha-nova diff`, which compares two local files without Home Assistant.

// previewNames is set only around a preview render (withPreviewNames).
// ponytail: a package variable, since verbs run one at a time (the MCP
// server holds mcpVerbLock); pass it down if renders ever run concurrently.
var previewNames *plainNames

type plainNames struct {
	entity map[string]string // entity_id → “Name”, or “Name” (entity_id) when another entity has that name too
	area   map[string]string // area_id → “Name”
}

func withPreviewNames(names *plainNames, render func()) {
	previewNames = names
	defer func() { previewNames = nil }()
	render()
}

// loadPreviewNames reads every entity's friendly name, and the area names
// when the draft sets an area; nil when the states can't be read, which
// keeps the exact ids.
func loadPreviewNames(s *relaySession, draft []byte) *plainNames {
	states, err := loadStates(s)
	if err != nil {
		return nil
	}
	names := &plainNames{entity: map[string]string{}, area: map[string]string{}}
	ids := map[string]string{}
	for _, st := range states {
		ids[st.EntityID] = st.Attributes.FriendlyName
	}
	uniqueNames(ids, names.entity)
	if bytes.Contains(draft, []byte(`"area_id"`)) {
		var areas []struct {
			AreaID string `json:"area_id"`
			Name   string `json:"name"`
		}
		if s.wsDecode(map[string]any{"type": "config/area_registry/list"}, &areas, "the area registry") == nil {
			ids := map[string]string{}
			for _, a := range areas {
				ids[a.AreaID] = a.Name
			}
			uniqueNames(ids, names.area)
		}
	}
	return names
}

// uniqueNames fills shown with each id's quoted name. The id leads, with the
// name beside it, where the name alone could point at another one: two ids
// share it (case-insensitive, as rendered), it mixes scripts (a Cyrillic
// “о” posing as a Latin one), or it holds an id-like text. The id leads so
// a cut cell never loses it.
// ponytail: mixed scripts catch the common look-alike trick, not a name
// written wholly in another script; a confusables table if that shows up.
func uniqueNames(ids map[string]string, shown map[string]string) {
	count := map[string]int{}
	for _, name := range ids {
		count[strings.ToLower(sayText(name, false))]++
	}
	for id, name := range ids {
		switch text := sayText(name, false); {
		case text == "":
		case count[strings.ToLower(text)] == 1 && !mixedScripts(text) && !idLike.MatchString(text):
			shown[id] = "“" + text + "”"
		default:
			shown[id] = id + " (“" + text + "”)"
		}
	}
}

var idLike = regexp.MustCompile(`[a-z0-9_]+\.[a-z0-9_]+`)

// mixedScripts: letters from more than one of Latin, Greek, and Cyrillic.
func mixedScripts(s string) bool {
	seen := 0
	for _, script := range []*unicode.RangeTable{unicode.Latin, unicode.Greek, unicode.Cyrillic} {
		if strings.IndexFunc(s, func(r rune) bool { return unicode.Is(script, r) }) >= 0 {
			seen++
		}
	}
	return seen > 1
}

// nameOf: an entity id's shown name during a preview render, else the id.
func nameOf(id string) string {
	if previewNames != nil && previewNames.entity[id] != "" {
		return previewNames.entity[id]
	}
	return id
}

func areaOf(id string) string {
	if previewNames != nil && previewNames.area[id] != "" {
		return previewNames.area[id]
	}
	return id
}

// Keys whose string value may be an entity id; nothing else is renamed, so
// a message or template that happens to look like one stays as written.
var (
	entityKeys = map[string]bool{"entity_id": true, "entity": true, "entities": true, "at": true, "above": true, "below": true}
	clockKeys  = map[string]bool{"at": true, "after": true, "before": true}
	spanKeys   = map[string]bool{"for": true, "delay": true, "timeout": true}
	stateKeys  = map[string]bool{"state": true, "to": true, "from": true, "not_to": true, "not_from": true}
	weekdays   = map[string]string{"mon": "Monday", "tue": "Tuesday", "wed": "Wednesday", "thu": "Thursday", "fri": "Friday", "sat": "Saturday", "sun": "Sunday"}
	modes      = map[string]string{"single": "one run at a time", "restart": "a new run restarts it", "queued": "runs wait in line", "parallel": "runs at the same time"}
)

// plainCell: a config diff value in plain words; ok=false keeps
// formatValue. Only during a preview render.
func plainCell(segs []segment, v any) (string, bool) {
	if previewNames == nil {
		return "", false
	}
	key := ""
	for i := len(segs) - 1; i >= 0 && key == ""; i-- {
		key = segs[i].key
	}
	if text, isMode := modes[fmt.Sprint(v)]; isMode && key == "mode" && len(segs) == 1 {
		return text, true
	}
	return plainValue(key, v)
}

// plainValue words one value by the key that holds it.
func plainValue(key string, v any) (string, bool) {
	switch t := v.(type) {
	case bool:
		if stateKeys[key] { // a state compared as text: true is "True", never "on"
			return "", false
		}
		return map[bool]string{true: "on", false: "off"}[t], true
	case string:
		switch {
		case entityKeys[key] && entityIDPattern.MatchString(t):
			return nameOf(t), true
		case key == "area_id":
			return areaOf(t), true
		case key == "weekday":
			day, ok := weekdays[t]
			return day, ok
		case clockKeys[key]:
			return plainClock(t)
		case spanKeys[key]:
			return plainSpan(t)
		}
	case []any:
		return plainList(key, t)
	case map[string]any:
		if isDurationMap(t) {
			return plainDuration(t), true
		}
		text := plainItem(t)
		return text, text != ""
	}
	return "", false
}

// plainList: ids as names, weekdays as days, steps as their phrases; any
// other list keeps its exact form.
func plainList(key string, items []any) (string, bool) {
	var parts []string
	for _, item := range items {
		text, ok := plainValue(key, item)
		if _, isText := item.(string); !ok || isText && !entityKeys[key] && key != "area_id" && key != "weekday" {
			return "", false
		}
		parts = append(parts, text)
	}
	switch {
	case len(parts) == 0:
		return "", false
	case key == "weekday":
		return joinAnd(parts), true
	case key == "at":
		return strings.Join(parts, " or "), true
	case key == "" || slices.Contains([]string{"triggers", "conditions", "actions", "sequence"}, key):
		return strings.Join(parts, "; "), true
	}
	return strings.Join(parts, ", "), true
}

func joinAnd(parts []string) string {
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// plainClock: "22:00:00" → "22:00" (Home Assistant times are the home's
// local time); anything else (sunset, seconds, a template) stays exact.
func plainClock(t string) (string, bool) {
	if m := clockPattern.FindStringSubmatch(t); m != nil && (m[3] == "" || m[3] == "00") {
		return m[1] + ":" + m[2], true
	}
	if entityIDPattern.MatchString(t) {
		return "the time in " + nameOf(t), true
	}
	return "", false
}

// The whole string must match: "6:0:0.5" or "0:0:1.5" stay exact.
var (
	clockPattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])(?::([0-5][0-9]))?$`)
	spanPattern  = regexp.MustCompile(`^([0-9]{1,3}):([0-5][0-9]):([0-5][0-9])$`)
)

// plainSpan: "00:05:00" → "5 min".
func plainSpan(t string) (string, bool) {
	m := spanPattern.FindStringSubmatch(t)
	if m == nil {
		return "", false
	}
	n := func(s string) json.Number { return json.Number(strings.TrimLeft(s[:len(s)-1], "0") + s[len(s)-1:]) }
	return plainDuration(map[string]any{"hours": n(m[1]), "minutes": n(m[2]), "seconds": n(m[3])}), true
}

// plainDuration drops zero parts: {"hours":0,"minutes":5} → "5 min".
func plainDuration(m map[string]any) string {
	var parts []string
	for _, u := range durationUnits {
		v, ok := m[u.key]
		if n, isNumber := v.(json.Number); !ok || isNumber && n.String() == "0" {
			continue
		}
		parts = append(parts, formatValue(v)+" "+u.suffix)
	}
	if len(parts) == 0 {
		return "0 s"
	}
	return strings.Join(parts, " ")
}

// plainItem words a trigger, condition, or step whose every key it knows
// ("“Hall Motion” changes to on", "only on Saturday and Sunday", "turn on
// “Decorative Lights”"); "" for any other shape, which keeps its exact
// rendering. A key it doesn't know is never dropped from a row.
func plainItem(m map[string]any) string {
	only := func(keys ...string) bool {
		for key := range m {
			if !slices.Contains(keys, key) {
				return false
			}
		}
		return true
	}
	value := func(key string) string {
		if m[key] == nil {
			return ""
		}
		text, ok := plainValue(key, m[key])
		if !ok {
			text = formatValue(m[key])
			if _, isText := m[key].(string); !isText {
				if n, isNumber := m[key].(json.Number); isNumber {
					text = n.String()
				} else {
					text = "\x00" // not a plain value: the item keeps its exact form
				}
			}
		}
		return text
	}
	var text string
	switch trigger := cmpStr(m, "trigger", "platform"); {
	case trigger != "":
		text = plainTrigger(m, trigger, only, value)
	case m["condition"] != nil:
		text = plainCondition(m, only, value)
	case cmpStr(m, "action", "service") != "":
		text = plainStep(m, only)
	case only("delay") && value("delay") != "":
		text = "wait " + value("delay")
	}
	if strings.Contains(text, "\x00") {
		return ""
	}
	return text
}

// cmpStr: the first of keys holding a string.
func cmpStr(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, _ := m[key].(string); s != "" {
			return s
		}
	}
	return ""
}

func plainTrigger(m map[string]any, kind string, only func(...string) bool, value func(string) string) string {
	key := "trigger"
	if m["trigger"] == nil {
		key = "platform"
	}
	who, _ := plainWho(m["entity_id"], " or ")
	forText := ""
	if f := value("for"); f != "" {
		forText = " for " + f
	}
	switch {
	case kind == "state" && only(key, "entity_id", "to", "from", "for") && who != "" && hasNonNull(m, "to", "from"):
		text := who + " changes"
		if from := value("from"); from != "" {
			text += " from " + from
		}
		if to := value("to"); to != "" {
			text += " to " + to
		}
		return text + forText
	case kind == "numeric_state" && only(key, "entity_id", "above", "below", "for") && who != "":
		return who + plainRange(value("above"), value("below"), "goes") + forText
	case kind == "time" && only(key, "at") && value("at") != "":
		return "at " + value("at")
	case kind == "sun" && only(key, "event", "offset") && value("event") != "":
		offset := ""
		if o := value("offset"); o != "" {
			offset = " (offset " + o + ")"
		}
		return "at " + value("event") + offset
	case kind == "homeassistant" && only(key, "event"):
		return map[string]string{"start": "when Home Assistant starts", "shutdown": "when Home Assistant shuts down"}[value("event")]
	}
	return ""
}

// hasNonNull: every present key of keys holds a value (a null "to" means
// "any state, not attributes", which the phrase would hide).
func hasNonNull(m map[string]any, keys ...string) bool {
	for _, key := range keys {
		if v, present := m[key]; present && v == nil {
			return false
		}
	}
	return true
}

func plainCondition(m map[string]any, only func(...string) bool, value func(string) string) string {
	kind, _ := m["condition"].(string)
	who, many := plainWho(m["entity_id"], " and ")
	is := map[bool]string{false: " is ", true: " are "}[many]
	switch {
	case kind == "time" && only("condition", "after", "before", "weekday"):
		var parts []string
		if after := value("after"); after != "" {
			parts = append(parts, "after "+after)
		}
		if before := value("before"); before != "" {
			parts = append(parts, "before "+before)
		}
		if days := value("weekday"); days != "" {
			parts = append(parts, "only on "+days)
		}
		return strings.Join(parts, ", ")
	case kind == "state" && only("condition", "entity_id", "state", "for") && who != "" && value("state") != "":
		text := who + is + value("state")
		if f := value("for"); f != "" {
			text += " for " + f
		}
		return text
	case kind == "numeric_state" && only("condition", "entity_id", "above", "below") && who != "":
		return who + plainRange(value("above"), value("below"), strings.TrimSpace(is))
	}
	return ""
}

func plainRange(above, below, verb string) string {
	switch {
	case above != "" && below != "":
		return " " + verb + " above " + above + " and below " + below
	case above != "":
		return " " + verb + " above " + above
	case below != "":
		return " " + verb + " below " + below
	}
	return "\x00"
}

// plainStep: "turn on “Decorative Lights” with brightness_pct 40" for a
// service step that targets entities only, with plain data values.
func plainStep(m map[string]any, only func(...string) bool) string {
	key := "action"
	if m["action"] == nil {
		key = "service"
	}
	target, _ := m["target"].(map[string]any)
	metadata, isMap := m["metadata"].(map[string]any)
	switch {
	case !only(key, "target", "entity_id", "data", "metadata"),
		!entityIDPattern.MatchString(cmpStr(m, key)), // a templated service keeps its exact form
		m["target"] != nil && (target == nil || len(target) != 1 || target["entity_id"] == nil),
		m["metadata"] != nil && (!isMap || len(metadata) != 0),
		(m["target"] == nil) == (m["entity_id"] == nil):
		return ""
	}
	ids := m["entity_id"]
	if target != nil {
		ids = target["entity_id"]
	}
	who, _ := plainWho(ids, " and ")
	if who == "" {
		return ""
	}
	_, name := splitID(cmpStr(m, key))
	text := lowerFirst(sayVerb(name)) + " " + who
	if mode, isArm := strings.CutPrefix(name, "alarm_arm_"); isArm { // sayVerb says only "Arm": the mode is the change
		text += " in " + strings.ReplaceAll(mode, "_", " ") + " mode"
	}
	data, _ := m["data"].(map[string]any)
	if m["data"] != nil && data == nil {
		return ""
	}
	var parts []string
	for _, k := range sortedKeys(data) {
		switch v := data[k].(type) {
		case string, json.Number, bool:
			if text, isText := v.(string); isText {
				parts = append(parts, fmt.Sprintf("%s %q", k, text)) // quoted: a value never reads as another field or step
			} else {
				parts = append(parts, k+" "+formatValue(v))
			}
		default:
			return ""
		}
	}
	if len(parts) > 0 {
		text += " with " + strings.Join(parts, ", ")
	}
	return text
}

// plainWho names the entities an item is about; many: more than one.
func plainWho(ids any, join string) (string, bool) {
	var list []any
	switch t := ids.(type) {
	case string:
		list = []any{t}
	case []any:
		list = t
	}
	var names []string
	for _, id := range list {
		s, ok := id.(string)
		if !ok || !entityIDPattern.MatchString(s) {
			return "", false
		}
		names = append(names, nameOf(s))
	}
	if join == " and " {
		return joinAnd(names), len(names) > 1
	}
	return strings.Join(names, join), len(names) > 1
}
