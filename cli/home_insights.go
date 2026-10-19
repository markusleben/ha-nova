package main

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ha-nova map --insights: the data of setup step 4 (onboarding 0.27 spec
// §4 step 4, §7.5; contract in docs/work/2026-10-18-onboarding-0.27-spec.md §7.6).
// Read-only and deterministic: fixed rules, no AI. Each insight's action
// names a plan command; nothing here writes.

const (
	offlineAfter      = 24 * time.Hour
	roomsPlanMaxItems = 25 // ponytail: one plan's writes (and its rollback) stay short; the rest wait for the next plan
)

type insightsResult struct {
	Stats      map[string]int `json:"stats"`
	Insights   []homeInsight  `json:"insights"`
	Automation *templateMatch `json:"automation,omitempty"`
}

// homeInsight: one "Worth a look" row. Action, when set, is "rooms"
// (ha-nova plan rooms) or a template id (ha-nova plan template <id>).
type homeInsight struct {
	Kind        string           `json:"kind"`
	EntityID    string           `json:"entity_id,omitempty"`
	DeviceID    string           `json:"device_id,omitempty"`
	Name        string           `json:"name,omitempty"`
	Area        string           `json:"area,omitempty"`
	Value       string           `json:"value,omitempty"`
	Count       int              `json:"count"`
	Suggestions []roomSuggestion `json:"suggestions,omitempty"`
	Action      string           `json:"action,omitempty"`
}

// roomSuggestion: high = the device name holds the room's own name;
// medium = a room word (kitchen, Küche) that exactly one room has. Anything
// less certain is never suggested.
type roomSuggestion struct {
	DeviceID   string `json:"device_id"`
	Name       string `json:"name"`
	AreaID     string `json:"area_id"`
	Area       string `json:"area"`
	Confidence string `json:"confidence"`
}

// insights ranks the fixed rules: low batteries, devices without a room,
// automations that can't run, devices offline > 24 h, automations that
// never ran. phone is the linked phone (nil: none), the battery reminder's
// only target; offered says whether a template's plan would be preview tier
// (one that would need a code is never offered).
func (x *homeIndex) insights(phone *phoneTarget, offered func(templateMatch) bool) insightsResult {
	counts := x.counts()
	auto, autoRows := x.automations()
	r := insightsResult{Insights: []homeInsight{}, Stats: map[string]int{"rooms": counts["areas"], "devices": counts["devices"],
		"lights": x.countDomain("light"), "locks": x.countDomain("lock"), "automations": auto["total"].(int)}}

	_, batteries := x.batteries()
	private := x.privateDevices()
	var low []mapRow
	for _, b := range batteries {
		if b.Low && b.Kind == "device" && !private[x.entityDevice[b.EntityID]] {
			low = append(low, b)
		}
	}
	reminder, canRemind := x.matchTemplate(templateBatteryReminder, phone)
	if len(low) > 0 {
		in := homeInsight{Kind: "battery_low", EntityID: low[0].EntityID, Name: low[0].Name, Area: low[0].Area, Value: low[0].Level, Count: len(low)}
		// Offered only when it covers the battery shown and that one is still
		// above the threshold: a numeric_state trigger fires on crossing it.
		level, err := strconv.ParseFloat(strings.TrimSuffix(low[0].Level, "%"), 64)
		covered := false
		for _, u := range reminder.Uses {
			covered = covered || u.EntityID == low[0].EntityID
		}
		if canRemind && covered && err == nil && level >= reminderBelow && offered(reminder) {
			in.Action = templateBatteryReminder
		}
		r.Insights = append(r.Insights, in)
	}
	if n := counts["devices_without_area"]; n > 0 {
		in := homeInsight{Kind: "no_room", Count: n, Suggestions: x.roomSuggestions()}
		if len(in.Suggestions) > 0 {
			in.Action = "rooms"
		}
		r.Insights = append(r.Insights, in)
	}
	automationInsight := func(kind, issue string) {
		var hit []mapRow
		for _, row := range autoRows {
			if row.Issue == issue {
				hit = append(hit, row)
			}
		}
		if len(hit) > 0 {
			r.Insights = append(r.Insights, homeInsight{Kind: kind, EntityID: hit[0].EntityID, Name: hit[0].Name, Count: len(hit)})
		}
	}
	automationInsight("automation_unavailable", "unavailable")
	if offline := x.offlineDevices(); len(offline) > 0 {
		in := offline[0]
		in.Count = len(offline)
		r.Insights = append(r.Insights, in)
	}
	automationInsight("automation_never_ran", "never_triggered")

	for _, id := range automationTemplates {
		if id == templateBatteryReminder && len(r.Insights) > 0 && r.Insights[0].Action == templateBatteryReminder { // the row offers it
			continue
		}
		if m, ok := x.matchTemplate(id, phone); ok && offered(m) {
			r.Automation = &m
			break
		}
	}
	return r
}

func (x *homeIndex) countDomain(domain string) int {
	n := 0
	for _, id := range x.enabled {
		if st := x.state[id]; st != nil && inDomain(id, domain) && !st.isGroup() {
			n++
		}
	}
	return n
}

func (x *homeIndex) deviceNames() map[string]string {
	names := map[string]string{}
	for _, d := range x.h.devices {
		names[d.ID] = firstNonEmpty(d.NameByUser, d.Name)
	}
	return names
}

// privateDevices: phones (mobile_app) and anything carrying a presence
// entity. Their names say who is where, so no insight or suggestion names
// them (home-map spec §5).
func (x *homeIndex) privateDevices() map[string]bool {
	private := map[string]bool{}
	for _, d := range x.h.devices {
		if x.deviceDomain[d.ID] == "mobile_app" {
			private[d.ID] = true
		}
	}
	for id, device := range x.entityDevice {
		if domain, _ := splitID(id); device != "" && presenceDomains[domain] {
			private[device] = true
		}
	}
	return private
}

// offlineDevices: enabled devices whose every enabled entity with a state
// has been unavailable for at least 24 h (restored orphans don't count);
// Value is the youngest such age, a lower bound. Longest first.
func (x *homeIndex) offlineDevices() []homeInsight {
	type acc struct {
		offline, live bool
		age           time.Duration
	}
	by := map[string]*acc{}
	private := x.privateDevices()
	for _, id := range x.enabled {
		device, st := x.entityDevice[id], x.state[id]
		if device == "" || st == nil || st.Attributes.Restored || !x.deviceOn[device] || x.service[device] || private[device] {
			continue
		}
		a := by[device]
		if a == nil {
			a = &acc{age: 1 << 62}
			by[device] = a
		}
		age, ok := x.age(st)
		if st.State != "unavailable" || !ok || age < offlineAfter {
			a.live = true
			continue
		}
		a.offline, a.age = true, min(a.age, age)
	}
	names := x.deviceNames()
	var rows []homeInsight
	ages := map[string]time.Duration{}
	for device, a := range by {
		if a.offline && !a.live && names[device] != "" {
			ages[device] = a.age
			rows = append(rows, homeInsight{Kind: "device_offline", DeviceID: device, Name: names[device], Area: x.areaName[x.deviceArea[device]], Value: shortAge(a.age)})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if ages[rows[i].DeviceID] != ages[rows[j].DeviceID] {
			return ages[rows[i].DeviceID] > ages[rows[j].DeviceID]
		}
		return rows[i].Name+rows[i].DeviceID < rows[j].Name+rows[j].DeviceID
	})
	return rows
}

// Room words per kind, folded (lower case, no diacritics). roomStems also
// match as the start of a compound word (Wohnzimmerlampe, Küchenlicht).
var (
	roomWords = map[string][]string{
		"kitchen": {"kitchen"}, "bedroom": {"bedroom"}, "living": {"living", "livingroom", "lounge"},
		"bath": {"bath", "bathroom", "bad", "wc", "toilet", "toilette"}, "office": {"office", "study"},
		"hall": {"hall", "hallway", "corridor", "entrance"}, "dining": {"dining"}, "garage": {"garage"},
		"garden": {"garden", "patio", "terrace", "yard"}, "kids": {"kids", "nursery"}, "laundry": {"laundry"},
		"basement": {"basement", "cellar"},
	}
	roomStems = map[string][]string{
		"kitchen": {"kuche"}, "bedroom": {"schlafzimmer"}, "living": {"wohnzimmer"}, "bath": {"badezimmer"},
		"office": {"buro", "arbeitszimmer"}, "hall": {"flur", "diele", "eingang"}, "dining": {"esszimmer"},
		"garden": {"garten", "terrasse"}, "kids": {"kinderzimmer"}, "laundry": {"waschkuche"}, "basement": {"keller"},
	}
)

func foldWords(s string) []string {
	folded := diacriticStrip.Replace(strings.ToLower(stripCombiningMarks(s)))
	return strings.FieldsFunc(folded, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// roomKinds: every room kind a word list names.
func roomKinds(words []string) map[string]bool {
	kinds := map[string]bool{}
	for _, w := range words {
		for kind, list := range roomWords {
			for _, k := range list {
				if w == k {
					kinds[kind] = true
				}
			}
		}
		for kind, list := range roomStems {
			for _, k := range list {
				if strings.HasPrefix(w, k) {
					kinds[kind] = true
				}
			}
		}
	}
	return kinds
}

type foldedArea struct {
	id, name string
	words    []string
	kinds    map[string]bool
}

// suggestRoom: the one existing area a device name points to, or ok=false.
// A room's own name inside the device name wins (high); a name inside a
// longer matching name ("Bedroom" in "Guest Bedroom") gives way to it; two
// separate rooms named, or a room word of another kind beside it, is
// ambiguous and suggests nothing. Otherwise one room kind in the name that
// exactly one area has gives medium.
func suggestRoom(name string, areas []foldedArea) (foldedArea, string, bool) {
	words := foldWords(name)
	kinds := roomKinds(words)
	type span struct {
		area       foldedArea
		start, end int
	}
	var spans []span
	for _, a := range areas {
		for start := range words {
			if len(a.words) > 0 && start+len(a.words) <= len(words) && equalWords(words[start:start+len(a.words)], a.words) {
				spans = append(spans, span{a, start, start + len(a.words)})
			}
		}
	}
	var direct []span
	for i, s := range spans {
		inside := false
		for j, o := range spans {
			if i != j && o.start <= s.start && s.end <= o.end && o.end-o.start > s.end-s.start {
				inside = true
			}
		}
		if !inside {
			direct = append(direct, s)
		}
	}
	for _, d := range direct[min(1, len(direct)):] {
		if d.area.id != direct[0].area.id { // two separate rooms named
			return foldedArea{}, "", false
		}
	}
	if len(direct) > 0 {
		for kind := range kinds {
			if !direct[0].area.kinds[kind] {
				return foldedArea{}, "", false
			}
		}
		return direct[0].area, "high", true
	}
	if len(kinds) != 1 {
		return foldedArea{}, "", false
	}
	var match []foldedArea
	for _, a := range areas {
		for kind := range kinds {
			if a.kinds[kind] {
				match = append(match, a)
			}
		}
	}
	if len(match) == 1 {
		return match[0], "medium", true
	}
	return foldedArea{}, "", false
}

func equalWords(a, b []string) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return len(a) == len(b)
}

// roomSuggestions: enabled devices without any area (own or inherited),
// not a service or a private device, with a confident room; by room, then
// name; at most one plan's worth.
func (x *homeIndex) roomSuggestions() []roomSuggestion {
	var areas []foldedArea
	for _, a := range x.h.areas {
		words := foldWords(a.Name)
		areas = append(areas, foldedArea{id: a.AreaID, name: a.Name, words: words, kinds: roomKinds(words)})
	}
	private := x.privateDevices()
	out := []roomSuggestion{}
	for _, d := range x.h.devices {
		name := firstNonEmpty(d.NameByUser, d.Name)
		if !x.deviceOn[d.ID] || x.service[d.ID] || x.deviceArea[d.ID] != "" || private[d.ID] || name == "" {
			continue
		}
		if area, confidence, ok := suggestRoom(name, areas); ok {
			out = append(out, roomSuggestion{DeviceID: d.ID, Name: name, AreaID: area.id, Area: area.name, Confidence: confidence})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Area != b.Area {
			return a.Area < b.Area
		}
		return a.Name+"\x00"+a.DeviceID < b.Name+"\x00"+b.DeviceID
	})
	return out[:min(len(out), roomsPlanMaxItems)]
}
