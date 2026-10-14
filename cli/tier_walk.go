package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Stored-member traversal (indirect-actuation.md → Expanding the members).

// walk unions every branch of an action sequence (choose/if/parallel/
// repeat) and classifies each concrete step; enabled:false is skipped.
func (e *tierEngine) walk(node any, via string, depth int) {
	switch n := node.(type) {
	case []any:
		for i, child := range n {
			e.walk(child, via+"/"+strconv.Itoa(i), depth)
		}
	case map[string]any:
		if n["enabled"] == false || n["condition"] != nil || n["trigger"] != nil || n["platform"] != nil {
			return
		}
		name, _ := n["action"].(string)
		if name == "" {
			name, _ = n["service"].(string)
		}
		domain, _ := n["domain"].(string)
		actionType, _ := n["type"].(string)
		switch {
		case name != "":
			e.storedCall(name, n, via, depth)
		case n["service_template"] != nil:
			e.gap(reasonTemplatedAction, "", via, "service_template")
		case domain != "" && actionType != "" && n["device_id"] != nil:
			e.storedCall(deviceActionService(domain, actionType), map[string]any{"device_id": n["device_id"], "data": map[string]any{"position": n["position"]}}, via, depth)
		case n["scene"] != nil:
			e.storedCall("scene.turn_on", map[string]any{"entity_id": n["scene"]}, via, depth)
		case n["event"] != nil:
			if eventType, _ := n["event"].(string); eventType == "" || isTemplated(eventType) {
				e.gap(reasonListener, "", via, "templated event type")
			} else {
				e.checkEvents(eventType, "")
			}
		}
		for _, key := range sortedKeys(n) {
			if !walkSkipKeys[key] {
				e.walk(n[key], via+"/"+key, depth)
			}
		}
	}
}

// Keys holding conditions, triggers, payloads, or targets — never actions.
var walkSkipKeys = map[string]bool{
	"conditions": true, "if": true, "while": true, "until": true, "triggers": true, "wait_for_trigger": true,
	"variables": true, "data": true, "data_template": true, "target": true, "entity_id": true, "device_id": true,
	"area_id": true, "event_data": true, "event_data_template": true, "response_variable": true, "metadata": true,
}

// storedCall classifies one stored service step against its literal targets.
func (e *tierEngine) storedCall(name string, step map[string]any, via string, depth int) {
	if isTemplated(name) || !entityIDPattern.MatchString(name) {
		e.gap(reasonTemplatedAction, "", via, name)
		return
	}
	domain, service := splitID(name)
	data, _ := step["data"].(map[string]any)
	targets := collectTargets(step)
	if len(targets["floor_id"])+len(targets["label_id"]) > 0 {
		e.gap(reasonStoredSelector, "", via, name)
	}
	ids := targets["entity_id"]
	for _, area := range targets["area_id"] {
		ids = append(ids, e.areaEntities(area, domain, via)...)
	}
	for _, device := range targets["device_id"] {
		ids = append(ids, e.deviceEntities(device, domain)...)
	}
	if domain == "script" && !scriptEntityServices[service] {
		ids, name = append(ids, name), "script.turn_on"
	}
	if len(ids) == 0 {
		level, code := classifyAction(name, data, nil)
		e.raise(level, tierReason{Code: code, Via: via})
		e.members = append(e.members, tierMember{Action: name, Via: via, Class: level.String()})
		return
	}
	for _, id := range ids {
		if id == "all" || id == "none" || isTemplated(id) || !entityIDPattern.MatchString(id) {
			e.gap(reasonUnresolvedTarget, "", via, name+" → "+id)
			continue
		}
		e.visit(id, name, data, depth, via)
	}
}

// collectTargets gathers target keys from target, data, and the step root;
// a comma-separated string is a list, as Home Assistant reads it.
func collectTargets(step map[string]any) map[string][]string {
	out := map[string][]string{}
	sources := []map[string]any{step}
	for _, key := range []string{"target", "data", "data_template"} {
		if m, isMap := step[key].(map[string]any); isMap {
			sources = append(sources, m)
		}
	}
	for _, source := range sources {
		for _, key := range []string{"entity_id", "area_id", "device_id", "floor_id", "label_id"} {
			switch v := source[key].(type) {
			case string:
				for _, part := range strings.Split(v, ",") {
					out[key] = append(out[key], strings.TrimSpace(part))
				}
			case []any:
				for _, item := range v {
					out[key] = append(out[key], fmt.Sprint(item))
				}
			case nil:
			default:
				out[key] = append(out[key], fmt.Sprint(v))
			}
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
