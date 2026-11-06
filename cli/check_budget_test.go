package main

import (
	"strings"
	"testing"
	"time"
)

// #1009: a check reads until its call's deadline, not until the 30 s
// session budget, and a check that deadline still cuts says it ran out of
// time and stays typed.

func TestACheckReadsUntilTheCallsDeadline(t *testing.T) {
	h := newSlowHarness(t, presenceHome(), 20*time.Millisecond, 150*time.Millisecond)
	if env := h.planLeave(); !strings.Contains(reasonCodes(env), reasonLimitCut) {
		t.Fatalf("the session budget alone: reasons %s, want the check cut", reasonCodes(env))
	}
	withCheckDeadline(t, afterCheck+10*time.Second)
	env := h.planLeave()
	if env.Tier != "preview" || !env.Coverage.Complete || len(env.Consumers) != 22 || strings.Contains(reasonCodes(env), reasonLimitCut) {
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
