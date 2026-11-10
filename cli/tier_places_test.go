package main

import (
	"slices"
	"testing"
)

// MAR-142: a trigger that targets an area, floor, or label is indexed under
// that area, floor, or label, never under the entity; the consumer scan reads
// where the entity sits, so such an automation counts like one naming it.
func TestConsumerScanReadsWhereTheEntitySits(t *testing.T) {
	home := func(f *fakeHA, trigger map[string]any, entity map[string]any, device, area map[string]any) {
		attrs := map[string]any{"_di": "dev_k"}
		for k, v := range entity {
			attrs[k] = v
		}
		f.ent("light.kitchen", "hue", "off", attrs)
		f.devices = []map[string]any{{"id": "dev_k"}}
		for k, v := range device {
			f.devices[0][k] = v
		}
		f.areas = []map[string]any{{"area_id": "kuche", "floor_id": "eg"}}
		for k, v := range area {
			f.areas[0][k] = v
		}
		f.automation("door", "d1", map[string]any{
			"triggers": []any{trigger},
			"actions":  []any{step("lock.unlock", "lock.front")},
		})
	}
	turnedOn := func(target map[string]any) map[string]any {
		return map[string]any{"trigger": "light.turned_on", "target": target}
	}
	area := turnedOn(map[string]any{"area_id": "kuche"})
	label := turnedOn(map[string]any{"label_id": "night"})
	args := []string{"light.turn_on", "light.kitchen"}
	typed := func(name string, setup func(*fakeHA)) tierCase {
		return tierCase{name: name, setup: setup, args: args, exit: 3, tier: "typed", reasons: []string{reasonAccessLock},
			check: func(t *testing.T, env verbTestEnvelope) {
				if !slices.Contains(consumerList(env), "automation.door") {
					t.Fatalf("consumers %v lack automation.door", consumerList(env))
				}
			}}
	}
	runTierCases(t, []tierCase{
		typed("the entity's own area", func(f *fakeHA) {
			home(f, area, map[string]any{"_ai": "kuche"}, nil, nil)
			f.related["area-automation:kuche"] = []string{"automation.door"}
		}),
		typed("its device's area", func(f *fakeHA) {
			home(f, area, nil, map[string]any{"area_id": "kuche"}, nil)
			f.related["area-automation:kuche"] = []string{"automation.door"}
		}),
		typed("the area's floor", func(f *fakeHA) {
			home(f, turnedOn(map[string]any{"floor_id": "eg"}), map[string]any{"_ai": "kuche"}, nil, nil)
			f.related["floor:eg"] = []string{"automation.door"}
		}),
		typed("a label on the entity", func(f *fakeHA) {
			home(f, label, map[string]any{"_lb": []any{"night"}}, nil, nil)
			f.related["label:night"] = []string{"automation.door"}
		}),
		typed("a label on its device", func(f *fakeHA) {
			home(f, label, nil, map[string]any{"labels": []any{"night"}}, nil)
			f.related["label:night"] = []string{"automation.door"}
		}),
		typed("its device's parent device", func(f *fakeHA) {
			home(f, turnedOn(map[string]any{"device_id": "dev_hub"}), nil, map[string]any{"parent_device_id": "dev_hub"}, nil)
			f.devices = append(f.devices, map[string]any{"id": "dev_hub"})
			f.related["device:dev_hub"] = []string{"automation.door"}
		}),
		typed("a label on its device's parent device", func(f *fakeHA) {
			home(f, label, nil, map[string]any{"parent_device_id": "dev_hub"}, nil)
			f.devices = append(f.devices, map[string]any{"id": "dev_hub", "labels": []any{"night"}})
			f.related["label:night"] = []string{"automation.door"}
		}),
		typed("its parent device's area, for a child device without one", func(f *fakeHA) {
			home(f, area, nil, map[string]any{"parent_device_id": "dev_hub"}, nil)
			f.devices = append(f.devices, map[string]any{"id": "dev_hub", "area_id": "kuche"})
			f.related["area-automation:kuche"] = []string{"automation.door"}
		}),
		typed("a label on its area", func(f *fakeHA) {
			home(f, label, map[string]any{"_ai": "kuche"}, nil, map[string]any{"labels": []any{"night"}})
			f.related["label:night"] = []string{"automation.door"}
		}),
		typed("an exclude block still counts: Home Assistant subtracts it only at run time", func(f *fakeHA) {
			home(f, turnedOn(map[string]any{"floor_id": "eg", "exclude": map[string]any{"entity_id": "light.kitchen"}}), map[string]any{"_ai": "kuche"}, nil, nil)
			f.related["floor:eg"] = []string{"automation.door"}
		}),
		{name: "an unreadable area registry is typed", setup: func(f *fakeHA) {
			home(f, area, map[string]any{"_ai": "kuche"}, nil, nil)
			f.fail["config/area_registry/list"] = true
		}, args: args, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "an unreadable device registry is typed", setup: func(f *fakeHA) {
			home(f, area, nil, map[string]any{"area_id": "kuche"}, nil)
			f.fail["config/device_registry/list"] = true
		}, args: args, exit: 3, tier: "typed", reasons: []string{reasonScanFailed}},
		{name: "an automation that only acts on the area is no consumer", setup: func(f *fakeHA) {
			home(f, map[string]any{"trigger": "state", "entity_id": "binary_sensor.hall"}, map[string]any{"_ai": "kuche"}, nil, nil)
			f.related["area-automation:kuche"] = []string{"automation.door"}
		}, args: args, exit: 0, tier: "ordinary"},
		{name: "one that acts on the area but reacts to another area is no consumer", setup: func(f *fakeHA) {
			home(f, turnedOn(map[string]any{"area_id": "flur"}), map[string]any{"_ai": "kuche"}, nil, nil)
			f.related["area-automation:kuche"] = []string{"automation.door"}
		}, args: args, exit: 0, tier: "ordinary"},
	})
}

// Astra's widening paths (MAR-142): an automation found where the entity
// sits keeps a calm edit from running without its preview, and its calls
// enter the call set, so a call_service listener on them counts again.
func TestWhereTheEntitySitsReachesCalmEditsAndCallListeners(t *testing.T) {
	areaDoor := func(f *fakeHA, object string, action map[string]any) {
		f.related["area-automation:hall"] = append(f.related["area-automation:hall"], "automation."+object)
		f.automation(object, object, map[string]any{
			"triggers": []any{map[string]any{"trigger": "light.turned_on", "target": map[string]any{"area_id": "hall"}}},
			"actions":  []any{action},
		})
	}
	f := newFakeHA()
	before := hallDraft("light.turn_off")
	before["actions"] = []any{step("light.turn_off", "light.kitchen")}
	f.storeConfig(hallPath, before)
	f.ent("light.hall", "hue", "off", map[string]any{"_ai": "hall"})
	f.areas = []map[string]any{{"area_id": "hall"}}
	areaDoor(f, "hall_door", step("lock.unlock", "lock.front"))
	h := newWriteHarness(t, f)
	added := hallDraft("light.turn_off")
	added["actions"] = []any{step("light.turn_off", "light.kitchen"), step("light.turn_on", "light.hall")}
	if _, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "a1", "--file", draftFile(t, added)); env.Tier != "typed" || !slices.Contains(reasonList(env), reasonAccessLock) {
		t.Errorf("a calm edit that adds a lamp beside an area-triggered unlock: tier %q (reasons %s), want typed", env.Tier, reasonCodes(env))
	}

	h, f = callHome(t)
	f.ent("light.hall", "hue", "off", map[string]any{"_ai": "hall"})
	f.areas = []map[string]any{{"area_id": "hall"}}
	areaDoor(f, "hall_lock", step("lock.lock", "lock.front"))
	listen(f, "lockwatch", map[string]any{"domain": "lock"}, step("lock.unlock", "lock.front"))
	if _, env := h.run(runPlanCommand, "call", "light.turn_on", "light.hall"); env.Tier != "typed" || !slices.Contains(consumerList(env), "automation.lockwatch") {
		t.Errorf("a lock call from an area-triggered automation: tier %q (reasons %s), consumers %v; want typed by the lock listener", env.Tier, reasonCodes(env), consumerList(env))
	}
}
