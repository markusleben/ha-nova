package main

import (
	"cmp"
	"encoding/json"
	"math"
	"regexp"
	"slices"
	"strings"
)

// Everyday lines (docs/work/2026-10-28-confirmation-localization-spec.md,
// Delivery 3): a raw MQTT message or a Supervisor call opens with what
// happens in the home, then its detail. A line comes only from an exact
// match of request shape and known keys (Design 7); anything else reads the
// generic line with the whole request as its detail. The tier never depends
// on a line.

// everydayLine: the goal every surface asks, what completes it (the
// settings a change sets, or what follows from it: said after the goal, and
// the phone body's first sentence), and the detail.
type everydayLine struct {
	goal   actionGoal
	more   string
	detail string
}

// text: the line as the chat, window, and form say it.
func (l everydayLine) text(lang string) string {
	if l.more == "" {
		return l.goal.text(lang)
	}
	return confirmT(lang, "everyday.more", "action", l.goal.text(lang), "more", l.more)
}

// callEveryday: the everyday line of an mqtt.publish or hassio.* call; nil
// for any other service, and for a call with targets (neither service takes
// one), which reads as today with its targets named.
func callEveryday(lang string, s *relaySession, action planAction) *everydayLine {
	domain, name := splitID(action.Service)
	var line everydayLine
	switch {
	case len(action.EntityIDs) > 0:
		return nil
	case action.Service == "mqtt.publish":
		line = mqttEveryday(lang, action)
	case domain == "hassio":
		line = supervisorCallEveryday(lang, s, name, action)
	default:
		return nil
	}
	return &line
}

// zigbeeBridge: the Zigbee2MQTT bridge requests on the default base topic
// (Zigbee2MQTT docs, MQTT topics and messages, 2026-10). Another base topic
// reads generic: HA NOVA can't tell it is Zigbee2MQTT's.
const zigbeeBridge = "zigbee2mqtt/bridge/request/"

// mqttEveryday: a Zigbee2MQTT bridge request in everyday words when the
// call is exactly topic and payload (qos allowed; retain and
// evaluate_payload absent or false) and the payload a string holding one
// JSON object with exactly the request's keys; else the generic line.
func mqttEveryday(lang string, action planAction) everydayLine {
	data := action.Data
	generic := everydayLine{goal: actionGoal{verb: "mqtt_send"}, detail: mqttDetail(lang, data)}
	for key, value := range data {
		switch key {
		case "topic", "payload":
		case "qos":
			if q, ok := value.(float64); !ok || q != 0 && q != 1 && q != 2 {
				return generic
			}
		case "retain", "evaluate_payload":
			if value != false {
				return generic
			}
		default:
			return generic
		}
	}
	topic, _ := data["topic"].(string)
	payload, isText := data["payload"].(string)
	request, bridge := strings.CutPrefix(topic, zigbeeBridge)
	var body map[string]any
	if !bridge || !isText || json.Unmarshal([]byte(payload), &body) != nil || body == nil {
		return generic
	}
	goal, more, ok := zigbeeGoal(lang, request, body)
	if !ok {
		return generic
	}
	return everydayLine{goal: goal, more: more,
		detail: confirmT(lang, "detail.zigbee", "request", sayText(request, true), "fields", detailFields(lang, "", body), "topic", sayDetail(lang, topic, true))}
}

// zigbeeGoal: one bridge request's goal; ok is false for any request, key,
// or value type the table doesn't hold.
func zigbeeGoal(lang, request string, body map[string]any) (goal actionGoal, more string, ok bool) {
	only := func(required []string, optional ...string) bool {
		for _, key := range required {
			if _, set := body[key]; !set {
				return false
			}
		}
		for key := range body {
			if !slices.Contains(required, key) && !slices.Contains(optional, key) {
				return false
			}
		}
		return true
	}
	name := func(key string) (string, bool) { // a name the line shows whole
		text, isText := body[key].(string)
		return sayText(text, true), isText && sayText(text, false) != "" && len([]rune(text)) <= 60
	}
	flag := func(key string) (on, ok bool) { // absent reads false
		value, set := body[key]
		if !set {
			return false, true
		}
		on, ok = value.(bool)
		return on, ok
	}
	whole := func(key string, most float64) (int, bool) {
		n, isNumber := body[key].(float64)
		return int(n), isNumber && n >= 0 && n <= most && n == math.Trunc(n)
	}
	switch request {
	case "device/options":
		id, named := name("id")
		options, _ := body["options"].(map[string]any)
		if !only([]string{"id", "options"}) || !named || len(options) == 0 {
			return goal, "", false
		}
		var parts []string
		for _, key := range sortedKeys(options) {
			switch value := options[key]; key {
			case "invert_cover":
				on, isBool := value.(bool)
				if !isBool {
					return goal, "", false
				}
				parts = append(parts, map[bool]string{true: confirmT(lang, "everyday.zigbee.invert_cover_on"), false: confirmT(lang, "everyday.zigbee.invert_cover_off")}[on])
			case "occupancy_timeout":
				seconds, isNumber := value.(float64)
				if !isNumber || seconds < 1 || seconds > 86400 || seconds != math.Trunc(seconds) { // 0 turns the timer off
					return goal, "", false
				}
				parts = append(parts, confirmT(lang, "everyday.zigbee.occupancy_timeout", "count", int(seconds)))
			default:
				return goal, "", false
			}
		}
		return actionGoal{verb: "change", frame: "zigbee", names: []string{id}}, sayList(lang, parts), true
	case "device/rename":
		from, okFrom := name("from")
		to, okTo := name("to")
		ha, okHA := flag("homeassistant_rename")
		if !only([]string{"from", "to"}, "homeassistant_rename") || !okFrom || !okTo || !okHA {
			return goal, "", false
		}
		more := confirmT(lang, "everyday.zigbee.new_name", "to", to)
		if ha {
			more = confirmT(lang, "everyday.zigbee.new_name_ha", "to", to)
		}
		return actionGoal{verb: "zigbee_rename", frame: "zigbee", names: []string{from}}, more, true
	case "device/remove":
		id, named := name("id")
		force, okForce := flag("force")
		block, okBlock := flag("block")
		if !only([]string{"id"}, "force", "block") || !named || !okForce || !okBlock {
			return goal, "", false
		}
		line := map[[2]bool][2]string{ // verb and more: what follows stays out of the title, so it keeps the name
			{false, false}: {"zigbee_remove"},
			{false, true}:  {"zigbee_remove_block", confirmT(lang, "everyday.zigbee.blocked")},
			{true, false}:  {"zigbee_remove_force", confirmT(lang, "everyday.zigbee.force_kept")},
			{true, true}:   {"zigbee_remove_force_block", confirmT(lang, "everyday.zigbee.force_blocked")},
		}[[2]bool{force, block}]
		return actionGoal{verb: line[0], frame: "zigbee", names: []string{id}}, line[1], true
	case "permit_join":
		seconds, timed := whole("time", 254)
		device, via := name("device")
		_, through := body["device"]
		switch {
		case !only([]string{"time"}, "device") || !timed || through && (!via || seconds == 0):
			return goal, "", false
		case seconds == 0:
			return actionGoal{verb: "zigbee_permit_off"}, "", true
		case via:
			return actionGoal{verb: "zigbee_permit_via", names: []string{device}, args: map[string]string{"duration": confirmT(lang, "unit.seconds", "count", seconds)}}, "", true
		}
		return actionGoal{verb: "zigbee_permit", args: map[string]string{"duration": confirmT(lang, "unit.seconds", "count", seconds)}}, "", true
	}
	return goal, "", false
}

// mqttDetail: the generic line's detail, the whole call: topic, payload,
// and every other field as sent.
func mqttDetail(lang string, data map[string]any) string {
	parts := []string{confirmT(lang, "detail.mqtt", "topic", detailValue(lang, data["topic"]), "payload", detailValue(lang, data["payload"]))}
	for _, key := range sortedKeys(data) {
		switch {
		case key == "topic" || key == "payload":
		case key == "retain" && data[key] == true:
			parts = append(parts, confirmT(lang, "detail.retained"))
		default:
			parts = append(parts, confirmT(lang, "detail.field", "key", sayText(key, false), "value", detailValue(lang, data[key])))
		}
	}
	return strings.Join(parts, ", ")
}

var appSlug = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// supervisorCallEveryday: the five hassio.* calls ha-nova call runs, with
// exactly their data (rawSupervisorRoutes words the raw Supervisor items).
func supervisorCallEveryday(lang string, s *relaySession, name string, action planAction) everydayLine {
	service := sayText("hassio."+name, true)
	slug, _ := action.Data["addon"].(string)
	verb := map[string]string{"addon_start": "start", "addon_stop": "stop", "addon_restart": "restart"}[name]
	switch {
	case verb != "" && len(action.Data) == 1 && appSlug.MatchString(slug) && !strings.EqualFold(slug, "self"):
		return everydayLine{goal: actionGoal{verb: verb, frame: "app", names: []string{appLabel(lang, appDeviceName(s, slug), slug)}},
			detail: confirmT(lang, "detail.supervisor_call", "service", service)}
	case name == "host_reboot" && len(action.Data) == 0:
		return everydayLine{goal: actionGoal{verb: name}, detail: confirmT(lang, "detail.supervisor_call", "service", service)}
	case name == "host_shutdown" && len(action.Data) == 0:
		return everydayLine{goal: actionGoal{verb: name}, more: confirmT(lang, "everyday.host_shutdown"), detail: confirmT(lang, "detail.supervisor_call", "service", service)}
	}
	if len(action.Data) == 0 {
		return everydayLine{goal: actionGoal{verb: "supervisor_run"}, detail: confirmT(lang, "detail.supervisor_call", "service", service)}
	}
	return everydayLine{goal: actionGoal{verb: "supervisor_run"}, detail: confirmT(lang, "detail.supervisor_call_data", "service", service, "fields", detailFields(lang, "", action.Data))}
}

// appLabel names an App by its name and slug, or by its slug alone when no
// name could be read.
func appLabel(lang, name, slug string) string {
	if sayText(name, false) != "" {
		return confirmT(lang, "everyday.app_name", "name", sayText(name, true), "slug", slug)
	}
	return sayText(slug, true)
}

// appDeviceName: the name of the App's device in Home Assistant (the
// Supervisor integration keeps one per installed App, identified
// ["hassio", slug]), read like every other plan read (I1), over Home
// Assistant Link too; "" when there is none or the registry can't be read.
func appDeviceName(s *relaySession, slug string) string {
	var rows []struct {
		Name        string  `json:"name"`
		NameByUser  string  `json:"name_by_user"`
		Identifiers [][]any `json:"identifiers"`
	}
	if s == nil || s.wsDecode(map[string]any{"type": "config/device_registry/list"}, &rows, "the device registry") != nil {
		return ""
	}
	for _, row := range rows {
		for _, id := range row.Identifiers {
			if len(id) == 2 && id[0] == "hassio" && id[1] == slug {
				return cmp.Or(row.NameByUser, row.Name)
			}
		}
	}
	return ""
}

// appStoreName: the App's name in the Supervisor's store, which knows
// installed and new Apps alike (a raw Supervisor item's read); "" when
// unreadable. slug is a checked App slug (rawSupervisorMatch).
func appStoreName(s *relaySession, slug string) string {
	var info struct {
		Body struct {
			Data struct {
				Name string `json:"name"`
			} `json:"data"`
		} `json:"body"`
	}
	if s == nil {
		return ""
	}
	raw, err := s.sendWithin("supervisor", []byte(`{"method":"GET","path":"/store/addons/`+slug+`"}`), relaySessionRequestTimeout)
	if err != nil || json.Unmarshal(raw, &info) != nil || sayText(info.Body.Data.Name, false) == "" {
		return ""
	}
	return info.Body.Data.Name
}

// detailFields: "id → “Hall lamp”, options.invert_cover → true": nested
// settings spelled by their path.
func detailFields(lang, path string, fields map[string]any) string {
	var parts []string
	for _, key := range sortedKeys(fields) {
		if nested, ok := fields[key].(map[string]any); ok && len(nested) > 0 {
			parts = append(parts, detailFields(lang, path+key+".", nested))
			continue
		}
		parts = append(parts, confirmT(lang, "detail.field", "key", sayText(path+key, false), "value", detailValue(lang, fields[key])))
	}
	return strings.Join(parts, ", ")
}

// detailValue: a text quoted, anything else as JSON, defanged; up to 300
// characters (sayDetail).
func detailValue(lang string, value any) string {
	if text, ok := value.(string); ok {
		return sayDetail(lang, text, true)
	}
	return sayDetail(lang, detailText(value), false)
}

func detailText(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

// sayDetail: a request's text on a detail line, defanged as sayText but
// up to 300 characters; a longer one ends in "…" and states its length.
func sayDetail(lang, s string, quote bool) string {
	shown := sayTextUpTo(s, 300, quote)
	switch n := len([]rune(s)); {
	case shown == "" && quote:
		return "“”"
	case n > 300:
		return confirmT(lang, "detail.cut", "text", shown, "count", n)
	}
	return shown
}
