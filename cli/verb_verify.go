package main

import (
	"strconv"
	"strings"
	"time"
)

// Post-call verification (typed-verbs spec §2.3, outcome-verification.md):
// poll /api/states against a per-domain expect table, wait out transitional
// states, and report verified|accepted|unverified|failed per target.

var (
	verifyInterval   = 500 * time.Millisecond
	verifyWindowFast = 3 * time.Second  // lights, switches, helpers
	verifyWindowSlow = 10 * time.Second // covers, locks, alarm panels
)

type verifyRow struct {
	EntityID string `json:"entity_id"`
	Expect   string `json:"expect,omitempty"`
	Observed string `json:"observed,omitempty"`
	Outcome  string `json:"outcome"`
}

// probe: one entity reaching a state (field "") or an attribute value.
type probe struct {
	entityID, field, want string
	slow                  bool
}

var onOffDomains = map[string]bool{
	"light": true, "switch": true, "fan": true, "input_boolean": true, "siren": true, "humidifier": true, "remote": true, "automation": true,
}

var transitionalStates = map[string]bool{"opening": true, "closing": true, "locking": true, "unlocking": true, "arming": true, "disarming": true, "pending": true}

// expectFor maps one call on one entity to its probe; false means no state
// promise is known (an unknown service is accepted, never verified).
func expectFor(service, entityID string, data map[string]any, before *entityState) (probe, bool) {
	domain, _ := splitID(entityID)
	_, name := splitID(effectiveService(service, domain))
	p := probe{entityID: entityID, slow: domain == "cover" || domain == "lock" || domain == "alarm_control_panel"}
	value := func(key string) (string, bool) {
		v, present := data[key]
		if !present {
			return "", false
		}
		return deltaValue(v), true
	}
	ok := true
	switch {
	case onOffDomains[domain] && (name == "turn_on" || name == "turn_off"):
		p.want = strings.TrimPrefix(name, "turn_")
	case onOffDomains[domain] && name == "toggle" && (before.state() == "on" || before.state() == "off"):
		p.want = map[string]string{"on": "off", "off": "on"}[before.state()]
	case domain == "lock":
		p.want, ok = map[string]string{"lock": "locked", "unlock": "unlocked", "open": "open|unlocked"}[name]
	case domain == "cover" && name == "set_cover_position":
		p.field = "current_position"
		p.want, ok = value("position")
	case domain == "cover":
		p.want, ok = map[string]string{"open_cover": "open", "close_cover": "closed"}[name]
	case domain == "alarm_control_panel" && strings.HasPrefix(name, "alarm_arm_"):
		p.want = "armed_" + strings.TrimPrefix(name, "alarm_arm_")
	case domain == "alarm_control_panel":
		p.want, ok = map[string]string{"alarm_disarm": "disarmed", "alarm_trigger": "triggered"}[name]
	case name == "set_value" && (domain == "input_number" || domain == "input_text" || domain == "number" || domain == "text"):
		p.want, ok = value("value")
	case name == "select_option" && (domain == "input_select" || domain == "select"):
		p.want, ok = value("option")
	case domain == "climate" && name == "set_hvac_mode":
		p.want, ok = value("hvac_mode")
	case domain == "climate" && name == "set_temperature":
		p.field = "temperature"
		p.want, ok = value("temperature")
	default:
		ok = false
	}
	return p, ok
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
// is verified only when every member it was read to set has a probe that
// matches; otherwise it is accepted.
func verifyCall(s *relaySession, e *tierEngine, action planAction) ([]verifyRow, string) {
	var probes []probe
	var rows []verifyRow
	accepted := len(action.EntityIDs) == 0 // targetless: no state to read
	for _, id := range action.EntityIDs {
		domain, _ := splitID(id)
		switch {
		case domain == "scene" && isRun(domain, action.Service):
			members, complete := sceneProbes(e, id)
			if !complete || len(members) == 0 {
				rows, accepted = append(rows, verifyRow{EntityID: id, Outcome: "accepted"}), true
				continue
			}
			probes = append(probes, members...)
		case statelessRun(domain, action.Service):
			rows, accepted = append(rows, verifyRow{EntityID: id, Outcome: "accepted"}), true
		default:
			p, known := expectFor(action.Service, id, action.Data, e.states[id])
			if !known {
				rows, accepted = append(rows, verifyRow{EntityID: id, Outcome: "accepted"}), true
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
		if row.Outcome == "failed" {
			return rows, "failed"
		}
		if row.Outcome == "unverified" {
			outcome = "unverified"
		}
	}
	return rows, outcome
}

func sceneProbes(e *tierEngine, sceneID string) ([]probe, bool) {
	var probes []probe
	for _, member := range e.members {
		if member.Via != sceneID {
			continue
		}
		p, known := expectFor(member.Action, member.EntityID, nil, e.states[member.EntityID])
		if !known {
			return nil, false
		}
		probes = append(probes, p)
	}
	return probes, true
}

// pollProbes reads each pending probe every verifyInterval until it holds
// a non-transitional match or its window closes.
func pollProbes(s *relaySession, probes []probe) []verifyRow {
	rows := make([]verifyRow, len(probes))
	start := time.Now()
	for i, p := range probes {
		rows[i] = verifyRow{EntityID: p.entityID, Expect: describeProbe(p), Outcome: "unverified"}
	}
	for {
		pending := false
		for i, p := range probes {
			if rows[i].Outcome != "unverified" {
				continue
			}
			window := verifyWindowFast
			if p.slow {
				window = verifyWindowSlow
			}
			if time.Since(start) > window {
				continue
			}
			st, err := s.stateFresh(p.entityID)
			switch {
			case err != nil:
				rows[i].Observed = "read failed"
			case st == nil:
				rows[i].Observed = "missing"
			default:
				rows[i].Observed = describeObserved(p, st)
				if st.State == "jammed" {
					rows[i].Outcome = "failed"
					continue
				}
				if !transitionalStates[st.State] && sameValue(rows[i].Observed, p.want) {
					rows[i].Outcome = "verified"
					continue
				}
			}
			pending = true
		}
		if !pending {
			return rows
		}
		time.Sleep(verifyInterval)
	}
}

func describeProbe(p probe) string {
	if p.field != "" {
		return p.field + "=" + p.want
	}
	return p.want
}

func describeObserved(p probe, st *entityState) string {
	if p.field != "" {
		return deltaValue(st.attr(p.field))
	}
	return st.State
}

// sameValue matches any "|" alternative; numbers compare numerically.
func sameValue(observed, want string) bool {
	for _, alternative := range strings.Split(want, "|") {
		a, errA := strconv.ParseFloat(observed, 64)
		b, errB := strconv.ParseFloat(alternative, 64)
		if observed == alternative || (errA == nil && errB == nil && a == b) {
			return true
		}
	}
	return false
}
