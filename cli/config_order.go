package main

import (
	"bytes"
	"encoding/json"
	"maps"
	"sort"
	"strconv"
	"strings"
)

// Home Assistant evaluates these maps top-down (a variable may use the one
// before it; script fields render in order), so their key ORDER is content.
// renderConfigChanges compares maps unordered; this adds the order check.
var orderSemanticKeys = map[string]bool{"variables": true, "trigger_variables": true, "fields": true}

// orderedKeyLists maps the path of every object stored under an
// order-semantic key to its keys in document order. Paths use the canonical
// step form, so `action: {…}` and `actions: [{…}]` are one path.
func orderedKeyLists(raw []byte) map[string]string {
	out := map[string]string{}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var walk func(path string, semantic bool) bool
	walk = func(path string, semantic bool) bool {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		switch tok {
		case json.Delim('{'):
			if stepLists[strings.TrimPrefix(path, "/")] { // one step object is a one-item list (canonicalSteps)
				path += "/0"
			}
			var keys []string
			for dec.More() {
				keyTok, err := dec.Token()
				key, isString := keyTok.(string)
				child := path + "/" + key
				if canon, aliased := diffPluralAlias[key]; aliased && path == "" {
					child = "/" + canon // the same path as HA's saved plural key
				}
				if err != nil || !isString || !walk(child, orderSemanticKeys[key]) {
					return false
				}
				keys = append(keys, key)
			}
			_, _ = dec.Token()
			if semantic {
				out[path] = strings.Join(keys, ", ")
			}
		case json.Delim('['):
			for i := 0; dec.More(); i++ {
				if !walk(path+"/"+strconv.Itoa(i), false) {
					return false
				}
			}
			_, _ = dec.Token()
		}
		return true
	}
	walk("", false)
	return out
}

// orderChanges renders one diff row per order-semantic map whose key order
// differs between before and after (same keys, other order).
func orderChanges(before, after []byte) []string {
	a, b := orderedKeyLists(before), orderedKeyLists(after)
	var rows []string
	for path, order := range b {
		if previous, ok := a[path]; ok && previous != order && sameKeySet(previous, order) {
			rows = append(rows, tableRow(strings.TrimPrefix(path, "/")+" (order)", previous, order))
		}
	}
	sort.Strings(rows)
	return rows
}

func sameKeySet(a, b string) bool {
	x, y := strings.Split(a, ", "), strings.Split(b, ", ")
	sort.Strings(x)
	sort.Strings(y)
	return strings.Join(x, ",") == strings.Join(y, ",")
}

// configChanges is THE comparison of two configs everywhere (plan diff,
// read-back, revert guard, snapshot verify, ha-nova diff): content after
// normalizeConfig plus the order of order-semantic maps.
func configChanges(before, after []byte) ([]string, error) {
	a, err := configObjectFromBytes(before)
	if err != nil {
		return nil, err
	}
	b, err := configObjectFromBytes(after)
	if err != nil {
		return nil, err
	}
	return append(renderConfigChanges(a, b), orderChanges(before, after)...), nil
}

// canonicalSteps compares a top-level step list as Home Assistant saves it.
// One object is a one-item list (HA reads both alike; a singular
// `trigger: {…}` is saved as `triggers: {…}`). HA's validator renames a
// service step's `service` to `action` in the posted config itself, at any
// depth, so a draft written with `service` reads back with `action`
// (automations and scripts, checked against HA 2026.10).
func canonicalSteps(key string, v any) any {
	if blueprint, isMap := v.(map[string]any); isMap && key == "use_blueprint" {
		return blueprintInputSteps(blueprint)
	}
	if !stepLists[key] {
		return v
	}
	if obj, isObj := v.(map[string]any); isObj {
		v = []any{obj}
	}
	if key == "actions" || key == "sequence" {
		return serviceAsAction(v)
	}
	return v
}

// blueprintInputSteps: HA validates the substituted blueprint, which holds
// the same input objects, so steps passed as inputs are renamed too.
func blueprintInputSteps(blueprint map[string]any) map[string]any {
	inputs, isMap := blueprint["input"].(map[string]any)
	if !isMap {
		return blueprint
	}
	out := maps.Clone(blueprint)
	renamed := make(map[string]any, len(inputs))
	for name, value := range inputs { // input names are never renamed
		renamed[name] = serviceAsAction(value)
	}
	out["input"] = renamed
	return out
}

var stepLists = map[string]bool{"triggers": true, "conditions": true, "actions": true, "sequence": true}

// serviceAsAction walks the action tree like the tier walk (never into
// conditions, triggers, payloads, or targets) and renames `service` to
// `action` where only `service` is set. Both sides of a comparison get the
// same rename, so a different service, target, or step still differs.
func serviceAsAction(node any) any {
	switch n := node.(type) {
	case []any:
		out := make([]any, len(n))
		for i, child := range n {
			out[i] = serviceAsAction(child)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, child := range n {
			if !walkSkipKeys[k] {
				child = serviceAsAction(child)
			}
			out[k] = child
		}
		_, both := out["action"]
		if service, legacy := out["service"]; legacy && !both {
			delete(out, "service")
			out["action"] = service
		}
		return out
	}
	return node
}
