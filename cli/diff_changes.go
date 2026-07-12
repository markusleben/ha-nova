package main

import (
	"fmt"
	"sort"
	"strings"
)

// This file owns the change list of `ha-nova diff`: the configChange record
// each walk pass appends, the label-collision safety net, and the final
// sorted/deduplicated row rendering. The tree walk that finds the changes
// lives in diff.go, the cell/label rendering in diff_format.go and
// diff_label.go.

type configChange struct {
	order      int
	path       string
	segs       []segment
	label      string
	beforeCell string
	afterCell  string
}

func renderConfigChanges(before, after map[string]interface{}) []string {
	var changes []configChange
	diffMaps(nil, normalizeConfig(before), normalizeConfig(after), &changes)
	disambiguateLabels(changes)
	sort.SliceStable(changes, func(i, j int) bool {
		if changes[i].order != changes[j].order {
			return changes[i].order < changes[j].order
		}
		return changes[i].path < changes[j].path
	})
	lines := make([]string, 0, len(changes))
	var prev configChange
	for i, c := range changes {
		// The aligned-item pairing and the notification-copy pass can surface
		// the same field change twice; identical entries are adjacent after
		// the stable sort.
		if i > 0 && c.path == prev.path && c.label == prev.label &&
			c.beforeCell == prev.beforeCell && c.afterCell == prev.afterCell {
			continue
		}
		lines = append(lines, tableRow(c.label, c.beforeCell, c.afterCell))
		prev = c
	}
	return lines
}

// disambiguateLabels is the safety net behind the anchor heuristics: no local
// rule can guarantee two DIFFERENT changes never condense to the same label
// (the same entity in two separate or/and groups, for example). When distinct
// paths share a label, those rows fall back to the full label that keeps
// every positional token — verbose, but never ambiguous. Labels are compared
// in their RENDERED form: two labels that differ only beyond the field-cell
// cap truncate to the same visible text, which is exactly the collision the
// user would face.
func disambiguateLabels(changes []configChange) {
	rendered := func(label string) string {
		return escapeCellWithCap(label, maxFieldCellBytes)
	}
	byLabel := make(map[string][]int, len(changes))
	for i, c := range changes {
		byLabel[rendered(c.label)] = append(byLabel[rendered(c.label)], i)
	}
	for _, idxs := range byLabel {
		distinct := false
		for _, i := range idxs[1:] {
			if changes[i].path != changes[idxs[0]].path {
				distinct = true
				break
			}
		}
		if !distinct {
			continue
		}
		for _, i := range idxs {
			changes[i].label = humanizeLabelFull(changes[i].segs)
		}
	}
}

func appendSegment(segs []segment, s segment) []segment {
	out := make([]segment, len(segs)+1)
	copy(out, segs)
	out[len(segs)] = s
	return out
}

func makeChange(segs []segment, before, after interface{}) configChange {
	var bf, af string
	switch {
	case before == nil:
		// The empty Before column IS the "added" statement; no verbal wrapper.
		bf, af = "—", formatValue(after)
	case after == nil:
		bf, af = formatValue(before), "—"
	default:
		bf, af = formatValue(before), formatValue(after)
		if bf == af {
			// Same rendered text but a real change → a type/representation
			// difference (e.g. number 5 vs string "5"). Disambiguate by type so
			// the user never sees a confusing "5 | 5" row; the suffix is added
			// cap-aware so long values cannot swallow it.
			bf = withTypeSuffix(bf, jsonTypeName(before))
			af = withTypeSuffix(af, jsonTypeName(after))
		} else {
			bf, af = focusDivergence(bf, af)
		}
	}
	return configChange{
		order:      topOrder(segs),
		path:       pathString(segs),
		segs:       segs,
		label:      humanizeLabel(segs),
		beforeCell: bf,
		afterCell:  af,
	}
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
			// Zero-padded so lexicographic path sorting equals numeric index
			// order ("[0002]" < "[0010]"); paths are sort keys, never shown.
			fmt.Fprintf(&sb, "[%04d]", s.index)
		} else {
			if sb.Len() > 0 {
				sb.WriteByte('.')
			}
			sb.WriteString(s.key)
		}
	}
	return sb.String()
}

// valuesEqual compares two decoded JSON values for semantic equality. Numbers
// that are mathematically equal but differ in representation (5 vs 5.0, 1e3 vs
// 1000, including INSIDE duration maps like {"seconds":30} vs {"seconds":30.0})
// compare equal: Home Assistant can round-trip a config and change the numeric
// form, and a false "drift" would block a safe revert and train the user to wave
// the guard away. big.Rat keeps large ints (epoch-nanos, ids) exact, so distinct
// values never collapse to a false match. A number vs a string of the same digits
// stays a real change (different types). Maps are key-order-independent.
