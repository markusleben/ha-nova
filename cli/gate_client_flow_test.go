package main

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A typed batch is approved before any member runs, also when its first
// member isn't typed: one ask names every member's write and reload, and
// each goes out with that grant, byte for byte as asked.
func TestGateTypedBatchAsksForEveryMemberFirst(t *testing.T) {
	g := &fakeGate{}
	h := gatedHarness(t, batchHome(), g)
	unlock := map[string]any{"alias": "Old arrival", "sequence": []any{step("lock.unlock", "lock.front")}}
	_, env := h.batch(h.writePlan("script", "s1", arrival("light.turn_off")), h.writePlan("script", "s2", unlock))
	if env.Plan == nil || env.Tier != "typed" || env.Plan.Channel != channelRelay || env.Plan.Code != "" {
		t.Fatalf("batch plan: %+v", env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	asks, ops, sends := g.snapshot()
	if exit != 0 || len(asks) != 1 || len(asks[0]) != 2 || len(sends) != 2 || strings.Join(ops, ",") != "open,wait" {
		t.Fatalf("exit %d %+v; asks %q sends %+v", exit, applied.Error, asks, sends)
	}
	for i, send := range sends {
		if send != (gateSend{asks[0][i], "grant-ask1"}) {
			t.Fatalf("send %d %+v, asked %q", i, send, asks[0][i])
		}
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
			gateWindow = func(ctx context.Context, message, first, cancel string, wait time.Duration) windowAnswer {
				if first != confirmT("en", "gate.open") || cancel != confirmT("en", "window.cancel") {
					t.Errorf("buttons %q, %q", first, cancel)
				}
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

// ha-nova mcp refuses a code for a relay plan (nothing sent), then answers
// waiting_for_approval in the home's language while its job's ask waits; a
// second call joins the same job, and the outcome comes once the person
// approved, sent once.
func TestGateMCPApplyWaitsForApproval(t *testing.T) {
	for _, lang := range []string{"en", "de"} {
		t.Run(lang, func(t *testing.T) { gateMCPApply(t, lang) })
	}
}

func gateMCPApply(t *testing.T, lang string) {
	shortApplyWait(t)
	g := &fakeGate{release: make(chan struct{})}
	f := newFakeHA()
	f.config = map[string]any{"language": lang}
	h := gatedHarness(t, f, g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	c := startMCP(t, h.paths)
	mcpInit(c)
	var env map[string]any
	waitFor(t, "the refusal", func() bool {
		env = envelopeOf(c.tool("ha_apply", map[string]any{"plan_id": plan.ID, "confirm_code": "1234"}))
		return env["status"] != "applying"
	})
	if _, _, sends := g.snapshot(); envCode(env) != "CONFIRM_IN_HOME_ASSISTANT" || !strings.Contains(nextOf(env), "without confirm_code") || len(sends) != 0 {
		t.Fatalf("a code through MCP: %v (sends %+v)", env, sends)
	}
	apply := map[string]any{"plan_id": plan.ID}
	waitFor(t, "the ask", func() bool {
		env = envelopeOf(c.tool("ha_apply", apply))
		return env["status"] == "waiting_for_approval"
	})
	env = envelopeOf(c.tool("ha_apply", apply))
	if env["status"] != "waiting_for_approval" || env["say"] != confirmT(lang, "say.relay_waiting") || !strings.Contains(nextOf(env), "call ha_apply again with plan_id "+plan.ID) {
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

// A raw plan with an item typed only by a skill's mark keeps this
// computer's asks (the Relay neither sees nor needs the mark), also when
// another item is typed by itself; one typed by its own requests alone is
// a relay plan that asks for exactly the bytes it sends.
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
	_, mixed := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/update","input_boolean_id":"guest","name":"G"},"typed":true},{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"guest"}}]`)
	if mixed.Tier != "typed" || mixed.Plan == nil || mixed.Plan.Channel == channelRelay || mixed.Plan.Code == "" {
		t.Fatalf("mixed plan: %+v", mixed)
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
