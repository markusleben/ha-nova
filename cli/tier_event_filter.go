package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
)

// Event-trigger filters as Home Assistant attaches them
// (homeassistant/triggers/event.py): what the listener census may rule out.

// literalEntity: the entity_id an event trigger's event_data provably holds
// once Home Assistant attaches it (homeassistant/triggers/event.py renders it
// once with the trigger_variables, then fires only on events whose data
// equals it key by key): a string from a literal or from exactly {{ name }}
// with a literal string trigger variable, trimmed as Home Assistant trims a
// template (spaces and control characters, a superset of Python's strip);
// "" for a list, a map, a number, or null, which stay what they are and
// never equal an event's entity_id. No entity_id key, a templated key, or
// any other template proves nothing.
func literalEntity(filter map[string]any, vars any) (string, bool) {
	value, present := filter["entity_id"]
	if !present {
		return "", false
	}
	for key := range filter {
		if isTemplated(key) {
			return "", false
		}
	}
	s, isString := value.(string)
	if !isString {
		return "", true
	}
	if isTemplated(s) {
		m := variableTemplate.FindStringSubmatch(s)
		known, _ := vars.(map[string]any)
		if m == nil {
			return "", false
		}
		if s, isString = known[m[1]].(string); !isString || isTemplated(s) {
			return "", false
		}
	}
	return trimRendered(s), true
}

// rendered: v as Home Assistant attaches an event trigger's event_type or
// event_data, which it renders as templates: every string, keys too, trimmed
// as a Template trims its source and its render. trimRendered trims spaces
// and control characters, a superset of Python's strip, so a filter read
// here can only match more than Home Assistant's.
func rendered(v any) any {
	switch x := v.(type) {
	case string:
		return trimRendered(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = rendered(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for key, item := range x {
			if _, twice := out[trimRendered(key)]; twice { // which one Home Assistant keeps depends on the order: may be either
				out[trimRendered(key)] = "{{ either }}"
				continue
			}
			out[trimRendered(key)] = rendered(item)
		}
		return out
	}
	return v
}

func trimRendered(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) })
}

// stateChanged names the state_changed listener that may react to id's
// state, "" when none may: a running wait on such events (or one the scan
// can't read), or a trigger whose event_data may name id.
func (t *listenerTable) stateChanged(id string) string {
	if t.waits["state_changed"] || t.waits[""] {
		return "couldn't rule out a reaction from a running wait (it may be on state_changed events)"
	}
	for _, hit := range t.hits["state_changed"] {
		raw, _ := json.Marshal(hit.filter)
		switch {
		case hit.literal && hit.entity == id:
			return "state_changed trigger of " + hit.automation
		case hit.literal:
		case isTemplated(string(raw)):
			return "couldn't rule out a reaction from " + hit.automation + " (templated state_changed trigger)"
		default:
			return "couldn't rule out a reaction from " + hit.automation + " (state_changed trigger on every entity)"
		}
	}
	return ""
}

var variableTemplate = regexp.MustCompile(`^\s*\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}\s*$`)
