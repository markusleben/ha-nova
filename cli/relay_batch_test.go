package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// Batched loaded-config reads (MAR-180, docs/work/2026-11-20-ws-batch-reads-spec.md).

func withFakeBatches(t *testing.T) {
	fakeRelayBatch.Store(true)
	t.Cleanup(func() { fakeRelayBatch.Store(false); fakeRelayBatchFails.Store(false) })
}

// censusHome: twenty listening automations, one whose config is gone, and a
// call_service listener the census must explain.
func censusHome() *fakeHA {
	f := newFakeHA()
	unexplainedCallService(f)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("listen_%02d", i)
		f.automation(id, id, map[string]any{
			"triggers": []any{map[string]any{"trigger": "event", "event_type": "tag_scanned"}},
			"actions":  []any{step("light.turn_on", "light.hall")},
		})
	}
	f.automation("gone", "gone_id", map[string]any{})
	delete(f.configs, "/api/config/automation/config/gone_id")
	return f
}

// brokenHome: censusHome with one config read that fails.
func brokenHome() *fakeHA {
	f := censusHome()
	f.automation("broken", "broken_id", map[string]any{"triggers": []any{}})
	f.fail["automation/config automation.broken"] = true
	return f
}

func configReads(log *fakeRelayLog) int {
	n := 0
	for _, c := range log.all() {
		if c.endpoint == "ws" && c.payload["type"] == "automation/config" {
			n++
		}
	}
	return n
}

func TestTheCensusReadsConfigsInBatchesWithTheSameResult(t *testing.T) {
	broken := func(log *fakeRelayLog) int {
		n := 0
		for _, c := range log.all() {
			if c.endpoint == "ws" && c.payload["type"] == "automation/config" && c.payload["entity_id"] == "automation.broken" {
				n++
			}
		}
		return n
	}
	for _, tc := range []struct {
		name   string
		home   func() *fakeHA
		failed bool
	}{{"a config gone", censusHome, false}, {"a read that fails", brokenHome, true}} {
		fakeRelayBatch.Store(false)
		_, single, _, singleLog := runPlanForTest(t, tc.home(), "light.turn_on", "light.kitchen")
		withFakeBatches(t)
		_, batched, _, batchLog := runPlanForTest(t, tc.home(), "light.turn_on", "light.kitchen")
		if singleLog.batches.Load() != 0 {
			t.Fatalf("%s: a Relay without ws_batch got %d batches", tc.name, singleLog.batches.Load())
		}
		// 16 and the rest; the scan may batch the failing one again, as
		// its warm read's failure is never handed on
		if got := batchLog.batches.Load(); got != 2 && !(tc.failed && got == 3) {
			t.Fatalf("%s: batches = %d, want 2", tc.name, got)
		}
		// Every answered config is read once. The failing one is read by
		// the warm census, the scan's batch, and the scan itself at most,
		// whichever of them gets to it first (#1299).
		for label, log := range map[string]*fakeRelayLog{"single": singleLog, "batched": batchLog} {
			if answered, failing := configReads(log)-broken(log), broken(log); answered != 22 || tc.failed != (failing > 0) || failing > 3 {
				t.Fatalf("%s %s: answered config reads %d (want 22), failing one read %d times", tc.name, label, answered, failing)
			}
		}
		if single.Tier != batched.Tier || reasonCodes(single) != reasonCodes(batched) || !slices.Equal(single.Coverage.Notes, batched.Coverage.Notes) || single.Coverage.Complete != batched.Coverage.Complete {
			t.Fatalf("%s: batched plan differs: single %s [%s] %+v, batched %s [%s] %+v", tc.name, single.Tier, reasonCodes(single), single.Coverage, batched.Tier, reasonCodes(batched), batched.Coverage)
		}
	}
}

// A batch that fails as a whole was sent ahead of its consumers: nobody used
// that failure, so the scan reads each config once on its own and plans as
// without batches, never a config that is gone (whatever its message says).
func TestAFailedBatchIsAFailedReadNeverNotFound(t *testing.T) {
	home := func() *fakeHA {
		f := newFakeHA()
		unexplainedCallService(f)
		f.automation("other", "other_id", map[string]any{"triggers": []any{}})
		return f
	}
	singleExit, single, _, singleLog := runPlanForTest(t, home(), "light.turn_on", "light.kitchen")
	withFakeBatches(t)
	fakeRelayBatchFails.Store(true)
	exit, env, _, log := runPlanForTest(t, home(), "light.turn_on", "light.kitchen")
	if n := log.batches.Load(); n < 1 || n > 2 || configReads(log) != configReads(singleLog) { // the warm census's batch, and the scan's when it gets there first
		t.Fatalf("batches %d, single config reads %d: want a batch or two, then each config read as without batches (%d)", n, configReads(log), configReads(singleLog))
	}
	if codes := reasonCodes(env); exit != singleExit || env.Tier != single.Tier || codes != reasonCodes(single) || strings.Contains(codes, reasonUnreadConfig) {
		t.Fatalf("exit %d tier %s reasons %s: want the plan without batches (%s [%s])", exit, env.Tier, codes, single.Tier, reasonCodes(single))
	}
}

func TestReadBatchedSkipsWhatItMayNotBatch(t *testing.T) {
	withFakeBatches(t)
	paths, log := setupVerbRelayTest(t, newFakeHA().reply)
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	requests := []map[string]any{loadedConfigRequest("automation.a", "automation"), loadedConfigRequest("script.b", "script")}
	s.replay = map[string]bool{}
	s.readBatched(requests, 0)
	s.replay = nil
	s.readBatched(requests[:1], 0)
	s.cfg.bridgeCredential = "hanova-dev-v1.x"
	s.readBatched(requests, 0)
	if n := log.batches.Load(); n != 0 {
		t.Fatalf("batches = %d: want none while replaying, for one read, or for the App's own callers", n)
	}
}

func TestWSBatchItemIsExactlyALoadedConfigRead(t *testing.T) {
	for _, tc := range []struct {
		request map[string]any
		want    bool
	}{
		{map[string]any{"type": "automation/config", "entity_id": "automation.a_1"}, true},
		{map[string]any{"type": "script/config", "entity_id": "script.b"}, true},
		{map[string]any{"type": "script/config", "entity_id": "automation.a"}, false},
		{map[string]any{"type": "automation/config", "entity_id": "automation.A"}, false},
		{map[string]any{"type": "automation/config", "entity_id": "automation."}, false},
		{map[string]any{"type": "automation/config", "entity_id": "automation.a", "id": 1}, false},
		{map[string]any{"type": "config/entity_registry/get", "entity_id": "automation.a"}, false},
		{map[string]any{"type": "light/config", "entity_id": "light.a"}, false},
		{nil, false},
	} {
		if got := wsBatchItem(tc.request); got != tc.want {
			t.Errorf("wsBatchItem(%v) = %v, want %v", tc.request, got, tc.want)
		}
	}
}

func TestGateRatesABatchAsOneReadOrRefusesIt(t *testing.T) {
	item := `{"type":"automation/config","entity_id":"automation.a"}`
	refused := gateCheck{Verdict: gateRefuse, Kind: "write", Why: "batch"}
	sixteen := strings.Repeat(item+",", 15) + item
	for _, tc := range []struct {
		name, body string
		want       gateCheck
	}{
		{"reads", `{"batch":[` + item + `,{"type":"script/config","entity_id":"script.b"}]}`, gateCheck{Verdict: gatePass, Kind: "read"}},
		{"sixteen", `{"batch":[` + sixteen + `]}`, gateCheck{Verdict: gatePass, Kind: "read"}},
		{"seventeen", `{"batch":[` + sixteen + "," + item + `]}`, refused},
		{"empty", `{"batch":[]}`, refused},
		{"not a list", `{"batch":` + item + `}`, refused},
		{"another key", `{"batch":[` + item + `],"type":"automation/config"}`, refused},
		{"an item with another key", `{"batch":[{"type":"automation/config","entity_id":"automation.a","force":true}]}`, refused},
		{"another read", `{"batch":[{"type":"config/entity_registry/list"}]}`, refused},
		{"a write", `{"batch":[{"type":"config/area_registry/delete","area_id":"office"}]}`, refused},
		{"a duplicate key", `{"batch":[{"type":"automation/config","entity_id":"automation.a","entity_id":"automation.b"}]}`, gateCheck{Verdict: gateRefuse, Kind: "write"}},
	} {
		got, err := gateVerdict("ws", []byte(tc.body), func() (*relaySession, error) { return nil, errors.New("no session") })
		if err != nil || got.Verdict != tc.want.Verdict || got.Kind != tc.want.Kind || got.Why != tc.want.Why {
			t.Errorf("%s: gateVerdict = %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
}

// serveFakeBatch answers a POST /ws batch as the Relay does: only on the
// gate's pass as a read, each item as POST /ws answers it alone, in order.
func serveFakeBatch(t *testing.T, w http.ResponseWriter, r *http.Request, log *fakeRelayLog, raw []byte, items []any, reply func(*http.Request, string, map[string]any) any) {
	t.Helper()
	w.Header().Set("content-type", "application/json")
	if !fakeRelayBatch.Load() {
		t.Errorf("a batch reached a Relay without ws_batch: %s", raw)
	}
	if check, _ := gateVerdict("ws", raw, func() (*relaySession, error) { return nil, errors.New("static check") }); check.Verdict != gatePass || check.Kind != "read" {
		t.Errorf("the gate would not pass this batch as a read (%+v): %s", check, raw)
	}
	log.batches.Add(1)
	if fakeRelayBatchFails.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]any{"code": "GATE_UNAVAILABLE", "message": "not_found is not this read's answer"}})
		return
	}
	failed := func(code, message string) any {
		return map[string]any{"ok": false, "error": map[string]any{"code": code, "message": message}}
	}
	var results []any
	for _, item := range items {
		p, _ := item.(map[string]any)
		itemRaw, _ := json.Marshal(p)
		log.mu.Lock()
		log.calls = append(log.calls, fakeRelayCall{endpoint: "ws", payload: p, raw: itemRaw})
		log.mu.Unlock()
		switch data := reply(r, "ws", p).(type) {
		case fakeRelayRefusal:
			results = append(results, failed(data.code, "refused"))
		case fakeRelayStatus:
			results = append(results, failed(data.code, data.message))
		case fakeRelayFailure:
			results = append(results, failed(data.code, data.message))
		default:
			results = append(results, map[string]any{"ok": true, "data": data})
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": map[string]any{"results": results}})
}
