package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Applying a batch (docs/work/2026-10-29-batch-confirmation-spec.md §4): it
// stops at the first member that does not go through, and the ledger says
// what went through, what stopped, and what was not attempted.

// batchFour: the plans that change a1 and s1 and delete a2 and s2.
func batchFour(h *writeHarness) []string {
	h.t.Helper()
	return []string{h.writePlan("automation", "a1", map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_on", "light.hall")}}),
		h.writePlan("script", "s1", arrival("light.turn_off")), h.member("delete", "automation.kitchen_motion_old"), h.member("delete", "script.old_arrival")}
}

// batchOfFour: batchFour as one typed batch with a chat code.
func batchOfFour(h *writeHarness) verbTestEnvelope {
	h.t.Helper()
	exit, env := h.batch(batchFour(h)...)
	if exit != 3 || env.Plan == nil || env.Plan.Code == "" {
		h.t.Fatalf("plan batch: exit %d env %+v", exit, env.Error)
	}
	return env
}

// writeThen stores config writes like Home Assistant and runs also when
// one reaches a path ending in suffix; reply replaces the answer there.
func writeThen(suffix string, also func(f *fakeHA), reply any) func(*fakeHA, string, map[string]any) any {
	return func(f *fakeHA, path string, body map[string]any) any {
		if strings.HasSuffix(path, suffix) {
			if also != nil {
				also(f)
			}
			if reply != nil {
				return reply
			}
		}
		f.storeConfig(path, body)
		return coreReply(200, map[string]any{"result": "ok"})
	}
}

// A member changed before anything was sent: nothing is sent, no copy is
// saved, and the batch is used up.
func TestBatchApplySendsNothingWhenAMemberChanged(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	env := batchOfFour(h)
	h.f.configs["/api/config/script/config/s1"] = arrival("light.toggle")
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if exit != 2 || applied.Error.Code != "BATCH_DRIFT" || applied.Say != "Not done: “Arrival” changed since you saw the list, so nothing was sent." ||
		len(h.writes()) != 0 || len(h.f.snapshots) != 0 || len(h.planFiles()) != 0 {
		t.Fatalf("exit %d env %+v writes %v files %v", exit, applied, h.writes(), h.planFiles())
	}
}

// A member that changed while the earlier ones were sent stops the batch
// right before its own send.
func TestBatchApplyStopsAtAMemberChangedMidway(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	env := batchOfFour(h)
	h.f.configWrite = writeThen("/s1", func(f *fakeHA) {
		f.configs["/api/config/automation/config/a2"] = map[string]any{"alias": "Kitchen motion old", "actions": []any{step("light.toggle", "light.kitchen")}}
	}, nil)
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	items := batchItems(t, applied)
	if exit != 6 || applied.Status != "failed" || applied.Error.Code != "BATCH_STOPPED" || itemStatuses(items) != "verified,verified,drift,not_attempted" || len(h.writes()) != 2 ||
		applied.Say != "Partly done: 2 of 4 went through; “Kitchen motion old” changed since you saw it, so I stopped. The rest weren't sent." {
		t.Fatalf("exit %d env %+v items %s writes %v", exit, applied, itemStatuses(items), h.writes())
	}
}

// Home Assistant refuses a member: the batch stops there, never retried.
func TestBatchApplyStopsWhereHomeAssistantRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		suffix, statuses, say string
		writes                int
	}{
		"the first":  {"/a1", "failed,not_attempted,not_attempted,not_attempted", "Not done: Home Assistant refused “Night mode”. Nothing changed. The rest weren't sent.", 1},
		"the second": {"/s1", "verified,failed,not_attempted,not_attempted", "Partly done: 1 of 4 went through; Home Assistant refused “Arrival”. The rest weren't sent.", 2},
	} {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, batchHome())
			env := batchOfFour(h)
			h.f.configWrite = writeThen(c.suffix, nil, coreReply(400, map[string]any{"message": "Message malformed"}))
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
			if items := batchItems(t, applied); exit != 6 || applied.Status != "failed" || itemStatuses(items) != c.statuses || applied.Say != c.say || len(h.writes()) != c.writes {
				t.Fatalf("exit %d say %q items %s writes %v", exit, applied.Say, itemStatuses(items), h.writes())
			}
		})
	}
}

// A member whose write may have landed ends the batch as unknown: read it,
// never send it again.
func TestBatchApplyOutcomeUnknownIsNeverRetried(t *testing.T) {
	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	h := newWriteHarness(t, batchHome())
	env := batchOfFour(h)
	h.f.configWrite = writeThen("/s1", func(*fakeHA) { time.Sleep(400 * time.Millisecond) }, nil)
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	items := batchItems(t, applied)
	if exit != 7 || !applied.OutcomeUnknown || applied.Error.Code != "OUTCOME_UNKNOWN" || itemStatuses(items) != "verified,unknown,not_attempted,not_attempted" || len(h.writes()) != 2 ||
		applied.Say != "Not sure: 1 of 4 went through, and “Arrival” may have too; I can't tell. I didn't try again. The rest weren't sent." {
		t.Fatalf("exit %d env %+v items %s writes %v", exit, applied, itemStatuses(items), h.writes())
	}
}

// A copy that can't be saved sends nothing and keeps the batch; only then
// may the user go on without copies, for the whole batch.
func TestBatchApplySnapshotFailureSendsNothing(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	env := batchOfFour(h)
	if exit, early := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code, "--no-snapshot"); exit != 4 || early.Error.Code != "NO_SNAPSHOT_NOT_OFFERED" {
		t.Fatalf("--no-snapshot first: exit %d %+v", exit, early.Error)
	}
	h.f.snapshotErr = true
	if exit, failed := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 1 || failed.Error.Code != "SNAPSHOT_FAILED" || len(h.writes()) != 0 {
		t.Fatalf("snapshot failure: exit %d %+v writes %v", exit, failed.Error, h.writes())
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code, "--no-snapshot")
	items := batchItems(t, applied)
	if exit != 0 || len(h.writes()) != 4 || items[2].Restore != "" || items[3].Snapshot != "" || applied.Say != "Done: all 4 went through. I checked each one. No copies were saved, as you chose." {
		t.Fatalf("without copies: exit %d env %+v items %+v", exit, applied, items)
	}
}

// The batch file is trusted only through its hash and its members' own:
// an edited tier, order, or member request sends nothing.
func TestBatchApplyNeverTrustsTheBatchFile(t *testing.T) {
	for name, c := range map[string]struct {
		edit func(map[string]any)
		code string
	}{
		"tier": {func(p map[string]any) { p["tier"] = "preview" }, "PLAN_INVALID"},
		"order": {func(p map[string]any) {
			m := p["batch_members"].([]any)
			m[0], m[1] = m[1], m[0]
		}, "PLAN_INVALID"},
		"a member's request": {func(p map[string]any) {
			m := p["batch_members"].([]any)[1].(map[string]any)
			m["config_request"].(map[string]any)["draft"] = map[string]any{"alias": "Arrival", "sequence": []any{step("lock.unlock", "lock.front")}}
		}, "BATCH_DRIFT"},
		"one member":   {func(p map[string]any) { p["batch_members"] = p["batch_members"].([]any)[:1] }, "PLAN_INVALID"},
		"the text":     {func(p map[string]any) { p["prompt"] = "I'm about to change nothing." }, "PLAN_INVALID"},
		"the language": {func(p map[string]any) { p["lang"] = "de" }, "PLAN_INVALID"},
		"a member's hash": {func(p map[string]any) {
			p["batch_members"].([]any)[2].(map[string]any)["hash"] = "sha256:" + strings.Repeat("0", 64)
		}, "BATCH_DRIFT"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, batchHome())
			env := batchOfFour(h)
			h.tamper(env.Plan.ID, c.edit)
			if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit == 0 || applied.Error == nil || applied.Error.Code != c.code || len(h.writes()) != 0 {
				t.Fatalf("exit %d env %+v writes %v", exit, applied.Error, h.writes())
			}
		})
	}
}

// The checks every plan gets: a wrong code counts, a batch another run
// holds or one that expired sends nothing, a window batch takes no code.
func TestBatchApplyKeepsTheSharedChecks(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	env := batchOfFour(h)
	if exit, wrong := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", wrongCode(env.Plan.Code)); exit != 4 || wrong.Error.Code != "CODE_MISMATCH" ||
		!strings.Contains(wrong.Error.Message, "1 of 3") {
		t.Fatalf("wrong code: exit %d %+v", exit, wrong.Error)
	}
	open := filepath.Join(planDir(h.paths), env.Plan.ID+".json")
	held := filepath.Join(planDir(h.paths), env.Plan.ID+".claimed-1")
	if err := os.Rename(open, held); err != nil {
		t.Fatal(err)
	}
	if exit, claimed := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 4 || claimed.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("held by another run: exit %d %+v", exit, claimed.Error)
	}
	if err := os.Rename(held, open); err != nil {
		t.Fatal(err)
	}
	previous := verbNow
	verbNow = func() time.Time { return previous().Add(planTTL + time.Second) }
	exit, expired := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	verbNow = previous
	if exit != 4 || expired.Error.Code != "PLAN_EXPIRED" || len(h.writes()) != 0 {
		t.Fatalf("expired: exit %d %+v writes %v", exit, expired.Error, h.writes())
	}
	useWindow(t)
	_, window := h.batch(batchFour(h)...)
	if env, exit := applyPlan(h.paths, verbOptions{}, window.Plan.ID, storedCode(t, h, window.Plan.ID), false); exit != 4 || env.Error.Code != "CONFIRM_IN_WINDOW" || len(h.writes()) != 0 {
		t.Fatalf("a window batch with its code: exit %d %+v", exit, env.Error)
	}
}
