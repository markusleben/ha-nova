package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode"
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
		if strings.ContainsFunc(method, func(r rune) bool { return r > unicode.MaxASCII }) { // JavaScript upper-cases "ﬆ" to "ST", Go doesn't
			method = ""
		}
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
// where a failed read still counts as typed. Refused: a body that is not
// exactly one JSON object, an endpoint the gate doesn't know, a request the
// spec refuses (gateDenied), and any shape the CLI never sends
// (gateRecognized). Kind is read only for a recognized read that passes:
// the worker's own reads pass the Relay only as reads, so a typed verb's
// read (an escaped config id, readStoredConfig) and a files or backups
// read (readLiveFile, rawSnapshotList) count; a flow step's GET stays a
// write. On
// err (the session didn't open) the check is still the fail-closed typed
// verdict.
func gateVerdict(endpoint string, body []byte, session func() (*relaySession, error)) (gateCheck, error) {
	refused := gateCheck{Verdict: gateRefuse, Kind: "write"}
	payload, ok := decodeObject(body)
	if !ok || payload == nil || !json.Valid(body) || !slices.Contains([]string{"ws", "core", "files", "backups", "supervisor"}, endpoint) || !gateFinite(payload) || gateDuplicateKeys(body) {
		return refused, nil
	}
	requests := gateRequests(endpoint, payload)
	for _, request := range requests {
		if gateDenied(endpoint, request) {
			return refused, nil
		}
	}
	if endpoint == "core" {
		if level, why, handled, err := gatePlanTier(payload, body, session); handled {
			return gateCheck{Verdict: gateDecisionOf(level), Kind: "write", Why: why}, err
		}
	}
	if !gateRecognized(endpoint, payload, body) {
		return refused, nil
	}
	check := gateCheck{Verdict: gatePass, Kind: "write"}
	rank := map[gateDecision]int{gatePass: 0, gateTyped: 1, gateRefuse: 2}
	for i, request := range requests {
		n := newRawNamer(nil, nil)
		level, why := rawItemTier(n, rawRequest{Endpoint: endpoint}, request)
		raw, _ := json.Marshal(request)
		read := rawRelayRead(endpoint, raw)
		if endpoint != "files" && endpoint != "backups" && !read && holdsHiddenValue(raw) { // guardRaw: a hidden value carried back
			return refused, nil
		}
		relisten := !read && (rawListened(endpoint, request) != nil || endpoint == "ws" && request["type"] == "todo/item/move")
		access := !read && (rawAccessTarget(endpoint, raw) != (rawTarget{}) || endpoint == "supervisor" && gateSupervisorOptions(request))
		reread := level == tierTyped && n.missed && check.Verdict == gatePass // a live read can only lower the static tier
		if reread || access || relisten && level < tierTyped {                // once typed, a listener changes nothing
			s, err := session()
			if err != nil {
				return gateCheck{Verdict: gateTyped, Kind: "write", Why: why, Item: i}, err
			}
			if reread {
				level, why = rawItemTier(newRawNamer(s, nil), rawRequest{Endpoint: endpoint}, request)
			}
			if access || relisten && level < tierTyped {
				if liveLevel, liveWhy := gateLiveTier(s, endpoint, request, raw); liveLevel > level {
					level, why = liveLevel, liveWhy
				}
			}
		}
		if verdict := gateDecisionOf(level); rank[verdict] > rank[check.Verdict] {
			check = gateCheck{Verdict: verdict, Kind: "write", Why: why, Item: i}
		}
	}
	action, _ := payload["action"].(string)
	storeRead := map[string]bool{"files read_file": true, "files list_dir": true, "files list_deleted": true, "backups list": true, "backups load": true}[endpoint+" "+action]
	target, _ := payload["path"].(string)
	target, _ = relayCorePath(target)
	verbRead := checkReadOnlyRequest(endpoint, payload) == nil && !flowStepPath.MatchString(target)
	if check.Verdict == gatePass && (rawRelayRead(endpoint, body) || verbRead || checkPhoneRead(endpoint, payload) == nil || storeRead) { // the phone lookup's template only renders
		check.Kind = "read"
	}
	return check, nil
}

// gateLiveTier runs what a raw write's plan and `ha-nova relay` decide from
// live Home Assistant reads beyond rawItemTier (computeRawPlan,
// guardRawRequestIn): the access-target refusals (a lock's entry, a factory
// reset button, an owner's account), an App's stored option secrets, and the
// listeners a write can start (rawListeners, rawShoppingList). A failed read
// is typed. Not here, as they need the client's own state, not the request:
// the setup forms a flow answered earlier (formSecretSet) and the copies
// earlier in a plan (kept).
func gateLiveTier(s *relaySession, endpoint string, request map[string]any, raw []byte) (tierLevel, string) {
	refusal, err := checkRawAccessTargetIn(s, endpoint, raw)
	if refusal == nil && err == nil && endpoint == "supervisor" {
		refusal, err = rawSupervisorFormSecret(s, raw)
	}
	switch {
	case err != nil && rawAccessTarget(endpoint, raw).warning != "": // whose warning it accepts is never left to a grant
		return tierRefused, "access_target"
	case err != nil:
		return tierTyped, "unread"
	case refusal != nil:
		return tierRefused, "access_target"
	}
	e := newTierEngine(s)
	return max(e.rawShoppingList(endpoint, request), e.rawListeners(endpoint, request)), "listeners"
}

// gateSupervisorOptions: an App options write, whose stored secrets
// rawSupervisorFormSecret reads.
func gateSupervisorOptions(request map[string]any) bool {
	method, _ := request["method"].(string)
	path, _ := request["path"].(string)
	kind, _ := rawSupervisorMatch(method, path)
	return kind == "options"
}

// gateFinite: every number in v is one the Relay passes on unchanged; one
// JSON.parse turns into Infinity (sent as null) or rounds to zero is not.
func gateFinite(v any) bool {
	switch v := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(v), 64)
		return err == nil && (f != 0 || !strings.ContainsAny(strings.Split(strings.ToLower(string(v)), "e")[0], "123456789"))
	case map[string]any:
		for _, inner := range v {
			if !gateFinite(inner) {
				return false
			}
		}
	case []any:
		for _, inner := range v {
			if !gateFinite(inner) {
				return false
			}
		}
	}
	return true
}

// gateDuplicateKeys: an object anywhere in body names a key twice (after
// unescaping, as Home Assistant reads it), or body doesn't parse. Readers
// disagree on which value wins, so the gate never rates such a body (§2).
func gateDuplicateKeys(body []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var walk func() bool
	walk = func() bool {
		token, err := dec.Token()
		if err != nil {
			return true
		}
		delim, isDelim := token.(json.Delim)
		switch {
		case !isDelim:
			return false
		case delim == '{':
			seen := map[string]bool{}
			for dec.More() {
				key, err := dec.Token()
				name, isKey := key.(string)
				if err != nil || !isKey || seen[name] {
					return true
				}
				seen[name] = true
				if walk() {
					return true
				}
			}
		case delim == '[':
			for dec.More() {
				if walk() {
					return true
				}
			}
		}
		_, err = dec.Token()
		return err != nil
	}
	return walk()
}

// gateRecognized: a shape the CLI sends to a device-facing endpoint: what
// `ha-nova relay` passes (its raw guard), what the typed verbs read
// (checkReadOnlyRequest, checkPhoneRead) or write besides the plans'
// service calls and config writes (dashboards, a device's area), Link
// setup's cloud/status read, a listed
// Supervisor request, every files action the Relay knows, and every backups action (the Relay checks
// those itself).
func gateRecognized(endpoint string, payload map[string]any, body []byte) bool {
	switch endpoint {
	case "ws", "core":
		return checkRawRelayRequest(endpoint, body) == nil || checkReadOnlyRequest(endpoint, payload) == nil ||
			checkPhoneRead(endpoint, payload) == nil || endpoint == "ws" && (dashboardWrite(payload) || deviceAreaUpdate(payload) || len(payload) == 1 && payload["type"] == "cloud/status") // Link setup (cloudLocalDiscovery)
	case "supervisor":
		return checkRawSupervisor(payload) == nil
	case "files": // the actions the Relay knows: a new one passes only a worker that rates it
		return slices.Contains([]any{"list_dir", "read_file", "write_file", "delete_file", "restore_file", "list_deleted"}, payload["action"])
	}
	return true
}

// gateLogRead: a log read, refused at the gate and by the CLI alike (spec
// section 2: free text no redactor bounds; guardRaw).
func gateLogRead(endpoint string, request map[string]any) bool {
	switch endpoint {
	case "core":
		raw, _ := request["path"].(string)
		decoded, ok := relayCorePath(raw)
		return ok && strings.HasPrefix(strings.ToLower(decoded), "/api/error_log")
	case "ws": // the log list, or its lines as events (system_log fire_event)
		trigger, _ := json.Marshal(request["trigger"])
		eventType, _ := request["event_type"].(string)
		return request["type"] == "system_log/list" ||
			request["type"] == "subscribe_events" && strings.EqualFold(eventType, "system_log_event") ||
			request["type"] == "subscribe_trigger" && strings.Contains(strings.ToLower(string(trigger)), "system_log_event")
	case "supervisor":
		method, _ := request["method"].(string)
		path, _ := request["path"].(string)
		kind, _ := rawSupervisorMatch(method, path)
		return kind == "logs"
	}
	return false
}

// gateDenied: what the gate refuses whatever allows it elsewhere (spec §2):
// event firing, execute_script, setting a state, every webhook request,
// backup downloads and Supervisor paths on core, log reads (free text no
// redactor bounds), and subscriptions the raw guard refuses (every
// event, call_service, phone confirmations, templates).
func gateDenied(endpoint string, request map[string]any) bool {
	if gateLogRead(endpoint, request) {
		return true
	}
	switch endpoint {
	case "core":
		method, _ := request["method"].(string)
		raw, _ := request["path"].(string)
		decoded, ok := relayCorePath(raw)
		ok = ok && !strings.ContainsAny(raw, "\t\n\r") // a URL parser drops them
		decoded = strings.ToLower(decoded)
		for _, prefix := range []string{"/api/webhook", "/api/backup", "/api/hassio"} {
			ok = ok && !strings.HasPrefix(decoded, prefix)
		}
		return !ok || method != http.MethodGet && (strings.HasPrefix(decoded, "/api/states") || strings.HasPrefix(decoded, "/api/events"))
	case "ws":
		wsType, _ := request["type"].(string)
		switch {
		case slices.Contains([]string{"fire_event", "execute_script", "call_service"}, wsType):
			return true
		case strings.HasPrefix(wsType, "subscribe_"):
			return checkRawWS(request) != nil
		}
	}
	return false
}
