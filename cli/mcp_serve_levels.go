package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Household levels for chat apps (docs/work/2026-10-24-chat-apps-household-
// spec.md): the owner picks a level per allowed user, and the bridge caps it
// by the user's Home Assistant role on every request. The bridge acts with
// the App's administrator access, so Home Assistant's own admin checks never
// run: everything below Manage everything is an allowlist that fails closed.

type chatLevel int

const (
	levelAsk    chatLevel = iota + 1 // Ask only: questions, never a change
	levelUse                         // Use the home: devices, scenes, and scripts; never configuration
	levelManage                      // Manage everything: what the CLI does, for administrators
)

var chatLevelNames = map[chatLevel]string{levelAsk: "ask", levelUse: "use", levelManage: "manage"}

func (l *chatLevel) UnmarshalJSON(raw []byte) error {
	var name string
	if err := json.Unmarshal(raw, &name); err != nil {
		return err
	}
	for level, known := range chatLevelNames {
		if known == name {
			*l = level
			return nil
		}
	}
	return fmt.Errorf("unknown chat-app level %q", name)
}

// roleCap: the most a level gives u, by their Home Assistant role. A missing
// or unknown group caps at Ask only.
func roleCap(u haUser) chatLevel {
	switch {
	case u.IsOwner || slices.Contains(u.GroupIDs, "system-admin"):
		return levelManage
	case slices.Contains(u.GroupIDs, "system-users"):
		return levelUse
	}
	return levelAsk
}

// chatToolLevel: the lowest level each tool needs in a chat app. A tool not
// listed is refused at every level (TestChatLevelsDecideEveryTool).
var chatToolLevel = map[string]chatLevel{
	"ha_skill": levelAsk, "ha_find": levelAsk, "ha_get": levelAsk, "ha_history": levelAsk,
	"ha_call": levelUse, "ha_apply": levelUse,
	"ha_map": levelManage, "ha_trace": levelManage, "ha_read": levelManage, "ha_plan": levelManage,
}

// chatArgLevel: below Manage everything, the level each argument of a tool
// needs; an argument not listed needs Manage everything. ha_get reads state
// only: config, relations, and dashboards show automations and scripts.
var chatArgLevel = map[string]map[string]chatLevel{
	"ha_skill": {"name": levelAsk, "task": levelAsk},
	"ha_find": {"phrase": levelAsk, "domain": levelAsk, "area": levelAsk, "state": levelAsk, "limit": levelAsk,
		"include_disabled": levelAsk, "aliases": levelAsk},
	"ha_get": {"entity_id": levelAsk, "config": levelManage, "related": levelManage, "references": levelManage,
		"dashboard": levelManage, "dashboards": levelManage},
	"ha_history": {"entity_id": levelAsk, "since": levelAsk, "until": levelAsk, "logbook": levelAsk},
	"ha_call": {"service": levelUse, "entity_ids": levelUse, "match": levelUse, "area": levelUse, "device": levelUse,
		"data": levelUse, "response": levelUse, "skill": levelUse},
	"ha_apply": {"plan_id": levelUse, "confirm_code": levelUse, "no_snapshot": levelManage},
}

// chatPlanKinds: per plan kind, the lowest level that plans it (ha_plan) and
// applies it (ha_apply). Only a call applies below Manage everything: its
// plan comes from ha_call's needs_confirmation, and its service and targets
// are checked again at apply. A kind not listed is refused at every level.
var chatPlanKinds = map[string]struct{ plan, apply chatLevel }{
	"call":  {levelManage, levelUse},
	"write": {levelManage, levelManage}, "delete": {levelManage, levelManage}, "revert": {levelManage, levelManage},
	"restore": {levelManage, levelManage}, "rooms": {levelManage, levelManage}, "template": {levelManage, levelManage},
	"raw": {levelManage, levelManage},
	// batch: 2 to 20 automation and script writes and deletes, configuration
	// all of it: never below Manage everything, planned or applied.
	"batch": {levelManage, levelManage},
}

// phoneAllowed: u is this chat app's own user, an active person, and their
// level with u's role as Home Assistant lists it now is Use the home or
// more. A policy that allows nothing (level zero) allows no phone.
func (p *chatPolicy) phoneAllowed(u haUser) bool {
	return p.level > 0 && u.ID == p.user && u.IsActive && !u.SystemGenerated && min(p.granted, roleCap(u)) >= levelUse
}

// chatCodes: a chat code may confirm for p: the owner's switch is on, and
// only at Manage everything.
func (p *chatPolicy) chatCodes() bool { return p.codes && p.level >= levelManage }

// chatMediaSkill: the one skill below Manage everything, ha-nova:media, and
// only on a media player (Markus's call, 2026-10-29).
func chatMediaSkill(skill, service string) bool {
	return skill == "ha-nova:media" && strings.HasPrefix(service, "media_player.")
}

// mayApply: this user's level applies a plan of kind now.
func (p *chatPolicy) mayApply(kind string) bool {
	k, known := chatPlanKinds[kind]
	return known && p.level >= k.apply
}

// chatLevelRefusal: a tool call this user's level doesn't allow, refused
// before anything is read or sent; nil: allowed (or not a chat app).
// ha_apply reads the plan's kind from this user's own plans.
func (srv *mcpServer) chatLevelRefusal(tool mcpTool, args map[string]json.RawMessage) map[string]any {
	c, name := srv.chat, tool.name
	if c == nil {
		return nil
	}
	need, known := chatToolLevel[name]
	if known && need < levelManage {
		for key, raw := range args {
			if argSet(raw) {
				level, listed := chatArgLevel[name][key]
				if !listed {
					level = levelManage
				}
				need = max(need, level)
			}
		}
	}
	switch name {
	case "ha_call": // a skill, or an owned service, is Manage everything's; below it only ha-nova:media on a media player
		if service, skill := text(args, "service"), text(args, "skill"); (skill != "" || ownerSkill(service) != "") && !chatMediaSkill(skill, service) {
			need = levelManage
		}
	case "ha_plan":
		kind, listed := chatPlanKinds[text(args, "kind")]
		known = known && listed
		need = max(need, kind.plan)
		if known && kind.apply <= c.level && c.level < need { // a call: Use the home calls with ha_call
			return chatStopped(name, refusedWith("CHAT_LEVEL", "from a chat app, this Home Assistant user's service calls run through ha_call, not ha_plan",
				"Call ha_call with the same service and targets instead: it runs the call, or asks for the tap on their phone that it needs."))
		}
	case "ha_apply":
		if id := text(args, "plan_id"); planIDPattern.MatchString(id) {
			plan, err := readPlanFile(filepath.Join(planDir(srv.paths), id+".json"))
			kind, listed := chatPlanKinds[plan.Kind]
			switch {
			case errors.Is(err, os.ErrNotExist): // apply says so itself, as for any unknown id
			case err != nil: // unreadable: only Manage everything gets apply's own answer
				need = levelManage
			default:
				known = known && listed
				need = max(need, kind.apply)
			}
		}
	}
	if known && c.level >= need {
		return nil
	}
	return chatStopped(name, chatLevelFailure(c, tool.readOnly && name != "ha_plan"))
}

// argSet: an argument given a value (false, "", 0, and null are not).
func argSet(raw json.RawMessage) bool {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return true
	}
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0
	}
	return true
}

// chatLevelFailure: a read or a change p's level refuses.
func chatLevelFailure(p *chatPolicy, read bool) *verbFailure {
	return refusedWith(chatLevelWords(p, read))
}

// chatLevelWords: chatLevelFailure's code, message, and next step, naming
// what set the level: the owner's choice, or the user's Home Assistant role.
func chatLevelWords(p *chatPolicy, read bool) (code, message, next string) {
	if p.level == 0 { // removed, or the settings unreadable, while the request waited (reread)
		return "CHAT_APPS_REFUSED", "HA NOVA no longer accepts changes from this chat app (the owner's chat-app settings changed or can't be read)",
			"Tell the user nothing changed: the Home Assistant owner changed who may use HA NOVA in chat apps."
	}
	why := "the owner's choice on the HA NOVA page"
	if p.level < p.granted {
		why = "their role in Home Assistant"
	}
	if read {
		return "CHAT_LEVEL", "from a chat app, this Home Assistant user reads devices and their state only, not automations, scripts, dashboards, or settings (" + why + ")",
			"Tell the user HA NOVA can't show them that in a chat app. Their devices and their state are readable with ha_find, ha_get (without config, related, references, or dashboards), and ha_history."
	}
	can := "can ask questions but not change anything"
	if p.level >= levelUse {
		can = "can use most of the home's devices and run its scenes and scripts, but not change device settings, automations, scripts, dashboards, or Home Assistant's settings"
	}
	return "CHAT_LEVEL", "from a chat app, this Home Assistant user " + can + " (" + why + ")",
		"Tell the user nothing changed: I can't change that for you. Someone who manages Home Assistant can."
}

// chatGate (ha-nova serve): the owner's read-only switch, config plans for
// administrators, and this user's level, with the level read again from
// Home Assistant before a change. A refusal, or nil to run the tool.
func (srv *mcpServer) chatGate(tool mcpTool, args map[string]json.RawMessage) map[string]any {
	if srv.chat == nil {
		return nil
	}
	if tool.name == "ha_call" || tool.name == "ha_apply" {
		fresh, code, why := srv.chat.bridge.policy(srv.chat.user, true)
		switch {
		case code == http.StatusServiceUnavailable:
			return chatStopped(tool.name, refusedWith("ACCESS_UNKNOWN", why,
				"Tell the user nothing changed: HA NOVA couldn't check their Home Assistant account just now. They can try again in a moment."))
		case fresh == nil:
			return chatStopped(tool.name, refusedWith("CHAT_APPS_REFUSED", "HA NOVA no longer accepts changes from this Home Assistant user in chat apps",
				"Tell the user nothing changed: the Home Assistant owner changed who may use HA NOVA in chat apps, or their Home Assistant account changed."))
		}
		srv.chat = fresh
	}
	if refusal := srv.chatRefusal(tool); refusal != nil {
		return refusal
	}
	if refusal := srv.chatAdminRefusal(tool.name, args); refusal != nil { // config plans: Home Assistant administrators only
		return refusal
	}
	return srv.chatLevelRefusal(tool, args)
}

// chatPlanFailure: apply's check under the verb lock, on the plan it
// claimed: its kind at this user's level as read then (chatApply and
// chatLevelRefusal check before the lock, when the file may be claimed
// by another apply), and a chat code only at Manage everything with codes
// on. nil: allowed, or not a chat app.
func chatPlanFailure(plan storedPlan) *verbFailure {
	switch {
	case chatCall == nil:
		return nil
	case !chatCall.mayApply(plan.Kind):
		return chatLevelFailure(chatCall, false)
	case plan.Tier == tierTyped.String() && plan.Channel == channelChat && !chatCall.chatCodes():
		return refusedWith("CONFIRM_UNAVAILABLE", chatCodesOffWhy(chatCall), chatCodesOffNext(chatCall))
	}
	return nil
}

// chatDeviceDomains: Use the home controls these devices, every service but
// the ones that change a helper's own config (reload, set_options).
var chatDeviceDomains = map[string]bool{
	"light": true, "switch": true, "fan": true, "cover": true, "lock": true, "alarm_control_panel": true, "valve": true,
	"siren": true, "climate": true, "media_player": true, "vacuum": true, "humidifier": true, "water_heater": true,
	"input_boolean": true, "input_number": true, "input_select": true, "select": true, "number": true, "button": true,
	"input_button": true,
}

// chatCallFailure: below Manage everything, a chat app's service call stays
// inside Use the home's allowlist, on the service and every target the tier
// engine resolved, before anything is sent: in ha_call and again in apply's
// recompute, with the level read again; never on a target Home Assistant
// marks as a device's configuration or diagnostics, and with no skill but
// ha-nova:media on a media player. An entity registry Home Assistant didn't
// answer decides nothing: refused. nil: allowed, or not a chat app.
func chatCallFailure(req planCallRequest, targets []verbTarget, e *tierEngine) *verbFailure {
	if chatCall == nil || chatCall.level >= levelManage {
		return nil
	}
	settings := slices.ContainsFunc(targets, func(t verbTarget) bool { return e.reg[t.EntityID].categorized })
	switch {
	case chatCall.level < levelUse, req.Skill != "" && !chatMediaSkill(req.Skill, req.Service), settings, !useTheHome(req.Service, targets):
		return chatLevelFailure(chatCall, false)
	case e.regFailed && len(targets) > 0:
		return refusedWith("ACCESS_UNKNOWN", "HA NOVA couldn't read Home Assistant's entity registry, so it can't tell whether a target is a device's own settings; nothing was sent",
			"Tell the user nothing changed: HA NOVA couldn't check this just now. They can try again in a moment.")
	}
	return nil
}

// useTheHome: Use the home's allowlist. Running what exists (a scene, a
// script, an automation's trigger), never turning one off or reloading it;
// homeassistant.turn_on/turn_off/toggle only on devices.
func useTheHome(service string, targets []verbTarget) bool {
	domain, name := splitID(service)
	all := func(ok func(string) bool) bool {
		for _, t := range targets {
			if d, _ := splitID(t.EntityID); !ok(d) {
				return false
			}
		}
		return true
	}
	is := func(want string) func(string) bool { return func(d string) bool { return d == want } }
	switch {
	case chatDeviceDomains[domain]:
		return name != "reload" && name != "set_options" && len(targets) > 0 && all(is(domain))
	case service == "scene.turn_on", service == "script.turn_on", service == "automation.trigger":
		return len(targets) > 0 && all(is(domain))
	case domain == "script" && !scriptEntityServices[name]: // a script run by its own name
		return all(is("script"))
	case domain == "homeassistant" && (name == "turn_on" || name == "turn_off" || name == "toggle"):
		return len(targets) > 0 && all(func(d string) bool { return chatDeviceDomains[d] })
	}
	return false
}
