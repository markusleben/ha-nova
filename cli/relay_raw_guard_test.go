package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

// rawRelayCounter is a Relay that counts every request it receives.
func rawRelayCounter(t *testing.T) (runtimePaths, *atomic.Int32) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))
	var hits atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("content-type", "application/json")
		_, _ = fmt.Fprint(w, `{"ok":true,"data":{"status":200,"body":[]}}`)
	}))
	t.Cleanup(relay.Close)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relay.URL}); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayAuthToken("test-token"); err != nil {
		t.Fatal(err)
	}
	return paths, &hits
}

// The reproduced bypass: a raw unlock reached Home Assistant with no plan.
func TestRawRelayRefusesServiceCalls(t *testing.T) {
	paths, hits := rawRelayCounter(t)
	cases := [][]string{
		{"core", "--method", "POST", "--path", "/api/services/lock/unlock", "--body", `{"entity_id":"lock.front_door"}`},
		{"core", "--method", "post", "--path", "/api/services/lock/unlock?return_response"},
		{"core", "--method", "POST", "--path", "/api/%73ervices/lock/unlock"},
		{"core", "--method", "POST", "--path", "/api/%2573ervices/cover/open_cover"},
		{"core", "--method", "POST", "--path", "/api/x/../services/lock/unlock"},
		{"ws", "--data", `{"type":"call_service","domain":"lock","service":"unlock","target":{"entity_id":"lock.front_door"}}`},
		{"ws", "--data", `{"type":" Call_Service ","domain":"lock","service":"unlock"}`},
		{"ws", "--data", `{"type":"execute_script","sequence":[{"action":"lock.unlock"}]}`},
		{"ws", "--data", `{"message":{"type":"call_service","domain":"lock","service":"unlock"},"collect_events":{"max_events":1}}`},
	}
	for _, args := range cases {
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, args) })
		if exit != verbExitRefused || !strings.Contains(stderr, "nothing was sent") || !strings.Contains(stderr, "ha-nova call") ||
			strings.Contains(stderr, "ha-nova setup") {
			t.Errorf("%v: exit %d, stderr %q; want a refusal naming ha-nova call", args, exit, stderr)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the Relay received %d requests; a refused raw request must send nothing", n)
	}
}

func TestRawRelayRefusesEventsAndWebhooks(t *testing.T) {
	rawRefused(t, "core", `{"method":"POST","path":"/api/events/my_event","body":{}}`)
	rawRefused(t, "core", `{"method":"POST","path":"/api/webhook/abc"}`)
	rawRefused(t, "core", `{"method":"GET","path":"/api/webhook/abc"}`)
	rawRefused(t, "ws", `{"type":"fire_event","event_type":"my_event"}`)
	rawRefused(t, "ws", `{"type":"webhook/list"}`)
	rawRefused(t, "ws", `{"type":"webhook/handle","webhook_id":"x","body":"{}"}`)
	rawAllowed(t, "core", `{"method":"GET","path":"/api/events"}`)
	rawRefused(t, "core", `{"method":"GET","path":"/api/backup/download/abc?agent_id=hassio.local"}`) // holds every stored credential
	rawRefused(t, "core", `{"method":"GET","path":"/api/hassio/backups/abc/download"}`)
}

func TestRawRelayRefusesOtherActuationSurfaces(t *testing.T) {
	for _, path := range []string{"/api/states/person.me", "/api/intent/handle", "/api/conversation/process", "/api/hassio/core/restart",
		"/api/google_assistant", "/api/alexa/smart_home", "/api/mobile_app/registrations", "/api/cloud/google_actions/sync"} {
		rawRefused(t, "core", `{"method":"POST","path":"`+path+`"}`)
	}
	rawRefused(t, "ws", `{"type":"conversation/process","text":"open the garage"}`)
	rawRefused(t, "ws", `{"type":"assist_pipeline/run","start_stage":"intent"}`)
	rawRefused(t, "ws", `{"type":"backup/restore","backup_id":"x"}`)
	rawRefused(t, "ws", `{"type":"supervisor/api","endpoint":"/host/reboot","method":"post"}`)
	rawRefused(t, "ws", `{"type":"supervisor/api","endpoint":"/addons","method":"get"}`) // no skill reads it raw
	rawAllowed(t, "core", `{"method":"GET","path":"/api/states/lock.front_door"}`)
}

func TestRawRelayRefusesConfigPlanBypass(t *testing.T) {
	rawRefused(t, "core", `{"method":"POST","path":"/api/config/automation/config/1","body":{}}`)
	rawRefused(t, "core", `{"method":"DELETE","path":"/api/config/script/config/leave"}`)
	rawRefused(t, "ws", `{"type":"lovelace/dashboards/delete","dashboard_id":"x"}`)
	rawAllowed(t, "core", `{"method":"GET","path":"/api/config/automation/config/1"}`)
	rawAllowed(t, "core", `{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"trend"}}`)
	rawAllowed(t, "core", `{"method":"POST","path":"/api/config/scene/config/1","body":{}}`)
	rawAllowed(t, "core", `{"method":"POST","path":"/api/template","body":{"template":"{{ 1 }}"}}`)
}

func TestRawRelayRefusesCoverClassOverride(t *testing.T) {
	rawRefused(t, "ws", `{"type":"config/entity_registry/update","entity_id":"cover.garage","device_class":"curtain"}`)
	rawAllowed(t, "ws", `{"type":"config/entity_registry/update","entity_id":"cover.garage","name":"Garage"}`)
	rawAllowed(t, "ws", `{"type":"config/entity_registry/update","entity_id":"binary_sensor.door","device_class":"door"}`)
	rawRefused(t, "ws", `{"type":"blueprint/save","domain":"automation","path":"a.yaml","yaml":"x","allow_override":true}`)
	rawAllowed(t, "ws", `{"type":"blueprint/save","domain":"automation","path":"a.yaml","yaml":"x"}`)
}

func TestRawRelayRefusesCallServiceSubscriptions(t *testing.T) {
	rawRefused(t, "ws", `{"message":{"type":"subscribe_events"},"collect_events":{"max_events":1}}`)
	rawRefused(t, "ws", `{"message":{"type":"subscribe_events","event_type":"call_service"},"collect_events":{"max_events":1}}`)
	rawRefused(t, "ws", `{"message":{"type":"subscribe_events","event_type":"*"},"collect_events":{"max_events":1}}`)
	rawRefused(t, "ws", `{"message":{"type":"subscribe_trigger","trigger":{"platform":"event","event_type":"call_service"}},"collect_events":{}}`)
	rawAllowed(t, "ws", `{"message":{"type":"subscribe_events","event_type":"zha_event"},"collect_events":{"max_events":1}}`)
	rawAllowed(t, "ws", `{"message":{"type":"subscribe_trigger","trigger":{"platform":"state","entity_id":"event.x"}},"collect_events":{}}`)
}

func rawRefused(t *testing.T, endpoint, body string) {
	t.Helper()
	if err := checkRawRelayRequest(endpoint, []byte(body)); err == nil || !strings.Contains(err.Error(), "nothing was sent") {
		t.Errorf("%s %s: got %v, want a refusal", endpoint, body, err)
	}
}

func rawAllowed(t *testing.T, endpoint, body string) {
	t.Helper()
	if err := checkRawRelayRequest(endpoint, []byte(body)); err != nil {
		t.Errorf("%s %s: refused (%v), want allowed", endpoint, body, err)
	}
}

// Codex P1: the Relay parses the core path as a URL, dropping the #fragment;
// the guard must drop it too, or `/api/services/lock/unlock#/../../template`
// reads as /api/template (allowed) while HA gets the unlock.
func TestRawRelayRefusesFragmentPathTricks(t *testing.T) {
	rawRefused(t, "core", `{"method":"POST","path":"/api/services/lock/unlock#/../../../template"}`)
	rawRefused(t, "core", `{"method":"POST","path":"/api/services/lock/unlock?x=1#frag"}`)
	rawRefused(t, "core", `{"method":"POST","path":"/api/events/e#/../../api/template"}`)
	// A real template render still works (fragment dropped, path unchanged).
	rawAllowed(t, "core", `{"method":"POST","path":"/api/template#x"}`)
}

// Codex P1: a JSON number that overflows Go's float64 (but JS JSON.parse
// accepts) must not fail the guard open. UseNumber keeps call_service visible.
func TestRawRelayRefusesNumberOverflow(t *testing.T) {
	rawRefused(t, "ws", `{"type":"call_service","domain":"lock","service":"unlock","id":1e1000}`)
	rawRefused(t, "core", `{"method":"POST","path":"/api/services/lock/unlock","id":99999999999999999999999}`)
}

// The Relay's URL parse strips whitespace and control characters (and turns
// `\` into `/`) before HA sees the path; neither the guard nor the read
// allowlist may check a different path than the one sent.
func TestRawRelayRefusesWhitespaceAndControlPaths(t *testing.T) {
	for _, path := range []string{" /api/services/lock/unlock", "/api/serv\tices/lock/unlock", "/api/services/lock/unlock\n", "\x01/api/events/e"} {
		body, _ := json.Marshal(map[string]any{"method": "POST", "path": path})
		if err := checkRawRelayRequest("core", body); err == nil {
			t.Errorf("%q: allowed, want refused", path)
		}
	}
	for _, path := range []string{"/api/states/.\t./webhook/abc", "/api/states/..\\webhook/abc", " /api/webhook/abc", "/api/states/x#/../../webhook/a"} {
		if readablePath(path) || mcpReadablePath(path) {
			t.Errorf("read allowlist accepted %q", path)
		}
	}
	if !readablePath("/api/states/lock.front_door") || !mcpReadablePath("/api/history/period?filter_entity_id=light.x") {
		t.Error("plain reads must stay allowed")
	}
}

// Deny-by-default: an integration's own WS or HTTP API can move a device
// (a Z-Wave lock, a Zigbee cluster, an Alarmo panel); none is on the
// allowlist, so none is sent.
func TestRawRelayRefusesIntegrationWrites(t *testing.T) {
	for _, body := range []string{
		`{"type":"zwave_js/set_value","device_id":"d","command_class":98,"property":"targetMode","value":0}`, // unlock a Z-Wave lock
		`{"type":"zwave_js/set_lock_usercode","entity_id":"lock.front","code_slot":1,"usercode":"1234"}`,
		`{"type":"zha/devices/clusters/attributes/value","ieee":"x","endpoint_id":1,"cluster_id":257,"attribute":0,"value":2}`,
		`{"type":"matter/open_commissioning_window","device_id":"d"}`,
		`{"type":"esphome/device/command","device_id":"d"}`,
		`{"message":{"type":"zwave_js/set_value","device_id":"d"},"collect_events":{"max_events":1}}`,
	} {
		rawRefused(t, "ws", body)
	}
	for _, path := range []string{"/api/config/config_entries/entry/01JABCDEF/reload", "/api/alarmo/area", "/api/zwave_js/node/1/set", "/api/frigate/notifications", "/api/config/core/update"} {
		rawRefused(t, "core", `{"method":"POST","path":"`+path+`","body":{}}`)
	}
}

// An unknown WS type is refused, also a near-miss of an allowed one (HA
// dispatches on the exact string), and a body with no type at all.
func TestRawRelayRefusesUnknownWSTypes(t *testing.T) {
	for _, body := range []string{`{"type":"my_integration/do"}`, `{"type":" ping"}`, `{"type":"Get_States"}`, `{"type":"lovelace/config/save","url_path":"x","config":{}}`,
		`{"type":"lovelace/dashboards/create","url_path":"a-b"}`, `{"type":"lovelace/config","force":true}`, `{}`, `{"type":["ping"]}`,
		`{"type":"config/entity_registry/update","entity_id":"button.reboot","device_class":"identify"}`,
		`{"type":"config/entity_registry/update","entity_id":"lock.front","disabled_by":"user"}`,
		`{"type":"lovelace/resources/create","res_type":"module","url":"https://example.com/card.js"}`,
		`{"type":"lovelace/resources/update","resource_id":"x","url":"/local/../api/x"}`,
		`{"type":"hacs/repositories/add","repository":"someone/thing","category":"integration"}`} {
		rawRefused(t, "ws", body)
	}
}

func TestRawRelayAllowlistedWritesPass(t *testing.T) {
	for _, body := range []string{`{"type":"ping"}`, `{"type":"config/area_registry/create","name":"Garden"}`, `{"type":"input_boolean/create","name":"Guest"}`,
		`{"type":"config/entity_registry/update","entity_id":"light.desk","name":"Desk","area_id":"office"}`, `{"type":"lovelace/config","url_path":"x"}`,
		`{"type":"config/entity_registry/update","entity_id":"button.doorbell","area_id":"hall"}`, `{"type":"energy/save_prefs","energy_sources":[]}`,
		`{"type":"lovelace/resources/create","res_type":"module","url":"/hacsfiles/mushroom/mushroom.js"}`, `{"type":"lovelace/resources/update","resource_id":"x","res_type":"js"}`} {
		rawAllowed(t, "ws", body)
	}
	for _, req := range []string{`{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"trend"}}`,
		`{"method":"DELETE","path":"/api/config/config_entries/flow/0123abcd"}`, `{"method":"POST","path":"/api/config/config_entries/options/flow/0123abcd","body":{}}`,
		`{"method":"DELETE","path":"/api/config/config_entries/entry/01JABCDEF"}`,
		`{"method":"POST","path":"/api/config/core/check_config"}`, `{"method":"delete","path":"/api/config/scene/config/1712345"}`} {
		rawAllowed(t, "core", req)
	}
	// Allowlisted shapes reached through escapes or dot segments are refused:
	// the path checked must be the path sent.
	for _, path := range []string{"/api/config/scene/config/%31", "/api/config/scene/config/x/../../../../services/lock/unlock", "/api/config//core/check_config", "/api/config/scene/config/1/"} {
		rawRefused(t, "core", `{"method":"POST","path":"`+path+`"}`)
	}
}

// The read verbs' WS allowlist stays readable raw, and no write the guard
// names a better route for is allowlisted.
func TestRawAllowlistCoversReadVerbsOnly(t *testing.T) {
	for wsType := range readOnlyWSTypes {
		if rawWSAllowed[wsType] == "" {
			t.Errorf("read-verb type %s is not raw-readable", wsType)
		}
	}
	for wsType := range rawWSHints {
		if _, allowed := rawWSAllowed[wsType]; allowed {
			t.Errorf("%s has a refusal hint but is allowlisted", wsType)
		}
	}
}

// Removing or reconfiguring what marks a sibling as access-capable (a lock,
// an alarm panel, an access cover, a switch_as_x lock) would make a later
// call ordinary: the CLI reads the registry first and refuses.
func TestRawRelayRefusesAccessTargetRemoval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))
	var forwarded atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["path"] == "/api/states/cover.blind" {
			_, _ = fmt.Fprint(w, `{"ok":true,"data":{"status":200,"body":{"entity_id":"cover.blind","state":"open","attributes":{"device_class":"blind"}}}}`)
			return
		}
		if body["type"] == "config/entity_registry/list" {
			_, _ = fmt.Fprint(w, `{"ok":true,"data":[{"entity_id":"lock.garden_gate","config_entry_id":"SWX1"},{"entity_id":"cover.blind","config_entry_id":"BL1"},{"entity_id":"switch.gate_relay","config_entry_id":"SW1"},{"entity_id":"button.node_7","original_name":"Factory reset"},{"entity_id":"button.doorbell"}]}`)
			return
		}
		forwarded.Add(1)
		_, _ = fmt.Fprint(w, `{"ok":true,"data":{"status":200,"body":{}}}`)
	}))
	t.Cleanup(relay.Close)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "127.0.0.1", HAURL: "http://127.0.0.1:8123", RelayBaseURL: relay.URL}); err != nil {
		t.Fatal(err)
	}
	if err := writeRelayAuthToken("test-token"); err != nil {
		t.Fatal(err)
	}
	refused := [][]string{
		{"core", "--method", "DELETE", "--path", "/api/config/config_entries/entry/SWX1"},
		{"core", "--method", "POST", "--path", "/api/config/config_entries/options/flow", "--body", `{"handler":"SWX1"}`},
		{"core", "--method", "POST", "--path", "/api/config/config_entries/flow", "--body", `{"handler":"switch_as_x","source":"reconfigure","entry_id":"SWX1"}`},
		{"ws", "--data", `{"type":"config/entity_registry/remove","entity_id":"lock.garden_gate"}`},
		{"ws", "--data", `{"type":"config/entity_registry/update","entity_id":"button.node_7","name":"Tidy"}`},
		{"ws", "--data", `{"type":"config/entity_registry/update","entity_id":"button.node_7","new_entity_id":"button.tidy"}`},
	}
	for _, args := range refused {
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, args) })
		if exit != verbExitRefused || !strings.Contains(stderr, "nothing was sent") || strings.Contains(stderr, "ha-nova setup") {
			t.Errorf("%v: exit %d, stderr %q; want a refusal", args, exit, stderr)
		}
	}
	if n := forwarded.Load(); n != 0 {
		t.Fatalf("%d refused requests were forwarded", n)
	}
	for _, args := range [][]string{
		{"core", "--method", "DELETE", "--path", "/api/config/config_entries/entry/BL1"},
		{"ws", "--data", `{"type":"config/entity_registry/remove","entity_id":"switch.gate_relay"}`},
		{"ws", "--data", `{"type":"config/entity_registry/update","entity_id":"button.doorbell","new_entity_id":"button.front_bell"}`},
	} {
		if exit := runRelayCommand(paths, args); exit != 0 {
			t.Errorf("%v: exit %d, want sent", args, exit)
		}
	}
	if n := forwarded.Load(); n != 3 {
		t.Fatalf("forwarded %d, want the 3 non-access requests", n)
	}
}

// Every WS type a skill writes as a payload is on the raw allowlist, or is
// one the skills name only to say it is never sent raw.
func TestSkillWSTypesAreAllowlisted(t *testing.T) {
	neverRaw := map[string]bool{"lovelace/config/save": true, "lovelace/dashboards/create": true, "lovelace/dashboards/delete": true,
		"hacs/repositories/add": true, "hacs/repositories/clear_new": true, "hacs/repository/state": true, "recorder/change_statistics_unit": true}
	typePattern := regexp.MustCompile(`"type" *: *"([a-z_]+(?:/[a-z0-9_]+)+)"`)
	err := fs.WalkDir(os.DirFS("../skills"), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		text, err := fs.ReadFile(os.DirFS("../skills"), path)
		for _, m := range typePattern.FindAllStringSubmatch(string(text), -1) {
			if _, allowed := rawWSAllowed[m[1]]; !allowed && !neverRaw[m[1]] {
				t.Errorf("skills/%s sends WS %s, which the raw guard refuses", path, m[1])
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
