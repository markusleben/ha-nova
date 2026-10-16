package main

import (
	"fmt"
	"strings"
)

// dashboardDiff renders the change between two dashboard configs as table
// rows `| Field | before | after |` at view and card level (dashboard-write
// spec): never a JSON dump of the whole document. "—" in the before column
// means added, in the after column removed.

const maxDashboardDiffRows = 40

func dashboardDiff(before, after map[string]any) []string {
	var rows []string
	for _, key := range unionKeys(before, after) {
		if key != "views" && !valuesEqual(before[key], after[key]) {
			rows = append(rows, valueRow("Dashboard › "+key, before[key], after[key]))
		}
	}
	bViews, aViews := dashboardViews(before), dashboardViews(after)
	bKeys, aKeys := viewKeys(bViews), viewKeys(aViews)
	bIndex := map[string]int{}
	for i, key := range bKeys {
		bIndex[key] = i
	}
	aIndex := map[string]int{}
	var aOrder []string // labels of the views both sides keep, in after order
	for i, key := range aKeys {
		aIndex[key] = i
		label := viewLabel(i, aViews[i])
		j, kept := bIndex[key]
		if !kept {
			rows = append(rows, tableRow(label, "—", viewSummary(aViews[i])))
			continue
		}
		aOrder = append(aOrder, label)
		rows = append(rows, viewDiff(label, bViews[j], aViews[i])...)
	}
	for j, key := range bKeys {
		if _, kept := aIndex[key]; !kept {
			rows = append(rows, tableRow(viewLabel(j, bViews[j]), viewSummary(bViews[j]), "—"))
		}
	}
	// The kept views in before order, relabeled with their after labels.
	var keptBefore []string
	for _, key := range bKeys {
		if i, kept := aIndex[key]; kept {
			keptBefore = append(keptBefore, viewLabel(i, aViews[i]))
		}
	}
	if strings.Join(keptBefore, "\x00") != strings.Join(aOrder, "\x00") {
		rows = append(rows, tableRow("View order", strings.Join(keptBefore, ", "), strings.Join(aOrder, ", ")))
	}
	if len(rows) > maxDashboardDiffRows {
		more := len(rows) - maxDashboardDiffRows
		rows = append(rows[:maxDashboardDiffRows], tableRow("…", "", plural(more, "more change")))
	}
	return rows
}

func valueRow(field string, before, after any) string {
	b, a := formatValue(before), formatValue(after)
	if before == nil {
		b = "—"
	}
	if after == nil {
		a = "—"
	}
	b, a = focusDivergence(b, a)
	return tableRow(field, b, a)
}

// viewDiff: the view's own keys, then each card container.
func viewDiff(label string, before, after map[string]any) []string {
	var rows []string
	for _, key := range unionKeys(before, after) {
		if !containerKeys[key] && !valuesEqual(before[key], after[key]) {
			rows = append(rows, valueRow(label+" › "+key, before[key], after[key]))
		}
	}
	rows = append(rows, listDiff(label, "", asList(before["cards"]), asList(after["cards"]), sameType)...)
	bSections, aSections := asList(before["sections"]), asList(after["sections"])
	for _, pair := range alignItems(bSections, aSections, func(any, any) bool { return true }) {
		switch {
		case pair.b < 0:
			rows = append(rows, tableRow(fmt.Sprintf("%s › section %d", label, pair.a+1), "—", sectionSummary(aSections[pair.a])))
		case pair.a < 0:
			rows = append(rows, tableRow(fmt.Sprintf("%s › section %d", label, pair.b+1), sectionSummary(bSections[pair.b]), "—"))
		default:
			b, a := asMap(bSections[pair.b]), asMap(aSections[pair.a])
			section := fmt.Sprintf("section %d", pair.a+1)
			for _, key := range unionKeys(b, a) {
				if key != "cards" && !valuesEqual(b[key], a[key]) {
					rows = append(rows, valueRow(label+" › "+section+" › "+key, b[key], a[key]))
				}
			}
			rows = append(rows, listDiff(label, section, asList(b["cards"]), asList(a["cards"]), sameType)...)
		}
	}
	return append(rows, listDiff(label, "badges", asList(before["badges"]), asList(after["badges"]), sameType)...)
}

func sectionSummary(section any) string {
	cards := asList(asMap(section)["cards"])
	if len(cards) > 0 && asMap(cards[0])["type"] == "heading" {
		return cardSummary(cards[0]) + ", " + plural(len(cards), "card")
	}
	return plural(len(cards), "card")
}

func sameType(b, a any) bool {
	bm, am := asMap(b), asMap(a)
	if bm == nil || am == nil {
		return bm == nil && am == nil
	}
	return bm["type"] == am["type"]
}

// listDiff: added and removed cards as one row each, a kept card's changed
// keys as one row per key.
func listDiff(view, container string, before, after []any, pairable func(b, a any) bool) []string {
	var rows []string
	for _, pair := range alignItems(before, after, pairable) {
		switch {
		case pair.b < 0:
			rows = append(rows, tableRow(view+" › "+itemLabel(container, pair.a), "—", cardSummary(after[pair.a])))
		case pair.a < 0:
			rows = append(rows, tableRow(view+" › "+itemLabel(container, pair.b), cardSummary(before[pair.b]), "—"))
		default:
			place := view + " › " + itemLabel(container, pair.a) + " (" + cardSummary(after[pair.a]) + ")"
			b, a := asMap(before[pair.b]), asMap(after[pair.a])
			if b == nil || a == nil {
				rows = append(rows, valueRow(place, before[pair.b], after[pair.a]))
				continue
			}
			for _, key := range unionKeys(b, a) {
				if !valuesEqual(b[key], a[key]) {
					rows = append(rows, valueRow(place+" › "+key, b[key], a[key]))
				}
			}
		}
	}
	return rows
}

type alignedPair struct{ b, a int } // -1: absent on that side

// alignItems pairs list items: equal items at the head and tail are kept
// silently; the middle pairs by position while pairable holds, the rest are
// removed and added items. A move reads as a removal plus an addition.
func alignItems(before, after []any, pairable func(b, a any) bool) []alignedPair {
	prefix := 0
	for prefix < len(before) && prefix < len(after) && valuesEqual(before[prefix], after[prefix]) {
		prefix++
	}
	suffix := 0
	for suffix < len(before)-prefix && suffix < len(after)-prefix && valuesEqual(before[len(before)-1-suffix], after[len(after)-1-suffix]) {
		suffix++
	}
	var pairs []alignedPair
	bEnd, aEnd := len(before)-suffix, len(after)-suffix
	i := prefix
	for ; i < bEnd && i < aEnd && pairable(before[i], after[i]); i++ {
		pairs = append(pairs, alignedPair{i, i})
	}
	for j := i; j < bEnd; j++ {
		pairs = append(pairs, alignedPair{j, -1})
	}
	for j := i; j < aEnd; j++ {
		pairs = append(pairs, alignedPair{-1, j})
	}
	return pairs
}
