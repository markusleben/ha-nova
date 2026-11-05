package main

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// helperRelay is a raw relay with the dropdown helpers "mode" (Home, Away,
// Sleep) and "levels" (1.0, 1.5), and the schedule "heating" (two Monday
// blocks, one on Tuesday with data), as their lists return them; change
// swaps the first of a list, as another client would, and extra adds
// handlers.
func helperRelay(t *testing.T, extra map[string]func(map[string]any) any) (paths runtimePaths, sent func() []string, change func(collection string, item map[string]any)) {
	t.Helper()
	var mu sync.Mutex
	items := map[string]map[string]any{
		"input_select": {"id": "mode", "name": "Mode", "icon": "mdi:home", "options": []any{"Home", "Away", "Sleep"}},
		"schedule": {"id": "heating", "name": "Heating", "monday": []any{map[string]any{"from": "06:00:00", "to": "08:00:00"}, map[string]any{"from": "17:00:00", "to": "22:00:00"}},
			"tuesday": []any{map[string]any{"from": "06:00:00", "to": "08:00:00", "data": map[string]any{"temperature": 21}}}},
	}
	more := map[string][]any{"input_select": {map[string]any{"id": "levels", "name": "Levels", "options": []any{"1.0", "1.5"}}}}
	handlers := map[string]func(map[string]any) any{"config/entity_registry/list": func(map[string]any) any { // each helper's entity (#946)
		return []any{map[string]any{"entity_id": "input_select.mode", "platform": "input_select", "unique_id": "mode"},
			map[string]any{"entity_id": "input_select.levels", "platform": "input_select", "unique_id": "levels"},
			map[string]any{"entity_id": "schedule.heating", "platform": "schedule", "unique_id": "heating"}}
	}}
	for collection := range items {
		handlers[collection+"/list"] = func(map[string]any) any {
			mu.Lock()
			defer mu.Unlock()
			return append([]any{items[collection]}, more[collection]...)
		}
	}
	for key, handle := range extra {
		handlers[key] = handle
	}
	paths, sent = rawRelay(t, handlers)
	return paths, sent, func(collection string, item map[string]any) { mu.Lock(); defer mu.Unlock(); items[collection] = item }
}

const (
	selectWithoutSleep = `{"endpoint":"ws","payload":{"type":"input_select/update","input_select_id":"mode","name":"Mode","options":["Home","Away"]}}`
	selectKeepingAll   = `{"endpoint":"ws","payload":{"type":"input_select/update","input_select_id":"mode","name":"Mode","icon":"mdi:home","options":["Home","Away","Sleep","Guest"]}}`
	selectCopy         = `{"endpoint":"backups","payload":{"action":"save","category":"helpers","name":"auto-mode","data":{"options":["Home","Away","Sleep"],"id":"mode","name":"Mode","icon":"mdi:home"}}}`
	scheduleOnlyMonday = `{"endpoint":"ws","payload":{"type":"schedule/update","schedule_id":"heating","name":"Heating","monday":[{"from":"06:00:00","to":"08:00:00"}]}}`
	scheduleKeepingAll = `{"endpoint":"ws","payload":{"type":"schedule/update","schedule_id":"heating","name":"Heating","monday":[{"from":"6:00","to":"08:00"},{"from":"17:00:00","to":"22:00:00","data":{"eco":true}}],"tuesday":{"from":"06:00:00","to":"08:00:00","data":{"temperature":21.0}},"friday":[{"from":"07:00:00","to":"09:00:00"}]}}`
	scheduleCopy       = `{"endpoint":"backups","payload":{"action":"save","category":"helpers","name":"auto-heating","data":{"id":"heating","name":"Heating","monday":[{"from":"06:00:00","to":"08:00:00"},{"from":"17:00:00","to":"22:00:00"}],"tuesday":[{"from":"06:00:00","to":"08:00:00","data":{"temperature":21}}]}}}`
)

// An input_select or schedule update replaces the whole helper: one that
// leaves out an option, a time block, or a block's data is refused unless a
// copy of the live helper is saved first in the same plan; the refusal says
// what would go and how to keep it. An option sent as a number counts as
// left out: the Relay re-encodes it (1.0 becomes 1) before Home Assistant
// stores its text. One that keeps everything (a time as 6:00, one block not
// in a list, data added to a block or a key to its data) is a preview.
func TestPlanRawHelperUpdateThatDropsNeedsACopyFirst(t *testing.T) {
	paths, sent, _ := helperRelay(t, nil)
	staleCopy := strings.Replace(selectCopy, `"Sleep"`, `"Night"`, 1)
	prune := `{"endpoint":"backups","payload":{"action":"prune","keep_named":false}}`
	for items, says := range map[string]string{
		"[" + selectWithoutSleep + "]": `would remove the options "Sleep" from it`,
		"[" + scheduleOnlyMonday + "]": "would remove the time blocks monday 17:00:00-22:00:00, tuesday 06:00:00-08:00:00 from it",
		"[" + strings.Replace(scheduleKeepingAll, `,"data":{"temperature":21.0}`, "", 1) + "]":                                        "would remove the time blocks tuesday 06:00:00-08:00:00 (only its data) from it",
		`[{"endpoint":"ws","payload":{"type":"input_select/update","input_select_id":"levels","name":"Levels","options":[1.0,1.5]}}]`: `would remove the options "1.0", "1.5" from it`,
		"[" + staleCopy + "," + selectWithoutSleep + "]":                                                                              `"Sleep"`,
		"[" + selectCopy + "," + prune + "," + selectWithoutSleep + "]":                                                               `"Sleep"`,
	} {
		exit, env := planRaw(t, paths, items)
		if exit != verbExitRefused || env.Error == nil || env.Error.Code != "REFUSED" || !strings.Contains(env.Error.Message, says) ||
			!regexp.MustCompile(`send its item with the change, its id as (input_select|schedule)_id, every `).MatchString(env.Error.Next) || !strings.Contains(env.Error.Next, `"action":"save","category":"helpers","name":"auto-`) {
			t.Errorf("%s: exit %d %+v", items, exit, env.Error)
		}
	}
	addingData := strings.Replace(scheduleKeepingAll, `"data":{"temperature":21.0}`, `"data":{"temperature":21.0,"eco":true}`, 1)
	for _, items := range []string{selectKeepingAll, scheduleKeepingAll, addingData, selectCopy + "," + selectWithoutSleep, scheduleCopy + "," + scheduleOnlyMonday} {
		exit, env := planRaw(t, paths, "["+items+"]")
		if exit != verbExitConfirmation || env.Tier != "preview" {
			t.Fatalf("%s: exit %d %+v", items, exit, env.Error)
		}
		if strings.HasPrefix(items, `{"endpoint":"backups"`) {
			if exit, env = applyRaw(t, paths, env); exit != 0 || statuses(rawOutcomes(t, env)) != "done,done" {
				t.Fatalf("apply %s: exit %d %+v %s", items, exit, env.Error, env.Result)
			}
		}
	}
	if exit, env := planRaw(t, paths, "["+selectKeepingAll+","+selectWithoutSleep+"]"); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "updates the input_select mode once") {
		t.Fatalf("a second update that drops what the first added: exit %d %+v", exit, env.Error)
	}
	if got := strings.Join(sent(), " | "); got != "backups save | input_select/update | backups save | schedule/update" {
		t.Fatalf("sent %s", got)
	}
}

// A key whose value is null is still a key: an update that leaves it out of
// a block's data removes it, and one that sends it as null keeps it.
func TestPlanRawHelperUpdateKeepsANullDataKey(t *testing.T) {
	paths, _, change := helperRelay(t, nil)
	change("schedule", map[string]any{"id": "heating", "name": "Heating", "monday": []any{map[string]any{"from": "06:00:00", "to": "08:00:00", "data": map[string]any{"mode": nil}}}})
	update := `[{"endpoint":"ws","payload":{"type":"schedule/update","schedule_id":"heating","name":"Heating","monday":[{"from":"06:00:00","to":"08:00:00"%s}]}}]`
	for data, refused := range map[string]bool{"": true, `,"data":{}`: true, `,"data":{"mode":null}`: false} {
		exit, env := planRaw(t, paths, fmt.Sprintf(update, data))
		if refused && (exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "monday 06:00:00-08:00:00 (only its data)")) ||
			!refused && (exit != verbExitConfirmation || env.Tier != "preview") {
			t.Errorf("data %q: exit %d %+v", data, exit, env.Error)
		}
	}
}

// A field an update sends as null is shown by the read-back only when the
// helper holds it as null: a read-back without it gets no checkpoint.
func TestPlanRawHelperCheckpointNeedsANullFieldShown(t *testing.T) {
	paths, _, change := helperRelay(t, nil)
	update := `[{"endpoint":"ws","payload":{"type":"input_select/update","input_select_id":"mode","name":"Mode","icon":"mdi:home","options":["Home","Away","Sleep"]%s}}]`
	for field, checkpoint := range map[string]bool{"": true, `,"initial":null`: false} {
		_, env := planRaw(t, paths, fmt.Sprintf(update, field))
		exit, env := applyRaw(t, paths, env)
		if exit != 0 || (env.Undo != nil) != checkpoint || !checkpoint && !strings.Contains(fmt.Sprint(env.Notices), "its read-back does not show the update") {
			t.Errorf("field %q: exit %d undo %+v notices %v", field, exit, env.Undo, env.Notices)
		}
	}
	change("input_select", map[string]any{"id": "mode", "name": "Mode", "icon": "mdi:home", "options": []any{"Home", "Away", "Sleep"}, "initial": nil})
	_, env := planRaw(t, paths, fmt.Sprintf(update, `,"initial":null`))
	if exit, env := applyRaw(t, paths, env); exit != 0 || env.Undo == nil {
		t.Errorf("a null the helper holds: exit %d notices %v", exit, env.Notices)
	}
}

// The helper's live item is the plan's basis: one edited in Home Assistant
// since the preview is drift, and an option added there would be dropped, so
// the update is refused; either way nothing is sent.
func TestPlanRawHelperUpdateDriftsWhenTheHelperChanged(t *testing.T) {
	for _, tc := range []struct {
		item       map[string]any
		code, says string
	}{
		{map[string]any{"id": "mode", "name": "House mode", "icon": "mdi:home", "options": []any{"Home", "Away", "Sleep"}}, "PLAN_DRIFT", "nothing was sent"},
		{map[string]any{"id": "mode", "name": "Mode", "icon": "mdi:home", "options": []any{"Home", "Away", "Sleep", "Party"}}, "REFUSED", `would remove the options "Party"`},
	} {
		paths, sent, change := helperRelay(t, nil)
		exit, env := planRaw(t, paths, "["+selectKeepingAll+"]")
		if exit != verbExitConfirmation {
			t.Fatalf("plan: exit %d %+v", exit, env.Error)
		}
		change("input_select", tc.item)
		exit, env = applyRaw(t, paths, env)
		if env.Error == nil || env.Error.Code != tc.code || !strings.Contains(env.Error.Message, tc.says) || len(sent()) != 0 {
			t.Fatalf("apply after the helper changed: exit %d %+v %v", exit, env.Error, sent())
		}
	}
}

// An update is checked against the live helper again right before it is
// sent: another client may change it while an earlier step runs.
func TestPlanRawHelperUpdateRechecksTheHelperBeforeItsUpdate(t *testing.T) {
	var change func(string, map[string]any)
	paths, sent, change := helperRelay(t, map[string]func(map[string]any) any{
		"backups save": func(map[string]any) any {
			change("input_select", map[string]any{"id": "mode", "name": "Mode", "icon": "mdi:home", "options": []any{"Home", "Away", "Sleep", "Party"}})
			return map[string]any{"file": "helpers/auto-mode.json.gz"}
		},
	})
	exit, env := planRaw(t, paths, "["+selectCopy+","+selectWithoutSleep+"]")
	if exit != verbExitConfirmation {
		t.Fatalf("plan: exit %d %+v", exit, env.Error)
	}
	exit, applied := applyRaw(t, paths, env)
	outcomes := rawOutcomes(t, applied)
	if exit == 0 || statuses(outcomes) != "done,refused" || !strings.Contains(outcomes[1].Error, "the input_select mode changed in Home Assistant since the preview") {
		t.Fatalf("apply: exit %d %+v %s", exit, applied.Error, applied.Result)
	}
	if got := strings.Join(sent(), " | "); got != "backups save" {
		t.Fatalf("sent %s", got)
	}
}

// A helper its list doesn't hold is NOT_FOUND, with where its id comes
// from (an entity_id is the usual mistake): at planning for a dropdown helper
// or schedule, at apply for any other helper. One whose list can't be read
// is never updated.
func TestPlanRawHelperUpdateAbsentAndUnreadable(t *testing.T) {
	paths, sent, _ := helperRelay(t, map[string]func(map[string]any) any{"input_number/list": func(map[string]any) any { return []any{} }})
	gone := strings.Replace(selectWithoutSleep, `"input_select_id":"mode"`, `"input_select_id":"gone"`, 1)
	if exit, env := planRaw(t, paths, "["+gone+"]"); exit != verbExitNotFound || env.Error == nil || env.Error.Code != "NOT_FOUND" ||
		!strings.Contains(env.Error.Message, "there is no input_select gone") || !strings.Contains(env.Error.Next, `ws {"type":"input_select/list"}, not its entity_id`) {
		t.Fatalf("a dropdown helper not in its list: exit %d %+v", exit, env.Error)
	}
	exit, env := planRaw(t, paths, `[{"endpoint":"ws","payload":{"type":"input_number/update","input_number_id":"gone","name":"Heat","min":0,"max":35}}]`)
	if exit != verbExitConfirmation {
		t.Fatalf("plan: exit %d %+v", exit, env.Error)
	}
	if exit, env = applyRaw(t, paths, env); exit != verbExitNotFound || env.Error == nil || env.Error.Code != "NOT_FOUND" ||
		!strings.Contains(env.Error.Next, `ws {"type":"input_number/list"}`) {
		t.Fatalf("a number helper not in its list: exit %d %+v", exit, env.Error)
	}
	unreadable, _ := rawRelay(t, map[string]func(map[string]any) any{
		"schedule/list": func(map[string]any) any { return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "no answer"} },
	})
	if exit, env := planRaw(t, unreadable, "["+scheduleKeepingAll+"]"); exit != verbExitError || env.Plan != nil || env.Error == nil ||
		!strings.Contains(env.Error.Message, "cannot read the schedule heating") {
		t.Fatalf("an unreadable schedule: exit %d %+v", exit, env.Error)
	}
	if len(sent()) != 0 {
		t.Fatalf("planning sent %v", sent())
	}
}

// An update after its copy says what it removes, in the home's language:
// options by name, time blocks by weekday and HH:MM, and a block whose data
// alone goes as its data. One that removes nothing says nothing more.
func TestPlanRawHelperUpdateSaysWhatGoes(t *testing.T) {
	dataOnly := strings.Replace(scheduleKeepingAll, `,"data":{"temperature":21.0}`, "", 1)
	for _, tc := range []struct{ lang, items, says string }{
		{"en", selectCopy + "," + selectWithoutSleep, "Removes “Sleep” from the dropdown helper."},
		{"de", selectCopy + "," + selectWithoutSleep, "Entfernt „Sleep“ aus dem Dropdown-Menü-Helfer."},
		{"en", scheduleCopy + "," + scheduleOnlyMonday, "Removes Monday 17:00–22:00 and Tuesday 06:00–08:00 from the schedule."},
		{"de", scheduleCopy + "," + dataOnly, "Entfernt die Daten von Dienstag 06:00–08:00 aus dem Zeitplan."},
		{"en", selectKeepingAll, ""},
	} {
		paths, _, _ := helperRelay(t, map[string]func(map[string]any) any{
			"GET /api/config": func(map[string]any) any {
				return map[string]any{"status": 200, "body": map[string]any{"language": tc.lang}}
			},
		})
		exit, env := planRaw(t, paths, "["+tc.items+"]")
		if exit != verbExitConfirmation || !strings.Contains(env.Say, tc.says) || tc.says == "" && strings.Contains(env.Say, "Removes") {
			t.Errorf("%s %s: exit %d say %q %+v", tc.lang, tc.items, exit, env.Say, env.Error)
		}
	}
}

// A copy never keeps a value HA NOVA hides (guardRaw), so a removal from a
// helper or scene that holds one is refused, also after its copy: nothing
// could bring the value back.
func TestRawRemovalOfAHiddenValueIsRefused(t *testing.T) {
	paths, sent, change := helperRelay(t, nil)
	change("schedule", map[string]any{"id": "heating", "name": "Heating", "monday": []any{map[string]any{"from": "06:00:00", "to": "08:00:00"}},
		"tuesday": []any{map[string]any{"from": "06:00:00", "to": "08:00:00", "data": map[string]any{"default_code": "4711"}}}})
	scheduleCopy := `{"endpoint":"backups","payload":{"action":"save","category":"helpers","name":"auto-heating","data":{"id":"heating","name":"Heating",` +
		`"monday":[{"from":"06:00:00","to":"08:00:00"}],"tuesday":[{"from":"06:00:00","to":"08:00:00","data":{"default_code":"` + secretHidden + `"}}]}}}`
	scenes, sceneSent, changeScene := sceneRelay(t)
	changeScene(map[string]any{"id": "evening", "name": "Evening", "entities": map[string]any{"light.desk": "on", "light.hall": map[string]any{"state": "off", "default_code": "4711"}}})
	sceneCopy := `{"endpoint":"backups","payload":{"action":"save","category":"scenes","name":"auto-evening","data":{"id":"evening","name":"Evening",` +
		`"entities":{"light.desk":"on","light.hall":{"state":"off","default_code":"` + secretHidden + `"}}}}}`
	for _, tc := range []struct {
		paths runtimePaths
		items string
	}{{paths, "[" + scheduleOnlyMonday + "]"}, {paths, "[" + scheduleCopy + "," + scheduleOnlyMonday + "]"}, {scenes, "[" + sceneCopy + "," + sceneSaveWithoutHall + "]"}} {
		exit, env := planRaw(t, tc.paths, tc.items)
		if exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, "holds a value HA NOVA hid, which no copy can keep") ||
			!strings.Contains(env.Error.Next, "They can make the change in Home Assistant.") {
			t.Errorf("%s: exit %d %+v", tc.items, exit, env.Error)
		}
	}
	if got := append(sent(), sceneSent()...); len(got) != 0 {
		t.Fatalf("sent %v", got)
	}
}

// The copy an update that drops needs can't be left out: when it fails,
// apply offers no --no-snapshot and refuses one, and goes on once the copy
// is saved.
func TestPlanRawHelperUpdateKeepsItsCopy(t *testing.T) {
	var full atomic.Bool
	full.Store(true)
	paths, sent, _ := helperRelay(t, map[string]func(map[string]any) any{
		"backups save": func(map[string]any) any {
			if full.Load() {
				return fakeRelayFailure{"SNAPSHOT_STORE_FULL", "the snapshot store is full"}
			}
			return map[string]any{"file": "helpers/auto-heating.json.gz"}
		},
	})
	exit, env := planRaw(t, paths, "["+scheduleCopy+","+scheduleOnlyMonday+"]")
	if exit != verbExitConfirmation {
		t.Fatalf("plan: exit %d %+v", exit, env.Error)
	}
	exit, failed := applyRaw(t, paths, env)
	if exit != verbExitError || failed.Error == nil || failed.Error.Code != "SNAPSHOT_REQUIRED" ||
		!strings.Contains(failed.Error.Next, "removes time blocks from a schedule") || !strings.Contains(failed.Error.Next, "never with --no-snapshot") {
		t.Fatalf("the copy failed: exit %d %+v", exit, failed.Error)
	}
	if exit, skipped := applyRaw(t, paths, env, "--no-snapshot"); exit != verbExitRefused || skipped.Error == nil || skipped.Error.Code != "NO_SNAPSHOT_NOT_OFFERED" ||
		!strings.Contains(skipped.Error.Message, "removes time blocks from a schedule") {
		t.Fatalf("--no-snapshot: exit %d %+v", exit, skipped.Error)
	}
	full.Store(false)
	if exit, done := applyRaw(t, paths, env); exit != 0 || statuses(rawOutcomes(t, done)) != "done,done" {
		t.Fatalf("apply once the copy saves: exit %d %+v %s", exit, done.Error, done.Result)
	}
	if got := strings.Join(sent(), " | "); got != "backups save | backups save | schedule/update" {
		t.Fatalf("sent %s", got)
	}
}

// ha-nova relay ws sends one request, so no copy can come first: an update
// that leaves out an option or block is refused there, also inside a
// collect_events envelope, and the refusal names the plan that keeps a
// copy. One that keeps everything goes through. A chat app's raw plan gets
// the same refusal.
func TestRelayHelperUpdateThatDropsIsRefused(t *testing.T) {
	paths, sent, _ := helperRelay(t, nil)
	payload := func(item string) string {
		return item[strings.Index(item, `"payload":`)+len(`"payload":`) : len(item)-1]
	}
	for _, body := range []string{payload(selectWithoutSleep), payload(scheduleOnlyMonday), `{"message":` + payload(selectWithoutSleep) + `,"collect_events":{"max_events":1}}`} {
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", body}) })
		if exit != rawRefusedExit || !strings.Contains(stderr, "refused raw helper update") || !strings.Contains(stderr, "would remove the") || !strings.Contains(stderr, "ha-nova plan raw") {
			t.Errorf("%s: exit %d %s", body, exit, stderr)
		}
	}
	if got := sent(); len(got) != 0 {
		t.Fatalf("refused updates sent %v", got)
	}
	var exit int
	stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", payload(selectKeepingAll)}) })
	if exit != 0 || strings.Join(sent(), " | ") != "input_select/update" {
		t.Fatalf("an update keeping every option: exit %d %s %v", exit, stderr, sent())
	}
	c := startMCP(t, paths)
	mcpInit(c)
	chat, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws",
		"payload": map[string]any{"type": "input_select/update", "input_select_id": "mode", "name": "Mode", "options": []any{"Home"}}}}}))
	if code != "REFUSED" || !strings.Contains(fmt.Sprint(chat), `would remove the options "Away", "Sleep"`) ||
		!strings.Contains(fmt.Sprint(chat), "backups save of the helper's current item first (ha_plan kind raw)") || strings.Contains(fmt.Sprint(chat), "on a computer") {
		t.Fatalf("chat: %s %v", code, chat)
	}
}
