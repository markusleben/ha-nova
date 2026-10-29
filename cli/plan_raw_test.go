package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// rawKey names a Relay request for the raw-plan fakes: the WS type,
// "METHOD path", or "files|backups action".
func rawKey(endpoint string, body map[string]any) string {
	switch endpoint {
	case "ws":
		return fmt.Sprint(body["type"])
	case "core":
		return fmt.Sprint(body["method"], " ", body["path"])
	}
	return endpoint + " " + fmt.Sprint(body["action"])
}

// rawRelay is a Relay answering by rawKey from handlers; the registry is
// empty, a state is missing, and any other request succeeds. sent lists the
// keys of every request that is not a read, in order.
func rawRelay(t *testing.T, handlers map[string]func(map[string]any) any) (paths runtimePaths, sent func() []string) {
	t.Helper()
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		key := rawKey(endpoint, body)
		if handle := handlers[key]; handle != nil {
			return handle(body)
		}
		if handle := handlers[strings.SplitN(key, "?", 2)[0]]; handle != nil { // a query matches its path
			return handle(body)
		}
		switch {
		case key == "config/entity_registry/list":
			return []any{}
		case strings.HasPrefix(key, "GET /api/states/"):
			return map[string]any{"status": 404, "body": map[string]any{"message": "Entity not found."}}
		case endpoint == "ws":
			return map[string]any{}
		}
		return map[string]any{"status": 200, "body": map[string]any{}}
	})
	return paths, func() []string {
		var keys []string
		for _, c := range calls() {
			var body map[string]any
			_ = json.Unmarshal([]byte(c.body), &body)
			if c.endpoint == "core" || c.endpoint == "ws" {
				if rawRelayRead(c.endpoint, []byte(c.body)) && !rawPlanOnlyRead(c.endpoint, body) {
					continue
				}
			} else if body["action"] == "list" || body["action"] == "load" {
				continue
			}
			keys = append(keys, rawKey(c.endpoint, body))
		}
		return keys
	}
}

func planRaw(t *testing.T, paths runtimePaths, items string) (int, verbTestEnvelope) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "requests.json")
	if err := os.WriteFile(file, []byte(items), 0o600); err != nil {
		t.Fatal(err)
	}
	return runVerbForTest(t, func() int { return runPlanCommand(paths, []string{"raw", "--file", file}) })
}

func applyRaw(t *testing.T, paths runtimePaths, env verbTestEnvelope, extra ...string) (int, verbTestEnvelope) {
	t.Helper()
	if env.Plan == nil {
		t.Fatalf("no plan: %+v", env)
	}
	args := append([]string{"--plan", env.Plan.ID}, extra...)
	if env.Plan.Code != "" {
		args = append(args, "--confirm", env.Plan.Code)
	}
	return runVerbForTest(t, func() int { return runApplyCommand(paths, args) })
}

func rawOutcomes(t *testing.T, env verbTestEnvelope) []rawItemOutcome {
	t.Helper()
	var result rawApplyResult
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result %s: %v", env.Result, err)
	}
	return result.Items
}

func statuses(items []rawItemOutcome) string {
	var out []string
	for _, item := range items {
		out = append(out, item.Status)
	}
	return strings.Join(out, ",")
}

// A preview plan names every item in plain words, one line each, and apply
// sends them in order and hands back each answer.
func TestPlanRawPreviewNamesItemsAndAppliesInOrder(t *testing.T) {
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config/device_registry/list": func(map[string]any) any {
			return []any{map[string]any{"id": "d1", "name": "Hall lamp", "name_by_user": nil}}
		},
		"config/area_registry/list":     func(map[string]any) any { return []any{map[string]any{"area_id": "office", "name": "Office"}} },
		"input_boolean/create":          func(map[string]any) any { return map[string]any{"id": "guest_mode", "name": "Guest mode"} },
		"config/device_registry/update": func(map[string]any) any { return map[string]any{"id": "d1"} },
	})
	exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/device_registry/update","device_id":"d1","name_by_user":"Desk lamp","area_id":"office"}},
		{"endpoint":"ws","payload":{"type":"input_boolean/create","name":"Guest mode"}}]`)
	if exit != verbExitConfirmation || env.Tier != "preview" || env.Plan.Code != "" {
		t.Fatalf("plan: exit %d %+v", exit, env)
	}
	for _, want := range []string{"1. Change the device “Hall lamp”: area “Office”, name “Desk lamp”.", "2. Create the toggle helper “Guest mode”.", "Nothing has run yet.", sayYesAsk("en")} {
		if !strings.Contains(env.Say, want) {
			t.Errorf("say lacks %q:\n%s", want, env.Say)
		}
	}
	if strings.Contains(env.Say, "d1") || len(sent()) != 0 {
		t.Fatalf("say shows an id or the plan sent something: %q %v", env.Say, sent())
	}
	exit, env = applyRaw(t, paths, env)
	items := rawOutcomes(t, env)
	if exit != 0 || env.Status != "accepted" || statuses(items) != "done,done" || !strings.Contains(string(items[1].Response), "guest_mode") {
		t.Fatalf("apply: exit %d %+v %s", exit, env, env.Result)
	}
	if got := strings.Join(sent(), " | "); got != "config/device_registry/update | input_boolean/create" {
		t.Fatalf("sent %s", got)
	}
}

// Apply stops at the first item that does not go through: a refused one
// is failed, one that may have run is OUTCOME_UNKNOWN; later items are
// never sent.
func TestPlanRawStopsAtFirstFailure(t *testing.T) {
	for _, tc := range []struct {
		failure      fakeRelayFailure
		exit         int
		code, status string
	}{
		{fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "invalid_format: max must be greater than min"}, verbExitUnverified, "RAW_FAILED", "done,rejected,not_sent"},
		{fakeRelayFailure{"UPSTREAM_TIMEOUT", "no answer"}, verbExitUnknown, "OUTCOME_UNKNOWN", "done,unknown,not_sent"},
	} {
		paths, sent := rawRelay(t, map[string]func(map[string]any) any{"counter/create": func(map[string]any) any { return tc.failure }})
		_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/create","name":"A"}},
			{"endpoint":"ws","payload":{"type":"counter/create","name":"B"}},{"endpoint":"ws","payload":{"type":"timer/create","name":"C"}}]`)
		exit, env := applyRaw(t, paths, env)
		if exit != tc.exit || env.Error == nil || env.Error.Code != tc.code || statuses(rawOutcomes(t, env)) != tc.status {
			t.Fatalf("%s: exit %d %+v %s", tc.failure.code, exit, env.Error, env.Result)
		}
		if tc.code == "OUTCOME_UNKNOWN" && (!env.OutcomeUnknown || !strings.Contains(env.Error.Next, "never apply this plan again")) {
			t.Fatalf("unknown: %+v", env)
		}
		if got := strings.Join(sent(), " | "); got != "input_boolean/create | counter/create" {
			t.Fatalf("%s: sent %s", tc.failure.code, got)
		}
		if !strings.HasPrefix(env.Say, "Partly done:") && !strings.HasPrefix(env.Say, "Not sure:") {
			t.Fatalf("say %q", env.Say)
		}
	}
}

// The guard runs again right before each item: an earlier item that
// renamed switch.gate to lock.gate cannot carry the removal past it.
func TestPlanRawRechecksEachItemLive(t *testing.T) {
	var mu sync.Mutex
	registry := []any{map[string]any{"entity_id": "switch.gate", "device_id": "d1"}}
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/list": func(map[string]any) any { mu.Lock(); defer mu.Unlock(); return registry },
		"config/entity_registry/update": func(map[string]any) any {
			mu.Lock()
			defer mu.Unlock()
			registry = []any{map[string]any{"entity_id": "lock.gate", "device_id": "d1"}}
			return map[string]any{}
		},
	})
	exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"switch.gate","new_entity_id":"lock.gate"}},
		{"endpoint":"ws","payload":{"type":"config/entity_registry/remove","entity_id":"lock.gate"}}]`)
	if exit != verbExitConfirmation || env.Tier != "typed" || env.Plan.Code == "" {
		t.Fatalf("plan: exit %d %+v", exit, env)
	}
	exit, env = applyRaw(t, paths, env)
	items := rawOutcomes(t, env)
	if exit != verbExitUnverified || statuses(items) != "done,refused" || !strings.Contains(items[1].Error, "lock.gate") {
		t.Fatalf("apply: exit %d %+v %s", exit, env.Error, env.Result)
	}
	if got := strings.Join(sent(), " | "); got != "config/entity_registry/update" {
		t.Fatalf("sent %s", got)
	}
}

// A leading backups save is the config snapshot: when it fails, nothing
// else is sent, and the same plan and code go on with --no-snapshot.
func TestPlanRawSnapshotFailedThenNoSnapshot(t *testing.T) {
	full := true
	paths, sent := rawRelay(t, map[string]func(map[string]any) any{
		"backups save": func(map[string]any) any {
			if full {
				return fakeRelayFailure{"SNAPSHOT_STORE_FULL", "the snapshot store is full"}
			}
			return map[string]any{"file": "scenes/auto-x.json.gz"}
		},
		"GET /api/config/scene/config/evening": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"id": "evening", "name": "Evening"}}
		},
	})
	_, env := planRaw(t, paths, `[{"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-evening","data":{"id":"evening"}}},
		{"endpoint":"core","payload":{"method":"DELETE","path":"/api/config/scene/config/evening"}}]`)
	if env.Tier != "typed" || !strings.Contains(env.Say, "I'm about to delete the scene “Evening”. HA NOVA saves a copy first (“scenes/auto-evening”).") {
		t.Fatalf("plan %+v", env)
	}
	if exit, early := applyRaw(t, paths, env, "--no-snapshot"); exit != verbExitRefused || early.Error.Code != "NO_SNAPSHOT_NOT_OFFERED" {
		t.Fatalf("no-snapshot before a failure: exit %d %+v", exit, early.Error)
	}
	exit, failed := applyRaw(t, paths, env)
	if exit != verbExitError || failed.Error == nil || failed.Error.Code != "SNAPSHOT_FAILED" || !strings.Contains(failed.Error.Next, "--no-snapshot") {
		t.Fatalf("snapshot failure: exit %d %+v", exit, failed.Error)
	}
	if got := strings.Join(sent(), " | "); got != "backups save" {
		t.Fatalf("sent after the failed snapshot: %s", got)
	}
	exit, done := applyRaw(t, paths, env, "--no-snapshot")
	if exit != 0 || statuses(rawOutcomes(t, done)) != "skipped,done" {
		t.Fatalf("no-snapshot: exit %d %+v %s", exit, done.Error, done.Result)
	}
	if got := strings.Join(sent(), " | "); got != "backups save | DELETE /api/config/scene/config/evening" {
		t.Fatalf("sent %s", got)
	}
}

// A verified helper update gets its update-revert checkpoint (before and
// after from the list); an update the read-back does not show gets none.
func TestPlanRawHelperUpdateSavesCheckpoint(t *testing.T) {
	for _, lands := range []bool{true, false} {
		var mu sync.Mutex
		max := 30
		paths, _ := rawRelay(t, map[string]func(map[string]any) any{
			"input_number/list": func(map[string]any) any {
				mu.Lock()
				defer mu.Unlock()
				return []any{map[string]any{"id": "heat", "name": "Heating limit", "min": 0, "max": max}}
			},
			"input_number/update": func(map[string]any) any {
				mu.Lock()
				defer mu.Unlock()
				if lands {
					max = 35
				}
				return map[string]any{"id": "heat", "max": 35}
			},
		})
		_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_number/update","input_number_id":"heat","max":35}}]`)
		if !strings.Contains(env.Say, "change the number helper “Heating limit”: max 35") {
			t.Fatalf("say %q", env.Say)
		}
		exit, env := applyRaw(t, paths, env)
		snap, err := loadUndoSnapshot(paths)
		if !lands {
			if exit != 0 || env.Undo != nil || err == nil || len(env.Notices) == 0 || !strings.Contains(fmt.Sprint(env.Notices), "not revertible") {
				t.Fatalf("unverified update: exit %d undo %+v notices %v err %v", exit, env.Undo, env.Notices, err)
			}
			continue
		}
		if exit != 0 || env.Undo == nil || err != nil || snap.Domain != "input_number" || snap.TargetID != "heat" || snap.Name != "Heating limit" ||
			!strings.Contains(compactJSON(snap.BeforeConfig), `"max":30`) || !strings.Contains(compactJSON(snap.ExpectedAfter), `"max":35`) {
			t.Fatalf("checkpoint: exit %d undo %+v snap %+v err %v", exit, env.Undo, snap, err)
		}
	}
}

// An energy save that empties or replaces a list is typed, also when the
// prefs can't be read; one that adds to a list is a preview.
func TestPlanRawEnergyTier(t *testing.T) {
	prefs := map[string]any{"energy_sources": []any{map[string]any{"type": "grid"}}, "device_consumption": []any{map[string]any{"stat_consumption": "sensor.a"}}}
	var reply any = prefs
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{"energy/get_prefs": func(map[string]any) any { return reply }})
	for _, tc := range []struct {
		prefs   any
		payload string
		tier    string
	}{
		{prefs, `{"type":"energy/save_prefs","device_consumption":[]}`, "typed"},
		{prefs, `{"type":"energy/save_prefs","device_consumption":[{"stat_consumption":"sensor.b"}]}`, "preview"}, // one entry replaced
		{prefs, `{"type":"energy/save_prefs","device_consumption":[{"stat_consumption":"sensor.a"},{"stat_consumption":"sensor.b"}]}`, "preview"},
		{prefs, `{"type":"energy/save_prefs","energy_sources":[{"type":"solar"}],"device_consumption":[{"stat_consumption":"sensor.a"}]}`, "preview"},
		{map[string]any{"device_consumption": []any{map[string]any{"stat_consumption": "sensor.a", "price": json.Number("0.30")}, map[string]any{"stat_consumption": "sensor.b"}}},
			`{"type":"energy/save_prefs","device_consumption":[{"stat_consumption":"sensor.a","price":0.3}]}`, "preview"}, // 0.30 is 0.3: one entry dropped
		{map[string]any{"device_consumption": []any{map[string]any{"stat_consumption": "sensor.a", "price": json.Number("0.30")}, map[string]any{"stat_consumption": "sensor.b"}}},
			`{"type":"energy/save_prefs","device_consumption":[{"stat_consumption":"sensor.a","price":0.31}]}`, "typed"}, // both entries gone
		{fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "unknown_error: boom"}, `{"type":"energy/save_prefs","device_consumption":[{"stat_consumption":"sensor.b"}]}`, "typed"},
		{fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "not_found: No prefs"}, `{"type":"energy/save_prefs","device_consumption":[{"stat_consumption":"sensor.b"}]}`, "preview"},
	} {
		reply = tc.prefs
		exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":`+tc.payload+`}]`)
		if exit != verbExitConfirmation || env.Tier != tc.tier {
			t.Errorf("%v %s: exit %d tier %s", tc.prefs, tc.payload, exit, env.Tier)
		}
	}
}

// Reads belong to relay and ha_read, except the reads only a plan sends;
// anything the guard refuses refuses the whole plan, and nothing is sent.
func TestPlanRawReadsAndRefusals(t *testing.T) {
	paths, sent := rawRelay(t, nil)
	for _, item := range []string{
		`{"endpoint":"ws","payload":{"type":"blueprint/import","url":"https://example.com/a.yaml"}}`,
		`{"endpoint":"ws","payload":{"type":"camera/stream","entity_id":"camera.door"}}`,
		`{"endpoint":"ws","payload":{"type":"media_source/resolve_media","media_content_id":"media-source://x"}}`,
		`{"endpoint":"core","payload":{"method":"GET","path":"/api/config/config_entries/flow/0123abcd"}}`,
		`{"endpoint":"core","payload":{"method":"GET","path":"/api/config/config_entries/subentries/flow/0123abcd"}}`,
		`{"endpoint":"core","payload":{"method":"GET","path":"/api/repairs/issues/fix/0123abcd"}}`,
		`{"endpoint":"core","payload":{"method":"DELETE","path":"/api/config/config_entries/flow/0123abcd"}}`,
	} {
		if exit, env := planRaw(t, paths, "["+item+"]"); exit != verbExitConfirmation || env.Tier != "preview" {
			t.Errorf("%s: exit %d %+v", item, exit, env.Error)
		}
	}
	for item, says := range map[string]string{
		`{"endpoint":"ws","payload":{"type":"get_states"}}`:                                             "it only reads",
		`{"endpoint":"core","payload":{"method":"GET","path":"/api/states"}}`:                           "it only reads",
		`{"endpoint":"core","payload":{"method":"GET","path":"/api/config/config_entries/flow/a?x=1"}}`: "it only reads",
		`{"endpoint":"files","payload":{"action":"read_file","path":"configuration.yaml"}}`:             "it only reads",
		`{"endpoint":"backups","payload":{"action":"list"}}`:                                            "it only reads",
		`{"endpoint":"ws","payload":{"message":{"type":"input_boolean/create"},"collect_events":{}}}`:   "collect_events",
		`{"endpoint":"ws","payload":{"type":"call_service","domain":"lock","service":"unlock"}}`:        "ha-nova call",
		`{"endpoint":"core","payload":{"method":"POST","path":"/api/services/lock/unlock"}}`:            "ha-nova call",
	} {
		exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_boolean/create","name":"A"}},`+item+"]")
		if exit != verbExitRefused || env.Error == nil || env.Error.Code != "REFUSED" || !strings.Contains(env.Error.Message, "item 2: ") || !strings.Contains(env.Error.Message, says) {
			t.Errorf("%s: exit %d %+v", item, exit, env.Error)
		}
	}
	for _, items := range []string{"[]", `[{"endpoint":"trace","payload":{}}]`, `[{"endpoint":"ws","payload":[]}]`,
		`[{"endpoint":"core","payload":{"method":"PUT","path":"/api/x"}}]`, `[{"endpoint":"ws","payload":{},"extra":1}]`,
		"[" + strings.Repeat(`{"endpoint":"ws","payload":{"type":"input_boolean/create"}},`, 20) + `{"endpoint":"ws","payload":{"type":"input_boolean/create"}}]`} {
		if exit, env := planRaw(t, paths, items); exit != verbExitError || env.Error == nil || env.Error.Code != "USAGE" {
			t.Errorf("%.80s: exit %d %+v", items, exit, env.Error)
		}
	}
	if len(sent()) != 0 {
		t.Fatalf("planning sent %v", sent())
	}
}

// A typed plan's say and confirmation form name every item, and each
// item's answer is redacted like relay's.
func TestPlanRawTypedSayPromptAndRedaction(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"config/entity_registry/update": func(map[string]any) any {
			return map[string]any{"entity_entry": map[string]any{"options": map[string]any{"lock": map[string]any{"default_code": "4711"}}}}
		},
		"GET /api/states/sensor.door_battery": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"entity_id": "sensor.door_battery", "state": "80", "attributes": map[string]any{"friendly_name": "Door battery"}}}
		},
	})
	_, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"config/entity_registry/update","entity_id":"sensor.door_battery","name":"Front door battery"}},
		{"endpoint":"files","payload":{"action":"delete_file","path":"packages/old.yaml"}},
		{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"guest"}}]`)
	plan, err := readPlanFile(filepath.Join(planDir(paths), env.Plan.ID+".json"))
	if err != nil || env.Tier != "typed" {
		t.Fatalf("plan %+v %v", env, err)
	}
	for _, want := range []string{"1. Change the entity “Door battery”: name “Front door battery”.", "2. Delete the file “packages/old.yaml” in the config folder.",
		"3. Delete the toggle helper “guest”.", "This deletes", sayNeedsYou("en", "")} {
		if !strings.Contains(env.Say, want) || want != sayNeedsYou("en", "") && !strings.Contains(plan.Prompt, want) {
			t.Errorf("say or prompt lacks %q:\n%s\n---\n%s", want, env.Say, plan.Prompt)
		}
	}
	if strings.Contains(plan.Prompt, env.Plan.Code) {
		t.Fatalf("the prompt holds the code")
	}
	_, applied := applyRaw(t, paths, env)
	items := rawOutcomes(t, applied)
	if statuses(items) != "done,done,done" || strings.Contains(string(applied.Result), "4711") || !strings.Contains(string(items[0].Response), secretHidden) {
		t.Fatalf("apply %s", applied.Result)
	}
}
