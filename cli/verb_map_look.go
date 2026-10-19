package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

// --entity: where-used from search/related (entity, plus its device for
// device triggers/actions), group membership and helper sources from
// states, and the dashboard cards that name it.

type usedList struct {
	Total int      `json:"total"`
	Shown int      `json:"shown"`
	Rows  []mapRow `json:"rows"`
}

type whereUsedResult struct {
	EntityID    string   `json:"entity_id"`
	Name        string   `json:"name"`
	Area        string   `json:"area,omitempty"`
	Device      string   `json:"device,omitempty"`
	Integration string   `json:"integration,omitempty"`
	Automations usedList `json:"automations"`
	Scripts     usedList `json:"scripts"`
	Scenes      usedList `json:"scenes"`
	Groups      usedList `json:"groups"`
	Helpers     usedList `json:"helpers"`
	Dashboards  struct {
		Total int            `json:"total"`
		Shown int            `json:"shown"`
		Rows  []dashboardUse `json:"rows"`
	} `json:"dashboards"`
	NotChecked []string `json:"not_checked"`
}

// dashboardUse: one card (or badge) that names the entity; Card is empty
// for the view's own settings, View too for the dashboard's.
type dashboardUse struct {
	Dashboard string `json:"dashboard"`
	URLPath   string `json:"url_path"`
	View      string `json:"view,omitempty"`
	Card      string `json:"card,omitempty"`
}

const whereUsedMaxSiblings = 30

// whereUsedNotChecked: what this read cannot see, so an empty list is never
// read as "used nowhere".

var whereUsedNotChecked = []string{"templates", "helpers that hide their source", "integration options", "disabled or unloadable automations and scripts", "area, floor, and label targets"}

func (x *homeIndex) whereUsed(s *relaySession, id string, limit int) (whereUsedResult, error) {
	if x.state[id] == nil && !x.registered[id] {
		return whereUsedResult{}, &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("%s does not exist", id),
			next: `Search with: ha-nova find "<phrase>"`}
	}
	device := x.entityDevice[id]
	// The device read covers the device's descendants too (child devices and
	// all their entities); everything in that family except this entity is
	// read separately and subtracted.
	family := map[string]bool{}
	var descendants []string
	for queue := []string{device}; device != "" && len(queue) > 0; queue = queue[1:] {
		if !family[queue[0]] {
			family[queue[0]] = true
			if queue[0] != device {
				descendants = append(descendants, queue[0])
			}
			queue = append(queue, x.children[queue[0]]...)
		}
	}
	sort.Strings(descendants)
	var siblings []string
	for other, owner := range x.entityDevice {
		if family[owner] && other != id {
			siblings = append(siblings, other)
		}
	}
	sort.Strings(siblings)
	notChecked := slices.Clone(whereUsedNotChecked)
	related := func(itemType, itemID string) map[string]any {
		return map[string]any{"type": "search/related", "item_type": itemType, "item_id": itemID}
	}
	// search/related on a device also returns what uses the device's OTHER
	// entities; those are subtracted, so a device row is a reference to the
	// device itself (device triggers, conditions, actions). ponytail: an
	// item naming both the device and a sibling drops out too.
	requests := []map[string]any{related("entity", id)}
	switch {
	case device == "":
	case len(siblings)+len(descendants) > whereUsedMaxSiblings:
		notChecked = append(notChecked, "device-level references (the device has many entities)")
	default:
		requests = append(requests, related("device", device))
		for _, child := range descendants {
			requests = append(requests, related("device", child))
		}
		for _, sibling := range siblings {
			requests = append(requests, related("entity", sibling))
		}
	}
	var reads []func()
	for _, request := range requests {
		reads = append(reads, func() { _, _ = s.ws(request) })
	}
	var boards []lovelaceBoard
	var boardGaps []refGap
	var boardsErr error
	reads = append(reads, func() { boards, boardGaps, boardsErr = readLovelacePanels(s) })
	prefetch(reads)
	switch {
	case boardsErr != nil:
		notChecked = append([]string{"dashboards"}, notChecked...)
	case len(boardGaps) > 0:
		notChecked = append([]string{plural(len(boardGaps), "dashboard") + " (could not be read)"}, notChecked...)
	}
	results := make([]map[string][]string, len(requests))
	for i, request := range requests {
		if err := s.wsDecode(request, &results[i], "search/related for "+fmt.Sprint(request["item_id"])); err != nil {
			return whereUsedResult{}, err
		}
	}
	viaDevice := map[string]bool{}
	if len(results) > 1 {
		siblingUse := map[string]bool{}
		for _, rel := range results[2:] {
			for _, item := range append(rel["automation"], rel["script"]...) {
				siblingUse[item] = true
			}
		}
		for _, item := range append(results[1]["automation"], results[1]["script"]...) {
			viaDevice[item] = !siblingUse[item]
		}
	}
	r := whereUsedResult{EntityID: id, Name: x.name(id), Area: x.area(id), Integration: x.platform[id], NotChecked: notChecked}
	for _, d := range x.h.devices {
		if d.ID == device {
			r.Device = firstNonEmpty(d.NameByUser, d.Name)
		}
	}
	list := func(key string) usedList {
		rows, seen := []mapRow{}, map[string]bool{}
		for _, item := range results[0][key] {
			if !seen[item] && item != id {
				seen[item] = true
				rows = append(rows, mapRow{EntityID: item, Name: x.name(item)})
			}
		}
		for item, direct := range viaDevice {
			if direct && !seen[item] && inDomain(item, key) {
				seen[item] = true
				rows = append(rows, mapRow{EntityID: item, Name: x.name(item), Via: "device"})
			}
		}
		if key == "group" { // a group state lists its members
			for _, st := range x.h.states {
				members, _ := st.Attributes.Members.([]any)
				if x.realGroup(st.EntityID) && !seen[st.EntityID] && slices.Contains(members, any(id)) {
					seen[st.EntityID] = true
					rows = append(rows, mapRow{EntityID: st.EntityID, Name: x.name(st.EntityID)})
				}
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].EntityID < rows[j].EntityID })
		return usedList{Total: len(rows), Shown: min(len(rows), limit), Rows: rows[:min(len(rows), limit)]}
	}
	r.Automations, r.Scripts, r.Scenes, r.Groups = list("automation"), list("script"), list("scene"), list("group")
	helpers, hidden, err := x.attributeUsers(s, id)
	if err != nil {
		return whereUsedResult{}, err
	}
	if hidden > 0 {
		r.NotChecked = append(r.NotChecked, plural(hidden, "unavailable helper")+" (source hidden)")
	}
	r.Helpers = usedList{Total: len(helpers), Shown: min(len(helpers), limit), Rows: helpers[:min(len(helpers), limit)]}
	uses := dashboardUses(boards, id)
	r.Dashboards.Total, r.Dashboards.Shown, r.Dashboards.Rows = len(uses), min(len(uses), limit), uses[:min(len(uses), limit)]
	return r, nil
}

// realGroup: a group entity, not a source helper (min/max) that lists its
// sources the same way.
func (x *homeIndex) realGroup(id string) bool {
	st, platform := x.state[id], x.platform[id]
	return st != nil && st.isGroup() && (platform == "group" || !sourceHelperPlatforms[platform])
}

// attributeUsers: other entities whose state attributes name id (a utility
// meter's, min/max's, or threshold's source); automations, scripts, scenes,
// and groups are listed on their own; presence never appears in map output.
// hidden counts source helpers that are unavailable: Home Assistant drops
// their attributes. /api/states is memoized.
func (x *homeIndex) attributeUsers(s *relaySession, id string) ([]mapRow, int, error) {
	_, body, err := s.coreGET("/api/states")
	var states []refState
	if err == nil {
		err = json.Unmarshal(body, &states)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("cannot parse /api/states: %w", err)
	}
	match := &refScanner{needles: []string{id}}
	rows, hidden := []mapRow{}, 0
	for _, st := range states {
		domain, _ := splitID(st.EntityID)
		if st.EntityID == id || domain == "automation" || domain == "script" || domain == "scene" || presenceDomains[domain] || x.realGroup(st.EntityID) {
			continue
		}
		if st.State == "unavailable" && sourceHelperPlatforms[x.platform[st.EntityID]] {
			hidden++
			continue
		}
		if len(match.find(st.Attributes)) > 0 {
			rows = append(rows, mapRow{EntityID: st.EntityID, Name: x.name(st.EntityID)})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].EntityID < rows[j].EntityID })
	return rows, hidden, nil
}

// dashboardUses: every card, badge, or header card that names id as a whole
// id anywhere inside (entities rows, nested stacks, conditions, templates);
// each nested match names the path down to its innermost card.
func dashboardUses(boards []lovelaceBoard, id string) []dashboardUse {
	match := &refScanner{needles: []string{id}}
	mentions := func(v any) bool { return len(match.find(v)) > 0 }
	summary := func(card any) string { // presence never appears in map output
		if entity, _ := asMap(card)["entity"].(string); presenceDomains[strings.SplitN(entity, ".", 2)[0]] {
			kind, _ := asMap(card)["type"].(string)
			return kind
		}
		return cardSummary(card)
	}
	var trails func(card any) []string
	trails = func(card any) []string {
		m, out := asMap(card), []string{}
		if m == nil || mentions(without(m, "cards", "card")) { // the card's own settings (entities, conditions, templates)
			out = append(out, summary(card))
		}
		for _, child := range append(asList(m["cards"]), m["card"]) {
			if child != nil && mentions(child) {
				for _, t := range trails(child) {
					out = append(out, summary(card)+" › "+t)
				}
			}
		}
		return out
	}
	uses := []dashboardUse{}
	for _, b := range boards {
		cfg := asMap(b.Config)
		if !mentions(cfg) {
			continue
		}
		if mentions(without(cfg, "views")) { // the dashboard's own settings
			uses = append(uses, dashboardUse{Dashboard: b.Title, URLPath: b.URLPath})
		}
		for i, v := range dashboardViews(cfg) {
			if !mentions(v) {
				continue
			}
			view, _ := v["title"].(string)
			if path, _ := v["path"].(string); view == "" && path != "" {
				view = "/" + path
			} else if view == "" {
				view = fmt.Sprintf("view %d", i+1)
			}
			own := []any{without(v, "cards", "sections", "badges", "header"), without(asMap(v["header"]), "card")}
			for _, section := range asList(v["sections"]) {
				own = append(own, without(asMap(section), "cards"))
			}
			if mentions(own) { // the view's own settings (visibility, a section's visibility, background)
				uses = append(uses, dashboardUse{Dashboard: b.Title, URLPath: b.URLPath, View: view})
			}
			for _, c := range viewContainers(v) {
				for _, item := range c.items {
					if !mentions(item) {
						continue
					}
					for _, card := range trails(item) {
						if c.label == "badges" || c.label == "header" {
							card = strings.TrimSuffix(c.label, "s") + ": " + card
						}
						uses = append(uses, dashboardUse{Dashboard: b.Title, URLPath: b.URLPath, View: view, Card: card})
					}
				}
			}
		}
	}
	return uses
}

// without copies m minus keys.
func without(m map[string]any, keys ...string) map[string]any {
	out := maps.Clone(m)
	for _, k := range keys {
		delete(out, k)
	}
	return out
}
