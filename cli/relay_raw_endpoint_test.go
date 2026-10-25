package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// rawGuardRelay is a Relay that answers the guard's registry and state
// reads from registry() and classes (a cover with no class has no state, as
// a disabled one; its registry entry then answers with stored[id]), and
// counts the registry reads and every request it forwards.
func rawGuardRelay(t *testing.T, registry func() string, classes, stored map[string]string) (paths runtimePaths, forwarded, registryReads *atomic.Int32) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(home, ".test-relay-token"))
	forwarded, registryReads = &atomic.Int32{}, &atomic.Int32{}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if path, _ := body["path"].(string); body["method"] == "GET" && strings.HasPrefix(path, "/api/states/") {
			id := strings.TrimPrefix(path, "/api/states/")
			if class, ok := classes[id]; ok {
				_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"status":200,"body":{"entity_id":%q,"state":"closed","attributes":{"device_class":%q}}}}`, id, class)
				return
			}
			_, _ = fmt.Fprint(w, `{"ok":true,"data":{"status":404,"body":{"message":"Entity not found."}}}`)
			return
		}
		if body["type"] == "config/entity_registry/get" {
			id, _ := body["entity_id"].(string)
			_, _ = fmt.Fprintf(w, `{"ok":true,"data":{"entity_id":%q,"device_class":null,"original_device_class":%q}}`, id, stored[id])
			return
		}
		if body["type"] == "config/entity_registry/list" {
			registryReads.Add(1)
			_, _ = fmt.Fprintf(w, `{"ok":true,"data":%s}`, registry())
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
	return paths, forwarded, registryReads
}

// Disabling a device disables its lock, alarm panel, or access cover with
// it, which the entity registry rule already refuses one entity at a time.
func TestRawRelayRefusesDisablingAccessDevice(t *testing.T) {
	registry := `[{"entity_id":"sensor.door_battery","device_id":"dlock"},{"entity_id":"lock.front_door","device_id":"dlock"},
		{"entity_id":"alarm_control_panel.home","device_id":"dalarm"},{"entity_id":"cover.garage","device_id":"dgarage"},
		{"entity_id":"cover.gate","device_id":"dgate"},{"entity_id":"cover.blind","device_id":"dblind"},{"entity_id":"switch.plug","device_id":"dplug"},
		{"entity_id":"cover.curtain","device_id":"dcurtain"},{"entity_id":"cover.old_garage","device_id":"doldgarage"},
		{"entity_id":"switch.gate_relay","device_id":"drelay"},{"entity_id":"cover.gate_relay","device_id":"drelay","platform":"switch_as_x"},
		{"entity_id":"switch.lamp_relay","device_id":"dlamp"},{"entity_id":"light.lamp_relay","device_id":"dlamp","platform":"switch_as_x"},
		{"entity_id":"switch.template_bolt"},{"entity_id":"lock.template_bolt","platform":"switch_as_x"}]`
	paths, forwarded, _ := rawGuardRelay(t, func() string { return registry }, map[string]string{"cover.garage": "garage", "cover.blind": "blind"},
		map[string]string{"cover.curtain": "curtain", "cover.old_garage": "garage"})
	for _, body := range []string{
		`{"type":"config/device_registry/update","device_id":"dlock","disabled_by":"user"}`,
		`{"type":"config/device_registry/update","device_id":"dalarm","disabled_by":null}`, // enabling
		`{"type":"config/device_registry/update","device_id":"dgarage","disabled_by":"user","name_by_user":"Garage"}`,
		`{"type":"config/device_registry/update","device_id":"dgate","disabled_by":"user"}`, // no state, no class: fails closed
		`{"message":{"type":"config/device_registry/update","device_id":"dlock","disabled_by":"user"},"collect_events":{"max_events":1}}`,
		`{"type":"config/device_registry/update","device_id":"doldgarage","disabled_by":null}`,             // a disabled garage door: class from its registry entry
		`{"type":"config/entity_registry/update","entity_id":"switch.gate_relay","disabled_by":"user"}`,    // the switch a switch_as_x gate runs on
		`{"type":"config/entity_registry/update","entity_id":"switch.template_bolt","disabled_by":"user"}`, // device-less, as tier_scan's switchAsX pairs them
		`{"type":"config/device_registry/update","disabled_by":"user"}`,                                    // no device named: fails closed
	} {
		var exit int
		stderr := captureStderr(t, func() { exit = runRelayCommand(paths, []string{"ws", "--data", body}) })
		if exit != verbExitRefused || !strings.Contains(stderr, "nothing was sent") || !strings.Contains(stderr, "Home Assistant UI") || strings.Contains(stderr, "ha-nova setup") {
			t.Errorf("%s: exit %d, stderr %q; want a refusal", body, exit, stderr)
		}
	}
	if n := forwarded.Load(); n != 0 {
		t.Fatalf("%d refused requests were forwarded", n)
	}
	for _, body := range []string{
		`{"type":"config/device_registry/update","device_id":"dlock","name_by_user":"Front door","area_id":"hall"}`,
		`{"type":"config/device_registry/update","device_id":"dplug","disabled_by":"user"}`,
		`{"type":"config/device_registry/update","device_id":"dblind","disabled_by":"user"}`,
		`{"type":"config/device_registry/update","device_id":"dcurtain","disabled_by":null}`, // a disabled curtain: class from its registry entry
		`{"type":"config/entity_registry/update","entity_id":"switch.lamp_relay","disabled_by":"user"}`,
	} {
		if exit := runRelayCommand(paths, []string{"ws", "--data", body}); exit != 0 {
			t.Errorf("%s: exit %d, want sent", body, exit)
		}
	}
	if n := forwarded.Load(); n != 5 {
		t.Fatalf("forwarded %d, want the 5 requests that disable no access entity", n)
	}
}

// A caller sending several requests re-runs the check before each send on
// one session: the registry is read again, so an earlier request that
// renamed switch.gate to lock.gate cannot carry the removal past it.
func TestRawAccessTargetRecheckReadsLive(t *testing.T) {
	var mu sync.Mutex
	registry := `[{"entity_id":"switch.gate","device_id":"d1"}]`
	paths, _, reads := rawGuardRelay(t, func() string { mu.Lock(); defer mu.Unlock(); return registry }, nil, nil)
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	remove := []byte(`{"type":"config/entity_registry/remove","entity_id":"lock.gate"}`)
	if refusal, err := checkRawAccessTargetIn(s, "ws", remove); refusal != nil || err != nil {
		t.Fatalf("before the rename: refusal %v, err %v; want sent", refusal, err)
	}
	mu.Lock()
	registry = `[{"entity_id":"lock.gate","device_id":"d1"}]`
	mu.Unlock()
	if refusal, err := checkRawAccessTargetIn(s, "ws", remove); refusal == nil || err != nil || !strings.Contains(refusal.Error(), "lock.gate") {
		t.Fatalf("after the rename: refusal %v, err %v; want refused", refusal, err)
	}
	if n := reads.Load(); n != 2 {
		t.Fatalf("registry read %d times, want once per check", n)
	}
}

// The shared guard refuses exactly what the raw route refused before it
// (the cases of relay_raw_guard_test.go), with the same words.
func TestRawGuardReproducesRelayRefusals(t *testing.T) {
	paths, forwarded, _ := rawGuardRelay(t, func() string { return `[]` }, nil, nil)
	core := func(method, path string) string {
		body, _ := json.Marshal(map[string]any{"method": method, "path": path})
		return string(body)
	}
	refused := map[string][]string{"core": {
		`{"method":"POST","path":"/api/services/lock/unlock","body":{"entity_id":"lock.front_door"}}`, `{"method":"post","path":"/api/services/lock/unlock?return_response"}`,
		core("POST", "/api/%73ervices/lock/unlock"), core("POST", "/api/%2573ervices/cover/open_cover"), core("POST", "/api/x/../services/lock/unlock"),
		`{"method":"POST","path":"/api/events/my_event","body":{}}`, core("POST", "/api/webhook/abc"), core("GET", "/api/webhook/abc"),
		core("GET", "/api/backup/download/abc?agent_id=hassio.local"), core("GET", "/api/hassio/backups/abc/download"),
		core("POST", "/api/states/person.me"), core("POST", "/api/intent/handle"), core("POST", "/api/conversation/process"), core("POST", "/api/hassio/core/restart"),
		core("POST", "/api/google_assistant"), core("POST", "/api/alexa/smart_home"), core("POST", "/api/mobile_app/registrations"), core("POST", "/api/cloud/google_actions/sync"),
		`{"method":"POST","path":"/api/config/automation/config/1","body":{}}`, core("DELETE", "/api/config/script/config/leave"),
		`{"method":"POST","path":"/api/events/mobile_app_notification_action","body":{"action":"HANOVA_OK_x"}}`, `{"method":"POST","path":"/api/webhook/abc","body":{"type":"fire_event"}}`,
		core("POST", "/api/services/lock/unlock#/../../../template"), core("POST", "/api/services/lock/unlock?x=1#frag"), core("POST", "/api/events/e#/../../api/template"),
		`{"method":"POST","path":"/api/services/lock/unlock","id":99999999999999999999999}`,
		core("POST", " /api/services/lock/unlock"), core("POST", "/api/serv\tices/lock/unlock"), core("POST", "/api/services/lock/unlock\n"), core("POST", "\x01/api/events/e"),
		core("POST", "/api/config/config_entries/entry/01JABCDEF/reload"), core("POST", "/api/alarmo/area"), core("POST", "/api/zwave_js/node/1/set"),
		core("POST", "/api/frigate/notifications"), core("POST", "/api/config/core/update"),
		core("POST", "/api/config/scene/config/%31"), core("POST", "/api/config/scene/config/x/../../../../services/lock/unlock"),
		core("POST", "/api/config//core/check_config"), core("POST", "/api/config/scene/config/1/"),
	}, "ws": {
		`{"type":"call_service","domain":"lock","service":"unlock","target":{"entity_id":"lock.front_door"}}`, `{"type":" Call_Service ","domain":"lock","service":"unlock"}`,
		`{"type":"execute_script","sequence":[{"action":"lock.unlock"}]}`, `{"message":{"type":"call_service","domain":"lock","service":"unlock"},"collect_events":{"max_events":1}}`,
		`{"type":"fire_event","event_type":"my_event"}`, `{"type":"webhook/list"}`, `{"type":"webhook/handle","webhook_id":"x","body":"{}"}`,
		`{"type":"conversation/process","text":"open the garage"}`, `{"type":"assist_pipeline/run","start_stage":"intent"}`, `{"type":"backup/restore","backup_id":"x"}`,
		`{"type":"supervisor/api","endpoint":"/host/reboot","method":"post"}`, `{"type":"supervisor/api","endpoint":"/addons","method":"get"}`,
		`{"type":"lovelace/dashboards/delete","dashboard_id":"x"}`,
		`{"type":"config/entity_registry/update","entity_id":"lock.front","options_domain":"lock","options":{"default_code":"1234"}}`,
		`{"type":"config/entity_registry/update","entity_id":"alarm_control_panel.home","options_domain":"alarm_control_panel","options":{"default_code":"1"}}`,
		`{"type":"config/entity_registry/update","entity_id":"lock.front","options_domain":"cloud.google_assistant","options":{"disable_2fa":true}}`,
		`{"type":"config/entity_registry/update","entity_id":"light.desk","options_domain":"conversation","options":{"should_expose":true}}`,
		`{"type":"config/entity_registry/update","entity_id":"switch.plug","options_domain":"switch_as_x","options":{"entity_id":"lock.x"}}`,
		`{"type":"config/entity_registry/update","entity_id":"sensor.power","options_domain":"sensor.private","options":{}}`,
		`{"type":"config/entity_registry/update","entity_id":"sensor.power","options_domain":"number","options":{}}`,
		`{"type":"config/entity_registry/update","entity_id":"sensor.power","options":{"display_precision":1}}`,
		`{"type":"config/entity_registry/update","entity_id":"light.desk","options_domain":"light","options":{}}`,
		`{"type":"config/entity_registry/update","entity_id":"cover.garage","device_class":"curtain"}`,
		`{"type":"blueprint/save","domain":"automation","path":"a.yaml","yaml":"x","allow_override":true}`,
		`{"message":{"type":"subscribe_events"},"collect_events":{"max_events":1}}`, `{"message":{"type":"subscribe_events","event_type":"call_service"},"collect_events":{"max_events":1}}`,
		`{"message":{"type":"subscribe_events","event_type":"*"},"collect_events":{"max_events":1}}`,
		`{"message":{"type":"subscribe_trigger","trigger":{"platform":"event","event_type":"call_service"}},"collect_events":{}}`,
		`{"message":{"type":"subscribe_trigger","trigger":{"platform":"event","event_type":"{{ 'mobile_app_' ~ 'x' }}"}},"collect_events":{}}`,
		`{"type":"cloud/status"}`, `{"message":{"type":"mobile_app/push_notification_channel","webhook_id":"x"},"collect_events":{}}`,
		`{"type":"webhook/handle","webhook_id":"x","method":"POST","body":"{}"}`,
		`{"type":"fire_event","event_type":"mobile_app_notification_action","event_data":{"action":"HANOVA_OK_x"}}`,
		`{"type":"call_service","domain":"lock","service":"unlock","id":1e1000}`,
		`{"type":"zwave_js/set_value","device_id":"d","command_class":98,"property":"targetMode","value":0}`,
		`{"type":"zwave_js/set_lock_usercode","entity_id":"lock.front","code_slot":1,"usercode":"1234"}`,
		`{"type":"zha/devices/clusters/attributes/value","ieee":"x","endpoint_id":1,"cluster_id":257,"attribute":0,"value":2}`,
		`{"type":"matter/open_commissioning_window","device_id":"d"}`, `{"type":"esphome/device/command","device_id":"d"}`,
		`{"message":{"type":"zwave_js/set_value","device_id":"d"},"collect_events":{"max_events":1}}`,
		`{"type":"my_integration/do"}`, `{"type":" ping"}`, `{"type":"Get_States"}`, `{"type":"lovelace/config/save","url_path":"x","config":{}}`,
		`{"type":"lovelace/dashboards/create","url_path":"a-b"}`, `{"type":"lovelace/config","force":true}`, `{}`, `{"type":["ping"]}`,
		`{"type":"config/entity_registry/update","entity_id":"button.reboot","device_class":"identify"}`,
		`{"type":"config/entity_registry/update","entity_id":"lock.front","disabled_by":"user"}`,
		`{"type":"lovelace/resources/create","res_type":"module","url":"https://example.com/card.js"}`,
		`{"type":"lovelace/resources/update","resource_id":"x","url":"/local/../api/x"}`,
		`{"type":"hacs/repositories/add","repository":"someone/thing","category":"integration"}`,
	}}
	for _, event := range []string{"mobile_app_notification_action", "MOBILE_APP_NOTIFICATION_received", "mobile_app_notification_cleared", "ios.notification_action_fired"} {
		refused["ws"] = append(refused["ws"], `{"message":{"type":"subscribe_events","event_type":"`+event+`"},"collect_events":{"max_events":1}}`,
			`{"message":{"type":"subscribe_trigger","trigger":{"platform":"event","event_type":"`+event+`"}},"collect_events":{}}`)
	}
	allowed := map[string][]string{"core": {
		core("GET", "/api/events"), core("GET", "/api/states/lock.front_door"), core("GET", "/api/config/automation/config/1"),
		`{"method":"POST","path":"/api/config/config_entries/flow","body":{"handler":"trend"}}`, `{"method":"POST","path":"/api/config/scene/config/1","body":{}}`,
		`{"method":"POST","path":"/api/template","body":{"template":"{{ 1 }}"}}`, core("POST", "/api/template#x"),
		core("DELETE", "/api/config/config_entries/flow/0123abcd"), `{"method":"POST","path":"/api/config/config_entries/options/flow/0123abcd","body":{}}`,
		core("DELETE", "/api/config/config_entries/entry/01JABCDEF"), core("POST", "/api/config/core/check_config"), core("delete", "/api/config/scene/config/1712345"),
	}, "ws": {
		`{"type":"config/entity_registry/update","entity_id":"sensor.power","options_domain":"sensor","options":{"display_precision":1}}`,
		`{"type":"config/entity_registry/update","entity_id":"number.target","options_domain":"number","options":{"unit_of_measurement":"°C"}}`,
		`{"type":"config/entity_registry/update","entity_id":"cover.garage","name":"Garage"}`,
		`{"type":"config/entity_registry/update","entity_id":"binary_sensor.door","device_class":"door"}`,
		`{"type":"blueprint/save","domain":"automation","path":"a.yaml","yaml":"x"}`,
		`{"message":{"type":"subscribe_events","event_type":"zha_event"},"collect_events":{"max_events":1}}`,
		`{"message":{"type":"subscribe_trigger","trigger":{"platform":"state","entity_id":"event.x"}},"collect_events":{}}`,
		`{"type":"ping"}`, `{"type":"config/area_registry/create","name":"Garden"}`, `{"type":"input_boolean/create","name":"Guest"}`,
		`{"type":"config/entity_registry/update","entity_id":"light.desk","name":"Desk","area_id":"office"}`, `{"type":"lovelace/config","url_path":"x"}`,
		`{"type":"config/entity_registry/update","entity_id":"button.doorbell","area_id":"hall"}`, `{"type":"energy/save_prefs","energy_sources":[]}`,
		`{"type":"lovelace/resources/create","res_type":"module","url":"/hacsfiles/mushroom/mushroom.js"}`, `{"type":"lovelace/resources/update","resource_id":"x","res_type":"js"}`,
	}}
	for endpoint, bodies := range refused {
		for _, body := range bodies {
			want := checkRawRelayRequest(endpoint, []byte(body))
			send, refusal, err := guardRawRequest(paths, verbOptions{}, endpoint, []byte(body))
			if want == nil || refusal == nil || refusal.Error() != want.Error() || err != nil || send != nil {
				t.Errorf("%s %s: refusal %v, err %v; want %v", endpoint, body, refusal, err, want)
			}
		}
	}
	for endpoint, bodies := range allowed {
		for _, body := range bodies {
			send, refusal, err := guardRawRequest(paths, verbOptions{}, endpoint, []byte(body))
			if refusal != nil || err != nil || string(send) != body {
				t.Errorf("%s %s: refusal %v, err %v, send %s; want sent unchanged", endpoint, body, refusal, err, send)
			}
		}
	}
	// A write that carries a hidden marker back is refused; a read may.
	if _, refusal, _ := guardRawRequest(paths, verbOptions{}, "ws", []byte(`{"type":"backup/generate","password":"`+secretHidden+`"}`)); refusal == nil || !strings.Contains(refusal.Error(), "HA NOVA hid") {
		t.Errorf("hidden marker in a write: refusal %v", refusal)
	}
	if _, refusal, _ := guardRawRequest(paths, verbOptions{}, "ws", []byte(`{"type":"search/related","item_id":"`+secretHidden+`"}`)); refusal != nil {
		t.Errorf("hidden marker in a read: refused %v", refusal)
	}
	// backups: a snapshot never stores a code; files pass as sent.
	snapshot := `{"action":"save","data":"{\"options\":{\"lock\":{\"default_code\":\"4711\"}}}"}`
	if send, refusal, err := guardRawRequest(paths, verbOptions{}, "backups", []byte(snapshot)); refusal != nil || err != nil || strings.Contains(string(send), "4711") || !strings.Contains(string(send), secretHidden) {
		t.Errorf("backups save: send %s, refusal %v, err %v", send, refusal, err)
	}
	if send, refusal, err := guardRawRequest(paths, verbOptions{}, "files", []byte(`{"action":"read_file","path":"packages/a.yaml"}`)); refusal != nil || err != nil || string(send) != `{"action":"read_file","path":"packages/a.yaml"}` {
		t.Errorf("files read: send %s, refusal %v, err %v", send, refusal, err)
	}
	if _, refusal, err := guardRawRequest(paths, verbOptions{}, "trace", []byte(`{}`)); refusal != nil || err == nil {
		t.Errorf("unknown endpoint: refusal %v, err %v; want an error", refusal, err)
	}
	if n := forwarded.Load(); n != 0 {
		t.Fatalf("the guard sent %d requests; it only reads the registry", n)
	}
}
