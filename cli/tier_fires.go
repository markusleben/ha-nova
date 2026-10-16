package main

import (
	"encoding/json"
	"slices"
	"strings"
)

// Whether a matched consumer fires for the planned change. The tier never
// reads this: every trigger that references the entity is still classified
// (fail closed); only the user-facing consumer list is worded and filtered.

const (
	firesWont = "wont" // provably not for this change (internal; never shown)
	firesMay  = "may"
	firesWill = "will"
)

var firesRank = map[string]int{"": 0, firesWont: 1, firesMay: 2, firesWill: 3}

// stateDelta is the planned state change of a scanned entity. to comes from
// the action alone (turn_on/turn_off on an on/off entity), so a verdict that
// rests on it holds whenever the step runs; from is the plan-time state and
// only ever decides will versus may, never wont.
type stateDelta struct {
	from, to string
}

func plannedDelta(id, eff string, data map[string]any, st *entityState) stateDelta {
	domain, _ := splitID(id)
	serviceDomain, name := splitID(eff)
	d := stateDelta{from: st.state()}
	if d.from != "on" && d.from != "off" {
		d.from = ""
	}
	if serviceDomain != domain || !onOffDomains[domain] {
		return stateDelta{}
	}
	switch name {
	case "turn_off":
		d.to = "off"
	case "turn_on":
		for key, v := range data { // brightness 0 or a step can end off
			n, isNumber := numberOf(v)
			if strings.Contains(key, "_step") || (slices.Contains([]string{"brightness", "brightness_pct", "percentage"}, key) && (!isNumber || n <= 0)) {
				return d
			}
		}
		d.to = "on"
	}
	return d // toggle: its end state follows live state, so to stays unknown
}

// triggerMatch: "" when no trigger references the entity (or its device);
// otherwise the strongest per-trigger verdict. A trigger targeted by a
// selector this scan does not resolve counts as a match (may).
func triggerMatch(cfg map[string]any, id string, needles []string, d stateDelta) string {
	best := ""
	for _, trigger := range flattenTriggers(firstPresent(cfg, "triggers", "trigger")) {
		verdict := ""
		if target, present := trigger["target"]; present {
			if m, isMap := target.(map[string]any); !isMap || m["area_id"] != nil || m["floor_id"] != nil || m["label_id"] != nil || m["device_id"] != nil {
				verdict = firesMay
			}
		}
		raw, _ := json.Marshal(trigger)
		for _, needle := range needles {
			if strings.Contains(string(raw), needle) {
				verdict = strongest(verdict, stateTriggerFires(trigger, id, d))
			}
		}
		best = strongest(best, verdict)
	}
	return best
}

func strongest(a, b string) string {
	if firesRank[b] > firesRank[a] {
		return b
	}
	return a
}

// stateTriggerFires: wont only when the trigger's to/not_to filters reject
// the end state the action itself sets; will only for a plain state trigger
// on the entity whose filters accept a real change (to known, from known and
// different); everything else (other trigger kinds, attribute, templated or
// unknown ids, an unknown or unchanged state) may.
func stateTriggerFires(trigger map[string]any, id string, d stateDelta) string {
	if firstPresent(trigger, "trigger", "platform") != "state" || trigger["attribute"] != nil || !slices.Contains(literalIDs(trigger["entity_id"]), id) {
		return firesMay
	}
	verdict := firesWill
	for _, key := range []string{"to", "not_to", "from", "not_from"} {
		v, present := trigger[key]
		if !present || v == nil { // to: null still needs a state change, checked below
			continue
		}
		values, literal := literalStates(v)
		want := map[bool]string{true: d.to, false: d.from}[key == "to" || key == "not_to"]
		switch {
		case !literal || want == "":
			verdict = firesMay
		case slices.Contains(values, want) != strings.HasPrefix(key, "not_"):
		case key == "to" || key == "not_to":
			return firesWont
		default:
			verdict = firesMay // from is live state: it never proves wont
		}
	}
	if verdict == firesWill && (d.to == "" || d.from == "" || d.from == d.to) {
		verdict = firesMay
	}
	return verdict
}

func literalIDs(v any) []string {
	var ids []string
	switch x := v.(type) {
	case string:
		for _, part := range strings.Split(x, ",") {
			ids = append(ids, strings.ToLower(strings.TrimSpace(part)))
		}
	case []any:
		for _, item := range x {
			if s, isString := item.(string); isString {
				ids = append(ids, strings.ToLower(strings.TrimSpace(s)))
			}
		}
	}
	return ids
}

func literalStates(v any) ([]string, bool) {
	if s, isString := v.(string); isString {
		return []string{s}, true
	}
	list, isList := v.([]any)
	var out []string
	for _, item := range list {
		s, isString := item.(string)
		if !isString {
			return nil, false
		}
		out = append(out, s)
	}
	return out, isList
}

// fireRank orders verdicts; an event-listener match ranks as may.
func fireRank(match string) int {
	if rank, known := firesRank[match]; known {
		return rank
	}
	return firesRank[firesMay]
}

// matchConsumer keeps the strongest verdict per consumer. will also needs an
// automation that is on and has no conditions of its own.
func (e *tierEngine) matchConsumer(automation, match, on string) {
	if match == "" {
		return
	}
	conditions := firstPresent(e.configs[automation], "conditions", "condition")
	if list, isList := conditions.([]any); match == firesWill && ((conditions != nil && (!isList || len(list) > 0)) || e.states[automation].state() != "on") {
		match = firesMay
	}
	e.firedOn[automation] = append(e.firedOn[automation], on) // "" (an event listener) is always reached
	for i := range e.consumers {
		if c := &e.consumers[i]; c.Automation == automation && (c.Match == "" || fireRank(match) > fireRank(c.Match)) {
			c.Match = match
		}
	}
}

// shown is the user-facing members and consumers: an ordinary consumer that
// provably does not fire for the planned change is dropped, and so is every
// member (and further consumer) reached only through it. A consumer that
// raised the tier stays, as may.
func (e *tierEngine) shown() ([]tierMember, []tierConsumer) {
	dropped := map[string]bool{}
	routes := map[string][]string{} // member entity → every via that reaches it
	for _, m := range e.members {
		if m.EntityID != "" {
			routes[m.EntityID] = append(routes[m.EntityID], m.Via)
		}
	}
	for id, vias := range e.routes {
		if _, member := routes[id]; member {
			routes[id] = append(routes[id], vias...)
		}
	}
	var live func(id string, seen map[string]bool) bool
	routeLive := func(via string, seen map[string]bool) bool {
		root, _, _ := strings.Cut(via, "/")
		if dropped[root] {
			return false
		}
		return live(root, seen)
	}
	live = func(id string, seen map[string]bool) bool {
		if _, member := routes[id]; !member { // a direct target, or not acted on as a member
			return true
		}
		if seen[id] { // a cycle keeps the entity (fail toward showing)
			return true
		}
		seen[id] = true
		defer delete(seen, id)
		for _, via := range routes[id] {
			if routeLive(via, seen) {
				return true
			}
		}
		return false
	}
	ran := func(automation string) bool { // run as a step or a direct target, not only by its trigger
		_, member := routes[automation]
		return member || len(e.routes[automation]) > 0
	}
	for changed := true; changed; {
		changed = false
		for _, c := range e.consumers {
			if dropped[c.Automation] || c.Class != tierOrdinary.String() || ran(c.Automation) {
				continue
			}
			unreached := len(e.firedOn[c.Automation]) > 0 && !slices.ContainsFunc(e.firedOn[c.Automation], func(on string) bool { return live(on, map[string]bool{}) })
			if c.Match == firesWont || unreached {
				dropped[c.Automation], changed = true, true
			}
		}
	}
	// certain: reached without passing through a consumer that only may
	// fire, so a will downstream of a may stays may.
	match := map[string]string{}
	for _, c := range e.consumers {
		match[c.Automation] = c.Match
	}
	var certain func(id string, seen map[string]bool) bool
	certain = func(id string, seen map[string]bool) bool {
		m, consumer := match[id]
		_, member := routes[id]
		switch {
		case seen[id]:
			return false
		case consumer && !ran(id) && m != firesWill:
			return false
		case !member && (!consumer || ran(id)):
			return true // a direct target, or not acted on as a member
		}
		seen[id] = true
		defer delete(seen, id)
		if consumer && !ran(id) { // a will consumer is as certain as what fires it
			return slices.ContainsFunc(e.firedOn[id], func(on string) bool { return certain(on, seen) })
		}
		return slices.ContainsFunc(routes[id], func(via string) bool {
			root, _, _ := strings.Cut(via, "/")
			return !dropped[root] && certain(root, seen)
		})
	}
	consumers := []tierConsumer{}
	for _, c := range e.consumers {
		switch {
		case c.Match == firesWont:
			c.Match = firesMay
		case c.Match == firesWill && !slices.ContainsFunc(e.firedOn[c.Automation], func(on string) bool { return certain(on, map[string]bool{}) }):
			c.Match = firesMay
		}
		if !dropped[c.Automation] {
			consumers = append(consumers, c)
		}
	}
	// A row keeps only a live route of its own; a dropped row whose action a
	// live path repeated (deduplicated, so it left no row) is shown through
	// that path.
	members, shownLive := []tierMember{}, map[string]bool{}
	for _, m := range e.members {
		if routeLive(m.Via, map[string]bool{}) {
			members, shownLive[m.EntityID] = append(members, m), true
		}
	}
	for _, m := range e.members {
		if m.EntityID == "" || shownLive[m.EntityID] || routeLive(m.Via, map[string]bool{}) {
			continue
		}
		if i := slices.IndexFunc(e.routes[m.EntityID], func(via string) bool { return via != "" && routeLive(via, map[string]bool{}) }); i >= 0 {
			m.Via = e.routes[m.EntityID][i]
			members, shownLive[m.EntityID] = append(members, m), true
		}
	}
	return members, consumers
}
