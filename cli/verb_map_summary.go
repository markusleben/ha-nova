package main

import (
	"sort"
)

const (
	mapExamples        = 3
	mapProblemRows     = 5
	mapAreaRows        = 20
	mapIntegrationRows = 10
	batteryLowBelow    = 20 // health skill's structured detector
)

// Domains whose normal state is unknown (never pressed, never activated).
var statelessDomains = map[string]bool{"tag": true, "button": true, "input_button": true, "event": true, "scene": true, "stt": true, "tts": true,
	"conversation": true, "notify": true, "wake_word": true, "ai_task": true, "image": true, "assist_satellite": true}

// presenceDomains never appear in map output: their names and states are
// who is where (spec §5).
var presenceDomains = map[string]bool{"person": true, "device_tracker": true, "zone": true, "geo_location": true}

var storageHelperDomains = []string{"input_boolean", "input_button", "input_datetime", "input_number", "input_select", "input_text", "counter", "timer", "schedule"}

// integrationProblemStates: a config entry that is not working. not_loaded
// counts only while the entry is not disabled.
var integrationProblemStates = map[string]bool{"setup_error": true, "setup_retry": true, "migration_error": true, "failed_unload": true}

type mapRow struct {
	EntityID string `json:"entity_id"`
	Name     string `json:"name"`
	Area     string `json:"area,omitempty"`
	State    string `json:"state,omitempty"`
	Issue    string `json:"issue,omitempty"`
	Age      string `json:"age,omitempty"`
	Level    string `json:"level,omitempty"`
	Low      bool   `json:"low,omitempty"`
	// Kind (batteries): "device" (a diagnostic battery entity), "phone"
	// (mobile_app), or "level" (a charge level without device-battery
	// evidence: a vehicle, home storage, or unclassified).
	Kind string `json:"kind,omitempty"`
	Via  string `json:"via,omitempty"`
	// Restored: Home Assistant keeps the registry row but no integration
	// provides the entity any more (an orphan cleanup candidate).
	Restored bool `json:"restored,omitempty"`
	// LastTriggered: automations section, the age of the last run ("never").
	LastTriggered string `json:"last_triggered_age,omitempty"`
}

type areaSummary struct {
	AreaID   string         `json:"area_id"`
	Name     string         `json:"name"`
	FloorID  string         `json:"floor_id,omitempty"`
	Devices  int            `json:"devices"`
	Entities int            `json:"entities"`
	Domains  map[string]int `json:"domains,omitempty"`
}

type integrationRow struct {
	Domain   string `json:"domain"`
	Entries  int    `json:"entries"`
	Devices  int    `json:"devices"`
	Entities int    `json:"entities"`
}

type integrationProblem struct {
	Domain  string `json:"domain"`
	State   string `json:"state"`
	Entries int    `json:"entries"`
}

type cappedAreas struct {
	Total int           `json:"total"`
	Shown int           `json:"shown"`
	Rows  []areaSummary `json:"rows"`
}

type mapSummary struct {
	Counts       map[string]int   `json:"counts"`
	Domains      map[string]int   `json:"domains"`
	Floors       []map[string]any `json:"floors,omitempty"`
	Areas        cappedAreas      `json:"areas"`
	Automations  map[string]any   `json:"automations"`
	Scripts      map[string]int   `json:"scripts"`
	Scenes       map[string]int   `json:"scenes"`
	Helpers      map[string]any   `json:"helpers"`
	Integrations map[string]any   `json:"integrations"`
	Unavailable  map[string]any   `json:"unavailable"`
	Batteries    map[string]any   `json:"batteries"`
	More         []string         `json:"more,omitempty"`
}

func (x *homeIndex) counts() map[string]int {
	c := map[string]int{"floors": len(x.h.floors), "areas": len(x.h.areas), "entities": len(x.enabled), "entities_disabled": len(x.disabled)}
	for _, d := range x.h.devices {
		switch {
		case d.DisabledBy != "":
			c["devices_disabled"]++
		case x.deviceArea[d.ID] == "" && !x.service[d.ID]:
			c["devices"]++
			c["devices_without_area"]++
		default:
			c["devices"]++
		}
	}
	for _, id := range x.enabled {
		if !x.registered[id] {
			c["unregistered"]++
		}
		if device := x.entityDevice[id]; device != "" && x.entityArea[id] == "" && !x.service[device] {
			c["device_entities_without_area"]++
		}
	}
	return c
}

func (x *homeIndex) domains() map[string]int {
	out := map[string]int{}
	for _, id := range x.enabled {
		domain, _ := splitID(id)
		out[domain]++
	}
	return out
}

// areaRows: every area, ordered by floor (level, then name), then name.
func (x *homeIndex) areaRows(withDomains bool) []areaSummary {
	rows := map[string]*areaSummary{}
	out := []areaSummary{}
	for _, a := range x.h.areas {
		rows[a.AreaID] = &areaSummary{AreaID: a.AreaID, Name: a.Name, FloorID: a.FloorID}
	}
	for _, d := range x.h.devices {
		if row := rows[x.deviceArea[d.ID]]; row != nil && x.deviceOn[d.ID] {
			row.Devices++
		}
	}
	for _, id := range x.enabled {
		if row := rows[x.entityArea[id]]; row != nil {
			row.Entities++
			if withDomains {
				if row.Domains == nil {
					row.Domains = map[string]int{}
				}
				domain, _ := splitID(id)
				row.Domains[domain]++
			}
		}
	}
	order := x.floorOrder()
	for _, a := range x.h.areas {
		out = append(out, *rows[a.AreaID])
	}
	sort.SliceStable(out, func(i, j int) bool {
		oi, oj := order(out[i].FloorID), order(out[j].FloorID)
		if oi != oj {
			return oi < oj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// floorOrder ranks floors by level, then name; no floor sorts last.
func (x *homeIndex) floorOrder() func(string) int {
	floors := append(x.h.floors[:0:0], x.h.floors...)
	sort.SliceStable(floors, func(i, j int) bool {
		li, lj := levelOf(floors[i].Level), levelOf(floors[j].Level)
		if li != lj {
			return li < lj
		}
		return floors[i].Name < floors[j].Name
	})
	rank := map[string]int{}
	for i, f := range floors {
		rank[f.FloorID] = i
	}
	return func(id string) int {
		if r, ok := rank[id]; ok {
			return r
		}
		return len(floors)
	}
}

func levelOf(level *int) int {
	if level == nil {
		return 1 << 20
	}
	return *level
}

func (x *homeIndex) summary() mapSummary {
	s := mapSummary{Counts: x.counts(), Domains: x.domains()}
	order := x.floorOrder()
	perFloor := map[string]int{}
	for _, a := range x.h.areas {
		perFloor[a.FloorID]++
	}
	for _, f := range x.h.floors {
		s.Floors = append(s.Floors, map[string]any{"floor_id": f.FloorID, "name": f.Name, "areas": perFloor[f.FloorID]})
	}
	sort.SliceStable(s.Floors, func(i, j int) bool {
		return order(s.Floors[i]["floor_id"].(string)) < order(s.Floors[j]["floor_id"].(string))
	})
	s.Floors = s.Floors[:min(len(s.Floors), mapAreaRows)] // counts.floors stays exact
	areas := x.areaRows(false)
	byEntities(areas)
	s.Areas = cappedAreas{Total: len(areas), Rows: areas[:min(len(areas), mapAreaRows)]}
	s.Areas.Shown = len(s.Areas.Rows)
	if s.Areas.Shown < s.Areas.Total {
		s.More = append(s.More, "ha-nova map --section areas")
	}
	auto, autoRows := x.automations()
	auto["examples"] = capRows(autoRows, mapExamples)
	if len(autoRows) > mapExamples {
		s.More = append(s.More, "ha-nova map --section automations")
	}
	s.Automations = auto
	s.Scripts, s.Scenes = x.configCounts("script"), x.configCounts("scene")
	delete(s.Scenes, "unavailable")
	s.Helpers = x.helpers()
	rows, problems, entries, disabled := x.integrations()
	all := len(rows)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Entities != rows[j].Entities {
			return rows[i].Entities > rows[j].Entities
		}
		return rows[i].Devices > rows[j].Devices
	})
	s.Integrations = map[string]any{"entries": entries, "domains": all, "disabled": disabled, "shown": min(all, mapIntegrationRows),
		"rows": rows[:min(all, mapIntegrationRows)], "problems": problems[:min(len(problems), mapProblemRows)], "problems_total": len(problems)}
	if all > mapIntegrationRows || len(problems) > mapProblemRows {
		s.More = append(s.More, "ha-nova map --section integrations")
	}
	unavail, unavailRows := x.unavailable()
	unavail["examples"] = capRows(unavailRows, mapExamples)
	if len(unavailRows) > mapExamples {
		s.More = append(s.More, "ha-nova map --section unavailable")
	}
	s.Unavailable = unavail
	batteries, batteryRows := x.batteries()
	var low []mapRow
	for _, row := range batteryRows {
		if row.Low && row.Kind == "device" {
			low = append(low, row)
		}
	}
	batteries["examples"] = capRows(low, mapExamples)
	if len(low) > mapExamples {
		s.More = append(s.More, "ha-nova map --section batteries")
	}
	s.Batteries = batteries
	return s
}

// byEntities: most entities first, then name (stable for ties).
func byEntities(areas []areaSummary) {
	sort.SliceStable(areas, func(i, j int) bool {
		if areas[i].Entities != areas[j].Entities {
			return areas[i].Entities > areas[j].Entities
		}
		return areas[i].Name < areas[j].Name
	})
}

func capRows(rows []mapRow, n int) []mapRow {
	if rows == nil {
		return []mapRow{}
	}
	return rows[:min(len(rows), n)]
}

// automations: counts plus the problem rows (unavailable first, then
// enabled automations that never ran), each sorted by name.
func (x *homeIndex) automations() (map[string]any, []mapRow) {
	c := map[string]any{"total": 0, "on": 0, "off": 0, "unavailable": 0, "restored": 0, "disabled": 0, "never_triggered": 0}
	var broken, never []mapRow
	for _, id := range x.enabled {
		st := x.state[id]
		if !inDomain(id, "automation") || st == nil {
			continue
		}
		c["total"] = c["total"].(int) + 1
		issue := automationIssue(st)
		if issue == "restored" || issue == "never_triggered" {
			c[issue] = c[issue].(int) + 1
		}
		if issue != "restored" && (st.State == "on" || st.State == "off" || st.State == "unavailable") {
			c[st.State] = c[st.State].(int) + 1
		}
		switch issue {
		case "unavailable":
			broken = append(broken, mapRow{EntityID: id, Name: x.name(id), State: st.State, Issue: issue})
		case "never_triggered":
			never = append(never, mapRow{EntityID: id, Name: x.name(id), State: st.State, Issue: issue})
		}
	}
	for _, id := range x.disabled {
		if inDomain(id, "automation") {
			c["total"] = c["total"].(int) + 1
			c["disabled"] = c["disabled"].(int) + 1
		}
	}
	byName(broken)
	byName(never)
	return c, append(broken, never...)
}

// automationIssue: "unavailable" — Home Assistant could not load the config,
// so it never runs; "restored" — unavailable only because nothing provides
// it any more (its config is gone: an orphan); "never_triggered" — enabled,
// no run recorded.
func automationIssue(st *mapState) string {
	switch {
	case st.State == "unavailable" && st.Attributes.Restored:
		return "restored"
	case st.State == "unavailable":
		return "unavailable"
	case st.State == "on" && st.Attributes.LastTriggered == nil:
		return "never_triggered"
	}
	return ""
}

func byName(rows []mapRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].EntityID < rows[j].EntityID
	})
}

func (x *homeIndex) configCounts(domain string) map[string]int {
	c := map[string]int{"total": 0, "unavailable": 0, "disabled": 0}
	for _, id := range x.enabled {
		if st := x.state[id]; inDomain(id, domain) && st != nil {
			c["total"]++
			if st.State == "unavailable" {
				c["unavailable"]++
			}
		}
	}
	for _, id := range x.disabled {
		if inDomain(id, domain) {
			c["total"]++
			c["disabled"]++
		}
	}
	return c
}

func (x *homeIndex) helpers() map[string]any {
	by := map[string]int{}
	total := 0
	for _, id := range x.enabled {
		domain, _ := splitID(id)
		for _, helper := range storageHelperDomains {
			if domain == helper {
				by[domain]++
				total++
			}
		}
	}
	for _, e := range x.h.helperEntries {
		if e.Source != "ignore" {
			by[e.Domain]++
			total++
		}
	}
	return map[string]any{"total": total, "by_domain": by}
}

// integrations: config entries except ignored discoveries and helpers, per
// domain; devices by their owning entry, entities by config_entry_id.
func (x *homeIndex) integrations() ([]integrationRow, []integrationProblem, int, int) {
	byDomain := map[string]*integrationRow{}
	problems := map[[2]string]int{}
	entries, disabled := 0, 0
	for _, e := range x.h.entries {
		if e.Source == "ignore" || x.helperEntry[e.EntryID] {
			continue
		}
		entries++
		if byDomain[e.Domain] == nil {
			byDomain[e.Domain] = &integrationRow{Domain: e.Domain}
		}
		byDomain[e.Domain].Entries++
		if e.DisabledBy != "" {
			disabled++
		} else if integrationProblemStates[e.State] || e.State == "not_loaded" {
			problems[[2]string{e.Domain, e.State}]++
		}
	}
	for _, d := range x.h.devices {
		if row := byDomain[x.deviceDomain[d.ID]]; row != nil && x.deviceOn[d.ID] {
			row.Devices++
		}
	}
	for _, e := range x.h.entities {
		if row := byDomain[x.entryDomain[e.ConfigEntry]]; row != nil && e.DisabledBy == "" && !x.helperEntry[e.ConfigEntry] {
			row.Entities++
		}
	}
	rows := []integrationRow{}
	for _, row := range byDomain {
		rows = append(rows, *row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Domain < rows[j].Domain })
	out := []integrationProblem{}
	for key, n := range problems {
		out = append(out, integrationProblem{Domain: key[0], State: key[1], Entries: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain+out[i].State < out[j].Domain+out[j].State })
	return rows, out, entries, disabled
}
