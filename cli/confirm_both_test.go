package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// The window and the linked phone ask together (confirm_both.go): the first
// decisive answer wins, the other is withdrawn, and a late answer changes
// nothing. Timing is injected: each side answers when its gate opens.

type raceWindow struct {
	mu       sync.Mutex
	messages []string
	closed   bool // withdrawn: its context ended before it answered
}

// useRaceWindow: a window that answers when gate closes. stuck: one that
// can't be closed (it ignores the withdrawal and answers late anyway).
func useRaceWindow(t *testing.T, answer windowAnswer, gate <-chan struct{}, stuck bool) *raceWindow {
	t.Helper()
	w := &raceWindow{}
	savedAvailable, savedRunner := windowAvailable, windowRunner
	windowAvailable = func() bool { return true }
	windowRunner = func(ctx context.Context, message, _ string, wait time.Duration) windowAnswer {
		w.mu.Lock()
		w.messages = append(w.messages, message)
		w.mu.Unlock()
		if stuck {
			<-gate
			return answer
		}
		select {
		case <-gate:
			return answer
		case <-ctx.Done():
			w.mu.Lock()
			w.closed = true
			w.mu.Unlock()
			return windowNoAnswer
		}
	}
	t.Cleanup(func() { windowAvailable, windowRunner = savedAvailable, savedRunner })
	return w
}

func (w *raceWindow) wasClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// tapWhen taps the plan notification's button (ok or no) once gate closes.
func (p *fakePhones) tapWhen(t *testing.T, gate <-chan struct{}, confirm bool) {
	go func() {
		for give := time.Now().Add(5 * time.Second); time.Now().Before(give); {
			p.mu.Lock()
			var ok, no string
			if len(p.sent) > 0 {
				if actions, _ := p.sent[0]["data"].(map[string]any)["actions"].([]any); len(actions) == 2 {
					ok, no = actions[0].(map[string]any)["action"].(string), actions[1].(map[string]any)["action"].(string)
				}
			}
			p.mu.Unlock()
			if ok != "" {
				<-gate
				p.events <- tap(map[bool]string{true: ok, false: no}[confirm], "owner-id", "REMOTE")
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
}

// waitSent: the plan notification went out (the race starts from there).
func (p *fakePhones) waitSent() {
	for give := time.Now().Add(5 * time.Second); time.Now().Before(give); time.Sleep(2 * time.Millisecond) {
		p.mu.Lock()
		n := len(p.sent)
		p.mu.Unlock()
		if n > 0 {
			return
		}
	}
}

func (p *fakePhones) last() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sent[len(p.sent)-1]
}

func opened() chan struct{} { c := make(chan struct{}); close(c); return c }

func TestBothAskFirstAnswerWins(t *testing.T) {
	type side struct {
		confirm bool
		first   bool
	}
	cases := map[string]struct {
		window, phone side
		code          string // "" applied
		where         string // the applied plan's channel
		result        string // the phone's result line (prefix)
	}{
		"window confirms, phone confirms late": {side{true, true}, side{true, false}, "", channelWindow, "Approved on the computer. Home Assistant confirms it."},
		"phone confirms, window confirms late": {side{true, false}, side{true, true}, "", channelPhone, "Home Assistant confirms it."},
		"window cancels, phone confirms late":  {side{false, true}, side{true, false}, "CONFIRM_DECLINED", "", "Cancelled on the computer. Nothing changed."},
		"phone cancels, window confirms late":  {side{true, false}, side{false, true}, "CONFIRM_DECLINED", "", "Nothing changed."},
		"window confirms, phone cancels late":  {side{true, true}, side{false, false}, "", channelWindow, "Approved on the computer."},
		"phone confirms, window cancels late":  {side{false, false}, side{true, true}, "", channelPhone, "Home Assistant confirms it."},
	}
	for name, c := range cases {
		for _, stuck := range []bool{false, true} { // a window that can't be closed answers late; it is ignored
			t.Run(fmt.Sprintf("%s/stuck=%v", name, stuck), func(t *testing.T) {
				h := newWriteHarness(t, newFakeHA())
				p := usePhones(t, "mobile_app_iphone")
				first, late := make(chan struct{}), make(chan struct{})
				gate := map[bool]chan struct{}{true: first, false: late}
				w := useRaceWindow(t, map[bool]windowAnswer{true: windowConfirmed, false: windowCancelled}[c.window.confirm], gate[c.window.first], stuck)
				p.tapWhen(t, gate[c.phone.first], c.phone.confirm)
				plan := h.plan("lock.unlock", "lock.front").Plan
				go func() { p.waitSent(); close(first); time.Sleep(250 * time.Millisecond); close(late) }()
				exit, env := h.run(runApplyCommand, "--plan", plan.ID)
				posts := len(h.posts())
				if c.code == "" && (exit != 0 || env.Status != "verified" || posts != 1 || env.Plan.Channel != c.where) ||
					c.code != "" && (exit != 4 || env.Error == nil || env.Error.Code != c.code || posts != 0) {
					t.Fatalf("exit %d status %q plan %+v err %+v posts %d", exit, env.Status, env.Plan, env.Error, posts)
				}
				if !c.window.first && !stuck && !w.wasClosed() {
					t.Fatal("the window was not withdrawn")
				}
				if msg := p.last()["message"].(string); !strings.HasPrefix(msg, c.result) || p.last()["data"].(map[string]any)["actions"] != nil {
					t.Fatalf("phone result %q, want %q", msg, c.result)
				}
				<-late // a late answer, after the decision, changes nothing
				time.Sleep(50 * time.Millisecond)
				if len(h.posts()) != posts {
					t.Fatalf("a late answer sent: %d", len(h.posts()))
				}
				if !strings.Contains(w.messages[0], windowBoth) {
					t.Fatalf("window text %q", w.messages[0])
				}
			})
		}
	}
}

// Both answer at the same instant: exactly one decides, and what happened
// matches who won.
func TestBothAskSimultaneousAnswersApplyAtMostOnce(t *testing.T) {
	for i := 0; i < 20; i++ {
		for _, phoneConfirms := range []bool{true, false} {
			h := newWriteHarness(t, newFakeHA())
			p := usePhones(t, "mobile_app_iphone")
			gate := make(chan struct{})
			useRaceWindow(t, windowConfirmed, gate, false)
			p.tapWhen(t, gate, phoneConfirms)
			plan := h.plan("lock.unlock", "lock.front").Plan
			go func() { p.waitSent(); close(gate) }()
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			msg := p.last()["message"].(string)
			switch {
			case exit == 0 && len(h.posts()) == 1 && env.Plan.Channel == channelWindow && strings.HasPrefix(msg, "Approved on the computer."):
			case exit == 0 && len(h.posts()) == 1 && phoneConfirms && env.Plan.Channel == channelPhone && msg == "Home Assistant confirms it.":
			case exit == 4 && len(h.posts()) == 0 && !phoneConfirms && env.Error.Code == "CONFIRM_DECLINED" && strings.Contains(env.Error.Message, "phone"):
			default:
				t.Fatalf("run %d phone confirms %v: exit %d plan %+v err %+v posts %d phone %q", i, phoneConfirms, exit, env.Plan, env.Error, len(h.posts()), msg)
			}
		}
	}
}

// One side out: the other decides alone, never a fallback to chat.
func TestBothAskOneSideOut(t *testing.T) {
	t.Run("window can't open: the phone decides", func(t *testing.T) {
		h := newWriteHarness(t, newFakeHA())
		p := usePhones(t, "mobile_app_iphone")
		useRaceWindow(t, windowFailed, opened(), false)
		p.tapWhen(t, opened(), true)
		plan := h.plan("lock.unlock", "lock.front").Plan
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || len(h.posts()) != 1 || env.Plan.Channel != channelPhone {
			t.Fatalf("exit %d %+v %+v", exit, env.Plan, env.Error)
		}
	})
	t.Run("window closed by the OS: the phone decides", func(t *testing.T) {
		h := newWriteHarness(t, newFakeHA())
		p := usePhones(t, "mobile_app_iphone")
		useRaceWindow(t, windowNoAnswer, opened(), false)
		late := make(chan struct{})
		p.tapWhen(t, late, true)
		plan := h.plan("lock.unlock", "lock.front").Plan
		go func() { time.Sleep(50 * time.Millisecond); close(late) }()
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || len(h.posts()) != 1 || env.Plan.Channel != channelPhone {
			t.Fatalf("exit %d %+v %+v", exit, env.Plan, env.Error)
		}
	})
	t.Run("notify fails: the window decides", func(t *testing.T) {
		h := newWriteHarness(t, newFakeHA())
		usePhones(t, "mobile_app_iphone")
		phoneNotify = func(*relaySession, string, map[string]any) error { return fmt.Errorf("HTTP 500") }
		late := make(chan struct{})
		useRaceWindow(t, windowConfirmed, late, false)
		plan := h.plan("lock.unlock", "lock.front").Plan
		go func() { time.Sleep(50 * time.Millisecond); close(late) }()
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || len(h.posts()) != 1 || env.Plan.Channel != channelWindow {
			t.Fatalf("exit %d %+v %+v", exit, env.Plan, env.Error)
		}
	})
	t.Run("notify fails, the window gets no answer: said", func(t *testing.T) {
		h := newWriteHarness(t, newFakeHA())
		usePhones(t, "mobile_app_iphone")
		phoneNotify = func(*relaySession, string, map[string]any) error { return fmt.Errorf("HTTP 500") }
		useRaceWindow(t, windowConfirmed, make(chan struct{}), false)
		plan := h.plan("lock.unlock", "lock.front").Plan
		var exit int
		var env verbTestEnvelope
		stderr := captureStderr(t, func() { exit, env = h.run(runApplyCommand, "--plan", plan.ID) })
		if exit != 4 || env.Error.Code != "CONFIRM_CANCELLED" || !strings.Contains(env.Error.Message, "the phone couldn't be asked") ||
			!strings.Contains(env.Error.Next, "confirm-phone list") || !strings.Contains(stderr, phoneQuietText) || len(h.posts()) != 0 {
			t.Fatalf("exit %d %+v stderr %q", exit, env.Error, stderr)
		}
	})
	t.Run("neither can ask: refused", func(t *testing.T) {
		h := newWriteHarness(t, newFakeHA())
		p := usePhones(t, "mobile_app_iphone")
		p.probeErr = fmt.Errorf("UPSTREAM_WS_TIMEOUT")
		useRaceWindow(t, windowFailed, opened(), false)
		plan := h.plan("lock.unlock", "lock.front").Plan
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(h.posts()) != 0 || env.Plan != nil ||
			!strings.Contains(env.Error.Message, "window couldn't open") {
			t.Fatalf("exit %d %+v %+v", exit, env.Plan, env.Error)
		}
	})
	t.Run("linked phone unreachable: the window alone, and it says so", func(t *testing.T) {
		h := newWriteHarness(t, newFakeHA())
		p := usePhones(t, "mobile_app_iphone")
		w := useRaceWindow(t, windowConfirmed, opened(), false)
		plan := h.plan("lock.unlock", "lock.front").Plan
		p.status = phoneMissing
		var exit int
		var env verbTestEnvelope
		stderr := captureStderr(t, func() { exit, env = h.run(runApplyCommand, "--plan", plan.ID) })
		if exit != 0 || len(h.posts()) != 1 || len(p.sent) != 0 || !strings.Contains(stderr, phoneQuietText) || strings.Contains(w.messages[0], windowBoth) || env.Plan.Channel != channelWindow {
			t.Fatalf("exit %d %+v sent %d stderr %q", exit, env.Error, len(p.sent), stderr)
		}
	})
}

// No answer by the one deadline: both withdrawn, nothing sent, the plan kept.
func TestBothAskExpireTogether(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t, "mobile_app_iphone")
	w := useRaceWindow(t, windowConfirmed, make(chan struct{}), false)
	plan := h.plan("lock.unlock", "lock.front").Plan
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if exit != 4 || env.Error.Code != "CONFIRM_CANCELLED" || !strings.Contains(env.Error.Message, "neither") || len(h.posts()) != 0 || len(h.planFiles()) != 1 || !w.wasClosed() ||
		!strings.Contains(env.Error.Next, "a new notification goes to their phone") || !strings.Contains(env.Error.Next, phoneTapLine("", "Unlock")) {
		t.Fatalf("exit %d %+v posts %d closed %v", exit, env.Error, len(h.posts()), w.wasClosed())
	}
	if last := p.last(); last["title"] != "Not confirmed: Unlock “lock.front”" || last["data"].(map[string]any)["actions"] != nil {
		t.Fatalf("phone result %+v", last)
	}
}

// The plan's `say` names both channels where a phone is linked here.
func TestWindowPlanSayNamesTheLinkedPhone(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	useRaceWindow(t, windowCancelled, opened(), false)
	if say := h.plan("lock.unlock", "lock.front").Say; !strings.HasSuffix(say, sayWindowAsk) {
		t.Fatalf("no phone: %q", say)
	}
	if err := setPhoneLink(h.paths, "owner-id", phoneEntry{}, &phoneLink{Service: "mobile_app_iphone", DeviceID: "d1", UserID: "owner-id", Name: "iPhone"}); err != nil {
		t.Fatal(err)
	}
	if env := h.plan("lock.unlock", "lock.front"); !strings.HasSuffix(env.Say, sayWindowPhoneAsk("Unlock", "")) || !strings.Contains(env.NextStep, "and a notification on their linked phone") {
		t.Fatalf("phone linked: %q next %q", env.Say, env.NextStep)
	}
}

// A window that can't open where discovery found no phone, but finds it at
// the fallback: the plan moves to the phone (windowFallback), never a code.
func TestWindowFallbackFindsThePhoneLate(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t, "mobile_app_iphone")
	useWindow(t, windowFailed)
	plan := h.plan("lock.unlock", "lock.front").Plan
	calls := 0
	phoneDiscover = func(*relaySession) (phoneSetup, phoneStatus) {
		if calls++; calls == 1 {
			return phoneSetup{}, phoneNone
		}
		return p.setup, phoneReady
	}
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if exit != 3 || env.Plan == nil || env.Plan.Channel != channelPhone || env.Plan.Code != "" || len(h.posts()) != 0 ||
		!strings.HasSuffix(env.Say, sayPhoneAsk("Unlock", "")) || !strings.Contains(env.NextStep, "a notification on their phone") {
		t.Fatalf("fallback: exit %d %+v say %q next %q", exit, env.Plan, env.Say, env.NextStep)
	}
}

// A slow phone lookup that leaves too little time asks no one.
func TestSlowDiscoveryLeavesNoTimeAsksNoOne(t *testing.T) {
	for _, window := range []bool{true, false} {
		h := newWriteHarness(t, newFakeHA())
		p := usePhones(t, "mobile_app_iphone")
		var w *raceWindow
		if window {
			w = useRaceWindow(t, windowConfirmed, opened(), false)
		}
		plan := h.plan("lock.unlock", "lock.front").Plan
		phoneDiscover = func(*relaySession) (phoneSetup, phoneStatus) {
			time.Sleep(1500 * time.Millisecond)
			return p.setup, phoneReady
		}
		exit, env := h.run(runApplyCommand, "--plan", plan.ID)
		if exit != 4 || env.Error.Code != "PLAN_EXPIRED" || len(p.sent) != 0 || len(h.posts()) != 0 || w != nil && len(w.messages) != 0 {
			t.Fatalf("window %v: exit %d %+v sent %d", window, exit, env.Error, len(p.sent))
		}
	}
}
