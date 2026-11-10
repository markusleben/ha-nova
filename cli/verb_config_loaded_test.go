package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Apply sends no reload after a save (Home Assistant's config view reloads
// on its own) and waits, read-only, until Home Assistant runs the saved
// config (awaitLoaded); a delete keeps its reload.

// afterSave answers every request after the first config save with hook
// (handled false: the fake answers as usual). Runs under f.mu.
func afterSave(f *fakeHA, hook func(endpoint string, p map[string]any) (any, bool)) {
	saved := false
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		if p["method"] == "POST" && strings.HasPrefix(fmt.Sprint(p["path"]), "/api/config/") {
			saved = true
			return nil, false
		}
		if !saved {
			return nil, false
		}
		return hook(endpoint, p)
	}
}

func noServiceSent(t *testing.T, h *writeHarness) {
	t.Helper()
	for _, c := range h.posts() {
		if strings.HasPrefix(fmt.Sprint(c.payload["path"]), "/api/services/") {
			t.Fatalf("apply sent %v %v", c.payload["path"], c.payload["body"])
		}
	}
}

func applyAll(h *writeHarness, env verbTestEnvelope) (int, verbTestEnvelope) {
	h.t.Helper()
	args := []string{"--plan", env.Plan.ID}
	if env.Plan.Code != "" {
		args = append(args, "--confirm", env.Plan.Code)
	}
	return h.run(runApplyCommand, args...)
}

func TestConfigSaveWaitsUntilHomeAssistantLoadedIt(t *testing.T) {
	for _, item := range []struct{ domain, id, entity, key string }{
		{"automation", "a1", "automation.night_mode", "actions"},
		{"script", "s1", "script.arrival", "sequence"},
	} {
		t.Run(item.domain, func(t *testing.T) {
			f := batchHome()
			old := f.configs["/api/config/"+item.domain+"/config/"+item.id]
			reads := 0
			afterSave(f, func(endpoint string, p map[string]any) (any, bool) {
				if p["type"] != item.domain+"/config" {
					return nil, false
				}
				if reads++; reads <= 3 { // the reload is still running: the old config
					return map[string]any{"config": old}, true
				}
				return nil, false
			})
			h := newWriteHarness(t, f)
			draft := map[string]any{"alias": "Changed", item.key: []any{step("light.turn_off", "light.attic")}}
			_, env := h.run(runPlanCommand, "write", "--domain", item.domain, "--id", item.id, "--file", draftFile(t, draft))
			exit, applied := applyAll(h, env)
			if exit != 0 || applied.Status != "verified" || reads != 4 || !strings.Contains(applied.Verification[0].Observed, "loaded: true") {
				t.Fatalf("exit %d reads %d env %+v", exit, reads, applied)
			}
			noServiceSent(t, h)
		})
	}
}

func TestConfigSaveNeverLoadedStaysUnverified(t *testing.T) {
	f := batchHome()
	f.loaded = map[string]any{"automation.night_mode": f.configs["/api/config/automation/config/a1"]}
	h := newWriteHarness(t, f)
	draft := map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_off", "light.attic")}}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, draft))
	exit, applied := applyAll(h, env)
	if exit != verbExitUnverified || applied.Status != "unverified" || applied.BeforeConfig == nil ||
		!strings.HasPrefix(applied.Verification[0].Note, "saved, but Home Assistant hadn't loaded it within ") ||
		!strings.HasSuffix(applied.Verification[0].Note, " s: whether the change is running is unknown") ||
		!strings.Contains(applied.NextStep, "ha-nova get automation.night_mode --config") || !strings.HasSuffix(applied.NextStep, "never apply it again.") {
		t.Fatalf("exit %d env %+v", exit, applied)
	}
	noServiceSent(t, h)
}

// A late approval or a long batch spends the command's budget; the save still
// gets a short look, since Home Assistant reloads only after its 200.
func TestConfigSaveWithASpentBudgetStillLooks(t *testing.T) {
	saved := commandStart
	commandStart = time.Now().Add(-2 * windowWait)
	t.Cleanup(func() { commandStart = saved })
	f := batchHome()
	old := f.configs["/api/config/automation/config/a1"]
	reads := 0
	afterSave(f, func(endpoint string, p map[string]any) (any, bool) {
		if p["type"] != "automation/config" {
			return nil, false
		}
		if reads++; reads <= 2 {
			return map[string]any{"config": old}, true
		}
		return nil, false
	})
	h := newWriteHarness(t, f)
	draft := map[string]any{"alias": "Changed", "actions": []any{step("light.turn_off", "light.attic")}}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, draft))
	if exit, applied := applyAll(h, env); exit != 0 || applied.Status != "verified" || reads != 3 {
		t.Fatalf("exit %d reads %d env %+v", exit, reads, applied)
	}
}

func TestConfigCreateWaitsForItsRegistryEntity(t *testing.T) {
	f := newFakeHA()
	var path string
	var body map[string]any
	f.configWrite = func(f *fakeHA, p string, b map[string]any) any {
		path, body, f.configs[p] = p, b, b // saved, not loaded yet
		return coreReply(200, map[string]any{"result": "ok"})
	}
	lists := 0
	afterSave(f, func(endpoint string, p map[string]any) (any, bool) {
		if p["type"] == "config/entity_registry/list" {
			if lists++; lists == 3 {
				f.storeConfig(path, body)
			}
		}
		return nil, false
	})
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, hallDraft("light.turn_on")))
	exit, applied := applyAll(h, env)
	if exit != 0 || applied.Status != "verified" || lists != 3 || applied.Verification[0].EntityID != "automation.hall_motion" {
		t.Fatalf("exit %d lists %d env %+v", exit, lists, applied)
	}
	noServiceSent(t, h)
}

func TestConfigLoadReadFailureKeepsPolling(t *testing.T) {
	f := batchHome()
	fails := 0
	afterSave(f, func(endpoint string, p map[string]any) (any, bool) {
		if p["type"] == "automation/config" && fails < 2 {
			fails++
			return fakeRelayFailure{"UPSTREAM_ERROR", "boom"}, true
		}
		return nil, false
	})
	h := newWriteHarness(t, f)
	draft := map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_off", "light.attic")}}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, draft))
	if exit, applied := applyAll(h, env); exit != 0 || applied.Status != "verified" || fails != 2 {
		t.Fatalf("exit %d fails %d env %+v", exit, fails, applied)
	}
}

// Home Assistant keeps a blueprint item substituted (raw_config); the
// loaded config is the substitution with the item's own keys over it.
func TestConfigBlueprintSaveComparesTheSubstitution(t *testing.T) {
	substituted := map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.hall"}}, "actions": []any{step("light.turn_on", "light.hall")}}
	for name, loaded := range map[string]map[string]any{
		"loaded":     {"alias": "Hall", "triggers": substituted["triggers"], "actions": substituted["actions"]},
		"not loaded": {"alias": "Hall", "triggers": substituted["triggers"], "actions": []any{step("light.turn_on", "light.attic")}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHA()
			f.ent("binary_sensor.hall", "", "off", nil)
			f.substitute = map[string]any{"substituted_config": substituted}
			f.loaded = map[string]any{"automation.hall": loaded}
			h := newWriteHarness(t, f)
			draft := map[string]any{"alias": "Hall", "use_blueprint": map[string]any{"path": "motion.yaml", "input": map[string]any{"light": "light.hall"}}}
			_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, draft))
			exit, applied := applyAll(h, env)
			if want := name == "loaded"; (exit == 0) != want || (applied.Status == "verified") != want {
				t.Fatalf("exit %d env %+v", exit, applied)
			}
		})
	}
}

func TestConfigScriptFieldsCompareClean(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	draft := map[string]any{"alias": "Greet", "fields": map[string]any{"who": map[string]any{"selector": map[string]any{"text": nil}}, "loud": map[string]any{"selector": map[string]any{"boolean": nil}}},
		"sequence": []any{step("light.turn_on", "light.hall")}}
	_, env := h.run(runPlanCommand, "write", "--domain", "script", "--id", "greet", "--file", draftFile(t, draft))
	if exit, applied := applyAll(h, env); exit != 0 || applied.Status != "verified" {
		t.Fatalf("exit %d env %+v", exit, applied)
	}
	noServiceSent(t, h)
}

func TestConfigDisabledEntityStopsTheWaitAtOnce(t *testing.T) {
	f := batchHome()
	for _, row := range f.full {
		if row["unique_id"] == "a1" {
			row["disabled_by"] = "user"
		}
	}
	lists, configs := 0, 0
	afterSave(f, func(endpoint string, p map[string]any) (any, bool) {
		lists += map[bool]int{true: 1}[p["type"] == "config/entity_registry/list"]
		configs += map[bool]int{true: 1}[p["type"] == "automation/config"]
		return nil, false
	})
	h := newWriteHarness(t, f)
	draft := map[string]any{"alias": "Night mode", "actions": []any{step("light.turn_off", "light.attic")}}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, draft))
	exit, applied := applyAll(h, env)
	if exit != 0 || applied.Status != "verified" || applied.Verification[0].Note != "disabled in the entity registry: saved, not running" || lists != 1 || configs != 0 {
		t.Fatalf("exit %d lists %d configs %d env %+v", exit, lists, configs, applied)
	}
}

func TestConfigBatchMembersEachWaitUntilLoaded(t *testing.T) {
	f := batchHome()
	reads := 0
	afterSave(f, func(endpoint string, p map[string]any) (any, bool) {
		if p["type"] == "script/config" && p["entity_id"] == "script.old_arrival" {
			if reads++; reads <= 2 {
				return map[string]any{"config": map[string]any{"alias": "Old arrival", "sequence": []any{}}}, true
			}
		}
		return nil, false
	})
	h := newWriteHarness(t, f)
	_, env := h.batch(h.writePlan("script", "s1", arrival("light.turn_off")),
		h.writePlan("script", "s2", map[string]any{"alias": "Old arrival", "sequence": []any{step("light.turn_off", "light.kitchen")}}))
	exit, applied := applyAll(h, env)
	items := batchItems(t, applied)
	if exit != 0 || len(items) != 2 || reads != 3 {
		t.Fatalf("exit %d reads %d items %+v", exit, reads, items)
	}
	for _, item := range items {
		if item.Status != "verified" || !strings.Contains(item.Verification[0].Observed, "loaded: true") {
			t.Fatalf("item %+v", item)
		}
	}
	noServiceSent(t, h)
}

// A revert and a restore are saves too; the delete before the restore
// keeps its reload.
func TestConfigRevertAndRestoreWaitWithoutAReload(t *testing.T) {
	h := nightHarness(t)
	draft := map[string]any{"alias": "Night", "triggers": []any{}, "actions": []any{step("light.turn_off", "light.attic")}}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "n1", "--file", draftFile(t, draft))
	if exit, applied := applyAll(h, env); exit != 0 || applied.Status != "verified" {
		t.Fatalf("update: exit %d env %+v", exit, applied)
	}
	_, env = h.run(runPlanCommand, "revert", "--target", "n1")
	if exit, applied := applyAll(h, env); exit != 0 || applied.Status != "verified" {
		t.Fatalf("revert: exit %d env %+v", exit, applied)
	}
	noServiceSent(t, h)
	_, env = h.run(runPlanCommand, "delete", "automation.night")
	exit, deleted := applyAll(h, env)
	if exit != 0 || len(h.posts()) == 0 || h.posts()[len(h.posts())-1].payload["path"] != "/api/services/automation/reload" {
		t.Fatalf("delete: exit %d env %+v posts %+v", exit, deleted, h.posts())
	}
	reloads := len(h.posts())
	_, env = h.run(runPlanCommand, restoreArgs(t, deleted.Restore)...)
	if exit, applied := applyAll(h, env); exit != 0 || applied.Status != "verified" || h.posts()[len(h.posts())-1].payload["path"] == "/api/services/automation/reload" || len(h.posts()) != reloads+1 {
		t.Fatalf("restore: exit %d env %+v posts %+v", exit, applied, h.posts())
	}
}
