package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// haReply answers states, configs, the registry get, history, and logbook.
func haReply(states map[string]any, uniqueIDs map[string]string, history []any) func(*http.Request, string, map[string]any) any {
	return func(_ *http.Request, endpoint string, payload map[string]any) any {
		if endpoint == "ws" {
			switch payload["type"] {
			case "config/entity_registry/get":
				return map[string]any{"entity_id": payload["entity_id"], "unique_id": uniqueIDs[fmt.Sprint(payload["entity_id"])]}
			case "config/entity_registry/list_for_display":
				return map[string]any{"entities": []map[string]any{{"ei": "light.kitchen_ceiling", "en": "Kitchen ceiling"}, {"ei": "switch.kitchen", "en": "Kitchen"}}}
			case "search/related":
				return map[string]any{"automation": []string{"automation.night"}}
			}
			return []any{}
		}
		path := fmt.Sprint(payload["path"])
		switch {
		case strings.HasPrefix(path, "/api/states/"):
			if state, ok := states[strings.TrimPrefix(path, "/api/states/")]; ok {
				return coreReply(200, state)
			}
			return coreReply(404, map[string]any{"message": "Entity not found."})
		case strings.HasPrefix(path, "/api/config/"):
			return coreReply(200, map[string]any{"alias": "stored", "path": path})
		case strings.HasPrefix(path, "/api/history/period/"):
			return coreReply(200, []any{history})
		case strings.HasPrefix(path, "/api/logbook/"):
			return coreReply(200, []any{map[string]any{"message": "turned on"}})
		}
		return coreReply(404, nil)
	}
}

func TestGetConfigResolvesConfigIDNeverBySlug(t *testing.T) {
	states := map[string]any{
		"automation.kitchen_lights": map[string]any{"state": "on", "attributes": map[string]any{"id": "1700000000001", "friendly_name": "Kitchen lights"}},
		"script.night":              map[string]any{"state": "off", "attributes": map[string]any{}},
		"automation.yaml_only":      map[string]any{"state": "on", "attributes": map[string]any{}},
	}
	paths, log := setupVerbRelayTest(t, haReply(states, map[string]string{"script.night": "night_routine_v2"}, nil))

	exit, env := runVerbForTest(t, func() int {
		return runGetCommand(paths, []string{"automation.kitchen_lights", "--config", "--related"})
	})
	var result getResult
	_ = json.Unmarshal(env.Result, &result)
	if exit != 0 || result.ConfigID != "1700000000001" || len(result.Related) == 0 || env.Resolved[0].Name != "Kitchen lights" {
		t.Fatalf("automation: exit %d, env %+v, result %+v", exit, env, result)
	}
	exit, env = runVerbForTest(t, func() int { return runGetCommand(paths, []string{"--config", "script.night"}) })
	_ = json.Unmarshal(env.Result, &result)
	if exit != 0 || result.ConfigID != "night_routine_v2" {
		t.Fatalf("script: exit %d, result %+v", exit, result)
	}
	exit, env = runVerbForTest(t, func() int { return runGetCommand(paths, []string{"automation.yaml_only", "--config"}) })
	if exit != verbExitNotFound || env.Error == nil || env.Error.Code != "CONFIG_NOT_FOUND" {
		t.Fatalf("id-less automation: exit %d, env %+v", exit, env)
	}

	got := strings.Join(log.corePaths(), "\n")
	for _, want := range []string{"/api/config/automation/config/1700000000001", "/api/config/script/config/night_routine_v2"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing config read %s in:\n%s", want, got)
		}
	}
	for _, slug := range []string{"/config/kitchen_lights", "/config/night", "/config/yaml_only"} {
		if strings.Contains(got+"\n", slug+"\n") {
			t.Fatalf("config read by slug %s:\n%s", slug, got)
		}
	}
}

func TestGetMissingEntityExitsFiveWithCandidates(t *testing.T) {
	paths, _ := setupVerbRelayTest(t, haReply(map[string]any{}, nil, nil))

	exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, []string{"light.kitchen"}) })
	if exit != verbExitNotFound || env.Error == nil || env.Error.Code != "NOT_FOUND" {
		t.Fatalf("exit %d, env %+v", exit, env)
	}
	if len(env.Error.Candidates) != 1 || env.Error.Candidates[0].EntityID != "light.kitchen_ceiling" {
		t.Fatalf("candidates = %+v, want only light.kitchen_ceiling (same domain)", env.Error.Candidates)
	}
}

func TestGetRefusesBadInputBeforeSending(t *testing.T) {
	paths, log := setupVerbRelayTest(t, haReply(nil, nil, nil))
	for _, args := range [][]string{{"light.kitchen", "--config"}, {"../api/services"}, {}, {"a.b", "c.d"}} {
		exit, env := runVerbForTest(t, func() int { return runGetCommand(paths, args) })
		if exit != verbExitError || env.Error == nil || env.Error.Code != "USAGE" {
			t.Fatalf("get %v: exit %d, env %+v", args, exit, env)
		}
	}
	if calls := log.all(); len(calls) != 0 {
		t.Fatalf("usage errors reached the Relay: %+v", calls)
	}
}

func TestHistorySummarizesBoundedWindow(t *testing.T) {
	previous := verbNow
	verbNow = func() time.Time { return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { verbNow = previous })
	var rows []any
	for i := 0; i < 250; i++ {
		state := "off"
		if i/2%2 == 1 { // off off on on off off ...
			state = "on"
		}
		rows = append(rows, map[string]any{"state": state, "last_changed": fmt.Sprintf("t%03d", i)})
	}
	states := map[string]any{"light.desk": map[string]any{"state": "off", "attributes": map[string]any{}}}
	paths, log := setupVerbRelayTest(t, haReply(states, nil, rows))

	exit, env := runVerbForTest(t, func() int { return runHistoryCommand(paths, []string{"light.desk", "--since", "2d", "--logbook"}) })
	var result historyResult
	_ = json.Unmarshal(env.Result, &result)
	h := result.History
	if exit != 0 || h.Total != 250 || h.Returned != historyMaxRows || h.Changes != 124 {
		t.Fatalf("exit %d, total/returned/changes = %d/%d/%d, want 250/200/124", exit, h.Total, h.Returned, h.Changes)
	}
	if h.First.At != "t000" || h.Last.At != "t249" || h.Rows[0].At != "t050" {
		t.Fatalf("first/last/rows[0] = %s/%s/%s", h.First.At, h.Last.At, h.Rows[0].At)
	}
	if result.Logbook == nil || result.Logbook.Total != 1 || result.Since != "2026-10-12T12:00:00Z" {
		t.Fatalf("logbook/since: %+v %s", result.Logbook, result.Since)
	}
	wantPath := "/api/history/period/2026-10-12T12:00:00Z?end_time=2026-10-14T12%3A00%3A00Z&filter_entity_id=light.desk&minimal_response&no_attributes"
	if !strings.Contains(strings.Join(log.corePaths(), "\n"), wantPath) {
		t.Fatalf("history path missing %s in %v", wantPath, log.corePaths())
	}
}

func TestHistoryRefusesWindowsOverSevenDays(t *testing.T) {
	paths, log := setupVerbRelayTest(t, haReply(nil, nil, nil))
	for _, since := range []string{"8d", "169h", "0h", "-1h", "soon"} {
		exit, env := runVerbForTest(t, func() int { return runHistoryCommand(paths, []string{"light.desk", "--since", since}) })
		if exit != verbExitError || env.Error == nil || env.Error.Code != "USAGE" {
			t.Fatalf("--since %s: exit %d, env %+v", since, exit, env)
		}
	}
	if calls := log.all(); len(calls) != 0 {
		t.Fatalf("refused windows reached the Relay: %+v", calls)
	}
}

func TestUsageListsReadVerbs(t *testing.T) {
	out := captureStdout(t, printUsage)
	for _, usage := range []string{findUsage, getUsage, historyUsage} {
		if !strings.Contains(out, usage) {
			t.Fatalf("usage misses %q:\n%s", usage, out)
		}
	}
}
