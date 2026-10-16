package main

import (
	"encoding/json"
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
			e.storedCall(name, n, via, depth, false)
		case n["service_template"] != nil:
			e.gap(reasonTemplatedAction, "", via, "service_template")
		case domain != "" && actionType != "" && n["device_id"] != nil:
			e.storedCall(deviceActionService(domain, actionType), map[string]any{"device_id": n["device_id"], "data": map[string]any{"position": n["position"]}}, via, depth, true)
		case n["scene"] != nil:
			e.storedCall("scene.turn_on", map[string]any{"entity_id": n["scene"]}, via, depth, false)
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
	"for_each": true,
}

// storedCall classifies one stored service step against its literal
// targets. A payload or target Home Assistant renders at runtime is unknown.
// deviceForm marks a device action (domain + type), which acts on its own
// chosen entity, hidden or categorized or not.
func (e *tierEngine) storedCall(name string, step map[string]any, via string, depth int, deviceForm bool) {
	if isTemplated(name) || !entityIDPattern.MatchString(name) {
		e.gap(reasonTemplatedAction, "", via, name)
		return
	}
	data, dataKnown := stepData(step)
	targets, targetsKnown := collectTargets(step)
	if !dataKnown || !targetsKnown {
		e.gap(reasonUnresolvedTarget, "", via, name)
		return
	}
	if name == "scene.apply" {
		e.sceneMembers(data["entities"], via, depth)
		return
	}
	domain, service := splitID(name)
	if len(targets["floor_id"])+len(targets["label_id"]) > 0 {
		e.gap(reasonStoredSelector, "", via, name)
	}
	ids := targets["entity_id"]
	var devices []string
	for _, area := range targets["area_id"] {
		ids = append(ids, e.areaEntities(area, domain, via)...)
		if related, err := e.related("area", area); err == nil {
			devices = append(devices, related["device"]...)
		}
	}
	for _, device := range targets["device_id"] {
		family := e.familyEntities(device, domain)
		if !deviceForm { // a service call skips what Home Assistant's target helper skips
			family = e.primaryEntities(family, "", domain)
		}
		ids = append(ids, family...)
		devices = append(devices, e.deviceFamily(device)...)
	}
	if !coreDomains[domain] { // an integration resolves the device itself, whatever entities show
		for _, device := range devices {
			if found := e.deviceAccessEntity(device, false); found != "" { // each targeted device itself
				e.gap(reasonDeviceAccess, "", via, name+" on the device of "+found)
			}
		}
	}
	if domain == "script" && !scriptEntityServices[service] {
		ids, name = append(ids, name), "script.turn_on"
	}
	if len(ids) == 0 {
		level, code := classifyTargetless(name, data)
		e.raise(level, tierReason{Code: code, Via: via})
		e.members = append(e.members, tierMember{Action: name, Via: via, Class: level.String()})
		return
	}
	e.prefetchStates(ids)
	for _, id := range ids {
		e.visit(id, name, data, depth, via)
	}
}

// stepData merges data_template over data, as Home Assistant does; a
// payload that is itself a template string cannot be known.
func stepData(step map[string]any) (map[string]any, bool) {
	merged := map[string]any{}
	for _, key := range []string{"data", "data_template"} {
		switch v := step[key].(type) {
		case nil:
		case map[string]any:
			for k, x := range v {
				merged[k] = x
			}
		default:
			return nil, false
		}
	}
	return merged, true
}

// collectTargets gathers literal targets from target, data, and the step
// root; a comma-separated string is a list, as Home Assistant reads it. Any
// templated, non-literal, or "all"/"none" value makes the step unresolved.
func collectTargets(step map[string]any) (map[string][]string, bool) {
	out := map[string][]string{}
	sources := []map[string]any{step}
	switch target := step["target"].(type) {
	case nil:
	case map[string]any:
		sources = append(sources, target)
	default:
		return nil, false // a whole-target template
	}
	for _, key := range []string{"data", "data_template"} {
		if m, isMap := step[key].(map[string]any); isMap {
			sources = append(sources, m)
		}
	}
	for _, source := range sources {
		for _, key := range []string{"entity_id", "area_id", "device_id", "floor_id", "label_id"} {
			var values []string
			switch v := source[key].(type) {
			case nil:
				continue
			case string:
				values = strings.Split(v, ",")
			case []any:
				for _, item := range v {
					text, isString := item.(string)
					if !isString {
						return nil, false
					}
					values = append(values, text)
				}
			default:
				return nil, false
			}
			for _, value := range values {
				value = strings.TrimSpace(value)
				if value == "" || isTemplated(value) || (key == "entity_id" && !entityIDPattern.MatchString(value)) {
					return nil, false // includes all/none: not a literal entity_id
				}
				out[key] = append(out[key], value)
			}
		}
	}
	return out, true
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// integrationScene: an unread scene is bounded by its platform only if that
// platform provides no lock, alarm panel, or access cover. Cover classes come
// from the live state (the registry list has none).
func (e *tierEngine) integrationScene(id, pl string) {
	rows, err := e.fullRegistry()
	if err != nil {
		e.readFailed(id, "", "registry")
		return
	}
	var owned []fullRegistryRow
	var covers []string
	for _, row := range rows {
		if row.Platform == pl {
			owned = append(owned, row)
			if strings.HasPrefix(row.EntityID, "cover.") {
				covers = append(covers, row.EntityID)
			}
		}
	}
	e.prefetchStates(covers)
	for _, row := range owned {
		dc := ""
		if strings.HasPrefix(row.EntityID, "cover.") {
			st, err := e.state(row.EntityID)
			if err != nil {
				e.readFailed(row.EntityID, id, "state")
				return
			}
			dc = st.str("device_class") // absent state or class: fails closed
		}
		if accessCapable(row.EntityID, dc) {
			e.gap(reasonSceneAccess, id, "", pl+" provides "+row.EntityID)
			return
		}
	}
	e.complete = false
	e.raise(tierPreview, tierReason{Code: reasonSceneUnread, EntityID: id, Detail: pl})
}

// sceneMembers classifies a scene's target states (stored or scene.apply).
func (e *tierEngine) sceneMembers(entities any, via string, depth int) {
	if entities == nil {
		return
	}
	members, isMap := entities.(map[string]any)
	if !isMap {
		e.gap(reasonUnresolvedTarget, "", via, "scene entities")
		return
	}
	ids := sortedKeys(members)
	e.prefetchStates(ids)
	for _, id := range ids {
		e.sceneMember(id, members[id], via, depth)
	}
}

// sceneMember maps a stored target state to its service; a group member
// reproduces that state on each leaf, mapped per leaf domain.
func (e *tierEngine) sceneMember(id string, value any, via string, depth int) {
	if !entityIDPattern.MatchString(id) {
		e.gap(reasonUnresolvedTarget, "", via, id)
		return
	}
	st, err := e.state(id)
	if err != nil {
		e.readFailed(id, via, "state")
		return
	}
	domain, _ := splitID(id)
	if leaves := groupMembers(domain, st); leaves != nil {
		state, _ := json.Marshal(value)
		key := "scene-group " + id + " " + string(state) // per requested state
		if e.visited[key] || e.cut(depth+1, id, via) {
			return
		}
		e.visited[key] = true
		// The group is a member too (its own state is promised); its leaves
		// carry the class.
		groupService, groupData := sceneMemberService(id, value)
		e.members = append(e.members, tierMember{EntityID: id, Action: groupService, Via: via, Class: tierOrdinary.String(), data: groupData})
		e.prefetchStates(leaves)
		for _, leaf := range leaves {
			e.sceneMember(leaf, value, id, depth+1)
		}
		return
	}
	service, data := sceneMemberService(id, value)
	e.visit(id, service, data, depth, via)
}
