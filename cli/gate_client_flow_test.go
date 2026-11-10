package main

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A later batch member the first ask didn't cover is asked for when the
// Relay holds it (428 without a grant), with its reload, and sent once more
// with that grant.
func TestGateAsksALaterBatchMemberWhenTheRelayHoldsIt(t *testing.T) {
	g := &fakeGate{hold: func(send gateSend) any {
		if send.grant == "" {
			return fakeRelayStatus{428, "GATE_APPROVAL_REQUIRED", "held"}
		}
		return nil
	}}
	h := gatedHarness(t, batchHome(), g)
	unlock := func(alias string) map[string]any {
		return map[string]any{"alias": alias, "sequence": []any{step("lock.unlock", "lock.front")}}
	}
	_, env := h.batch(h.writePlan("script", "s1", unlock("Arrival")), h.writePlan("script", "s2", unlock("Old arrival")))
	if env.Plan == nil || env.Plan.Channel != channelRelay || env.Plan.Code != "" {
		t.Fatalf("batch plan: %+v", env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	asks, _, sends := g.snapshot()
	var grants []string
	for _, send := range sends {
		grants = append(grants, send.grant)
	}
	if exit != 0 || len(asks) != 2 || strings.Join(grants, ",") != "grant-ask1,grant-ask1,,grant-ask2,grant-ask2" ||
		sends[2].key != asks[1][0] || sends[3].key != asks[1][0] || sends[4].key != asks[1][1] || sends[0].key != asks[0][0] {
		t.Fatalf("exit %d %+v; asks %q sends %+v", exit, applied.Error, asks, sends)
	}
}

// The waiting window says where to confirm; its first button only opens
// Home Assistant's NOVA page (from the paired profile), Cancel ends the ask,
// and a Cancel that raced the approval wins: nothing is sent.
func TestGateWaitingWindow(t *testing.T) {
	savedAvailable, savedWindow, savedOpen := windowAvailable, gateWindow, gateOpenHA
	t.Cleanup(func() { windowAvailable, gateWindow, gateOpenHA = savedAvailable, savedWindow, savedOpen })
	windowAvailable = func() bool { return true }
	panel, _ := haNOVAAppPanelURL("http://127.0.0.1:8123")
	for name, tc := range map[string]struct {
		answer func(ctx context.Context) windowAnswer
		sent   int
		code   string
	}{
		"open Home Assistant": {func(context.Context) windowAnswer { return windowConfirmed }, 1, ""},
		"cancel":              {func(context.Context) windowAnswer { return windowCancelled }, 0, "CONFIRM_DECLINED"},
		"cancel racing":       {func(ctx context.Context) windowAnswer { <-ctx.Done(); return windowCancelled }, 0, "CONFIRM_DECLINED"},
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{}
			if name != "cancel racing" {
				g.release = make(chan struct{}) // the approval comes after the window's answer
			}
			var texts, opened []string
			gateWindow = func(ctx context.Context, message, lang string, wait time.Duration) windowAnswer {
				texts = append(texts, message)
				return tc.answer(ctx)
			}
			gateOpenHA = func(url string) error {
				opened = append(opened, url)
				close(g.release)
				return nil
			}
			h := gatedHarness(t, newFakeHA(), g)
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			_, ops, sends := g.snapshot()
			want := confirmT("en", "gate.window")
			if runtime.GOOS == "windows" {
				want += "\n\n" + confirmT("en", "gate.windows_ok")
			}
			if len(sends) != tc.sent || len(texts) != 1 || texts[0] != want || (tc.code != "" && (env.Error == nil || env.Error.Code != tc.code || !strings.Contains(strings.Join(ops, ","), "cancel"))) {
				t.Fatalf("exit %d %+v; sends %+v ops %v texts %q", exit, env.Error, sends, ops, texts)
			}
			if wantOpened := map[bool]int{true: 1, false: 0}[name == "open Home Assistant"]; len(opened) != wantOpened || (wantOpened == 1 && opened[0] != panel.URL) {
				t.Fatalf("opened %q, want %s", opened, panel.URL)
			}
		})
	}
}

// A stop signal (ha-nova mcp told to stop) ends a waiting ask unsent, and
// an approval that arrives after it never goes out.
func TestGateStopSignalEndsTheAskUnsent(t *testing.T) {
	asks := planAsks
	t.Cleanup(func() { planAsks = asks })
	ctx, stop := context.WithCancel(context.Background())
	planAsks = ctx
	g := &fakeGate{release: make(chan struct{}), onWait: stop}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	_, ops, sends := g.snapshot()
	if exit != verbExitRefused || env.Error == nil || env.Error.Code != "CONFIRM_CANCELLED" || len(sends) != 0 || !strings.Contains(strings.Join(ops, ","), "cancel") {
		t.Fatalf("stopped: exit %d %+v; ops %v sends %+v", exit, env.Error, ops, sends)
	}
	paths, calls := readRelay(t, nil)
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.sendWriteOnce("core", []byte(`{"method":"POST","path":"/api/services/lock/unlock"}`), "grant-ask1"); err == nil || len(calls()) != 0 {
		t.Fatalf("an approved send after the stop: %v (calls %v)", err, calls())
	}
}

// ha-nova mcp answers waiting_for_approval while its job's ask waits; a
// second call joins the same job, and the outcome comes once the person
// approved, sent once.
func TestGateMCPApplyWaitsForApproval(t *testing.T) {
	shortApplyWait(t)
	g := &fakeGate{release: make(chan struct{})}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	c := startMCP(t, h.paths)
	mcpInit(c)
	apply := map[string]any{"plan_id": plan.ID}
	var env map[string]any
	waitFor(t, "the ask", func() bool {
		env = envelopeOf(c.tool("ha_apply", apply))
		return env["status"] == "waiting_for_approval"
	})
	env = envelopeOf(c.tool("ha_apply", apply))
	if env["status"] != "waiting_for_approval" || env["say"] != confirmT("en", "say.relay_waiting") || !strings.Contains(nextOf(env), "call ha_apply again with plan_id "+plan.ID) {
		t.Fatalf("joined call: %v", env)
	}
	close(g.release)
	waitFor(t, "the outcome", func() bool {
		env = envelopeOf(c.tool("ha_apply", apply))
		return env["status"] != "applying" && env["status"] != "waiting_for_approval"
	})
	if asks, _, sends := g.snapshot(); env["status"] != "verified" || len(asks) != 1 || len(sends) != 1 || sends[0].grant != "grant-ask1" {
		t.Fatalf("outcome %v; asks %q sends %+v", env, asks, sends)
	}
}

// A raw plan typed only by a skill's mark keeps this computer's asks (the
// gate may pass it); one typed by its own request is a relay plan that asks
// for exactly the bytes it sends.
func TestGateRawPlans(t *testing.T) {
	g := &fakeGate{}
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"health <nil>":          func(map[string]any) any { return map[string]any{"status": "ok", "gate": 1} },
		gateAskRoute + " <nil>": func(p map[string]any) any { return g.ask(context.Background(), p) },
		"input_boolean/list": func(map[string]any) any {
			return []any{map[string]any{"id": "guest", "name": "Guest"}}
		},
		"config/entity_registry/list": func(map[string]any) any {
			return []any{map[string]any{"entity_id": "input_boolean.guest", "platform": "input_boolean", "unique_id": "guest"}}
		},
	})
	_, marked := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/update","input_boolean_id":"guest","name":"G"},"typed":true}]`)
	if marked.Tier != "typed" || marked.Plan == nil || marked.Plan.Channel == channelRelay || marked.Plan.Code == "" {
		t.Fatalf("skill-typed plan: %+v", marked)
	}
	_, typed := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"guest"}}]`)
	if typed.Tier != "typed" || typed.Plan == nil || typed.Plan.Channel != channelRelay || typed.Plan.Code != "" {
		t.Fatalf("typed plan: %+v", typed)
	}
	exit, applied := applyRaw(t, paths, typed)
	asks, _, _ := g.snapshot()
	if exit != 0 || len(asks) != 1 || len(asks[0]) != 1 || !strings.HasPrefix(asks[0][0], "ws ") || !strings.Contains(strings.Join(sent(), "|"), "input_boolean/delete") {
		t.Fatalf("apply: exit %d %+v asks %q sent %v", exit, applied.Error, asks, sent())
	}
}
