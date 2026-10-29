package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
)

// How the phone's ask ends when the computer ends it (confirm_both.go): a
// stop on the computer and a window answer while the question is still on
// its way.

// Stopped on the computer (SIGTERM) while both ask: nothing is sent. The
// phone's question is cleared, unless the phone had answered first: then it
// hears that the computer stopped it, so the tap isn't left unanswered.
func TestBothAskStoppedOnTheComputer(t *testing.T) {
	if runtime.GOOS == "windows" || signal.Ignored(syscall.SIGTERM) {
		t.Skip("no catchable SIGTERM here")
	}
	held := make(chan os.Signal, 1) // the test's own listener: the signal never ends the test binary
	signal.Notify(held, syscall.SIGTERM)
	savedEnd, savedRunner := endBySignal, windowRunner
	t.Cleanup(func() { signal.Stop(held); endBySignal, windowRunner = savedEnd, savedRunner })
	stop := func() {
		self, _ := os.FindProcess(os.Getpid())
		_ = self.Signal(syscall.SIGTERM)
		<-held
		time.Sleep(50 * time.Millisecond) // delivered to the window's listener too
	}
	for _, phoneFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("phone answered first=%v", phoneFirst), func(t *testing.T) {
			h := newWriteHarness(t, newFakeHA())
			p := usePhones(t, "mobile_app_iphone")
			var ended os.Signal
			endBySignal = func(sig os.Signal) { ended = sig }
			windowAvailable = func() bool { return true }
			windowRunner = func(ctx context.Context, _, _ string, _ time.Duration) windowAnswer {
				if !phoneFirst {
					p.waitSent()
					stop()
				}
				<-ctx.Done() // withdrawn: by the stop, or by the phone's answer
				if phoneFirst {
					stop() // the stop comes before the window has closed
				}
				return windowNoAnswer
			}
			if phoneFirst {
				p.tapWhen(t, opened(), true)
			}
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_CANCELLED" || ended != syscall.SIGTERM || len(h.posts()) != 0 {
				t.Fatalf("exit %d err %+v ended %v posts %d", exit, env.Error, ended, len(h.posts()))
			}
			if !phoneFirst && !p.withdrawn() {
				t.Fatalf("the phone's question was not cleared alone: sent %v cleared %v", p.sent, p.cleared)
			}
			if last := p.last(); phoneFirst && (last["title"] != "Cancelled: Unlock “lock.front”" ||
				last["message"] != "Stopped on the computer. Nothing changed." || last["data"].(map[string]any)["actions"] != nil) {
				t.Fatalf("phone result %+v", last)
			}
		})
	}
}

// Confirmed in the window while the question is still on its way to the
// phone: the clear waits for it, so it can't arrive first and leave the
// question standing.
func TestWithdrawalWaitsForTheQuestionInFlight(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	p := usePhones(t, "mobile_app_iphone")
	gate := make(chan struct{})
	useRaceWindow(t, windowConfirmed, gate, false)
	fake, confirm := phoneNotify, sync.OnceFunc(func() { close(gate) })
	var mu sync.Mutex
	var order []string
	phoneNotify = func(s *relaySession, service string, body map[string]any) error {
		if data, _ := body["data"].(map[string]any); data["actions"] != nil {
			confirm() // the window confirms while the question is on its way
			time.Sleep(300 * time.Millisecond)
		}
		err := fake(s, service, body)
		mu.Lock()
		order = append(order, fmt.Sprint(body["message"]))
		mu.Unlock()
		return err
	}
	plan := h.plan("lock.unlock", "lock.front").Plan
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || env.Plan == nil || env.Plan.Channel != channelWindow || len(h.posts()) != 1 {
		t.Fatalf("exit %d plan %+v err %+v posts %d", exit, env.Plan, env.Error, len(h.posts()))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] == "clear_notification" || order[1] != "clear_notification" || !p.withdrawn() {
		t.Fatalf("sent in order %q, cleared %v: want the question, then its clear", order, p.cleared)
	}
}
