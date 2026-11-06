package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Issue #1025: over Home Assistant Link every read waits a round trip, so a
// check's time is the number of reads that wait on one another, not the
// number of reads. morningHome is a routine whose one parallel step opens
// 12 covers, each with an automation that reacts to it, in a home with 60
// more automations and a listener of automation_triggered (the listener
// scan reads every automation).
func morningHome() (*fakeHA, map[string]any) {
	f := newFakeHA()
	var covers []any
	for i := range 12 {
		cover := fmt.Sprintf("cover.room_%02d", i)
		f.ent(cover, "zha", "closed", map[string]any{"_di": fmt.Sprintf("dev_%02d", i), "device_class": "shutter"})
		f.automation(fmt.Sprintf("follow_%02d", i), fmt.Sprintf("f%02d", i), map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": cover}}, "actions": []any{step("light.turn_on", "light.hall")}})
		f.related["entity:"+cover] = []string{fmt.Sprintf("automation.follow_%02d", i)}
		covers = append(covers, cover)
	}
	for i := range 60 {
		f.automation(fmt.Sprintf("other_%02d", i), fmt.Sprintf("o%02d", i), map[string]any{"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.kitchen"}}, "actions": []any{}})
	}
	f.automation("reachability", "reach", map[string]any{"triggers": []any{map[string]any{"trigger": "event", "event_type": "automation_triggered"}}, "actions": []any{}})
	f.events = []map[string]any{{"event": "automation_triggered", "listener_count": 1}}
	routine := func(alias string) map[string]any {
		return map[string]any{"alias": "Morning", "triggers": []any{map[string]any{"trigger": "state", "entity_id": "binary_sensor.door"}}, "actions": []any{map[string]any{"parallel": []any{
			map[string]any{"sequence": []any{map[string]any{"action": "cover.open_cover", "target": map[string]any{"entity_id": covers}}}},
			map[string]any{"sequence": []any{map[string]any{"alias": alias, "action": "light.turn_on", "target": map[string]any{"entity_id": "light.kitchen"}}}},
		}}}}
	}
	f.storeConfig("/api/config/automation/config/morning", routine("Kitchen"))
	return f, routine("Kitchen light") // a nested edit: the whole draft is weighed
}

// A nested edit of that routine is checked completely, reads every state
// from one state list, and its reads wait on one another for far fewer
// rounds than one consumer scan after another did (47 rounds; 22 now).
func TestAMorningRoutineChecksInFewRoundTrips(t *testing.T) {
	fastVerify(t)
	f, draft := morningHome()
	f.service = applies
	var mu sync.Mutex
	done, rounds := 0, 0 // done: the deepest round a finished read was in
	paths, log := setupFakeRelay(t, func(r *http.Request, endpoint string, p map[string]any) any {
		mu.Lock()
		round := done + 1 // a read starts after the reads it waited for
		rounds = max(rounds, round)
		mu.Unlock()
		time.Sleep(40 * time.Millisecond)
		reply := f.reply(r, endpoint, p)
		mu.Lock()
		done = max(done, round)
		mu.Unlock()
		return reply
	}, true)
	h := &writeHarness{t: t, f: f, paths: paths, log: log}
	_, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "morning", "--file", draftFile(t, draft))
	if env.Plan == nil || env.Coverage == nil || !env.Coverage.Complete || len(env.Consumers) != 13 { // the covers' and the listener's
		t.Fatalf("plan %+v coverage %+v consumers %d reasons %s", env.Plan, env.Coverage, len(env.Consumers), reasonCodes(env))
	}
	lists, single := 0, 0
	for _, c := range log.all() {
		switch path, _ := c.payload["path"].(string); {
		case path == "/api/states":
			lists++
		case strings.HasPrefix(path, "/api/states/"):
			single++
		}
	}
	if lists != 1 || single != 0 || rounds > 30 {
		t.Fatalf("state lists %d, single state reads %d, rounds %d", lists, single, rounds)
	}
}

// A state list Home Assistant answered without a list is a failed read: the
// check is typed, never one in which every entity has no state.
func TestAStateListWithoutAListIsAFailedRead(t *testing.T) {
	f, draft := morningHome()
	f.extra = func(_ string, p map[string]any) (any, bool) {
		return coreReply(200, nil), p["path"] == "/api/states"
	}
	h := newWriteHarness(t, f)
	exit, env := h.run(runPlanCommand, "write", "--domain", "automation", "--id", "morning", "--file", draftFile(t, draft))
	codes := reasonCodes(env)
	if exit != verbExitConfirmation || env.Tier != "typed" || !strings.Contains(codes, reasonScanFailed) || strings.Contains(codes, reasonCoverClassUnknown) { // a cover with no state
		t.Fatalf("exit %d tier %s reasons %s", exit, env.Tier, codes)
	}
}
