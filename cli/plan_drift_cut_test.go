package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Cut-plan drift (docs/work/2026-11-03-cut-plan-drift-spec.md): an update
// of an automation whose helper many automations react to. A read budget
// that cut the impact check at another point each run made every apply
// drift, and a blueprint that ran out of budget read as unresolved.

// presenceHome: input_boolean.presence starts 11 automations (p00–p10, p00
// through a blueprint); each turns on a room light whose own automation
// (l00–l10) turns on a lamp. The draft "leave" switches presence off.
func presenceHome() *fakeHA {
	f := newFakeHA()
	f.ent("input_boolean.presence", "input_boolean", "on", map[string]any{"friendly_name": "Presence"})
	var consumers []string
	blueprints := map[string]any{}
	for i := range 11 {
		room, lamp := fmt.Sprintf("light.room_%02d", i), fmt.Sprintf("light.lamp_%02d", i)
		f.ent(room, "hue", "off", map[string]any{"_di": fmt.Sprintf("dev_%02d", i)})
		f.ent(lamp, "hue", "off", nil)
		p := map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.presence", "to": "off"}}, "actions": []any{step("light.turn_on", room)}}
		if i == 0 {
			blueprints["presence/away.yaml"] = p
			p = map[string]any{"use_blueprint": map[string]any{"path": "presence/away.yaml", "input": map[string]any{"light": room}}}
		}
		f.automation(fmt.Sprintf("presence_%02d", i), fmt.Sprintf("p%02d", i), p)
		consumers = append(consumers, fmt.Sprintf("automation.presence_%02d", i))
		f.automation(fmt.Sprintf("lamp_%02d", i), fmt.Sprintf("l%02d", i), map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": room}}, "actions": []any{step("light.turn_on", lamp)}})
		f.related["entity:"+room] = []string{fmt.Sprintf("automation.lamp_%02d", i)}
	}
	f.related["entity:input_boolean.presence"] = consumers
	f.extra = func(_ string, p map[string]any) (any, bool) {
		if p["type"] != "blueprint/substitute" || f.fail["blueprint/substitute"] {
			return nil, false
		}
		return map[string]any{"substituted_config": blueprints[fmt.Sprint(p["path"])]}, true
	}
	before := leaveDraft()
	before["actions"] = []any{} // the draft adds its step: an update weighs what it changes
	f.storeConfig("/api/config/automation/config/leave", before)
	return f
}

func leaveDraft() map[string]any {
	return map[string]any{"alias": "Leaving", "triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.door"}},
		"actions": []any{step("input_boolean.turn_off", "input_boolean.presence")}}
}

// slowHarness answers every read after latency; hangFirst makes the next n
// reads of an automation's loaded config hang until their request ends (the
// read budget); hung takes a core path, "related:" + an entity id, or
// "automation/config " + an entity id.
type slowHarness struct {
	*writeHarness
	mu   sync.Mutex
	hung map[string]int
}

func newSlowHarness(t *testing.T, f *fakeHA, latency, budget time.Duration) *slowHarness {
	fastVerify(t)
	f.service = applies
	h := &slowHarness{hung: map[string]int{}}
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		h.mu.Lock()
		path := fmt.Sprint(p["path"])
		switch p["type"] {
		case "search/related":
			path = "related:" + fmt.Sprint(p["item_id"])
		case "automation/config":
			path = "automation/config " + fmt.Sprint(p["entity_id"])
		}
		n := h.hung[path]
		if n > 0 {
			h.hung[path] = n - 1
		}
		h.mu.Unlock()
		if n > 0 {
			<-r.Context().Done()
			return nil
		}
		time.Sleep(latency)
		return f.reply(r, endpoint, p)
	}, true)
	h.writeHarness = &writeHarness{t: t, f: f, paths: paths, log: log}
	saved := relaySessionOverallTimeout
	relaySessionOverallTimeout = budget
	t.Cleanup(func() { relaySessionOverallTimeout = saved })
	return h
}

func (h *slowHarness) hangFirst(config string, n int) {
	entity := h.automationOf(config)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hung["automation/config "+entity] = n
}

// automationOf: the entity of the automation with config id config.
func (h *slowHarness) automationOf(config string) string {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	for id, st := range h.f.states {
		if attrs, _ := st["attributes"].(map[string]any); strings.HasPrefix(id, "automation.") && attrs["id"] == config {
			return id
		}
	}
	h.t.Fatalf("no automation with config id %q", config)
	return ""
}

func (h *slowHarness) configReads(config string) int {
	entity, n := h.automationOf(config), 0
	for _, c := range h.log.all() {
		if c.payload["type"] == "automation/config" && c.payload["entity_id"] == entity {
			n++
		}
	}
	return n
}

func (h *slowHarness) planLeave() verbTestEnvelope {
	h.t.Helper()
	return h.plan("write", "--domain", "automation", "--id", "leave", "--file", draftFile(h.t, leaveDraft()))
}

func (h *slowHarness) plan(args ...string) verbTestEnvelope {
	h.t.Helper()
	_, env := h.run(runPlanCommand, args...)
	if env.Plan == nil {
		h.t.Fatalf("no plan: %+v", env)
	}
	return env
}

// The walk reads breadth first: over a link with 20 ms per read it finishes
// well inside a budget the depth-first walk (about 90 reads in a row) ran
// out of, so each run is the same full check, not a cut at a new point.
func TestRepeatedPlansOverASlowLinkAreTheSamePlan(t *testing.T) {
	h := newSlowHarness(t, presenceHome(), 20*time.Millisecond, 1200*time.Millisecond)
	var hashes []string
	for range 3 {
		env := h.planLeave()
		if env.Tier != "preview" || env.Coverage == nil || !env.Coverage.Complete || len(env.Consumers) != 22 {
			t.Fatalf("tier %s coverage %+v consumers %d reasons %s", env.Tier, env.Coverage, len(env.Consumers), reasonCodes(env))
		}
		hashes = append(hashes, env.Plan.Hash)
	}
	if hashes[0] != hashes[1] || hashes[1] != hashes[2] {
		t.Fatalf("hashes %v", hashes)
	}
}

// Markus's flow: the plan's check is cut (p05 never answers), so it asks in
// the window; the check before the window and the one before the save are
// cut elsewhere (l05) and read everything the plan read, unchanged. The
// automation is saved once.
func TestCutChecksThatGotFurtherStillApply(t *testing.T) {
	w := useWindow(t, windowConfirmed)
	h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
	h.hangFirst("p05", 1)
	planned := h.planLeave()
	if planned.Tier != "typed" || planned.Plan.Channel != channelWindow || !strings.Contains(reasonCodes(planned), reasonLimitCut) {
		t.Fatalf("setup: tier %s plan %+v reasons %s", planned.Tier, planned.Plan, reasonCodes(planned))
	}
	h.hangFirst("l05", 2) // the window's check and apply's
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != 0 || len(w.messages) != 1 || len(h.writes()) != 1 {
		t.Fatalf("exit %d windows %d writes %d env %+v", exit, len(w.messages), len(h.writes()), env)
	}
}

// A recompute cut before it read everything the plan read gets a second
// pass on a fresh budget with the answers it has, so it is not drift only
// because it stopped earlier.
func TestACheckCutShorterThanThePlanReadsOnceMore(t *testing.T) {
	h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
	h.hangFirst("l05", 1)
	planned := h.planLeave()
	if planned.Plan.Code == "" {
		t.Fatalf("setup: %+v", planned)
	}
	h.hangFirst("p05", 1) // apply's first pass misses p05 and all it sets off
	h.hangFirst("l05", 1) // its second pass stops where the plan's did
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != 0 || len(h.writes()) != 1 || h.configReads("p05") != 3 {
		t.Fatalf("exit %d writes %d p05 reads %d env %+v", exit, len(h.writes()), h.configReads("p05"), env)
	}
}

// What both checks read must be what it was, and what only the recompute
// read must add no reason: a changed consumer, or a new one that unlocks a
// door, is drift, cut or not. Nothing is asked or saved.
func TestCutChecksStillSeeARealChange(t *testing.T) {
	for name, change := range map[string]func(f *fakeHA){
		"a consumer both read changed": func(f *fakeHA) {
			f.configs["/api/config/automation/config/p03"] = map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.presence", "to": "off"}}, "actions": []any{step("light.turn_off", "light.room_03")}}
		},
		"one only the recompute read now unlocks a door": func(f *fakeHA) {
			f.configs["/api/config/automation/config/p05"] = map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.presence", "to": "off"}}, "actions": []any{step("lock.unlock", "lock.front")}}
		},
		"a new consumer only the recompute read unlocks a door": func(f *fakeHA) {
			f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.presence"}}, "actions": []any{step("lock.unlock", "lock.front")}})
			f.related["entity:input_boolean.presence"] = append(f.related["entity:input_boolean.presence"], "automation.door")
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := useWindow(t) // no window may open
			h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
			h.hangFirst("p05", 1)
			planned := h.planLeave()
			if planned.Plan.Channel != channelWindow {
				t.Fatalf("setup: %+v", planned.Plan)
			}
			h.f.mu.Lock()
			change(h.f)
			h.f.mu.Unlock()
			h.hangFirst("l05", 4)
			exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
			if exit != verbExitDrift || len(w.messages) != 0 || len(h.writes()) != 0 {
				t.Fatalf("exit %d windows %d writes %d say %q", exit, len(w.messages), len(h.writes()), env.Say)
			}
		})
	}
}

// A blueprint substitution the read budget cut is a cut check, never an
// unresolved blueprint of that automation (a new reason at every run);
// one Home Assistant can't substitute still is.
func TestABlueprintCutByTheBudgetIsACut(t *testing.T) {
	f := presenceHome()
	f.hang["blueprint/substitute"] = true
	h := newSlowHarness(t, f, 0, 300*time.Millisecond)
	if codes := reasonCodes(h.planLeave()); strings.Contains(codes, reasonBlueprint) || !strings.Contains(codes, reasonLimitCut) {
		t.Fatalf("reasons %s", codes)
	}
	f.hang = map[string]bool{}
	f.fail["blueprint/substitute"] = true
	if codes := reasonCodes(h.planLeave()); !strings.Contains(codes, reasonBlueprint) {
		t.Fatalf("reasons %s", codes)
	}
}

// A recompute the read budget cut can have the preview's hash while it
// missed a read the preview had: here a lamp's consumers, read as none at
// the preview, that now unlock a door. That is drift, never the confirmed
// plan.
func TestACutRecomputeThatMissedAReadIsDrift(t *testing.T) {
	w := useWindow(t) // no window may open
	h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
	h.mu.Lock()
	h.hung["related:light.lamp_10"] = 1
	h.mu.Unlock()
	planned := h.planLeave()
	h.f.mu.Lock()
	h.f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.lamp_03"}}, "actions": []any{step("lock.unlock", "lock.front")}})
	h.f.related["entity:light.lamp_03"] = []string{"automation.door"}
	h.f.mu.Unlock()
	h.mu.Lock()
	h.hung["related:light.lamp_10"], h.hung["related:light.lamp_03"] = 10, 1 // the recompute misses both
	h.mu.Unlock()
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != verbExitDrift || len(w.messages) != 0 || len(h.writes()) != 0 {
		t.Fatalf("exit %d windows %d writes %d say %q err %+v", exit, len(w.messages), len(h.writes()), env.Say, env.Error)
	}
}

// A recompute with the preview's hash is replayed too: the listener scan's
// reads never enter the basis, so a listener added where the recompute's
// scan was cut shows only in the replay.
func TestACutRecomputeThatMissedANewListenerIsDrift(t *testing.T) {
	h := cutHome(t)
	h.cutAt("a10")
	_, planned := h.run(runCallCommand, appInstall...)
	if planned.Plan == nil || planned.Plan.Code == "" {
		t.Fatalf("setup: %+v", planned)
	}
	h.f.mu.Lock()
	h.f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service"}}, "actions": []any{step("lock.unlock", "lock.front")}})
	h.f.events = []map[string]any{{"event": "call_service", "listener_count": 2.0}}
	h.f.mu.Unlock()
	h.cutAt("door") // the recompute's scan is cut before the new listener
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != verbExitDrift || len(h.posts()) != 0 {
		t.Fatalf("exit %d posts %d say %q", exit, len(h.posts()), env.Say)
	}
}

// A replay that reaches what the preview had no answer to by a new way
// (here a group that gained the lamp the preview stopped before: its
// automation's config never answered) is drift.
func TestANewRouteToAReadThePreviewMissedIsDrift(t *testing.T) {
	w := useWindow(t) // no window may open
	f := presenceHome()
	f.ent("group.garage", "group", "on", map[string]any{"entity_id": []any{"light.lamp_09"}})
	f.automation("lamp_03", "l03", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.room_03"}}, "actions": []any{step("light.turn_on", "light.lamp_03"), step("light.turn_on", "group.garage")}})
	h := newSlowHarness(t, f, 0, 300*time.Millisecond)
	h.hangFirst("l10", 1)
	planned := h.planLeave()
	h.f.mu.Lock()
	h.f.ent("group.garage", "group", "on", map[string]any{"entity_id": []any{"light.lamp_09", "light.lamp_10"}})
	h.f.mu.Unlock()
	h.hangFirst("l10", 10)
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != verbExitDrift || len(w.messages) != 0 || len(h.writes()) != 0 {
		t.Fatalf("exit %d windows %d writes %d say %q err %+v", exit, len(w.messages), len(h.writes()), env.Say, env.Error)
	}
}

// A consumer added where a cut preview looked is drift, even when the
// recompute that reads it is cut before it can tell what that consumer does.
func TestACutCheckSeesANewConsumerWhereThePreviewLooked(t *testing.T) {
	w := useWindow(t) // no window may open
	h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
	h.hangFirst("l05", 1)
	planned := h.planLeave()
	if planned.Plan.Channel != channelWindow {
		t.Fatalf("setup: %+v", planned.Plan)
	}
	h.f.mu.Lock()
	h.f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.room_03"}}, "actions": []any{step("lock.unlock", "lock.front")}})
	h.f.related["entity:light.room_03"] = append(h.f.related["entity:light.room_03"], "automation.door")
	h.f.mu.Unlock()
	h.hangFirst("door", 4) // every check: the door automation stays unread
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != verbExitDrift || len(w.messages) != 0 || len(h.writes()) != 0 {
		t.Fatalf("exit %d windows %d writes %d say %q", exit, len(w.messages), len(h.writes()), env.Say)
	}
}

// A recompute that found a reason the preview did not say is drift as it
// is: no replay overrules it.
func TestANewReasonIsNeverReplayedAway(t *testing.T) {
	cut := tierReason{Code: reasonLimitCut, Detail: readBudgetDetail}
	stored := storedPlan{Hash: "sha256:preview", Kind: "write", Tier: "typed", Reasons: []tierReason{cut}, Reads: map[string]bool{"ws {}": true}}
	first := storedPlan{Hash: "sha256:first", Kind: "write", Tier: "typed", Reasons: []tierReason{cut, {Code: reasonAccessLock, EntityID: "lock.front", Via: "automation.a09"}}}
	passes := 0
	got, err := recheck(nil, &verbEnvelope{}, stored, func(*verbEnvelope) (storedPlan, error) { passes++; return first, nil })
	if err != nil || passes != 1 || got.replayed || stillConfirmed(stored, got) {
		t.Fatalf("passes %d err %v replayed %v", passes, err, got.replayed)
	}
}

// A cut batch member's replay after a sibling's write is still the one the
// user confirmed when its cuts say other entities, at reads the preview
// had no answer to, and only the written sibling's config differs; a
// replay that diverged or failed a new read, a recompute that was not
// replayed, or another config's change is drift.
func TestExcusedByBatchKeepsACutMemberWhoseCutsDiffer(t *testing.T) {
	plan := func(reasons []tierReason, configs map[string]any, lost ...string) storedPlan {
		p := storedPlan{Kind: "write", Server: "default", Tier: "typed", Reasons: reasons, ConfigAction: &configAction{Op: "write", Domain: "automation", ConfigID: "h1"},
			Basis: map[string]any{"current": "c", "draft": "d", "configs": configs, "entities": map[string]any{}}}
		if lost != nil {
			p.Basis["lost"] = lost
		}
		p.Hash = "sha256:" + planHash(p.Kind, p.Server, *p.ConfigAction, p.Tier, p.Reasons, p.Basis)
		return p
	}
	stored := plan([]tierReason{{Code: reasonLimitCut, Detail: readBudgetDetail}}, map[string]any{"automation:h2": "old", "automation:z1": "z"}, "light.desk|automation.z1|state")
	elsewhere := []tierReason{{Code: reasonScanFailed, EntityID: "light.desk", Via: "automation.z1", Detail: "state"}}
	newGap := []tierReason{{Code: reasonScanFailed, EntityID: "light.porch", Via: "automation.z1", Detail: "state"}}
	replay := func(p storedPlan, diverged bool) storedPlan { p.replayed, p.diverged = true, diverged; return p }
	written := map[string]string{"automation:h2": "new"}
	for name, c := range map[string]struct {
		fresh storedPlan
		want  bool
	}{
		"a replay whose cut says another entity": {replay(plan(elsewhere, map[string]any{"automation:h2": "new", "automation:z1": "z"}), false), true},
		"a replay that asked for a new read":     {replay(plan(elsewhere, map[string]any{"automation:h2": "new", "automation:z1": "z"}), true), false},
		"a recompute that was not replayed":      {plan(elsewhere, map[string]any{"automation:h2": "new", "automation:z1": "z"}), false},
		"another config changed":                 {replay(plan(elsewhere, map[string]any{"automation:h2": "new", "automation:z1": "y"}), false), false},
		"a replay that failed a new read":        {replay(plan(newGap, map[string]any{"automation:h2": "new", "automation:z1": "z"}), false), false},
	} {
		if got := excusedByBatch(stored, c.fresh, written, func(string) (string, bool) { return "", false }); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// A read the preview had an answer to that its replay can't read again may
// have changed: drift, even when the preview found nothing there (a new
// consumer of a lamp whose consumers can't be read now).
func TestAReplayThatLostAReadIsDrift(t *testing.T) {
	w := useWindow(t) // no window may open
	h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
	h.mu.Lock()
	h.hung["related:light.lamp_10"] = 1 // the preview stops at the last lamp
	h.mu.Unlock()
	planned := h.planLeave()
	if planned.Plan.Channel != channelWindow {
		t.Fatalf("setup: %+v", planned.Plan)
	}
	h.f.mu.Lock()
	h.f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.lamp_03"}}, "actions": []any{step("lock.unlock", "lock.front")}})
	h.f.related["entity:light.lamp_03"] = []string{"automation.door"}
	h.f.mu.Unlock()
	h.mu.Lock()
	h.hung["related:light.lamp_03"] = 10 // every check: the lamp's consumers stay unread
	h.mu.Unlock()
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != verbExitDrift || len(w.messages) != 0 || len(h.writes()) != 0 {
		t.Fatalf("exit %d windows %d writes %d say %q", exit, len(w.messages), len(h.writes()), env.Say)
	}
}

// A plan file whose reads were edited is refused: its replay would fail
// what the edit chose (here the consumers of a lamp that now has one).
func TestAnEditedReadListIsRefused(t *testing.T) {
	w := useWindow(t) // no window may open
	h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
	h.mu.Lock()
	h.hung["related:light.lamp_10"] = 1
	h.mu.Unlock()
	planned := h.planLeave()
	path := filepath.Join(planDir(h.paths), planned.Plan.ID+".json")
	stored, err := readPlanFile(path)
	edited := 0
	for key := range stored.Reads {
		if strings.Contains(key, "search/related") && strings.Contains(key, `"light.lamp_03"`) {
			stored.Reads[key], edited = false, edited+1
		}
	}
	if err != nil || edited != 1 {
		t.Fatalf("setup: %v, %d edited", err, edited)
	}
	if err := writePlanFile(path, stored); err != nil {
		t.Fatal(err)
	}
	h.f.mu.Lock()
	h.f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.lamp_03"}}, "actions": []any{step("lock.unlock", "lock.front")}})
	h.f.related["entity:light.lamp_03"] = []string{"automation.door"}
	h.f.mu.Unlock()
	h.mu.Lock()
	h.hung["related:light.lamp_03"] = 1 // the recompute before the replay doesn't get there
	h.mu.Unlock()
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit == 0 || len(w.messages) != 0 || len(h.writes()) != 0 {
		t.Fatalf("exit %d windows %d writes %d say %q", exit, len(w.messages), len(h.writes()), env.Say)
	}
}

// A batch keeps its members' plans as they were made, and only their hash
// binds them; a member whose reads were edited there is not replayed, so
// its cut check is drift.
func TestABatchMemberWithEditedReadsIsNotReplayed(t *testing.T) {
	w := useWindow(t) // no window may open
	f := presenceHome()
	f.ent("light.desk", "hue", "off", nil)
	f.automation("desk", "d1", map[string]any{"alias": "Desk", "actions": []any{step("light.turn_on", "light.desk")}})
	h := newSlowHarness(t, f, 0, 300*time.Millisecond)
	h.mu.Lock()
	h.hung["related:light.lamp_10"] = 1
	h.mu.Unlock()
	leave := h.planLeave().Plan.ID
	exit, batch := h.batch(leave, h.writePlan("automation", "d1", map[string]any{"alias": "Desk", "actions": []any{step("light.turn_off", "light.desk")}}))
	if batch.Plan == nil {
		t.Fatalf("plan batch: exit %d %+v", exit, batch.Error)
	}
	path := filepath.Join(planDir(h.paths), batch.Plan.ID+".json")
	stored, err := readPlanFile(path)
	edited := 0
	for i, m := range stored.BatchMembers {
		for key := range m.Reads {
			if m.ID == leave && strings.Contains(key, "search/related") && strings.Contains(key, `"light.lamp_03"`) {
				stored.BatchMembers[i].Reads[key], edited = false, edited+1
			}
		}
	}
	if err != nil || edited != 1 {
		t.Fatalf("setup: %v, %d edited", err, edited)
	}
	if err := writePlanFile(path, stored); err != nil {
		t.Fatal(err)
	}
	h.f.mu.Lock()
	h.f.automation("door", "door", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.lamp_03"}}, "actions": []any{step("lock.unlock", "lock.front")}})
	h.f.related["entity:light.lamp_03"] = []string{"automation.door"}
	h.f.mu.Unlock()
	h.mu.Lock()
	h.hung["related:light.lamp_03"] = 10
	h.mu.Unlock()
	exit, env := h.run(runApplyCommand, "--plan", batch.Plan.ID)
	if exit != verbExitDrift || len(w.messages) != 0 || len(h.writes()) != 0 {
		t.Fatalf("exit %d windows %d writes %d say %q", exit, len(w.messages), len(h.writes()), env.Say)
	}
}

// A reason that is not a read with no answer keeps its detail in the
// compare, a count or depth cut too: the replay read what it names as the
// preview did. A replay fails reads only where the preview failed them.
func TestAReplayComparesTheDetailOfEveryOtherReason(t *testing.T) {
	plan := func(reasons ...tierReason) storedPlan {
		p := storedPlan{Kind: "write", Server: "default", Tier: "typed", Reasons: reasons, ConfigAction: &configAction{Op: "write", Domain: "automation", ConfigID: "h1"},
			Basis: map[string]any{"current": "c", "draft": "d", "configs": map[string]any{}, "entities": map[string]any{}, "lost": []string{"light.desk||state"}}}
		p.Hash = "sha256:" + planHash(p.Kind, p.Server, *p.ConfigAction, p.Tier, p.Reasons, p.Basis)
		p.replayed = true
		return p
	}
	sibling := func(found string) tierReason {
		return tierReason{Code: reasonDeviceAccessSibling, EntityID: "light.porch", Via: "automation.leave", Detail: found}
	}
	cut := tierReason{Code: reasonLimitCut, Detail: readBudgetDetail}
	stored := plan(cut, sibling("lock.front"))
	for name, c := range map[string]struct {
		fresh storedPlan
		want  bool
	}{
		"another cut, the same sibling":   {plan(tierReason{Code: reasonScanFailed, EntityID: "light.desk", Detail: "state"}, sibling("lock.front")), true},
		"the sibling names another lock":  {plan(cut, sibling("lock.back")), false},
		"a read the preview did not fail": {plan(tierReason{Code: reasonScanFailed, EntityID: "light.porch", Detail: "state"}, sibling("lock.front")), false},
		"a depth cut it did not reach":    {plan(cut, sibling("lock.front"), tierReason{Code: reasonLimitCut, EntityID: "light.porch", Via: "group.garage", Detail: "deeper than 8"}), false},
	} {
		if got := sameAsConfirmed(stored, c.fresh); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// Only Home Assistant's own answer, data or its refusal of the command, is
// an answer a replay reads again; no answer in time or over the link is none.
func TestOnlyHomeAssistantsOwnRefusalCountsAsAnAnswer(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{nil, true},
		{&relayCallError{code: "UPSTREAM_WS_COMMAND_ERROR", message: "HA rejected 'config/entity_registry/get': not_found"}, true},
		{&relayCallError{code: "UPSTREAM_WS_TIMEOUT"}, false},
		{&relayCallError{code: "UPSTREAM_HTTP_TIMEOUT"}, false},
		{&relayCallError{code: "UPSTREAM_HTTP_ERROR"}, false},
		{context.DeadlineExceeded, false},
		{errNotReplayed, false},
	} {
		if got := answered(c.err); got != c.want {
			t.Errorf("answered(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// A batch member whose embedded reads were edited is drift even when its
// recompute has its hash: the file is never trusted.
func TestAnEditedBatchMemberIsDriftEvenWithItsHash(t *testing.T) {
	w := useWindow(t) // no window may open
	f := presenceHome()
	f.ent("light.desk", "hue", "off", nil)
	f.automation("desk", "d1", map[string]any{"alias": "Desk", "actions": []any{step("light.turn_on", "light.desk")}})
	h := newSlowHarness(t, f, 0, 300*time.Millisecond)
	h.mu.Lock()
	h.hung["related:light.lamp_10"] = 1
	h.mu.Unlock()
	leave := h.planLeave().Plan.ID
	exit, batch := h.batch(leave, h.writePlan("automation", "d1", map[string]any{"alias": "Desk", "actions": []any{step("light.turn_off", "light.desk")}}))
	if batch.Plan == nil {
		t.Fatalf("plan batch: exit %d %+v", exit, batch.Error)
	}
	path := filepath.Join(planDir(h.paths), batch.Plan.ID+".json")
	stored, err := readPlanFile(path)
	edited := 0
	for i, m := range stored.BatchMembers {
		for key := range m.Reads {
			if m.ID == leave && strings.Contains(key, "search/related") && strings.Contains(key, `"light.lamp_03"`) {
				stored.BatchMembers[i].Reads[key], edited = false, edited+1
			}
		}
	}
	if err != nil || edited != 1 {
		t.Fatalf("setup: %v, %d edited", err, edited)
	}
	if err := writePlanFile(path, stored); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.hung["related:light.lamp_10"], h.hung["related:light.lamp_03"] = 10, 10 // the recompute stops where the edit says
	h.mu.Unlock()
	exit, env := h.run(runApplyCommand, "--plan", batch.Plan.ID)
	if exit != verbExitDrift || len(w.messages) != 0 || len(h.writes()) != 0 {
		t.Fatalf("exit %d windows %d writes %d say %q err %+v", exit, len(w.messages), len(h.writes()), env.Say, env.Error)
	}
}

// A replay with the preview's hash still passes only sameAsConfirmed: the
// hash leaves each reason's detail out, here the lock a sibling names.
func TestAReplayWithThePreviewsHashComparesEveryDetail(t *testing.T) {
	plan := func(lock string) storedPlan {
		p := storedPlan{Kind: "write", Server: "default", Tier: "typed", ConfigAction: &configAction{Op: "write", Domain: "automation", ConfigID: "h1"},
			Reasons: []tierReason{{Code: reasonLimitCut, Detail: readBudgetDetail}, {Code: reasonDeviceAccessSibling, EntityID: "light.porch", Via: "automation.leave", Detail: lock}},
			Basis:   map[string]any{"current": "c", "draft": "d", "configs": map[string]any{}, "entities": map[string]any{}}}
		p.Hash = "sha256:" + planHash(p.Kind, p.Server, *p.ConfigAction, p.Tier, p.Reasons, p.Basis)
		return p
	}
	stored, same, other := plan("lock.front"), plan("lock.front"), plan("lock.back")
	same.replayed, other.replayed = true, true
	if other.Hash != stored.Hash || !stillConfirmed(stored, same) || stillConfirmed(stored, other) {
		t.Fatalf("same hash %v, the same lock %v, another lock %v", other.Hash == stored.Hash, stillConfirmed(stored, same), stillConfirmed(stored, other))
	}
}

// A cut preview whose recompute completes is replayed like any other: the
// recompute said nothing the preview did not, so the confirmation the user
// gave in the window still holds, and the automation is saved once.
func TestACutPlanWhoseRecomputeCompletesStillApplies(t *testing.T) {
	w := useWindow(t, windowConfirmed)
	h := newSlowHarness(t, presenceHome(), 0, 300*time.Millisecond)
	h.hangFirst("p05", 1)
	planned := h.planLeave()
	if planned.Tier != "typed" || planned.Plan.Channel != channelWindow {
		t.Fatalf("setup: tier %s plan %+v reasons %s", planned.Tier, planned.Plan, reasonCodes(planned))
	}
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	if exit != 0 || len(w.messages) != 1 || len(h.writes()) != 1 {
		t.Fatalf("exit %d windows %d writes %d env %+v", exit, len(w.messages), len(h.writes()), env)
	}
}

// A replay that diverged is never the confirmed plan, and neither is the
// plan computed after it, even with the preview's hash (recheck).
func TestADivergedReplayIsNeverTheConfirmedPlan(t *testing.T) {
	stored := storedPlan{Hash: "sha256:same"}
	for name, c := range map[string]struct {
		fresh storedPlan
		want  bool
	}{
		"a recompute":                {storedPlan{Hash: "sha256:same"}, true},
		"a replay that diverged":     {storedPlan{Hash: "sha256:same", replayed: true, diverged: true}, false},
		"the plan computed after it": {storedPlan{Hash: "sha256:same", diverged: true}, false},
	} {
		if got := stillConfirmed(stored, c.fresh); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

// relatedReads counts the consumer lookups of an entity the session made.
func (h *slowHarness) relatedReads(id string) int {
	n := 0
	for _, c := range h.log.all() {
		if c.payload["type"] == "search/related" && c.payload["item_id"] == id {
			n++
		}
	}
	return n
}

// Read-ahead stays inside the walk's depth: it reads no consumers of the
// entity the walk cuts as too deep (a chain of automations, each turning
// on the next lamp when the one before it changes).
func TestReadAheadStopsAtTheWalksDepth(t *testing.T) {
	f := newFakeHA()
	f.ent("input_boolean.presence", "input_boolean", "on", nil)
	on := "input_boolean.presence"
	for i := range 12 {
		lamp := fmt.Sprintf("light.c%02d", i)
		f.ent(lamp, "hue", "off", nil)
		f.automation(fmt.Sprintf("chain_%02d", i), fmt.Sprintf("c%02d", i), map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": on}}, "actions": []any{step("light.turn_on", lamp)}})
		f.related["entity:"+on] = []string{fmt.Sprintf("automation.chain_%02d", i)}
		on = lamp
	}
	before := leaveDraft()
	before["actions"] = []any{} // the draft adds its step: an update weighs what it changes
	f.storeConfig("/api/config/automation/config/leave", before)
	h := newSlowHarness(t, f, 0, 10*time.Second)
	planned := h.planLeave()
	deep := ""
	for _, r := range planned.Reasons {
		if r.Code == reasonLimitCut && strings.HasPrefix(r.Detail, "deeper than") {
			deep = r.EntityID
		}
	}
	if deep == "" || h.relatedReads(deep) != 0 {
		t.Fatalf("cut at %q, its consumers read %d times; reasons %s", deep, h.relatedReads(deep), reasonCodes(planned))
	}
}

// Read-ahead follows only a literal service, as the walk does: a templated
// one is a gap, and the consumers of its target are never read.
func TestReadAheadSkipsATemplatedActionsTargets(t *testing.T) {
	f := presenceHome()
	f.ent("light.porch", "hue", "off", nil)
	f.automation("templated", "t1", map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "input_boolean.presence", "to": "off"}}, "actions": []any{map[string]any{"action": "{{ 'light.turn_on' }}", "target": map[string]any{"entity_id": "light.porch"}}}})
	f.related["entity:input_boolean.presence"] = append(f.related["entity:input_boolean.presence"], "automation.templated")
	h := newSlowHarness(t, f, 0, 10*time.Second)
	planned := h.planLeave()
	if !strings.Contains(reasonCodes(planned), reasonTemplatedAction) || h.relatedReads("light.porch") != 0 {
		t.Fatalf("reasons %s, the porch's consumers read %d times", reasonCodes(planned), h.relatedReads("light.porch"))
	}
}

// A raw plan whose check was cut before it read anything it keeps (here the
// event listener counts, at the read budget) is still a plan: typed, the
// cut said.
func TestARawPlanCutBeforeItReadAnythingIsAPlan(t *testing.T) {
	f := newFakeHA()
	listensToTheList(f)
	f.hang["/api/events"] = true
	paths, _ := setupVerbRelayTest(t, f.reply)
	saved := relaySessionOverallTimeout
	relaySessionOverallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { relaySessionOverallTimeout = saved })
	exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"todo/item/move","entity_id":"todo.shopping_list","uid":"a"}}]`)
	if exit != verbExitConfirmation || env.Plan == nil || env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonLimitCut) {
		t.Fatalf("exit %d tier %s reasons %s err %+v", exit, env.Tier, reasonCodes(env), env.Error)
	}
}
