package main

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Device deletion (docs/work/2026-10-26-device-delete-spec.md): the shared
// raw guard refuses a device with a lock, alarm panel, or access cover (its
// child devices included), HA NOVA's own App device, an unknown device, and
// an unreadable registry; a deletable device plans typed, named with the
// entities that go, and applies after its snapshot.

func deviceDeleteRelay(t *testing.T, devices []any) (runtimePaths, func() []string) {
	t.Helper()
	return rawRelay(t, map[string]func(map[string]any) any{
		"config/device_registry/list": func(map[string]any) any { return devices },
		"config/entity_registry/list": func(map[string]any) any {
			return []any{
				map[string]any{"entity_id": "lock.front", "device_id": "dlock", "config_entry_id": "E1"},
				map[string]any{"entity_id": "lock.gate", "device_id": "dchildlock", "config_entry_id": "E1"},
				map[string]any{"entity_id": "light.hall", "device_id": "dlamp", "config_entry_id": "E1"},
				map[string]any{"entity_id": "sensor.hall_power", "device_id": "dlamp", "config_entry_id": "E1"},
				map[string]any{"entity_id": "switch.hall_template", "device_id": "dlamp", "config_entry_id": "E2"},
				map[string]any{"entity_id": "sensor.hall_child", "device_id": "dchild", "config_entry_id": "E1"},
			}
		},
	})
}

func deleteDevicePlan(t *testing.T, paths runtimePaths, device string) (int, verbTestEnvelope) {
	t.Helper()
	return planRaw(t, paths, `[{"endpoint":"backups","payload":{"action":"save","category":"metadata","name":"auto-device","data":{}}},
		{"endpoint":"ws","payload":{"type":"config/device_registry/remove","device_id":"`+device+`"}}]`)
}

func TestDeviceDeleteRefusals(t *testing.T) {
	paths, sent := deviceDeleteRelay(t, []any{
		map[string]any{"id": "dlock", "name": "Front door", "config_entry_id": "E1"},
		map[string]any{"id": "dgate", "name": "Gate hub", "config_entry_id": "E1"},
		map[string]any{"id": "dchildlock", "name": "Gate", "config_entry_id": "E1", "parent_device_id": "dgate"},
		map[string]any{"id": "drelay", "name": "NOVA Relay", "config_entry_id": "H1", "identifiers": []any{[]any{"hassio", "2368fcfa_ha_nova_relay"}}},
		map[string]any{"id": "dnuki", "name": "Haustür", "config_entry_id": "E3", "identifiers": []any{[]any{"nuki", 743657972}}}, // a number, as Nuki stores it
	})
	for device, want := range map[string]string{
		"dlock":  "deletion of device dlock, which has lock.front",
		"dgate":  "deletion of device dgate, which has lock.gate",
		"drelay": "HA NOVA's own App device",
		"dgone":  "deletion of unknown device dgone",
		"":       "device deletion without its device_id",
	} {
		exit, env := deleteDevicePlan(t, paths, device)
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, want) {
			t.Errorf("%q: exit %d %+v, want refusal %q", device, exit, env.Error, want)
		}
	}
	for _, device := range []string{"dlock", "drelay"} {
		var exit int
		stderr := captureStderr(t, func() {
			exit = runRelayCommand(paths, []string{"ws", "--data", `{"type":"config/device_registry/remove","device_id":"` + device + `"}`})
		})
		if exit != verbExitRefused || !strings.Contains(stderr, "nothing was sent") {
			t.Errorf("relay %s: exit %d, stderr %q", device, exit, stderr)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("refused deletions sent %v", got)
	}
}

func TestDeviceDeleteUnreadableRegistrySendsNothing(t *testing.T) {
	paths, sent := rawRelay(t, nil) // the device registry answers an object, not a list
	exit, env := deleteDevicePlan(t, paths, "dlamp")
	if exit == verbExitConfirmation || env.Plan != nil || env.Error == nil || !strings.Contains(env.Error.Message, "cannot read which devices") {
		t.Fatalf("exit %d %+v", exit, env.Error)
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("sent %v", got)
	}
}

func TestDeviceDeleteTypedNamedAndApplied(t *testing.T) {
	paths, sent := deviceDeleteRelay(t, []any{
		map[string]any{"id": "dlamp", "name": "Hall lamp", "name_by_user": nil, "config_entry_id": "E1"},
		map[string]any{"id": "dchild", "name": "Hall lamp LED", "config_entry_id": "E1", "parent_device_id": "dlamp"},
	})
	exit, env := deleteDevicePlan(t, paths, "dlamp")
	if exit != verbExitConfirmation || env.Tier != "typed" || env.Plan == nil {
		t.Fatalf("plan: exit %d %+v", exit, env)
	}
	plan, err := readPlanFile(filepath.Join(planDir(paths), env.Plan.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	want := "I'm about to delete the device “Hall lamp”, its 1 child device, and its 3 entities. Detail: device ID “dlamp”. HA NOVA saves a copy first (“metadata/auto-device”)."
	if !strings.Contains(env.Say, want) || !strings.Contains(plan.Prompt, want) || !strings.Contains(env.Say, "This deletes") {
		t.Fatalf("say or prompt lacks %q:\n%s\n---\n%s", want, env.Say, plan.Prompt)
	}
	_, applied := applyRaw(t, paths, env)
	if got := statuses(rawOutcomes(t, applied)); got != "done,done" {
		t.Fatalf("apply %s", applied.Result)
	}
	if got := strings.Join(sent(), " | "); got != "backups save | config/device_registry/remove" {
		t.Fatalf("sent %s", got)
	}
}

// What a device delete removes is part of the plan: an entity or child
// device added since the preview is drift, and nothing is sent.
func TestDeviceDeleteMembershipDrift(t *testing.T) {
	for _, added := range []map[string]any{
		{"entity_id": "switch.hall_template", "device_id": "dlamp", "config_entry_id": "E2"}, // another integration's: unlinked, not counted
		{"entity_id": "sensor.hall_power", "device_id": "dlamp", "config_entry_id": "E1"},
	} {
		var mu sync.Mutex
		entities := []any{map[string]any{"entity_id": "light.hall", "device_id": "dlamp", "config_entry_id": "E1"}}
		paths, sent := rawRelay(t, map[string]func(map[string]any) any{
			"config/device_registry/list": func(map[string]any) any {
				return []any{map[string]any{"id": "dlamp", "name": "Hall lamp", "config_entry_id": "E1"}}
			},
			"config/entity_registry/list": func(map[string]any) any { mu.Lock(); defer mu.Unlock(); return entities },
		})
		exit, env := deleteDevicePlan(t, paths, "dlamp")
		if exit != verbExitConfirmation || !strings.Contains(env.Say, "and its 1 entity") {
			t.Fatalf("plan: exit %d %q", exit, env.Say)
		}
		mu.Lock()
		entities = append(entities, added)
		mu.Unlock()
		if exit, drift := applyRaw(t, paths, env); exit != verbExitDrift || len(sent()) != 0 {
			t.Fatalf("%s attached since the preview: exit %d %+v sent %v", added["entity_id"], exit, drift.Error, sent())
		}
	}
}
