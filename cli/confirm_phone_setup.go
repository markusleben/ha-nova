package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"regexp"
	"slices"
	"sort"
)

// Who confirms on a phone, and where (confirm_phone.go). The confirmer is
// the Home Assistant owner: only the owner can arm a pairing in the App
// (nova/src/security/owner-check.ts), so "the user this computer is paired
// for" is the owner there, and the owner is one fixed account the Relay
// can't become. Their phones: the owner's person → its device trackers
// from the Companion app (mobile_app) → each device's legacy notify
// service, named by Home Assistant's own slugify (rendered by Home
// Assistant, so it matches exactly).

// phoneTargetsTemplate lists the owner's Companion phones; the owner id is a
// template variable, never template source.
const phoneTargetsTemplate = `{%- set ns = namespace(out=[]) -%}
{%- set apps = integration_entities('mobile_app') -%}
{%- for p in states.person if p.attributes.user_id == owner -%}
{%- for t in p.attributes.device_trackers | default([], true) if t in apps -%}
{%- set name = device_attr(device_id(t), 'name') -%}
{%- if name -%}{%- set ns.out = ns.out + [{'service': ('mobile_app_' ~ name) | slugify, 'name': name}] -%}{%- endif -%}
{%- endfor -%}
{%- endfor -%}
{{ ns.out | to_json }}`

var phoneServicePattern = regexp.MustCompile(`^mobile_app_[a-z0-9_]+$`)

// checkPhoneRead is the fixed allowlist of the discovery reads.
func checkPhoneRead(endpoint string, payload map[string]any) error {
	switch endpoint {
	case "ws":
		if t := payload["type"]; len(payload) == 1 && (t == "auth/current_user" || t == "config/auth/list") {
			return nil
		}
	case "core":
		body, _ := payload["body"].(map[string]any)
		vars, _ := body["variables"].(map[string]any)
		_, isText := vars["owner"].(string)
		if len(payload) == 3 && payload["method"] == http.MethodPost && payload["path"] == "/api/template" &&
			len(body) == 2 && body["template"] == phoneTargetsTemplate && len(vars) == 1 && isText {
			return nil
		}
	}
	return errors.New("not a phone-confirmation read; nothing was sent")
}

func (s *relaySession) phoneRead(endpoint string, payload map[string]any, into any) error {
	if err := checkPhoneRead(endpoint, payload); err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	data, err := s.send(endpoint, body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, into)
}

type haUser struct {
	ID              string `json:"id"`
	IsOwner         bool   `json:"is_owner"`
	IsActive        bool   `json:"is_active"`
	SystemGenerated bool   `json:"system_generated"`
}

// discoverPhone: ok only when the owner has a phone with the Companion app
// and the Relay is not the owner itself. Any failed read is "no phone".
// setup.owner is set whenever the owner is known, also when !ok: the pin
// is keyed by it.
func discoverPhone(s *relaySession) (phoneSetup, bool) {
	var current haUser
	var users []haUser
	if s.phoneRead("ws", map[string]any{"type": "auth/current_user"}, &current) != nil ||
		s.phoneRead("ws", map[string]any{"type": "config/auth/list"}, &users) != nil {
		return phoneSetup{}, false
	}
	owner := ""
	for _, u := range users {
		if u.IsOwner && u.IsActive && !u.SystemGenerated {
			owner = u.ID
		}
	}
	if owner == "" || current.ID == "" || owner == current.ID {
		return phoneSetup{owner: owner}, false // the Relay acts as the owner: a forged tap would look genuine
	}
	var core struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
	}
	payload := map[string]any{"method": http.MethodPost, "path": "/api/template",
		"body": map[string]any{"template": phoneTargetsTemplate, "variables": map[string]any{"owner": owner}}}
	if s.phoneRead("core", payload, &core) != nil || core.Status != http.StatusOK {
		return phoneSetup{owner: owner}, false
	}
	status, services, err := s.coreGET("/api/services")
	if err != nil || status != http.StatusOK {
		return phoneSetup{owner: owner}, false
	}
	targets := phoneTargetsFrom(core.Body, services)
	return phoneSetup{owner: owner, relayUser: current.ID, targets: targets}, len(targets) > 0
}

// phoneTargetsFrom keeps the rendered targets whose notify service exists.
// rendered is the template's text (a JSON string holding the list), or the
// list itself.
func phoneTargetsFrom(rendered, services json.RawMessage) []phoneTarget {
	var text string
	if json.Unmarshal(rendered, &text) == nil {
		rendered = json.RawMessage(text)
	}
	var found []phoneTarget
	var domains []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if json.Unmarshal(rendered, &found) != nil || json.Unmarshal(services, &domains) != nil {
		return nil
	}
	notify := map[string]bool{}
	for _, d := range domains {
		if d.Domain == "notify" {
			for name := range d.Services {
				notify[name] = true
			}
		}
	}
	seen := map[string]bool{}
	var out []phoneTarget
	for _, t := range found {
		if phoneServicePattern.MatchString(t.Service) && notify[t.Service] && !seen[t.Service] {
			seen[t.Service] = true
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out
}

// The phone pin (config.json top level, keyed by the Home Assistant owner's
// user id, which is unique per Home Assistant — not by the reusable server
// name, so a removed or renamed server profile can't carry it to another
// home): "on" once a phone was found — later a missing phone refuses typed
// plans instead of moving them to a weaker channel — or "off" (the owner
// turned it off on the phone).
const phoneSettingKey = "confirm_phone"

func phonePin(paths runtimePaths, owner string) string {
	if owner == "" {
		return ""
	}
	pins, _ := phonePins(paths)
	return pins[owner]
}

// phonePins: ok=false when config.json can't be read now (mid-transaction,
// unreadable): the pins are unknown, never "none".
func phonePins(paths runtimePaths) (map[string]string, bool) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return nil, os.IsNotExist(err)
	}
	var pins map[string]string
	_ = json.Unmarshal(doc.top[phoneSettingKey], &pins) // unreadable: as if none (setPhonePin starts over the same way)
	return pins, true
}

func setPhonePin(paths runtimePaths, owner, value string) error {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return err
	}
	pins := map[string]string{}
	_ = json.Unmarshal(doc.top[phoneSettingKey], &pins) // unreadable: start over
	top := make(map[string]json.RawMessage, len(doc.top))
	for key, v := range doc.top {
		top[key] = v
	}
	if value == "" {
		delete(pins, owner)
	} else {
		pins[owner] = value
	}
	delete(top, phoneSettingKey)
	if len(pins) > 0 {
		raw, _ := json.Marshal(pins)
		top[phoneSettingKey] = raw
	}
	return writeJSONFileIfUnchanged(paths.ConfigFile, top, 0o600, doc.source)
}

// detectAfterWindow picks the channel where no window opens: the phone,
// else a form, else chat. A pinned phone that is missing now refuses
// (channelPhoneMissing), so breaking the phone's setup in Home Assistant
// can't move a plan to a channel the model can answer.
func detectAfterWindow(s *relaySession) string {
	setup, found := phoneDiscover(s)
	pins, readable := phonePins(s.paths)
	if !readable || setup.owner == "" && slices.Contains(slices.Collect(maps.Values(pins)), "on") {
		// The pin can't be checked (the owner unread, or config.json
		// unreadable now): refuse rather than risk a weaker channel.
		return channelPhoneMissing
	}
	switch pin := pins[setup.owner]; {
	case pin == "off":
	case found:
		// A concurrent planner may have written it first: its "on" counts.
		if pin == "" && setPhonePin(s.paths, setup.owner, "on") != nil && phonePin(s.paths, setup.owner) != "on" {
			return channelPhoneUnpinned // unpinned, a later broken phone setup would move plans to a weaker channel
		}
		return channelPhone
	case pin == "on":
		return channelPhoneMissing
	}
	if formsShown {
		return channelForm
	}
	return channelChat
}

const confirmPhoneUsage = "ha-nova setup confirm-phone on|off"

// runConfirmPhoneSetting: off is confirmed on the phone itself, so a model
// that runs it can't complete it; on (back to automatic) needs none.
func runConfirmPhoneSetting(paths runtimePaths, args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: " + confirmPhoneUsage + "\n\nWhere no confirmation window can open, HA NOVA asks the Home Assistant owner's\nphone (Companion app) to confirm unlocks and deletes: \"on\" (default) uses it when\nfound; \"off\" uses your app's form, or a one-time code in chat that your AI\nassistant can see too. Turning it off is confirmed on the phone.")
		return 0
	}
	if len(args) != 1 || args[0] != "on" && args[0] != "off" {
		printHumanErr("Usage: %s", confirmPhoneUsage)
		return 1
	}
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		printHumanErr("cannot reach HA NOVA (run ha-nova setup first): %s", err)
		return 1
	}
	defer s.close()
	setup, found := phoneDiscover(s)
	if setup.owner == "" {
		printHumanErr("HA NOVA can't read who owns this Home Assistant, so it can't change phone confirmation here. Nothing changed.")
		return 1
	}
	pin := phonePin(paths, setup.owner)
	if args[0] == "on" || pin == "off" {
		if args[0] == "on" && pin == "off" {
			if err := setPhonePin(paths, setup.owner, ""); err != nil {
				printHumanErr("cannot save the setting: %s", err)
				return 1
			}
		}
		printHumanInfo("Phone confirmation is %s.", args[0])
		return 0
	}
	if !found {
		if pin != "on" {
			printHumanInfo("HA NOVA found no phone that can confirm here, so it doesn't use one. Nothing changed.")
			return 0
		}
		printHumanErr("HA NOVA can't reach the phone that confirms, so it can't ask there. Nothing changed. Sign in to the Home Assistant Companion app as the owner on your phone, or, to turn it off anyway, set \"%s\": {\"%s\": \"off\"} in %s yourself.", phoneSettingKey, setup.owner, paths.ConfigFile)
		return 1
	}
	if askPhone(s, setup, "ha-nova-setting", "Turn off phone confirmation? HA NOVA then asks in a window, in your app's form, or with a one-time code in chat that your AI assistant can see too.\n\n"+phoneAsked, cliWindowWait) != phoneConfirmed {
		printHumanErr("Nothing changed: Confirm was not pressed on the phone in time, or the notification could not be sent.")
		return 1
	}
	if err := setPhonePin(paths, setup.owner, "off"); err != nil {
		printHumanErr("cannot save the setting: %s", err)
		return 1
	}
	printHumanInfo("Phone confirmation is off.")
	return 0
}
