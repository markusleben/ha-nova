package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"os"
	"sort"
	"strings"
)

// runDiffCommand renders a deterministic, human-readable change list between two
// Home Assistant config bodies (the "before" and "after" of an update). The
// output is a stable artifact — like `git diff` — that the skill prints verbatim
// under a localized "## Changes" heading. Keeping the rendering here, not in the
// LLM, makes the diff identical on every run and across clients/models.
func runDiffCommand(_ runtimePaths, args []string) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var beforePath, afterPath string
	fs.StringVar(&beforePath, "before", "", "path to the current/before config JSON")
	fs.StringVar(&afterPath, "after", "", "path to the proposed/after config JSON")
	if err := fs.Parse(args); err != nil {
		printErr("%s", err)
		return 1
	}
	if strings.TrimSpace(beforePath) == "" || strings.TrimSpace(afterPath) == "" {
		printErr("--before <file> and --after <file> are required")
		return 1
	}
	before, err := readConfigObject(beforePath)
	if err != nil {
		printErr("cannot read before config: %s", err)
		return 1
	}
	after, err := readConfigObject(afterPath)
	if err != nil {
		printErr("cannot read after config: %s", err)
		return 1
	}
	for _, line := range renderConfigChanges(before, after) {
		fmt.Fprintln(os.Stdout, line)
	}
	return 0
}

func readConfigObject(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return configObjectFromBytes(data)
}

// configObjectFromBytes decodes a JSON object body into a map, keeping numeric
// tokens exact. Shared by `ha-nova diff` and the snapshot drift check so both
// compare config content the same way.
func configObjectFromBytes(data []byte) (map[string]interface{}, error) {
	v, err := decodeJSONNumber(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("config is not a JSON object")
	}
	return m, nil
}

func decodeJSONNumber(data []byte) (interface{}, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v interface{}
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// diffIgnoredKeys are bookkeeping fields, not user-meaningful config — never
// shown as a change.
var diffIgnoredKeys = map[string]bool{
	"id": true, "unique_id": true, "created_at": true, "modified_at": true,
	"editor": true, "last_triggered": true, "last_changed": true, "last_updated": true,
}

// diffPluralAlias canonicalizes the singular trigger/condition/action aliases to
// HA's stored plural form, so an alias difference never reads as a real change.
var diffPluralAlias = map[string]string{
	"trigger": "triggers", "condition": "conditions", "action": "actions",
}

func normalizeConfig(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if diffIgnoredKeys[k] {
			continue
		}
		if canon, ok := diffPluralAlias[k]; ok {
			k = canon
		}
		out[k] = v
	}
	return out
}

type configChange struct {
	order int
	path  string
	text  string
}

func renderConfigChanges(before, after map[string]interface{}) []string {
	var changes []configChange
	diffMaps(nil, normalizeConfig(before), normalizeConfig(after), &changes)
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].order != changes[j].order {
			return changes[i].order < changes[j].order
		}
		return changes[i].path < changes[j].path
	})
	lines := make([]string, 0, len(changes))
	for _, c := range changes {
		lines = append(lines, "- "+c.text)
	}
	return lines
}

type segment struct {
	key     string
	index   int
	isIndex bool
}

func diffValues(segs []segment, before, after interface{}, changes *[]configChange) {
	bMap, bIsMap := before.(map[string]interface{})
	aMap, aIsMap := after.(map[string]interface{})
	// Duration-shaped maps are presented as single scalar values (e.g. "5 min").
	if bIsMap && aIsMap && !isDurationMap(bMap) && !isDurationMap(aMap) {
		diffMaps(segs, bMap, aMap, changes)
		return
	}
	bArr, bIsArr := before.([]interface{})
	aArr, aIsArr := after.([]interface{})
	if bIsArr && aIsArr {
		diffArrays(segs, bArr, aArr, changes)
		return
	}
	if !valuesEqual(before, after) {
		*changes = append(*changes, makeChange(segs, before, after))
	}
}

func diffMaps(segs []segment, before, after map[string]interface{}, changes *[]configChange) {
	for _, k := range unionKeys(before, after) {
		bv, bok := before[k]
		av, aok := after[k]
		child := appendSegment(segs, segment{key: k})
		switch {
		case bok && !aok:
			// A key present on one side only is a real change ONLY if its value is
			// non-empty. HA stores empty optional fields (description: "",
			// conditions: []) that a filtered read-back / draft may omit; treating
			// missing-vs-empty as a change would cry drift on nearly every real
			// config and block a safe revert.
			if !isEmptyValue(bv) {
				*changes = append(*changes, makeChange(child, bv, nil))
			}
		case !bok && aok:
			if !isEmptyValue(av) {
				*changes = append(*changes, makeChange(child, nil, av))
			}
		default:
			diffValues(child, bv, av, changes)
		}
	}
}

// isEmptyValue reports whether v is "absent-equivalent": a missing key and a key
// set to null, "", [], or {} carry the same meaning, so HA adding/omitting an
// empty optional field is neither a rendered change nor drift.
func isEmptyValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []interface{}:
		return len(t) == 0
	case map[string]interface{}:
		return len(t) == 0
	}
	return false
}

func diffArrays(segs []segment, before, after []interface{}, changes *[]configChange) {
	if len(before) != len(after) {
		// Structural change: report the count, do not flood with per-element diffs.
		*changes = append(*changes, configChange{
			order: topOrder(segs),
			path:  pathString(segs),
			text:  fmt.Sprintf("%s: %d → %d items", humanizeLabel(segs), len(before), len(after)),
		})
		return
	}
	for i := range before {
		diffValues(appendSegment(segs, segment{index: i, isIndex: true}), before[i], after[i], changes)
	}
}

func appendSegment(segs []segment, s segment) []segment {
	out := make([]segment, len(segs)+1)
	copy(out, segs)
	out[len(segs)] = s
	return out
}

func makeChange(segs []segment, before, after interface{}) configChange {
	label := humanizeLabel(segs)
	var text string
	switch {
	case before == nil:
		text = fmt.Sprintf("%s: added (%s)", label, formatValue(after))
	case after == nil:
		text = fmt.Sprintf("%s: removed (was %s)", label, formatValue(before))
	default:
		bf, af := formatValue(before), formatValue(after)
		if bf == af {
			// Same rendered text but a real change → a type/representation
			// difference (e.g. number 5 vs string "5"). Disambiguate by type so
			// the user never sees a confusing "5 → 5".
			bf = fmt.Sprintf("%s (%s)", bf, jsonTypeName(before))
			af = fmt.Sprintf("%s (%s)", af, jsonTypeName(after))
		}
		text = fmt.Sprintf("%s: %s → %s", label, bf, af)
	}
	return configChange{order: topOrder(segs), path: pathString(segs), text: text}
}

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

// topKeyOrder pins the order of the well-known top-level fields so the change
// list always reads in the same sequence; everything else sorts after, by path.
var topKeyOrder = map[string]int{
	"alias": 0, "description": 1, "mode": 2, "enabled": 3,
	"triggers": 4, "conditions": 5, "actions": 6, "sequence": 7,
}

func topOrder(segs []segment) int {
	if len(segs) > 0 && !segs[0].isIndex {
		if o, ok := topKeyOrder[segs[0].key]; ok {
			return o
		}
	}
	return 100
}

func pathString(segs []segment) string {
	var sb strings.Builder
	for _, s := range segs {
		if s.isIndex {
			fmt.Fprintf(&sb, "[%d]", s.index)
		} else {
			if sb.Len() > 0 {
				sb.WriteByte('.')
			}
			sb.WriteString(s.key)
		}
	}
	return sb.String()
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

// valuesEqual compares two decoded JSON values for semantic equality. Numbers
// that are mathematically equal but differ in representation (5 vs 5.0, 1e3 vs
// 1000, including INSIDE duration maps like {"seconds":30} vs {"seconds":30.0})
// compare equal: Home Assistant can round-trip a config and change the numeric
// form, and a false "drift" would block a safe revert and train the user to wave
// the guard away. big.Rat keeps large ints (epoch-nanos, ids) exact, so distinct
// values never collapse to a false match. A number vs a string of the same digits
// stays a real change (different types). Maps are key-order-independent.
func valuesEqual(a, b interface{}) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(json.Number)
		return ok && numbersEqual(av, bv)
	case map[string]interface{}:
		bv, ok := b.(map[string]interface{})
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			bvv, exists := bv[k]
			if !exists || !valuesEqual(v, bvv) {
				return false
			}
		}
		return true
	case []interface{}:
		bv, ok := b.([]interface{})
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !valuesEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		ab, errA := json.Marshal(a)
		bb, errB := json.Marshal(b)
		return errA == nil && errB == nil && bytes.Equal(ab, bb)
	}
}

func numbersEqual(a, b json.Number) bool {
	ar, aok := new(big.Rat).SetString(a.String())
	br, bok := new(big.Rat).SetString(b.String())
	if !aok || !bok {
		return a.String() == b.String()
	}
	return ar.Cmp(br) == 0
}

func unionKeys(a, b map[string]interface{}) []string {
	seen := map[string]bool{}
	var keys []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
