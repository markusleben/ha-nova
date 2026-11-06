package main

import (
	"encoding/json"
	"reflect"
	"strings"
)

// What an update changes (issue #1009). An update that only inserts steps
// into a live step list weighs the steps from the first inserted one to the
// end: the steps before it run as they do today, and those after it may read
// what it set or waited for. Any other change weighs the whole draft: a
// create or restore, a removed, changed, or moved step (it can be what made
// a later step safe: a gate, an undo, an error that stops the run), and
// triggers, conditions, mode, variables, or a blueprint, which change what
// every step does. Alias and icon count only when no step reads this.

// draftSteps returns the draft's indexes to weigh, or nil for every step.
func draftSteps(domain string, draft map[string]any, current json.RawMessage) map[int]bool {
	var live map[string]any
	if current == nil || json.Unmarshal(current, &live) != nil || live == nil {
		return nil
	}
	was, now := normalizeConfig(live), normalizeConfig(draft)
	key := "actions"
	if domain == "script" {
		key = "sequence"
	}
	before, beforeList := was[key].([]any)
	after, afterList := now[key].([]any)
	if !beforeList || !afterList {
		return nil
	}
	b, a := stepTexts(before), stepTexts(after)
	cosmetic := []string{key, "description"}
	if text, _ := json.Marshal(draft); !strings.Contains(string(text), "this.") {
		cosmetic = append(cosmetic, "alias", "icon")
	}
	for _, k := range cosmetic {
		delete(was, k)
		delete(now, k)
	}
	if !reflect.DeepEqual(was, now) {
		return nil
	}
	first := 0
	for first < len(b) && first < len(a) && a[first] == b[first] {
		first++
	}
	for i, j := first, first; i < len(b); j++ { // every live step is still there, in order
		if j == len(a) {
			return nil
		}
		if a[j] == b[i] {
			i++
		}
	}
	weigh := map[int]bool{}
	for j := first; j < len(a); j++ {
		weigh[j] = true
	}
	return weigh
}

func stepTexts(steps []any) []string {
	out := make([]string, len(steps))
	for i, step := range steps {
		raw, _ := json.Marshal(step) // map keys sorted: equal steps, equal text
		out[i] = string(raw)
	}
	return out
}
