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

// staleStoreServer: Home Assistant shows no NOVA App update until
// homeassistant.update_entity ran on the App's entity (and, with showsAfter,
// only from then on). checkStatus is Home Assistant's answer to that call.
type staleStoreServer struct {
	checks, installs, statesAfterCheck atomic.Int32
	installed                          atomic.Bool
}

func newStaleStoreServer(t *testing.T, showsAfter bool, checkStatus int) (*staleStoreServer, *httptest.Server) {
	t.Helper()
	s := &staleStoreServer{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			version := "0.2.6"
			if s.installed.Load() {
				version = "0.4.0"
			}
			fmt.Fprintf(w, `{"ok":true,"data":{"version":%q,"ha_ws_connected":true}}`, version)
		case "/core":
			var req struct {
				Method string          `json:"method"`
				Path   string          `json:"path"`
				Body   json.RawMessage `json:"body"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode core payload: %v", err)
			}
			entity := completedRelayUpdateEntity("update.nova_relay_update", "0.2.6")
			switch {
			case s.installed.Load():
				entity = completedRelayUpdateEntity("update.nova_relay_update", "0.4.0")
			case showsAfter && s.checks.Load() > 0:
				entity = relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
			}
			switch req.Path {
			case "/api/states", "/api/states/update.nova_relay_update":
				if s.checks.Load() > 0 {
					s.statesAfterCheck.Add(1)
				}
				if req.Path == "/api/states" {
					w.Write(statesEnvelope(t, []map[string]interface{}{entity}))
					return
				}
				body, _ := json.Marshal(map[string]interface{}{"ok": true, "data": map[string]interface{}{"status": 200, "body": entity}})
				w.Write(body)
			case "/api/services/homeassistant/update_entity":
				if req.Method != "POST" || string(req.Body) != `{"entity_id":"update.nova_relay_update"}` {
					t.Errorf("check = %s %s, want POST with only the App's entity", req.Method, req.Body)
				}
				s.checks.Add(1)
				fmt.Fprintf(w, `{"ok":true,"data":{"status":%d,"body":[]}}`, checkStatus)
			case "/api/services/update/install":
				s.installs.Add(1)
				s.installed.Store(true)
				w.Write([]byte(`{"ok":true,"data":{"status":200,"body":[]}}`))
			default:
				t.Errorf("unexpected core path %q", req.Path)
			}
		case "/ws":
			writeRegistryOrPingResponse(t, w, r, "update.nova_relay_update")
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	return s, server
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core":
			var req struct {
				Path string `json:"path"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			switch req.Path {
			case "/api/states", "/api/states/update.nova_relay_update":
				latest := "0.3.0" // below the floor (0.4.0)
				if checks.Load() > 0 {
					latest = "0.4.0"
				}
				entity := relayUpdateEntity("update.nova_relay_update", "NOVA Relay")
				entity["attributes"].(map[string]interface{})["latest_version"] = latest
				if req.Path == "/api/states" {
					w.Write(statesEnvelope(t, []map[string]interface{}{entity}))
					return
				}
				body, _ := json.Marshal(map[string]interface{}{"ok": true, "data": map[string]interface{}{"status": 200, "body": entity}})
				w.Write(body)
			case "/api/services/homeassistant/update_entity":
				checks.Add(1)
				w.Write([]byte(`{"ok":true,"data":{"status":200,"body":[]}}`))
			default:
				t.Errorf("unexpected core path %q", req.Path)
			}
		case "/ws":
			writeRegistryOrPingResponse(t, w, r, "update.nova_relay_update")
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	runGuidedRelayUpdate(paths, config{RelayBaseURL: server.URL}, "token", bufio.NewReader(strings.NewReader("n\n")), &out)
	if checks.Load() != 1 || !strings.Contains(out.String(), "HA NOVA installs the newest NOVA App (now v0.4.0)") {
		t.Fatalf("checks=%d; want the check and the newer App in the offer: %s", checks.Load(), out.String())
	}
}
