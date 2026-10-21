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
