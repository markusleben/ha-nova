package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reported edit: an existing automation gains a second state trigger, an
// id on the first, and a template condition. Both checks share one tier.

func frameHome() *fakeHA {
	f := newFakeHA()
	f.ent("light.frame", "hue", "off", nil)
	f.ent("sensor.feels_like", "template", "21", nil)
	f.ent("binary_sensor.bewegung_treppe", "zha", "off", nil)
	f.ent("binary_sensor.kontaktsensor_haustur_contact", "zha", "off", nil)
	f.ent("input_boolean.presence", "input_boolean", "on", nil)
	f.storeConfig("/api/config/automation/config/frame_stairs", map[string]any{
		"alias":    "The Frame Kunstmodus bei Bewegung Treppe",
		"triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.bewegung_treppe", "to": "on"}},
		"actions":  []any{step("light.turn_on", "light.frame")},
	})
	return f
}

func frameDraft() map[string]any {
	return map[string]any{
		"alias": "The Frame Kunstmodus bei Bewegung Treppe",
		"triggers": []any{
			map[string]any{"id": "treppe", "trigger": "state", "entity_id": "binary_sensor.bewegung_treppe", "to": "on"},
			map[string]any{"trigger": "state", "entity_id": "binary_sensor.kontaktsensor_haustur_contact", "to": "on"},
		},
		"conditions": []any{map[string]any{
			"condition":      "template",
			"value_template": "{{ trigger.id == 'treppe' or (as_timestamp(now()) - as_timestamp(states.input_boolean.presence.last_changed)) < 600 }}",
		}},
		"actions": []any{step("light.turn_on", "light.frame")},
	}
}

func planFrame(t *testing.T, f *fakeHA) (writeHarness, verbTestEnvelope) {
	t.Helper()
	h := newWriteHarness(t, f)
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "frame_stairs", "--file", draftFile(t, frameDraft()))
	if env.Plan == nil || env.Config == nil {
		t.Fatalf("plan: %+v", env)
	}
	return *h, env
}

func gateOf(t *testing.T, h writeHarness, env verbTestEnvelope) gateCheck {
	t.Helper()
	sent, err := json.Marshal(map[string]any{"method": "POST", "path": env.Config.Path, "body": env.Config.Body})
	if err != nil {
		t.Fatal(err)
	}
	var s *relaySession
	t.Cleanup(func() {
		if s != nil {
			s.close()
		}
	})
	check, err := gateVerdict("core", sent, func() (*relaySession, error) {
		opened, err := newRelaySession(h.paths, verbOptions{})
		s = opened
		return opened, err
	})
	if err != nil {
		t.Fatal(err)
	}
	return check
}

func TestFrameTriggerEditAgreesWithTheGate(t *testing.T) {
	h, env := planFrame(t, frameHome())
	if env.Tier == "typed" || env.Coverage == nil || !strings.Contains(strings.Join(env.Coverage.Notes, ","), noteTemplates) {
		t.Fatalf("plan tier %s notes %v reasons %s", env.Tier, env.Coverage, reasonCodes(env))
	}
	if check := gateOf(t, h, env); check.Verdict != gatePass {
		t.Fatalf("gate %+v for a %s plan", check, env.Tier)
	}
}

func TestFrameTriggerEditAgreesWhenTheRegistryCannotBeRead(t *testing.T) {
	f := frameHome()
	f.fail["config/entity_registry/list_for_display"] = true
	h, env := planFrame(t, f)
	if env.Tier != "typed" || !strings.Contains(reasonCodes(env), reasonScanFailed) || !scanGapTyped(env.Reasons) {
		t.Fatalf("plan tier %s reasons %s", env.Tier, reasonCodes(env))
	}
	if check := gateOf(t, h, env); check.Verdict != gateTyped {
		t.Fatalf("gate %+v, want typed", check)
	}
}

// The App finishes the registry read and does not hold the save. This
// computer, which could not read it, asks in the window and then sends.
func TestGateScanGapAsksOnThisComputer(t *testing.T) {
	f := frameHome()
	f.fail["config/entity_registry/list_for_display"] = true
	g := &fakeGate{notNeeded: true}
	h := gatedHarness(t, f, g)
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "frame_stairs", "--file", draftFile(t, frameDraft()))
	if env.Plan == nil || env.Tier != "typed" || !scanGapTyped(env.Reasons) {
		t.Fatalf("plan tier %s reasons %s err %+v", env.Tier, reasonCodes(env), env.Error)
	}
	w := useWindow(t, windowConfirmed)
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	asks, _, sends := g.snapshot()
	if exit != 0 || applied.Error != nil || len(asks) != 1 || len(sends) != 1 || sends[0].grant != "" || len(w.messages) != 1 ||
		applied.Plan == nil || applied.Plan.Channel != channelWindow {
		t.Fatalf("exit %d %+v asks %d sends %+v windows %q", exit, applied.Error, len(asks), sends, w.messages)
	}
}

func TestGateScanGapCancelSendsNothing(t *testing.T) {
	f := frameHome()
	f.fail["config/entity_registry/list_for_display"] = true
	g := &fakeGate{notNeeded: true}
	h := gatedHarness(t, f, g)
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "frame_stairs", "--file", draftFile(t, frameDraft()))
	useWindow(t, windowCancelled)
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	_, _, sends := g.snapshot()
	if exit != verbExitRefused || applied.Error == nil || applied.Error.Code != "CONFIRM_DECLINED" || len(sends) != 0 || len(h.planFiles()) != 1 {
		t.Fatalf("exit %d %+v sends %d plans %d", exit, applied.Error, len(sends), len(h.planFiles()))
	}
}

// An unlock the App does not hold is still refused. The update line is only
// for an App older than this CLI's floor.
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
			if err := os.MkdirAll(filepath.Dir(h.paths.VersionFile), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(h.paths.VersionFile, []byte(`{"skill_version":"0.31.0","min_relay_version":"0.13.27"}`), 0o644); err != nil {
				t.Fatal(err)
			}
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			_, _, sends := g.snapshot()
			update := env.Error != nil && strings.Contains(env.Error.Next, "Update HA NOVA on this computer")
			if exit != verbExitRefused || env.Error == nil || env.Error.Code != "GATE_DISAGREES" || len(sends) != 0 || update != tc.update {
				t.Fatalf("exit %d %+v update %v sends %d", exit, env.Error, update, len(sends))
			}
		})
	}
}
