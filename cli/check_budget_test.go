package main

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// #1009: a check reads until its call's deadline, not until the 30 s
// session budget, and a check that deadline still cuts says it ran out of
// time and stays typed. A complete check lets the update run as an
// ordinary one (calmUpdate).

func TestACheckReadsUntilTheCallsDeadline(t *testing.T) {
	h := newSlowHarness(t, presenceHome(), 20*time.Millisecond, 150*time.Millisecond)
	if env := h.planLeave(); !strings.Contains(reasonCodes(env), reasonLimitCut) {
		t.Fatalf("the session budget alone: reasons %s, want the check cut", reasonCodes(env))
	}
	withCheckDeadline(t, afterCheck+10*time.Second)
	env := h.planLeave()
	if env.Tier != "ordinary" || !env.Coverage.Complete || len(env.Consumers) != 22 || strings.Contains(reasonCodes(env), reasonLimitCut) {
		t.Fatalf("with the call's deadline: tier %s coverage %+v consumers %d reasons %s", env.Tier, env.Coverage, len(env.Consumers), reasonCodes(env))
	}
}

func TestACheckTheDeadlineCutsSaysItRanOutOfTime(t *testing.T) {
	h := newSlowHarness(t, presenceHome(), 20*time.Millisecond, 150*time.Millisecond)
	withCheckDeadline(t, afterCheck+200*time.Millisecond)
	env := h.planLeave()
	if env.Tier != "typed" || !strings.Contains(env.Say, "ran out of time") || strings.Contains(env.Say, "can't check everything") {
		t.Fatalf("a check the deadline cut: tier %s say %q", env.Tier, env.Say)
	}
}

func withCheckDeadline(t *testing.T, d time.Duration) {
	t.Helper()
	saved := checkDeadline
	checkDeadline = time.Now().Add(d)
	t.Cleanup(func() { checkDeadline = saved })
}

// A check the deadline cut still seals its plan in Home Assistant's
// language: the language read gets the reserve left after the check, so
// the say isn't English for a German home.
func TestACheckTheDeadlineCutsStillSpeaksTheHomesLanguage(t *testing.T) {
	f := presenceHome()
	f.config = map[string]any{"language": "de"}
	h := newSlowHarness(t, f, 20*time.Millisecond, 150*time.Millisecond)
	withCheckDeadline(t, afterCheck+200*time.Millisecond)
	env := h.planLeave()
	if env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonLimitCut) || !strings.Contains(env.Say, confirmT("de", "unseen.out_of_time")) {
		t.Fatalf("a check the deadline cut in a German home: tier %s reasons %s say %q", env.Tier, reasonCodes(env), env.Say)
	}
}

// #827 on a gated App: before a person's ask, a cut plan's check ends
// where the replay, the gate read or phone lookup, and the person's
// windowMinWait still fit in the call's ask. An uncut plan, a job's ask
// (its own life), and a call with no time for a capped check (MCP's 40 s
// window) read to the call's deadline as before.
func TestACutPlansCheckBeforeAnAskLeavesTheAskItsTime(t *testing.T) {
	paths, _ := setupVerbRelayTest(t, newFakeHA().reply)
	savedStart, savedDeadline, savedWait, savedApproval := commandStart, checkDeadline, windowWait, gateApproval
	t.Cleanup(func() {
		commandStart, checkDeadline, windowWait, gateApproval = savedStart, savedDeadline, savedWait, savedApproval
	})
	cut := storedPlan{Reads: map[string]bool{"core {}": true}, Reasons: []tierReason{{Code: reasonLimitCut, Detail: "read budget"}}}
	var s *relaySession
	until := func(stored storedPlan) time.Time { // one call's session
		var deadline time.Time
		var err error
		if s, err = newRelaySession(paths, verbOptions{}); err != nil {
			t.Fatal(err)
		}
		defer s.close()
		_, _ = recheckBeforeAsk(s, &verbEnvelope{}, stored, func(*verbEnvelope) (storedPlan, error) {
			s.readUntilDeadline()
			deadline, _ = s.ctx.Deadline()
			return storedPlan{}, errors.New("checked")
		})
		return deadline
	}
	commandStart, windowWait = time.Now(), cliWindowWait
	checkDeadline = commandStart.Add(cliCheckBudget)
	want := commandStart.Add(30 * time.Second)
	if got := until(cut); !got.Equal(want) {
		t.Fatalf("a cut plan's check ends %v after the start, want %v", got.Sub(commandStart), want.Sub(commandStart))
	}
	if got := until(storedPlan{Kind: "batch", BatchMembers: []storedPlan{{}, cut}}); !got.Equal(want) {
		t.Fatalf("a batch with a cut member: the check ends %v after the start, want %v", got.Sub(commandStart), want.Sub(commandStart))
	}
	if !s.askCeiling.IsZero() {
		t.Fatal("the ceiling outlived the check")
	}
	uncapped := func(name string) {
		t.Helper()
		if got := until(cut); !got.Equal(checkDeadline.Add(-afterCheck)) {
			t.Fatalf("%s: the check ends %v after the start, want the call's deadline", name, got.Sub(commandStart))
		}
	}
	if got := until(storedPlan{}); !got.Equal(checkDeadline.Add(-afterCheck)) {
		t.Fatalf("an uncut plan's check ends %v after the start, want the call's deadline", got.Sub(commandStart))
	}
	gateApproval = new(atomic.Int32)
	uncapped("a job's ask")
	gateApproval = nil
	commandStart, windowWait = time.Now(), mcpWindowWait
	checkDeadline = commandStart.Add(mcpCheckBudget)
	uncapped("MCP's window")
}

// With none of the call's ask left after the check, the gate's ask is
// never opened: a clear refusal, nothing asked or sent.
func TestGateAskWithoutTimeLeftRefuses(t *testing.T) {
	g := &fakeGate{}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	saved := commandStart
	t.Cleanup(func() { commandStart = saved })
	commandStart = time.Now().Add(-(windowWait + time.Second))
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if asks, _, sends := g.snapshot(); exit != verbExitRefused || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" ||
		!strings.Contains(env.Error.Message, "took the time this call has to ask") || len(asks)+len(sends) != 0 {
		t.Fatalf("exit %d %+v asks %q sends %+v", exit, env.Error, asks, sends)
	}
}
