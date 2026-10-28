package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The Supervisor endpoint: the guard mirrors the Relay's table exactly,
// relay and ha_read only read, and a plan tiers every App change.

func supervisorBody(method, path string, extra map[string]any) string {
	body := map[string]any{"method": method, "path": path}
	for k, v := range extra {
		body[k] = v
	}
	out, _ := json.Marshal(body)
	return string(out)
}

func TestSupervisorGuardTable(t *testing.T) {
	allowed := []string{}
	for _, route := range rawSupervisorRoutes {
		path := strings.NewReplacer("{slug}", "core_mosquitto", "{identifier}", "kernel").Replace(route.path)
		var extra map[string]any
		switch route.kind {
		case "options":
			extra = map[string]any{"data": map[string]any{"auto_update": true}}
		case "logs":
			extra = map[string]any{"lines": 50}
		}
		allowed = append(allowed, supervisorBody(route.method, path, extra))
	}
	allowed = append(allowed, supervisorBody("POST", "/addons/a0d7b954_vscode/options", map[string]any{"data": map[string]any{"options": map[string]any{"log_level": "debug"}}}),
		supervisorBody("POST", "/addons/core_mosquitto/uninstall", map[string]any{"data": map[string]any{"remove_config": false}}))
	for _, body := range allowed {
		if _, refusal, err := guardRaw("supervisor", []byte(body), nil); refusal != nil || err != nil {
			t.Errorf("%s: refused %v %v", body, refusal, err)
		}
	}
	refused := [][2]string{
		{"POST", "/dns/options"}, {"GET", "/dns/info"}, {"POST", "/security/options"}, {"POST", "/supervisor/options"},
		{"POST", "/supervisor/update"}, {"GET", "/auth/list"}, {"GET", "/backups"}, {"POST", "/backups/x/restore/full"},
		{"POST", "/addons/core_mosquitto/stdin"}, {"POST", "/addons/core_mosquitto/security"}, {"POST", "/addons/core_mosquitto/start"},
		{"POST", "/addons/core_mosquitto/update"}, {"POST", "/addons/core_mosquitto/sys_options"}, {"POST", "/host/reboot"},
		{"POST", "/host/shutdown"}, {"POST", "/os/update"}, {"POST", "/network/interface/eth0/update"}, {"GET", "/mounts"},
		{"GET", "/docker/info"}, {"GET", "/jobs/info"}, {"POST", "/store/repositories"}, {"POST", "/refresh_updates"},
		{"POST", "/core/restart"}, {"GET", "/core/api/states"}, {"GET", "/addons/self/info"}, {"GET", "/addons/core_mosquitto/logs/follow"},
		{"GET", "/v2/apps"}, {"GET", "/info/"}, {"GET", "/info?x=1"}, {"GET", "//info"}, {"GET", "/addons/../dns/info"},
		{"GET", "/addons/%2e%2e/info"}, {"POST", "/info"}, {"PUT", "/info"},
		{"POST", "/addons/2368fcfa_ha_nova_relay/options"}, {"POST", "/addons/local_ha_nova_relay_b4/uninstall"}, {"POST", "/store/addons/2368fcfa_ha_nova_relay/install"},
	}
	for _, r := range refused {
		var extra map[string]any
		if strings.HasSuffix(r[1], "/options") {
			extra = map[string]any{"data": map[string]any{"auto_update": false}}
		}
		if _, refusal, _ := guardRaw("supervisor", []byte(supervisorBody(r[0], r[1], extra)), nil); refusal == nil {
			t.Errorf("%s %s: not refused", r[0], r[1])
		}
	}
	for _, body := range []string{
		supervisorBody("GET", "/info", map[string]any{"data": map[string]any{}}),
		supervisorBody("GET", "/core/logs", map[string]any{"lines": 0}),
		supervisorBody("GET", "/core/logs", map[string]any{"lines": 1001}),
		supervisorBody("GET", "/core/logs", map[string]any{"lines": "10"}),
		supervisorBody("GET", "/info", map[string]any{"extra": 1}),
		supervisorBody("POST", "/store/addons/core_mosquitto/install", map[string]any{"data": map[string]any{"background": false}}),
		supervisorBody("POST", "/addons/core_mosquitto/uninstall", map[string]any{"data": map[string]any{"remove_config": "yes"}}),
		supervisorBody("POST", "/addons/core_mosquitto/options", map[string]any{"data": map[string]any{"boot": "manual"}}),
		supervisorBody("POST", "/addons/core_mosquitto/options", map[string]any{"data": map[string]any{"options": map[string]any{}, "auto_update": true}}),
		supervisorBody("POST", "/addons/core_mosquitto/options", map[string]any{"data": map[string]any{"options": map[string]any{"password": secretHidden}}}),
		"[1]",
	} {
		if _, refusal, _ := guardRaw("supervisor", []byte(body), nil); refusal == nil {
			t.Errorf("%s: not refused", body)
		}
	}
}

// relay and ha_read send Supervisor reads only; a write is planned.
func TestRelaySupervisorSendsReadsOnly(t *testing.T) {
	paths, calls := readRelay(t, nil)
	write := supervisorBody("POST", "/addons/core_mosquitto/uninstall", nil)
	if _, refusal, err := guardRawRequest(paths, verbOptions{}, "supervisor", []byte(write)); refusal == nil || err != nil || !strings.Contains(refusal.Error(), "ha-nova plan raw") {
		t.Fatalf("write: %v %v", refusal, err)
	}
	if exit := runRelayCommand(paths, []string{"supervisor", "-d", write}); exit != rawRefusedExit || len(calls()) != 0 {
		t.Fatalf("relay supervisor write: exit %d, sent %v", exit, calls())
	}
	if exit := runRelayCommand(paths, []string{"supervisor", "-d", supervisorBody("GET", "/addons", nil)}); exit != 0 || len(calls()) != 1 || calls()[0].endpoint != "supervisor" {
		t.Fatalf("relay supervisor read: exit %d, sent %v", exit, calls())
	}
	if rawRelayRead("supervisor", []byte(write)) || !rawRelayRead("supervisor", []byte(supervisorBody("GET", "/core/logs", nil))) {
		t.Fatal("rawRelayRead misreads a Supervisor request")
	}
}

func TestMCPReadSupervisor(t *testing.T) {
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		return map[string]any{"status": 200, "body": map[string]any{"result": "ok", "data": map[string]any{"options": map[string]any{"mqtt_password": "hunter2", "ssl": true}}}}
	})
	c := startMCP(t, paths)
	mcpInit(c)
	res := c.tool("ha_read", map[string]any{"endpoint": "supervisor", "path": "/addons/core_mosquitto/logs", "lines": 20})
	if _, code := readEnvelope(res); code != "" || calls()[0].body != `{"lines":20,"method":"GET","path":"/addons/core_mosquitto/logs"}` {
		t.Fatalf("logs: code %q, sent %v", code, calls())
	}
	res = c.tool("ha_read", map[string]any{"endpoint": "supervisor", "path": "/addons/core_mosquitto/info"})
	if text, _ := json.Marshal(res); strings.Contains(string(text), "hunter2") || !strings.Contains(string(text), "ssl") {
		t.Fatalf("info not redacted: %s", text)
	}
	before := len(calls())
	for _, path := range []string{"/addons/core_mosquitto/uninstall", "/dns/info", "/addons/self/info"} {
		if _, code := readEnvelope(c.tool("ha_read", map[string]any{"endpoint": "supervisor", "path": path})); code != "REFUSED" {
			t.Errorf("%s: code %q", path, code)
		}
	}
	if len(calls()) != before {
		t.Fatalf("refused reads were sent: %v", calls()[before:])
	}
}

func TestPlanRawSupervisorTiers(t *testing.T) {
	paths, sent := readRelay(t, func(endpoint string, body map[string]any) any {
		if endpoint == "supervisor" && body["path"] == "/addons/core_mosquitto/info" {
			return map[string]any{"status": 200, "body": map[string]any{"result": "ok", "data": map[string]any{"schema": []any{
				map[string]any{"name": "log_level", "type": "string"}, map[string]any{"name": "mqtt_pass", "type": "string"},
				map[string]any{"name": "wpa", "type": "string", "format": "password"},
				map[string]any{"name": "server", "type": "schema", "schema": []any{map[string]any{"name": "pw2", "type": "string", "format": "password"}}}}}}}
		}
		if endpoint == "supervisor" && body["method"] == "GET" {
			return map[string]any{"status": 200, "body": map[string]any{"result": "ok", "data": map[string]any{"name": "Mosquitto broker"}}}
		}
		return map[string]any{"status": 200, "body": map[string]any{"result": "ok", "data": map[string]any{}}}
	})
	sentWrites := func() []readCall {
		var writes []readCall
		for _, c := range sent() {
			if strings.Contains(c.body, `"POST"`) {
				writes = append(writes, c)
			}
		}
		return writes
	}
	cases := []struct{ path, data, tier, line string }{
		{"/store/addons/core_mosquitto/install", "", "typed", "Install the App “Mosquitto broker” (core_mosquitto)"},
		{"/addons/core_mosquitto/uninstall", `,"data":{"remove_config":true}`, "typed", "Uninstall the App “Mosquitto broker” (core_mosquitto); its settings, data, and config folder are deleted"},
		{"/addons/core_mosquitto/options", `,"data":{"options":{"log_level":"debug","mqtt_pass":"n3w"}}`, "typed", `Change the settings of the App “Mosquitto broker” (core_mosquitto): “log_level” to 'debug', “mqtt_pass” (a new secret value, not shown)`},
		{"/addons/core_mosquitto/options", `,"data":{"options":{"server":{"password":"nested-new"}}}`, "typed", "“server” (a new secret value, not shown)"},
		{"/addons/core_mosquitto/options", `,"data":{"options":{"wpa":"wpa-new-1","server":{"host":"h","pw2":"nested-pw2"}}}`, "typed", "“server” (a new secret value, not shown), “wpa” (a new secret value, not shown)"},
		{"/addons/core_mosquitto/options", `,"data":{"auto_update":false}`, "preview", "Turn off automatic updates for the App “Mosquitto broker” (core_mosquitto)"},
	}
	for _, tc := range cases {
		exit, env := planRaw(t, paths, `[{"endpoint":"supervisor","payload":{"method":"POST","path":"`+tc.path+`"`+tc.data+`}}]`)
		if exit != verbExitConfirmation || env.Tier != tc.tier || !strings.Contains(strings.ToLower(env.Say), strings.ToLower(tc.line)) || rawPreviewLeaks(t, paths, env) {
			t.Fatalf("%s: exit %d tier %s say %q", tc.path, exit, env.Tier, env.Say)
		}
		if exit, applied := applyRaw(t, paths, env); exit != 0 || statuses(rawOutcomes(t, applied)) != "done" {
			t.Fatalf("%s: apply exit %d %s", tc.path, exit, applied.Result)
		}
	}
	if strings.Contains(fmt.Sprint(sentWrites()), "n3w") == false {
		t.Fatalf("the new secret value was not sent: %v", sentWrites())
	}
	if got := len(sentWrites()); got != len(cases) {
		t.Fatalf("sent %d supervisor writes, want %d", got, len(cases))
	}
	for _, items := range []string{
		`[{"endpoint":"supervisor","payload":{"method":"GET","path":"/addons"}}]`,
		`[{"endpoint":"supervisor","payload":{"method":"POST","path":"/addons/2368fcfa_ha_nova_relay/uninstall"}}]`,
		`[{"endpoint":"supervisor","payload":{"method":"POST","path":"/host/reboot"}}]`,
		`[{"endpoint":"supervisor","payload":{"method":"POST","path":"/addons/core_mosquitto/stdin","data":{}}}]`,
	} {
		if exit, env := planRaw(t, paths, items); exit == verbExitConfirmation || env.Plan != nil {
			t.Errorf("%s: planned (exit %d)", items, exit)
		}
	}
}

// rawPreviewLeaks: a new secret shows in the say, the items, or the stored
// confirmation prompt.
func rawPreviewLeaks(t *testing.T, paths runtimePaths, env verbTestEnvelope) bool {
	t.Helper()
	plan, err := readPlanFile(filepath.Join(planDir(paths), env.Plan.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	text := env.Say + string(env.Result) + plan.Prompt // the phone question is built from the same item line (rawGoal)
	for _, secret := range []string{"nested-new", "n3w", "wpa-new-1", "nested-pw2"} {
		if strings.Contains(text, secret) {
			return true
		}
	}
	return false
}

// The App's schema can't be read: no value is shown.
func TestPlanRawSupervisorPreviewFailsClosed(t *testing.T) {
	paths, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		return map[string]any{"status": 500, "body": map[string]any{"result": "error", "message": "x"}}
	})
	exit, env := planRaw(t, paths, `[{"endpoint":"supervisor","payload":{"method":"POST","path":"/addons/core_x/options","data":{"options":{"log_level":"debug"}}}}]`)
	if exit != verbExitConfirmation || strings.Contains(env.Say, "debug") || !strings.Contains(env.Say, "“log_level” (a new secret value, not shown)") {
		t.Fatalf("exit %d say %q", exit, env.Say)
	}
}

// A Supervisor 4xx is a rejected step, not done.
func TestPlanRawSupervisorRejection(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"supervisor <nil>": func(map[string]any) any {
			return map[string]any{"status": 400, "body": map[string]any{"result": "error", "message": "App is not available"}}
		},
	})
	_, env := planRaw(t, paths, `[{"endpoint":"supervisor","payload":{"method":"POST","path":"/store/addons/core_x/install"}}]`)
	_, applied := applyRaw(t, paths, env)
	items := rawOutcomes(t, applied)
	if statuses(items) != "rejected" || !strings.Contains(items[0].Error, "the Supervisor answered HTTP 400: App is not available") {
		t.Fatalf("apply %s", applied.Result)
	}
}

func TestSupervisorResponseRedaction(t *testing.T) {
	body := []byte(`{"ok":true,"data":{"status":200,"body":{"data":{"wifi":{"psk":"wlan","psk_set":true},"options":{"api_key":"k1","server":"mqtt","ha_llat":"t","logins":[{"password":"p"}],"adminPin":"9876","dbpass":"dbx","env":[{"name":"DB_PASSWORD","value":"envx"}],"repo":"https://u:ghp_y@github.com/a/b","odd":"https://n@e.com:pw_odd@h/p","spaced":" https://a:pw_spaced@h/p"},"error_key":"app_x"}}}}`)
	out := string(redactRawResponse("supervisor", body))
	for _, leaked := range []string{"wlan", "k1", `"t"`, `"p"`, "9876", "dbx", "envx", "ghp_y", "pw_odd", "pw_spaced"} {
		if strings.Contains(out, leaked) {
			t.Errorf("%s leaked: %s", leaked, out)
		}
	}
	if !strings.Contains(out, `"psk_set":true`) || !strings.Contains(out, `"server":"mqtt"`) || !strings.Contains(out, `"error_key":"app_x"`) {
		t.Fatalf("over-redacted: %s", out)
	}
	failure := `{"ok":false,"error":{"code":"SUPERVISOR_REQUEST_REFUSED","message":"x"}}`
	if got := string(redactRawResponse("supervisor", []byte(failure))); !strings.Contains(got, "SUPERVISOR_REQUEST_REFUSED") {
		t.Fatalf("the Relay's own error code was hidden: %s", got)
	}
	if _, refusal, _ := guardRaw("supervisor", []byte(supervisorBody("POST", "/addons/core_x/options", map[string]any{"data": map[string]any{"options": map[string]any{"broker": "!secret wifi"}}})), nil); refusal == nil {
		t.Fatal("a !secret reference was not refused")
	}
}

func TestSupervisorRefusedOverHomeAssistantLink(t *testing.T) {
	if _, err := newRelayRequest(t.Context(), relayTransportSelection{BaseURL: cloudRelayVirtualBaseURL, Via: relayViaCloud}, "supervisor", []byte("{}")); err != errSupervisorViaCloud {
		t.Fatalf("cloud: %v", err)
	}
}
