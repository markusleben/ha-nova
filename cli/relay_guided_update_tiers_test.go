package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// withTierReads answers the tier engine's reads from f in front of a test's
// own Relay, until the first write: a read verb's request (I1 allowlist)
// that next doesn't serve itself. Everything else, and every request after
// a write, reaches next and its checks.
func withTierReads(f *fakeHA, next http.HandlerFunc) http.Handler {
	var wrote atomic.Bool
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		endpoint := strings.TrimPrefix(r.URL.Path, "/")
		own := p["path"] == "/api/states" || p["type"] == "config/entity_registry/list"
		if p["method"] == "POST" {
			wrote.Store(true)
		}
		if wrote.Load() || own || checkReadOnlyRequest(endpoint, p) != nil {
			next(w, r)
			return
		}
		serveFakeHA(w, r, f, p)
	})
}

// The terminal yes stands for a preview: when something in the home reacts
// to the install, so that `ha-nova call update.install` would ask for a
// typed confirmation, the guided update sends nothing.
func TestGuidedRelayUpdateInstallsOnlyAtPreview(t *testing.T) {
	for _, tc := range []struct {
		name         string
		setup        func(f *fakeHA)
		wantInstalls int32
	}{
		{name: "nothing reacts: a preview installs after the yes", setup: func(*fakeHA) {}, wantInstalls: 1},
		{name: "an automation reacts to the App's update entity", setup: func(f *fakeHA) {
			f.related["entity:update.nova_relay_update"] = []string{"automation.door"}
			f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "update.nova_relay_update"}}, "actions": []any{step("lock.unlock", "lock.front")}})
		}},
		{name: "an automation listens for service calls", setup: func(f *fakeHA) {
			f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service"}}, "actions": []any{step("lock.unlock", "lock.front")}})
			f.events = []map[string]any{{"event": "call_service", "listener_count": 1.0}}
		}},
		{name: "the event listeners can't be read", setup: func(f *fakeHA) { f.fail["/api/events"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := guidedUpdatePaths(t)
			shrinkGuidedUpdatePolling(t, time.Second)
			s, server := newStaleStoreServer(t, true, 200)
			s.checks.Store(1) // the App already shows the update: no store check
			tc.setup(s.ha)
			install := planCallRequest{Service: "update.install", EntityIDs: []string{"update.nova_relay_update"}, Data: map[string]any{"backup": true}, Skill: "ha-nova:updates"}
			cfg := config{RelayBaseURL: server.URL}
			if tc.wantInstalls == 1 && (relayGuidedCallAllowed(paths, homeUpdateRoute(cfg, "token"), install, tierOrdinary) || !relayGuidedCallAllowed(paths, homeUpdateRoute(cfg, "token"), install, tierPreview)) {
				t.Fatalf("setup: the install must classify as exactly preview")
			}
			var out bytes.Buffer
			verified := runGuidedRelayUpdate(paths, homeUpdateRoute(cfg, "token"), bufio.NewReader(strings.NewReader("\n")), &out)
			s.ha.mu.Lock()
			posts := s.posts
			s.ha.mu.Unlock()
			if s.installs.Load() != tc.wantInstalls || verified != (tc.wantInstalls == 1) || s.checks.Load() != 1 {
				t.Fatalf("installs=%d verified=%t posts=%v; output: %s", s.installs.Load(), verified, posts, out.String())
			}
			if tc.wantInstalls == 1 {
				return
			}
			if len(posts) != 0 || !strings.Contains(out.String(), "Update the NOVA App now?") ||
				!strings.HasSuffix(out.String(), "HA NOVA can't start the NOVA App update from here. Nothing was installed.\n"+relayUpdateManualPath+"\n") ||
				strings.Contains(out.String(), "Updating the NOVA App") {
				t.Fatalf("want the question, then no POST and the manual path (posts %v): %s", posts, out.String())
			}
		})
	}
}
