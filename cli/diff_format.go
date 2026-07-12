package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// This file holds the presentation half of `ha-nova diff`: turning a computed
// change (a path plus before/after values) into the stable GFM table data rows
// (`| Field | before | after |`) the skill prints verbatim under its localized
// header row. The comparison and tree-walk logic lives in diff.go; splitting
// the two keeps each file focused and under the repo's size guardrail.

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
		// Raw on purpose: truncation and escaping happen exactly once, in
		// escapeCell, when the value becomes a table cell.
		return t
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
		return compactJSON(t)
	default:
		return compactJSON(v)
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
		return compactJSON(m)
	}
	return strings.Join(parts, " ")
}

// summarizeConfigItem renders one trigger/condition/action/step as a short,
// recognizable phrase for the aligned per-item diff lines. Preference order:
// the user's own alias, then the semantic head of the item (service call,
// trigger platform, condition type, delay/wait, block type with nested
// counts), then the compact-JSON fallback.
func summarizeConfigItem(v interface{}) string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return formatValue(v)
	}
	if alias, _ := m["alias"].(string); alias != "" {
		return fmt.Sprintf("%q", alias)
	}
	if action, _ := m["action"].(string); action != "" {
		return "action " + action
	}
	if service, _ := m["service"].(string); service != "" {
		return "action " + service
	}
	if platform, _ := m["platform"].(string); platform != "" {
		return "trigger " + platform
	}
	if trigger, _ := m["trigger"].(string); trigger != "" {
		return "trigger " + trigger
	}
	if condition, _ := m["condition"].(string); condition != "" {
		return "condition " + condition
	}
	if delay, ok := m["delay"]; ok {
		return "delay " + formatValue(delay)
	}
	if _, ok := m["wait_template"]; ok {
		return "wait_template"
	}
	if _, ok := m["wait_for_trigger"]; ok {
		return "wait_for_trigger"
	}
	if blockSummary := summarizeBlockItem(m); blockSummary != "" {
		return blockSummary
	}
	return compactJSON(m)
}

// configItemKind identifies what KIND of step an item is (which service call,
// which trigger platform, which block type) for the aligned-pairing decision.
// Empty string = unrecognized shape, never pairable.
func configItemKind(v interface{}) string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return ""
	}
	if s, _ := m["action"].(string); s != "" {
		return "action:" + s
	}
	if s, _ := m["service"].(string); s != "" {
		return "action:" + s
	}
	if s, _ := m["platform"].(string); s != "" {
		return "trigger:" + s
	}
	if s, _ := m["trigger"].(string); s != "" {
		return "trigger:" + s
	}
	if s, _ := m["condition"].(string); s != "" {
		return "condition:" + s
	}
	for _, key := range []string{"delay", "wait_template", "wait_for_trigger", "if", "choose", "repeat", "parallel", "stop"} {
		if _, ok := m[key]; ok {
			return key
		}
	}
	return ""
}

// summarizeBlockItem names structural blocks with their nested sizes so
// "3 actions wrapped into one if-block" is visible as exactly that.
func summarizeBlockItem(m map[string]interface{}) string {
	if _, ok := m["if"]; ok {
		summary := fmt.Sprintf("if/then block (%s", countItems(m["then"], "action"))
		if _, hasElse := m["else"]; hasElse {
			summary += ", " + countItems(m["else"], "else action")
		}
		return summary + ")"
	}
	if _, ok := m["choose"]; ok {
		return fmt.Sprintf("choose block (%s)", countItems(m["choose"], "option"))
	}
	if _, ok := m["repeat"]; ok {
		return "repeat block"
	}
	if _, ok := m["parallel"]; ok {
		return fmt.Sprintf("parallel block (%s)", countItems(m["parallel"], "branch"))
	}
	if _, ok := m["stop"]; ok {
		return "stop"
	}
	return ""
}

func countItems(v interface{}, noun string) string {
	items, ok := v.([]interface{})
	if !ok {
		return "1 " + noun
	}
	if len(items) == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", len(items), noun)
}

func compactJSON(v interface{}) string {
	out, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(out)
}

// maxCellBytes caps one table cell's raw content. 80 keeps ordinary
// one-sentence notification copy whole while two value columns still fit a
// terminal line; longer values truncate with `…` — the full text is always one
// `show yaml` away, and write-safety obliges the summary to name what a
// truncated value changed.
const maxCellBytes = 80

// tableRow renders one GFM table data row. Every cell passes through
// escapeCell exactly once, here — callers hand over raw content and never
// pre-escape.
func tableRow(field, before, after string) string {
	return "| " + escapeCell(field) + " | " + escapeCell(before) + " | " + escapeCell(after) + " |"
}

// escapeCell makes one cell safe inside a table row the skill prints
// verbatim: rune-safe truncation to maxCellBytes, control-character escaping
// (a raw newline would break the whole table, not just one bullet), and pipe
// escaping so a value — Jinja templates love `|` — can never add a column.
// Truncation runs first so an escape sequence is never cut in half; escapes
// may push the final cell slightly past the cap, which bounds content, not
// framing.
func escapeCell(s string) string {
	truncated := false
	if len(s) > maxCellBytes {
		cut := maxCellBytes
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
		truncated = true
	}
	s = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`, "|", `\|`).Replace(s)
	if truncated {
		s += "…"
	}
	return s
}

// itemsCell renders an array length as a self-labeling cell ("1 item",
// "3 items") so a count row can never be misread as a numeric value change.
func itemsCell(n int) string {
	if n == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", n)
}

// divergenceContextBytes is how much shared text stays visible before the
// first difference when focusDivergence trims a long common prefix.
const divergenceContextBytes = 20

// withTypeSuffix appends the disambiguating type label so it survives the
// cell cap: for identically-rendered values the type IS the only visible
// change, so the value is pre-trimmed (rune-safe, `…`) to leave room for the
// suffix inside maxCellBytes — escapeCell must never cut the label off.
func withTypeSuffix(v, typeName string) string {
	suffix := " (" + typeName + ")"
	limit := maxCellBytes - len(suffix) - len("…")
	if len(v)+len(suffix) > maxCellBytes {
		cut := limit
		for cut > 0 && !utf8.RuneStart(v[cut]) {
			cut--
		}
		v = v[:cut] + "…"
	}
	return v + suffix
}

// focusDivergence keeps the FIRST difference of a changed value pair visible.
// Without it, two long values sharing their first maxCellBytes bytes (a
// notification message edited near the end) would truncate into two identical
// `<prefix>…` cells — and write-safety requires changed copy to be visible in
// the preview. When either side would truncate, the shared prefix is cut to
// divergenceContextBytes of context (rune-safe, marked with a leading `…`);
// escapeCell still caps the tail afterwards.
func focusDivergence(before, after string) (string, string) {
	if len(before) <= maxCellBytes && len(after) <= maxCellBytes {
		return before, after
	}
	p := 0
	for p < len(before) && p < len(after) && before[p] == after[p] {
		p++
	}
	if p <= divergenceContextBytes {
		return before, after
	}
	start := p - divergenceContextBytes
	for start > 0 && !utf8.RuneStart(before[start]) {
		start--
	}
	return "…" + before[start:], "…" + after[start:]
}
