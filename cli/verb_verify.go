package main

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Post-call verification (typed-verbs spec §2.3, outcome-verification.md):
// poll /api/states against a per-domain expect table, wait out transitional
// states, and report verified|accepted|unverified|failed per target. A field
// the call sets that no probe can check downgrades the target to accepted.

var (
	verifyInterval   = 500 * time.Millisecond
	verifyWindowFast = 3 * time.Second  // lights, switches, helpers
	verifyWindowSlow = 10 * time.Second // covers, valves, locks, alarm panels
)

type verifyRow struct {
	EntityID string `json:"entity_id"`
	Expect   string `json:"expect,omitempty"`
	Observed string `json:"observed,omitempty"`
	Outcome  string `json:"outcome"`
	Note     string `json:"note,omitempty"`
}

// check: one field ("" = state) compared literally, against a fixed set,
// numerically within tol, or as a JSON list.
type check struct {
	field, kind string // literal | oneOf | numeric | list
	want        any
	tol         float64
}

type probe struct {
	entityID string
	checks   []check
	slow     bool
	before   *entityState // the plan-time state: "still X" vs "ended X"
}

var onOffDomains = map[string]bool{
	"light": true, "switch": true, "fan": true, "input_boolean": true, "siren": true, "humidifier": true, "remote": true, "automation": true,
}

var slowDomains = map[string]bool{"cover": true, "valve": true, "lock": true, "alarm_control_panel": true}

var transitionalStates = map[string]bool{"opening": true, "closing": true, "locking": true, "unlocking": true, "arming": true, "disarming": true, "pending": true}

// Service fields that promise no end state.
var ignorableFields = map[string]bool{"transition": true, "flash": true}

// Scene attributes worth probing; the rest of a stored snapshot (names,
// supported modes, derived colors) is not a promise.
var sceneAttributes = map[string]float64{
	"brightness": 2, "color_temp": 3, "color_temp_kelvin": 100, "percentage": 1,
	"current_tilt_position": 0, "temperature": 0.05,
	"preset_mode": -1, "fan_mode": -1, "effect": -1, // -1: literal
}

// expectFor maps one call on one entity to its probe; false means some
// promise cannot be checked (an unknown service is accepted, never verified).
func expectFor(service, entityID string, data map[string]any, before *entityState, scene bool) (probe, bool) {
	domain, _ := splitID(entityID)
	_, name := splitID(effectiveService(service, domain))
	p := probe{entityID: entityID, slow: slowDomains[domain], before: before}
	literal := func(want string) { p.checks = append(p.checks, check{kind: "literal", want: want}) }
	switch state, stored := data[sceneStateKey].(string); {
	case scene && stored:
		literal(state) // the scene's stored state is its promise
	case scene:
	case onOffDomains[domain] && (name == "turn_on" || name == "turn_off"):
		literal(strings.TrimPrefix(name, "turn_"))
	case onOffDomains[domain] && name == "toggle" && (before.state() == "on" || before.state() == "off"):
		literal(map[string]string{"on": "off", "off": "on"}[before.state()])
	case domain == "lock" && name == "open":
		p.checks = append(p.checks, check{kind: "oneOf", want: []string{"open", "unlocked"}})
	case domain == "lock" && (name == "lock" || name == "unlock"):
		literal(name + "ed")
	case (domain == "cover" || domain == "valve") && (name == "open_cover" || name == "open_valve"):
		literal("open")
	case (domain == "cover" || domain == "valve") && (name == "close_cover" || name == "close_valve"):
		literal("closed")
	case domain == "alarm_control_panel" && strings.HasPrefix(name, "alarm_arm_"):
		literal("armed_" + strings.TrimPrefix(name, "alarm_arm_"))
	case domain == "alarm_control_panel" && (name == "alarm_disarm" || name == "alarm_trigger"):
		literal(map[string]string{"alarm_disarm": "disarmed", "alarm_trigger": "triggered"}[name])
	}
	for _, key := range sortedKeys(data) {
		_, probed := sceneAttributes[key]
		if ignorableFields[key] || key == sceneStateKey || (scene && !probed && key != "position") {
			continue // no end-state promise, or snapshot noise (names, derived colors)
		}
		c, known := fieldCheck(domain, key, data[key], before)
		if !known && scene {
			c, known = sceneCheck(key, data[key])
		}
		if !known {
			return p, false
		}
		p.checks = append(p.checks, c)
	}
	return p, len(p.checks) > 0
}

// fieldCheck maps a service field to the attribute (or state) it sets.
func fieldCheck(domain, key string, v any, before *entityState) (check, bool) {
	num, isNumber := v.(float64)
	text, isText := v.(string)
	numeric := func(field string, want, tol float64) (check, bool) {
		return check{field: field, kind: "numeric", want: want, tol: tol}, isNumber
	}
	switch key {
	case "brightness_pct":
		return numeric("brightness", num*255/100, 3)
	case "brightness":
		return numeric("brightness", num, 1)
	case "color_temp_kelvin":
		return numeric("color_temp_kelvin", num, 100)
	case "color_temp":
		return numeric("color_temp", num, 3)
	case "percentage":
		return numeric("percentage", num, math.Max(1, before.num("percentage_step")/2+0.5))
	case "position":
		return numeric("current_position", num, 0)
	case "tilt_position":
		return numeric("current_tilt_position", num, 0)
	case "temperature", "target_temp_high", "target_temp_low", "humidity":
		return numeric(key, num, 0.05)
	case "preset_mode", "fan_mode", "swing_mode", "effect":
		return check{field: key, kind: "literal", want: text}, isText
	case "hvac_mode", "option", "value": // the entity state itself (stateBackedField)
		if !stateBackedField(domain, key) {
			return check{}, false
		}
		if domain == "input_number" || domain == "number" {
			return numeric("", num, 1e-9)
		}
		return check{kind: "literal", want: text}, isText
	case "rgb_color":
		_, isList := v.([]any)
		return check{field: key, kind: "list", want: v}, isList
	}
	return check{}, false
}

func sceneCheck(key string, v any) (check, bool) {
	tol, known := sceneAttributes[key]
	if !known {
		return check{}, false
	}
	if tol < 0 {
		text, isText := v.(string)
		return check{field: key, kind: "literal", want: text}, isText
	}
	num, isNumber := v.(float64)
	return check{field: key, kind: "numeric", want: num, tol: tol}, isNumber
}

func (st *entityState) state() string {
	if st == nil {
		return ""
	}
	return st.State
}

// statelessRun: a scene, script, button, or automation run does not show
// the call in the target's own state.
func statelessRun(domain, service string) bool {
	_, name := splitID(service)
	return isRun(domain, service) || ((domain == "button" || domain == "input_button") && name == "press")
}

// verifyCall builds the probes for the frozen action and polls them. A scene
// is verified only when every member it was read to set (through groups
// too) has a probe and every probe matches; otherwise it is accepted.
func verifyCall(s *relaySession, e *tierEngine, action planAction) ([]verifyRow, string) {
	var probes []probe
	var rows []verifyRow
	accepted := len(action.EntityIDs) == 0 // targetless: no state to read
	acceptRow := func(id string) { rows, accepted = append(rows, verifyRow{EntityID: id, Outcome: "accepted"}), true }
	for _, id := range action.EntityIDs {
		domain, _ := splitID(id)
		switch {
		case domain == "scene" && isRun(domain, action.Service):
			members, complete := sceneProbes(e, id)
			if !complete || len(members) == 0 {
				acceptRow(id)
				continue
			}
			probes = append(probes, members...)
		case statelessRun(domain, action.Service):
			acceptRow(id)
		default:
			p, known := expectFor(action.Service, id, action.Data, e.states[id], false)
			if !known {
				acceptRow(id)
				continue
			}
			probes = append(probes, p)
		}
	}
	rows = append(rows, pollProbes(s, probes)...)
	outcome := "verified"
	if accepted {
		outcome = "accepted"
	}
	for _, row := range rows {
		switch row.Outcome {
		case "failed":
			return rows, "failed"
		case "unverified":
			outcome = "unverified"
		case "accepted":
			if outcome == "verified" {
				outcome = "accepted"
			}
		}
	}
	return rows, outcome
}

// sceneProbes collects the probes of every member whose Via chain (scene →
// group → leaf) leads back to the scene.
func sceneProbes(e *tierEngine, sceneID string) ([]probe, bool) {
	parent := map[string]string{}
	for _, m := range e.members {
		if m.EntityID != "" {
			parent[m.EntityID] = m.Via
		}
	}
	reaches := func(via string) bool {
		for hops := 0; via != "" && hops <= tierMaxDepth; hops++ {
			if via == sceneID {
				return true
			}
			via = parent[via]
		}
		return false
	}
	var probes []probe
	for _, m := range e.members {
		if !reaches(m.Via) {
			continue
		}
		p, known := expectFor(m.Action, m.EntityID, m.data, e.states[m.EntityID], true)
		if !known {
			return nil, false
		}
		probes = append(probes, p)
	}
	return probes, true
}

// pollProbes reads each pending probe every verifyInterval until all its
// checks hold in a settled state or its window closes; every read is bounded
// by what is left of the window. A slow device still moving at the end is
// accepted (in progress), not unverified.
func pollProbes(s *relaySession, probes []probe) []verifyRow {
	rows := make([]verifyRow, len(probes))
	last := make([]*entityState, len(probes))
	late := make([]string, len(probes)) // what went wrong after a good read
	start := time.Now()
	window := func(p probe) time.Duration {
		if p.slow {
			return verifyWindowSlow
		}
		return verifyWindowFast
	}
	for i, p := range probes {
		rows[i] = verifyRow{EntityID: p.entityID, Expect: describeChecks(p.checks), Outcome: "unverified"}
	}
	for {
		pending := false
		for i, p := range probes {
			remaining := window(p) - time.Since(start)
			if rows[i].Outcome != "unverified" || remaining <= 0 {
				continue
			}
			st, err := s.stateFresh(p.entityID, max(remaining, 100*time.Millisecond))
			switch {
			case (err != nil || st == nil) && last[i] != nil:
				// keep the last good observation: a slow read is not a failed device
				late[i] = map[bool]string{true: "failed", false: "found the entity missing"}[err != nil]
			case err != nil:
				rows[i].Observed = "read failed"
			case st == nil:
				rows[i].Observed = "missing"
			default:
				last[i], late[i] = st, ""
				rows[i].Observed = describeObserved(p.checks, st)
				if st.State == "jammed" {
					rows[i].Outcome = "failed"
					continue
				}
				if !transitionalStates[st.State] && checksHold(p.checks, st) {
					rows[i].Outcome = "verified"
					continue
				}
			}
			pending = true
		}
		if !pending {
			break
		}
		time.Sleep(verifyInterval)
	}
	for i, p := range probes {
		switch {
		case rows[i].Outcome != "unverified" || last[i] == nil:
		case late[i] != "": // the final read failed: never accepted on an older answer
			rows[i].Note = fmt.Sprintf("last read %s; later reads %s", rows[i].Observed, late[i])
		case p.slow && transitionalStates[last[i].State]:
			rows[i].Outcome, rows[i].Note = "accepted", "still "+last[i].State+" when the window closed"
		default: // the device answered, it just did not get there
			verb := "ended"
			if unchanged(p, last[i]) {
				verb = "still"
			}
			rows[i].Note = fmt.Sprintf("%s %s after %s (expected %s)", verb, rows[i].Observed, window(p), rows[i].Expect)
		}
	}
	return rows
}

// unchanged: every check that failed reads exactly what it read before the
// call — the device did not move ("still"); otherwise it moved elsewhere.
func unchanged(p probe, now *entityState) bool {
	if p.before == nil {
		return false
	}
	for _, c := range p.checks {
		if checksHold([]check{c}, now) {
			continue
		}
		a, _ := json.Marshal(fieldValue(c, now))
		b, _ := json.Marshal(fieldValue(c, p.before))
		if string(a) != string(b) {
			return false
		}
	}
	return true
}

func fieldValue(c check, st *entityState) any {
	if c.field == "" {
		return st.State
	}
	return st.attr(c.field)
}

func checksHold(checks []check, st *entityState) bool {
	for _, c := range checks {
		observed := fieldValue(c, st)
		switch c.kind {
		case "literal":
			if text, _ := observed.(string); text != c.want {
				return false
			}
		case "oneOf":
			text, _ := observed.(string)
			if !slices.Contains(c.want.([]string), text) {
				return false
			}
		case "numeric":
			n, ok := numberOf(observed)
			if !ok || math.Abs(n-c.want.(float64)) > c.tol {
				return false
			}
		case "list":
			a, _ := json.Marshal(observed)
			b, _ := json.Marshal(c.want)
			if string(a) != string(b) {
				return false
			}
		}
	}
	return true
}

// numberOf reads a numeric attribute, or a numeric state (helpers report
// their value as a string state).
func numberOf(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case string:
		n, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return n, err == nil
	}
	return 0, false
}

func describeChecks(checks []check) string {
	var parts []string
	for _, c := range checks {
		want := deltaValue(c.want)
		if c.kind == "numeric" {
			want = "≈" + deltaValue(math.Round(c.want.(float64)*100)/100)
		}
		if c.field == "" {
			parts = append(parts, want)
		} else {
			parts = append(parts, c.field+"="+want)
		}
	}
	return strings.Join(parts, ", ")
}

func describeObserved(checks []check, st *entityState) string {
	parts := []string{st.State}
	for _, c := range checks {
		if c.field != "" {
			parts = append(parts, c.field+"="+deltaValue(st.attr(c.field)))
		}
	}
	return strings.Join(parts, ", ")
}
