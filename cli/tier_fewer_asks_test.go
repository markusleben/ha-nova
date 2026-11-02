package main

import (
	"fmt"
	"strings"
	"testing"
)

// Fewer asks for everyday things (docs/work/2026-11-02-fewer-asks-spec.md):
// media controls, adding to or updating a to-do list, and a room's lights run
// at once; everything around them keeps its tier.

// everydayHome: a playing TV at volume 0.4, a shopping list, and every media
// and to-do service these cases call.
func everydayHome(more ...func(*fakeHA)) func(*fakeHA) {
	return func(f *fakeHA) {
		f.ent("media_player.tv", "cast", "playing", map[string]any{"friendly_name": "TV", "volume_level": 0.4})
		f.ent("todo.shop", "local_todo", "2", map[string]any{"friendly_name": "Shopping list"})
		f.services = f.defaultServices()
		extra := map[string][]string{
			"media_player": {"media_play", "media_play_pause", "media_stop", "media_next_track", "media_previous_track", "select_source",
				"volume_mute", "volume_up", "volume_down", "volume_set", "play_media", "join", "unjoin"},
			"todo": {"update_item", "remove_item", "remove_completed_items"},
		}
		for _, d := range f.services {
			for _, name := range extra[d["domain"].(string)] {
				d["services"].(map[string]any)[name] = map[string]any{"target": map[string]any{}}
			}
		}
		for _, m := range more {
			m(f)
		}
	}
}

func volumeAt(level any) func(*fakeHA) {
	return func(f *fakeHA) {
		attrs := f.states["media_player.tv"]["attributes"].(map[string]any)
		if level == nil {
			delete(attrs, "volume_level")
		} else {
			attrs["volume_level"] = level
		}
	}
}

func onMedia(service string, sets ...string) []string {
	args := []string{"media_player." + service, "media_player.tv", "--skill", "ha-nova:media"}
	for _, set := range sets {
		args = append(args, "--set", set)
	}
	return args
}

func onShop(service string, sets ...string) []string {
	args := []string{"todo." + service, "todo.shop", "--skill", "ha-nova:todo"}
	for _, set := range sets {
		args = append(args, "--set", set)
	}
	return args
}

// unlocksOn: an automation that unlocks the front door when entity changes.
func unlocksOn(entity string) func(*fakeHA) {
	return func(f *fakeHA) {
		f.related["entity:"+entity] = []string{"automation.door"}
		f.automation("door", "d1", map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": entity}},
			"actions":  []any{step("lock.unlock", "lock.front")},
		})
	}
}

// lightRuns: an automation that runs service on the TV when the kitchen light changes.
func lightRuns(service string) func(*fakeHA) {
	return func(f *fakeHA) {
		f.related["entity:light.kitchen"] = []string{"automation.tv"}
		f.automation("tv", "t1", map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.kitchen"}},
			"actions":  []any{step(service, "media_player.tv")},
		})
	}
}

func TestEverydayMediaAndTodoRunAtOnce(t *testing.T) {
	ordinary := func(name string, args []string, more ...func(*fakeHA)) tierCase {
		return tierCase{name: name, setup: everydayHome(more...), args: args, exit: 0, tier: "ordinary", absent: []string{reasonOwnedMember}}
	}
	kept := func(name string, args []string, more ...func(*fakeHA)) tierCase {
		return tierCase{name: name, setup: everydayHome(more...), args: args, exit: 3, tier: "preview", reasons: []string{reasonOwnedMember}}
	}
	onDoorDevice := func(f *fakeHA) {
		f.ent("media_player.tv", "cast", "playing", map[string]any{"_di": "dev_door", "volume_level": 0.4})
		f.ent("lock.door", "cast", "locked", map[string]any{"_di": "dev_door"})
	}
	runTierCases(t, []tierCase{
		ordinary("play", onMedia("media_play")),
		ordinary("pause", onMedia("media_pause")),
		ordinary("play or pause", onMedia("media_play_pause")),
		ordinary("stop", onMedia("media_stop")),
		ordinary("next track", onMedia("media_next_track")),
		ordinary("previous track", onMedia("media_previous_track")),
		ordinary("source", onMedia("select_source", "source=TV")),
		ordinary("mute", onMedia("volume_mute", "is_volume_muted=true")),
		ordinary("unmute at 0.4", onMedia("volume_mute", "is_volume_muted=false")),
		ordinary("volume down", onMedia("volume_down")),
		ordinary("volume up from 0.4", onMedia("volume_up")),
		ordinary("volume up from 0.7 ends at 0.8", onMedia("volume_up"), volumeAt(0.7)),
		ordinary("volume 0.4 to 0.6", onMedia("volume_set", "volume_level=0.6")),
		ordinary("volume 0.5 to 0.8, the largest moderate step", onMedia("volume_set", "volume_level=0.8"), volumeAt(0.5)),
		ordinary("volume 0.9 down to 0.1", onMedia("volume_set", "volume_level=0.1"), volumeAt(0.9)),
		ordinary("add to the shopping list", onShop("add_item", "item=milk")),
		ordinary("check an item off", onShop("update_item", "item=uid1", "status=completed")),
		ordinary("rename an item", onShop("update_item", "item=uid1", "rename=oat milk")),

		kept("play media", onMedia("play_media", "media_content_id=media-source://a", "media_content_type=music")),
		kept("join", onMedia("join", `group_members=["media_player.kitchen"]`)),
		kept("unjoin", onMedia("unjoin")),
		kept("volume above 0.8", onMedia("volume_set", "volume_level=0.85"), volumeAt(0.7)),
		kept("a jump from 0.4 to 0.8", onMedia("volume_set", "volume_level=0.8")),
		kept("volume up from 0.75", onMedia("volume_up"), volumeAt(0.75)),
		kept("volume with no level known", onMedia("volume_set", "volume_level=0.2"), volumeAt(nil)),
		kept("volume up with no level known", onMedia("volume_up"), volumeAt(nil)),
		kept("unmute at 0.9", onMedia("volume_mute", "is_volume_muted=false"), volumeAt(0.9)),
		kept("a templated volume", onMedia("volume_set", "volume_level={{ 0.3 }}")),
		kept("a pause on a door lock's device", onMedia("media_pause"), onDoorDevice),
		kept("0.6 to 0.804 is above the cap", onMedia("volume_set", "volume_level=0.804"), volumeAt(0.6)),
		kept("0.396 to 0.704 rises more than 0.3", onMedia("volume_set", "volume_level=0.704"), volumeAt(0.396)),
		kept("a universal player's commands may be anything", onMedia("media_pause"), func(f *fakeHA) {
			f.ent("media_player.tv", "universal", "playing", map[string]any{"volume_level": 0.4})
		}),
		kept("a grouped speaker pauses its group too", onMedia("media_pause"), func(f *fakeHA) {
			f.states["media_player.tv"]["attributes"].(map[string]any)["group_members"] = []any{"media_player.tv", "media_player.kitchen"}
		}),
		{name: "a stored step that turns the TV up keeps the preview", setup: everydayHome(lightRuns("media_player.volume_up")),
			args: []string{"light.turn_off", "light.kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonOwnedMember}},
		{name: "turning the TV up again from its own automation keeps the preview", setup: everydayHome(volumeAt(0.7), func(f *fakeHA) {
			f.related["entity:media_player.tv"] = []string{"automation.louder"}
			f.automation("louder", "l1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "state", "entity_id": "media_player.tv", "attribute": "volume_level"}},
				"actions":  []any{step("media_player.volume_up", "media_player.tv")},
			})
		}), args: onMedia("volume_up"), exit: 3, tier: "preview", reasons: []string{reasonOwnedMember}},
		{name: "a stored step that pauses the TV runs at once", setup: everydayHome(lightRuns("media_player.media_pause")),
			args: []string{"light.turn_off", "light.kitchen"}, exit: 0, tier: "ordinary", absent: []string{reasonOwnedMember}},
		{name: "a shopping list whose update event starts an unlock stays typed", setup: everydayHome(func(f *fakeHA) {
			f.ent("todo.shopping_list", "shopping_list", "1", nil)
			f.events = []map[string]any{{"event": "shopping_list_updated", "listener_count": 1}}
			f.automation("groceries", "g1", map[string]any{
				"triggers": []any{map[string]any{"trigger": "event", "event_type": "shopping_list_updated"}},
				"actions":  []any{step("lock.unlock", "lock.front")},
			})
		}), args: []string{"todo.add_item", "todo.shopping_list", "--skill", "ha-nova:todo", "--set", "item=milk"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		kept("remove an item", onShop("remove_item", "item=uid1")),
		kept("clear completed items", onShop("remove_completed_items")),

		{name: "a pause that starts an unlock stays typed", setup: everydayHome(unlocksOn("media_player.tv")),
			args: onMedia("media_pause"), exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
		{name: "an item that starts an unlock stays typed", setup: everydayHome(unlocksOn("todo.shop")),
			args: onShop("add_item", "item=milk"), exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
	})
}

func TestRoomLightsRunAtOnce(t *testing.T) {
	area := func(ids ...string) func(*fakeHA) {
		return func(f *fakeHA) { f.related["area:kitchen"] = ids }
	}
	group := func(id string, members ...any) func(*fakeHA) {
		return func(f *fakeHA) { f.ent(id, "group", "on", map[string]any{"entity_id": members}) }
	}
	both := func(f *fakeHA) {
		for _, setup := range []func(*fakeHA){area("light.kitchen", "light.hall"), group("light.all", "light.kitchen", "light.hall")} {
			setup(f)
		}
	}
	runTierCases(t, []tierCase{
		{name: "two lights in a room", setup: area("light.kitchen", "light.hall"),
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 0, tier: "ordinary", absent: []string{reasonExpanded},
			check: func(t *testing.T, env verbTestEnvelope) {
				if len(env.Resolved) != 2 || env.Resolved[0].By != "area" || strings.Join(env.Action.EntityIDs, ",") != "light.hall,light.kitchen" {
					t.Fatalf("resolved %+v action %+v", env.Resolved, env.Action)
				}
			}},
		{name: "on and toggle too", setup: area("light.kitchen", "light.hall"),
			args: []string{"light.toggle", "--area", "kitchen"}, exit: 0, tier: "ordinary"},
		{name: "a light group by name", setup: group("light.all", "light.kitchen", "light.hall"),
			args: []string{"light.turn_on", "light.all"}, exit: 0, tier: "ordinary", absent: []string{reasonExpanded}},
		{name: "a room holding a light group and its lights", setup: both,
			args: []string{"light.turn_off", "--area", "kitchen"}, exit: 0, tier: "ordinary"},

		{name: "eleven lights stay preview", setup: func(f *fakeHA) {
			var ids []string
			for i := range 11 {
				ids = append(ids, fmt.Sprintf("light.spot_%d", i))
				f.ent(ids[i], "hue", "on", nil)
			}
			area(ids...)(f)
		}, args: []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a light on a door lock's device keeps the room's preview", setup: func(f *fakeHA) {
			f.ent("light.door_ring", "nuki", "on", map[string]any{"_di": "dev_nuki"})
			f.ent("lock.nuki", "nuki", "locked", map[string]any{"_di": "dev_nuki"})
			area("light.kitchen", "light.door_ring")(f)
		}, args: []string{"light.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a generic call on a room with a shutter stays preview", setup: func(f *fakeHA) {
			f.ent("cover.blind", "shelly", "open", map[string]any{"device_class": "shutter"})
			area("light.kitchen", "light.hall", "cover.blind")(f)
		}, args: []string{"homeassistant.turn_off", "--area", "kitchen"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a group member without state keeps the preview", setup: group("light.all", "light.kitchen", "light.ghost"),
			args: []string{"light.turn_off", "light.all"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "an unregistered group member is typed", setup: func(f *fakeHA) {
			f.ent("light.yaml", "", "on", nil)
			group("light.all", "light.kitchen", "light.yaml")(f)
		}, args: []string{"light.turn_off", "light.all"}, exit: 3, tier: "typed", reasons: []string{reasonUnregisteredActuator}},
		{name: "a template light listing lights stays preview", setup: func(f *fakeHA) {
			f.ent("light.tpl_room", "template", "on", map[string]any{"entity_id": []any{"light.kitchen", "light.hall"}})
		}, args: []string{"light.turn_off", "light.tpl_room"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a group holding a switch stays preview", setup: func(f *fakeHA) {
			f.ent("switch.kettle", "shelly", "on", nil)
			group("light.all", "light.kitchen", "switch.kettle")(f)
		}, args: []string{"light.turn_off", "light.all"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "two named groups stay preview", setup: func(f *fakeHA) {
			group("light.up", "light.kitchen")(f)
			group("light.down", "light.hall")(f)
		}, args: []string{"light.turn_off", "light.up", "light.down"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a device with two lights stays preview", setup: func(f *fakeHA) {
			f.ent("light.strip_a", "hue", "on", map[string]any{"_di": "dev_strip"})
			f.ent("light.strip_b", "hue", "on", map[string]any{"_di": "dev_strip"})
		}, args: []string{"light.turn_off", "--device", "dev_strip"}, exit: 3, tier: "preview", reasons: []string{reasonExpanded}},
		{name: "a room's light that starts an unlock stays typed", setup: func(f *fakeHA) {
			area("light.kitchen", "light.hall")(f)
			unlocksOn("light.hall")(f)
		}, args: []string{"light.turn_on", "--area", "kitchen"}, exit: 3, tier: "typed", reasons: []string{reasonAccessLock}},
	})
}

// The result of a room's lights names each light it read back.
func TestRoomLightsResultNamesEachLight(t *testing.T) {
	f := newFakeHA()
	f.ent("light.hall", "hue", "on", map[string]any{"friendly_name": "Hall"})
	f.related["area:kitchen"] = []string{"light.kitchen", "light.hall"}
	h := newWriteHarness(t, f)
	exit, env := h.run(runCallCommand, "light.turn_off", "--area", "kitchen")
	if exit != 0 || env.Tier != "ordinary" || len(h.posts()) != 1 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
	if !strings.Contains(env.Say, "“Hall”") || !strings.Contains(env.Say, "“Kitchen”") {
		t.Fatalf("say %q", env.Say)
	}
}
