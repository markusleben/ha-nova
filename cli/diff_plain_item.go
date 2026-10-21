package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// The step phrases of plain preview rows (diff_plain.go).

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
