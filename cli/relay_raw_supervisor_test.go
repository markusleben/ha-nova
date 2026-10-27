package main

import (
	"encoding/json"
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
	paths, sent := rawRelay(t, nil)
	cases := []struct{ path, data, tier, line string }{
		{"/store/addons/core_mosquitto/install", "", "typed", "Install the App “core_mosquitto”"},
		{"/addons/core_mosquitto/uninstall", `,"data":{"remove_config":true}`, "typed", "Uninstall the App “core_mosquitto” and delete its settings folder"},
		{"/addons/core_mosquitto/options", `,"data":{"options":{"log_level":"debug"}}`, "typed", "Change the settings “log_level” of the App “core_mosquitto”"},
		{"/addons/core_mosquitto/options", `,"data":{"auto_update":false}`, "preview", "Turn off automatic updates for the App “core_mosquitto”"},
	}
	for _, tc := range cases {
		exit, env := planRaw(t, paths, `[{"endpoint":"supervisor","payload":{"method":"POST","path":"`+tc.path+`"`+tc.data+`}}]`)
		if exit != verbExitConfirmation || env.Tier != tc.tier || !strings.Contains(strings.ToLower(env.Say), strings.ToLower(tc.line)) {
			t.Fatalf("%s: exit %d tier %s say %q", tc.path, exit, env.Tier, env.Say)
		}
		if exit, applied := applyRaw(t, paths, env); exit != 0 || statuses(rawOutcomes(t, applied)) != "done" {
			t.Fatalf("%s: apply exit %d %s", tc.path, exit, applied.Result)
		}
	}
	if got := len(sent()); got != len(cases) {
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
	body := []byte(`{"ok":true,"data":{"status":200,"body":{"data":{"wifi":{"psk":"wlan","psk_set":true},"options":{"api_key":"k1","server":"mqtt","ha_llat":"t","logins":[{"password":"p"}]}}}}}`)
	out := string(redactRawResponse("supervisor", body))
	for _, leaked := range []string{"wlan", "k1", `"t"`, `"p"`} {
		if strings.Contains(out, leaked) {
			t.Errorf("%s leaked: %s", leaked, out)
		}
	}
	if !strings.Contains(out, `"psk_set":true`) || !strings.Contains(out, `"server":"mqtt"`) {
		t.Fatalf("over-redacted: %s", out)
	}
}

func TestSupervisorRefusedOverHomeAssistantLink(t *testing.T) {
	if _, err := newRelayRequest(t.Context(), relayTransportSelection{BaseURL: cloudRelayVirtualBaseURL, Via: relayViaCloud}, "supervisor", []byte("{}")); err != errSupervisorViaCloud {
		t.Fatalf("cloud: %v", err)
	}
}
