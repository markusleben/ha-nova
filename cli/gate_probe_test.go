package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"
)

// The App's own check may send exactly a helper options read's two requests
// (MAR-153): kind probe, only for a passing start of an allowlisted,
// built-in entry with options (read live) and a bodyless abort by flow id.
func TestGateProbeKind(t *testing.T) {
	start := `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"ETHERMO"}}`
	for _, tc := range []struct {
		name, body string
		setup      func(*fakeHA)
		verdict    gateDecision
		kind       string
	}{
		{"start", start, nil, gatePass, "probe"},
		{"abort", `{"method":"DELETE","path":"/api/config/config_entries/options/flow/fethermo"}`, nil, gatePass, "probe"},
		{"a lower-case method, as the Relay sends it upper-cased", `{"method":"post","path":"/api/config/config_entries/options/flow","body":{"handler":"ETHERMO"}}`, nil, gatePass, "probe"},
		{"a step (the save)", `{"method":"POST","path":"/api/config/config_entries/options/flow/fethermo","body":{"heater":"switch.heater"}}`, nil, gatePass, "write"},
		{"another domain's entry", `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"ESHELLY"}}`, nil, gatePass, "write"},
		{"an unknown entry", `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"EMISSING"}}`, nil, gatePass, "write"},
		{"a subentry flow", `{"method":"POST","path":"/api/config/config_entries/subentries/flow","body":{"handler":["ETHERMO","heat"]}}`, nil, gatePass, "write"},
		{"a config flow", `{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"generic_thermostat"}}`, nil, "", "write"},
		{"an extra body key", `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"ETHERMO","show_advanced_options":true}}`, nil, gatePass, "write"},
		{"an extra key", `{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"ETHERMO"},"headers":{}}`, nil, "", "write"},
		{"a query", `{"method":"POST","path":"/api/config/config_entries/options/flow?x=1","body":{"handler":"ETHERMO"}}`, nil, "", "write"},
		{"an abort with a body", `{"method":"DELETE","path":"/api/config/config_entries/options/flow/fethermo","body":{}}`, nil, "", "write"},
		{"an abort of no flow id", `{"method":"DELETE","path":"/api/config/config_entries/options/flow/f/x"}`, nil, "", "write"},
		{"an entry without options", start, func(f *fakeHA) { f.entries[len(f.entries)-2]["supports_options"] = false }, gatePass, "write"},
		{"a custom integration on the domain", start, func(f *fakeHA) {
			f.manifests = slices.DeleteFunc(f.manifests, func(m map[string]any) bool { return m["domain"] == "generic_thermostat" })
			f.manifests = append(f.manifests, map[string]any{"domain": "generic_thermostat", "is_built_in": false})
		}, gatePass, "write"},
		{"an unreadable entry list", start, func(f *fakeHA) { f.fail["config_entries/get"] = true }, "", "write"},
		{"a reload that unlocks", start, func(f *fakeHA) {
			f.automation("thermo_unlock", "thermo_unlock", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "climate.thermo"}}, "actions": []any{step("lock.unlock", "lock.front")}})
			f.related["entity:climate.thermo"] = []string{"automation.thermo_unlock"}
		}, gateTyped, "write"},
	} {
		h, f, flows := thermoHome(t)
		if tc.setup != nil {
			tc.setup(f)
		}
		check, err := gateVerdict("core", []byte(tc.body), func() (*relaySession, error) { return newRelaySession(h.paths, verbOptions{}) })
		if err != nil || check.Kind != tc.kind || tc.verdict != "" && check.Verdict != tc.verdict || len(flows.starts)+len(flows.aborts) != 0 {
			t.Errorf("%s: %+v %v, flows %v %v; want %s %s, no flow", tc.name, check, err, flows.starts, flows.aborts, tc.verdict, tc.kind)
		}
	}
	if check, _ := gateVerdict("core", []byte(start), func() (*relaySession, error) { return nil, http.ErrServerClosed }); check.Kind != "write" {
		t.Errorf("no session: %+v, want write", check)
	}
}

// A check of the start never starts a flow itself: its session refuses the
// helper read before sending anything, whatever the listener scan decides.
func TestGateProbeStartCheckStartsNoFlow(t *testing.T) {
	h, _, flows := thermoHome(t)
	var s *relaySession
	if _, err := gateVerdict("core", []byte(`{"method":"POST","path":"/api/config/config_entries/options/flow","body":{"handler":"ETHERMO"}}`), func() (*relaySession, error) {
		opened, err := newRelaySession(h.paths, verbOptions{})
		s = opened
		return opened, err
	}); err != nil || s == nil {
		t.Fatalf("err %v, session %v", err, s)
	}
	defer s.close()
	if _, err := s.read(helperOptionsEndpoint, map[string]any{"entry_id": "ETHERMO"}); err == nil || len(flows.starts) != 0 {
		t.Errorf("the start's own session read the helper: err %v, flows started %v", err, flows.starts)
	}
}

// A start goes out only while enough of the read budget is left to start
// and abort it.
func TestHelperOptionsNeedTheirBudget(t *testing.T) {
	h, _, flows := thermoHome(t)
	reserve := helperStartReserve
	helperStartReserve = time.Hour
	t.Cleanup(func() { helperStartReserve = reserve })
	s, err := newRelaySession(h.paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if _, err := s.read(helperOptionsEndpoint, map[string]any{"entry_id": "ETHERMO"}); err == nil || len(flows.starts) != 0 {
		t.Errorf("err %v, flows started %v; want no start", err, flows.starts)
	}
}

// The worker's path on a gated App: a fake Relay forwards the check's own
// requests only on pass with kind read or probe, as gate.ts does for the
// App's gate principal. Beside a UI thermostat and an unlock listener on
// climate calls, an everyday light call passes (it was typed in 0.30, the
// start refused); a call on the thermostat still asks.
func TestGateProbeThroughTheGatedRelay(t *testing.T) {
	_, f, flows := thermoHome(t)
	listen(f, "climate_unlock", map[string]any{"domain": "climate"}, step("lock.unlock", "lock.front"))
	var mu sync.Mutex
	var paths runtimePaths
	var refused []string
	session := func() (*relaySession, error) {
		mu.Lock()
		defer mu.Unlock()
		return newRelaySession(paths, verbOptions{})
	}
	gated, _ := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		raw, _ := json.Marshal(p)
		if check, err := gateVerdict(endpoint, raw, session); err != nil || check.Verdict != gatePass || check.Kind != "read" && check.Kind != "probe" {
			mu.Lock()
			refused = append(refused, endpoint+" "+string(raw))
			mu.Unlock()
			return fakeRelayStatus{http.StatusUnprocessableEntity, "GATE_REFUSED", "refused"}
		}
		return f.reply(r, endpoint, p)
	}, true)
	mu.Lock()
	paths = gated
	mu.Unlock()
	for _, tc := range []struct {
		body string
		want gateDecision
	}{
		{`{"method":"POST","path":"/api/services/light/turn_off","body":{"entity_id":"light.kitchen"}}`, gatePass},
		{`{"method":"POST","path":"/api/services/climate/set_temperature","body":{"entity_id":"climate.thermo","temperature":21}}`, gateTyped},
	} {
		flows.starts, flows.aborts = nil, nil
		check, err := gateVerdict("core", []byte(tc.body), session)
		mu.Lock()
		got := refused
		refused = nil
		mu.Unlock()
		if err != nil || check.Verdict != tc.want || len(got) != 0 || len(flows.starts) == 0 || len(flows.aborts) != len(flows.starts) {
			t.Errorf("%s: %+v %v, refused %v, flows started %v aborted %v; want %s, the helper read and aborted", tc.body, check, err, got, flows.starts, flows.aborts, tc.want)
		}
	}
}
