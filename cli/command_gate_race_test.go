package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The Relay doesn't serialize its checks (gate-worker.ts): the worker rates
// requests from many devices at once. Every concurrent answer must be its
// own request's verdict. Under -race (ci.yml go-race) this covers what the
// checks share: sessions, the tier engine's tables, and the wording catalogs.
// The worker reads its Relay from its relay file, as runGateCommand loads it.
func TestGateWorkerConcurrentChecks(t *testing.T) {
	post := gateWorkerOnRelay(t, newFakeHA().reply)
	unlock := `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":["lock.front"]}}`
	cases := []struct{ path, endpoint, body, verdict, kind string }{
		{"/check", "core", unlock, "typed", "write"},
		{"/check", "core", `{"method":"POST","path":"/api/services/light/turn_on","body":{"entity_id":["light.kitchen"]}}`, "pass", "write"},
		{"/check", "core", `{"method":"GET","path":"/api/states"}`, "pass", "read"},
		{"/check", "ws", `{"type":"get_states"}`, "pass", "read"},
		{"/check", "ws", `{"type":"fire_event","event_type":"mobile_app_notification_action"}`, "refuse", "write"},
		{"/check", "ws", `{"type":"config/area_registry/delete","area_id":"office"}`, "typed", "write"},
		{"/check", "core", `{"method":"POST","path":"/api/events/mobile_app_notification_action","body":{}}`, "refuse", "write"},
		{"/check", "ws", `[1]`, "refuse", "write"},
		{"/ask", "core", unlock, "typed", ""},
	}
	var wg sync.WaitGroup
	for i := range 3 * len(cases) {
		tc := cases[i%len(cases)]
		wg.Add(1)
		go func() {
			defer wg.Done()
			in := map[string]any{"principal": map[string]any{"kind": "device", "device_id": "d1"}, "endpoint": tc.endpoint, "body_b64": b64(tc.body)}
			if tc.path == "/ask" {
				in = map[string]any{"device": "Laptop", "requests": []any{map[string]any{"endpoint": tc.endpoint, "body_b64": b64(tc.body)}}}
			}
			status, out := post(tc.path, gateTestSecret, in)
			title, _ := out["title"].(string)
			switch {
			case status != http.StatusOK || out["verdict"] != tc.verdict:
				t.Errorf("%s %s: HTTP %d %v, want %s", tc.path, tc.body, status, out, tc.verdict)
			case tc.path == "/check" && out["kind"] != tc.kind:
				t.Errorf("%s %s: %v, want kind %s", tc.path, tc.body, out, tc.kind)
			case tc.path == "/ask" && !strings.HasPrefix(title, "Unlock"):
				t.Errorf("%s %s: %v, want an unlock ask", tc.path, tc.body, out)
			}
		}()
	}
	wg.Wait()
}

// gateWorkerOnRelay: the worker's handler, reading its Relay (reply) from its
// relay file as runGateCommand loads it.
func gateWorkerOnRelay(t *testing.T, reply func(*http.Request, string, map[string]any) any) func(path, token string, in map[string]any) (int, map[string]any) {
	t.Helper()
	paths, _ := setupVerbRelayTest(t, reply)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "relay.json")
	if err := os.WriteFile(file, []byte(`{"relay_url":"`+cfg.RelayBaseURL+`","credential":"`+strings.Repeat("A", 43)+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if bridgeRelay, err = loadGateRelay(file); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bridgeRelay = nil })
	return gateTestServer(t, paths)
}

// Under the gate the worker's own reads pass the Relay's gate, which asks
// the same worker for a nested /check of each while the outer one is still
// open (here a UI thermostat's call, whose check also starts and aborts its
// options flow to read the heater). The fake Relay answers the worker's
// first read only after the nested check of exactly its bytes returned, so a
// worker that serialized its checks would stall both until the budget ran out.
func TestGateWorkerAnswersANestedCheck(t *testing.T) {
	_, f, flows := thermoHome(t)
	type answer struct {
		status int
		out    map[string]any
	}
	inner := make(chan answer, 1)
	var nested atomic.Bool
	var post func(path, token string, in map[string]any) (int, map[string]any)
	post = gateWorkerOnRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		if nested.CompareAndSwap(false, true) {
			raw, _ := io.ReadAll(r.Body)
			done := make(chan struct{})
			go func() {
				defer close(done)
				status, out := post("/check", gateTestSecret, map[string]any{"principal": map[string]any{"kind": "device", "device_id": "gate"}, "endpoint": endpoint, "body_b64": b64(string(raw))})
				inner <- answer{status, out}
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second): // answer the outer read anyway: a stall fails the test instead of hanging it
			}
		}
		return f.reply(r, endpoint, p)
	})
	start := time.Now()
	status, out := post("/check", gateTestSecret, map[string]any{"principal": map[string]any{"kind": "device", "device_id": "d1"}, "endpoint": "core",
		"body_b64": b64(`{"method":"POST","path":"/api/services/climate/set_temperature","body":{"entity_id":["climate.thermo"],"temperature":21}}`)})
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("the outer check took %s", took)
	}
	if status != http.StatusOK || out["verdict"] != "pass" || out["kind"] != "write" {
		t.Errorf("outer check: HTTP %d %v, want pass/write", status, out)
	}
	select {
	case got := <-inner:
		if got.status != http.StatusOK || got.out["verdict"] != "pass" || got.out["kind"] != "read" {
			t.Errorf("nested check of the worker's own read: HTTP %d %v, want pass/read", got.status, got.out)
		}
	default:
		t.Errorf("no nested check returned (options flows started %v)", flows.starts)
	}
	if len(flows.starts) == 0 || len(flows.aborts) != len(flows.starts) {
		t.Errorf("flows started %v, aborted %v; want each started flow aborted", flows.starts, flows.aborts)
	}
}
