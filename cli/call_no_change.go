package main

import (
	"errors"
	"strings"
)

// noChangeServices: the actions `call` may skip when every target already
// reads as their end state — full effective services, so a custom
// integration's own "unlock" never qualifies. Each one is the access direction, where a stale
// state costs the user one more step (next_step: plan call, which never
// skips), never security; the securing direction (lock, close, arm,
// turn_off) and anything without a fixed end state (toggle, scripts,
// buttons, lock.open) always runs. turn_on only where Home Assistant owns the
// state itself: a light or switch "on" can be a hardware group with members
// off, or a securing switch (guard mode, child lock).
var noChangeServices = map[string]bool{"lock.unlock": true, "cover.open_cover": true, "alarm_control_panel.alarm_disarm": true,
	"input_boolean.turn_on": true, "automation.turn_on": true}

// noChangeCall is the call path's no-op check: before a plan is stored, and
// also when only the confirmation channel (an unreachable phone) or Home
// Assistant's own code (CODE_REQUIRED) stands in the way — nothing would be
// sent, so neither is needed. true: env is the no_change result.
func noChangeCall(e *tierEngine, env *verbEnvelope, req planCallRequest, err error) bool {
	var failure *verbFailure
	errors.As(err, &failure)
	probe := *env
	switch {
	case e == nil:
		return false
	case err == nil && env.Action != nil && e.level != tierOrdinary:
	case failure != nil && failure.code == "CONFIRM_UNAVAILABLE" && env.Action != nil:
	case failure != nil && failure.code == "REFUSED" && len(env.Reasons) == 1 && env.Reasons[0].Code == reasonCodeRequired:
		ids := []string{}
		for _, t := range env.Resolved {
			ids = append(ids, t.EntityID)
		}
		probe.Action = &planAction{Service: req.Service, EntityIDs: ids, Data: req.Data}
		probe.Deltas = planDeltas(req.Service, ids, req.Data, e.states)
	default:
		return false
	}
	if !alreadyDone(e, &probe, *probe.Action) {
		return false
	}
	markNoChange(e, &probe, *probe.Action)
	*env = probe
	return true
}

// alreadyDone: every frozen target of the call already reads, fresh from
// Home Assistant in this session, as the state the action ends in, so there
// is nothing to send and nothing to confirm. Fail-closed: any service data,
// a target without a state delta (unread, unavailable, unknown, a mixed
// domain), an assumed state, a cover not fully open, a coverage gap,
// a skipped target the user must hear about (EXPANDED_TARGET_WITHOUT_STATE), or a
// call_service listener (it reacts to the call itself, not to a state
// change) keeps the normal path.
func alreadyDone(e *tierEngine, env *verbEnvelope, action planAction) bool {
	listeners := e.liveEvents()["call_service"] // first: a failed read clears e.complete
	if len(action.EntityIDs) == 0 || len(action.Data) > 0 || action.Response || !e.complete || len(e.skipped) > 0 ||
		listeners > callServiceListenerBaseline {
		return false
	}
	for _, id := range action.EntityIDs {
		domain, _ := splitID(id)
		effective := effectiveService(action.Service, domain)
		st := e.states[id]
		// assumed_state: Home Assistant only guesses it (RF, optimistic templates), so the device may differ.
		if st == nil || !noChangeServices[effective] || !strings.HasPrefix(effective, domain+".") || st.attr("assumed_state") == true {
			return false
		}
		if position, set := st.Attributes["current_position"]; domain == "cover" && set && position != 100.0 { // "open" covers 1–99 % too
			return false
		}
		done := false
		for _, d := range env.Deltas {
			if d.EntityID == id && d.Field == "state" {
				done = d.After != "" && d.Before == d.After && d.Warning == ""
			}
		}
		if !done {
			return false
		}
	}
	return true
}

// markNoChange turns env into the no-op result: no plan, no tier, exit 0,
// and one plain sentence to relay ("“Front Door” is already unlocked;
// nothing to do."). The deltas stay as the evidence.
func markNoChange(e *tierEngine, env *verbEnvelope, action planAction) {
	names := []string{}
	for _, id := range action.EntityIDs {
		names = append(names, sayText(e.sayName(env, id), true))
	}
	is := " is"
	if len(names) > 1 {
		is = " are"
	}
	after := env.Deltas[0].After // alreadyDone: every target's end state; a homeassistant.* mix may differ
	for _, d := range env.Deltas {
		if d.After != after {
			after = "done"
		}
	}
	*env = verbEnvelope{SchemaVersion: env.SchemaVersion, Verb: env.Verb, Server: env.Server, Status: "no_change",
		Resolved: env.Resolved, Deltas: env.Deltas,
		Say:      sayList(names) + is + " already " + after + "; nothing to do.",
		NextStep: "Relay `say` only. If the user says it is not so (Home Assistant can miss a manual change), run the same request with ha-nova plan call instead of call (same targets, --area, --device, or --match), show its preview, and apply only after their confirmation."}
}
