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
		if key != "views" && keyChanged(before, after, key) {
			rows = append(rows, keyRow("Dashboard › "+key, before, after, key))
		}
	}
	if notList(before["views"]) || notList(after["views"]) { // not a view list: show it as a value
		if keyChanged(before, after, "views") {
			rows = append(rows, keyRow("Dashboard › views", before, after, "views"))
		}
		return capDiffRows(rows)
	}
	bRaw, aRaw := asList(before["views"]), asList(after["views"])
	bViews, aViews := dashboardViews(before), dashboardViews(after)
	pairs := pairViews(bViews, aViews) // after index → before index
	for i := range aViews {
		label := viewLabel(i, aViews[i])
		j, kept := pairs[i]
		switch {
		case !kept:
			rows = append(rows, tableRow(label, "—", viewSummary(aViews[i])))
			rows = append(rows, containerRows(label, aViews[i], false)...)
		case bViews[j] == nil || aViews[i] == nil:
			if !valuesEqual(bRaw[j], aRaw[i]) {
				rows = append(rows, valueRow(label, bRaw[j], aRaw[i]))
			}
		default:
			rows = append(rows, viewDiff(label, bViews[j], aViews[i])...)
		}
	}
	kept := map[int]bool{}
	for _, j := range pairs {
		kept[j] = true
	}
	for j := range bViews {
		if !kept[j] {
			label := viewLabel(j, bViews[j])
			rows = append(rows, tableRow(label, viewSummary(bViews[j]), "—"))
			rows = append(rows, containerRows(label, bViews[j], true)...)
		}
	}
	// The kept views in before order vs after order, both with after labels.
	var keptBefore, keptAfter []string
	for j := range bViews {
		for i, pj := range pairs {
			if pj == j {
				keptBefore = append(keptBefore, viewLabel(i, aViews[i]))
			}
		}
	}
	for i := range aViews {
		if _, ok := pairs[i]; ok {
			keptAfter = append(keptAfter, viewLabel(i, aViews[i]))
		}
	}
	if strings.Join(keptBefore, "\x00") != strings.Join(keptAfter, "\x00") {
		rows = append(rows, tableRow("View order", strings.Join(keptBefore, ", "), strings.Join(keptAfter, ", ")))
	}
	return capDiffRows(rows)
}

// pairViews matches views by path, else title; the rest pair by position
// among themselves, so a renamed view still shows its card changes.
func pairViews(bViews, aViews []map[string]any) map[int]int {
	bKeys, aKeys := viewKeys(bViews), viewKeys(aViews)
	bIndex := map[string]int{}
	for j, key := range bKeys {
		bIndex[key] = j
	}
	pairs, used := map[int]int{}, map[int]bool{}
	for i, key := range aKeys {
		if j, ok := bIndex[key]; ok && !strings.HasPrefix(key, "#") {
			pairs[i], used[j] = j, true
		}
	}
	var bFree []int
	for j := range bViews {
		if !used[j] {
			bFree = append(bFree, j)
		}
	}
	for i := range aViews {
		if _, ok := pairs[i]; !ok && len(bFree) > 0 {
			pairs[i], bFree = bFree[0], bFree[1:]
		}
	}
	return pairs
}

// containerRows lists every card of an added or removed view, row by row.
func containerRows(label string, view map[string]any, removed bool) []string {
	var rows []string
	for _, c := range viewContainers(view) {
		for j, card := range c.items {
			before, after := "—", cardSummary(card)
			if removed {
				before, after = after, before
			}
			rows = append(rows, tableRow(label+" › "+itemLabel(c.label, j), before, after))
		}
	}
	return rows
}

// keyChanged: a key added, removed (null included), or changed.
func keyChanged(before, after map[string]any, key string) bool {
	b, inBefore := before[key]
	a, inAfter := after[key]
	return inBefore != inAfter || !valuesEqual(b, a)
}

func capDiffRows(rows []string) []string {
	if len(rows) > maxDashboardDiffRows {
		more := len(rows) - maxDashboardDiffRows
		rows = append(rows[:maxDashboardDiffRows], tableRow("…", "", plural(more, "more change")))
	}
	return rows
}

// notList: present but not a JSON array (asList would hide its content).
func notList(v any) bool {
	_, isList := v.([]any)
	return v != nil && !isList
}

// keyRow: one key's change; an absent key shows "—", an explicit null "null".
func keyRow(field string, before, after map[string]any, key string) string {
	cell := func(m map[string]any, plain bool) string {
		v, present := m[key]
		if present && v == nil {
			return "null"
		}
		if text, ok := plainCell([]segment{{key: key}, {}}, v); ok && plain { // never the top-level mode rule
			return text
		}
		return formatValue(v)
	}
	b, a := cell(before, false), cell(after, false)
	if pb, pa := cell(before, true), cell(after, true); b != a && pb != pa { // plain words only where they keep the difference (["light.a"] vs "light.a")
		b, a = pb, pa
	}
	b, a = focusDivergence(b, a)
	return tableRow(field, b, a)
}

func valueRow(field string, before, after any) string {
	cell := func(v any) string { // a bare badge is an entity id
		if id, isText := v.(string); isText && entityIDPattern.MatchString(id) {
			return nameOf(id)
		}
		return formatValue(v)
	}
	b, a := formatValue(before), formatValue(after)
	if pb, pa := cell(before), cell(after); b != a && pb != pa {
		b, a = pb, pa
	}
	b, a = focusDivergence(b, a)
	return tableRow(field, b, a)
}

// viewDiff: the view's own keys, then each card container.
func viewDiff(label string, before, after map[string]any) []string {
	var rows []string
	for _, key := range unionKeys(before, after) {
		if (!containerKeys[key] || notList(before[key]) || notList(after[key])) && keyChanged(before, after, key) {
			rows = append(rows, keyRow(label+" › "+key, before, after, key))
		}
	}
	rows = append(rows, listDiff(label, "", asList(before["cards"]), asList(after["cards"]), sameType)...)
	bSections, aSections := asList(before["sections"]), asList(after["sections"])
	for _, pair := range alignItems(bSections, aSections, func(any, any) bool { return true }) {
		switch {
		case pair.b < 0:
			section := fmt.Sprintf("%s › section %d", label, pair.a+1)
			rows = append(rows, tableRow(section, "—", sectionSummary(aSections[pair.a])))
			rows = append(rows, listDiff(section, "", nil, asList(asMap(aSections[pair.a])["cards"]), sameType)...)
		case pair.a < 0:
			section := fmt.Sprintf("%s › section %d", label, pair.b+1)
			rows = append(rows, tableRow(section, sectionSummary(bSections[pair.b]), "—"))
			rows = append(rows, listDiff(section, "", asList(asMap(bSections[pair.b])["cards"]), nil, sameType)...)
		default:
			b, a := asMap(bSections[pair.b]), asMap(aSections[pair.a])
			section := fmt.Sprintf("section %d", pair.a+1)
			if b == nil || a == nil || notList(b["cards"]) || notList(a["cards"]) {
				if !valuesEqual(bSections[pair.b], aSections[pair.a]) {
					rows = append(rows, valueRow(label+" › "+section, bSections[pair.b], aSections[pair.a]))
				}
				continue
			}
			for _, key := range unionKeys(b, a) {
				if key != "cards" && keyChanged(b, a, key) {
					rows = append(rows, keyRow(label+" › "+section+" › "+key, b, a, key))
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
// keys as one row per key (nested card lists recursed).
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
			rows = append(rows, cardDiff(place, before[pair.b], after[pair.a])...)
		}
	}
	return rows
}

// nestedLists are card keys holding cards (stacks, grid), picture-elements
// elements, or heading badges; they are diffed card by card.
var nestedLists = map[string]string{"cards": "", "elements": "elements", "badges": "badges"}

func cardDiff(place string, before, after any) []string {
	b, a := asMap(before), asMap(after)
	if b == nil || a == nil {
		if !valuesEqual(before, after) {
			return []string{valueRow(place, before, after)}
		}
		return nil
	}
	var rows []string
	for _, key := range unionKeys(b, a) {
		container, nested := nestedLists[key]
		_, bList := b[key].([]any)
		_, aList := a[key].([]any)
		switch {
		case !keyChanged(b, a, key):
		case nested && bList && aList:
			rows = append(rows, listDiff(place, container, asList(b[key]), asList(a[key]), sameType)...)
		case key == "card" && asMap(b[key]) != nil && asMap(a[key]) != nil: // conditional, entity-filter
			rows = append(rows, cardDiff(place+" › card ("+cardSummary(a[key])+")", b[key], a[key])...)
		default:
			rows = append(rows, keyRow(place+" › "+key, b, a, key))
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
