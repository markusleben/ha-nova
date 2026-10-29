package main

import (
	"strings"
	"testing"
)

// Members that read each other's configs (docs/work/2026-10-29-batch-
// confirmation-spec.md §4, the batch's own writes): automations on one
// light each read the others in their consumer scan. A later member's
// re-check excuses only the configs this apply already wrote and verified;
// everything else it reads, its tier, and its reasons must still be the
// confirmed ones.

// sameLightHome: three automations on light.hall that each scan the others,
// and one outside the batch (z1).
func sameLightHome() *fakeHA {
	f := newFakeHA()
	for id, alias := range map[string]string{"h1": "Hall one", "h2": "Hall two", "h3": "Hall three", "z1": "Hall other"} {
		f.storeConfig("/api/config/automation/config/"+id, map[string]any{"alias": alias, "actions": []any{step("light.turn_on", "light.hall")}})
	}
	f.related["entity:light.hall"] = []string{"automation.hall_one", "automation.hall_two", "automation.hall_three", "automation.hall_other"}
	return f
}

func hallOff(alias string) map[string]any {
	return map[string]any{"alias": alias, "actions": []any{step("light.turn_off", "light.hall")}}
}

// Two updates and a delete on one light: one batch, every one verified.
func TestBatchSameLightUpdatesAndDeleteGoThrough(t *testing.T) {
	h := newWriteHarness(t, sameLightHome())
	ids := []string{h.writePlan("automation", "h1", hallOff("Hall one")), h.writePlan("automation", "h2", hallOff("Hall two")), h.member("delete", "automation.hall_three")}
	exit, env := h.batch(ids...)
	if exit != 3 || env.Plan == nil {
		t.Fatalf("plan batch: exit %d %+v", exit, env.Error)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if items := batchItems(t, applied); exit != 0 || itemStatuses(items) != "verified,verified,verified" || len(h.writes()) != 3 {
		t.Fatalf("apply: exit %d %+v items %s writes %v", exit, applied.Error, itemStatuses(items), h.writes())
	}
}

// Two updates that read each other go in either order.
func TestBatchMutualReadsRunInTheGivenOrder(t *testing.T) {
	for name, order := range map[string][2]int{"one first": {0, 1}, "two first": {1, 0}} {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, sameLightHome())
			ids := []string{h.writePlan("automation", "h1", hallOff("Hall one")), h.writePlan("automation", "h2", hallOff("Hall two"))}
			exit, env := h.batch(ids[order[0]], ids[order[1]])
			if exit != 3 || env.Plan == nil {
				t.Fatalf("plan batch: exit %d %+v", exit, env.Error)
			}
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			if items := batchItems(t, applied); exit != 0 || itemStatuses(items) != "verified,verified" {
				t.Fatalf("apply: exit %d %+v items %s", exit, applied.Error, itemStatuses(items))
			}
		})
	}
}

// Only this apply's own verified writes are excused: a config outside the
// batch or an acted entity that changed meanwhile stops the next member.
func TestBatchExcusesOnlyItsOwnWrites(t *testing.T) {
	for name, change := range map[string]func(f *fakeHA){
		"a config outside the batch": func(f *fakeHA) {
			f.configs["/api/config/automation/config/z1"] = map[string]any{"alias": "Hall other", "actions": []any{step("light.toggle", "light.hall")}}
		},
		"an acted entity": func(f *fakeHA) {
			f.states["light.hall"]["attributes"] = map[string]any{"device_class": "relay"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newWriteHarness(t, sameLightHome())
			_, env := h.batch(h.writePlan("automation", "h1", hallOff("Hall one")), h.writePlan("automation", "h2", hallOff("Hall two")))
			h.f.configWrite = writeThen("/h1", change, nil)
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			if items := batchItems(t, applied); exit != 6 || applied.Error.Code != "BATCH_STOPPED" || itemStatuses(items) != "verified,drift" || len(h.writes()) != 1 {
				t.Fatalf("exit %d %+v items %s writes %v", exit, applied.Error, itemStatuses(items), h.writes())
			}
		})
	}
}

// A sibling's new config that makes a later member's consumer unlock a lock
// raises that member's tier: it stops as drift, never sent. The same
// sibling turning on a light changes nothing it would set off: it goes.
func TestBatchSiblingThatRaisesALaterTierStops(t *testing.T) {
	for name, c := range map[string]struct {
		action   map[string]any
		statuses string
	}{
		"an unlock": {step("lock.unlock", "lock.front"), "verified,drift"},
		"a light":   {step("light.turn_on", "light.kitchen"), "verified,verified"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHA()
			onHall := map[string]any{"trigger": "state", "entity_id": "light.hall"}
			f.storeConfig("/api/config/automation/config/y1", map[string]any{"alias": "Hall guard", "triggers": []any{onHall}, "actions": []any{step("light.turn_off", "light.kitchen")}})
			f.storeConfig("/api/config/automation/config/h1", map[string]any{"alias": "Hall one", "actions": []any{step("light.turn_on", "light.hall")}})
			f.related["entity:light.hall"] = []string{"automation.hall_one", "automation.hall_guard"}
			h := newWriteHarness(t, f)
			guard := h.writePlan("automation", "y1", map[string]any{"alias": "Hall guard", "triggers": []any{onHall}, "actions": []any{c.action, step("light.turn_on", "light.hall")}})
			exit, env := h.batch(guard, h.writePlan("automation", "h1", hallOff("Hall one")))
			if exit != 3 || env.Plan == nil {
				t.Fatalf("plan batch: exit %d %+v", exit, env.Error)
			}
			args := []string{"--plan", env.Plan.ID}
			if env.Plan.Code != "" {
				args = append(args, "--confirm", env.Plan.Code)
			}
			_, applied := h.run(runApplyCommand, args...)
			if items := batchItems(t, applied); itemStatuses(items) != c.statuses {
				t.Fatalf("apply: %+v items %s writes %v", applied.Error, itemStatuses(items), h.writes())
			}
		})
	}
}

// Two automations that swap their lights read each other. The first one's
// write takes it out of the second one's scan; read once more, it is still
// the config this apply verified, so the second goes too. Edited by someone
// else after its check, it stops the second.
func TestBatchSiblingThatLeavesALaterScan(t *testing.T) {
	for name, c := range map[string]struct {
		edit     map[string]any
		statuses string
		writes   int
	}{
		"as verified":            {nil, "verified,verified", 2},
		"edited after its check": {map[string]any{"alias": "Hall one", "actions": []any{step("light.toggle", "light.kitchen")}}, "verified,drift", 1},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHA()
			f.storeConfig("/api/config/automation/config/h1", map[string]any{"alias": "Hall one", "actions": []any{step("light.turn_on", "light.hall")}})
			f.storeConfig("/api/config/automation/config/h2", map[string]any{"alias": "Hall two", "actions": []any{step("light.turn_on", "light.kitchen")}})
			f.storeConfig("/api/config/automation/config/z1", map[string]any{"alias": "Hall other", "actions": []any{step("light.turn_on", "light.hall")}})
			f.related["entity:light.hall"] = []string{"automation.hall_one", "automation.hall_other"}
			f.related["entity:light.kitchen"] = []string{"automation.hall_two"}
			h := newWriteHarness(t, f)
			exit, env := h.batch(h.writePlan("automation", "h1", map[string]any{"alias": "Hall one", "actions": []any{step("light.turn_on", "light.kitchen")}}),
				h.writePlan("automation", "h2", map[string]any{"alias": "Hall two", "actions": []any{step("light.turn_on", "light.hall")}}))
			if exit != 3 || env.Plan == nil {
				t.Fatalf("plan batch: exit %d %+v", exit, env.Error)
			}
			wrote, edited := false, false
			h.f.configWrite = writeThen("/h1", func(f *fakeHA) {
				wrote = true
				f.related["entity:light.hall"] = []string{"automation.hall_other"}
				f.related["entity:light.kitchen"] = []string{"automation.hall_two", "automation.hall_one"}
			}, nil)
			h.f.extra = func(_ string, p map[string]any) (any, bool) { // the next member's scan: after h1's check
				if wrote && c.edit != nil && !edited && p["type"] == "search/related" {
					h.f.configs["/api/config/automation/config/h1"] = c.edit
					edited = true
				}
				return nil, false
			}
			_, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			if items := batchItems(t, applied); itemStatuses(items) != c.statuses || len(h.writes()) != c.writes || edited != (c.edit != nil) {
				t.Fatalf("apply: %+v items %s writes %v edited %v", applied.Error, itemStatuses(items), h.writes(), edited)
			}
		})
	}
}

// The excuse covers exactly the content this apply wrote and verified under
// its keys (a create's new key too), or a written key no longer read whose
// live config is still that content; a different content, a failed read,
// or any other difference is drift.
func TestExcusedByBatchExcusesOnlyWrittenConfigs(t *testing.T) {
	plan := func(tier string, reasons []tierReason, configs map[string]any) storedPlan {
		p := storedPlan{Kind: "write", Server: "default", Tier: tier, Reasons: reasons, ConfigAction: &configAction{Op: "write", Domain: "automation", ConfigID: "h1"},
			Basis: map[string]any{"current": "c", "draft": "d", "configs": configs, "entities": map[string]any{"light.hall": map[string]any{"pl": "hue"}}}}
		p.Hash = "sha256:" + planHash(p.Kind, p.Server, *p.ConfigAction, p.Tier, p.Reasons, p.Basis)
		return p
	}
	stored := plan("preview", nil, map[string]any{"automation:h2": "old", "automation:z1": "z"})
	h2 := map[string]string{"automation:h2": "new"}
	left := plan("preview", nil, map[string]any{"automation:z1": "z"})
	for name, c := range map[string]struct {
		fresh   storedPlan
		written map[string]string
		live    map[string]string
		want    bool
	}{
		"unchanged":                                 {plan("preview", nil, map[string]any{"automation:h2": "old", "automation:z1": "z"}), nil, nil, true},
		"a sibling as written and verified":         {plan("preview", nil, map[string]any{"automation:h2": "new", "automation:z1": "z"}), h2, nil, true},
		"a sibling created and verified":            {plan("preview", nil, map[string]any{"automation:h2": "old", "automation:h9": "n", "automation:z1": "z"}), map[string]string{"automation:h9": "n"}, nil, true},
		"a sibling changed again since":             {plan("preview", nil, map[string]any{"automation:h2": "other", "automation:z1": "z"}), h2, nil, false},
		"a written sibling that left":               {left, h2, map[string]string{"automation:h2": "new"}, true},
		"a written sibling that left, edited since": {left, h2, map[string]string{"automation:h2": "other"}, false},
		"a written sibling that left, unreadable":   {left, h2, nil, false},
		"a sibling not read nor written":            {left, nil, nil, false},
		"a sibling not written":                     {plan("preview", nil, map[string]any{"automation:h2": "new", "automation:z1": "z"}), nil, nil, false},
		"another one written":                       {plan("preview", nil, map[string]any{"automation:h2": "new", "automation:z1": "z"}), map[string]string{"automation:z1": "z"}, nil, false},
		"a config outside the batch":                {plan("preview", nil, map[string]any{"automation:h2": "new", "automation:z1": "y"}), h2, nil, false},
		"a higher tier":                             {plan("typed", nil, map[string]any{"automation:h2": "new", "automation:z1": "z"}), h2, nil, false},
		"a new reason":                              {plan("preview", []tierReason{{Code: reasonAccessLock, EntityID: "lock.front"}}, map[string]any{"automation:h2": "new", "automation:z1": "z"}), h2, nil, false},
	} {
		live := func(key string) (string, bool) { digest, read := c.live[key]; return digest, read }
		if got := excusedByBatch(stored, c.fresh, c.written, live); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
	entity := plan("preview", nil, map[string]any{"automation:h2": "new", "automation:z1": "z"})
	entity.Basis["entities"] = map[string]any{"light.hall": map[string]any{"pl": "hue", "device_class": "relay"}}
	entity.Hash = "sha256:" + planHash(entity.Kind, entity.Server, *entity.ConfigAction, entity.Tier, entity.Reasons, entity.Basis)
	if excusedByBatch(stored, entity, h2, func(string) (string, bool) { return "", false }) {
		t.Error("an acted entity that changed was excused")
	}
}

// A sibling changed again by someone else after this apply verified it is
// not what the batch wrote: the next member stops.
func TestBatchSiblingChangedAfterItsCheckStops(t *testing.T) {
	h := newWriteHarness(t, sameLightHome())
	_, env := h.batch(h.writePlan("automation", "h1", hallOff("Hall one")), h.writePlan("automation", "h2", hallOff("Hall two")))
	wrote, changed := false, false
	h.f.configWrite = writeThen("/h1", func(*fakeHA) { wrote = true }, nil)
	h.f.extra = func(_ string, p map[string]any) (any, bool) { // the next member's scan: after h1's check
		if wrote && !changed && p["type"] == "search/related" {
			h.f.configs["/api/config/automation/config/h1"] = map[string]any{"alias": "Hall one", "actions": []any{step("light.toggle", "light.hall")}}
			changed = true
		}
		return nil, false
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if items := batchItems(t, applied); exit != 6 || !changed || itemStatuses(items) != "verified,drift" || len(h.writes()) != 1 {
		t.Fatalf("exit %d %+v items %s writes %v", exit, applied.Error, itemStatuses(items), h.writes())
	}
}

// A new script and the automation changed to run it: the script's scan
// reads the automation's config, and the automation's check reads the
// script's entity, which is never excused, so the automation runs first.
func TestBatchMutualEntityReadRunsTheReaderFirst(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/h1", map[string]any{"alias": "Hall one", "actions": []any{step("light.turn_on", "light.hall")}})
	f.related["entity:light.hall"] = []string{"automation.hall_one"}
	h := newWriteHarness(t, f)
	script := h.member("write", "--domain", "script", "--id", "evening", "--create", "--file", draftFile(t, map[string]any{"alias": "Evening", "sequence": []any{step("light.turn_on", "light.hall")}}))
	caller := h.writePlan("automation", "h1", map[string]any{"alias": "Hall one", "actions": []any{step("script.turn_on", "script.evening")}})
	if exit, env := h.batch(script, caller); exit != 4 || env.Error.Code != "BATCH_ORDER" || !strings.Contains(env.Error.Message, "move “Hall one” before “Evening”") {
		t.Fatalf("script first: exit %d %+v", exit, env.Error)
	}
	exit, env := h.batch(caller, script)
	if exit != 3 || env.Plan == nil {
		t.Fatalf("caller first: exit %d %+v", exit, env.Error)
	}
	args := []string{"--plan", env.Plan.ID}
	if env.Plan.Code != "" {
		args = append(args, "--confirm", env.Plan.Code)
	}
	if exit, applied := h.run(runApplyCommand, args...); exit != 0 || itemStatuses(batchItems(t, applied)) != "verified,verified" {
		t.Fatalf("apply: exit %d %+v items %s", exit, applied.Error, itemStatuses(batchItems(t, applied)))
	}
}

// Two new scripts that run each other each read the other's entity, which
// is never excused: neither can run first, in either order.
func TestBatchMutualEntityReadsAreRefused(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	create := func(id, alias, calls string) string {
		return h.member("write", "--domain", "script", "--id", id, "--create", "--file", draftFile(t, map[string]any{"alias": alias, "sequence": []any{step("script.turn_on", calls)}}))
	}
	ids := []string{create("morning", "Morning", "script.evening"), create("evening", "Evening", "script.morning")}
	for _, order := range [][2]int{{0, 1}, {1, 0}} {
		if exit, env := h.batch(ids[order[0]], ids[order[1]]); exit != 4 || env.Error.Code != "BATCH_ORDER" || !strings.Contains(env.Error.Message, "neither can run first") {
			t.Fatalf("order %v: exit %d %+v", order, exit, env.Error)
		}
	}
}
