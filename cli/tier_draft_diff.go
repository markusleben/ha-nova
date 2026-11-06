package main

import (
	"encoding/json"
	"reflect"
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
// payload), or when it names an entity, or its device, that the weighed
// steps reach (draftReaches, after their walk).

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
	if text, _ := json.Marshal(withoutPayloads(rest)); strings.Contains(string(text), "{{") || strings.Contains(string(text), "{%") {
		return nil, ""
	}
	unweighed, _ := json.Marshal(rest)
	weigh := map[int]bool{}
	for j := first; j < len(a); j++ {
		weigh[j] = true
	}
	return weigh, string(unweighed)
}

// draftReaches: an entity the walk reached, or its device, is named in the
// unweighed text.
func (e *tierEngine) draftReaches(unweighed string) bool {
	ids := slices.Clone(e.acted)
	for _, m := range e.members {
		ids = append(ids, m.EntityID)
	}
	for _, id := range ids {
		if id != "" && strings.Contains(unweighed, id) || e.reg[id].di != "" && strings.Contains(unweighed, e.reg[id].di) {
			return true
		}
	}
	return false
}

// withoutPayloads drops data payloads: a template there shapes what a step
// sends, never whether or where it runs.
func withoutPayloads(node any) any {
	switch n := node.(type) {
	case []any:
		out := make([]any, len(n))
		for i, child := range n {
			out[i] = withoutPayloads(child)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for key, child := range n {
			if key != "data" && key != "data_template" {
				out[key] = withoutPayloads(child)
			}
		}
		return out
	}
	return node
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
