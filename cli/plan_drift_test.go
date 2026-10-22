package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
		"entity_picture": "/api/hassio/addons/x/icon", "supported_features": 29.0, "display_precision": 0.0,
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
		"edited automation both runs read": {func(f *fakeHA) {
			f.configs["/api/config/automation/config/a03"] = map[string]any{"triggers": []any{}, "actions": []any{step("light.turn_off", "light.kitchen")}}
		}, "Since the last preview, an automation or script this sets off was changed. "},
		"a new consumer unlocks a door": {func(f *fakeHA) {
			f.related["entity:update.nova_relay_update"] = []string{"automation.auto_a01"}
			f.configs["/api/config/automation/config/a01"] = map[string]any{
				"triggers": []any{map[string]any{"trigger": "state", "entity_id": "update.nova_relay_update"}}, "actions": []any{step("lock.unlock", "lock.front")}}
		}, "Since the last preview, "},
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
			if env.Error == nil || !strings.Contains(env.Error.Next, "Never run this another way") {
				t.Fatalf("next %+v", env.Error)
			}
		})
	}
}

// A plan file edited to another target keeps no code: its fields no longer
// make its hash, so the recompute is a new plan with a new code.
func TestApplyRefusesAnEditedCutPlan(t *testing.T) {
	h := cutHome(t)
	h.f.ent("update.other_app_update", "hassio", "on", map[string]any{"installed_version": "1.0", "latest_version": "2.0", "supported_features": 29.0})
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
	if exit == 0 || len(h.posts()) != 0 {
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
