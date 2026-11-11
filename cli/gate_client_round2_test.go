package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// Second review round for the CLI on a gated App (gate_client.go).

// gate_seen that can't be saved fails closed: the typed plan is refused,
// nothing is planned or sent, and the next attempt checks again.
func TestGateSeenThatCantBeSavedRefuses(t *testing.T) {
	g := &fakeGate{}
	h := gatedHarness(t, newFakeHA(), g)
	if err := os.Chmod(h.paths.ConfigDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(h.paths.ConfigDir, 0o700) })
	_, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
	if _, _, sends := g.snapshot(); env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || !strings.Contains(env.Error.Next, "settings file") ||
		env.Plan != nil || len(h.planFiles()) != 0 || len(sends) != 0 || gateSeen(h.paths, activeServerProfile()) {
		t.Fatalf("unsaved gate_seen: %+v", env)
	}
	if err := os.Chmod(h.paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelRelay || !gateSeen(h.paths, activeServerProfile()) {
		t.Fatalf("saved on the next try: %+v", plan)
	}
}

// Opening an ask has its own time: longer than one poll (the worker may
// prepare many requests), but never past the command's own wait, after
// which nothing is sent.
func TestGateOpenHasItsOwnTime(t *testing.T) {
	saved, savedStart := gatePollTimeout, commandStart
	t.Cleanup(func() { gatePollTimeout, commandStart = saved, savedStart })
	gatePollTimeout = 100 * time.Millisecond
	t.Run("longer than a poll", func(t *testing.T) {
		commandStart = time.Time{}
		g := &fakeGate{openDelay: 500 * time.Millisecond}
		h := gatedHarness(t, newFakeHA(), g)
		plan := h.plan("lock.unlock", "lock.front").Plan
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 {
			t.Fatalf("exit %d %+v", exit, env.Error)
		}
	})
	t.Run("past the command's wait", func(t *testing.T) {
		g := &fakeGate{openDelay: time.Minute}
		h := gatedHarness(t, newFakeHA(), g)
		plan := h.plan("lock.unlock", "lock.front").Plan
		commandStart = time.Now().Add(-(windowWait - time.Second))
		start := time.Now()
		exit, env := h.run(runApplyCommand, "--plan", plan.ID)
		if _, _, sends := g.snapshot(); exit != verbExitRefused || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" ||
			!strings.Contains(env.Error.Message, "nothing was sent") || len(sends) != 0 || time.Since(start) > 15*time.Second {
			t.Fatalf("exit %d %+v after %v", exit, env.Error, time.Since(start))
		}
	})
}

// A grant expires two minutes after the approval: a later batch member the
// Relay then holds is asked for once more (a person approves again) and
// sent once with the new grant; declined, it and the rest are not sent.
func TestGateAsksAgainWhenAGrantExpired(t *testing.T) {
	for _, approve := range []bool{true, false} {
		t.Run(map[bool]string{true: "approved", false: "declined"}[approve], func(t *testing.T) {
			g := &fakeGate{hold: func(send gateSend) any {
				if strings.Contains(send.key, "/api/config/script/config/s2") && send.grant == "grant-ask1" {
					return fakeRelayStatus{428, "GATE_APPROVAL_REQUIRED", "grant expired"}
				}
				return nil
			}}
			if !approve {
				g.endFor = map[string]string{"ask2": "cancelled"}
			}
			h := gatedHarness(t, batchHome(), g)
			unlock := func(alias string) map[string]any {
				return map[string]any{"alias": alias, "sequence": []any{step("lock.unlock", "lock.front")}}
			}
			_, env := h.batch(h.writePlan("script", "s1", unlock("Arrival")), h.writePlan("script", "s2", unlock("Old arrival")))
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			asks, _, sends := g.snapshot()
			var grants []string
			for _, send := range sends {
				grants = append(grants, send.grant)
			}
			items := batchItems(t, applied)
			want, second, status := "grant-ask1,grant-ask1,grant-ask2", "verified", 0
			if !approve {
				want, second, status = "grant-ask1,grant-ask1", "not_sent", exit
			}
			if exit != status || len(asks) != 2 || len(asks[1]) != 1 || strings.Join(grants, ",") != want || len(items) != 2 ||
				items[0].Status != "verified" || items[1].Status != second || !slices.Contains(asks[1], sends[1].key) {
				t.Fatalf("exit %d %+v; asks %q grants %s items %+v", exit, applied.Error, asks, strings.Join(grants, ","), items)
			}
			if !approve && (exit == 0 || !strings.Contains(applied.Error.Next, gateEnded("cancelled").next)) {
				t.Fatalf("declined: exit %d %+v", exit, applied.Error)
			}
		})
	}
}

// A gated plan whose checks spend the whole read budget (LIMIT_CUT on a large
// home or a slow Link) still asks through the Relay, in plan and in apply:
// the gate read has a time of its own, so a profile that saw the gate never
// refuses with CONFIRM_UNAVAILABLE for want of read time.
func TestGateReadAfterChecksSpentTheReadBudget(t *testing.T) {
	g := &fakeGate{}
	h := gatedHarness(t, newFakeHA(), g)
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelRelay || !gateSeen(h.paths, activeServerProfile()) {
		t.Fatalf("gate not seen: %+v", plan)
	}
	previous, previousDeadline := relaySessionOverallTimeout, checkDeadline
	relaySessionOverallTimeout, checkDeadline = 300*time.Millisecond, time.Now() // each check reads a session budget, as after a wait
	t.Cleanup(func() { relaySessionOverallTimeout, checkDeadline = previous, previousDeadline })
	h.f.hang["/api/events"] = true
	_, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
	if env.Plan == nil || env.Plan.Channel != channelRelay || !strings.Contains(reasonCodes(env), reasonLimitCut) {
		t.Fatalf("plan: %+v reasons %s", env.Error, reasonCodes(env))
	}
	exit, env := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if asks, _, sends := g.snapshot(); exit != 0 || env.Status != "verified" || len(asks) != 1 || len(sends) != 1 {
		t.Fatalf("apply: exit %d %s %+v asks %q sends %+v", exit, env.Status, env.Error, asks, sends)
	}
}
