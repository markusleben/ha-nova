package main

import "time"

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

// seedArea: the area with most entities (ties by name), if any has one.
func (x *homeIndex) seedArea() (firstSeed, bool) {
	areas := x.areaRows(false)
	byEntities(areas)
	if len(areas) > 0 && areas[0].Entities > 0 {
		return firstSeed{AreaID: areas[0].AreaID, Name: areas[0].Name}, true
	}
	return firstSeed{}, false
}

// pickSeed: the first usable match in area top, else the first anywhere.
func (x *homeIndex) pickSeed(top string, match func(id string, st *mapState) bool) (firstSeed, bool) {
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
	if best == "" {
		return firstSeed{}, false
	}
	return firstSeed{EntityID: best, Name: x.name(best), Area: x.area(best)}, true
}

// seeds: real names for example prompts; persons, trackers, and zones never.
func (x *homeIndex) seeds(out map[string]firstSeed) {
	area, ok := x.seedArea()
	if ok {
		out["area"] = area
	}
	pick := func(key string, match func(id string, st *mapState) bool) {
		if seed, ok := x.pickSeed(area.AreaID, match); ok {
			out[key] = seed
		}
	}
	pick("light", func(id string, st *mapState) bool { return inDomain(id, "light") && !st.isGroup() })
	pick("climate", func(id string, _ *mapState) bool { return inDomain(id, "climate") })
	if _, ok := out["climate"]; !ok {
		pick("climate", func(id string, st *mapState) bool {
			return inDomain(id, "sensor") && st.deviceClass() == "temperature" && x.platform[id] != "mobile_app"
		})
	}
	// Only covers that need no code (a garage door or gate is never an
	// everyday example: closing one runs at once).
	pick("cover", func(id string, st *mapState) bool {
		return inDomain(id, "cover") && !st.isGroup() && nonAccessCoverClasses[st.deviceClass()]
	})
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
