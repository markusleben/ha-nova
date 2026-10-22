package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A Supervisor App's update entity, as Home Assistant shows it.
func appUpdateHA() *fakeHA {
	f := newFakeHA()
	f.ent("update.nova_relay_update", "hassio", "on", map[string]any{
		"friendly_name": "NOVA Relay Update", "installed_version": "0.10.0", "latest_version": "0.11.0",
		"in_progress": false, "update_percentage": nil, "skipped_version": nil, "auto_update": false,
		"release_summary": nil, "release_url": "https://example.invalid/0.11.0", "title": "NOVA Relay",
		"entity_picture": "/api/hassio/addons/x/icon", "supported_features": 29.0, "display_precision": 0.0, "_di": "app_device",
	})
	f.services = append(f.defaultServices(), map[string]any{"domain": "update", "services": map[string]any{"install": map[string]any{"target": map[string]any{}}}})
	return f
}

var appInstall = []string{"update.install", "update.nova_relay_update", "--set", "backup=true", "--skill", "ha-nova:updates"}

// cutHome: a call_service listener forces the scan of every automation, and
// the hung config read cuts it at the read budget (the 2026-10 report: an
// App update asked for its code twice, and both applies said it changed).
func cutHome(t *testing.T) *writeHarness {
	f := appUpdateHA()
	f.events = []map[string]any{{"event": "call_service", "listener_count": 1.0}}
	for i := range 30 {
		id := fmt.Sprintf("a%02d", i)
		f.automation("auto_"+id, id, map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.hall"}}, "actions": []any{step("light.turn_on", "light.kitchen")}})
	}
	saved := relaySessionOverallTimeout
	relaySessionOverallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { relaySessionOverallTimeout = saved })
	return newWriteHarness(t, f)
}

// cutAt makes the scan stop at another automation (each run gets as far as
// its reads allow).
func (h *writeHarness) cutAt(id string) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	h.f.hang = map[string]bool{"/api/config/automation/config/" + id: true}
	h.f.fail = map[string]bool{}
}

func TestApplyKeepsACutPlanThatDidNotChange(t *testing.T) {
	h := cutHome(t)
	h.cutAt("a10")
	_, planned := h.run(runCallCommand, appInstall...)
	if planned.Tier != "typed" || planned.Plan == nil || planned.Plan.Code == "" || !strings.Contains(planned.Say, sayCheckGap) {
		t.Fatalf("setup: %+v", planned)
	}
	h.cutAt("a20") // the recompute reads further: a different basis, the same plan
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != 0 || env.Plan.ID != planned.Plan.ID || len(h.posts()) != 1 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
	body, _ := json.Marshal(h.posts()[0].payload["body"])
	if string(body) != `{"backup":true,"entity_id":["update.nova_relay_update"]}` {
		t.Fatalf("sent %s", body)
	}
}

func TestApplyStillRefusesARealChangeInACutPlan(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(f *fakeHA)
		say    string
	}{
		"new version": {func(f *fakeHA) {
			f.states["update.nova_relay_update"]["attributes"].(map[string]any)["latest_version"] = "0.12.0"
		},
			"Since the last preview, “NOVA Relay Update” changed: latest version was 0.11.0, now 0.12.0. "},
		"install running": {func(f *fakeHA) {
			f.states["update.nova_relay_update"]["attributes"].(map[string]any)["in_progress"] = true
		},
			"Since the last preview, “NOVA Relay Update” changed: in progress was no, now yes. "},
		"a new consumer unlocks a door": {func(f *fakeHA) {
			f.related["entity:update.nova_relay_update"] = []string{"automation.auto_a01"}
			f.configs["/api/config/automation/config/a01"] = map[string]any{
				"triggers": []any{map[string]any{"trigger": "state", "entity_id": "update.nova_relay_update"}}, "actions": []any{step("lock.unlock", "lock.front")}}
		}, "HA NOVA checked again, and what this sets off looks different now. "},
	} {
		t.Run(name, func(t *testing.T) {
			h := cutHome(t)
			h.cutAt("a10")
			planned := h.plan(appInstall...)
			h.cutAt("a20")
			h.f.mu.Lock()
			tc.change(h.f)
			h.f.mu.Unlock()
			exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
			if exit != verbExitDrift || len(h.posts()) != 0 || env.Plan == nil || env.Plan.ID == planned.Plan.ID || !strings.HasPrefix(env.Say, tc.say) {
				t.Fatalf("exit %d say %q posts %d", exit, env.Say, len(h.posts()))
			}
			if env.Error == nil || !strings.Contains(env.Error.Next, "Never run it another way") {
				t.Fatalf("next %+v", env.Error)
			}
		})
	}
}

// A plan file edited to another target keeps no code: its fields no longer
// make its hash, so the recompute is a new plan with a new code.
func TestApplyRefusesAnEditedCutPlan(t *testing.T) {
	h := cutHome(t)
	twin := map[string]any{}
	for k, v := range h.f.states["update.nova_relay_update"]["attributes"].(map[string]any) {
		twin[k] = v
	}
	h.f.ent("update.other_app_update", "hassio", "on", twin) // reads exactly alike: only the re-hash tells them apart
	h.cutAt("a10")
	planned := h.plan(appInstall...)
	path := filepath.Join(planDir(h.paths), planned.Plan.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.ReplaceAll(string(raw), "update.nova_relay_update", "update.other_app_update")
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if len(h.posts()) != 0 {
		t.Fatalf("an edited plan ran: exit %d env %+v", exit, env)
	}
}

// What a Supervisor refresh rewrites (progress, notes, picture, title,
// timestamps) and a media player's changing feature bits are not drift.
func TestApplyIgnoresRefreshOnlyChanges(t *testing.T) {
	f := appUpdateHA()
	f.ent("media_player.tv", "cast", "playing", map[string]any{"friendly_name": "TV", "supported_features": 21437.0})
	h := newWriteHarness(t, f)
	update := h.plan(appInstall...).Plan
	pause := h.plan("media_player.media_pause", "media_player.tv", "--skill", "ha-nova:media").Plan
	h.f.mu.Lock()
	attrs := h.f.states["update.nova_relay_update"]["attributes"].(map[string]any)
	attrs["release_summary"], attrs["entity_picture"], attrs["title"], attrs["update_percentage"] = "Fixes", "/new.png", "NOVA Relay App", 0.0
	h.f.states["update.nova_relay_update"]["last_updated"] = "2026-10-22T11:53:00+00:00"
	h.f.states["media_player.tv"]["attributes"].(map[string]any)["supported_features"] = 152461.0 // another app on the TV
	h.f.mu.Unlock()
	for _, plan := range []*verbPlan{update, pause} {
		if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", "yes"); exit != 0 {
			t.Fatalf("%s: exit %d env %+v", plan.ID, exit, env)
		}
	}
}

// The window (and phone) ask runs the same check: a cut plan that did not
// change opens its window instead of refusing forever.
func TestWindowPlanKeepsACutPlanThatDidNotChange(t *testing.T) {
	useWindow(t, windowConfirmed)
	h := cutHome(t)
	h.cutAt("a10")
	planned := h.plan(appInstall...).Plan
	if planned.Channel != channelWindow {
		t.Fatalf("setup: %+v", planned)
	}
	h.cutAt("a20")
	if exit, env := h.run(runApplyCommand, "--plan", planned.ID); exit != 0 || len(h.posts()) != 1 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
}

// A config the preview read but the recompute could not read again might
// have changed: that is drift, not "the check got less far".
func TestApplyRefusesACutPlanThatCouldNotReadAgain(t *testing.T) {
	h := cutHome(t)
	h.f.script("morning", step("light.turn_on", "light.kitchen"))
	h.f.mu.Lock()
	h.f.fail = map[string]bool{"/api/config/automation/config/a10": true} // the scan stops early, the walk goes on
	h.f.mu.Unlock()
	planned := h.plan("script.turn_on", "script.morning")
	if planned.Plan.Code == "" {
		t.Fatalf("setup: %+v", planned)
	}
	h.cutAt("a10") // now the budget runs out before the script is read
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != verbExitDrift || len(h.posts()) != 0 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
}

// A full check (only a deterministic gap) stays exact: a new device in an
// area a script switches off is drift.
func TestApplyRefusesANewAreaMemberWithoutACut(t *testing.T) {
	f := newFakeHA()
	f.ent("switch.coffee", "shelly", "on", map[string]any{"_ai": "kitchen"})
	f.ent("switch.freezer", "shelly", "on", nil)
	f.full = []map[string]any{{"entity_id": "switch.coffee", "platform": "shelly", "area_id": "kitchen"}, {"entity_id": "switch.freezer", "platform": "shelly"}}
	f.script("evening", map[string]any{"action": "switch.turn_off", "target": map[string]any{"area_id": "kitchen"}}, map[string]any{"action": "{{ service }}"})
	f.related["area:kitchen"] = []string{"switch.coffee"}
	h := newWriteHarness(t, f)
	unchanged := h.plan("script.turn_on", "script.evening")
	if !slices.ContainsFunc(unchanged.Members, func(m tierMember) bool { return m.EntityID == "switch.coffee" }) {
		t.Fatalf("setup: members %+v", unchanged.Members)
	}
	if exit, env := h.run(runApplyCommand, "--plan", unchanged.Plan.ID, "--confirm", unchanged.Plan.Code); exit != 0 {
		t.Fatalf("unchanged: exit %d env %+v", exit, env)
	}
	planned := h.plan("script.turn_on", "script.evening").Plan
	h.f.mu.Lock()
	h.f.related["area:kitchen"] = []string{"switch.coffee", "switch.freezer"}
	h.f.full[1]["area_id"] = "kitchen"
	for _, row := range h.f.compact {
		if row["ei"] == "switch.freezer" {
			row["ai"] = "kitchen"
		}
	}
	h.f.mu.Unlock()
	if exit, env := h.run(runApplyCommand, "--plan", planned.ID, "--confirm", planned.Code); exit != verbExitDrift || len(h.posts()) != 1 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
}

// The preview's scan hit a failed read; the apply's ran out of budget. The
// hashes differ, the plan does not: the user's code still applies it.
func TestApplyKeepsACutPlanWhoseGapsDiffer(t *testing.T) {
	h := cutHome(t)
	h.f.mu.Lock()
	h.f.fail = map[string]bool{"/api/config/automation/config/a05": true}
	h.f.mu.Unlock()
	planned := h.plan(appInstall...)
	h.cutAt("a20")
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID, "--confirm", planned.Plan.Code)
	if exit != 0 || len(h.posts()) != 1 || env.Plan.Hash != planned.Plan.Hash {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
}
