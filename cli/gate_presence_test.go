package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func stubIdle(t *testing.T, read func() (time.Duration, bool)) {
	t.Helper()
	savedIdle, savedEvery := idleTime, gateIdleEvery
	t.Cleanup(func() { idleTime, gateIdleEvery = savedIdle, savedEvery })
	idleTime, gateIdleEvery = read, 10*time.Millisecond
}

// The open says whether the person is at this computer only where this
// plan's window with Confirm opens, and only to an App with the window
// contract; without it the App asks the phone at once.
func TestGatePresenceOnlyWhereTheWindowConfirms(t *testing.T) {
	saved := windowAvailable
	t.Cleanup(func() { windowAvailable = saved })
	windowAvailable = func() bool { return true }
	s := &relaySession{paths: runtimePaths{ConfigFile: filepath.Join(t.TempDir(), "none.json")}, gate: &relayGate{checked: true, on: true, window: true}}
	plan := &gateConfirm{lang: "en", text: "Unlock?\n\nFront door"}
	for want, idle := range map[string]func() (time.Duration, bool){
		"present": func() (time.Duration, bool) { return 19 * time.Second, true },
		"away":    func() (time.Duration, bool) { return 20 * time.Second, true },
		"unknown": func() (time.Duration, bool) { return 0, false },
	} {
		stubIdle(t, idle)
		if got := s.gatePresence(plan); got != want {
			t.Fatalf("%s: %q", want, got)
		}
	}
	long := &gateConfirm{lang: "en", text: "Turn these off?\n\n" + strings.Repeat("• light.room\n", 60)}
	older := &relaySession{paths: s.paths, gate: &relayGate{checked: true, on: true}}
	for name, got := range map[string]string{
		"held write":   s.gatePresence(nil),
		"no text":      s.gatePresence(&gateConfirm{lang: "en"}),
		"too long":     s.gatePresence(long),
		"older App":    older.gatePresence(plan),
		"no gate read": (&relaySession{paths: s.paths}).gatePresence(plan),
	} {
		if got != "" {
			t.Fatalf("%s: %q", name, got)
		}
	}
	windowAvailable = func() bool { return false }
	if got := s.gatePresence(plan); got != "" {
		t.Fatalf("no window here: %q", got)
	}
}

// Through apply: the open carries presence on an App with the window
// contract; an older gated App gets none and no /gate/v1/me read.
func TestGatePresenceSentWithTheOpen(t *testing.T) {
	stubIdle(t, func() (time.Duration, bool) { return time.Second, true })
	for _, older := range []bool{false, true} {
		g := &fakeGate{phones: true, channels: []string{"window"}}
		windowConfirmStubs(t, func(windowText) windowAnswer { return windowConfirmed }, nil)
		h := gatedHarness(t, newFakeHA(), g)
		h.log.noWin.Store(older)
		h.log.me.Store(map[string]any{"window": true, "why": "on"})
		plan := h.plan("lock.unlock", "lock.front").Plan
		exit, env := h.run(runApplyCommand, "--plan", plan.ID)
		_, _, sends := g.snapshot()
		g.mu.Lock()
		presence := append([]any(nil), g.presence...)
		g.mu.Unlock()
		want := map[bool]any{false: "present", true: nil}[older]
		if exit != 0 || len(sends) != 1 || len(presence) != 1 || presence[0] != want || (h.log.meGot.Load() == 0) != older {
			t.Fatalf("older %v: exit %d %+v; presence %v me reads %d", older, exit, env.Error, presence, h.log.meGot.Load())
		}
	}
}

// While the plan's window waits, the person leaving it (no input for 20
// seconds) tells the App once (op away), so the phone asks now.
func TestGateAwayWhileTheWindowWaits(t *testing.T) {
	g := &fakeGate{release: make(chan struct{}), phones: true, channels: []string{"window"}}
	var reads atomic.Int32
	stubIdle(t, func() (time.Duration, bool) {
		if reads.Add(1) < 4 {
			return time.Second, true
		}
		return 25 * time.Second, true
	})
	g.onAway = func() { close(g.release) } // then the phone approves
	windowConfirmStubs(t, nil, nil)
	windowRunner = func(ctx context.Context, _ windowText, _ time.Duration) windowAnswer {
		<-ctx.Done()
		return windowNoAnswer
	}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	_, _, sends := g.snapshot()
	g.mu.Lock()
	defer g.mu.Unlock()
	if exit != 0 || len(sends) != 1 || g.aways != 1 || g.presence[0] != "present" {
		t.Fatalf("exit %d %+v; aways %d presence %v sends %+v", exit, env.Error, g.aways, g.presence, sends)
	}
}

// A window that can't confirm after all (the App refused Confirm, or the
// window failed) asks the phone now; nothing is read where Linux has no
// idle time.
func TestGateWindowThatCantConfirmAsksThePhoneNow(t *testing.T) {
	for name, tc := range map[string]struct {
		windowOps []any
		confirm   windowAnswer
	}{
		"App refuses Confirm": {[]any{fakeRelayStatus{409, "GATE_WINDOW_OFF", "no"}}, windowConfirmed},
		"window failed":       {nil, windowNoAnswer},
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{release: make(chan struct{}), phones: true, channels: []string{"window"}, windowOps: tc.windowOps}
			stubIdle(t, func() (time.Duration, bool) { return 0, false })
			g.onAway = func() { close(g.release) }
			windowConfirmStubs(t, func(windowText) windowAnswer { return tc.confirm }, func(ctx context.Context, _ string) windowAnswer {
				<-ctx.Done()
				return windowNoAnswer
			})
			h := gatedHarness(t, newFakeHA(), g)
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			_, _, sends := g.snapshot()
			g.mu.Lock()
			defer g.mu.Unlock()
			if exit != 0 || len(sends) != 1 || g.aways != 1 || g.presence[0] != "unknown" {
				t.Fatalf("exit %d %+v; aways %d presence %v", exit, env.Error, g.aways, g.presence)
			}
		})
	}
}

func TestParseHIDIdle(t *testing.T) {
	out := "+-o IOHIDSystem  <class IOHIDSystem>\n    {\n      \"HIDIdleTime\" = 20942889708\n    }\n"
	if got, ok := parseHIDIdle(out); !ok || got != 20942889708*time.Nanosecond {
		t.Fatalf("%v %v", got, ok)
	}
	if _, ok := parseHIDIdle("no reading"); ok {
		t.Fatal("read an idle time from nothing")
	}
}
