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

// runDiffCommand renders a deterministic, human-readable change table between
// two Home Assistant config bodies (the "before" and "after" of an update).
// Every output line is one GFM table data row `| Field | before | after |`;
// the skill adds its localized header row above and prints the rows verbatim.
// The output is a stable artifact — like `git diff` — and keeping the
// rendering here, not in the LLM, makes it identical on every run and across
// clients/models. Presentation helpers (labels, cells) live in diff_format.go.
func runDiffCommand(_ runtimePaths, args []string) int {
	fs := flag.NewFlagSet("diff", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var beforePath, afterPath, outPath string
	fs.StringVar(&beforePath, "before", "", "path to the current/before config JSON")
	fs.StringVar(&afterPath, "after", "", "path to the proposed/after config JSON")
	fs.StringVar(&outPath, "out", "", "optional path to write the rendered diff")
	if err := fs.Parse(args); err != nil {
		if helpRequested(err, fs, "ha-nova diff --before <file> --after <file> [--out <file>]") {
			return 0
		}
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
	lines := renderConfigChanges(before, after)
	if strings.TrimSpace(outPath) != "" {
		rendered := ""
		if len(lines) > 0 {
			rendered = strings.Join(lines, "\n") + "\n"
		}
		if err := os.WriteFile(outPath, []byte(rendered), 0o644); err != nil {
			printErr("cannot write diff output: %s", err)
			return 1
		}
		return 0
	}
	for _, line := range lines {
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
	// The input must be exactly one JSON value. json.Decoder stops at the end of the
	// first value, so a truncated/garbage file like `{…}\n{…}` would otherwise
	// silently compare only its first object — making diff or the revert drift check
	// operate on a partial config instead of failing the malformed input.
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("config must be a single JSON object")
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
			// HA accepts a singular `trigger: {…}` as the one-item plural list
			// `triggers: [{…}]`. Wrap a single-object aliased value so a
			// singular-object vs plural-list form compares equal instead of reading
			// as a map-vs-list fake change — which would also block a safe revert
			// through the shared snapshot-verify comparator.
			if obj, isObj := v.(map[string]interface{}); isObj {
				v = []interface{}{obj}
			}
		}
		out[k] = v
	}
	return out
}

type segment struct {
	key     string
	index   int
	isIndex bool
	// anchor is the recognizable summary of the list item this index points at
	// ("condition sensor.x", `"My alias"`). humanizeLabel lets it replace the
	// positional index tokens through or/and wrappers: a layperson recognizes
	// the entity, never the index chain. Key and branch tokens survive, and
	// disambiguateLabels restores the full chain on a label collision.
	anchor string
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
		// Structural change: the count line stays as the group header, followed
		// by aligned per-item lines so the user can see WHAT was added/removed —
		// a bare count can actively misrepresent a change (nesting actions into
		// one if-block reduces the count without removing behavior).
		*changes = append(*changes, configChange{
			order:      topOrder(segs),
			path:       pathString(segs),
			segs:       segs,
			label:      humanizeLabel(segs),
			beforeCell: itemsCell(len(before)),
			afterCell:  itemsCell(len(after)),
		})
		diffAlignedItems(segs, before, after, changes)
		diffNotificationCopyInCommonItems(segs, before, after, changes)
		return
	}
	for i := range before {
		diffValues(appendSegment(segs, segment{index: i, isIndex: true, anchor: itemAnchorFor(segs, before, before[i])}), before[i], after[i], changes)
	}
}

// maxAlignedItemsPerSide bounds the per-item lines rendered for one array's
// length change; the remainder is summarized honestly, never dropped silently.
const maxAlignedItemsPerSide = 8

// diffAlignedItems renders which items changed when an array's length differs:
// items equal at the head and tail (common prefix/suffix) are skipped; the
// middle windows are paired positionally as long as both sides hold the same
// KIND of item (same service call, trigger platform, block type …) — those
// pairs get a normal field-level diff. From the first dissimilar pair on, the
// remainders are rendered as per-item removed/added summary lines. A pure move
// shows as symmetric removed+added lines — deliberate: deterministic and
// readable beats clever matching here.
func diffAlignedItems(segs []segment, before, after []interface{}, changes *[]configChange) {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && valuesEqual(before[prefix], after[prefix]) {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix &&
		valuesEqual(before[len(before)-1-suffix], after[len(after)-1-suffix]) {
		suffix++
	}
	bMid := before[prefix : len(before)-suffix]
	aMid := after[prefix : len(after)-suffix]
	paired := 0
	for paired < len(bMid) && paired < len(aMid) && alignedPairable(bMid[paired], aMid[paired]) {
		diffValues(appendSegment(segs, segment{index: prefix + paired, isIndex: true, anchor: itemAnchorFor(segs, before, bMid[paired])}), bMid[paired], aMid[paired], changes)
		paired++
	}
	renderAlignedSide(segs, bMid[paired:], prefix+paired, true, changes)
	renderAlignedSide(segs, aMid[paired:], prefix+paired, false, changes)
}

// alignedPairable reports whether two items are the same kind of step, so a
// positional field-level diff reads as an edit of one item rather than noise
// between two unrelated items.
func alignedPairable(before, after interface{}) bool {
	kind := configItemKind(before)
	return kind != "" && kind == configItemKind(after)
}

// renderAlignedSide emits one table row per added/removed item; the empty
// side of the row carries the absence marker, so the Before/After columns
// encode add vs remove positionally.
func renderAlignedSide(segs []segment, items []interface{}, offset int, removed bool, changes *[]configChange) {
	rendered := len(items)
	if rendered > maxAlignedItemsPerSide {
		rendered = maxAlignedItemsPerSide
	}
	side := func(itemSegs []segment, summary string) configChange {
		c := configChange{
			order:      topOrder(segs),
			path:       pathString(itemSegs),
			segs:       itemSegs,
			beforeCell: summary,
			afterCell:  "—",
		}
		if !removed {
			c.beforeCell, c.afterCell = c.afterCell, c.beforeCell
		}
		return c
	}
	for i := 0; i < rendered; i++ {
		itemSegs := appendSegment(segs, segment{index: offset + i, isIndex: true})
		c := side(itemSegs, summarizeConfigItem(items[i]))
		c.label = humanizeLabel(itemSegs)
		*changes = append(*changes, c)
	}
	if len(items) > rendered {
		verb := "added"
		if removed {
			verb = "removed"
		}
		// The honest cap stays a table row: a plain line mid-output would
		// terminate the GFM table and split it in two.
		c := side(appendSegment(segs, segment{index: offset + rendered, isIndex: true}), fmt.Sprintf("… and %d more %s", len(items)-rendered, verb))
		c.segs = segs
		c.label = humanizeLabel(segs)
		*changes = append(*changes, c)
	}
}

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
