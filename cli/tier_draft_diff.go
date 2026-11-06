package main

import (
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// What an update changes (issue #1009). An update that only inserts steps
// into a live step list weighs the steps from the first inserted one to the
// end: the steps before it run as they do today, and those after it may read
// what it set or waited for. Any other change weighs the whole draft: a
// create or restore, a removed, changed, or moved step (it can be what made
// a later step safe: a gate, an undo, an error that stops the run), and
// triggers, conditions, mode, variables, or a blueprint, which change what
// every step does, and so does the order of variables or fields. Alias
// and icon count only when nothing in the draft names this.
//
// The next run may read what an inserted step set, so the rest of the
// config (its triggers, conditions, and the steps before the insertion) is
// weighed too when a template there could read any state (outside a data
// payload, whose targets still count), when it targets a device, area,
// floor, or label, or when it names an entity the weighed steps reach, or
// one on its device (draftReaches, after their walk), or when one of
// its steps does something other than a read (actsUnweighed): what reacts
// to it may read what the inserted steps set.

// draftSteps returns the draft's indexes to weigh, or nil for every step,
// and the text of what it leaves unweighed. rawDraft gives the key order of
// variables and fields, which is content.
func draftSteps(domain string, draft map[string]any, rawDraft, current json.RawMessage) (map[int]bool, string) {
	var live map[string]any
	if current == nil || json.Unmarshal(current, &live) != nil || live == nil {
		return nil, ""
	}
	was, now := normalizeConfig(live), normalizeConfig(draft)
	key := "actions"
	if domain == "script" {
		key = "sequence"
	}
	before, beforeList := was[key].([]any)
	after, afterList := now[key].([]any)
	if !beforeList || !afterList {
		return nil, ""
	}
	wasOrder, nowOrder := orderedKeyLists(current), orderedKeyLists(rawDraft)
	b, a := stepTexts(before, key, wasOrder), stepTexts(after, key, nowOrder)
	if !reflect.DeepEqual(outsideSteps(wasOrder, key), outsideSteps(nowOrder, key)) {
		return nil, ""
	}
	cosmetic := []string{key, "description"}
	if text, _ := json.Marshal(draft); !strings.Contains(string(text), "this") {
		cosmetic = append(cosmetic, "alias", "icon")
	}
	for _, k := range cosmetic {
		delete(was, k)
		delete(now, k)
	}
	if !reflect.DeepEqual(was, now) {
		return nil, ""
	}
	first := 0
	for first < len(b) && first < len(a) && a[first] == b[first] {
		first++
	}
	for i, j := first, first; i < len(b); j++ { // every live step is still there, in order
		if j == len(a) {
			return nil, ""
		}
		if a[j] == b[i] {
			i++
		}
	}
	rest := map[string]any{"config": now, "steps": after[:first]}
	text, _ := json.Marshal(withoutPayloads(rest))
	for _, wide := range []string{"{{", "{%", `"area_id"`, `"floor_id"`, `"label_id"`, `"device_id"`} {
		if strings.Contains(string(text), wide) {
			return nil, ""
		}
	}
	unweighed, _ := json.Marshal(rest)
	weigh := map[int]bool{}
	for j := first; j < len(a); j++ {
		weigh[j] = true
	}
	return weigh, strings.ToLower(string(unweighed))
}

// actsPattern: a step that does something (a service call, an event, a
// scene). What reacts to it may read, on the next run, what the weighed
// steps set, and a diff can't see those readers.
var actsPattern = regexp.MustCompile(`"(?:action|service|service_template)":"([^"]*)"|"(?:event|scene)":`)

// actsUnweighed: the unweighed part runs a step other than a read-only
// response service.
func actsUnweighed(unweighed string) bool {
	for _, m := range actsPattern.FindAllStringSubmatch(unweighed, -1) {
		if !readOnlyResponseServices[m[1]] {
			return true
		}
	}
	return false
}

// draftReaches: the walk reached a step with no entity (it may set
// anything), an entity while the unweighed part acts (actsUnweighed), an
// entity named in the unweighed text, or one on the same
// device as an entity it reached.
func (e *tierEngine) draftReaches(unweighed string) bool {
	ids := slices.Clone(e.acted)
	for _, m := range e.members {
		if m.EntityID == "" {
			return true
		}
		ids = append(ids, m.EntityID)
	}
	if len(ids) > 0 && actsUnweighed(unweighed) {
		return true
	}
	devices := map[string]bool{}
	for _, id := range ids {
		if e.reg[id].di != "" {
			devices[e.reg[id].di] = true
		}
	}
	for id, entry := range e.reg {
		if devices[entry.di] {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if id != "" && strings.Contains(unweighed, strings.ToLower(id)) {
			return true
		}
	}
	return false
}

// withoutPayloads drops the data payload of a step, but not its targets: a
// template there shapes what the step sends, never whether or where it runs.
func withoutPayloads(node any) any {
	switch n := node.(type) {
	case []any:
		out := make([]any, len(n))
		for i, child := range n {
			out[i] = withoutPayloads(child)
		}
		return out
	case map[string]any:
		_, action := n["action"]
		_, service := n["service"]
		out := map[string]any{}
		for key, child := range n {
			payload, isMap := child.(map[string]any)
			if (action || service) && (key == "data" || key == "data_template") && isMap {
				child = targetKeys(payload)
			}
			out[key] = withoutPayloads(child)
		}
		return out
	}
	return node
}

func targetKeys(payload map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"entity_id", "device_id", "area_id", "floor_id", "label_id"} {
		if v, ok := payload[key]; ok {
			out[key] = v
		}
	}
	return out
}

// stepTexts: each step as JSON (map keys sorted) plus the key order of the
// variables inside it, so equal texts are equal steps.
func stepTexts(steps []any, key string, order map[string]string) []string {
	out := make([]string, len(steps))
	for i, step := range steps {
		raw, _ := json.Marshal(step)
		prefix := "/" + key + "/" + strconv.Itoa(i) + "/"
		var keys []string
		for path, list := range order {
			if rest, inStep := strings.CutPrefix(path, prefix); inStep {
				keys = append(keys, rest+": "+list)
			}
		}
		slices.Sort(keys)
		out[i] = string(raw) + "\n" + strings.Join(keys, "\n")
	}
	return out
}

// outsideSteps: the key orders that are not inside a step.
func outsideSteps(order map[string]string, key string) map[string]string {
	out := map[string]string{}
	for path, list := range order {
		if !strings.HasPrefix(path, "/"+key+"/") {
			out[path] = list
		}
	}
	return out
}
