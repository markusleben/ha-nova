package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The gate's verdict set is closed: only a tier below typed passes, and any
// tier the gate doesn't know is refused.
func TestGateDecisionOfFailsClosed(t *testing.T) {
	for level, want := range map[tierLevel]gateDecision{tierOrdinary: gatePass, tierPreview: gatePass, tierTyped: gateTyped, tierRefused: gateRefuse, tierRefused + 1: gateRefuse, -1: gateRefuse} {
		if got := gateDecisionOf(level); got != want {
			t.Errorf("gateDecisionOf(%d) = %q, want %q", level, got, want)
		}
	}
}

// The worker's /check answer is this JSON; the Relay refuses any other shape.
func TestGateCheckWireShape(t *testing.T) {
	got, _ := json.Marshal(gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete", Item: 1})
	if want := `{"verdict":"typed","kind":"write","why":"delete"}`; string(got) != want {
		t.Errorf("wire = %s, want %s", got, want)
	}
	got, _ = json.Marshal(gateCheck{Verdict: gatePass, Kind: "read"})
	if want := `{"verdict":"pass","kind":"read"}`; string(got) != want {
		t.Errorf("wire = %s, want %s", got, want)
	}
}

func TestGateVerdict(t *testing.T) {
	noSession := func() (*relaySession, error) { return nil, errors.New("no session") }
	for _, tc := range []struct {
		name, endpoint, body string
		want                 gateCheck
		wantErr              bool
	}{
		{"unknown endpoint", "events", `{"type":"get_states"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"not an object", "ws", `[{"type":"get_states"}]`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"null on core", "core", `null`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"null on ws", "ws", `null`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"trailing data", "ws", `{"type":"get_states"} {"type":"config/area_registry/delete","area_id":"office"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"two typed, the first named", "ws", `{"type":"config/area_registry/delete","area_id":"office","message":{"type":"config/floor_registry/delete","floor_id":"up"}}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"not JSON", "core", `GET /api/states`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"read", "ws", `{"type":"get_states"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"core read", "core", `{"method":"get","path":"/api/states"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"previewed write", "ws", `{"type":"config/area_registry/create","name":"Office"}`, gateCheck{Verdict: gatePass, Kind: "write"}, false},
		{"typed write", "ws", `{"type":"config/area_registry/delete","area_id":"office"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"typed in an envelope", "ws", `{"message":{"type":"config/area_registry/delete","area_id":"office"},"collect_events":{"max_events":1}}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"typed envelope body", "ws", `{"type":"config/area_registry/delete","area_id":"office","message":{"type":"get_states"}}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete", Item: 1}, false},
		{"core delete, method as sent", "core", `{"method":" delete ","path":"/api/config/scene/config/evening"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"file delete", "files", `{"action":"delete_file","path":"scripts.yaml"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "file_delete"}, false},
		{"file write", "files", `{"action":"write_file","path":"scripts.yaml","content":""}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "file_write"}, false},
		{"file read", "files", `{"action":"read_file","path":"/config/ha_nova/e2e-roundtrip.yaml"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"folder list", "files", `{"action":"list_dir","path":"packages"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"www media delete", "files", `{"action":"delete_file","path":"/config/www/door.jpg"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "file_delete"}, false},
		{"www media restore", "files", `{"action":"restore_file","path":"/config/www/door.jpg","copy":"0123456789abcdef0123456789abcdef"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "file_restore"}, false},
		{"kept copies list", "files", `{"action":"list_deleted"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"unknown files action", "files", `{"action":"move_file","path":"/config/www/door.jpg"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"copy list", "backups", `{"action":"list"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"copy load", "backups", `{"action":"load","file":"automation/a1.json"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"escaped config read", "core", `{"method":"GET","path":"/api/config/automation/config/morning%20lights"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"copy save", "backups", `{"action":"save","category":"yaml","name":"scripts","data":{}}`, gateCheck{Verdict: gatePass, Kind: "write"}, false},
		{"App install", "supervisor", `{"method":"POST","path":"/store/addons/core_ssh/install"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "app_install"}, false},
		{"Supervisor log read", "supervisor", `{"method":"GET","path":"/core/logs"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"error log read", "core", `{"method":"GET","path":"/api/error_log"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"system log read", "ws", `{"type":"system_log/list"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"event firing", "ws", `{"type":"fire_event","event_type":"mobile_app_notification_action"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"event listener counts", "core", `{"method":"GET","path":"/api/events"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"stored config read", "core", `{"method":"GET","path":"/api/config/automation/config/a1"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"event POST", "core", `{"method":"POST","path":"/api/events/mobile_app_notification_action","body":{}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"state write", "core", `{"method":"POST","path":"/api/states/lock.front","body":{"state":"unlocked"}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"escaped webhook", "core", `{"method":"GET","path":"/api/%77ebhook/abc"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"backup download", "core", `{"method":"GET","path":"/api/backup/download/abc"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"flow step GET is a write", "core", `{"method":"GET","path":"/api/config/config_entries/flow/r1"}`, gateCheck{Verdict: gatePass, Kind: "write"}, false},
		{"verb read", "ws", `{"type":"search/related","item_type":"entity","item_id":"light.hall"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"dashboard save", "ws", `{"type":"lovelace/config/save","config":{"views":[]}}`, gateCheck{Verdict: gatePass, Kind: "write"}, false},
		{"dashboard delete", "ws", `{"type":"lovelace/dashboards/delete","dashboard_id":"d1"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "delete"}, false},
		{"unknown WS type", "ws", `{"type":"config/auth/delete_all_refresh_tokens"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"WS service call", "ws", `{"type":"call_service","domain":"lock","service":"unlock","target":{"entity_id":"lock.front"}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"core service call, session down", "core", `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":["lock.front"]}}`, gateCheck{Verdict: gateTyped, Kind: "write"}, true},
		{"core service call, one entity_id as a string", "core", `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":"lock.front"}}`, gateCheck{Verdict: gateTyped, Kind: "write"}, true},
		{"core service call, a comma list", "core", `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":"lock.front, lock.back"}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"a number JavaScript makes Infinity", "ws", `{"type":"config/entity_registry/update","entity_id":"lock.front","hidden_by":1e999}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"a number JavaScript rounds to zero", "ws", `{"type":"config/entity_registry/update","entity_id":"lock.front","hidden_by":1e-999}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"a non-ASCII method", "core", `{"method":"poﬆ","path":"/api/services/light/turn_on","body":{"entity_id":["light.hall"]}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"a tab in a webhook path", "core", `{"method":"GET","path":"/api/webho	ok/abc"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"a hidden value carried back", "ws", `{"type":"config/area_registry/create","name":"(hidden: a stored code, password, or key)"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"a duplicate key", "ws", `{"type":"get_states","type":"get_states"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"an escaped duplicate key, nested", "core", `{"method":"POST","path":"/api/services/light/turn_on","body":{"entity_id":["light.hall"],"\u0065ntity_id":["lock.front"]}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"Link setup", "ws", `{"type":"cloud/status"}`, gateCheck{Verdict: gatePass, Kind: "write"}, false},
		{"Link setup with more", "ws", `{"type":"cloud/status","x":1}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"core service call with a code", "core", `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":["lock.front"],"code":"1234"}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"core service call, extra field", "core", `{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":["lock.front"]},"x":1}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"config write, session down", "core", `{"method":"POST","path":"/api/config/automation/config/x","body":{}}`, gateCheck{Verdict: gateTyped, Kind: "write"}, true},
		{"config delete", "core", `{"method":"DELETE","path":"/api/config/script/config/x"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: reasonConfigDelete}, false},
		{"webhook read", "core", `{"method":"GET","path":"/api/webhook/abc"}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"phone event subscription", "ws", `{"message":{"type":"subscribe_events","event_type":"mobile_app_notification_action"},"collect_events":{"max_events":1}}`, gateCheck{Verdict: gateRefuse, Kind: "write"}, false},
		{"Supervisor read", "supervisor", `{"method":"GET","path":"/addons"}`, gateCheck{Verdict: gatePass, Kind: "read"}, false},
		{"unread access entity", "ws", `{"type":"config/entity_registry/update","entity_id":"lock.front","area_id":"hall"}`, gateCheck{Verdict: gateTyped, Kind: "write", Why: "access_entity"}, true},
	} {
		got, err := gateVerdict(tc.endpoint, []byte(tc.body), noSession)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("%s: gateVerdict = %+v, %v; want %+v, err %v", tc.name, got, err, tc.want, tc.wantErr)
		}
	}
}

// assertGateParity is the parity oracle (spec → Contract): the gate rates
// the request a stored call plan sends exactly as a plan of that same
// request (its frozen leaves and data) rates it. A typed plan may also be
// refused (stricter), and the gate never refuses a plan that doesn't ask.
// The gate sees only the frozen leaves, so a plan of the target expression
// (an expansion cut short, a group's own consumers) may ask where the frozen
// request alone doesn't; that plan is then re-run on the frozen request.
func assertGateParity(t *testing.T, paths runtimePaths, env verbTestEnvelope) {
	t.Helper()
	if env.Plan == nil || env.Plan.ID == "" {
		return
	}
	plan, err := readPlanFile(filepath.Join(planDir(paths), env.Plan.ID+".json"))
	if err != nil {
		t.Fatalf("stored plan: %v", err)
	}
	body := map[string]any{}
	for key, value := range plan.Action.Data {
		body[key] = value
	}
	if len(plan.Action.EntityIDs) > 0 {
		body["entity_id"] = slices.Clone(plan.Action.EntityIDs)
	}
	domain, name := splitID(plan.Action.Service)
	path := "/api/services/" + domain + "/" + name
	if plan.Action.Response {
		path += "?return_response"
	}
	sent, _ := json.Marshal(map[string]any{"method": "POST", "path": path, "body": body})
	var s *relaySession
	defer func() {
		if s != nil {
			s.close()
		}
	}()
	check, err := gateVerdict("core", sent, func() (*relaySession, error) {
		opened, err := newRelaySession(paths, verbOptions{})
		s = opened
		return opened, err
	})
	typed := plan.Tier == "typed"
	switch {
	case err != nil, check.Verdict == gateRefuse && !typed, check.Verdict == gateTyped && !typed:
	case check.Verdict == gatePass && typed:
		data := filepath.Join(t.TempDir(), "data.json")
		raw, _ := json.Marshal(plan.Action.Data)
		if err := os.WriteFile(data, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		args := append([]string{"call", plan.Action.Service}, plan.Action.EntityIDs...)
		if plan.Action.Response {
			args = append(args, "--response")
		}
		if skill := plan.Request.Skill; skill != "" {
			args = append(args, "--skill", skill)
		}
		exit, frozen := runVerbForTest(t, func() int { return runPlanCommand(paths, append(args, "--data-file", data)) })
		stored := frozen.Plan != nil && frozen.Plan.ID != ""
		if stored && (exit == verbExitOK && frozen.Tier == "ordinary" || exit == verbExitConfirmation && frozen.Tier == "preview") {
			return
		}
		t.Fatalf("gate parity: plan %s is typed, the gate passes it, and its frozen request plans as exit %d tier %q (stored %v) for %s", plan.ID, exit, frozen.Tier, stored, sent)
	default:
		return
	}
	t.Fatalf("gate parity: plan %s tier %s (reasons %s), gate %+v err %v for %s", plan.ID, plan.Tier, reasonCodes(env), check, err, sent)
}

// The gate rates an automation or script write as the plan of that draft
// does: typed exactly when the plan is typed.
func TestGateConfigWriteParity(t *testing.T) {
	unlock := hallDraft("light.turn_on")
	unlock["actions"] = []any{step("lock.unlock", "lock.front")}
	for _, tc := range []struct {
		name, domain string
		draft        map[string]any
		tier         string
	}{
		{"light", "automation", hallDraft("light.turn_on"), "preview"},
		{"unlock", "automation", unlock, "typed"},
		{"script that unlocks", "script", map[string]any{"alias": "Leave", "sequence": []any{step("lock.unlock", "lock.front")}}, "typed"},
		{"script that dims", "script", map[string]any{"alias": "Dim", "sequence": []any{step("light.turn_on", "light.hall")}}, "preview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newWriteHarness(t, newFakeHA())
			_, env := h.run(runPlanCommand, "write", "--domain", tc.domain, "--file", draftFile(t, tc.draft))
			if env.Tier != tc.tier || env.Config == nil {
				t.Fatalf("plan: tier %q env %+v, want %q", env.Tier, env, tc.tier)
			}
			sent, _ := json.Marshal(map[string]any{"method": "POST", "path": env.Config.Path, "body": env.Config.Body})
			var s *relaySession
			defer func() {
				if s != nil {
					s.close()
				}
			}()
			check, err := gateVerdict("core", sent, func() (*relaySession, error) {
				opened, err := newRelaySession(h.paths, verbOptions{})
				s = opened
				return opened, err
			})
			if err != nil || check.Kind != "write" || (check.Verdict == gateTyped) != (tc.tier == "typed") || check.Verdict == gateRefuse {
				t.Fatalf("gate %+v err %v for a %s plan of %s", check, err, tc.tier, sent)
			}
		})
	}
}

// gateRefusesOnPurpose: what a verb sends that a gated App refuses by
// design: the CLI's own phone ask's listener (the App asks itself, spec §3)
// and log reads (free text no redactor bounds, spec §2).
func gateRefusesOnPurpose(endpoint string, payload map[string]any) bool {
	message, _ := payload["message"].(map[string]any)
	return endpoint == "ws" && (message["type"] == "subscribe_events" && message["event_type"] == phoneEventType || payload["type"] == "system_log/list") ||
		endpoint == "core" && payload["method"] == "GET" && payload["path"] == "/api/error_log"
}

// The gate runs the live checks a raw write's plan runs: an access target
// is refused, and a write an automation listens to is typed exactly when a
// plan of it is (spec → Contract, parity).
func TestGateLiveRawChecks(t *testing.T) {
	for _, tc := range listenedWrites {
		for _, listening := range []bool{true, false} {
			f := listeningHome()
			if listening {
				f.automation("opener", "opener", map[string]any{"triggers": []any{tc.trigger}, "actions": []any{step("lock.unlock", "lock.front")}})
				f.related[tc.related] = []string{"automation.opener"}
			}
			paths, _, sent := listeningRelay(t, f)
			check, err := gateVerdict(cmp.Or(tc.endpoint, "ws"), []byte(tc.payload), func() (*relaySession, error) { return newRelaySession(paths, verbOptions{}) })
			want := map[bool]gateDecision{true: gateTyped, false: gatePass}[listening]
			if err != nil || check.Verdict != want || sent() != 0 {
				t.Errorf("%s (listening %v): %+v %v, want %s", tc.name, listening, check, err, want)
			}
		}
	}
	f := listeningHome()
	f.full = append(f.full, map[string]any{"entity_id": "button.node_7", "platform": "zwave_js", "unique_id": "n7", "original_name": "Factory reset"})
	paths, _, _ := listeningRelay(t, f)
	for _, body := range []string{
		`{"type":"config/entity_registry/update","entity_id":"button.node_7","name":"Tidy"}`,
		`{"type":"config/entity_registry/update","entity_id":"button.node_7","new_entity_id":"button.tidy"}`,
	} {
		if check, err := gateVerdict("ws", []byte(body), func() (*relaySession, error) { return newRelaySession(paths, verbOptions{}) }); err != nil || check.Verdict != gateRefuse {
			t.Errorf("%s: %+v %v, want refused", body, check, err)
		}
	}
}

// A scene closes a cover only without a position or at 0: Home Assistant
// coerces a string position, so "50" opens a garage door halfway.
func TestSceneCoverPositionDecidesTheService(t *testing.T) {
	for _, member := range []any{"closed", map[string]any{"state": "closed"}, map[string]any{"state": "closed", "current_position": float64(0)}} {
		if service, _ := sceneMemberService("cover.garage", member); service != "cover.close_cover" {
			t.Errorf("%v: %s, want close_cover", member, service)
		}
	}
	for _, position := range []any{float64(50), "50", json.Number("50"), "0", nil} {
		if service, _ := sceneMemberService("cover.garage", map[string]any{"state": "closed", "current_position": position}); service != "cover.set_cover_position" {
			t.Errorf("position %#v: %s, want set_cover_position", position, service)
		}
	}
}

// `ha-nova relay` sends nothing the gate refuses, its own guard passed or
// not (a file delete with a number Home Assistant would get as null).
func TestRelayRefusesWhatTheGateRefuses(t *testing.T) {
	paths, calls := readRelay(t, func(string, map[string]any) any { return nil })
	for _, args := range [][]string{
		{"files", "--data", `{"action":"delete_file","path":"scripts.yaml","ignored":1e1000}`},
		{"files", "--data", `{"action":"write_file","path":"scripts.yaml","content":"","n":1e-999}`},
		{"ws", "--data", `{"type":"config/area_registry/create","name":"Hall","n":1e999}`},
	} {
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, args) })
		if exit != verbExitRefused || !strings.Contains(stderr, "nothing was sent") {
			t.Errorf("%v: exit %d, stderr %q; want refused", args, exit, stderr)
		}
	}
	for _, c := range calls() {
		if c.endpoint == "files" || !rawRelayRead(c.endpoint, []byte(c.body)) {
			t.Errorf("sent %s %s", c.endpoint, c.body)
		}
	}
}

// A GET on a flow step re-runs it (Home Assistant's FlowManagerResourceView
// calls async_configure), so the gate rates it as that step's POST: typed for
// a repair or subentry flow, as the step itself for any other flow.
func TestGateFlowStepGETRatedAsItsPOST(t *testing.T) {
	noSession := func() (*relaySession, error) { return nil, errors.New("no session") }
	for path, want := range map[string]gateDecision{
		"/api/repairs/issues/fix/f1":                    gateTyped,
		"/api/config/config_entries/subentries/flow/f1": gateTyped,
		"/api/config/config_entries/options/flow/f1":    gatePass,
		"/api/config/config_entries/flow/f1":            gatePass,
	} {
		post, err := gateVerdict("core", mustJSON(map[string]any{"method": "POST", "path": path, "body": map[string]any{}}), noSession)
		if err != nil || post.Verdict != want {
			t.Errorf("POST %s: %+v %v, want %s", path, post, err, want)
		}
		for _, get := range [][2]string{{"GET", path}, {" get", path}, {"GET", path + "?x=1"}, {"GET", strings.TrimSuffix(path, "f1") + "%66%31"}} {
			check, err := gateVerdict("core", mustJSON(map[string]any{"method": get[0], "path": get[1]}), noSession)
			if err != nil || check.Verdict != want || check.Kind != "write" {
				t.Errorf("%q %s: %+v %v, want %s write", get[0], get[1], check, err, want)
			}
		}
	}
}
