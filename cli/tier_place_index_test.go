package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pairRelayInstance: the profile is paired with this Relay instance (the
// index is kept per home).
func pairRelayInstance(t *testing.T, paths runtimePaths, instance string) {
	t.Helper()
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelayInstanceID = instance
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
}

// MAR-149: automations found only where an entity sits are read once per
// last_changed of their state; the index never decides that one fires.
func TestPlaceIndexSkipsRoomAutomationsUntilTheyChange(t *testing.T) {
	f := newFakeHA()
	f.ent("light.kitchen", "hue", "off", map[string]any{"_ai": "kuche"})
	f.areas = []map[string]any{{"area_id": "kuche", "floor_id": "eg"}}
	stamp := func(object, at string) { f.states["automation."+object]["last_changed"] = at }
	room := func(object string, cfg map[string]any) {
		f.automation(object, object, cfg)
		stamp(object, "2026-11-12T01:00:00.000001+00:00")
		f.related["area-automation:kuche"] = append(f.related["area-automation:kuche"], "automation."+object)
	}
	for i := range 20 {
		room(fmt.Sprintf("room_%02d", i), map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.kitchen_motion"}},
			"actions":  []any{step("light.turn_on", "light.kitchen")},
		})
	}
	room("door", map[string]any{
		"triggers": []any{map[string]any{"trigger": "light.turned_on", "target": map[string]any{"area_id": "kuche"}}},
		"actions":  []any{step("lock.unlock", "lock.front")},
	})
	room("bp", map[string]any{"use_blueprint": map[string]any{"path": "homeassistant/motion_light.yaml", "input": map[string]any{}}})
	f.substitute = map[string]any{"substituted_config": map[string]any{
		"triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.hall_motion"}},
		"actions":  []any{step("light.turn_on", "light.kitchen")},
	}}
	paths, log := setupVerbRelayTest(t, f.reply)
	pairRelayInstance(t, paths, "relay-a")
	plan := func() (verbTestEnvelope, []string) {
		before := len(log.all())
		_, env := runVerbForTest(t, func() int { return runPlanCommand(paths, []string{"call", "light.turn_on", "light.kitchen"}) })
		var read []string
		for _, c := range log.all()[before:] {
			if c.endpoint == "ws" && c.payload["type"] == "automation/config" {
				read = append(read, fmt.Sprint(c.payload["entity_id"]))
			}
		}
		slices.Sort(read)
		return env, slices.Compact(read)
	}
	asksForTheDoor := func(env verbTestEnvelope, consumer string) {
		t.Helper()
		if env.Tier != "typed" || !slices.Contains(consumerList(env), consumer) {
			t.Fatalf("tier %q consumers %v (reasons %s), want typed with %s", env.Tier, consumerList(env), reasonCodes(env), consumer)
		}
	}

	env, cold := plan()
	asksForTheDoor(env, "automation.door")
	if len(cold) != 22 {
		t.Fatalf("a cold index reads %d configs %v, want all 22", len(cold), cold)
	}
	env, warm := plan()
	asksForTheDoor(env, "automation.door")
	if !slices.Equal(warm, []string{"automation.bp", "automation.door"}) {
		t.Fatalf("a warm index read %v, want only the blueprint instance and the door (a hit that targets the room is read live)", warm)
	}

	f.configs["/api/config/automation/config/room_07"] = map[string]any{
		"triggers": []any{map[string]any{"trigger": "light.turned_on", "target": map[string]any{"floor_id": "eg"}}},
		"actions":  []any{step("lock.unlock", "lock.front")},
	}
	stamp("room_07", "2026-11-12T02:00:00.000001+00:00") // Home Assistant re-adds the entity on any config change
	env, changed := plan()
	asksForTheDoor(env, "automation.room_07")
	if !slices.Contains(changed, "automation.room_07") || slices.Contains(changed, "automation.room_08") {
		t.Fatalf("after room_07 changed, read %v: want room_07 read again and room_08 still skipped", changed)
	}
}

func TestPlaceIndexFailsToALiveRead(t *testing.T) {
	setup := func(t *testing.T) (*fakeHA, runtimePaths, *fakeRelayLog) {
		f := newFakeHA()
		f.ent("light.kitchen", "hue", "off", map[string]any{"_ai": "kuche"})
		f.areas = []map[string]any{{"area_id": "kuche"}}
		f.automation("room", "room", map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.kitchen_motion"}},
			"actions":  []any{step("light.turn_on", "light.kitchen")},
		})
		f.related["area-automation:kuche"] = []string{"automation.room"}
		paths, log := setupVerbRelayTest(t, f.reply)
		pairRelayInstance(t, paths, "relay-a")
		return f, paths, log
	}
	reads := func(t *testing.T, paths runtimePaths, log *fakeRelayLog) int {
		before := len(log.all())
		if exit, env := runVerbForTest(t, func() int { return runPlanCommand(paths, []string{"call", "light.turn_on", "light.kitchen"}) }); exit != 0 || env.Tier != "ordinary" {
			t.Fatalf("exit %d tier %q (reasons %s), want ordinary", exit, env.Tier, reasonCodes(env))
		}
		n := 0
		for _, c := range log.all()[before:] {
			if c.endpoint == "ws" && c.payload["type"] == "automation/config" {
				n++
			}
		}
		return n
	}
	t.Run("a state without last_changed is never indexed", func(t *testing.T) {
		_, paths, log := setup(t)
		if reads(t, paths, log) != 1 || reads(t, paths, log) != 1 {
			t.Fatal("want one live read per plan")
		}
	})
	t.Run("a malformed index file is an empty one, and is replaced", func(t *testing.T) {
		f, paths, log := setup(t)
		f.states["automation.room"]["last_changed"] = "2026-11-12T01:00:00+00:00"
		file := filepath.Join(paths.CacheDir, "place-index.json")
		if err := os.MkdirAll(paths.CacheDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if reads(t, paths, log) != 1 || reads(t, paths, log) != 0 {
			t.Fatal("want a live read, then an index hit")
		}
	})
	t.Run("an index of another home is an empty one", func(t *testing.T) {
		f, paths, log := setup(t)
		f.states["automation.room"]["last_changed"] = "2026-11-12T01:00:00+00:00"
		if reads(t, paths, log) != 1 || reads(t, paths, log) != 0 {
			t.Fatal("want a live read, then an index hit")
		}
		pairRelayInstance(t, paths, "relay-b")
		if reads(t, paths, log) != 1 {
			t.Fatal("another Relay instance used the first home's index")
		}
	})
	t.Run("an unavailable automation is never indexed", func(t *testing.T) {
		f, paths, log := setup(t)
		f.states["automation.room"]["state"] = "unavailable"
		f.states["automation.room"]["last_changed"] = "2026-11-12T01:00:00+00:00"
		if reads(t, paths, log) != 1 || reads(t, paths, log) != 1 {
			t.Fatal("want one live read per plan")
		}
	})
	t.Run("a config read before the state is never paired with it", func(t *testing.T) {
		f, paths, _ := setup(t)
		f.states["automation.room"]["last_changed"] = "2026-11-12T01:00:00+00:00"
		s, err := newRelaySession(paths, verbOptions{})
		if err != nil {
			t.Fatal(err)
		}
		e := newTierEngine(s)
		e.warmConfig("automation.room") // the old config, memoized (read-ahead, the listener census)
		f.mu.Lock()
		f.configs["/api/config/automation/config/room"] = map[string]any{
			"triggers": []any{map[string]any{"trigger": "light.turned_on", "target": map[string]any{"area_id": "kuche"}}},
			"actions":  []any{step("lock.unlock", "lock.front")},
		}
		f.states["automation.room"]["last_changed"] = "2026-11-12T02:00:00+00:00" // reloaded with the new config
		f.mu.Unlock()
		e.placeOnlyMiss("automation.room", [][2]string{{"area", "kuche"}})
		s.close()
		if data, _ := os.ReadFile(filepath.Join(paths.CacheDir, "place-index.json")); strings.Contains(string(data), "automation.room") {
			t.Fatalf("the old config was saved under the new stamp: %s", data)
		}
	})
	t.Run("a replay skips only what its preview skipped, at the same stamp", func(t *testing.T) {
		f, paths, _ := setup(t)
		f.states["automation.room"]["last_changed"] = "2026-11-12T01:00:00+00:00"
		places := [][2]string{{"area", "kuche"}}
		s, err := newRelaySession(paths, verbOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer s.close()
		e := newTierEngine(s)
		state, _ := json.Marshal(map[string]any{"method": "GET", "path": "/api/states/automation.room"})
		s.replay = map[string]bool{"core " + string(state): true, fmt.Sprintf("place-index automation.room 2026-11-12T01:00:00+00:00 %v", places): true}
		if !e.placeOnlyMiss("automation.room", places) || s.diverged {
			t.Fatalf("the preview's skip was not replayed (diverged %v)", s.diverged)
		}
		s.replay = map[string]bool{"core " + string(state): true}
		if e.placeOnlyMiss("automation.room", [][2]string{{"area", "flur"}}) || !s.diverged {
			t.Fatalf("a skip the preview never made: want a read the replay lacks (diverged), got diverged %v", s.diverged)
		}
	})
	t.Run("a target that is no map, or null, targets the place", func(t *testing.T) {
		for _, target := range []any{nil, "{{ area }}", []any{"kuche"}} {
			if !targetsPlace(map[string]any{"triggers": []any{map[string]any{"target": target}}}, [][2]string{{"area", "kuche"}}) {
				t.Fatalf("target %#v: want a match", target)
			}
		}
		if targetsPlace(map[string]any{"triggers": []any{map[string]any{"target": map[string]any{"area_id": "kuche"}, "enabled": false}}}, [][2]string{{"area", "kuche"}}) {
			t.Fatal("a disabled trigger matched")
		}
	})
}
