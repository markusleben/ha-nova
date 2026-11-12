package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// countReads records every request the fake answers: "ws <type>" or "core <path>".
func countReads(f *fakeHA) func() []string {
	var mu sync.Mutex
	var seen []string
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		key, _ := p["type"].(string)
		if endpoint == "core" {
			key, _ = p["path"].(string)
		}
		mu.Lock()
		seen = append(seen, endpoint+" "+key)
		mu.Unlock()
		return nil, false
	}
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), seen...) }
}

func countPrefix(reads []string, prefix string) int {
	n := 0
	for _, r := range reads {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

func unexplainedCallService(f *fakeHA) {
	f.events = []map[string]any{{"event": "call_service", "listener_count": 2}}
	f.automation("watch", "w1", map[string]any{
		"triggers": []any{map[string]any{"trigger": "event", "event_type": "call_service", "event_data": map[string]any{"domain": "light"}}},
		"actions":  []any{step("light.turn_on", "light.hall")},
	})
}

func TestATypedTargetSkipsTheListenerCensus(t *testing.T) {
	f := newFakeHA()
	unexplainedCallService(f)
	reads := countReads(f)
	exit, env, _, _ := runPlanForTest(t, f, "lock.unlock", "lock.front")
	codes := reasonCodes(env)
	if exit != 3 || env.Tier != "typed" || !strings.Contains(codes, reasonAccessLock) || strings.Contains(codes, reasonListenerUnexpl) {
		t.Fatalf("exit %d tier %s reasons %s", exit, env.Tier, codes)
	}
	if n := countPrefix(reads(), "ws automation/config"); n != 0 {
		t.Fatalf("the census read %d automation configs for a typed target: %v", n, reads())
	}
}

func TestAnOrdinaryTargetStillRunsTheListenerCensus(t *testing.T) {
	f := newFakeHA()
	unexplainedCallService(f)
	reads := countReads(f)
	exit, env, _, _ := runPlanForTest(t, f, "light.turn_on", "light.kitchen")
	if exit != 3 || !strings.Contains(reasonCodes(env), reasonListenerUnexpl) {
		t.Fatalf("exit %d tier %s reasons %s", exit, env.Tier, reasonCodes(env))
	}
	if countPrefix(reads(), "ws automation/config") == 0 {
		t.Fatalf("no census for an ordinary target: %v", reads())
	}
}

func countExact(reads []string, want string) int {
	n := 0
	for _, r := range reads {
		if r == want {
			n++
		}
	}
	return n
}

func TestManyStatesComeFromOneRead(t *testing.T) {
	f := newFakeHA()
	var related []string
	for i := 0; i < relaySessionMaxConcurrent+2; i++ {
		id := fmt.Sprintf("follow_%d", i)
		related = append(related, "automation."+id)
		f.automation(id, id, map[string]any{
			"triggers": []any{map[string]any{"trigger": "state", "entity_id": "light.kitchen"}},
			"actions":  []any{step("light.turn_on", "light.hall")},
		})
	}
	f.related["entity:light.kitchen"] = related
	reads := countReads(f)
	runPlanForTest(t, f, "light.turn_off", "light.kitchen")
	if got := reads(); countPrefix(got, "core /api/states/automation.") != 0 || countExact(got, "core /api/states") != 1 {
		t.Fatalf("want the automations' states from one /api/states read: %v", got)
	}
}

func TestFewStatesKeepSingleReads(t *testing.T) {
	f := newFakeHA()
	reads := countReads(f)
	runPlanForTest(t, f, "light.turn_on", "light.kitchen")
	if got := reads(); countPrefix(got, "core /api/states/") == 0 || countExact(got, "core /api/states") != 0 {
		t.Fatalf("want single state reads only: %v", got)
	}
}
