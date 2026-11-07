package main

import (
	"encoding/json"
	"slices"
	"strings"
)

// gateVerdict is the one entry point that rates a request the Relay would
// forward, for the Relay's typed gate
// (docs/work/2026-11-03-relay-typed-gate-spec.md → Contract). It decides
// nothing itself: the tier comes from the classifiers the plans use, so a
// plan and the gate always agree. `ha-nova relay` (relayTyped) calls it
// today; the gate worker calls it with its own session.

type gateDecision string

const (
	gatePass   gateDecision = "pass"   // forward unchanged
	gateTyped  gateDecision = "typed"  // forward only with a person's approval
	gateRefuse gateDecision = "refuse" // never forward
)

type gateCheck struct {
	Verdict gateDecision `json:"verdict"`
	Kind    string       `json:"kind"`          // "read" only for a recognized read that passes, else "write"
	Why     string       `json:"why,omitempty"` // an opaque reason id for diagnostics; it never authorizes anything
	Item    int          `json:"-"`             // index in gateRequests of the request that set the verdict
}

// gateDecisionOf maps a tier onto the gate: below typed passes, typed waits
// for a person, and every other value is refused.
func gateDecisionOf(level tierLevel) gateDecision {
	switch level {
	case tierOrdinary, tierPreview:
		return gatePass
	case tierTyped:
		return gateTyped
	}
	return gateRefuse
}

// gateRequests: what the gate rates in a body, in order: a collect_events
// envelope's message and, when the envelope also has a type, the envelope
// itself; a core request with its method upper-cased in place, as the Relay
// sends it.
func gateRequests(endpoint string, payload map[string]any) []map[string]any {
	switch endpoint {
	case "core":
		method, _ := payload["method"].(string)
		payload["method"] = strings.ToUpper(strings.TrimSpace(method))
	case "ws":
		if message, ok := payload["message"].(map[string]any); ok {
			if _, typed := payload["type"]; typed {
				return []map[string]any{message, payload}
			}
			return []map[string]any{message}
		}
	}
	return []map[string]any{payload}
}

// gateVerdict rates body, the exact bytes sent to endpoint. Every request in
// it is rated and the strictest verdict wins (refuse, then typed, then
// pass), so a typed one never masks a refused one. The first pass reads
// nothing: a read the tier needs counts as failed, so typed. Only a typed
// result that came from such a read opens the session and rates again live,
// where a failed read still counts as typed. A body that is not one JSON
// object (null and trailing data included), an endpoint the gate doesn't
// know, or a request the raw guard refuses is refused. Kind is read only
// for a recognized read that passes. On err (the session didn't open) the
// check is still the fail-closed typed verdict.
func gateVerdict(endpoint string, body []byte, session func() (*relaySession, error)) (gateCheck, error) {
	payload, ok := decodeObject(body)
	if !ok || payload == nil || !json.Valid(body) || !slices.Contains([]string{"ws", "core", "files", "backups", "supervisor"}, endpoint) {
		return gateCheck{Verdict: gateRefuse, Kind: "write"}, nil
	}
	switch { // shapes no classifier here rates yet (service calls, config writes, Supervisor writes) stay refused
	case (endpoint == "ws" || endpoint == "core") && checkRawRelayRequest(endpoint, body) != nil,
		endpoint == "supervisor" && (rawSupervisorWrite(payload) || checkRawSupervisor(payload) != nil):
		return gateCheck{Verdict: gateRefuse, Kind: "write"}, nil
	}
	check := gateCheck{Verdict: gatePass, Kind: "write"}
	rank := map[gateDecision]int{gatePass: 0, gateTyped: 1, gateRefuse: 2}
	for i, request := range gateRequests(endpoint, payload) {
		n := newRawNamer(nil, nil)
		level, why := rawItemTier(n, rawRequest{Endpoint: endpoint}, request)
		if level == tierTyped && n.missed && check.Verdict == gatePass { // a live read can only lower it; once typed, it changes nothing
			s, err := session()
			if err != nil {
				return gateCheck{Verdict: gateTyped, Kind: "write", Why: why, Item: i}, err
			}
			level, why = rawItemTier(newRawNamer(s, nil), rawRequest{Endpoint: endpoint}, request)
		}
		if verdict := gateDecisionOf(level); rank[verdict] > rank[check.Verdict] {
			check = gateCheck{Verdict: verdict, Kind: "write", Why: why, Item: i}
		}
	}
	if check.Verdict == gatePass && rawRelayRead(endpoint, body) {
		check.Kind = "read"
	}
	return check, nil
}
