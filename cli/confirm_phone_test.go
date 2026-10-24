package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	status   phoneStatus
	sent     []map[string]any // every notify body but a clear, by service order
	cleared  []string         // "service tag" of every clear_notification
	services []string
	answer   func(ok, no string) []phoneEvent          // the events a tap produces; nil: none
	tapBy    func(service, ok, no string) []phoneEvent // set: answers every notification with buttons
	events   chan phoneEvent
	probeErr error // the Relay can't subscribe
}

var phoneNamesDevice = regexp.MustCompile(`(?i)\bthis (phone|device|watch)\b|\bhere\b`)

func usePhones(t *testing.T, targets ...string) *fakePhones {
	t.Helper()
	p := &fakePhones{events: make(chan phoneEvent, 8),
		setup: phoneSetup{owner: "owner-id", relayUser: "relay-id", confirmer: "owner-id"}}
	if len(targets) > 0 {
		p.status = phoneReady
	}
	for _, s := range targets {
		p.setup.targets = append(p.setup.targets, phoneTarget{Service: s, Name: s})
	}
	saved := []any{windowAvailable, phoneDiscover, phoneNotify, phoneCollect, phoneSettle, windowMinWait, windowWait, phoneTellSpan}
	windowAvailable = func() bool { return false }
	phoneSettle, windowMinWait, windowWait, phoneTellSpan = 10*time.Millisecond, time.Second, 2*time.Second, time.Second
	phoneDiscover = func(*relaySession) (phoneSetup, phoneStatus) { return p.setup, p.status }
	phoneNotify = func(_ *relaySession, service string, body map[string]any) error {
		p.mu.Lock()
		defer p.mu.Unlock()
		if body["message"] == "clear_notification" {
			p.cleared = append(p.cleared, service+" "+body["data"].(map[string]any)["tag"].(string))
			return nil
		}
		p.sent, p.services = append(p.sent, body), append(p.services, service)
		if text := fmt.Sprint(body["title"], " ", body["message"]); phoneNamesDevice.MatchString(text) { // a watch shows it too
			t.Errorf("notification names the device it shows on: %q", text)
		}
		data, _ := body["data"].(map[string]any)
		actions, _ := data["actions"].([]any)
		if len(actions) == 2 && p.tapBy != nil {
			ok := actions[0].(map[string]any)["action"].(string)
			no := actions[1].(map[string]any)["action"].(string)
			for _, ev := range p.tapBy(service, ok, no) {
				p.events <- ev
			}
		}
		if len(actions) == 2 && p.answer != nil && len(p.sent) == 1 { // the first notification is tapped
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
		windowAvailable, phoneDiscover = saved[0].(func() bool), saved[1].(func(*relaySession) (phoneSetup, phoneStatus))
		phoneNotify = saved[2].(func(*relaySession, string, map[string]any) error)
		phoneCollect = saved[3].(func(context.Context, *relaySession, time.Duration) ([]phoneEvent, error))
		phoneSettle, windowMinWait, windowWait, phoneTellSpan = saved[4].(time.Duration), saved[5].(time.Duration), saved[6].(time.Duration), saved[7].(time.Duration)
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
	setup := phoneSetup{owner: "owner-id", relayUser: "relay-id", confirmer: "owner-id"}
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
	// The Relay acting as the confirming user can never confirm (discovery refuses it anyway).
	if got := judgePhoneEvent(tap("OK", "x", "REMOTE"), "OK", "NO", phoneSetup{confirmer: "x", relayUser: "x"}); got != phoneRejected {
		t.Errorf("relay = confirmer: %v", got)
	}
	// Only the linked user counts, not the owner.
	if got := judgePhoneEvent(tap("OK", "owner-id", "REMOTE"), "OK", "NO", phoneSetup{owner: "owner-id", relayUser: "relay-id", confirmer: "partner-id"}); got != phoneRejected {
		t.Errorf("owner tap on a partner's link: %v", got)
	}
}

func TestPhonePlanConfirmsOnTheLinkedPhoneAndReplacesIt(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t, "mobile_app_iphone")
	env := h.plan("lock.unlock", "lock.front")
	plan := env.Plan
	if plan.Channel != channelPhone || plan.Code != "" || !strings.HasSuffix(env.Say, sayPhoneAsk("Unlock", "")) || strings.Contains(env.Say, "one-time code") ||
		!strings.Contains(env.NextStep, "a notification on their phone") || !strings.Contains(env.NextStep, "Never ask for a code") {
		t.Fatalf("phone plan: %+v say %q next %q", plan, env.Say, env.NextStep)
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
	if !strings.Contains(stderr, fmt.Sprintf(phoneSentText, "mobile_app_iphone", phoneTapLine("", "Unlock"))) {
		t.Fatalf("stderr %q", stderr)
	}
	// The linked phone got it once, then the verified result in its place (same tag, no buttons).
	result, _ := p.sent[1]["data"].(map[string]any)
	if strings.Join(p.services, ",") != "mobile_app_iphone,mobile_app_iphone" || p.sent[1]["title"] != "Done: Unlock “lock.front”" ||
		result["actions"] != nil || result["tag"] != p.sent[0]["data"].(map[string]any)["tag"] {
		t.Fatalf("notify calls %v %v", p.services, p.sent)
	}
	data := p.sent[0]["data"].(map[string]any)
	confirm := data["actions"].([]any)[0].(map[string]any)
	// A short question, the computer that asked, and the hint last.
	if _, auth := confirm["authenticationRequired"]; auth || confirm["destructive"] != true || confirm["title"] != "Unlock" || p.sent[0]["title"] != "Unlock “lock.front”?" ||
		!strings.HasPrefix(data["tag"].(string), "ha-nova-"+plan.ID+"-") || p.sent[1]["data"].(map[string]any)["tag"] != data["tag"] ||
		p.sent[0]["message"] != "Requested by “"+phoneHost()+"”. "+phoneHint("", "Unlock", false) || data["push"].(map[string]any)["interruption-level"] != "time-sensitive" {
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
		result string // the phone's result title
	}{
		"wrong user":    {func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "someone-else", "REMOTE")} }, "CONFIRM_REJECTED", "Not confirmed"},
		"relay user":    {func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "relay-id", "REMOTE")} }, "CONFIRM_REJECTED", "Not confirmed"},
		"fired locally": {func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "owner-id", "LOCAL")} }, "CONFIRM_REJECTED", "Not confirmed"},
		"wrong nonce":   {func(string, string) []phoneEvent { return []phoneEvent{tap("HANOVA_OK_guess", "owner-id", "REMOTE")} }, "CONFIRM_CANCELLED", "Not confirmed"},
		"cancel":        {func(_, no string) []phoneEvent { return []phoneEvent{tap(no, "owner-id", "REMOTE")} }, "CONFIRM_DECLINED", "Cancelled"},
		"no answer":     {nil, "CONFIRM_CANCELLED", "Not confirmed"},
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
			if last := p.sent[len(p.sent)-1]; last["title"] != c.result+": Unlock “lock.front”" || !strings.HasSuffix(last["message"].(string), "Nothing changed.") ||
				last["data"].(map[string]any)["actions"] != nil {
				t.Fatalf("no result on the phone: %+v", last)
			}
		})
	}
}

// No linked phone: the next channel. A linked phone that can't be asked
// refuses at plan and at apply, and #719's pin refuses until a phone is
// linked: never a move to a channel the model can answer.
func TestPhoneUnlinkedFallsThroughLinkedMissingRefuses(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t)
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelChat || plan.Code == "" {
		t.Fatalf("no phone: %+v", plan)
	}
	for status, next := range map[phoneStatus]string{phoneMissing: "ha-nova setup confirm-phone", phoneLegacy: "ha-nova setup confirm-phone \"<that phone's name>\""} {
		p.status = status
		exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
		if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || env.Plan != nil || !strings.Contains(env.Error.Next, next) {
			t.Fatalf("status %d: exit %d %+v", status, exit, env.Error)
		}
	}
	// A phone plan whose phone is gone at apply time never falls back.
	p.status = phoneReady
	p.setup.targets = []phoneTarget{{Service: "mobile_app_iphone"}}
	plan := h.plan("lock.unlock", "lock.front").Plan
	p.status = phoneMissing
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(h.posts()) != 0 || len(p.sent) != 0 {
		t.Fatalf("apply, phone gone: exit %d %+v", exit, env.Error)
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

// A window that can't open while the linked phone asks too: the phone
// alone decides, never a code (confirm_both_test.go has the races).
func TestWindowThatCannotOpenLeavesItToThePhone(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t, "mobile_app_iphone")
	useWindow(t, windowFailed)
	plan := h.plan("lock.unlock", "lock.front").Plan
	if plan.Channel != channelWindow {
		t.Fatalf("plan %+v", plan)
	}
	p.answer = func(ok, _ string) []phoneEvent { return []phoneEvent{tap(ok, "owner-id", "REMOTE")} }
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if exit != 0 || env.Status != "verified" || env.Plan.Channel != channelPhone || env.Plan.Code != "" || len(h.posts()) != 1 {
		t.Fatalf("exit %d %+v %+v", exit, env.Plan, env.Error)
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
	usePhones(t, "mobile_app_iphone")
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

// A tap counts only while its phone is still the linked one: unlinked,
// replaced, or its user demoted during the wait, nothing is sent.
func TestPhoneTapAfterTheLinkChangedSendsNothing(t *testing.T) {
	for name, change := range map[string]func(p *fakePhones){
		"unlinked": func(p *fakePhones) { p.status = phoneNone },
		"demoted":  func(p *fakePhones) { p.status = phoneMissing },
		"replaced": func(p *fakePhones) { p.setup.confirmer = "anna-id" },
	} {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, newFakeHA())
			p := usePhones(t, "mobile_app_iphone")
			plan := h.plan("lock.unlock", "lock.front").Plan
			p.answer = func(ok, _ string) []phoneEvent {
				change(p)
				return []phoneEvent{tap(ok, "owner-id", "REMOTE")}
			}
			if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(h.posts()) != 0 {
				t.Fatalf("exit %d %+v posts %d", exit, env.Error, len(h.posts()))
			}
		})
	}
}
