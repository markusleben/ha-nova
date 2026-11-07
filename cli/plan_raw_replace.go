package main

import (
	"encoding/json"
	"slices"
	"strings"
)

// The updates besides input_select and schedule that drop what they leave
// out (rawReplaced; Home Assistant core, verified 2026-11-07): a person
// update without device_trackers empties them (UPDATE_FIELDS default them
// to []); a registry update replaces each aliases or labels list it sends,
// and an entity's options_domain with options replaces that domain's
// options; a voice assistant update rebuilds the pipeline from the update
// alone, so a left-out prefer_local_intents or acknowledge_media_id goes
// back to its default (every other field is required). Zone, tag, and
// dashboard updates merge, and the other helpers' fields are scalars their
// update-revert checkpoint keeps.

var rawPersonUpdate = rawReplace{kind: "person", owner: "person", why: "an update without device_trackers empties them", noun: "device trackers",
	keep: "every device tracker it follows in device_trackers with the change", removes: "device trackers from a person", category: "persons", part: true,
	idKey: "person_id", source: `ws {"type":"person/list"} (its storage entry)`, read: listItem("person/list", "storage", "id"),
	dropped: func(live, body map[string]any) []helperDrop {
		return listDrops(live["device_trackers"], body["device_trackers"], "", strings.ToLower)
	}}

var rawEntityUpdate = rawReplace{kind: "entity", owner: "entity", why: "an update replaces each list it sends, and the options of the options_domain it sends", noun: "aliases, labels, or options",
	keep: "every alias and label it holds in each list you send, and every key of the options you send", removes: "aliases, labels, or options from an entity", category: "metadata", part: true,
	idKey: "entity_id", source: `ws {"type":"config/entity_registry/get","entity_id":"<its entity_id>"}`, sends: []string{"aliases", "labels", "options_domain"},
	read: func(s *relaySession, id string) (map[string]any, error) {
		data, err := rawRead(s, "ws", map[string]any{"type": "config/entity_registry/get", "entity_id": id})
		var entry map[string]any
		if err == nil {
			err = json.Unmarshal(data, &entry)
		}
		return entry, err
	},
	dropped: func(live, body map[string]any) []helperDrop {
		dropped := registryDrops(live, body, "aliases", "labels")
		domain, _ := body["options_domain"].(string)
		options, _ := live["options"].(map[string]any)
		was, _ := options[domain].(map[string]any)
		now, _ := body["options"].(map[string]any)
		var keys []string
		for key := range was {
			if _, kept := now[key]; !kept {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			dropped = append(dropped, helperDrop{option: key, list: domain + " options"})
		}
		return dropped
	}}

var rawDeviceUpdate = rawReplace{kind: "device", owner: "device", why: "an update replaces the labels it sends", noun: "labels",
	keep: "every label it holds in labels", removes: "labels from a device", category: "metadata", part: true,
	idKey: "device_id", source: `ws {"type":"config/device_registry/list"}`, sends: []string{"labels"}, read: listItem("config/device_registry/list", "", "id"),
	dropped: func(live, body map[string]any) []helperDrop { return registryDrops(live, body, "labels") }}

var rawAreaUpdate = rawReplace{kind: "area", owner: "area", why: "an update replaces each list it sends", noun: "aliases or labels",
	keep: "every alias and label it holds in each list you send", removes: "aliases or labels from an area", category: "metadata", part: true,
	idKey: "area_id", source: `ws {"type":"config/area_registry/list"}`, sends: []string{"aliases", "labels"}, read: listItem("config/area_registry/list", "", "area_id"),
	dropped: func(live, body map[string]any) []helperDrop { return registryDrops(live, body, "aliases", "labels") }}

var rawFloorUpdate = rawReplace{kind: "floor", owner: "floor", why: "an update replaces the aliases it sends", noun: "aliases",
	keep: "every alias it holds in aliases", removes: "aliases from a floor", category: "metadata", part: true,
	idKey: "floor_id", source: `ws {"type":"config/floor_registry/list"}`, sends: []string{"aliases"}, read: listItem("config/floor_registry/list", "", "floor_id"),
	dropped: func(live, body map[string]any) []helperDrop { return registryDrops(live, body, "aliases") }}

var rawPipelineUpdate = rawReplace{kind: "voice assistant", owner: "voice assistant", why: "an update resets each setting it leaves out", noun: "settings",
	keep: "every setting it holds with the change, prefer_local_intents and acknowledge_media_id included", removes: "settings from a voice assistant", category: "assist", part: true,
	idKey: "pipeline_id", source: `ws {"type":"assist_pipeline/pipeline/list"} (its pipelines entry)`, read: listItem("assist_pipeline/pipeline/list", "pipelines", "id"),
	dropped: func(live, body map[string]any) []helperDrop {
		var dropped []helperDrop
		for _, key := range []string{"prefer_local_intents", "acknowledge_media_id"} {
			if _, sent := body[key]; !sent && live[key] != nil && live[key] != false {
				dropped = append(dropped, helperDrop{option: key})
			}
		}
		return dropped
	}}

// registryDrops: the entries of each list in keys that body sends (null
// sends none) and that leave out what live holds. Home Assistant strips
// aliases; a label id it doesn't know is dropped, so labels compare exactly.
func registryDrops(live, body map[string]any, keys ...string) []helperDrop {
	var dropped []helperDrop
	for _, key := range keys {
		if sent := body[key]; sent != nil {
			list, same := "", func(s string) string { return s }
			if len(keys) > 1 {
				list = key
			}
			if key == "aliases" {
				same = strings.TrimSpace
			}
			dropped = append(dropped, listDrops(live[key], sent, list, same)...)
		}
	}
	return dropped
}

// listDrops: the entries of live that sent leaves out, text compared after
// same; an entry that isn't text (an entity's computed-name alias is null)
// compares as itself.
func listDrops(live, sent any, list string, same func(string) string) []helperDrop {
	entry := func(v any) (key, text string) {
		if s, ok := v.(string); ok {
			return jsonText(same(s)), s
		}
		return "~" + jsonText(v), jsonText(v)
	}
	kept := map[string]bool{}
	for _, v := range ensureList(sent) {
		key, _ := entry(v)
		kept[key] = true
	}
	var dropped []helperDrop
	for _, v := range ensureList(live) {
		if key, text := entry(v); !kept[key] {
			dropped = append(dropped, helperDrop{option: text, list: list})
		}
	}
	return dropped
}
