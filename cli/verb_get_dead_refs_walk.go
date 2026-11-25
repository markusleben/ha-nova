package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// The positions get --dead-references reads a literal reference at, as
// Home Assistant reads them: a step's, trigger's, or condition's entity_id,
// device_id, area_id, scene, and zone, a numeric_state's above and below
// entity, a step's action, the top-level ids of its data (entity_id,
// *_entity_id, device_id, area_id) and scene.apply's entity keys, and a
// state_changed event filter's entity_id. Variables, repeat for_each items,
// event payloads, data below its top level, and script fields hold no
// literal references; their templates still count.

var (
	deadRefServicePattern  = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+$`)
	deadRefRegistryPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	deadRefTemplateCall    = regexp.MustCompile(`\b(?:states|is_state|state_attr|is_state_attr|has_value)\(\s*['"]([a-z0-9_]+\.[a-z0-9_]+)['"]\s*[,)]`)
	deadRefTemplateExpand  = regexp.MustCompile(`\bexpand\(\s*((?:['"][a-z0-9_]+\.[a-z0-9_]+['"]\s*,\s*)*['"][a-z0-9_]+\.[a-z0-9_]+['"])\s*\)`)
	deadRefTemplateLiteral = regexp.MustCompile(`['"]([a-z0-9_]+\.[a-z0-9_]+)['"]`)
	deadRefTemplateAttr    = regexp.MustCompile(`\bstates\.([a-z0-9_]+\.[a-z0-9_]+)\b`)
)

func walkDeadRefs(path string, v any, opaque bool, add func(kind, ref, path string, template bool)) {
	switch t := v.(type) {
	case map[string]any:
		if !opaque {
			deadRefsAt(path, t, add)
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			child := joinRefPath(path, k)
			switch k {
			case "fields":
				continue
			case "data", "service_data":
				if data, isMap := t[k].(map[string]any); isMap && !opaque {
					deadRefsInData(child, t, data, add)
				}
				walkDeadRefs(child, t[k], true, add)
			case "event_data":
				if data, isMap := t[k].(map[string]any); isMap && !opaque && t["event_type"] == "state_changed" {
					deadRefIDs(joinRefPath(child, "entity_id"), "entity", data["entity_id"], add)
				}
				walkDeadRefs(child, t[k], true, add)
			default:
				walkDeadRefs(child, t[k], opaque || k == "variables" || k == "trigger_variables" || k == "for_each", add)
			}
		}
	case []any:
		for i, item := range t {
			walkDeadRefs(fmt.Sprintf("%s[%d]", path, i), item, opaque, add)
		}
	case string:
		if !isTemplated(t) {
			return
		}
		for _, re := range []*regexp.Regexp{deadRefTemplateCall, deadRefTemplateAttr} {
			for _, m := range re.FindAllStringSubmatch(t, -1) {
				add("entity", m[1], path, true)
			}
		}
		for _, m := range deadRefTemplateExpand.FindAllStringSubmatch(t, -1) {
			for _, id := range deadRefTemplateLiteral.FindAllStringSubmatch(m[1], -1) {
				add("entity", id[1], path, true)
			}
		}
	}
}

// deadRefsAt: the references a step, trigger, condition, or target names
// by its own keys.
func deadRefsAt(path string, m map[string]any, add func(kind, ref, path string, template bool)) {
	numeric := m["trigger"] == "numeric_state" || m["platform"] == "numeric_state" || m["condition"] == "numeric_state"
	for k, v := range m {
		child := joinRefPath(path, k)
		switch {
		case k == "entity_id", k == "device_id", k == "area_id":
			deadRefIDs(child, strings.TrimSuffix(k, "_id"), v, add)
		case k == "scene" || k == "zone" || numeric && (k == "above" || k == "below"):
			for _, id := range deadRefLiterals(v) {
				if domain, _ := splitID(id); entityIDPattern.MatchString(id) && (domain == k || k == "above" || k == "below") {
					add("entity", id, child, false)
				}
			}
		case k == "action" || k == "service":
			if name, _ := v.(string); deadRefServicePattern.MatchString(name) {
				add("action", name, child, false)
			}
		}
	}
}

// deadRefsInData: the ids at the top level of a step's data, and the
// entities scene.apply and scene.create set.
func deadRefsInData(path string, step, data map[string]any, add func(kind, ref, path string, template bool)) {
	for k, v := range data {
		switch {
		case k == "entity_id" || strings.HasSuffix(k, "_entity_id"):
			deadRefIDs(joinRefPath(path, k), "entity", v, add)
		case k == "device_id", k == "area_id":
			deadRefIDs(joinRefPath(path, k), strings.TrimSuffix(k, "_id"), v, add)
		}
	}
	action, _ := step["action"].(string)
	if service, _ := step["service"].(string); action == "" {
		action = service
	}
	if entities, isMap := data["entities"].(map[string]any); isMap && (action == "scene.apply" || action == "scene.create") {
		for id := range entities {
			deadRefIDs(joinRefPath(path, "entities"), "entity", id, add)
		}
	}
}

// deadRefIDs reports each literal id of an id field; an entity field also
// takes a registry id (device steps name entities by it).
func deadRefIDs(path, kind string, v any, add func(kind, ref, path string, template bool)) {
	for _, id := range deadRefLiterals(v) {
		if kind != "entity" || entityIDPattern.MatchString(id) || deadRefRegistryPattern.MatchString(id) {
			add(kind, id, path, false)
		}
	}
}

// deadRefLiterals: the literal ids of an id field (a string, a comma list,
// or a list); templated values and the all/none keywords are no ids.
func deadRefLiterals(v any) []string {
	var parts []string
	switch t := v.(type) {
	case string:
		if isTemplated(t) {
			return nil
		}
		parts = strings.Split(t, ",")
	case []any:
		for _, item := range t {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
	}
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && p != "all" && p != "none" && !isTemplated(p) {
			out = append(out, p)
		}
	}
	return out
}
