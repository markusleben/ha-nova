package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	probeErr error // the Relay can't subscribe
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
	phoneCollect = func(ctx context.Context, _ *relaySession, span time.Duration) ([]phoneEvent, error) {
		if p.probeErr != nil && span < time.Second {
			return nil, p.probeErr
		}
		if span < time.Second { // the probe window: subscribed, nothing yet
			return nil, nil
		}
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
		phoneCollect = saved[3].(func(context.Context, *relaySession, time.Duration) ([]phoneEvent, error))
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
	if phonePin(h.paths, "owner-id") != "on" {
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
	sends, cleared := append([]string{}, p.services[:2]...), append([]string{}, p.services[2:]...) // both run in parallel
	sort.Strings(sends)
	sort.Strings(cleared)
	if strings.Join(sends, ",")+","+strings.Join(cleared, ",") != "mobile_app_iphone,mobile_app_pixel,mobile_app_iphone,mobile_app_pixel" ||
		p.sent[2]["message"] != "clear_notification" || p.sent[3]["message"] != "clear_notification" {
		t.Fatalf("notify calls %v %v", p.services, p.sent)
	}
	data := p.sent[0]["data"].(map[string]any)
	confirm := data["actions"].([]any)[0].(map[string]any)
	if confirm["authenticationRequired"] != true || confirm["destructive"] != true || !strings.HasPrefix(data["tag"].(string), "ha-nova-"+plan.ID+"-") || p.sent[2]["data"].(map[string]any)["tag"] != data["tag"] ||
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
	if err := setPhonePin(h.paths, "owner-id", "on"); err != nil {
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
	// The pin belongs to this Home Assistant's owner, not to a server name:
	// another home's "off" changes nothing here.
	if err := setPhonePin(h.paths, "another-owner", "off"); err != nil {
		t.Fatal(err)
	}
	p.found = true
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelPhone {
		t.Fatalf("another owner's off: %+v", plan)
	}
	if err := setPhonePin(h.paths, "owner-id", "off"); err != nil {
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

// A found phone whose pin can't be saved refuses: without the pin, a later
// broken phone setup would move plans to a channel the model can answer.
func TestPhonePinThatCannotBeSavedRefuses(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	usePhones(t, "mobile_app_iphone")
	dir := filepath.Dir(h.paths.ConfigFile)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
	if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || env.Plan != nil {
		t.Fatalf("unsaved pin: exit %d %+v", exit, env)
	}
}

// An edited phone plan file (its prompt or channel) is never asked on the
// phone, and apply refuses it.
func TestEditedPhonePlanIsNeverAsked(t *testing.T) {
	for _, edit := range []func(map[string]any){
		func(p map[string]any) { p["prompt"] = "Turn on “Kitchen”." },
		func(p map[string]any) { p["channel"] = channelChat },
	} {
		h := newWriteHarness(t, newFakeHA())
		p := usePhones(t, "mobile_app_iphone")
		plan := h.plan("lock.unlock", "lock.front").Plan
		h.tamper(plan.ID, edit)
		exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", storedCode(t, h, plan.ID))
		if exit != 4 || len(p.sent) != 0 || len(h.posts()) != 0 {
			t.Fatalf("edited plan: exit %d %+v phone %d posts %d", exit, env.Error, len(p.sent), len(h.posts()))
		}
	}
}

// A Relay that can't subscribe gets no notification: buttons no listener
// can hear are never sent.
func TestPhoneNeverSendsWithoutAListener(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t, "mobile_app_iphone")
	plan := h.plan("lock.unlock", "lock.front").Plan
	p.probeErr = fmt.Errorf("UPSTREAM_WS_TIMEOUT")
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(p.sent) != 0 || len(h.posts()) != 0 {
		t.Fatalf("no listener: exit %d %+v phone %d", exit, env.Error, len(p.sent))
	}
}

// A stalled notify service never holds apply past the confirmation wait.
func TestPhoneStalledSendIsBoundedByTheWait(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	usePhones(t, "mobile_app_iphone", "mobile_app_pixel")
	plan := h.plan("lock.unlock", "lock.front").Plan
	phoneNotify = func(*relaySession, string, map[string]any) error { time.Sleep(10 * time.Second); return nil }
	start := time.Now()
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if took := time.Since(start); exit != 4 || env.Error == nil || took > 6*time.Second || len(h.posts()) != 0 {
		t.Fatalf("stalled: exit %d %+v after %v", exit, env.Error, took)
	}
}

// An automation that could copy a phone confirmation's buttons into its own
// notification (overlay) is typed to write; one on the tap itself is not.
func TestDraftWatchingConfirmationsIsTyped(t *testing.T) {
	ev := func(eventType any) map[string]any { return map[string]any{"trigger": "event", "event_type": eventType} }
	for _, watch := range []any{
		ev("call_service"), ev("mobile_app_notification_received"), ev("mobile_app_notification_cleared"), ev("{{ 'call_' ~ 'service' }}"),
		ev([]any{"mobile_app_notification_action", "call_service"}), map[string]any{"platform": "event"},
	} {
		if !watchesConfirmation(map[string]any{"triggers": []any{watch}}) {
			t.Errorf("not caught: %v", watch)
		}
	}
	waits := map[string]any{"actions": []any{map[string]any{"wait_for_trigger": []any{ev("call_service")}}}}
	if !watchesConfirmation(waits) || watchesConfirmation(map[string]any{"triggers": []any{ev("mobile_app_notification_action")}}) ||
		watchesConfirmation(map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.hall"}}}) {
		t.Fatal("wait_for_trigger or harmless triggers misjudged")
	}
	h := newWriteHarness(t, newFakeHA())
	notify := map[string]any{"action": "notify.mobile_app_iphone", "data": map[string]any{"message": "Porch light?", "data": map[string]any{"actions": "{{ trigger.event.data.service_data.data.actions }}"}}}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, map[string]any{
		"alias": "Echo", "triggers": []any{ev("call_service")}, "actions": []any{notify}}))
	if env.Tier != "typed" || !strings.Contains(env.Say, "could see another confirmation's buttons") {
		t.Fatalf("overlay draft: tier %q say %q", env.Tier, env.Say)
	}
	_, tap := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, map[string]any{
		"alias": "On tap", "triggers": []any{ev("mobile_app_notification_action")}, "actions": []any{step("light.turn_on", "light.hall")}}))
	if tap.Tier == "typed" {
		t.Fatalf("tap listener: %q %+v", tap.Tier, tap.Reasons)
	}
}

// An unreadable owner never skips a pin: the plan is refused, not moved.
func TestPhonePinWithUnknownOwnerRefuses(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t)
	p.setup.owner = "" // the owner's account couldn't be read
	if err := setPhonePin(h.paths, "owner-id", "on"); err != nil {
		t.Fatal(err)
	}
	if exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" {
		t.Fatalf("unknown owner: exit %d %+v", exit, env.Error)
	}
}
