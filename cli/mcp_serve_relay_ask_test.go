package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A chat app on a standalone NOVA Relay container (mcp_serve_relay_ask.go):
// its typed changes ask the phone linked to the container through the
// Relay, and wait for it without the verb lock.

// containerServe: the bridge on the plain token of a gated Relay (no
// bridge credential), users A (an administrator) and B (Use the home).
func containerServe(t *testing.T, g *fakeGate) *serveHarness {
	t.Helper()
	f, home := newFakeHA(), newFamilyHome() // its phones: a link on the bridge's host could be moved
	home.users = append(home.users, map[string]any{"id": userA, "name": "A", "is_active": true, "group_ids": []string{"system-admin"}},
		map[string]any{"id": userB, "name": "B", "is_active": true, "group_ids": []string{"system-admin"}})
	home.install(t, f)
	return serveOn(t, gatedHarness(t, f, g), `{"allowed_users":["`+userA+`","`+userB+`"],"roles":1,"levels":{"`+userB+`":"use"}}`)
}

func relayPlanFor(t *testing.T, s *serveHarness, user string) string {
	t.Helper()
	env := s.tool(user, "ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	plan, _ := env["plan"].(map[string]any)
	if plan["channel"] != channelRelay || plan["code"] != nil || !strings.Contains(env["say"].(string), confirmT("en", "say.relay_ask_container")) ||
		!strings.Contains(env["next_step"].(string), "the phone linked to their NOVA Relay container") {
		t.Fatalf("relay plan: %v", env)
	}
	return plan["id"].(string)
}

// waitingOnRelay calls ha_apply until it answers that the container's phone is asked.
func waitingOnRelay(t *testing.T, s *serveHarness, user, id string) {
	t.Helper()
	waitFor(t, "the ask on the container's phone", func() bool {
		env := s.tool(user, "ha_apply", map[string]any{"plan_id": id})
		return env["status"] == "waiting_for_phone" && env["say"] == confirmT("en", "say.relay_container_waiting")
	})
}

// A Use-the-home user's unlock asks the container's phone; while it waits,
// other chat users change things (no BUSY), and a call again joins the same
// ask. The tap sends exactly the approved bytes once; before it, nothing.
// The levels still bound that user.
func TestServeContainerAsksItsLinkedPhone(t *testing.T) {
	g := &fakeGate{release: make(chan struct{})}
	s := containerServe(t, g)
	if env := s.tool(userB, "ha_plan", map[string]any{"kind": "write", "domain": "script", "id": "s1", "draft": arrival("light.turn_off")}); envCode(env) != "CHAT_LEVEL" {
		t.Fatalf("Use the home writes a script: %v", env)
	}
	id := relayPlanFor(t, s, userB)
	waitingOnRelay(t, s, userB, id)
	start := time.Now()
	if env := s.tool(userA, "ha_call", map[string]any{"service": "light.turn_on", "entity_ids": []string{"light.kitchen"}}); env["status"] != "verified" || time.Since(start) > 3*time.Second {
		t.Fatalf("another user's change during the ask: %v", env)
	}
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "waiting_for_phone" || !strings.Contains(nextOf(env), "call ha_apply again with plan_id "+id) {
		t.Fatalf("joined call: %v", env)
	}
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": relayPlanFor(t, s, userB)}); envCode(env) != "CONFIRM_PENDING" {
		t.Fatalf("a second ask of the same user: %v", env)
	}
	asks, _, sends := g.snapshot()
	for _, send := range sends {
		if send.key == gateUnlock {
			t.Fatalf("sent before the OK: %+v", sends)
		}
	}
	if len(asks) != 1 || len(asks[0]) != 1 || asks[0][0] != gateUnlock {
		t.Fatalf("asks %q", asks)
	}
	close(g.release)
	var env map[string]any
	waitFor(t, "the outcome", func() bool {
		env = s.tool(userB, "ha_apply", map[string]any{"plan_id": id})
		return env["status"] != "waiting_for_phone" && env["status"] != "applying"
	})
	asks, _, sends = g.snapshot()
	unlocks := 0
	for _, send := range sends {
		if send.key == gateUnlock {
			unlocks++
			if send.grant != "grant-ask1" {
				t.Fatalf("unlock without its grant: %+v", send)
			}
		}
	}
	if env["status"] != "verified" || len(asks) != 1 || unlocks != 1 {
		t.Fatalf("outcome %v; asks %q sends %+v", env, asks, sends)
	}
}

// The clear refusals, each sending nothing: no phone linked to the
// container (and the bridge host's own phone is never moved there), a
// paused phone, an ask another token holder replaced, one nobody answered
// in time or the phone couldn't get (never pointing to a NOVA page the
// container hasn't), and an OK that came after the owner set chat apps to
// read only.
func TestServeContainerAskRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		g        *fakeGate
		code     string
		next     string
		readOnly bool
	}{
		"no phone":  {&fakeGate{phones: true}, "CONFIRM_UNAVAILABLE", gateChatNoPhoneNext, false},
		"paused":    {&fakeGate{openAnswer: fakeRelayStatus{429, "GATE_PAUSED", "Confirmations on the phone are paused"}}, "GATE_PAUSED", gateChatNext["GATE_PAUSED"], false},
		"replaced":  {&fakeGate{state: "cancelled"}, "CONFIRM_DECLINED", gateChatCancelledNext, false},
		"expired":   {&fakeGate{state: "expired"}, "CONFIRM_CANCELLED", gateChatEnded("expired").next, false},
		"lost":      {&fakeGate{state: "unavailable"}, "CONFIRM_UNAVAILABLE", gateChatEnded("unavailable").next, false},
		"read only": {&fakeGate{release: make(chan struct{})}, "READ_ONLY", "", true},
	} {
		t.Run(name, func(t *testing.T) {
			s := containerServe(t, c.g)
			seedLink(t, s.writeHarness, &annaLink)
			id := relayPlanFor(t, s, userA)
			if c.readOnly {
				waitingOnRelay(t, s, userA, id)
				if err := os.WriteFile(filepath.Join(s.data, "chat-apps.json"), []byte(`{"allowed_users":["`+userA+`"],"read_only":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
				close(c.g.release)
			}
			var env map[string]any
			waitFor(t, "the refusal", func() bool {
				env = s.tool(userA, "ha_apply", map[string]any{"plan_id": id})
				return env["status"] != "waiting_for_phone" && env["status"] != "applying"
			})
			_, ops, sends := c.g.snapshot()
			if envCode(env) != c.code || c.next != "" && nextOf(env) != c.next || strings.Contains(strings.Join(ops, ","), "link") || len(c.g.links) != 0 {
				t.Fatalf("%v; ops %v links %v", env, ops, c.g.links)
			}
			for _, send := range sends {
				if send.key == gateUnlock {
					t.Fatalf("sent: %+v", sends)
				}
			}
		})
	}
}
