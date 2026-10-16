package main

import (
	"fmt"
	"slices"
	"sort"
	"time"
)

// --entity: where-used from search/related (entity, plus its device for
// device triggers/actions) and group membership from states.

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
	NotChecked  []string `json:"not_checked"`
}

// whereUsedNotChecked: what search/related cannot see, so an empty list is
// never read as "used nowhere".
const whereUsedMaxSiblings = 30

var whereUsedNotChecked = []string{"dashboards", "templates", "helper and integration options", "disabled or unloadable automations and scripts", "area, floor, and label targets"}

func (x *homeIndex) whereUsed(s *relaySession, id string, limit int) (whereUsedResult, error) {
	if x.state[id] == nil && !x.registered[id] {
		return whereUsedResult{}, &verbFailure{exit: verbExitNotFound, code: "NOT_FOUND", message: fmt.Sprintf("%s does not exist", id),
			next: `Search with: ha-nova find "<phrase>"`}
	}
	device := x.entityDevice[id]
	var siblings []string
	for other, owner := range x.entityDevice {
		if owner == device && device != "" && other != id {
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
	case len(siblings) > whereUsedMaxSiblings:
		notChecked = append(notChecked, "device-level references (the device has many entities)")
	default:
		requests = append(requests, related("device", device))
		for _, sibling := range siblings {
			requests = append(requests, related("entity", sibling))
		}
	}
	var reads []func()
	for _, request := range requests {
		reads = append(reads, func() { _, _ = s.ws(request) })
	}
	prefetch(reads)
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
	return r, nil
}

// --first-look: the onboarding contract (home-map spec §4).

type firstLookResult struct {
	Home         map[string]int       `json:"home"`
	Observations []firstLookFinding   `json:"observations"`
	Seeds        map[string]firstSeed `json:"seeds"`
}

type firstLookFinding struct {
	Kind     string `json:"kind"`
	EntityID string `json:"entity_id,omitempty"`
	Name     string `json:"name,omitempty"`
	Area     string `json:"area,omitempty"`
	Value    string `json:"value,omitempty"`
	Domain   string `json:"domain,omitempty"`
	State    string `json:"state,omitempty"`
	Count    int    `json:"count"`
}

type firstSeed struct {
	AreaID   string `json:"area_id,omitempty"`
	EntityID string `json:"entity_id,omitempty"`
	Name     string `json:"name"`
	Area     string `json:"area,omitempty"`
}

func (x *homeIndex) firstLook() firstLookResult {
	counts := x.counts()
	auto, autoRows := x.automations()
	rows, problems, _, _ := x.integrations()
	r := firstLookResult{Home: map[string]int{"floors": counts["floors"], "areas": counts["areas"], "devices": counts["devices"],
		"entities": counts["entities"], "automations": auto["total"].(int), "scripts": x.configCounts("script")["total"],
		"scenes": x.configCounts("scene")["total"], "integrations": len(rows)}, Observations: []firstLookFinding{}, Seeds: map[string]firstSeed{}}

	var found []firstLookFinding
	_, batteries := x.batteries()
	var low []mapRow
	for _, b := range batteries {
		if b.Low && b.Kind == "device" {
			low = append(low, b)
		}
	}
	if len(low) > 0 {
		found = append(found, firstLookFinding{Kind: "battery_low", EntityID: low[0].EntityID, Name: low[0].Name, Area: low[0].Area, Value: low[0].Level, Count: len(low)})
	}
	addAutomation := func(kind string) {
		var hit []mapRow
		for _, row := range autoRows {
			if row.Issue == kind {
				hit = append(hit, row)
			}
		}
		if len(hit) > 0 {
			found = append(found, firstLookFinding{Kind: "automation_" + kind, EntityID: hit[0].EntityID, Name: hit[0].Name, Count: len(hit)})
		}
	}
	addAutomation("unavailable")
	if len(problems) > 0 {
		n := 0
		for _, p := range problems {
			n += p.Entries
		}
		found = append(found, firstLookFinding{Kind: "integration_problem", Domain: problems[0].Domain, State: problems[0].State, Count: n})
	}
	addAutomation("never_triggered")
	unavail, unavailRows := x.unavailable()
	if n := unavail["at_least_7d"].(int); n > 0 {
		oldest := unavailRows[0] // longest age first
		found = append(found, firstLookFinding{Kind: "unavailable_long", EntityID: oldest.EntityID, Name: oldest.Name, Area: oldest.Area, Value: oldest.Age, Count: n})
	}
	r.Observations = append(r.Observations, found[:min(len(found), 2)]...)
	x.seeds(r.Seeds)
	return r
}

// seeds: real names for example prompts; persons, trackers, and zones never.
func (x *homeIndex) seeds(out map[string]firstSeed) {
	areas := x.areaRows(false)
	byEntities(areas)
	top := ""
	if len(areas) > 0 && areas[0].Entities > 0 {
		top = areas[0].AreaID
		out["area"] = firstSeed{AreaID: top, Name: areas[0].Name}
	}
	pick := func(key string, match func(id string, st *mapState) bool) {
		best := ""
		for _, id := range x.enabled { // sorted: the first hit is stable
			st := x.state[id]
			if st == nil || st.State == "unavailable" || x.hidden[id] || x.category[id] != "" || !match(id, st) {
				continue
			}
			if x.entityArea[id] == top && top != "" {
				best = id
				break
			}
			if best == "" {
				best = id
			}
		}
		if best != "" {
			out[key] = firstSeed{EntityID: best, Name: x.name(best), Area: x.area(best)}
		}
	}
	pick("light", func(id string, st *mapState) bool { return inDomain(id, "light") && !st.isGroup() })
	pick("climate", func(id string, _ *mapState) bool { return inDomain(id, "climate") })
	if _, ok := out["climate"]; !ok {
		pick("climate", func(id string, st *mapState) bool { return inDomain(id, "sensor") && st.deviceClass() == "temperature" })
	}
	pick("cover", func(id string, st *mapState) bool { return inDomain(id, "cover") && !st.isGroup() })
	var last time.Time
	for _, id := range x.enabled {
		st := x.state[id]
		if !inDomain(id, "automation") || st == nil || st.State != "on" || st.Attributes.LastTriggered == nil {
			continue
		}
		if at, err := time.Parse(time.RFC3339Nano, *st.Attributes.LastTriggered); err == nil && at.After(last) {
			last = at
			out["automation"] = firstSeed{EntityID: id, Name: x.name(id)}
		}
	}
}
