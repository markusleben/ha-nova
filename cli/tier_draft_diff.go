package main

import (
	"encoding/json"
	"reflect"
	"strings"
)

// What an update changes (issue #1009). An update weighs only the top-level
// steps the draft adds or changes against the live config: an unchanged step
// already runs today, and the write does not change what it reaches. Whatever
// else the write changes can change what every step does, so the whole draft
// is weighed then: a create or restore, triggers, conditions, mode, variables,
// a blueprint. A step that sets or drops a variable changes what the steps
// after it read, and a removed or changed condition, wait, or stop lets them
// run where they did not, so the steps after either are weighed too.

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
	for _, cosmetic := range []string{key, "alias", "description", "icon"} {
		delete(was, cosmetic)
		delete(now, cosmetic)
	}
	if !reflect.DeepEqual(was, now) {
		return nil
	}
	b, a := stepTexts(before), stepTexts(after)
	// lcs[i][j]: the longest run of identical steps in b[i:] and a[j:].
	lcs := make([][]int, len(b)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(a)+1)
	}
	for i := len(b) - 1; i >= 0; i-- {
		for j := len(a) - 1; j >= 0; j-- {
			if b[i] == a[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	weigh := map[int]bool{}
	rest := false // every later step is weighed too
	for i, j := 0, 0; i < len(b) || j < len(a); {
		switch {
		case i < len(b) && j < len(a) && b[i] == a[j]:
			weigh[j] = rest
			i, j = i+1, j+1
		case j < len(a) && (i == len(b) || lcs[i][j+1] >= lcs[i+1][j]):
			weigh[j] = true
			rest = rest || carriesData(a[j])
			j++
		default:
			rest = rest || carriesData(b[i]) || gatesRest(before[i])
			i++
		}
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

// carriesData: a step that sets a variable a later step may read. A payload
// key spelled the same only errs toward weighing more.
func carriesData(text string) bool {
	return strings.Contains(text, `"variables":`) || strings.Contains(text, `"response_variable":`)
}
