package main

import (
	"encoding/json"
	"errors"
	"testing"
)

// The gate's verdict set is closed: only a tier below typed passes, and any
// tier the gate doesn't know is refused.
func TestGateDecisionOfFailsClosed(t *testing.T) {
	for level, want := range map[tierLevel]gateDecision{tierOrdinary: gatePass, tierPreview: gatePass, tierTyped: gateTyped, tierRefused: gateRefuse, tierRefused + 1: gateRefuse, -1: gateRefuse} {
		if got := gateDecisionOf(level); got != want {
			t.Errorf("gateDecisionOf(%d) = %q, want %q", level, got, want)
		}
	}
}

// The worker's /check answer is this JSON; the Relay refuses any other shape.
func TestGateCheckWireShape(t *testing.T) {
	got, _ := json.Marshal(gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete", Item: 1})
	if want := `{"verdict":"typed","kind":"write","why":"delete"}`; string(got) != want {
		t.Errorf("wire = %s, want %s", got, want)
	}
	got, _ = json.Marshal(gateCheck{Verdict: gatePass, Kind: "read"})
	if want := `{"verdict":"pass","kind":"read"}`; string(got) != want {
		t.Errorf("wire = %s, want %s", got, want)
	}
}

func TestGateVerdict(t *testing.T) {
	noSession := func() (*relaySession, error) { return nil, errors.New("no session") }
	for _, tc := range []struct {
		name, endpoint, body string
		want                 gateCheck
		wantErr              bool
	}{
		{"unknown endpoint", "events", `{"type":"get_states"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"not an object", "ws", `[{"type":"get_states"}]`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"null on core", "core", `null`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"null on ws", "ws", `null`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"trailing data", "ws", `{"type":"get_states"} {"type":"config/area_registry/delete","area_id":"office"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"two typed, the first named", "ws", `{"type":"config/area_registry/delete","area_id":"office","message":{"type":"config/floor_registry/delete","floor_id":"up"}}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"not JSON", "core", `GET /api/states`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"read", "ws", `{"type":"get_states"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"core read", "core", `{"method":"get","path":"/api/states"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"previewed write", "ws", `{"type":"config/area_registry/create","name":"Office"}`, gateCheck{Verdict: gatePass, Kind: "write"}, false},
		{"typed write", "ws", `{"type":"config/area_registry/delete","area_id":"office"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"typed in an envelope", "ws", `{"message":{"type":"config/area_registry/delete","area_id":"office"},"collect_events":{"max_events":1}}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"typed envelope body", "ws", `{"type":"config/area_registry/delete","area_id":"office","message":{"type":"get_states"}}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete", Item: 1}, false},
		{"core delete, method as sent", "core", `{"method":" delete ","path":"/api/config/scene/config/evening"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"file delete", "files", `{"action":"delete_file","path":"scripts.yaml"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "file_delete"}, false},
		{"Supervisor write, not rated yet", "supervisor", `{"method":"POST","path":"/store/addons/core_ssh/install"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"service call, not rated yet", "ws", `{"type":"call_service","domain":"lock","service":"unlock","target":{"entity_id":"lock.front"}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"core service call, not rated yet", "core", `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":"lock.front"}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"config write, not rated yet", "core", `{"method":"POST","path":"/api/config/automation/config/x","body":{}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"webhook read", "core", `{"method":"GET","path":"/api/webhook/abc"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"phone event subscription", "ws", `{"message":{"type":"subscribe_events","event_type":"mobile_app_notification_action"},"collect_events":{"max_events":1}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"Supervisor read", "supervisor", `{"method":"GET","path":"/addons"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"unread access entity", "ws", `{"type":"config/entity_registry/update","entity_id":"lock.front","area_id":"hall"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "access_entity"}, true},
	} {
		got, err := gateVerdict(tc.endpoint, []byte(tc.body), noSession)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("%s: gateVerdict = %+v, %v; want %+v, err %v", tc.name, got, err, tc.want, tc.wantErr)
		}
	}
}
