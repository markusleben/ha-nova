package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
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

// listensForServiceCalls: an automation reacts to every service call, so
// `ha-nova call update.install` asks for a typed confirmation (MAR-174).
func listensForServiceCalls(f *fakeHA) {
	f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service"}}, "actions": []any{step("lock.unlock", "lock.front")}})
	f.events = []map[string]any{{"event": "call_service", "listener_count": 1.0}}
}

// guidedVerbConfig saves what `ha-nova plan` and `ha-nova apply` read: the
// typed path runs them in process against the same Relay.
func guidedVerbConfig(t *testing.T, paths runtimePaths, relayURL string) {
	t.Helper()
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(t.TempDir(), "relay-token"))
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relayURL}); err != nil {
		t.Fatalf("saveConfig() error: %v", err)
	}
	if err := writeRelayAuthToken("token"); err != nil {
		t.Fatalf("writeRelayAuthToken() error: %v", err)
	}
}

// The check runs before the question. Nothing reacts: a preview, asked
// with a yes. Something reacts (or can't be read): the install asks for a
// typed confirmation where every typed change asks, never with the yes.
func TestGuidedRelayUpdateRatesBeforeAsking(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(f *fakeHA)
		typed bool
	}{
		{name: "nothing reacts: a preview installs after the yes", setup: func(*fakeHA) {}},
		{name: "an automation reacts to the App's update entity", typed: true, setup: func(f *fakeHA) {
			f.related["entity:update.nova_relay_update"] = []string{"automation.door"}
			f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "update.nova_relay_update"}}, "actions": []any{step("lock.unlock", "lock.front")}})
		}},
		{name: "an automation listens for service calls", typed: true, setup: listensForServiceCalls},
		{name: "the event listeners can't be read", typed: true, setup: func(f *fakeHA) { f.fail["/api/events"] = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := guidedUpdatePaths(t)
			shrinkGuidedUpdatePolling(t, time.Second)
			s, server := newStaleStoreServer(t, true, 200)
			s.checks.Store(1) // the App already shows the update: no store check
			tc.setup(s.ha)
			guidedVerbConfig(t, paths, server.URL)
			install := planCallRequest{Service: "update.install", EntityIDs: []string{"update.nova_relay_update"}, Data: map[string]any{"backup": true}, Skill: "ha-nova:updates"}
			rating := rateGuidedCall(paths, homeUpdateRoute(config{RelayBaseURL: server.URL}, "token"), install)
			if rating.typed() != tc.typed || !tc.typed && (rating.allows(tierOrdinary) || !rating.allows(tierPreview)) {
				t.Fatalf("setup: rating %+v, want typed=%t (else exactly preview)", rating, tc.typed)
			}
			var out bytes.Buffer
			verified := runGuidedRelayUpdate(paths, homeUpdateRoute(config{RelayBaseURL: server.URL}, "token"), bufio.NewReader(strings.NewReader("\n")), &out)
			s.ha.mu.Lock()
			posts := s.posts
			s.ha.mu.Unlock()
			text := out.String()
			check := strings.Index(text, "Checking what the update affects in your home")
			if check < 0 {
				t.Fatalf("no check before the question: %s", text)
			}
			if !tc.typed {
				if s.installs.Load() != 1 || !verified || strings.Index(text, "Update the NOVA App now?") < check {
					t.Fatalf("installs=%d verified=%t posts=%v; output: %s", s.installs.Load(), verified, posts, text)
				}
				return
			}
			if len(posts) != 0 || verified || strings.Contains(text, "Update the NOVA App now?") ||
				!strings.Contains(text, "This update needs your confirmation first, the same way an unlock does:") ||
				!strings.HasSuffix(text, "Code (Enter skips): Nothing was installed.\n") {
				t.Fatalf("want the typed confirmation and nothing sent after Enter (posts %v): %s", posts, text)
			}
		})
	}
}

// codeFromOutput types the code the guided update showed in out, changed by
// typo, once its prompt reads.
type codeFromOutput struct {
	out   *callBuffer
	typo  func(string) string
	typed bool
}

func (r *codeFromOutput) Read(p []byte) (int, error) {
	if r.typed {
		return 0, io.EOF
	}
	r.typed = true
	m := regexp.MustCompile(`To go ahead, type this code: ([0-9]{4})\n`).FindStringSubmatch(r.out.String())
	if m == nil {
		return 0, io.EOF
	}
	return copy(p, r.typo(m[1])+"\n"), nil
}

// A typed install sends only with its code, through apply: the right code
// installs once and the update is verified; a wrong one sends nothing.
func TestGuidedRelayUpdateTypedInstallsOnlyWithTheCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		typo func(string) string
		want int32
	}{
		{name: "the right code", typo: func(code string) string { return code }, want: 1},
		{name: "a wrong code", typo: wrongCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := guidedUpdatePaths(t)
			shrinkGuidedUpdatePolling(t, time.Second)
			s, server := newStaleStoreServer(t, true, 200)
			s.checks.Store(1)
			listensForServiceCalls(s.ha)
			guidedVerbConfig(t, paths, server.URL)
			out := &callBuffer{}
			verified := runGuidedRelayUpdate(paths, homeUpdateRoute(config{RelayBaseURL: server.URL}, "token"), bufio.NewReader(&codeFromOutput{out: out, typo: tc.typo}), out)
			text := out.String()
			if s.installs.Load() != tc.want || verified != (tc.want == 1) || strings.Contains(text, "Update the NOVA App now?") || strings.Contains(text, "I can see this code too") {
				t.Fatalf("installs=%d verified=%t; output: %s", s.installs.Load(), verified, text)
			}
			if tc.want == 0 && !strings.Contains(text, "Nothing was installed (the one-time code does not match") {
				t.Fatalf("a wrong code must say why nothing was installed: %s", text)
			}
			if tc.want == 1 && !strings.Contains(text, "The NOVA App is updated and works: v0.4.0.") {
				t.Fatalf("want the verified update: %s", text)
			}
			if plans, _ := filepath.Glob(filepath.Join(planDir(paths), "p_*.json")); tc.want == 1 && len(plans) != 0 {
				t.Fatalf("the applied plan stays behind: %v", plans)
			}
		})
	}
}

// A check that fails installs nothing, says why, and never asks.
func TestGuidedRelayUpdateFailedCheckNeverAsks(t *testing.T) {
	paths := guidedUpdatePaths(t)
	s, server := newStaleStoreServer(t, true, 200)
	s.checks.Store(1)
	s.ha.services = s.ha.defaultServices() // Home Assistant names no update.install: the call can't be planned
	var out bytes.Buffer
	if runGuidedRelayUpdate(paths, homeUpdateRoute(config{RelayBaseURL: server.URL}, "token"), bufio.NewReader(strings.NewReader("\n")), &out) || s.installs.Load() != 0 {
		t.Fatalf("installed after a failed check: %s", out.String())
	}
	text := out.String()
	if strings.Contains(text, "Update the NOVA App now?") || !strings.Contains(text, "HA NOVA couldn't check everything the update affects in your home, so it installs nothing (") ||
		!strings.HasSuffix(text, relayUpdateManualPath+"\n") {
		t.Fatalf("want the plain reason and the manual path, no question: %s", text)
	}
}

// A window plan asks in the window (the phone too, where one is linked):
// Confirm installs once, Cancel sends nothing, and a window that can't open
// hands over to a code typed here. Home Assistant turning the install down
// skips the wait.
func TestGuidedRelayUpdateTypedAsksInTheWindow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		answer   windowAnswer
		rejected bool
		want     int32
		says     string
	}{
		{name: "confirmed", answer: windowConfirmed, want: 1, says: "The NOVA App is updated and works: v0.4.0."},
		{name: "cancelled", answer: windowCancelled, says: "Nothing was installed (the confirmation window was cancelled"},
		{name: "the window can't open: a code instead", answer: windowFailed, want: 1, says: "The NOVA App is updated and works: v0.4.0."},
		{name: "Home Assistant turns it down", answer: windowConfirmed, rejected: true, says: "Home Assistant didn't start the update.\n" + relayUpdateManualPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := guidedUpdatePaths(t)
			shrinkGuidedUpdatePolling(t, time.Second)
			s, server := newStaleStoreServer(t, true, 200)
			s.checks.Store(1)
			listensForServiceCalls(s.ha)
			if tc.rejected {
				answers := s.ha.extra
				s.ha.extra = func(endpoint string, p map[string]any) (any, bool) {
					if endpoint == "core" && p["path"] == "/api/services/update/install" {
						return coreReply(400, map[string]any{"message": "no"}), true
					}
					return answers(endpoint, p)
				}
			}
			guidedVerbConfig(t, paths, server.URL)
			w := useWindow(t, tc.answer)
			out := &callBuffer{}
			verified := runGuidedRelayUpdate(paths, homeUpdateRoute(config{RelayBaseURL: server.URL}, "token"), bufio.NewReader(&codeFromOutput{out: out, typo: func(code string) string { return code }}), out)
			text := out.String()
			if s.installs.Load() != tc.want || verified != (tc.want == 1) || len(w.messages) != 1 || !strings.Contains(text, tc.says) || strings.Contains(text, "Update the NOVA App now?") {
				t.Fatalf("installs=%d verified=%t windows=%d; output: %s", s.installs.Load(), verified, len(w.messages), text)
			}
		})
	}
}
