package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// unregisteredLockHA: lock.front_door has a state but no registry row (YAML,
// template, demo); lock.entrance is registered with the has_entity_name
// fallback "Lock" while its state says "Entrance Door Lock".
func unregisteredLockHA() *fakeHA {
	f := newFakeHA()
	f.ent("lock.front_door", "", "locked", map[string]any{"friendly_name": "Front Door"})
	f.ent("lock.entrance", "zwave_js", "locked", map[string]any{"friendly_name": "Lock"})
	f.states["lock.entrance"]["attributes"] = map[string]any{"friendly_name": "Entrance Door Lock"}
	f.full = []map[string]any{
		{"entity_id": "lock.entrance", "name": nil, "original_name": "Lock"},
		{"entity_id": "lock.front", "name": nil, "original_name": "Front"},
	}
	return f
}

func findResultOf(t *testing.T, env verbTestEnvelope) findResult {
	t.Helper()
	var result findResult
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatalf("result: %v (%+v)", err, env)
	}
	return result
}

func TestFindMatchesUnregisteredEntityByFriendlyName(t *testing.T) {
	f := unregisteredLockHA()
	paths, log := setupVerbRelayTest(t, f.reply)
	for _, args := range [][]string{{"front door", "--domain", "lock"}, {"front door", "--domain", "lock", "--aliases"}, {"FRONT DÖOR"}} {
		exit, env := runVerbForTest(t, func() int { return runFindCommand(paths, args) })
		result := findResultOf(t, env)
		if exit != 0 || result.Total != 1 || result.Matches[0].EntityID != "lock.front_door" || result.Matches[0].By != "state" || result.Matches[0].Name != "Front Door" {
			t.Fatalf("find %v: exit %d, result %+v", args, exit, result)
		}
	}
	// --area cannot prove an area for an entity without a registry row.
	if exit, _ := runVerbForTest(t, func() int { return runFindCommand(paths, []string{"front door", "--area", "office"}) }); exit != verbExitNotFound {
		t.Fatalf("--area included an unregistered entity: exit %d", exit)
	}
	for _, p := range log.corePaths() {
		if p != "/api/states" {
			t.Fatalf("per-entity read %s: want only the one /api/states listing", p)
		}
	}
}

func TestFindShowsFriendlyNameOverRegistryFallback(t *testing.T) {
	f := unregisteredLockHA()
	paths, _ := setupVerbRelayTest(t, f.reply)
	// The registry row says "Lock" (original_name fallback); the row shows
	// the state's full friendly name.
	_, env := runVerbForTest(t, func() int { return runFindCommand(paths, []string{"entrance"}) })
	if result := findResultOf(t, env); result.Total != 1 || result.Matches[0].Name != "Entrance Door Lock" || result.Matches[0].By != "name" {
		t.Fatalf("display name: %+v", result)
	}
	// A plain domain listing: registered and unregistered rows, sorted by
	// entity_id, total exact past the limit.
	_, env = runVerbForTest(t, func() int { return runFindCommand(paths, []string{"--domain", "lock", "--limit", "2"}) })
	result := findResultOf(t, env)
	var ids []string
	for _, m := range result.Matches {
		ids = append(ids, m.EntityID)
	}
	if result.Total != 3 || result.Returned != 2 || strings.Join(ids, ",") != "lock.entrance,lock.front" {
		t.Fatalf("listing: total %d returned %d ids %v", result.Total, result.Returned, ids)
	}
}

func TestCallMatchResolvesUnregisteredEntityAndStaysTyped(t *testing.T) {
	h := newWriteHarness(t, unregisteredLockHA())
	exit, env := h.run(runCallCommand, "lock.unlock", "--match", "front door")
	if exit != verbExitConfirmation || env.Status != "needs_confirmation" || env.Plan == nil || env.Plan.Code == "" || len(h.posts()) != 0 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
	if len(env.Resolved) != 1 || env.Resolved[0].EntityID != "lock.front_door" || env.Resolved[0].By != "match" {
		t.Fatalf("resolved %+v", env.Resolved)
	}
	if !strings.Contains(reasonCodes(env), reasonUnregisteredActuator) {
		t.Fatalf("reasons %s: an unregistered lock must stay %s", reasonCodes(env), reasonUnregisteredActuator)
	}
	// "front" now names lock.front and lock.front_door: ambiguous, never a pick.
	exit, env = h.run(runCallCommand, "lock.unlock", "--match", "front")
	if exit != verbExitNotFound || env.Error == nil || env.Error.Code != "AMBIGUOUS_TARGET" || len(env.Error.Candidates) != 2 {
		t.Fatalf("ambiguous --match: exit %d env %+v", exit, env.Error)
	}
}

func TestGetCandidatesIncludeUnregisteredEntity(t *testing.T) {
	paths, _ := setupVerbRelayTest(t, unregisteredLockHA().reply)
	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"lock.front_do"}) })
	if exit != verbExitNotFound || env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	if len(env.Error.Candidates) != 1 || env.Error.Candidates[0].EntityID != "lock.front_door" {
		t.Fatalf("candidates %+v", env.Error.Candidates)
	}
}

func TestMCPFindMatchesUnregisteredEntity(t *testing.T) {
	h := newWriteHarness(t, unregisteredLockHA())
	c := startMCP(t, h.paths)
	mcpInit(c)
	content := c.tool("ha_find", map[string]any{"phrase": "front door", "domain": "lock"})["structuredContent"].(map[string]any)
	result, _ := content["result"].(map[string]any)
	matches, _ := result["matches"].([]any)
	if content["status"] != "ok" || len(matches) != 1 || matches[0].(map[string]any)["entity_id"] != "lock.front_door" {
		t.Fatalf("ha_find: %v", content)
	}
}
