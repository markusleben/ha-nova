package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// Delivery 3 (docs/work/2026-10-28-confirmation-localization-spec.md): a
// Zigbee2MQTT bridge request reads in everyday words only on an exact match
// of request shape and known keys; anything else reads the generic line
// with the whole request as its detail.

func zigbeeData(request, payload string, extra ...any) map[string]any {
	data := map[string]any{"topic": "zigbee2mqtt/bridge/request/" + request, "payload": payload}
	for i := 0; i+1 < len(extra); i += 2 {
		data[extra[i].(string)] = extra[i+1]
	}
	return data
}

func TestMQTTEverydayRecognizesZigbee2MQTTRequests(t *testing.T) {
	ieee, long := "0x00158d0001a2b3c4", strings.Repeat("Bewegungsmelder ", 2)+"Flur Obergeschoss Nord Ost"
	for _, c := range []struct {
		data                         map[string]any
		line, title, button, details string
	}{
		{zigbeeData("device/options", `{"id":"Rollladen Links Wohnzimmer","options":{"invert_cover":true}}`),
			"change the Zigbee device “Rollladen Links Wohnzimmer”: open and closed are swapped", "Change the Zigbee device “Rollladen Links Wohnzimmer”?", "Change",
			"Zigbee2MQTT request “device/options” (id → “Rollladen Links Wohnzimmer”, options.invert_cover → true), MQTT topic “zigbee2mqtt/bridge/request/device/options”"},
		{zigbeeData("device/options", `{"id":"Hall motion","options":{"invert_cover":false,"occupancy_timeout":90}}`, "qos", float64(1), "retain", false),
			"change the Zigbee device “Hall motion”: open and closed are no longer swapped and it reports no motion 90 seconds after the last motion", "Change the Zigbee device “Hall motion”?", "Change", ""},
		{zigbeeData("device/options", `{"id":"Hall motion","options":{"occupancy_timeout":1}}`),
			"change the Zigbee device “Hall motion”: it reports no motion 1 second after the last motion", "", "", ""},
		{zigbeeData("device/rename", `{"from":"bulb","to":"Hall lamp"}`), "rename the Zigbee device “bulb”: its new name is “Hall lamp”", "Rename the Zigbee device “bulb”?", "Rename", ""},
		{zigbeeData("device/rename", `{"from":"bulb","to":"Hall lamp","homeassistant_rename":false}`), "rename the Zigbee device “bulb”: its new name is “Hall lamp”", "", "", ""},
		{zigbeeData("device/rename", `{"from":"bulb","to":"Hall lamp","homeassistant_rename":true}`),
			"rename the Zigbee device “bulb”: its new name is “Hall lamp”, and its entity IDs in Home Assistant change too, so automations and dashboards that use the old ones need updating", "Rename the Zigbee device “bulb”?", "Rename", ""},
		{zigbeeData("device/rename", `{"from":"`+ieee+`","to":"`+long+`"}`), "rename the Zigbee device “" + ieee + "”: its new name is “" + long + "”", "Rename the Zigbee device “" + ieee + "”?", "", ""},
		{zigbeeData("device/remove", `{"id":"Flurlampe"}`), "remove the Zigbee device “Flurlampe” from the network", "Remove the Zigbee device “Flurlampe” from the network?", "Remove",
			"Zigbee2MQTT request “device/remove” (id → “Flurlampe”), MQTT topic “zigbee2mqtt/bridge/request/device/remove”"},
		{zigbeeData("device/remove", `{"id":"Flurlampe","block":true}`), "remove the Zigbee device “Flurlampe” from the network and block it: it can't rejoin until it is unblocked in Zigbee2MQTT",
			"Remove the Zigbee device “Flurlampe” from the network and block it?", "Remove", ""},
		{zigbeeData("device/remove", `{"id":"Flurlampe","force":true}`), "remove the Zigbee device “Flurlampe” from Zigbee2MQTT only: until it is factory reset, it keeps the network key and can still talk on the network",
			"Remove the Zigbee device “Flurlampe” from Zigbee2MQTT only?", "Remove", ""},
		{zigbeeData("device/remove", `{"id":"Flurlampe","force":true,"block":true}`), "remove the Zigbee device “Flurlampe” from Zigbee2MQTT only and block it: Zigbee2MQTT turns it away if it joins again, but until it is factory reset, it keeps the network key and can still talk on the network",
			"Remove the Zigbee device “Flurlampe” from Zigbee2MQTT only and block it?", "Remove", ""},
		{zigbeeData("device/remove", `{"id":"Flurlampe","force":false,"block":false}`), "remove the Zigbee device “Flurlampe” from the network", "", "", ""},
		{zigbeeData("permit_join", `{"time":254}`), "let new devices join the Zigbee network for 254 seconds", "Let new devices join the Zigbee network for 254 seconds?", "Allow", ""},
		{zigbeeData("permit_join", `{"time":60,"device":"Kitchen plug"}`), "let new devices join the Zigbee network through “Kitchen plug” for 60 seconds", "Let new devices join the Zigbee network through “Kitchen plug” for 60 seconds?", "Allow", ""},
		{zigbeeData("permit_join", `{"time":0}`), "turn joining off, so no new devices can join the Zigbee network", "Turn joining off, so no new devices can join the Zigbee network?", "Confirm", ""},
	} {
		line := mqttEveryday("en", planAction{Service: "mqtt.publish", Data: c.data})
		if got := line.text("en"); got != c.line {
			t.Errorf("%v: line %q, want %q", c.data["payload"], got, c.line)
		}
		if got := line.goal.title("en", ""); c.title != "" && got != c.title || len([]rune(got)) > phoneTitleLimit {
			t.Errorf("%v: title %q, want %q", c.data["payload"], got, c.title)
		}
		if got := line.goal.button("en"); c.button != "" && got != c.button {
			t.Errorf("%v: button %q, want %q", c.data["payload"], got, c.button)
		}
		if c.details != "" && line.detail != c.details {
			t.Errorf("%v: detail %q, want %q", c.data["payload"], line.detail, c.details)
		}
	}
}

func TestMQTTEverydayReadsGenericOtherwise(t *testing.T) {
	for name, data := range map[string]map[string]any{
		"another base topic":      {"topic": "z2m/bridge/request/device/remove", "payload": `{"id":"x"}`},
		"another request":         zigbeeData("device/configure", `{"id":"x"}`),
		"a device topic":          {"topic": "zigbee2mqtt/Flurlampe/set", "payload": `{"state":"OFF"}`},
		"an unknown option":       zigbeeData("device/options", `{"id":"x","options":{"transition":1}}`),
		"one unknown option more": zigbeeData("device/options", `{"id":"x","options":{"invert_cover":true,"transition":1}}`),
		"no options":              zigbeeData("device/options", `{"id":"x","options":{}}`),
		"an option as text":       zigbeeData("device/options", `{"id":"x","options":{"invert_cover":"true"}}`),
		"a negative timeout":      zigbeeData("device/options", `{"id":"x","options":{"occupancy_timeout":-1}}`),
		"a broken timeout":        zigbeeData("device/options", `{"id":"x","options":{"occupancy_timeout":1.5}}`),
		"no timeout at all":       zigbeeData("device/options", `{"id":"x","options":{"occupancy_timeout":0}}`),
		"a name cut to 60":        zigbeeData("device/remove", `{"id":"`+strings.Repeat("a", 61)+`"}`),
		"an extra payload key":    zigbeeData("device/remove", `{"id":"x","transaction":1}`),
		"keep_config":             zigbeeData("device/remove", `{"id":"x","keep_config":true}`),
		"a flag as text":          zigbeeData("device/remove", `{"id":"x","block":"true"}`),
		"no id":                   zigbeeData("device/remove", `{"id":""}`),
		"an id of controls":       zigbeeData("device/remove", "{\"id\":\"\u200b\"}"),
		"a bare-string payload":   zigbeeData("device/remove", `"my_bulb"`),
		"not JSON":                zigbeeData("device/remove", "my_bulb"),
		"a JSON list":             zigbeeData("device/remove", `[{"id":"x"}]`),
		"an object payload":       {"topic": "zigbee2mqtt/bridge/request/device/remove", "payload": map[string]any{"id": "x"}},
		"rename the last joined":  zigbeeData("device/rename", `{"last":true,"to":"x"}`),
		"the 1.x permit_join":     zigbeeData("permit_join", `{"value":true}`),
		"permit_join too long":    zigbeeData("permit_join", `{"time":255}`),
		"permit_join in parts":    zigbeeData("permit_join", `{"time":1.5}`),
		"permit_join off via one": zigbeeData("permit_join", `{"time":0,"device":"bulb"}`),
		"retained":                zigbeeData("device/remove", `{"id":"x"}`, "retain", true),
		"evaluated":               zigbeeData("device/remove", `{"id":"x"}`, "evaluate_payload", true),
		"another field":           zigbeeData("device/remove", `{"id":"x"}`, "payload_template", "x"),
		"qos 3":                   zigbeeData("device/remove", `{"id":"x"}`, "qos", float64(3)),
	} {
		line := mqttEveryday("en", planAction{Service: "mqtt.publish", Data: data})
		if line.goal.verb != "mqtt_send" || line.text("en") != "send an MQTT message" || line.goal.button("en") != "Confirm" ||
			!strings.HasPrefix(line.detail, "MQTT topic “") {
			t.Errorf("%s: %+v", name, line)
		}
	}
	for _, service := range []string{"mqtt.publish", "hassio.addon_restart"} {
		if line := callEveryday("en", nil, planAction{Service: service, EntityIDs: []string{"light.x"}, Data: zigbeeData("device/remove", `{"id":"x"}`)}); line != nil {
			t.Errorf("%s with targets: %+v", service, line)
		}
	}
	retained := mqttEveryday("en", planAction{Service: "mqtt.publish", Data: map[string]any{"topic": "homeassistant/light/x/config", "payload": "", "retain": true, "qos": float64(1)}})
	if want := "MQTT topic “homeassistant/light/x/config”, payload “”, qos → 1, kept on the broker (retained)"; retained.detail != want {
		t.Errorf("retained detail %q, want %q", retained.detail, want)
	}
	if detail := mqttEveryday("en", planAction{Service: "mqtt.publish", Data: map[string]any{"topic": float64(123), "payload": "on"}}).detail; detail != "MQTT topic 123, payload “on”" {
		t.Errorf("number topic detail %q", detail)
	}
	long := strings.Repeat("a", 400)
	if detail := mqttEveryday("en", planAction{Service: "mqtt.publish", Data: map[string]any{"topic": "x/y", "payload": long}}).detail; !strings.HasSuffix(detail, "…” (400 characters)") || len([]rune(detail)) > 360 {
		t.Errorf("long payload detail %q", detail)
	}
}

func TestSupervisorCallEveryday(t *testing.T) {
	for _, c := range []struct {
		name                         string
		data                         map[string]any
		line, title, button, details string
	}{
		{"addon_start", map[string]any{"addon": "core_mosquitto"}, "start the App “core_mosquitto”", "Start the App “core_mosquitto”?", "Start", "Supervisor action “hassio.addon_start”"},
		{"addon_stop", map[string]any{"addon": "core_mosquitto"}, "stop the App “core_mosquitto”", "", "Confirm", ""},
		{"addon_restart", map[string]any{"addon": "core_mosquitto"}, "restart the App “core_mosquitto”", "", "Restart", ""},
		{"host_reboot", map[string]any{}, "restart the machine Home Assistant runs on", "Restart the machine Home Assistant runs on?", "Restart", "Supervisor action “hassio.host_reboot”"},
		{"host_shutdown", nil, "shut down the machine Home Assistant runs on: Home Assistant stays off until someone turns the machine on again", "Shut down the machine Home Assistant runs on?", "Shut down", ""},
		{"addon_restart", map[string]any{"addon": "core_mosquitto", "extra": true}, "run a Supervisor action", "", "Confirm", "Supervisor action “hassio.addon_restart” (addon → “core_mosquitto”, extra → true)"},
		{"addon_restart", map[string]any{"addon": "../core"}, "run a Supervisor action", "", "Confirm", ""},
		{"addon_restart", map[string]any{"addon": "self"}, "run a Supervisor action", "", "", ""},
		{"host_reboot", map[string]any{"force": true}, "run a Supervisor action", "", "Confirm", ""},
	} {
		line := supervisorCallEveryday("en", nil, c.name, planAction{Service: "hassio." + c.name, Data: c.data})
		if got := line.text("en"); got != c.line {
			t.Errorf("%s %v: line %q, want %q", c.name, c.data, got, c.line)
		}
		if got := line.goal.title("en", ""); c.title != "" && got != c.title {
			t.Errorf("%s: title %q, want %q", c.name, got, c.title)
		}
		if got := line.goal.button("en"); c.button != "" && got != c.button {
			t.Errorf("%s %v: button %q, want %q", c.name, c.data, got, c.button)
		}
		if c.details != "" && line.detail != c.details {
			t.Errorf("%s: detail %q, want %q", c.name, line.detail, c.details)
		}
	}
}

// One plan, one text: say, the form's prompt, and the window open with the
// everyday line and its detail; the phone asks the line, its settings first.
func TestEverydayLineOnEverySurface(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"id": "Rollladen Links Wohnzimmer", "options": map[string]any{"invert_cover": true}})
	args := []string{"mqtt.publish", "--skill", "ha-nova:mqtt", "--data-file", draftFile(t, map[string]any{"topic": "zigbee2mqtt/bridge/request/device/options", "payload": string(payload)})}
	opening := "I'm about to change the Zigbee device “Rollladen Links Wohnzimmer”: open and closed are swapped. " +
		"Detail: Zigbee2MQTT request “device/options” (id → “Rollladen Links Wohnzimmer”, options.invert_cover → true), MQTT topic “zigbee2mqtt/bridge/request/device/options”. " +
		"This goes straight to a device or bridge, which HA NOVA can't check, so I need you to confirm it."
	t.Run("say, form, window", func(t *testing.T) {
		h := newWriteHarness(t, newFakeHA())
		w := useWindow(t, windowConfirmed)
		planned := h.plan(args...)
		plan, err := readPlanFile(filepath.Join(planDir(h.paths), planned.Plan.ID+".json"))
		if err != nil || !strings.HasPrefix(planned.Say, opening) || !strings.HasPrefix(plan.Prompt, opening) {
			t.Fatalf("say %q\nprompt %q (%v)", planned.Say, plan.Prompt, err)
		}
		if exit, _ := h.run(runApplyCommand, "--plan", planned.Plan.ID); exit != 0 || len(w.messages) != 1 || !strings.HasPrefix(w.messages[0], opening) {
			t.Fatalf("exit %d windows %q", exit, w.messages)
		}
	})
	t.Run("phone", func(t *testing.T) {
		h := newWriteHarness(t, newFakeHA())
		p := usePhones(t, "mobile_app_iphone")
		planned := h.plan(args...).Plan
		p.answer = func(_, no string) []phoneEvent { return []phoneEvent{tap(no, "owner-id", "REMOTE")} }
		if exit, env := h.run(runApplyCommand, "--plan", planned.ID); exit != 4 || env.Error.Code != "CONFIRM_DECLINED" || len(p.sent) == 0 {
			t.Fatalf("exit %d %+v", exit, env.Error)
		}
		if p.sent[0]["title"] != "Change the Zigbee device “Rollladen Links Wohnzimmer”?" ||
			!strings.HasPrefix(p.sent[0]["message"].(string), "Open and closed are swapped. This goes straight to a device or bridge, which HA NOVA can't check. ") ||
			strings.Contains(p.sent[0]["message"].(string), "Zigbee2MQTT request") {
			t.Fatalf("notification %q / %q", p.sent[0]["title"], p.sent[0]["message"])
		}
	})
}

// A generic line names its request on the phone too (nothing else there
// says what it targets): the body opens with its detail, each value cut for
// the phone, so two different requests never read the same.
func TestGenericLinesNameTheirRequestOnThePhone(t *testing.T) {
	mqtt := func(topic, payload string) []string {
		return []string{"mqtt.publish", "--skill", "ha-nova:mqtt", "--data-file", draftFile(t, map[string]any{"topic": topic, "payload": payload})}
	}
	long := strings.Repeat("x", 400)
	for _, c := range []struct {
		args        []string
		title, body string
	}{
		{mqtt("zigbee2mqtt/Haustür/set", `{"state":"UNLOCK"}`), "Send an MQTT message?", "MQTT topic “zigbee2mqtt/Haustür/set”, payload “{'state':'UNLOCK'}”. This goes straight to a device or bridge, which HA NOVA can't check. "},
		{mqtt("zigbee2mqtt/Flurlampe/set", `{"state":"OFF"}`), "Send an MQTT message?", "MQTT topic “zigbee2mqtt/Flurlampe/set”, payload “{'state':'OFF'}”. This goes straight to a device or bridge, which HA NOVA can't check. "},
		{mqtt("x/y", long), "Send an MQTT message?", "MQTT topic “x/y”, payload “" + strings.Repeat("x", 59) + "…” (400 characters). This goes straight"},
		{[]string{"hassio.addon_restart", "--skill", "ha-nova:service-call", "--set", "addon=core_ssh", "--set", "extra=1"}, "Run a Supervisor action?",
			"Supervisor action “hassio.addon_restart” (addon → “core_ssh”, extra → 1). This starts, stops, or changes an App, which HA NOVA can't check. "},
		{[]string{"hassio.host_reboot", "--skill", "ha-nova:service-call", "--set", "force=true"}, "Run a Supervisor action?",
			"Supervisor action “hassio.host_reboot” (force → true). This restarts the machine Home Assistant runs on, which HA NOVA can't check. "},
	} {
		h := newWriteHarness(t, newFakeHA())
		p := usePhones(t, "mobile_app_iphone")
		planned := h.plan(c.args...).Plan
		p.answer = func(_, no string) []phoneEvent { return []phoneEvent{tap(no, "owner-id", "REMOTE")} }
		if exit, env := h.run(runApplyCommand, "--plan", planned.ID); exit != 4 || env.Error.Code != "CONFIRM_DECLINED" || len(p.sent) == 0 {
			t.Fatalf("%v: exit %d %+v", c.args, exit, env.Error)
		}
		title, body := p.sent[0]["title"].(string), p.sent[0]["message"].(string)
		if title != c.title || len([]rune(title)) > phoneTitleLimit || !strings.HasPrefix(body, c.body) {
			t.Errorf("%v: notification %q / %q", c.args[len(c.args)-1], title, body)
		}
	}
}

// A Supervisor call reads as what it does to the App or host, the App named
// by its device in Home Assistant, with its action as the detail; a host's
// reason then names only the kind of action, so it doesn't repeat the line.
func TestSupervisorCallSaysTheApp(t *testing.T) {
	f := newFakeHA()
	f.devices = []map[string]any{{"id": "d1", "name": "Mosquitto broker", "name_by_user": nil, "identifiers": []any{[]any{"hassio", "core_mosquitto"}}}}
	for _, c := range []struct {
		args []string
		say  string
	}{
		{[]string{"hassio.addon_restart", "--set", "addon=core_mosquitto"}, "I'm about to restart the App “Mosquitto broker” (core_mosquitto). Detail: Supervisor action “hassio.addon_restart”. This starts, stops, or changes an App, which HA NOVA can't check, so I need you to confirm it."},
		{[]string{"hassio.addon_restart", "--set", "addon=core_ssh"}, "I'm about to restart the App “core_ssh”. Detail: Supervisor action “hassio.addon_restart”. This starts, stops, or changes an App"},
		{[]string{"hassio.host_reboot"}, "I'm about to restart the machine Home Assistant runs on. Detail: Supervisor action “hassio.host_reboot”. This runs a Supervisor action, which HA NOVA can't check"},
		{[]string{"hassio.addon_restart", "--set", "addon=core_ssh", "--set", "extra=1"}, "I'm about to run a Supervisor action. Detail: Supervisor action “hassio.addon_restart” (addon → “core_ssh”, extra → 1). This starts, stops, or changes an App, which HA NOVA can't check"},
	} {
		_, env, _, _ := runPlanForTest(t, f, append(c.args, "--skill", "ha-nova:service-call")...)
		if !strings.HasPrefix(env.Say, c.say) {
			t.Errorf("%v: say %q, want %q", c.args, env.Say, c.say)
		}
	}
}

// A raw Supervisor item asks on the phone what it does to the App, and what
// follows from it first in the body.
func TestRawSupervisorAsksTheApp(t *testing.T) {
	paths, _ := readRelay(t, func(endpoint string, body map[string]any) any {
		return map[string]any{"status": 200, "body": map[string]any{"result": "ok", "data": map[string]any{"name": "Mosquitto broker"}}}
	})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	env := verbEnvelope{}
	_, plan, err := computeRawPlan(s, &env, []rawRequest{{Endpoint: "supervisor", Payload: json.RawMessage(`{"method":"POST","path":"/addons/core_mosquitto/uninstall","data":{"remove_config":true}}`)}})
	if err != nil {
		t.Fatal(err)
	}
	ask := phonePlanAsk("en", sayAsk(newTierEngine(s), &env, plan, ""), false)
	if ask.title != "Uninstall the App “Mosquitto broker” (core_mosquitto)?" || ask.button != "Uninstall" || !strings.HasPrefix(ask.body, "Its settings, data, and config folder are deleted. ") {
		t.Fatalf("ask %+v", ask)
	}
	if about, _ := sayWhat(newTierEngine(s), &env, plan, ""); about != "I'm about to uninstall the App “Mosquitto broker” (core_mosquitto): its settings, data, and config folder are deleted. Detail: Supervisor POST “/addons/core_mosquitto/uninstall”." {
		t.Fatalf("say %q", about)
	}
}

// A device delete asks on the phone what it deletes; the copy saved first is
// no step of its own.
func TestDeviceDeleteAsksTheDevice(t *testing.T) {
	paths, _ := deviceDeleteRelay(t, []any{map[string]any{"id": "dlamp", "name": "Hall lamp", "config_entry_id": "E1"}})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	env := verbEnvelope{}
	_, plan, err := computeRawPlan(s, &env, []rawRequest{
		{Endpoint: "backups", Payload: json.RawMessage(`{"action":"save","category":"metadata","name":"auto-device","data":{}}`)},
		{Endpoint: "ws", Payload: json.RawMessage(`{"type":"config/device_registry/remove","device_id":"dlamp"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	ask := phonePlanAsk("en", sayAsk(newTierEngine(s), &env, plan, ""), false)
	if ask.title != "Delete the device “Hall lamp” and its 2 entities?" || ask.button != "Delete" || strings.Contains(ask.body, "Step") {
		t.Fatalf("ask %+v", ask)
	}
}

// An MQTT step says MQTT wherever no everyday line says the call: in a
// script or automation, or a call with targets.
func TestOwnedMQTTReasonSaysMQTTWithoutALine(t *testing.T) {
	call := &verbEnvelope{Action: &planAction{Service: "mqtt.publish"}}
	if got := sayOwnedAction("en", call, tierReason{}); got != "sends an MQTT message straight to a device or bridge" {
		t.Errorf("without a line: %q", got)
	}
	call.everyday = &everydayLine{goal: actionGoal{verb: "mqtt_send"}}
	if got := sayOwnedAction("en", call, tierReason{}); got != "goes straight to a device or bridge" {
		t.Errorf("with a line: %q", got)
	}
	call.Members = []tierMember{{EntityID: "script.night", Via: "script.night", Action: "mqtt.publish"}}
	if got := sayOwnedAction("en", call, tierReason{EntityID: "script.night", Via: "script.night"}); got != "sends an MQTT message straight to a device or bridge" {
		t.Errorf("a member: %q", got)
	}
}

// Apply numbers and counts the steps as the preview did, after the copy.
func TestPlanRawApplyCountsStepsAfterTheCopy(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{"counter/delete": func(map[string]any) any {
		return fakeRelayFailure{"UPSTREAM_WS_COMMAND_ERROR", "not_found: counter b"}
	}})
	useWindow(t, windowConfirmed)
	_, env := planRaw(t, paths, `[{"endpoint":"backups","payload":{"action":"save","category":"helpers","name":"auto-ab","data":{}}},
		{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"a"}},{"endpoint":"ws","payload":{"type":"counter/delete","counter_id":"b"}}]`)
	if !strings.HasPrefix(env.Say, "I'm about to do these 2 steps in Home Assistant, in this order:\nHA NOVA saves a copy first (“helpers/auto-ab”).\n1. ") {
		t.Fatalf("say %q", env.Say)
	}
	exit, applied := applyRaw(t, paths, env)
	if exit != verbExitUnverified || statuses(rawOutcomes(t, applied)) != "done,done,rejected" ||
		applied.Say != "Partly done: 1 step went through; step 2 didn't." || !strings.HasPrefix(applied.Error.Message, "step 2 did not go through") {
		t.Fatalf("exit %d say %q %+v", exit, applied.Say, applied.Error)
	}
}

// Without the copy (--no-snapshot), the window no longer says one is saved.
func TestPlanRawNoSnapshotDropsTheCopySentence(t *testing.T) {
	paths, _ := rawRelay(t, nil)
	_, env := planRaw(t, paths, `[{"endpoint":"backups","payload":{"action":"save","category":"helpers","name":"auto-guest","data":{}}},
		{"endpoint":"ws","payload":{"type":"input_boolean/delete","input_boolean_id":"guest"}}]`)
	plan, err := readPlanFile(filepath.Join(planDir(paths), env.Plan.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	copied, none := confirmText(plan, false), confirmText(plan, true)
	if !strings.Contains(copied, "HA NOVA saves a copy first") || strings.Contains(none, "HA NOVA saves a copy first") || !strings.Contains(none, sayNoCopyNote("en")) {
		t.Fatalf("with the copy %q\nwithout %q", copied, none)
	}
}
