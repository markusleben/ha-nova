package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func shrinkRelayStoreCheck(t *testing.T, timeout time.Duration) {
	t.Helper()
	oldInterval, oldTimeout := relayStoreCheckPollInterval, relayStoreCheckTimeout
	relayStoreCheckPollInterval = time.Millisecond
	relayStoreCheckTimeout = timeout
	t.Cleanup(func() {
		relayStoreCheckPollInterval = oldInterval
		relayStoreCheckTimeout = oldTimeout
	})
}

// fakeHARelay serves f over the Relay's /health, /core, and /ws: f.extra
// answers first (the App's own states and services), every other read (the
// tier engine's) is f's.
func fakeHARelay(t *testing.T, f *fakeHA) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload) // /health has no body
		serveFakeHA(w, r, f, payload)
	}))
	t.Cleanup(server.Close)
	return server
}

func serveFakeHA(w http.ResponseWriter, r *http.Request, f *fakeHA, payload map[string]any) {
	data := f.reply(r, strings.TrimPrefix(r.URL.Path, "/"), payload)
	if failure, isFailure := data.(fakeRelayFailure); isFailure {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"code": failure.code, "message": failure.message}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

// staleStoreServer: Home Assistant shows no NOVA App update until
// homeassistant.update_entity ran on the App's entity (and, with showsAfter,
// only from then on). checkStatus is Home Assistant's answer to that call;
// ha is the rest of the home the tier engine reads.
type staleStoreServer struct {
	checks, installs, statesAfterCheck atomic.Int32
	installed                          atomic.Bool
	ha                                 *fakeHA
	posts                              []string // every POSTed core path, under ha.mu
}

func newStaleStoreServer(t *testing.T, showsAfter bool, checkStatus int) (*staleStoreServer, *httptest.Server) {
	t.Helper()
	s := &staleStoreServer{ha: newFakeHA()}
	s.ha.ent("update.nova_relay_update", "hassio", "off", nil) // its compact registry row; the states below win
	s.ha.full = []map[string]any{novaRegistryEntry("update.nova_relay_update")}
	s.ha.services = appUpdateHA().services
	s.ha.extra = func(endpoint string, p map[string]any) (any, bool) {
		entity := completedRelayUpdateEntity("update.nova_relay_update", "0.2.6")
		switch {
		case s.installed.Load():
			entity = completedRelayUpdateEntity("update.nova_relay_update", "0.4.0")
		case showsAfter && s.checks.Load() > 0:
			entity = relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
		}
		switch {
		case endpoint == "health":
			version := "0.2.6"
			if s.installed.Load() {
				version = "0.4.0"
			}
			return map[string]any{"version": version, "ha_ws_connected": true}, true
		case endpoint == "ws" && p["type"] == "ping":
			return map[string]any{"type": "pong"}, true
		case endpoint != "core":
			return nil, false
		}
		path := fmt.Sprint(p["path"])
		if p["method"] == "POST" {
			s.posts = append(s.posts, path)
		}
		switch path {
		case "/api/states", "/api/states/update.nova_relay_update":
			if s.checks.Load() > 0 {
				s.statesAfterCheck.Add(1)
			}
			if path == "/api/states" {
				all := []any{entity}
				for id, st := range s.ha.states {
					if id != "update.nova_relay_update" {
						all = append(all, st)
					}
				}
				return coreReply(200, all), true
			}
			return coreReply(200, entity), true
		case "/api/services/homeassistant/update_entity":
			if body, _ := json.Marshal(p["body"]); p["method"] != "POST" || string(body) != `{"entity_id":"update.nova_relay_update"}` {
				t.Errorf("check = %s %s, want POST with only the App's entity", p["method"], body)
			}
			s.checks.Add(1)
			return coreReply(checkStatus, []any{}), true
		case "/api/services/update/install":
			s.installs.Add(1)
			s.installed.Store(true)
			return coreReply(200, []any{}), true
		}
		if p["method"] != "GET" {
			t.Errorf("unexpected core %s %q", p["method"], path)
		}
		return nil, false
	}
	return s, fakeHARelay(t, s.ha)
}

func TestGuidedRelayUpdateChecksHomeAssistantBeforeOffering(t *testing.T) {
	for _, tc := range []struct {
		name, input  string
		wantInstalls int32
	}{
		{name: "yes installs", input: "\n", wantInstalls: 1},
		{name: "no installs nothing", input: "n\n", wantInstalls: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := guidedUpdatePaths(t)
			shrinkGuidedUpdatePolling(t, time.Second)
			shrinkRelayStoreCheck(t, time.Second)
			s, server := newStaleStoreServer(t, true, 200)
			var out bytes.Buffer
			verified := runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader(tc.input)), &out)
			if s.checks.Load() != 1 || s.installs.Load() != tc.wantInstalls || verified != (tc.wantInstalls == 1) {
				t.Fatalf("checks=%d installs=%d verified=%t; output: %s", s.checks.Load(), s.installs.Load(), verified, out.String())
			}
			text := out.String()
			check := strings.Index(text, "Checking Home Assistant for a newer NOVA App...")
			preview := strings.Index(text, "HA NOVA installs the newest NOVA App (now v0.4.0)")
			question := strings.Index(text, "Update the NOVA App now?")
			if check < 0 || preview < check || question < preview {
				t.Fatalf("want check, then preview, then question: %s", text)
			}
		})
	}
}

func TestGuidedRelayUpdateCheckFindsNothingNamesCheckForUpdates(t *testing.T) {
	for _, tc := range []struct {
		name            string
		status          int
		wantStateChecks bool
	}{
		{name: "no newer App after the check", status: 200, wantStateChecks: true},
		{name: "Home Assistant refuses the check", status: 403, wantStateChecks: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := guidedUpdatePaths(t)
			shrinkRelayStoreCheck(t, 30*time.Millisecond)
			s, server := newStaleStoreServer(t, false, tc.status)
			var out bytes.Buffer
			if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("\n")), &out) {
				t.Fatalf("nothing to install must not report success")
			}
			if s.checks.Load() != 1 || s.installs.Load() != 0 || (s.statesAfterCheck.Load() > 0) != tc.wantStateChecks {
				t.Fatalf("checks=%d installs=%d state reads after check=%d; output: %s", s.checks.Load(), s.installs.Load(), s.statesAfterCheck.Load(), out.String())
			}
			if !strings.Contains(out.String(), "Nothing was installed.\n"+relayUpdateCheckManualPath) ||
				strings.Contains(out.String(), "Update the NOVA App now?") {
				t.Fatalf("want the Check for updates path and no offer: %s", out.String())
			}
		})
	}
}

func TestRelayStoreCheckSkipsAnUpdateAlreadyInstalling(t *testing.T) {
	paths := guidedUpdatePaths(t)
	installing := completedRelayUpdateEntity("update.nova_relay_update", "0.2.6")
	installing["attributes"].(map[string]interface{})["in_progress"] = true
	var checks atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core":
			var req struct {
				Path string `json:"path"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Path != "/api/states" {
				checks.Add(1)
			}
			w.Write(statesEnvelope(t, []map[string]interface{}{installing}))
		case "/ws":
			writeRegistryOrPingResponse(t, w, r, "update.nova_relay_update")
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("\n")), &out) || checks.Load() != 0 {
		t.Fatalf("an update in progress must be neither checked nor installed (calls %d): %s", checks.Load(), out.String())
	}
	if !strings.Contains(out.String(), relayUpdateManualPath) || strings.Contains(out.String(), "Checking Home Assistant") {
		t.Fatalf("want the plain manual path without a check: %s", out.String())
	}
}

// Home Assistant offers an App, but one below what this HA NOVA needs: the
// check runs, and the offer names the newer App it found.
func TestRelayStoreCheckLooksPastAnOfferBelowTheFloor(t *testing.T) {
	paths := guidedUpdatePaths(t)
	shrinkRelayStoreCheck(t, time.Second)
	var checks atomic.Int32
	f := newFakeHA()
	f.ent("update.nova_relay_update", "hassio", "on", nil)
	f.full = []map[string]any{novaRegistryEntry("update.nova_relay_update")}
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		if endpoint != "core" {
			return nil, false
		}
		switch p["path"] {
		case "/api/states", "/api/states/update.nova_relay_update":
			latest := "0.3.0" // below the floor (0.4.0)
			if checks.Load() > 0 {
				latest = "0.4.0"
			}
			entity := relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
			entity["attributes"].(map[string]interface{})["latest_version"] = latest
			if p["path"] == "/api/states" {
				return coreReply(200, []any{entity}), true
			}
			return coreReply(200, entity), true
		case "/api/services/homeassistant/update_entity":
			checks.Add(1)
			return coreReply(200, []any{}), true
		}
		if p["method"] != "GET" {
			t.Errorf("unexpected core %s %q", p["method"], p["path"])
		}
		return nil, false
	}
	server := fakeHARelay(t, f)
	var out bytes.Buffer
	runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("n\n")), &out)
	if checks.Load() != 1 || !strings.Contains(out.String(), "HA NOVA installs the newest NOVA App (now v0.4.0)") {
		t.Fatalf("checks=%d; want the check and the newer App in the offer: %s", checks.Load(), out.String())
	}
}

// The refresh is a service call like any other: when something in the home
// reacts to it, or the engine can't tell, HA NOVA does not send it and
// points to Home Assistant's own Check for updates instead.
func TestRelayStoreCheckSendsNothingAnAutomationReactsTo(t *testing.T) {
	unlocks := []any{step("lock.unlock", "lock.front")}
	for _, tc := range []struct {
		name  string
		setup func(f *fakeHA)
	}{
		{name: "an automation listens for service calls", setup: func(f *fakeHA) {
			f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service"}}, "actions": unlocks})
			f.events = []map[string]any{{"event": "call_service", "listener_count": 1.0}}
		}},
		{name: "an automation reacts to the App's update entity", setup: func(f *fakeHA) {
			f.related["entity:update.nova_relay_update"] = []string{"automation.door"}
			f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "update.nova_relay_update"}}, "actions": unlocks})
		}},
		{name: "the event listeners can't be read", setup: func(f *fakeHA) { f.fail["/api/events"] = true }},
		{name: "the registry can't be read", setup: func(f *fakeHA) { f.fail["config/entity_registry/list_for_display"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := guidedUpdatePaths(t)
			shrinkRelayStoreCheck(t, 30*time.Millisecond)
			s, server := newStaleStoreServer(t, true, 200)
			tc.setup(s.ha)
			var out bytes.Buffer
			if runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("\n")), &out) {
				t.Fatalf("nothing to install must not report success")
			}
			s.ha.mu.Lock()
			posts := s.posts
			s.ha.mu.Unlock()
			if len(posts) != 0 || s.checks.Load() != 0 || s.installs.Load() != 0 {
				t.Fatalf("posts=%v checks=%d installs=%d; output: %s", posts, s.checks.Load(), s.installs.Load(), out.String())
			}
			if !strings.Contains(out.String(), "Nothing was installed.\n"+relayUpdateCheckManualPath) ||
				strings.Contains(out.String(), "Update the NOVA App now?") {
				t.Fatalf("want the Check for updates path and no offer: %s", out.String())
			}
		})
	}
}
