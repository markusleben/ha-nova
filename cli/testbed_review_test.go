package main

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Third test-bed review batch: refusal next steps, YAML deletes, verify
// notes, find by state, disabled/unloaded entities, history --until, MCP
// next steps.

// 1, 9, 10: a used plan may have run; every re-plan confirms again.
func TestPlanLifecycleNextSteps(t *testing.T) {
	for _, code := range []string{"PLAN_NOT_FOUND", "PLAN_CLAIMED"} {
		if next := refused(code, "x").next; next != mayHaveRun || !strings.Contains(next, "read the target first") {
			t.Fatalf("%s next %q", code, next)
		}
	}
	for _, code := range []string{"PLAN_EXPIRED", "PLAN_INVALIDATED", "PLAN_INVALID", "PLAN_NOT_FOUND"} {
		if next := refused(code, "x").next; !strings.Contains(next, "confirmation again") || strings.Contains(next, "apply the new plan") {
			t.Fatalf("%s next %q", code, next)
		}
	}
	if !strings.Contains(refused("PLAN_INVALIDATED", "x").next, "explicit request") ||
		!strings.Contains(refused("REVERT_UNSUPPORTED", "x").next, "--target <config_id>") ||
		!strings.Contains(refused("CONFIG_ID_UNSUPPORTED", "x").next, "get <entity_id> --config") {
		t.Fatal("revert/config-id refusal next steps")
	}
}

// 2: deleting an id-less YAML automation names the YAML route, not "missing".
func TestDeleteOfAYAMLItemIsNotUIManaged(t *testing.T) {
	f := newFakeHA()
	f.automation("night", "", nil)
	f.loaded = map[string]any{"automation.night": map[string]any{"alias": "Night", "actions": []any{}}}
	h := newWriteHarness(t, f)
	if exit, env := h.run(runPlanCommand, "delete", "automation.night"); exit != 4 || env.Error.Code != "CONFIG_NOT_UI_MANAGED" || !strings.Contains(env.Error.Next, "ha-nova:yaml-config") {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	delete(f.loaded, "automation.night")
	if exit, env := h.run(runPlanCommand, "delete", "automation.night"); exit != 5 || env.Error.Code != "CONFIG_NOT_FOUND" {
		t.Fatalf("no loaded config: exit %d env %+v", exit, env)
	}
}

// 3, 12: a taken id gets a domain-specific next; a user-chosen create id
// stays in the code, a minted one is replaced by the alias.
func TestCreateCollisionsAndCodes(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/a1", hallDraft("light.turn_on"))
	f.script("hall_motion", step("light.turn_on", "light.hall"))
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--create", "--id", "a1", "--file", draftFile(t, hallDraft("light.turn_off")))
	if exit != 4 || env.Error.Code != "CONFIG_ID_IN_USE" || !strings.Contains(env.Error.Next, "without --id") {
		t.Fatalf("automation: exit %d env %+v", exit, env)
	}
	script := map[string]any{"alias": "Hall motion", "sequence": []any{step("lock.unlock", "lock.front")}}
	exit, env = h.run(runPlanCommand, "write", "--domain", "script", "--file", draftFile(t, script))
	if exit != 4 || env.Error.Code != "CONFIG_ID_IN_USE" || !strings.Contains(env.Error.Next, "--id <new> --create") {
		t.Fatalf("script: exit %d env %+v", exit, env)
	}
	_, env = h.run(runPlanCommand, "write", "--domain", "script", "--create", "--id", "front_open", "--file", draftFile(t, script))
	if !strings.HasPrefix(env.Plan.Code, "confirm:write-front_open-") {
		t.Fatalf("a chosen id left the code: %q", env.Plan.Code)
	}
}

// 5: "still" only when the device did not move, "ended" when it moved
// elsewhere; a late 404 keeps the last good read.
func TestVerifyNotes(t *testing.T) {
	fastVerify(t)
	cases := []struct {
		name, service, entity string
		effect                func(*fakeHA)
		note                  string
	}{
		{"unchanged", "light.turn_off", "light.kitchen", func(*fakeHA) {}, "still on after"},
		{"moved elsewhere", "light.turn_on", "light.hall", func(f *fakeHA) { f.states["light.hall"]["state"] = "unavailable" }, "ended unavailable after"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHA()
			f.service = func(f *fakeHA, _ string, _ map[string]any) any { tc.effect(f); return coreReply(200, []any{}) }
			h := newWriteHarness(t, f)
			if exit, env := h.run(runCallCommand, tc.service, tc.entity); exit != 6 || !strings.HasPrefix(env.Verification[0].Note, tc.note) {
				t.Fatalf("exit %d rows %+v", exit, env.Verification)
			}
		})
	}

	f := newFakeHA()
	var posted atomic.Bool
	var reads atomic.Int32
	f.service = func(*fakeHA, string, map[string]any) any { posted.Store(true); return coreReply(200, []any{}) }
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		if p["path"] == "/api/states/light.kitchen" && posted.Load() && reads.Add(1) > 1 {
			return coreReply(404, map[string]any{"message": "not found"})
		}
		return f.reply(r, endpoint, p)
	}, true)
	h := &writeHarness{t: t, f: f, paths: paths, log: log}
	if _, env := h.run(runCallCommand, "light.turn_off", "light.kitchen"); env.Verification[0].Note != "last read on; later reads found the entity missing" {
		t.Fatalf("late 404: %+v", env.Verification)
	}
}

// 5: a delete whose verification read fails says so instead of "absent".
func TestConfigDeleteVerifyReadFailed(t *testing.T) {
	f := newFakeHA()
	f.storeConfig("/api/config/automation/config/n1", map[string]any{"alias": "Night", "actions": []any{}})
	var deleted atomic.Bool
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		if p["method"] == "DELETE" {
			deleted.Store(true)
		} else if deleted.Load() && p["path"] == "/api/states/automation.night" {
			return fakeRelayFailure{"UPSTREAM_HTTP_TIMEOUT", "slow"}
		}
		return f.reply(r, endpoint, p)
	}, true)
	h := &writeHarness{t: t, f: f, paths: paths, log: log}
	_, env := h.run(runPlanCommand, "delete", "automation.night")
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
	if exit != 6 || applied.Verification[0].Observed != "read failed" {
		t.Fatalf("exit %d rows %+v", exit, applied.Verification)
	}
}

// 6: an empty state listing is an answer; a phrase missed by --state says
// to drop it; groups are marked.
func TestFindByStateDetails(t *testing.T) {
	f := newFakeHA()
	f.ent("light.all", "", "on", map[string]any{"entity_id": []any{"light.kitchen"}}) // a YAML light group
	f.ent("scene.movie", "", "on", map[string]any{"entity_id": []any{"light.kitchen"}})
	h := newWriteHarness(t, f)
	if exit, env := h.run(runFindCommand, "--domain", "fan", "--state", "on"); exit != 0 || !strings.Contains(string(env.Result), `"total":0`) {
		t.Fatalf("empty listing: exit %d %s", exit, env.Result)
	}
	if exit, env := h.run(runFindCommand, "kitchen", "--state", "off"); exit != 5 || !strings.Contains(env.Error.Next, "Drop --state") || strings.Contains(env.Error.Next, "include-disabled") {
		t.Fatalf("state miss: exit %d %+v", exit, env.Error)
	}
	_, env := h.run(runFindCommand, "--domain", "light", "--state", "on")
	var result findResult
	_ = json.Unmarshal(env.Result, &result)
	if _, scenes := h.run(runFindCommand, "--domain", "scene", "--state", "on"); strings.Contains(string(scenes.Result), `"group":true`) {
		t.Fatalf("a scene marked as group: %s", scenes.Result)
	}
	for _, m := range result.Matches {
		if m.State != "on" || m.Group != (m.EntityID == "light.all") || (m.Group && m.By != "state") {
			t.Fatalf("row %+v", m)
		}
	}
}

// 7, 8: disabled_by picks the UI path; registered but stateless is
// ENTITY_NOT_LOADED, never a "did you mean" loop on itself.
func TestDisabledAndUnloadedEntities(t *testing.T) {
	f := newFakeHA()
	for id, by := range map[string]string{"light.d": "device", "light.i": "integration", "light.c": "config_entry", "light.u": "user"} {
		f.full = append(f.full, map[string]any{"entity_id": id, "platform": "hue", "unique_id": id, "disabled_by": by})
	}
	f.unique["light.ghost"] = "g1"
	h := newWriteHarness(t, f)
	for id, want := range map[string]string{"light.d": "device is disabled", "light.i": "ha-nova:organize", "light.c": "integration is disabled", "light.u": "ha-nova:organize"} {
		if exit, env := h.run(runGetCommand, id); exit != 5 || env.Error.Code != "ENTITY_DISABLED" || !strings.Contains(env.Error.Next, want) {
			t.Fatalf("%s: exit %d env %+v", id, exit, env.Error)
		}
	}
	for _, run := range [][]string{{"get"}, {"call", "light.turn_on"}} {
		command := map[string]func(runtimePaths, []string) int{"get": runGetCommand, "call": runCallCommand}[run[0]]
		if exit, env := h.run(command, append(run[1:], "light.ghost")...); exit != 5 || env.Error.Code != "ENTITY_NOT_LOADED" {
			t.Fatalf("%v: exit %d env %+v", run, exit, env.Error)
		}
	}
	if len(h.posts()) != 0 {
		t.Fatal("an unloaded entity was called")
	}
}

// 11: history bounds and the MCP until argument.
func TestHistoryBounds(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	since, until, err := historyBounds("2026-10-15T09:00:00Z", "90m", now)
	if err != nil || !since.Equal(now.Add(-3*time.Hour)) || !until.Equal(now.Add(-90*time.Minute)) {
		t.Fatalf("since %v until %v err %v", since, until, err)
	}
	if _, _, err := historyBounds("24h", "8d", now); err == nil || !strings.Contains(err.Error(), "--until must be a duration ago of at most 7d") {
		t.Fatalf("--until 8d: %v", err)
	}
	for _, bad := range [][2]string{{"1h", "2h"}, {"9d", "1h"}, {"1h", "soon"}} {
		if _, _, err := historyBounds(bad[0], bad[1], now); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
	for _, tool := range mcpTools {
		if tool.name == "ha_history" {
			argv, err := tool.argv(map[string]json.RawMessage{"entity_id": json.RawMessage(`"light.hall"`), "since": json.RawMessage(`"2h"`), "until": json.RawMessage(`"1h"`)}, nil)
			if err != nil || !slices.Contains(argv, "--until=1h") {
				t.Fatalf("argv %v err %v", argv, err)
			}
		}
	}
}

// 15: an MCP client gets tool arguments for CLI flags and a no-shell path
// for skills; a plain next needs no rewrite.
func TestMCPNextStep(t *testing.T) {
	step := nextStep(map[string]any{"error": map[string]any{"next": refusalNext["CODE_REQUIRED"]}})
	if !strings.Contains(step, "confirm_code") || strings.Contains(step, "--confirm") {
		t.Fatalf("flag next %q", step)
	}
	step = nextStep(map[string]any{"error": map[string]any{"next": refusalNext["REVERT_UNSUPPORTED"]}})
	if !strings.Contains(step, "ha_plan with kind revert and id <config_id>") || !strings.Contains(step, "Home Assistant (Settings)") {
		t.Fatalf("skill next %q", step)
	}
	if step := nextStep(map[string]any{"error": map[string]any{"next": refusalNext["CODE_MISMATCH"]}}); step != "" {
		t.Fatalf("plain next rewritten: %q", step)
	}
	step = nextStep(map[string]any{"error": map[string]any{"next": refusalNext["CONFIG_NOT_UI_MANAGED"]}})
	if !strings.Contains(step, "Developer tools → YAML") || strings.Contains(step, "(Settings)") {
		t.Fatalf("yaml-config next %q", step)
	}
	step = nextStep(map[string]any{"error": map[string]any{"next": enableNext("device")}})
	if strings.Contains(step, "need a client with a shell") || step != "" {
		t.Fatalf("a next already naming Settings got the fallback: %q", step)
	}
	if step := nextStep(map[string]any{"error": map[string]any{"code": "SERVER_MISMATCH", "next": refusalNext["SERVER_MISMATCH"]}}); step != "This plan belongs to another server; plan again here." {
		t.Fatalf("server mismatch %q", step)
	}
	if step := nextStep(map[string]any{"error": map[string]any{"next": configIDInUseNext("script")}}); !strings.Contains(step, "id <new> create: true") {
		t.Fatalf("script collision %q", step)
	}
	temp := &mcpTempFiles{}
	t.Cleanup(temp.remove)
	argv, err := planArgv(map[string]json.RawMessage{"kind": json.RawMessage(`"write"`), "domain": json.RawMessage(`"script"`),
		"id": json.RawMessage(`"x"`), "create": json.RawMessage(`true`), "draft": json.RawMessage(`{}`)}, temp)
	if err != nil || !slices.Contains(argv, "--create") {
		t.Fatalf("ha_plan create argv %v err %v", argv, err)
	}
}
