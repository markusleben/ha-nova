package main

import (
	"encoding/json"
	"testing"
)

// Home Assistant's validator renames `service` to `action` in the posted
// config itself (at any depth) and the singular top-level keys to plural, then
// saves that (checked with HA 2026.10.1's automation and script validators).
// Each `stored` below is HA's saved form of `sent`. Equivalent spellings must
// compare equal; a real change in meaning must still differ.
func TestConfigChangesReadsHomeAssistantsSavedForm(t *testing.T) {
	for _, tc := range []struct {
		name, sent, stored string
		same               bool
	}{
		{"canonical", `{"triggers":[{"trigger":"time","at":"23:00"}],"actions":[{"action":"lock.lock","target":{"entity_id":"lock.entrance_door"}}]}`,
			`{"triggers":[{"trigger":"time","at":"23:00"}],"actions":[{"action":"lock.lock","target":{"entity_id":"lock.entrance_door"}}]}`, true},
		{"service key", `{"actions":[{"service":"lock.lock","target":{"entity_id":"lock.entrance_door"}}]}`,
			`{"actions":[{"target":{"entity_id":"lock.entrance_door"},"action":"lock.lock"}]}`, true},
		{"singular top keys", `{"trigger":[{"platform":"time","at":"23:00"}],"condition":[],"action":[{"service":"lock.lock","entity_id":"lock.entrance_door"}]}`,
			`{"triggers":[{"platform":"time","at":"23:00"}],"conditions":[],"actions":[{"entity_id":"lock.entrance_door","action":"lock.lock"}]}`, true},
		{"singular objects", `{"trigger":{"platform":"time","at":"23:00"},"action":{"service":"lock.lock","data":{"entity_id":"lock.entrance_door"}}}`,
			`{"triggers":{"platform":"time","at":"23:00"},"actions":{"data":{"entity_id":"lock.entrance_door"},"action":"lock.lock"}}`, true},
		{"one object vs one-item list", `{"actions":{"action":"lock.lock"}}`, `{"actions":[{"action":"lock.lock"}]}`, true},
		{"nested blocks", `{"actions":[{"if":[{"condition":"state","entity_id":"lock.entrance_door","state":"unlocked"}],"then":[{"service":"lock.lock"}],"else":[{"choose":[{"conditions":[],"sequence":[{"service":"light.turn_on"}]}],"default":[{"service":"light.turn_off"}]}]},{"repeat":{"count":2,"sequence":[{"service":"light.toggle"}]}},{"parallel":[{"service":"light.toggle"},{"sequence":[{"service":"light.toggle"}]}]}]}`,
			`{"actions":[{"if":[{"condition":"state","entity_id":"lock.entrance_door","state":"unlocked"}],"then":[{"action":"lock.lock"}],"else":[{"choose":[{"conditions":[],"sequence":[{"action":"light.turn_on"}]}],"default":[{"action":"light.turn_off"}]}]},{"repeat":{"count":2,"sequence":[{"action":"light.toggle"}]}},{"parallel":[{"action":"light.toggle"},{"sequence":[{"action":"light.toggle"}]}]}]}`, true},
		{"script sequence", `{"alias":"s","sequence":[{"service":"lock.lock"},{"choose":[{"conditions":[],"sequence":[{"service":"light.turn_on"}]}]}]}`,
			`{"alias":"s","sequence":[{"action":"lock.lock"},{"choose":[{"conditions":[],"sequence":[{"action":"light.turn_on"}]}]}]}`, true},
		{"script single step", `{"sequence":{"service":"lock.lock","entity_id":"lock.entrance_door"}}`,
			`{"sequence":{"entity_id":"lock.entrance_door","action":"lock.lock"}}`, true},

		{"other target", `{"actions":[{"service":"lock.lock","target":{"entity_id":"lock.entrance_door"}}]}`,
			`{"actions":[{"action":"lock.lock","target":{"entity_id":"lock.front_door"}}]}`, false},
		{"other service", `{"actions":[{"service":"lock.lock"}]}`, `{"actions":[{"action":"lock.unlock"}]}`, false},
		{"dropped step", `{"actions":[{"service":"lock.lock"},{"service":"light.turn_off"}]}`, `{"actions":[{"action":"lock.lock"}]}`, false},
		{"dropped actions", `{"actions":[{"service":"lock.lock"}]}`, `{"actions":[]}`, false},
		{"other time", `{"trigger":[{"platform":"time","at":"23:00"}]}`, `{"triggers":[{"platform":"time","at":"22:00"}]}`, false},
		{"nested other service", `{"actions":[{"if":[],"then":[{"service":"lock.lock"}]}]}`, `{"actions":[{"if":[],"then":[{"action":"lock.unlock"}]}]}`, false},
		{"payload key is not a step", `{"actions":[{"action":"notify.x","data":{"service":"a"}}]}`, `{"actions":[{"action":"notify.x","data":{"action":"a"}}]}`, false},
		{"condition key is not a step", `{"conditions":[{"condition":"template","service":"a"}]}`, `{"conditions":[{"condition":"template","action":"a"}]}`, false},
		{"variables reordered in a wrapped step", `{"sequence":{"variables":{"x":1,"y":"{{ x }}"}}}`, `{"sequence":[{"variables":{"y":"{{ x }}","x":1}}]}`, false},
		{"variables reordered under an alias", `{"action":{"variables":{"x":1,"y":"{{ x }}"}}}`, `{"actions":[{"variables":{"y":"{{ x }}","x":1}}]}`, false},
		{"variables kept under an alias", `{"action":{"variables":{"x":1,"y":"{{ x }}"}}}`, `{"actions":[{"variables":{"x":1,"y":"{{ x }}"}}]}`, true},
		{"blueprint input steps", `{"use_blueprint":{"path":"me/bp.yaml","input":{"on_motion":[{"service":"light.turn_on","target":{"entity_id":"light.k"}}],"delay":5}}}`,
			`{"use_blueprint":{"path":"me/bp.yaml","input":{"on_motion":[{"action":"light.turn_on","target":{"entity_id":"light.k"}}],"delay":5}}}`, true},
		{"blueprint input other target", `{"use_blueprint":{"path":"me/bp.yaml","input":{"on_motion":[{"service":"light.turn_on","target":{"entity_id":"light.k"}}]}}}`,
			`{"use_blueprint":{"path":"me/bp.yaml","input":{"on_motion":[{"action":"light.turn_on","target":{"entity_id":"light.x"}}]}}}`, false},
		{"blueprint data input keeps its keys", `{"use_blueprint":{"path":"me/bp.yaml","input":{"settings":{"service":"x"}}}}`, `{"use_blueprint":{"path":"me/bp.yaml","input":{"settings":{"action":"x"}}}}`, false},
		{"blueprint mixed list input keeps its keys", `{"use_blueprint":{"path":"me/bp.yaml","input":{"rows":[{"service":"x"},{"name":"y"}]}}}`, `{"use_blueprint":{"path":"me/bp.yaml","input":{"rows":[{"action":"x"},{"name":"y"}]}}}`, false},
		{"blueprint input name is not a step", `{"use_blueprint":{"path":"me/bp.yaml","input":{"service":"a"}}}`, `{"use_blueprint":{"path":"me/bp.yaml","input":{"action":"a"}}}`, false},
		{"top-level key outside steps", `{"fields":{"service":{"selector":{"text":{}}}}}`, `{"fields":{"action":{"selector":{"text":{}}}}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changes, err := configChanges([]byte(tc.sent), []byte(tc.stored))
			if err != nil || (len(changes) == 0) != tc.same {
				t.Fatalf("same=%v, got changes %q err %v", tc.same, changes, err)
			}
		})
	}
}

// The live finding: a draft with `service` saved through plan → apply must
// verify, while a real difference keeps a plain say that forbids explaining it away.
func TestApplyVerifiesHomeAssistantsRenamedServiceKey(t *testing.T) {
	draft := map[string]any{"alias": "Eingangstür um 23:00 verriegeln",
		"trigger": []any{map[string]any{"platform": "time", "at": "23:00"}},
		"action":  []any{map[string]any{"service": "lock.lock", "target": map[string]any{"entity_id": "lock.entrance_door"}}}}
	for name, stored := range map[string]string{
		"saved as planned": `{"alias":"Eingangstür um 23:00 verriegeln","triggers":[{"platform":"time","at":"23:00"}],"actions":[{"target":{"entity_id":"lock.entrance_door"},"action":"lock.lock"}]}`,
		"other target":     `{"alias":"Eingangstür um 23:00 verriegeln","triggers":[{"platform":"time","at":"23:00"}],"actions":[{"target":{"entity_id":"lock.front_door"},"action":"lock.lock"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHA()
			f.configWrite = func(f *fakeHA, path string, _ map[string]any) any {
				var saved map[string]any
				_ = json.Unmarshal([]byte(stored), &saved)
				f.storeConfig(path, saved)
				return coreReply(200, map[string]any{"result": "ok"})
			}
			h := newWriteHarness(t, f)
			_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--file", draftFile(t, draft))
			exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
			if name == "saved as planned" {
				if exit != 0 || applied.Status != "verified" || applied.Say != "" {
					t.Fatalf("exit %d env %+v", exit, applied)
				}
				return
			}
			if exit != 6 || applied.Status != "unverified" || applied.Say == "" || applied.NextStep == "" {
				t.Fatalf("exit %d env %+v", exit, applied)
			}
		})
	}
}
