package main

import (
	"encoding/json"
	"slices"
	"strings"
)

// quietSync: an automation that only puts a heater's switch or fan back in
// step after it reconnects (MAR-245) doesn't make that entity loud, when
// nothing reacts to the sync running either: no automation on
// automation_triggered, no wait that could be one, and nothing search/related
// ties to the sync's own entity.
func (e *tierEngine) quietSync(cfg map[string]any, automation, id string) bool {
	if !reconnectSync(cfg, id) || len(e.listeners.hits["automation_triggered"]) > 0 || e.listeners.waits["automation_triggered"] || e.listeners.waits[""] {
		return false
	}
	related, err := e.related("entity", automation)
	return err == nil && len(related["script"]) == 0 && len(related["group"]) == 0 &&
		!slices.ContainsFunc(related["automation"], func(a string) bool { return a != automation })
}

// reconnectSync: every trigger is a state trigger on id alone from only
// unavailable (to unset or on/off, a literal for allowed, nothing else), and
// every action is a literal delay, a choose or if on conditions, or a
// literal turn_on/turn_off of id itself with no data. A turn_on/turn_off
// leaves an available entity on or off, never unavailable, so it doesn't set
// the trigger off (an available entity can be unknown: not allowed); should
// the call itself take the entity away, the automation only sends
// turn_on/turn_off to it again, calls the set already holds (calledOn adds
// "<domain>.*", the helper rule homeassistant.turn_on/turn_off).
func reconnectSync(cfg map[string]any, id string) bool {
	domain, _ := splitID(id)
	triggers, ok := rawTriggers(firstPresent(cfg, "triggers", "trigger"))
	if domain != "switch" && domain != "fan" || !ok || len(triggers) == 0 {
		return false
	}
	for _, t := range triggers {
		_, toSet := t["to"]
		if !onlyKeys(t, "trigger", "platform", "entity_id", "from", "to", "for", "id", "enabled") || firstPresent(t, "trigger", "platform") != "state" ||
			!literalIn(t["entity_id"], id) || !literalIn(t["from"], "unavailable") || toSet && !literalIn(t["to"], "on", "off") || templated(t["for"]) {
			return false
		}
	}
	return reconnectSteps(firstPresent(cfg, "actions", "action"), id)
}

// rawTriggers: every trigger as written, disabled ones included (Home
// Assistant may still attach one), through exact {triggers: …} wrappers.
func rawTriggers(node any) ([]map[string]any, bool) {
	var out []map[string]any
	switch n := node.(type) {
	case []any:
		for _, child := range n {
			more, ok := rawTriggers(child)
			if !ok {
				return nil, false
			}
			out = append(out, more...)
		}
	case map[string]any:
		if nested, wrapped := n["triggers"]; wrapped {
			if len(n) != 1 {
				return nil, false
			}
			return rawTriggers(nested)
		}
		out = append(out, n)
	default:
		return nil, false
	}
	return out, true
}

func reconnectSteps(node any, id string) bool {
	domain, _ := splitID(id)
	steps, isList := node.([]any)
	if !isList {
		steps = []any{node}
	}
	for _, raw := range steps {
		step, isMap := raw.(map[string]any)
		switch {
		case !isMap:
			return false
		case step["delay"] != nil:
			if !onlyKeys(step, "delay", "alias") || templated(step["delay"]) {
				return false
			}
		case step["choose"] != nil:
			options, _ := step["choose"].([]any)
			if !onlyKeys(step, "choose", "default", "alias") || len(options) == 0 || step["default"] != nil && !reconnectSteps(step["default"], id) {
				return false
			}
			for _, option := range options {
				m, _ := option.(map[string]any)
				if m == nil || !onlyKeys(m, "conditions", "sequence", "alias") || !reconnectSteps(m["sequence"], id) {
					return false
				}
			}
		case step["if"] != nil:
			if !onlyKeys(step, "if", "then", "else", "alias") || !reconnectSteps(step["then"], id) || step["else"] != nil && !reconnectSteps(step["else"], id) {
				return false
			}
		default:
			service, _ := firstPresent(step, "action", "service").(string)
			target, _ := step["target"].(map[string]any)
			entity := step["entity_id"]
			if target != nil {
				entity = target["entity_id"]
			}
			if !onlyKeys(step, "action", "service", "target", "entity_id", "alias") || step["target"] != nil && (target == nil || !onlyKeys(target, "entity_id")) ||
				target != nil && step["entity_id"] != nil || !slices.Contains([]string{domain + ".turn_on", domain + ".turn_off", "homeassistant.turn_on", "homeassistant.turn_off"}, service) ||
				!literalIn(entity, id) {
				return false
			}
		}
	}
	return true
}

func onlyKeys(m map[string]any, keys ...string) bool {
	for k, v := range m {
		if !slices.Contains(keys, k) && !(k == "enabled" && v == true) {
			return false
		}
	}
	return true
}

// literalIn: v is one of the allowed strings, or a non-empty list of them.
func literalIn(v any, allowed ...string) bool {
	if s, isString := v.(string); isString {
		return slices.Contains(allowed, s)
	}
	list, _ := v.([]any)
	for _, item := range list {
		if s, _ := item.(string); !slices.Contains(allowed, s) {
			return false
		}
	}
	return len(list) > 0
}

// templated: a duration a template could make fail (and log).
func templated(v any) bool {
	raw, _ := json.Marshal(v)
	return strings.Contains(string(raw), "{{") || strings.Contains(string(raw), "{%")
}
