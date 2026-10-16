package main

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// section returns one full list, capped at limit with exact totals.
func (x *homeIndex) section(name string, limit int) map[string]any {
	out := map[string]any{"section": name}
	var total int
	switch name {
	case "areas":
		rows := x.areaRows(true)
		total, out["rows"] = len(rows), rows[:min(len(rows), limit)]
		out["floors"] = x.h.floors
	case "integrations":
		rows, problems, _, _ := x.integrations()
		total, out["rows"], out["problems"] = len(rows), rows[:min(len(rows), limit)], problems[:min(len(problems), limit)]
	case "automations":
		rows := x.automationRows()
		total, out["rows"] = len(rows), rows[:min(len(rows), limit)]
	case "unavailable":
		counts, rows := x.unavailable()
		total, out["rows"], out["counts"] = len(rows), rows[:min(len(rows), limit)], counts
	case "batteries":
		counts, rows := x.batteries()
		total, out["rows"], out["counts"] = len(rows), rows[:min(len(rows), limit)], counts
	}
	out["total"], out["shown"] = total, min(total, limit)
	switch {
	case total > limit && limit < 200:
		out["more"] = "ha-nova map --section " + name + " --limit 200"
	case total > limit: // the verb's ceiling: say so, never a loop
		out["more"] = "first 200 of the total; narrow with ha-nova find --domain/--area/--state"
	}
	return out
}

// automationRows: problems first (unavailable, never ran), then every other
// automation by name; registry-disabled ones last as state "disabled".
func (x *homeIndex) automationRows() []mapRow {
	var broken, never, rest, disabled []mapRow
	for _, id := range x.enabled {
		st := x.state[id]
		if !inDomain(id, "automation") || st == nil {
			continue
		}
		row := mapRow{EntityID: id, Name: x.name(id), State: st.State, LastTriggered: "never"}
		if st.Attributes.LastTriggered != nil {
			if at, err := time.Parse(time.RFC3339Nano, *st.Attributes.LastTriggered); err == nil {
				row.LastTriggered = shortAge(x.now.Sub(at))
			}
		}
		switch row.Issue = automationIssue(st); row.Issue {
		case "unavailable", "restored":
			broken = append(broken, row)
		case "never_triggered":
			never = append(never, row)
		default:
			rest = append(rest, row)
		}
	}
	for _, id := range x.disabled {
		if inDomain(id, "automation") {
			disabled = append(disabled, mapRow{EntityID: id, Name: x.name(id), State: "disabled"})
		}
	}
	rows := []mapRow{}
	for _, group := range [][]mapRow{broken, never, rest, disabled} {
		byName(group)
		rows = append(rows, group...)
	}
	return rows
}

// unavailable: every unavailable state, and unknown outside stateless
// domains; longest current-state age first.
func (x *homeIndex) unavailable() (map[string]any, []mapRow) {
	c := map[string]any{"total": 0, "unavailable": 0, "unknown": 0, "at_least_7d": 0, "restored": 0}
	type aged struct {
		row mapRow
		age time.Duration
	}
	var rows []aged
	for _, id := range x.enabled {
		st := x.state[id]
		domain, _ := splitID(id)
		if st == nil || presenceDomains[domain] || !(st.State == "unavailable" || (st.State == "unknown" && !statelessDomains[domain])) {
			continue
		}
		c["total"] = c["total"].(int) + 1
		c[st.State] = c[st.State].(int) + 1
		row := mapRow{EntityID: id, Name: x.name(id), Area: x.area(id), State: st.State, Restored: st.Attributes.Restored}
		if row.Restored {
			c["restored"] = c["restored"].(int) + 1
		}
		age, ok := x.age(st)
		if ok {
			row.Age = shortAge(age)
			if age >= 7*24*time.Hour {
				c["at_least_7d"] = c["at_least_7d"].(int) + 1
			}
		}
		rows = append(rows, aged{row, age})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].age > rows[j].age })
	out := []mapRow{}
	for _, r := range rows {
		out = append(out, r.row)
	}
	return c, out
}

// batteries: device_class battery states; low per the health detector;
// mobile_app batteries are phones, never device batteries. Lowest first.
func (x *homeIndex) batteries() (map[string]any, []mapRow) {
	c := map[string]any{"total": 0, "low": 0, "levels_low": 0, "phones_low": 0, "threshold": batteryLowBelow}
	type level struct {
		row   mapRow
		value float64
	}
	var rows []level
	for _, id := range x.enabled {
		st := x.state[id]
		if st == nil || st.deviceClass() != "battery" || st.State == "unavailable" || st.State == "unknown" {
			continue
		}
		row := mapRow{EntityID: id, Name: x.name(id), Area: x.area(id), Kind: "level"}
		switch {
		case x.platform[id] == "mobile_app":
			row.Kind = "phone"
		case x.category[id] == "diagnostic": // Zigbee, Z-Wave, Matter device batteries
			row.Kind = "device"
		}
		value := 1000.0
		switch number, err := strconv.ParseFloat(strings.TrimSpace(st.State), 64); {
		case err == nil:
			value, row.Level, row.Low = number, strconv.FormatFloat(number, 'f', -1, 64)+"%", number < batteryLowBelow
		case st.State == "low" || (inDomain(id, "binary_sensor") && st.State == "on"):
			value, row.Level, row.Low = -1, "low", true
		case inDomain(id, "binary_sensor") && st.State == "off":
			row.Level = "ok"
		default:
			row.Level = st.State
		}
		c["total"] = c["total"].(int) + 1
		if row.Low {
			key := map[string]string{"device": "low", "phone": "phones_low", "level": "levels_low"}[row.Kind]
			c[key] = c[key].(int) + 1
		}
		rows = append(rows, level{row, value})
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].value < rows[j].value })
	out := []mapRow{}
	for _, r := range rows {
		out = append(out, r.row)
	}
	return c, out
}
