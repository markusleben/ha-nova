package main

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// An App options change that sets a password, key, or other secret typed
// into the chat is refused before anything is sent: by its name, in a
// nested option or a list too, an environment entry by its name, or a URL
// with a password in it; an empty one, or an option that only looks
// similar, is not.
func TestAppOptionsWithASecretAreRefused(t *testing.T) {
	for _, tc := range []struct {
		options map[string]any
		refused string
	}{
		{map[string]any{"password": "hunter2"}, "password"},
		{map[string]any{"logins": []any{map[string]any{"username": "mqtt", "password": "hunter2"}}}, "password"},
		{map[string]any{"http_node": map[string]any{"username": "admin", "password": "hunter2"}}, "password"},
		{map[string]any{"credential_secret": "s3cret"}, "credential_secret"},
		{map[string]any{"s2_access_control_key": "0123456789ABCDEF"}, "s2_access_control_key"},
		{map[string]any{"network_key": []any{1, 3, 5}}, "network_key"},
		{map[string]any{"pin": 1234}, "pin"},
		{map[string]any{"env_vars": []any{map[string]any{"name": "DB_PASSWORD", "value": "hunter2"}}}, "DB_PASSWORD"},
		{map[string]any{"repository": "https://me:ghp_x@github.com/me/apps"}, "repository"},
		{map[string]any{"database_url": "postgresql://ha:Pa{ss@core-mariadb/ha"}, "database_url"},
		{map[string]any{"mqtt_url": "mqtt://u:p#ss@core-mosquitto"}, "mqtt_url"},
		{map[string]any{"broker": []any{" mqtt://u:pässwort@broker"}}, "broker"},
		{map[string]any{"feed": "https://registry:5000/@scope/pkg", "mode": "a:b@c"}, ""},
		{map[string]any{"dns": map[string]any{"provider": "dns-he", "he_user": "me", "he_pass": "hunter2"}}, "he_pass"},
		{map[string]any{"dns": map[string]any{"eurodns_apiKey": "k-1"}}, "eurodns_apiKey"},
		{map[string]any{"mqttPwd": "hunter2"}, "mqttPwd"},
		{map[string]any{"server": map[string]any{"dbpass": "hunter2"}}, "dbpass"},
		{map[string]any{"lego_env": []any{"LEGO_DISABLE_CNAME_SUPPORT=true", "NETCUP_API_PASSWORD=hunter2"}}, "NETCUP_API_PASSWORD"},
		{map[string]any{"password": "QUJDRA=="}, "password"},
		{map[string]any{"passive": "on", "tokenFile": "/ssl/token.txt", "certFile": "fullchain.pem", "lego_env": []any{"LEGO_DISABLE_CNAME_SUPPORT=true"}}, ""},
		{map[string]any{"password": "", "log_level": "debug", "ssl": true, "certfile": "fullchain.pem", "keyfile": "privkey.pem",
			"authorized_keys": []any{"ssh-ed25519 AAAAC3 me@laptop"}, "server": "mqtt://core-mosquitto:1883",
			"env_vars": []any{map[string]any{"name": "TZ", "value": "Europe/Berlin"}}, "logins": []any{}}, ""},
	} {
		body := supervisorBody("POST", "/addons/core_mosquitto/options", map[string]any{"data": map[string]any{"options": tc.options}})
		_, refusal, err := guardRaw("supervisor", []byte(body), nil)
		if err != nil || tc.refused == "" && refusal != nil ||
			tc.refused != "" && (refusal == nil || !strings.Contains(refusal.Error(), strconv.Quote(tc.refused)+" (a password or other secret)") || !strings.Contains(refusal.Error(), "the App's Configuration tab")) {
			t.Errorf("%v: refusal %v, err %v", tc.options, refusal, err)
		}
	}
	// A setup step's URL that Go can't parse is refused too (the same rule).
	for _, value := range []string{"postgresql://ha:Pa{ss@core-mariadb/ha", "mqtt://u:p#ss@core-mosquitto", "mqtt://u:pässwort@broker"} {
		body, _ := json.Marshal(map[string]any{"method": "POST", "path": "/api/config/config_entries/flow/0123abcd", "body": map[string]any{"broker_url": value}})
		if refusal := checkRawRelayRequest("core", body); refusal == nil || !strings.Contains(refusal.Error(), `"broker_url"`) {
			t.Errorf("flow step %s: refusal %v", value, refusal)
		}
	}
}

// The App's own form decides too: an option it marks a password is refused
// whatever its name, also nested and in a list, at planning (ha_plan too)
// and again at apply (here the form changed after the preview); an empty
// one, and an option the form doesn't mark, go through. A form HA NOVA
// can't read stops the change; an App without one (schema null) gets the
// name checks, and its preview shows no value.
func TestPlanRawAppOptionsFollowTheAppForm(t *testing.T) {
	var mu sync.Mutex
	motd := map[string]any{"name": "motd", "type": "string"}
	info := map[string]any{"status": 200, "body": map[string]any{"result": "ok", "data": map[string]any{"schema": []any{
		map[string]any{"name": "wpa", "type": "string", "format": "password"},
		map[string]any{"name": "server", "type": "schema", "schema": []any{map[string]any{"name": "entry", "type": "string", "format": "password", "multiple": true}}},
		motd}}}}
	paths, calls := readRelay(t, func(endpoint string, body map[string]any) any {
		mu.Lock()
		defer mu.Unlock()
		if endpoint == "supervisor" && body["path"] == "/addons/core_ssh/info" {
			return info
		}
		return map[string]any{"status": 200, "body": map[string]any{"result": "ok", "data": map[string]any{}}}
	})
	posts := func() (n int) {
		for _, c := range calls() {
			if c.endpoint == "supervisor" && strings.Contains(c.body, `"POST"`) {
				n++
			}
		}
		return n
	}
	item := func(options string) string {
		return `[{"endpoint":"supervisor","payload":{"method":"POST","path":"/addons/core_ssh/options","data":{"options":` + options + `}}}]`
	}
	for options, field := range map[string]string{`{"wpa":"wpa-new-1"}`: "wpa", `{"server":{"entry":["x1"]}}`: "entry", `{"motd":"hi","password":"p"}`: "password"} {
		exit, env := planRaw(t, paths, item(options))
		if exit != verbExitRefused || env.Plan != nil || env.Error == nil || env.Error.Code != "REFUSED" ||
			!strings.Contains(env.Error.Message, strconv.Quote(field)+" (a password or other secret)") || !strings.Contains(env.Error.Message, "set that option in the App's Configuration tab") {
			t.Errorf("%s: exit %d %+v", options, exit, env.Error)
		}
	}
	c := startMCP(t, paths)
	mcpInit(c)
	request := map[string]any{"endpoint": "supervisor", "payload": map[string]any{"method": "POST", "path": "/addons/core_ssh/options", "data": map[string]any{"options": map[string]any{"wpa": "wpa-new-1"}}}}
	if env, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "raw", "requests": []any{request}})); code != "REFUSED" || env["plan"] != nil {
		t.Errorf("ha_plan: %v", env)
	}
	exit, env := planRaw(t, paths, item(`{"wpa":"","motd":"hello"}`))
	if exit != verbExitConfirmation {
		t.Fatalf("an empty password and a plain option: exit %d %+v", exit, env.Error)
	}
	mu.Lock()
	motd["format"] = "password"
	mu.Unlock()
	if exit, applied := applyRaw(t, paths, env); exit != verbExitRefused || applied.Error == nil || !strings.Contains(applied.Error.Message, `"motd" (a password or other secret)`) || posts() != 0 {
		t.Fatalf("apply after the form changed: exit %d %+v, %d sent", exit, applied.Error, posts())
	}
	for reply, unreadable := range map[string]bool{`{"status":200,"body":{"data":{"schema":null}}}`: false, `{"status":200,"body":{"data":{}}}`: true, `{"status":404,"body":{"result":"error"}}`: true} {
		mu.Lock()
		info = map[string]any{}
		if err := json.Unmarshal([]byte(reply), &info); err != nil {
			t.Fatal(err)
		}
		mu.Unlock()
		exit, env := planRaw(t, paths, item(`{"motd":"hello"}`))
		if unreadable == (exit == verbExitConfirmation) || strings.Contains(env.Say, "hello") || unreadable && (env.Error == nil || !strings.Contains(env.Error.Message, "cannot read the App's stored options")) {
			t.Errorf("%s: exit %d %+v", reply, exit, env.Error)
		}
		if exit, env := planRaw(t, paths, item(`{"password":"p"}`)); exit != verbExitRefused || env.Error == nil || !strings.Contains(env.Error.Message, `"password" (a password or other secret)`) {
			t.Errorf("%s, a password: exit %d %+v", reply, exit, env.Error)
		}
	}
	if posts() != 0 {
		t.Fatalf("%d App writes were sent", posts())
	}
}
