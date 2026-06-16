package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file holds the presentation half of `ha-nova diff`: turning a computed
// change (a path plus before/after values) into the stable, human-readable text
// the skill prints verbatim under "## Changes". The comparison and tree-walk
// logic lives in diff.go; splitting the two keeps each file focused and under the
// repo's size guardrail.

// jsonTypeName names the JSON type of a decoded value for diff disambiguation.
func jsonTypeName(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		return "number"
	case []interface{}:
		return "list"
	case map[string]interface{}:
		return "object"
	case nil:
		return "null"
	}
	return "value"
}

var arraySingular = map[string]string{
	"triggers": "Trigger", "conditions": "Condition", "actions": "Action", "sequence": "Step",
}

// humanizeLabel turns a path into a stable, readable label, e.g.
// [mode] -> "Mode", [actions,1,delay] -> "Action 2 (delay)".
func humanizeLabel(segs []segment) string {
	var head string
	var leaf []string
	for i := 0; i < len(segs); {
		s := segs[i]
		if s.isIndex {
			leaf = append(leaf, fmt.Sprintf("#%d", s.index+1))
			i++
			continue
		}
		if i+1 < len(segs) && segs[i+1].isIndex {
			name := arraySingular[s.key]
			if name == "" {
				name = titleFirst(s.key)
			}
			token := fmt.Sprintf("%s %d", name, segs[i+1].index+1)
			if head == "" && len(leaf) == 0 {
				head = token
			} else {
				leaf = append(leaf, strings.ToLower(name)+fmt.Sprintf(" %d", segs[i+1].index+1))
			}
			i += 2
			continue
		}
		if head == "" && len(leaf) == 0 {
			head = titleFirst(s.key)
		} else {
			leaf = append(leaf, s.key)
		}
		i++
	}
	switch {
	case len(leaf) == 0:
		return head
	case head == "":
		return strings.Join(leaf, " › ")
	default:
		return head + " (" + strings.Join(leaf, " › ") + ")"
	}
}

func titleFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func formatValue(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return "—"
	case string:
		if t == "" {
			return `""`
		}
		// Escape control chars (newlines/tabs from a multiline description or
		// template) and cap length so one value can't split a `## Changes` bullet
		// into unprefixed lines — the skill prints this stdout verbatim.
		return truncate(escapeInline(t))
	case bool:
		if t {
			return "true"
		}
		return "false"
	case json.Number:
		return t.String()
	case map[string]interface{}:
		if isDurationMap(t) {
			return formatDuration(t)
		}
		return truncate(compactJSON(t))
	default:
		return truncate(compactJSON(v))
	}
}

var durationUnits = []struct{ key, suffix string }{
	{"days", "d"}, {"hours", "h"}, {"minutes", "min"}, {"seconds", "s"}, {"milliseconds", "ms"},
}

func isDurationMap(m map[string]interface{}) bool {
	if len(m) == 0 {
		return false
	}
	allowed := map[string]bool{}
	for _, u := range durationUnits {
		allowed[u.key] = true
	}
	for k := range m {
		if !allowed[k] {
			return false
		}
	}
	return true
}

func formatDuration(m map[string]interface{}) string {
	var parts []string
	for _, u := range durationUnits {
		if v, ok := m[u.key]; ok {
			parts = append(parts, fmt.Sprintf("%s %s", formatValue(v), u.suffix))
		}
	}
	if len(parts) == 0 {
		return truncate(compactJSON(m))
	}
	return strings.Join(parts, " ")
}

func compactJSON(v interface{}) string {
	out, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(out)
}

func truncate(s string) string {
	const max = 60
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func escapeInline(s string) string {
	return strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`).Replace(s)
}
