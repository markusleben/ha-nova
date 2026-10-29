package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Batch plans (docs/work/2026-10-29-batch-confirmation-spec.md): planning a
// batch, its refusals, and the claim of its members. Applying it:
// verb_apply_batch_test.go; its texts: confirm_batch_text_test.go.

// batchHome: two automations and two scripts the batch tests change.
func batchHome() *fakeHA {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_off", "light.hall")}})
	f.storeConfig("/api/config/automation/config/a2", map[string]any{"alias": "Kitchen motion old", "actions": []any{step("light.turn_on", "light.kitchen")}})
	f.storeConfig("/api/config/script/config/s1", map[string]any{"alias": "Arrival", "sequence": []any{step("light.turn_on", "light.hall")}})
	f.storeConfig("/api/config/script/config/s2", map[string]any{"alias": "Old arrival", "sequence": []any{step("light.turn_on", "light.kitchen")}})
	return f
}

// member plans one batch member and returns its plan id.
func (h *writeHarness) member(args ...string) string {
	h.t.Helper()
	_, env := h.run(runPlanCommand, args...)
	if env.Plan == nil {
		h.t.Fatalf("no plan for %v: %+v", args, env)
	}
	return env.Plan.ID
}

func (h *writeHarness) writePlan(domain, id string, draft map[string]any) string {
	h.t.Helper()
	args := []string{"write", "--domain", domain, "--file", draftFile(h.t, draft)}
	if id != "" {
		args = append(args, "--id", id)
	}
	return h.member(args...)
}

func (h *writeHarness) batch(ids ...string) (int, verbTestEnvelope) {
	h.t.Helper()
	args := []string{"batch"}
	for _, id := range ids {
		args = append(args, "--plan", id)
	}
	return h.run(runPlanCommand, args...)
}

func batchItems(t *testing.T, env verbTestEnvelope) []batchItem {
	t.Helper()
	var result struct {
		Items  []batchItem  `json:"items"`
		Counts *batchCounts `json:"counts"`
	}
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result %s: %v", env.Result, err)
	}
	return result.Items
}

func itemStatuses(items []batchItem) string {
	var out []string
	for _, item := range items {
		out = append(out, item.Status)
	}
	return strings.Join(out, ",")
}

func arrival(action string) map[string]any {
	return map[string]any{"alias": "Arrival", "sequence": []any{step(action, "light.hall")}}
}

// The four kinds of member in one list: one confirmation, every copy before
// the first send, one write each in order, and a ledger with the ways back.
func TestBatchMixedListAppliesInOrderWithOneConfirmation(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	ids := []string{h.writePlan("automation", "", hallDraft("light.turn_on")), h.writePlan("script", "s1", arrival("light.turn_off")),
		h.member("delete", "automation.kitchen_motion_old"), h.member("delete", "script.old_arrival")}
	exit, env := h.batch(ids...)
	if exit != 3 || env.Tier != "typed" || env.Plan == nil || !fourDigits.MatchString(env.Plan.Code) || len(h.writes()) != 0 {
		t.Fatalf("plan batch: exit %d env %+v", exit, env)
	}
	want := "I'm about to make these 4 changes in Home Assistant, in this order:\n1. Create the automation “Hall motion”.\n2. Change the script “Arrival”.\n3. Delete the automation “Kitchen motion old”.\n4. Delete the script “Old arrival”.\nOne confirmation covers all 4, so I need you to confirm it. A copy of each one deleted is saved first, so it can be restored. HA NOVA checks each one again right before it is sent and stops at the first one that doesn't go through. Nothing has changed yet.\n" + sayChatNote("en") + sayCodeBlock("en", env.Plan.Code)
	if env.Say != want {
		t.Fatalf("say:\n%s\nwant:\n%s", env.Say, want)
	}
	if files := h.planFiles(); len(files) != 1 || filepath.Base(files[0]) != env.Plan.ID+".json" {
		t.Fatalf("the members' own plans stay: %v", files)
	}
	if items := batchItems(t, env); len(items) != 4 || items[0].Op != "create" || items[1].Op != "update" || items[2].Op != "delete" || items[3].Tier != "typed" || items[1].Name != "Arrival" {
		t.Fatalf("items %+v", items)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	items := batchItems(t, applied)
	if exit != 0 || applied.Status != "verified" || applied.Say != "Done: all 4 went through. I checked each one." || itemStatuses(items) != "verified,verified,verified,verified" {
		t.Fatalf("apply: exit %d env %+v items %+v", exit, applied, items)
	}
	if items[1].Undo == nil || items[1].Undo.TargetID != "s1" || items[2].Restore == "" || items[3].Restore == "" || items[0].EntityID != "automation.hall_motion" {
		t.Fatalf("ways back %+v", items)
	}
	var order []string
	for _, c := range h.log.all() {
		if c.endpoint == "backups" || strings.HasPrefix(fmt.Sprint(c.payload["path"]), "/api/config/") && c.payload["method"] != "GET" {
			order = append(order, fmt.Sprint(c.endpoint, " ", c.payload["method"], " ", c.payload["path"]))
		}
	}
	if got := strings.Join(order, " | "); !strings.HasPrefix(got, "backups <nil> <nil> | backups <nil> <nil> | core POST /api/config/automation/config/") ||
		!strings.HasSuffix(got, "core POST /api/config/script/config/s1 | core DELETE /api/config/automation/config/a2 | core DELETE /api/config/script/config/s2") {
		t.Fatalf("order %s: every copy before the first write, then one write each in order", got)
	}
	if exit, again := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 4 || again.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("a second apply: exit %d %+v", exit, again.Error)
	}
}

// Every refusal names the member and hands every member plan back as it was.
func TestPlanBatchRefusesAndHandsTheMembersBack(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	updated := h.writePlan("automation", "a2", map[string]any{"alias": "Kitchen motion old", "actions": []any{step("light.turn_off", "light.kitchen")}})
	if exit, _ := h.run(runApplyCommand, "--plan", updated); exit != 0 {
		t.Fatal("update a2")
	}
	a1 := h.writePlan("automation", "a1", map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_on", "light.hall")}})
	s1 := h.writePlan("script", "s1", arrival("light.turn_off"))
	again := h.writePlan("automation", "a1", map[string]any{"alias": "Night mode", "actions": []any{step("light.toggle", "light.hall")}})
	deleted := h.member("delete", "script.old_arrival")
	call := h.plan("lock.unlock", "lock.front").Plan.ID
	revert := h.member("revert")
	expired := h.writePlan("script", "s2", map[string]any{"alias": "Old arrival", "sequence": []any{}})
	h.tamper(expired, func(p map[string]any) { p["expires_at"] = "2020-01-01T00:00:00Z" })
	other := h.writePlan("automation", "a2", map[string]any{"alias": "Kitchen motion old", "actions": []any{}})
	h.tamper(other, func(p map[string]any) { p["server"] = "other" })
	edited := h.member("delete", "automation.night_mode")
	h.tamper(edited, func(p map[string]any) { p["prompt"] = "I'm about to change nothing." })
	basis := h.writePlan("script", "s1", arrival("light.toggle"))
	h.tamper(basis, func(p map[string]any) { p["basis"].(map[string]any)["current"] = "absent" })
	var many []string
	for i := range 21 {
		many = append(many, fmt.Sprintf("p_%012x", i))
	}
	before := h.planFiles()
	for name, c := range map[string]struct {
		ids  []string
		code string
	}{
		"one plan": {[]string{a1}, "BATCH_SIZE"}, "21 plans": {many, "BATCH_SIZE"},
		"a plan twice": {[]string{a1, s1, a1}, "BATCH_DUPLICATE"}, "an item twice": {[]string{a1, again}, "BATCH_DUPLICATE"},
		"a missing plan": {[]string{a1, "p_000000000000"}, "PLAN_NOT_FOUND"}, "a call": {[]string{a1, call}, "BATCH_UNSUPPORTED"},
		"a revert": {[]string{revert, s1}, "BATCH_UNSUPPORTED"}, "expired": {[]string{s1, expired}, "PLAN_EXPIRED"},
		"another server": {[]string{other, s1}, "SERVER_MISMATCH"}, "an edited typed plan": {[]string{deleted, edited}, "PLAN_INVALID"},
		"an edited preview plan": {[]string{a1, basis}, "PLAN_INVALID"},
	} {
		exit, env := h.batch(c.ids...)
		if exit != 4 || env.Error == nil || env.Error.Code != c.code || env.Plan != nil {
			t.Errorf("%s: exit %d env %+v", name, exit, env.Error)
		}
		if got := h.planFiles(); strings.Join(got, ",") != strings.Join(before, ",") {
			t.Errorf("%s: the member plans were not handed back: %v", name, got)
		}
	}
	if exit, env := h.run(runPlanCommand, "batch", a1, s1); exit != 1 || env.Error.Code != "USAGE" {
		t.Errorf("ids without --plan: exit %d %+v", exit, env.Error)
	}
	if len(h.writes()) != 1 {
		t.Fatalf("plan batch wrote: %v", h.writes())
	}
}

// A member that changed since its preview is named; nothing is planned,
// and every member plan stays.
func TestPlanBatchRefusesAMemberChangedSinceItsPreview(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	a1 := h.writePlan("automation", "a1", map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_on", "light.hall")}})
	s1 := h.writePlan("script", "s1", arrival("light.turn_off"))
	h.f.configs["/api/config/script/config/s1"] = arrival("light.toggle")
	exit, env := h.batch(a1, s1)
	if exit != 2 || env.Error.Code != "PLAN_DRIFT" || !strings.Contains(env.Error.Message, "“Arrival” changed") || len(h.planFiles()) != 2 {
		t.Fatalf("exit %d env %+v files %v", exit, env.Error, h.planFiles())
	}
}

// A member that checks another member's item runs before it.
func TestPlanBatchRunsWhatChecksAnotherFirst(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	caller := h.writePlan("automation", "a1", map[string]any{"alias": "Night mode", "actions": []any{step("script.turn_on", "script.arrival")}})
	script := h.writePlan("script", "s1", arrival("light.turn_off"))
	exit, env := h.batch(script, caller)
	if exit != 4 || env.Error.Code != "BATCH_ORDER" || !strings.Contains(env.Error.Message, "move “Night mode” before “Arrival”") {
		t.Fatalf("exit %d env %+v", exit, env.Error)
	}
	if exit, env := h.batch(caller, script); exit != 3 || env.Plan == nil {
		t.Fatalf("in order: exit %d env %+v", exit, env.Error)
	}
}

// The members move into the batch: none can be applied alone or join a
// second batch, and one held by another run refuses the batch.
func TestPlanBatchClaimsItsMembers(t *testing.T) {
	h := newWriteHarness(t, batchHome())
	a1 := h.writePlan("automation", "a1", map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_on", "light.hall")}})
	s1 := h.writePlan("script", "s1", arrival("light.turn_off"))
	if exit, env := h.batch(a1, s1); exit != 3 || env.Plan == nil {
		t.Fatalf("batch: exit %d %+v", exit, env.Error)
	}
	if exit, env := h.run(runApplyCommand, "--plan", a1); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("a member alone: exit %d %+v", exit, env.Error)
	}
	if exit, env := h.batch(a1, s1); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("a second batch: exit %d %+v", exit, env.Error)
	}
	a2 := h.writePlan("automation", "a2", map[string]any{"alias": "Kitchen motion old", "actions": []any{}})
	s2 := h.writePlan("script", "s2", map[string]any{"alias": "Old arrival", "sequence": []any{}})
	held := filepath.Join(planDir(h.paths), a2+".claimed-1")
	if err := os.Rename(filepath.Join(planDir(h.paths), a2+".json"), held); err != nil {
		t.Fatal(err)
	}
	if exit, env := h.batch(s2, a2); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("a held member: exit %d %+v", exit, env.Error)
	}
	if _, err := os.Stat(filepath.Join(planDir(h.paths), s2+".json")); err != nil {
		t.Fatalf("the other member was not handed back: %v", err)
	}
	if len(h.writes()) != 0 {
		t.Fatalf("writes %v", h.writes())
	}
}

// Twenty updates: planned next to another open plan, all twenty stay; one
// natural yes applies them; every one of them stays revertible.
func TestPlanBatchOfTwentyUpdatesStaysRevertible(t *testing.T) {
	f := newFakeHA()
	for i := 1; i <= 20; i++ {
		f.storeConfig(fmt.Sprintf("/api/config/automation/config/b%d", i), map[string]any{"alias": fmt.Sprintf("Batch %d", i), "actions": []any{step("light.turn_on", "light.hall")}})
	}
	h := newWriteHarness(t, f)
	h.plan("lock.unlock", "lock.front")
	var ids []string
	for i := 1; i <= 20; i++ {
		ids = append(ids, h.writePlan("automation", fmt.Sprintf("b%d", i), map[string]any{"alias": fmt.Sprintf("Batch %d", i), "actions": []any{step("light.turn_off", "light.hall")}}))
	}
	exit, env := h.batch(ids...)
	if exit != 3 || env.Tier != "preview" || env.Plan.Confirm != "natural" || !strings.HasSuffix(env.Say, "Save all 20? I can also show the details.") {
		t.Fatalf("plan batch: exit %d env %+v", exit, env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if exit != 0 || len(h.writes()) != 20 || itemStatuses(batchItems(t, applied)) != strings.TrimSuffix(strings.Repeat("verified,", 20), ",") {
		t.Fatalf("apply: exit %d env %+v writes %d", exit, applied.Error, len(h.writes()))
	}
	stack, err := loadUndoSnapshotStack(h.paths)
	if err != nil || len(stack.Snapshots) != 20 {
		t.Fatalf("undo stack %d %v", len(stack.Snapshots), err)
	}
	if exit, reverted := h.run(runPlanCommand, "revert", "--target", "b1"); exit != 3 || reverted.Config == nil || reverted.Config.ConfigID != "b1" {
		t.Fatalf("revert the first: exit %d env %+v", exit, reverted.Error)
	}
}
