package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRelayFloor(t *testing.T, h *writeHarness) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.paths.VersionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.paths.VersionFile, []byte(`{"skill_version":"0.31.0","min_relay_version":"0.13.27"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The update line is only for an App older than this CLI's floor. A current
// App, or one whose version was not reported, is told to plan again.
func TestGateDisagreesNamesAnUpdateOnlyWhenTheAppIsOlder(t *testing.T) {
	for _, tc := range []struct {
		version string
		update  bool
	}{
		{"", false},
		{"0.13.27", false},
		{"0.13.26", true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			g := &fakeGate{notNeeded: true}
			h := gatedHarness(t, newFakeHA(), g)
			h.log.version = tc.version
			writeRelayFloor(t, h)
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			_, _, sends := g.snapshot()
			update := env.Error != nil && strings.Contains(env.Error.Next, "Update HA NOVA on this computer")
			again := env.Error != nil && strings.Contains(env.Error.Next, "Plan the change again")
			if exit != verbExitRefused || env.Error == nil || env.Error.Code != "GATE_DISAGREES" || len(sends) != 0 || update != tc.update || again == tc.update ||
				strings.Contains(env.Error.Next, "entity registry") {
				t.Fatalf("exit %d %+v update %v sends %d", exit, env.Error, update, len(sends))
			}
		})
	}
}

// A save this computer typed because the entity registry could not be read
// is still refused when the App does not hold it, and the message says so.
func TestGateDisagreesNamesAFailedRegistryRead(t *testing.T) {
	draft := map[string]any{
		"alias":    "The Frame Kunstmodus bei Bewegung Treppe",
		"triggers": []any{map[string]any{"id": "treppe", "trigger": "state", "entity_id": "binary_sensor.bewegung_treppe", "to": "on"}},
		"conditions": []any{map[string]any{
			"condition": "template", "value_template": "{{ trigger.id == 'treppe' }}",
		}},
		"actions": []any{step("light.turn_on", "light.frame")},
	}
	for _, tc := range []struct {
		version string
		update  bool
	}{
		{"0.13.27", false},
		{"0.13.26", true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			f := newFakeHA()
			f.ent("light.frame", "hue", "off", nil)
			f.storeConfig("/api/config/automation/config/frame_stairs", map[string]any{
				"alias": "The Frame Kunstmodus bei Bewegung Treppe", "triggers": []any{}, "actions": []any{step("light.turn_on", "light.frame")},
			})
			f.fail["config/entity_registry/list_for_display"] = true
			g := &fakeGate{notNeeded: true}
			h := gatedHarness(t, f, g)
			h.log.version = tc.version
			writeRelayFloor(t, h)
			_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "frame_stairs", "--file", draftFile(t, draft))
			if env.Plan == nil || env.Tier != "typed" || !registryReadFailed(env.Reasons) {
				t.Fatalf("plan tier %s reasons %s err %+v", env.Tier, reasonCodes(env), env.Error)
			}
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			_, _, sends := g.snapshot()
			update := applied.Error != nil && strings.Contains(applied.Error.Next, "Update HA NOVA on this computer")
			if exit != verbExitRefused || applied.Error == nil || applied.Error.Code != "GATE_DISAGREES" || len(sends) != 0 || len(h.planFiles()) != 1 ||
				update != tc.update || !strings.Contains(applied.Error.Next, "couldn't read the entity registry") {
				t.Fatalf("exit %d %+v update %v sends %d plans %d", exit, applied.Error, update, len(sends), len(h.planFiles()))
			}
		})
	}
}
