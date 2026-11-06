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
// and icon count only when nothing in the draft reads this. An inserted step
// that acts on an entity the rest of the config names weighs the whole draft
// too: the next run may read what it set.

// draftSteps returns the draft's indexes to weigh, or nil for every step.
// rawDraft gives the key order of variables and fields, which is content.
func draftSteps(domain string, draft map[string]any, rawDraft, current json.RawMessage) map[int]bool {
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
	wasOrder, nowOrder := orderedKeyLists(current), orderedKeyLists(rawDraft)
	b, a := stepTexts(before, key, wasOrder), stepTexts(after, key, nowOrder)
	if !reflect.DeepEqual(outsideSteps(wasOrder, key), outsideSteps(nowOrder, key)) {
		return nil
	}
	cosmetic := []string{key, "description"}
	if text, _ := json.Marshal(draft); !strings.Contains(string(text), "this.") && !strings.Contains(string(text), "this[") {
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
	// On its next run, the unweighed steps (and its triggers and
	// conditions) may read what an inserted step set.
	read, _ := json.Marshal(now)
	unweighed := string(read) + strings.Join(a[:first], "")
	for _, id := range actionTargets(after[first:]) {
		if strings.Contains(unweighed, id) {
			return nil
		}
	}
	weigh := map[int]bool{}
	for j := first; j < len(a); j++ {
		weigh[j] = true
	}
	return weigh
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
