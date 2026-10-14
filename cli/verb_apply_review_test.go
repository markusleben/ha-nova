package main

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Regressions from the PR3 review round (two adversarial reviews + codex).

// tamper rewrites one stored plan file through edit.
func (h *writeHarness) tamper(id string, edit func(map[string]any)) {
	h.t.Helper()
	path := filepath.Join(planDir(h.paths), id+".json")
	raw, err := os.ReadFile(path)
	var plan map[string]any
	if err == nil {
		err = json.Unmarshal(raw, &plan)
	}
	if err != nil {
		h.t.Fatal(err)
	}
	edit(plan)
	raw, _ = json.Marshal(plan)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		h.t.Fatal(err)
	}
}

// Items 1 and 2: tier, code, kind, and request come from the recomputed plan
// or are re-validated; file fields are never trusted.
func TestApplyNeverTrustsPlanFileFields(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	id := h.plan("lock.unlock", "lock.front").Plan.ID
	h.tamper(id, func(p map[string]any) { p["tier"] = "preview" })
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Error.Code != "PLAN_INVALID" {
		t.Fatalf("tier edited to preview: exit %d env %+v", exit, env)
	}
	plan := h.plan("lock.unlock", "lock.front").Plan
	h.tamper(plan.ID, func(p map[string]any) { p["code"] = "confirm:edited" })
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", "confirm:edited"); exit != 4 || env.Error.Code != "CODE_MISMATCH" {
		t.Fatalf("code edited: exit %d env %+v", exit, env)
	}
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code); exit != 0 || env.Plan.Code != plan.Code {
		t.Fatalf("derived code: exit %d env %+v", exit, env)
	}
	for name, edit := range map[string]func(map[string]any){
		"kind": func(p map[string]any) { p["kind"] = "write" },
		"request data": func(p map[string]any) {
			p["request"].(map[string]any)["data"] = map[string]any{"entity_id": "lock.other"}
		},
	} {
		id := h.plan("light.turn_off", "light.kitchen").Plan.ID
		h.tamper(id, edit)
		if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Error.Code != "PLAN_INVALID" {
			t.Fatalf("%s edited: exit %d env %+v", name, exit, env)
		}
	}
	id = h.plan("light.turn_off", "light.kitchen").Plan.ID
	h.tamper(id, func(p map[string]any) { p["request"].(map[string]any)["service"] = "mqtt.publish" })
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Reasons[0].Code != reasonOwnedService {
		t.Fatalf("owned service in the stored request: exit %d env %+v", exit, env)
	}
	if len(h.posts()) != 1 {
		t.Fatalf("%d posts, want only the legitimate one", len(h.posts()))
	}
}

// Item 3: listener counts and untouched fields are no drift; changed
// before-values are.
func TestApplyDriftBasisIgnoresNoise(t *testing.T) {
	f := newFakeHA()
	f.events = []map[string]any{{"event": "call_service", "listener_count": 1}}
	f.ent("climate.living", "ecobee", "heat", map[string]any{"temperature": 20.0})
	h := newWriteHarness(t, f)
	id := h.plan("light.turn_off", "light.kitchen").Plan.ID // preview: unexplained listener
	h.f.events[0]["listener_count"] = 2
	if exit, env := h.run(runApplyCommand, "--plan", id); exit == 2 {
		t.Fatalf("a listener-count change drifted: %+v", env)
	}
	id = h.plan("climate.set_temperature", "climate.living", "--set", "temperature=21").Plan.ID
	h.f.states["climate.living"]["state"] = "cool" // set_temperature does not change the state
	if exit, env := h.run(runApplyCommand, "--plan", id); exit == 2 {
		t.Fatalf("an unrelated state change drifted: %+v", env)
	}
	id = h.plan("climate.set_temperature", "climate.living", "--set", "temperature=22").Plan.ID
	h.f.states["climate.living"]["attributes"].(map[string]any)["temperature"] = 19.0
	if exit, _ := h.run(runApplyCommand, "--plan", id); exit != 2 {
		t.Fatalf("a changed before-value did not drift: exit %d", exit)
	}
}

// Items 4-9: verification honesty.
func TestVerificationHonesty(t *testing.T) {
	sceneSetup := func(leafToo bool) *fakeHA {
		f := newFakeHA()
		f.states["lock.front"]["state"] = "unlocked"
		f.ent("group.doors", "group", "unlocked", map[string]any{"entity_id": []any{"lock.front"}})
		f.ent("scene.away", "homeassistant", "unknown", nil)
		f.unique["scene.away"] = "a1"
		f.configs["/api/config/scene/config/a1"] = map[string]any{"entities": map[string]any{"group.doors": "locked"}}
		f.service = func(f *fakeHA, path string, body map[string]any) any {
			f.states["group.doors"]["state"] = "locked"
			if leafToo {
				f.states["lock.front"]["state"] = "locked"
			}
			return coreReply(200, []any{})
		}
		return f
	}
	for leafToo, want := range map[bool]string{true: "verified", false: "unverified"} {
		h := newWriteHarness(t, sceneSetup(leafToo))
		id := h.plan("scene.turn_on", "scene.away").Plan.ID
		if _, env := h.run(runApplyCommand, "--plan", id); env.Status != want || len(env.Verification) != 2 {
			t.Fatalf("scene via group (leaf changes: %v): %+v", leafToo, env)
		}
	}

	brightness := func(level float64) func(*fakeHA, string, map[string]any) any {
		return func(f *fakeHA, path string, body map[string]any) any {
			f.states["light.hall"]["state"] = "on"
			f.states["light.hall"]["attributes"] = map[string]any{"brightness": level}
			return coreReply(200, []any{})
		}
	}
	cases := []struct {
		name    string
		service func(*fakeHA, string, map[string]any) any
		args    []string
		exit    int
		status  string
	}{
		{"brightness reached", brightness(102), []string{"light.turn_on", "light.hall", "--set", "brightness_pct=40"}, 0, "verified"},
		{"brightness missed", brightness(255), []string{"light.turn_on", "light.hall", "--set", "brightness_pct=40"}, 6, "unverified"},
		{"unprobed field is only accepted", brightness(255), []string{"light.turn_on", "light.hall", "--set", "hs_color=[30,50]"}, 0, "accepted"},
		{"select options compare literally", func(f *fakeHA, _ string, _ map[string]any) any {
			f.states["input_select.mode"]["state"] = "on"
			return coreReply(200, []any{})
		}, []string{"input_select.select_option", "input_select.mode", "--set", "option=on|off"}, 6, "unverified"},
		{"a cover still moving at the end is accepted", func(f *fakeHA, _ string, _ map[string]any) any {
			f.states["cover.blind"]["state"] = "opening"
			return coreReply(200, []any{})
		}, []string{"cover.open_cover", "cover.blind"}, 0, "accepted"},
		{"proven rejection is failed", func(*fakeHA, string, map[string]any) any { return coreReply(422, map[string]any{"message": "bad"}) },
			[]string{"light.turn_on", "light.hall"}, 6, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeHA()
			f.ent("input_select.mode", "input_select", "off", nil)
			f.ent("cover.blind", "somfy", "closed", map[string]any{"device_class": "blind"})
			f.service = tc.service
			h := newWriteHarness(t, f)
			exit, env := h.run(runCallCommand, tc.args...)
			if exit != tc.exit || env.Status != tc.status {
				t.Fatalf("exit %d status %s, want %d %s: %+v", exit, env.Status, tc.exit, tc.status, env)
			}
			if tc.name == "a cover still moving at the end is accepted" && !strings.Contains(env.Verification[0].Note, "opening") {
				t.Fatalf("note %+v", env.Verification)
			}
		})
	}
}

// Item 7: the read budget is renewed after the POST, so a slow service call
// cannot starve verification.
func TestVerificationGetsAFreshBudgetAfterASlowCall(t *testing.T) {
	previous := relaySessionOverallTimeout
	relaySessionOverallTimeout = 400 * time.Millisecond
	t.Cleanup(func() { relaySessionOverallTimeout = previous })
	f := newFakeHA()
	f.service = func(f *fakeHA, path string, body map[string]any) any {
		time.Sleep(600 * time.Millisecond)
		return applies(f, path, body)
	}
	h := newWriteHarness(t, f)
	if exit, env := h.run(runCallCommand, "light.turn_off", "light.kitchen"); exit != 0 || env.Status != "verified" {
		t.Fatalf("exit %d env %+v", exit, env)
	}
}

// Item 10: response-only services are refused, statically and from
// /api/services, before anything is sent.
func TestResponseOnlyServicesAreRefused(t *testing.T) {
	f := newFakeHA()
	f.services = []map[string]any{{"domain": "music", "services": map[string]any{"search": map[string]any{"response": map[string]any{"optional": false}}}}}
	h := newWriteHarness(t, f)
	for _, args := range [][]string{{"weather.get_forecasts", "--set", "type=daily"}, {"music.search", "--set", "q=x"}} {
		if exit, env := h.run(runCallCommand, args...); exit != 4 || env.Reasons[0].Code != reasonResponseOnly {
			t.Fatalf("%v: exit %d env %+v", args, exit, env)
		}
	}
	if len(h.posts()) != 0 {
		t.Fatalf("posts %+v", h.posts())
	}
}

// pointRelayAt rewrites the saved Relay URL (a closed server = connection refused).
func (h *writeHarness) pointRelayAt(url string) {
	h.t.Helper()
	cfg, err := loadConfig(h.paths)
	if err != nil {
		h.t.Fatal(err)
	}
	cfg.RelayBaseURL = url
	if err := saveConfig(h.paths, cfg); err != nil {
		h.t.Fatal(err)
	}
}

// A Relay that never answers is an outage (exit 1, nothing planned or
// stored), never a typed plan full of SCAN_FAILED gaps.
func TestUnreachableRelayIsAnOperationalError(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	cfg, err := loadConfig(h.paths)
	if err != nil {
		t.Fatal(err)
	}
	live := cfg.RelayBaseURL
	id := h.plan("lock.unlock", "lock.front").Plan.ID
	code := ""
	if raw, err := os.ReadFile(filepath.Join(planDir(h.paths), id+".json")); err == nil {
		var stored storedPlan
		_ = json.Unmarshal(raw, &stored)
		code = stored.Code
	}
	dead := httptest.NewServer(nil)
	dead.Close()
	h.pointRelayAt(dead.URL)
	for _, args := range [][]string{{"call", "cover.open_cover", "cover.garage_door"}, {"plan", "call", "cover.open_cover", "cover.garage_door"}} {
		command := map[string]func(runtimePaths, []string) int{"call": runCallCommand, "plan": runPlanCommand}[args[0]]
		exit, env := h.run(command, args[1:]...)
		if exit != 1 || env.Error == nil || env.Error.Code != "RELAY_ERROR" || env.Tier != "" || env.Plan != nil {
			t.Fatalf("%v: exit %d env %+v", args, exit, env)
		}
	}
	if exit, env := h.run(runApplyCommand, "--plan", id, "--confirm", code); exit != 1 || env.Error.Code != "RELAY_ERROR" {
		t.Fatalf("apply: exit %d env %+v", exit, env)
	}
	if files := h.planFiles(); len(files) != 1 || !strings.HasSuffix(files[0], id+".json") {
		t.Fatalf("plan files %v: want only the original plan, released", files)
	}
	h.pointRelayAt(live)
	if exit, env := h.run(runApplyCommand, "--plan", id, "--confirm", code); exit != 0 || len(h.posts()) != 1 {
		t.Fatalf("apply after the outage: exit %d env %+v", exit, env)
	}
}
