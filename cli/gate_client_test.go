package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The CLI on a gated App (gate_client.go). fakeGate is the Relay's ask
// route (nova/src/http/gate-asks.ts) and its hold on typed requests.

type gateSend struct {
	key, grant string // "endpoint body" as sent, and the grant it carried
}

type fakeGate struct {
	mu         sync.Mutex
	asks       [][]string // each open's requests as "endpoint body"
	ops        []string   // open, wait, cancel in order
	sends      []gateSend // every config or service write
	notNeeded  bool
	openAnswer any               // the open's answer instead of an ask (a fakeRelayStatus)
	state      string            // how a wait ends; "" approved
	endFor     map[string]string // how one ask's wait ends (by ask id), before state
	release    chan struct{}     // non-nil: a wait answers once it is closed
	onWait     func()
	openDelay  time.Duration           // the Relay's worker prepares an open this long
	hold       func(send gateSend) any // a write's answer instead of Home Assistant's (nil: Home Assistant's)
	// The phone channel (gate_phone_move_test.go): with phones, an open
	// offers channels plus "phone" while a link is held and not paused,
	// and none at all is 409 GATE_NO_CHANNEL.
	phones      bool
	channels    []string
	link        map[string]any // the link the Relay holds
	paused      bool
	links       []map[string]any // every link sent to the phone route
	phoneAnswer any              // the phone route's answer instead of a link ask
	linkEnd     string           // how a link ask's wait ends ("": linked; "waiting": not yet; "lost": no answer)
	wsHeld      bool             // WS writes are sends too (hold sees them)
}

func (g *fakeGate) snapshot() (asks [][]string, ops []string, sends []gateSend) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([][]string(nil), g.asks...), append([]string(nil), g.ops...), append([]gateSend(nil), g.sends...)
}

func (g *fakeGate) op(name string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.ops = append(g.ops, name)
}

// ask answers the ask route as the Relay does.
func (g *fakeGate) ask(ctx context.Context, p map[string]any) any {
	switch {
	case p["requests"] != nil:
		var keys []string
		for _, item := range p["requests"].([]any) {
			request := item.(map[string]any)
			body, _ := base64.StdEncoding.DecodeString(fmt.Sprint(request["body_b64"]))
			keys = append(keys, fmt.Sprint(request["endpoint"], " ", string(body)))
		}
		g.mu.Lock()
		g.asks, g.ops = append(g.asks, keys), append(g.ops, "open")
		id := fmt.Sprint("ask", len(g.asks))
		g.mu.Unlock()
		select {
		case <-time.After(g.openDelay):
		case <-ctx.Done():
			return nil
		}
		if g.openAnswer != nil {
			return g.openAnswer
		}
		if g.phones {
			g.mu.Lock()
			defer g.mu.Unlock()
			channels := append([]string(nil), g.channels...)
			if g.link != nil && !g.paused {
				channels = append(channels, "phone")
			}
			if len(channels) == 0 {
				return fakeRelayStatus{409, "GATE_NO_CHANNEL", "No one can confirm this now"}
			}
			return map[string]any{"needed": !g.notNeeded, "ask_id": id, "channels": channels}
		}
		return map[string]any{"needed": !g.notNeeded, "ask_id": id}
	case p["op"] == "wait" && strings.HasPrefix(fmt.Sprint(p["ask_id"]), "link"):
		g.mu.Lock()
		defer g.mu.Unlock()
		g.ops = append(g.ops, "wait "+fmt.Sprint(p["ask_id"]))
		if g.linkEnd == "lost" { // the answer never arrives
			return fakeRelayStatus{502, "RELAY_UPSTREAM_ERROR", "connection dropped"}
		}
		if g.linkEnd == "waiting" { // nobody taps: one long-poll answers waiting
			g.mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			g.mu.Lock()
		}
		if g.linkEnd != "" {
			return map[string]any{"state": g.linkEnd}
		}
		g.link = g.links[len(g.links)-1]
		return map[string]any{"state": "linked"}
	case p["op"] == "wait":
		g.op("wait")
		if g.onWait != nil {
			g.onWait()
		}
		if g.release != nil {
			select {
			case <-g.release:
			case <-ctx.Done():
				return nil
			}
		}
		if end := g.endFor[fmt.Sprint(p["ask_id"])]; end != "" {
			return map[string]any{"state": end}
		}
		if g.state != "" {
			return map[string]any{"state": g.state}
		}
		return map[string]any{"state": "approved", "grant_id": fmt.Sprint("grant-", p["ask_id"])}
	case p["op"] == "cancel":
		g.op("cancel")
		return map[string]any{"state": "cancelled"}
	}
	return fakeRelayStatus{400, "GATE_BAD_ASK", "not an ask"}
}

// gatedHarness: a gated App (GET /health gate: 1) in front of f.
func gatedHarness(t *testing.T, f *fakeHA, g *fakeGate) *writeHarness {
	t.Helper()
	fastVerify(t)
	if f.service == nil {
		f.service = applies
	}
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		if endpoint == gateAskRoute {
			return g.ask(r.Context(), p)
		}
		if endpoint == gatePhoneRoute {
			return g.phone(p)
		}
		wsWrite := g.wsHeld && endpoint == "ws" && checkReadOnlyRequest(endpoint, p) != nil
		if path := fmt.Sprint(p["path"]); wsWrite || endpoint == "core" && p["method"] != "GET" && (strings.HasPrefix(path, "/api/services/") || strings.HasPrefix(path, "/api/config/")) {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw)) // Home Assistant's fake reads it too
			send := gateSend{endpoint + " " + string(raw), r.Header.Get(gateGrantHeader)}
			g.mu.Lock()
			g.sends = append(g.sends, send)
			g.mu.Unlock()
			if g.hold != nil {
				if answer := g.hold(send); answer != nil {
					return answer
				}
			}
		}
		return f.reply(r, endpoint, p)
	}, true)
	log.gate.Store(true)
	return &writeHarness{t: t, f: f, paths: paths, log: log}
}

const gateUnlock = "core " + `{"body":{"entity_id":["lock.front"]},"method":"POST","path":"/api/services/lock/unlock"}`

// A typed plan on a gated App is confirmed on the linked phone or in Home
// Assistant: no code, and apply asks the Relay for exactly the bytes it then
// sends once with the grant. A typed code is refused; nothing is sent.
func TestGateTypedPlanAsksTheRelayBeforeItSends(t *testing.T) {
	g := &fakeGate{}
	h := gatedHarness(t, newFakeHA(), g)
	env := h.plan("lock.unlock", "lock.front")
	plan := env.Plan
	if plan.Channel != channelRelay || plan.Code != "" || !strings.HasSuffix(env.Say, confirmT("en", "say.relay_ask")) ||
		!strings.Contains(env.NextStep, "apply --plan "+plan.ID+" without --confirm") || !strings.Contains(env.NextStep, channelAsks[channelRelay]) {
		t.Fatalf("relay plan: %+v say %q next %q", plan, env.Say, env.NextStep)
	}
	if !gateSeen(h.paths, activeServerProfile()) {
		t.Fatal("the profile didn't record that it saw the gate")
	}
	if exit, refused := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", "1234"); exit != verbExitRefused || refused.Error.Code != "CONFIRM_IN_HOME_ASSISTANT" || len(h.posts()) != 0 || len(h.planFiles()) != 1 {
		t.Fatalf("a typed code: exit %d %+v (posts %d)", exit, refused.Error, len(h.posts()))
	}
	exit, applied := h.run(runApplyCommand, "--plan", plan.ID)
	asks, ops, sends := g.snapshot()
	if exit != 0 || applied.Status != "verified" || len(asks) != 1 || strings.Join(asks[0], "|") != gateUnlock || strings.Join(ops, ",") != "open,wait" ||
		len(sends) != 1 || sends[0] != (gateSend{gateUnlock, "grant-ask1"}) {
		t.Fatalf("apply: exit %d %+v; asks %q ops %v sends %+v", exit, applied.Error, asks, ops, sends)
	}
}

// A config write and the reload sent after it are one ask; both go out with
// its grant, byte for byte as asked.
// A save asks for the save alone (Home Assistant reloads after it on its
// own); a delete asks with the reload apply sends after it.
func TestGateConfigWriteAsksForWhatIsSent(t *testing.T) {
	g := &fakeGate{}
	h := gatedHarness(t, batchHome(), g)
	id := h.writePlan("script", "s1", map[string]any{"alias": "Arrival", "sequence": []any{step("lock.unlock", "lock.front")}})
	exit, applied := h.run(runApplyCommand, "--plan", id)
	asks, _, sends := g.snapshot()
	if exit != 0 || applied.Status != "verified" || len(asks) != 1 || len(asks[0]) != 1 || len(sends) != 1 || !strings.Contains(asks[0][0], `"path":"/api/config/script/config/s1"`) {
		t.Fatalf("exit %d %+v; asks %q sends %+v", exit, applied.Error, asks, sends)
	}
	_, env := h.run(runPlanCommand, "delete", "script.old_arrival")
	exit, applied = h.run(runApplyCommand, "--plan", env.Plan.ID)
	asks, _, sends = g.snapshot()
	if exit != 0 || len(asks) != 2 || len(asks[1]) != 2 || len(sends) != 3 || !strings.Contains(asks[1][0], `"method":"DELETE"`) || !strings.Contains(asks[1][1], `"path":"/api/services/script/reload"`) {
		t.Fatalf("delete: exit %d %+v; asks %q sends %+v", exit, applied.Error, asks, sends)
	}
	for i, want := range []gateSend{{asks[0][0], "grant-ask1"}, {asks[1][0], "grant-ask2"}, {asks[1][1], "grant-ask2"}} {
		if sends[i] != want {
			t.Fatalf("send %d %+v, want %+v", i, sends[i], want)
		}
	}
}

// Every ask that ends without an approval sends nothing and hands the plan
// back for a retry.
func TestGateRefusalsSendNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		set  func(*fakeGate)
		code string
	}{
		"not needed":     {func(g *fakeGate) { g.notNeeded = true }, "GATE_DISAGREES"},
		"no ask route":   {func(g *fakeGate) { g.openAnswer = fakeRelayStatus{404, "NOT_FOUND", "no route"} }, "GATE_ROLLED_BACK"},
		"no channel":     {func(g *fakeGate) { g.openAnswer = fakeRelayStatus{409, "GATE_NO_CHANNEL", "no one"} }, "GATE_NO_CHANNEL"},
		"check down":     {func(g *fakeGate) { g.openAnswer = fakeRelayStatus{503, "GATE_UNAVAILABLE", "down"} }, "GATE_UNAVAILABLE"},
		"cancelled":      {func(g *fakeGate) { g.state = "cancelled" }, "CONFIRM_DECLINED"},
		"expired":        {func(g *fakeGate) { g.state = "expired" }, "CONFIRM_CANCELLED"},
		"no one answers": {func(g *fakeGate) { g.state = "unavailable" }, "CONFIRM_UNAVAILABLE"},
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{}
			tc.set(g)
			h := gatedHarness(t, newFakeHA(), g)
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			if _, _, sends := g.snapshot(); exit != verbExitRefused || env.Error == nil || env.Error.Code != tc.code || len(sends) != 0 || len(h.planFiles()) != 1 {
				t.Fatalf("exit %d %+v sends %+v plans %v", exit, env.Error, sends, h.planFiles())
			}
		})
	}
}

// A profile that saw the gate refuses typed plans once the App lost it:
// nothing is asked or sent, and no new typed plan is made.
func TestGateGoneAfterItWasSeenRefuses(t *testing.T) {
	g := &fakeGate{}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	h.log.gate.Store(false) // the App was rolled back
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	asks, _, sends := g.snapshot()
	if exit != verbExitRefused || env.Error == nil || env.Error.Code != "GATE_ROLLED_BACK" || len(asks)+len(sends) != 0 || len(h.planFiles()) != 1 ||
		!strings.Contains(env.Error.Message, "HA NOVA's App was rolled back to a version without its safety check; update it in Home Assistant") {
		t.Fatalf("apply: exit %d %+v asks %q sends %+v", exit, env.Error, asks, sends)
	}
	if _, again := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); again.Error == nil || again.Error.Code != "GATE_ROLLED_BACK" {
		t.Fatalf("a new plan: %+v", again)
	}
}

// An App without the gate, never seen: today's confirmation, no ask, no grant.
func TestGateOlderAppKeepsTodaysConfirmation(t *testing.T) {
	g := &fakeGate{}
	h := gatedHarness(t, newFakeHA(), g)
	h.log.gate.Store(false)
	plan := h.plan("lock.unlock", "lock.front").Plan
	if plan.Channel == channelRelay || plan.Code == "" {
		t.Fatalf("plan %+v", plan)
	}
	exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code)
	asks, _, sends := g.snapshot()
	if exit != 0 || len(asks) != 0 || len(sends) != 1 || sends[0].grant != "" || gateSeen(h.paths, activeServerProfile()) {
		t.Fatalf("exit %d %+v asks %q sends %+v", exit, env.Error, asks, sends)
	}
}

// A granted send's answer: 502 GATE_ANSWER_WITHHELD came after Home
// Assistant ran it (unknown, never retried); any other GATE_ code was
// refused before it (not unknown), and a 428 that carried a grant (it
// expired) is asked for once more, never a third time.
func TestGateGrantedSendOutcomes(t *testing.T) {
	for name, answer := range map[string]fakeRelayStatus{
		"answer withheld": {502, "GATE_ANSWER_WITHHELD", "ran"},
		"check down":      {503, "GATE_UNAVAILABLE", "down"},
		"held again":      {428, "GATE_APPROVAL_REQUIRED", "held"},
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{hold: func(gateSend) any { return answer }}
			h := gatedHarness(t, newFakeHA(), g)
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			asks, _, sends := g.snapshot()
			unknown := exit == verbExitUnknown && env.Error != nil && env.Error.Code == "OUTCOME_UNKNOWN"
			grants := []gateSend{{gateUnlock, "grant-ask1"}}
			if answer.status == http.StatusPreconditionRequired {
				grants = append(grants, gateSend{gateUnlock, "grant-ask2"})
			}
			if len(asks) != len(grants) || !slices.Equal(sends, grants) || unknown != (answer.status == 502) || exit == 0 {
				t.Fatalf("exit %d %+v asks %q sends %+v", exit, env.Error, asks, sends)
			}
		})
	}
}
