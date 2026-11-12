package main

import (
	"net/http"
	"slices"
	"testing"
	"time"
)

type afterResult struct {
	Baseline       map[string]any   `json:"baseline"`
	Updated        []map[string]any `json:"updated"`
	Rollback       any              `json:"rollback"`
	NewFailing     []map[string]any `json:"new_failing_integrations"`
	NewRepairs     []string         `json:"new_repairs"`
	NewlyUnavail   map[string]any   `json:"newly_unavailable"`
	AvailableAgain int              `json:"available_again"`
	AutomationErrs []map[string]any `json:"automation_errors"`
	TracesRead     int              `json:"traces_read"`
	NotChecked     []map[string]any `json:"not_checked"`
	StillPending   []updateRow      `json:"still_pending"`
}

// runSections runs map sections against one fixture and one HOME, so the
// baseline survives between them.
func runSections(t *testing.T, f *updatesFixture) func(section string) verbTestEnvelope {
	t.Helper()
	releaseNotesServer(t, nil)
	paths, _ := setupVerbRelayTest(t, func(r *http.Request, endpoint string, payload map[string]any) any {
		return f.reply(r, endpoint, payload)
	})
	return func(section string) verbTestEnvelope {
		exit, env := runVerbForTest(t, func() int { return runMapCommand(paths, []string{"--section", section}) })
		if exit != 0 {
			t.Fatalf("%s exit %d: %+v", section, exit, env)
		}
		return env
	}
}

func resolutionInfo(issues ...map[string]any) map[string]any {
	return map[string]any{"unhealthy": []string{}, "issues": append([]map[string]any{}, issues...)}
}

func TestAfterUpdateComparesWithTheCheck(t *testing.T) {
	f := haosHome()
	old := map[string]any{"uuid": "u1", "type": "update_rollback", "context": "core", "reference": "2026.9.0"}
	f.supervisor["/resolution/info"] = resolutionInfo(old)
	f.states = append(f.states, st("sensor.was_down", "unavailable", nil), st("sensor.ok", "21", nil),
		st("automation.party", "on", map[string]any{"friendly_name": "Party mode", "id": "1001"}))
	f.repairs = []map[string]any{{"domain": "zha", "issue_id": "old"}}
	f.traces = []map[string]any{{"item_id": "1001", "error": "old error", "timestamp": map[string]any{"start": ago(time.Hour)}}}
	run := runSections(t, &f)
	check := decodeInto[map[string]any](t, run("updates").Result)
	if b := check["baseline"].(map[string]any); b["state"] != "saved" {
		t.Fatalf("baseline %+v", b)
	}
	if b := decodeInto[map[string]any](t, run("updates").Result)["baseline"].(map[string]any); b["state"] != "kept" {
		t.Fatalf("second check replaced the baseline: %+v", b)
	}

	// The update: Core rolled back to 2026.10.2, Mosquitto updated, Z-Wave
	// fails, one sensor went down and one came back, an automation errors.
	f.states[0] = upd("update.home_assistant_core_update", "2026.10.2", "2026.11.1", map[string]any{"friendly_name": "Home Assistant Core"})
	f.states[1] = st("update.mosquitto_update", "off", map[string]any{"installed_version": "6.5.0", "latest_version": "6.5.0"})
	f.supervisor["/resolution/info"] = resolutionInfo(old,
		map[string]any{"uuid": "u2", "type": "update_rollback", "context": "core", "reference": "2026.11.1"},
		map[string]any{"uuid": "u3", "type": "update_failed", "context": "addon", "reference": "core_mosquitto"})
	f.entries[0]["state"] = "setup_error"
	f.repairs = append(f.repairs, map[string]any{"domain": "zwave_js", "issue_id": "new"})
	f.states[len(f.states)-3] = st("sensor.was_down", "on", nil)
	f.states[len(f.states)-2] = st("sensor.ok", "unavailable", map[string]any{"friendly_name": "Hall temperature"})
	f.traces = append(f.traces, map[string]any{"item_id": "1001", "error": "Error: <b>boom</b>", "timestamp": map[string]any{"start": time.Now().UTC().Format(time.RFC3339Nano)}})
	got := decodeInto[afterResult](t, run("after_update").Result)
	if got.Baseline == nil || len(got.NotChecked) != 0 {
		t.Fatalf("baseline %+v gaps %+v", got.Baseline, got.NotChecked)
	}
	if len(got.Updated) != 1 || got.Updated[0]["entity_id"] != "update.mosquitto_update" || got.Updated[0]["reached"] != true {
		t.Fatalf("updated %+v", got.Updated)
	}
	rb := got.Rollback.(map[string]any)
	if !slices.Equal(rb["rolled_back"].([]any), []any{"2026.11.1"}) || !slices.Equal(rb["failed"].([]any), []any{"addon"}) {
		t.Fatalf("rollback %+v", rb)
	}
	if len(got.NewFailing) != 1 || got.NewFailing[0]["integration"] != "zwave_js" || !slices.Equal(got.NewRepairs, []string{"zwave_js/new"}) {
		t.Fatalf("failing %+v repairs %v", got.NewFailing, got.NewRepairs)
	}
	if got.NewlyUnavail["count"] != float64(1) || got.NewlyUnavail["names"].([]any)[0] != "Hall temperature" || got.AvailableAgain != 1 {
		t.Fatalf("unavailable %+v back %d", got.NewlyUnavail, got.AvailableAgain)
	}
	if len(got.AutomationErrs) != 1 || got.AutomationErrs[0]["name"] != "Party mode" || got.AutomationErrs[0]["errors"] != float64(1) ||
		got.AutomationErrs[0]["last_error"] != "Error: (b)boom(/b)" || got.TracesRead != 2 {
		t.Fatalf("automation errors %+v", got.AutomationErrs)
	}
}

func TestAfterUpdateWithoutCheckOrSupervisorSaysSo(t *testing.T) {
	f := haosHome()
	f.supervisor["/resolution/info"] = resolutionInfo()
	run := runSections(t, &f)
	got := decodeInto[afterResult](t, run("after_update").Result)
	if got.Baseline != nil || !slices.Equal(codes(got.NotChecked), []string{"since_check"}) || len(got.StillPending) != 6 {
		t.Fatalf("no baseline %+v", got)
	}
	if rb, ok := got.Rollback.(map[string]any); !ok || len(rb["rolled_back"].([]any)) != 0 {
		t.Fatalf("rollback %+v", got.Rollback)
	}
	delete(f.supervisor, "/resolution/info")
	f.coreState = "STARTING"
	got = decodeInto[afterResult](t, run("after_update").Result)
	if got.Rollback != "unknown" || !slices.Contains(codes(got.NotChecked), "rollback") || !slices.Contains(codes(got.NotChecked), "started") {
		t.Fatalf("unreadable Supervisor %+v %+v", got.Rollback, got.NotChecked)
	}
}

func TestAfterUpdateIgnoresAnotherServersBaseline(t *testing.T) {
	file := t.TempDir() + "/b.json"
	if err := writeFileAtomic(file, []byte(`{"server":"other","taken_at":"2026-11-12T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if loadBaseline(file, "default") != nil || loadBaseline(file, "other") == nil {
		t.Fatal("baseline server binding")
	}
}
