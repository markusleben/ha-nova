package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Channel B (confirm_phone.go): fake phones stand in for the Companion app;
// the fake tap answers with the nonce from the payload it received.

type fakePhones struct {
	mu       sync.Mutex
	setup    phoneSetup
	found    bool
	sent     []map[string]any // every notify body, by service order
	services []string
	answer   func(ok, no string) []phoneEvent // the events a tap produces; nil: none
	events   chan phoneEvent
}

func usePhones(t *testing.T, targets ...string) *fakePhones {
	t.Helper()
	p := &fakePhones{found: len(targets) > 0, events: make(chan phoneEvent, 8),
		setup: phoneSetup{owner: "owner-id", relayUser: "relay-id"}}
	for _, s := range targets {
		p.setup.targets = append(p.setup.targets, phoneTarget{Service: s, Name: s})
	}
	saved := []any{windowAvailable, phoneDiscover, phoneNotify, phoneCollect, phoneSettle, windowMinWait, windowWait}
	windowAvailable = func() bool { return false }
	phoneSettle, windowMinWait, windowWait = 10*time.Millisecond, time.Second, 2*time.Second
	phoneDiscover = func(*relaySession) (phoneSetup, bool) { return p.setup, p.found }
	phoneNotify = func(_ *relaySession, service string, body map[string]any) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.sent, p.services = append(p.sent, body), append(p.services, service)
		data, _ := body["data"].(map[string]any)
		actions, _ := data["actions"].([]any)
		if len(actions) == 2 && p.answer != nil && len(p.sent) == 1 { // the first phone's notification is tapped
			ok := actions[0].(map[string]any)["action"].(string)
			no := actions[1].(map[string]any)["action"].(string)
			for _, ev := range p.answer(ok, no) {
				p.events <- ev
			}
		}
		return nil
	}
	phoneCollect = func(ctx context.Context, _ *relaySession) ([]phoneEvent, error) {
		select {
		case ev := <-p.events:
			return []phoneEvent{ev}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	t.Cleanup(func() {
		windowAvailable, phoneDiscover = saved[0].(func() bool), saved[1].(func(*relaySession) (phoneSetup, bool))
		phoneNotify = saved[2].(func(*relaySession, string, map[string]any) error)
		phoneCollect = saved[3].(func(context.Context, *relaySession) ([]phoneEvent, error))
		phoneSettle, windowMinWait, windowWait = saved[4].(time.Duration), saved[5].(time.Duration), saved[6].(time.Duration)
	})
	return p
}

func tap(action, user, origin string) phoneEvent {
	var ev phoneEvent
	ev.EventType, ev.Origin, ev.Data.Action, ev.Context.UserID = phoneEventType, origin, action, user
	ev.Context.ID = fmt.Sprintf("ctx-%s-%s-%d", user, origin, time.Now().UnixNano())
	return ev
}

func TestJudgePhoneEvent(t *testing.T) {
	setup := phoneSetup{owner: "owner-id", relayUser: "relay-id"}
	cases := []struct {
		ev   phoneEvent
		want phoneAnswer
	}{
		{tap("OK", "owner-id", "REMOTE"), phoneConfirmed},
		{tap("NO", "owner-id", "REMOTE"), phoneDeclined},
		{tap("OTHER", "owner-id", "REMOTE"), phoneIgnored},               // another notification's button
		{tap("OK", "someone-else", "REMOTE"), phoneRejected},             // a different HA user
		{tap("OK", "relay-id", "REMOTE"), phoneRejected},                 // the Relay's own user (REST /api/events)
		{tap("OK", "owner-id", "LOCAL"), phoneRejected},                  // fired inside HA, not by the app
		{tap("OK", "", "REMOTE"), phoneRejected},                         // no user
		{phoneEvent{EventType: "other", Origin: "REMOTE"}, phoneIgnored}, // not ours
	}
	for i, c := range cases {
		if got := judgePhoneEvent(c.ev, "OK", "NO", setup); got != c.want {
			t.Errorf("case %d: %v, want %v", i, got, c.want)
		}
	}
	// The relay acting as the owner can never confirm (discovery refuses it anyway).
	if got := judgePhoneEvent(tap("OK", "x", "REMOTE"), "OK", "NO", phoneSetup{owner: "x", relayUser: "x"}); got != phoneRejected {
		t.Errorf("relay = owner: %v", got)
	}
}

func TestPhonePlanConfirmsOnThePhoneAndClearsEveryPhone(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t, "mobile_app_iphone", "mobile_app_pixel")
	env := h.plan("lock.unlock", "lock.front")
	plan := env.Plan
	if plan.Channel != channelPhone || plan.Code != "" || !strings.HasSuffix(env.Say, sayPhoneAsk) || strings.Contains(env.Say, "one-time code") ||
		!strings.Contains(env.NextStep, "a notification on their phone") || !strings.Contains(env.NextStep, "Never ask for a code") {
		t.Fatalf("phone plan: %+v say %q next %q", plan, env.Say, env.NextStep)
	}
	if phonePin(h.paths, defaultServerProfileName) != "on" {
		t.Fatalf("the found phone is not pinned")
	}
	p.answer = func(ok, _ string) []phoneEvent {
		return []phoneEvent{tap("HANOVA_OK_unrelated", "owner-id", "REMOTE"), tap(ok, "owner-id", "REMOTE")}
	}
	var applied verbTestEnvelope
	var exit int
	stderr := captureStderr(t, func() { exit, applied = h.run(runApplyCommand, "--plan", plan.ID, "--confirm", "0000") })
	if exit != 0 || applied.Status != "verified" || len(h.posts()) != 1 {
		t.Fatalf("confirm: exit %d %+v posts %d", exit, applied.Error, len(h.posts()))
	}
	if !strings.Contains(stderr, phoneSentText) {
		t.Fatalf("stderr %q", stderr)
	}
	// Both phones got it, and both were cleared afterwards.
	if strings.Join(p.services, ",") != "mobile_app_iphone,mobile_app_pixel,mobile_app_iphone,mobile_app_pixel" ||
		p.sent[2]["message"] != "clear_notification" || p.sent[3]["message"] != "clear_notification" {
		t.Fatalf("notify calls %v %v", p.services, p.sent)
	}
	data := p.sent[0]["data"].(map[string]any)
	confirm := data["actions"].([]any)[0].(map[string]any)
	if confirm["authenticationRequired"] != true || confirm["destructive"] != true || data["tag"] != "ha-nova-"+plan.ID ||
		!strings.Contains(p.sent[0]["message"].(string), "Unlock “lock.front”") {
		t.Fatalf("payload %+v", p.sent[0])
	}
	// The ids exist only in the notification: never in output or a plan file.
	nonce := confirm["action"].(string)
	out, _ := json.Marshal(applied)
	if strings.Contains(string(out)+string(mustJSON(env))+stderr, "HANOVA_") || strings.Contains(planDirText(t, h), "HANOVA_") || !strings.HasPrefix(nonce, "HANOVA_OK_") {
		t.Fatalf("a nonce leaked")
	}
}

func TestPhoneForgedOrMissingAnswersSendNothing(t *testing.T) {
	cases := map[string]struct {
		answer func(ok, no string) []phoneEvent
		code   string
	}{
		"wrong user":    {func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "someone-else", "REMOTE")} }, "CONFIRM_REJECTED"},
		"relay user":    {func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "relay-id", "REMOTE")} }, "CONFIRM_REJECTED"},
		"fired locally": {func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "owner-id", "LOCAL")} }, "CONFIRM_REJECTED"},
		"wrong nonce":   {func(string, string) []phoneEvent { return []phoneEvent{tap("HANOVA_OK_guess", "owner-id", "REMOTE")} }, "CONFIRM_CANCELLED"},
		"cancel":        {func(_, no string) []phoneEvent { return []phoneEvent{tap(no, "owner-id", "REMOTE")} }, "CONFIRM_DECLINED"},
		"no answer":     {nil, "CONFIRM_CANCELLED"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, newFakeHA())
			p := usePhones(t, "mobile_app_iphone")
			plan := h.plan("lock.unlock", "lock.front").Plan
			p.answer = c.answer
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			if exit != 4 || env.Error == nil || env.Error.Code != c.code || len(h.posts()) != 0 || len(h.planFiles()) != 1 {
				t.Fatalf("exit %d %+v posts %d plans %d", exit, env.Error, len(h.posts()), len(h.planFiles()))
			}
			if last := p.sent[len(p.sent)-1]; last["message"] != "clear_notification" {
				t.Fatalf("not cleared: %+v", last)
			}
		})
	}
}

// Without the owner's phone (a standalone Relay on the owner's token), B is
// unavailable and the plan uses the next channel; once pinned, a missing
// phone refuses instead of moving to a channel the model can answer.
func TestPhoneUnavailableFallsThroughUntilPinned(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t)
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelChat || plan.Code == "" {
		t.Fatalf("no phone: %+v", plan)
	}
	if err := setPhonePin(h.paths, defaultServerProfileName, "on"); err != nil {
		t.Fatal(err)
	}
	exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
	if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || env.Plan != nil {
		t.Fatalf("pinned, missing: exit %d %+v", exit, env)
	}
	// A phone plan whose phone is gone at apply time never falls back.
	p.found = true
	plan := h.plan("lock.unlock", "lock.front").Plan
	p.found = false
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(h.posts()) != 0 || len(p.sent) != 0 {
		t.Fatalf("apply, phone gone: exit %d %+v", exit, env.Error)
	}
	if err := setPhonePin(h.paths, defaultServerProfileName, "off"); err != nil {
		t.Fatal(err)
	}
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelChat {
		t.Fatalf("off: %+v", plan)
	}
}

// discoverPhone against the Relay: the owner's Companion phones, and none
// when the Relay is the owner (standalone Relay on the owner's token).
func TestDiscoverPhoneNeedsAnOwnerOtherThanTheRelay(t *testing.T) {
	for _, relayUser := range []string{"supervisor-id", "owner-id"} {
		f := newFakeHA()
		f.services = append(f.defaultServices(), map[string]any{"domain": "notify", "services": map[string]any{"mobile_app_markuss_iphone": map[string]any{}, "persistent_notification": map[string]any{}}})
		f.extra = func(endpoint string, p map[string]any) (any, bool) {
			switch {
			case p["type"] == "auth/current_user":
				return map[string]any{"id": relayUser}, true
			case p["type"] == "config/auth/list":
				return []map[string]any{{"id": "supervisor-id", "system_generated": true, "is_active": true},
					{"id": "owner-id", "is_owner": true, "is_active": true}}, true
			case p["path"] == "/api/template":
				vars := p["body"].(map[string]any)["variables"].(map[string]any)
				if vars["owner"] != "owner-id" {
					return coreReply(200, "[]"), true
				}
				return coreReply(200, `[{"service":"mobile_app_markuss_iphone","name":"Markus’s iPhone"},{"service":"mobile_app_gone","name":"Gone"}]`), true
			}
			return nil, false
		}
		h := newWriteHarness(t, f)
		saved := windowAvailable
		windowAvailable = func() bool { return false }
		t.Cleanup(func() { windowAvailable = saved })
		s, err := newRelaySession(h.paths, verbOptions{})
		if err != nil {
			t.Fatal(err)
		}
		setup, ok := discoverPhone(s)
		s.close()
		if relayUser == "owner-id" {
			if ok {
				t.Fatalf("relay = owner: %+v", setup)
			}
			continue
		}
		if !ok || len(setup.targets) != 1 || setup.targets[0].Service != "mobile_app_markuss_iphone" || setup.owner != "owner-id" || setup.relayUser != "supervisor-id" {
			t.Fatalf("setup %+v ok %v", setup, ok)
		}
	}
}

func planDirText(t *testing.T, h *writeHarness) string {
	t.Helper()
	var all strings.Builder
	files, _ := filepath.Glob(filepath.Join(planDir(h.paths), "*"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		all.Write(b)
	}
	cfg, _ := os.ReadFile(h.paths.ConfigFile)
	all.Write(cfg)
	return all.String()
}

// A window that can't open moves the plan to the phone, not to a code.
func TestWindowThatCannotOpenFallsBackToThePhone(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	usePhones(t, "mobile_app_iphone")
	useWindow(t, windowFailed)
	plan := h.plan("lock.unlock", "lock.front").Plan
	if plan.Channel != channelWindow {
		t.Fatalf("plan %+v", plan)
	}
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if exit != 3 || env.Plan == nil || env.Plan.Channel != channelPhone || env.Plan.Code != "" || len(h.posts()) != 0 ||
		!strings.HasSuffix(env.Say, sayPhoneAsk) || !strings.Contains(env.NextStep, "a notification on their phone") {
		t.Fatalf("fallback: exit %d %+v say %q next %q", exit, env.Plan, env.Say, env.NextStep)
	}
}
