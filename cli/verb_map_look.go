package main

import (
	"encoding/json"
	"fmt"
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
// when only the view's or dashboard's own settings do.
type dashboardUse struct {
	Dashboard string `json:"dashboard"`
	URLPath   string `json:"url_path"`
	View      string `json:"view,omitempty"`
	Card      string `json:"card,omitempty"`
}

// whereUsedNotChecked: what search/related cannot see, so an empty list is
// never read as "used nowhere".
const whereUsedMaxSiblings = 30

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
				if st.isGroup() && !seen[st.EntityID] && slices.Contains(members, any(id)) {
					seen[st.EntityID] = true
					rows = append(rows, mapRow{EntityID: st.EntityID, Name: x.name(st.EntityID)})
				}
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].EntityID < rows[j].EntityID })
		return usedList{Total: len(rows), Shown: min(len(rows), limit), Rows: rows[:min(len(rows), limit)]}
	}
	r.Automations, r.Scripts, r.Scenes, r.Groups = list("automation"), list("script"), list("scene"), list("group")
	helpers, err := x.attributeUsers(s, id)
	if err != nil {
		return whereUsedResult{}, err
	}
	r.Helpers = usedList{Total: len(helpers), Shown: min(len(helpers), limit), Rows: helpers[:min(len(helpers), limit)]}
	uses := dashboardUses(boards, id)
	r.Dashboards.Total, r.Dashboards.Shown, r.Dashboards.Rows = len(uses), min(len(uses), limit), uses[:min(len(uses), limit)]
	return r, nil
}

// attributeUsers: other entities whose state attributes name id (a person's
// trackers, a utility meter's or threshold's source); automations, scripts,
// scenes, and groups are listed on their own. /api/states is memoized.
func (x *homeIndex) attributeUsers(s *relaySession, id string) ([]mapRow, error) {
	_, body, err := s.coreGET("/api/states")
	var states []refState
	if err == nil {
		err = json.Unmarshal(body, &states)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot parse /api/states: %w", err)
	}
	match := &refScanner{needles: []string{id}}
	rows := []mapRow{}
	for _, st := range states {
		domain, _ := splitID(st.EntityID)
		if st.EntityID == id || domain == "automation" || domain == "script" || domain == "scene" || x.state[st.EntityID] != nil && x.state[st.EntityID].isGroup() {
			continue
		}
		if len(match.find(st.Attributes)) > 0 {
			rows = append(rows, mapRow{EntityID: st.EntityID, Name: x.name(st.EntityID)})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].EntityID < rows[j].EntityID })
	return rows, nil
}

// dashboardUses: every card, badge, or header card that names id as a whole
// id anywhere inside (entities rows, nested stacks, conditions, templates);
// a nested match names the path down to the innermost card.
func dashboardUses(boards []lovelaceBoard, id string) []dashboardUse {
	match := &refScanner{needles: []string{id}}
	mentions := func(v any) bool { return len(match.find(v)) > 0 }
	var trail func(card any) string
	trail = func(card any) string {
		m := asMap(card)
		for _, child := range append(asList(m["cards"]), m["card"]) {
			if child != nil && mentions(child) {
				return cardSummary(card) + " › " + trail(child)
			}
		}
		return cardSummary(card)
	}
	uses := []dashboardUse{}
	for _, b := range boards {
		cfg := asMap(b.Config)
		if !mentions(cfg) {
			continue
		}
		found := len(uses)
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
			before := len(uses)
			for _, c := range viewContainers(v) {
				for _, item := range c.items {
					if !mentions(item) {
						continue
					}
					card := trail(item)
					if c.label == "badges" || c.label == "header" {
						card = strings.TrimSuffix(c.label, "s") + ": " + card
					}
					uses = append(uses, dashboardUse{Dashboard: b.Title, URLPath: b.URLPath, View: view, Card: card})
				}
			}
			if len(uses) == before { // only the view's own settings (visibility, background)
				uses = append(uses, dashboardUse{Dashboard: b.Title, URLPath: b.URLPath, View: view})
			}
		}
		if len(uses) == found { // only the dashboard's own settings
			uses = append(uses, dashboardUse{Dashboard: b.Title, URLPath: b.URLPath})
		}
	}
	return uses
}
