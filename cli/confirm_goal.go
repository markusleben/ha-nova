package main

import (
	"slices"
	"strings"
	"unicode"
)

// actionGoal is a plan's action as its confirmation sentence, the phone's
// title, and its button name it (docs/work/2026-10-28-confirmation-
// localization-spec.md, Design 5 and 8): a catalog verb, or a label where
// there is none, and the names it acts on. The names stay a list, so a
// title cut for length still counts every one, and a verb's button follows
// its id, never its words.
type actionGoal struct {
	verb  string            // catalog verb id (phrase); "" when raw or label names the action
	raw   string            // a raw step's verb id (rawVerb), phrased by raw.goal
	label string            // the action's own words: an id-derived service name (English), or Home Assistant's name
	named bool              // label is Home Assistant's own name (confirm_words.go), kept as written
	frame string            // what the names are ("script", "automation", "dashboard", "zigbee", "app", "device"); "" = the names alone
	args  map[string]string // a verb's other placeholders, rendered in the plan's language (goal.zigbee_permit: duration)
	names []string          // each sayText-quoted, or Home Assistant itself
	why   string            // a typed plan's reasons the title doesn't say (sayAsk), for the phone
	whole string            // a batch's question as one goal (batchPhoneGoal); verb, label, and names stay empty
}

// goalVerbs: a service's catalog verb by its name, in every domain. Any
// other service is named by its label (sayVerb).
var goalVerbs = map[string]string{
	"turn_on": "turn_on", "turn_off": "turn_off", "toggle": "toggle", "open_cover": "open", "close_cover": "close", "stop_cover": "stop",
	"set_cover_position": "set_position", "open_cover_tilt": "open_tilt", "close_cover_tilt": "close_tilt", "stop_cover_tilt": "stop_tilt",
	"set_cover_tilt_position": "set_tilt", "toggle_cover_tilt": "toggle_tilt", "lock": "lock", "unlock": "unlock", "open": "open",
	"alarm_disarm": "disarm", "alarm_trigger": "set_off", "press": "press", "trigger": "run", "set_value": "set", "select_option": "set",
	"restart": "restart", "stop": "stop",
}

// sayVerb: a service's catalog verb, else its label from the name
// ("set_temperature" → "Set temperature").
func sayVerb(name string) (verb, label string) {
	switch {
	case goalVerbs[name] != "":
		return goalVerbs[name], ""
	case strings.HasPrefix(name, "alarm_arm_"):
		return "arm", ""
	case name == "":
		return "run", ""
	}
	return "", upperFirst(strings.ReplaceAll(name, "_", " "))
}

// text: the action as one phrase, as a sentence continues it ("unlock
// “Front Door”").
func (g actionGoal) text(lang string) string {
	return g.phrase(lang, g.object(lang, sayList(lang, g.names)))
}

// object: names in the goal's frame ("the script “Night”"). An undo has
// frames of its own, as a language may need another case after its "to"
// (German names the item alone: „an „Flurlicht““).
func (g actionGoal) object(lang, names string) string {
	undo := g.verb == "revert"
	switch {
	case g.frame == "":
		return names
	case g.frame == "script" && undo:
		return confirmT(lang, "goal.revert_object.script", "names", names)
	case g.frame == "automation" && undo:
		return confirmT(lang, "goal.revert_object.automation", "names", names)
	case g.frame == "dashboard" && undo:
		return confirmT(lang, "goal.revert_object.dashboard", "names", names)
	case g.frame == "script":
		return confirmT(lang, "goal.object.script", "names", names)
	case g.frame == "automation":
		return confirmT(lang, "goal.object.automation", "names", names)
	case g.frame == "dashboard":
		return confirmT(lang, "goal.object.dashboard", "names", names)
	case g.frame == "zigbee":
		return confirmT(lang, "goal.object.zigbee", "names", names)
	case g.frame == "app":
		return confirmT(lang, "goal.object.app", "names", names)
	case g.frame == "device":
		return confirmT(lang, "goal.object.device", "names", names)
	}
	panic("confirm goal: unknown frame " + g.frame)
}

func (g actionGoal) phrase(lang, object string) string {
	switch g.verb {
	case "":
		if g.raw != "" {
			return confirmT(lang, "raw.goal", "action", rawVerb(lang, g.raw), "object", object)
		}
		action := lowerFirst(g.label)
		if g.named {
			action = g.label
		}
		return confirmT(lang, "goal.label", "action", action, "object", object)
	case "turn_on":
		return confirmT(lang, "goal.turn_on", "object", object)
	case "turn_off":
		return confirmT(lang, "goal.turn_off", "object", object)
	case "toggle":
		return confirmT(lang, "goal.toggle", "object", object)
	case "open":
		return confirmT(lang, "goal.open", "object", object)
	case "close":
		return confirmT(lang, "goal.close", "object", object)
	case "stop":
		return confirmT(lang, "goal.stop", "object", object)
	case "set_position":
		return confirmT(lang, "goal.set_position", "object", object)
	case "open_tilt":
		return confirmT(lang, "goal.open_tilt", "object", object)
	case "close_tilt":
		return confirmT(lang, "goal.close_tilt", "object", object)
	case "stop_tilt":
		return confirmT(lang, "goal.stop_tilt", "object", object)
	case "set_tilt":
		return confirmT(lang, "goal.set_tilt", "object", object)
	case "toggle_tilt":
		return confirmT(lang, "goal.toggle_tilt", "object", object)
	case "lock":
		return confirmT(lang, "goal.lock", "object", object)
	case "unlock":
		return confirmT(lang, "goal.unlock", "object", object)
	case "arm":
		return confirmT(lang, "goal.arm", "object", object)
	case "disarm":
		return confirmT(lang, "goal.disarm", "object", object)
	case "set_off":
		return confirmT(lang, "goal.set_off", "object", object)
	case "press":
		return confirmT(lang, "goal.press", "object", object)
	case "run":
		return confirmT(lang, "goal.run", "object", object)
	case "set":
		return confirmT(lang, "goal.set", "object", object)
	case "restart":
		return confirmT(lang, "goal.restart", "object", object)
	case "create":
		return confirmT(lang, "goal.create", "object", object)
	case "change":
		return confirmT(lang, "goal.change", "object", object)
	case "delete":
		return confirmT(lang, "goal.delete", "object", object)
	case "restore":
		return confirmT(lang, "goal.restore", "object", object)
	case "revert":
		return confirmT(lang, "goal.revert", "object", object)
	case "start":
		return confirmT(lang, "goal.start", "object", object)
	case "install":
		return confirmT(lang, "goal.install", "object", object)
	case "uninstall":
		return confirmT(lang, "goal.uninstall", "object", object)
	case "auto_update_on":
		return confirmT(lang, "goal.auto_update_on", "object", object)
	case "auto_update_off":
		return confirmT(lang, "goal.auto_update_off", "object", object)
	case "app_settings":
		return confirmT(lang, "goal.app_settings", "object", object)
	case "host_reboot":
		return confirmT(lang, "goal.host_reboot")
	case "host_shutdown":
		return confirmT(lang, "goal.host_shutdown")
	case "supervisor_run":
		return confirmT(lang, "goal.supervisor_run")
	case "mqtt_send":
		return confirmT(lang, "goal.mqtt_send")
	case "zigbee_rename":
		return confirmT(lang, "goal.zigbee_rename", "object", object)
	case "zigbee_remove":
		return confirmT(lang, "goal.zigbee_remove", "object", object)
	case "zigbee_remove_block":
		return confirmT(lang, "goal.zigbee_remove_block", "object", object)
	case "zigbee_remove_force":
		return confirmT(lang, "goal.zigbee_remove_force", "object", object)
	case "zigbee_remove_force_block":
		return confirmT(lang, "goal.zigbee_remove_force_block", "object", object)
	case "zigbee_permit":
		return confirmT(lang, "goal.zigbee_permit", "duration", g.args["duration"])
	case "zigbee_permit_via":
		return confirmT(lang, "goal.zigbee_permit_via", "duration", g.args["duration"], "object", object)
	case "zigbee_permit_off":
		return confirmT(lang, "goal.zigbee_permit_off")
	case "device_delete":
		return confirmT(lang, "goal.device_delete", "object", object, "entities", g.args["entities"])
	case "device_delete_children":
		return confirmT(lang, "goal.device_delete_children", "object", object, "children", g.args["children"], "entities", g.args["entities"])
	}
	panic("confirm goal: unknown verb " + g.verb)
}

// title: the phone's question in one lock-screen line ("Unlock “Front
// Door”?"); with prefix, a result's title ("Done: Unlock “Front Door”"),
// cut as its question would be, one rune short for the “?” it drops.
// Too long, it keeps the first name, cut to fit, and counts every other
// one ("Unlock “A” and 6 more?"), so it never understates what a tap
// confirms. The name never goes: with less than phoneNameRoom left for it,
// the object's kind goes first ("Remove “Flurlampe” …" for "Remove the
// Zigbee device “Flurlampe” …").
func (g actionGoal) title(lang, prefix string) string {
	limit := phoneTitleLimit
	line := func(names string) string {
		goal := g.whole
		if goal == "" {
			goal = upperFirst(g.phrase(lang, g.object(lang, names)))
		}
		if prefix == "" {
			return confirmT(lang, "phone.title", "goal", goal)
		}
		return confirmT(lang, "phone.result_title", "prefix", prefix, "goal", goal)
	}
	if prefix != "" {
		limit--
	}
	first := func(name string) string { return name }
	if len(g.names) > 1 {
		first = func(name string) string { return confirmT(lang, "list.more", "list", name, "count", len(g.names)-1) }
	}
	for {
		title := line(sayList(lang, g.names))
		if len([]rune(title)) <= limit || len(g.names) == 0 {
			return title
		}
		room := limit - len([]rune(line(first(""))))
		if room >= phoneNameRoom || g.frame == "" {
			return line(first(phoneCut(g.names[0], room)))
		}
		g.frame = ""
	}
}

// button: the verb's own button, else Confirm.
func (g actionGoal) button(lang string) string {
	if own := g.ownButton(lang); own != "" {
		return own
	}
	return confirmT(lang, "button.confirm")
}

// ownButton: the button that names the action; "" where Confirm answers
// it: a verb that reads like dismissing (close, stop) or is too long for a
// button (set the position of, undo the last change to). A raw step's verb
// and a label are their own button while they are one or two words whose
// id or label doesn't read like dismissing (cancel, ignore, hide); Home
// Assistant's own name never is, as no word list can check it in every
// language (Design 8).
func (g actionGoal) ownButton(lang string) string {
	switch g.verb {
	case "":
		own := g.label
		if g.raw != "" {
			own = upperFirst(rawVerb(lang, g.raw))
		}
		if own == "" || g.named || strings.Count(own, " ") > 1 || slices.ContainsFunc([]string{"cancel", "close", "stop", "dismiss", "ignore", "hide"}, func(v string) bool { return strings.EqualFold(v, g.raw) || strings.EqualFold(v, g.label) }) {
			return ""
		}
		return own
	case "turn_on":
		return confirmT(lang, "button.turn_on")
	case "turn_off":
		return confirmT(lang, "button.turn_off")
	case "toggle":
		return confirmT(lang, "button.toggle")
	case "open":
		return confirmT(lang, "button.open")
	case "lock":
		return confirmT(lang, "button.lock")
	case "unlock":
		return confirmT(lang, "button.unlock")
	case "arm":
		return confirmT(lang, "button.arm")
	case "disarm":
		return confirmT(lang, "button.disarm")
	case "set_off":
		return confirmT(lang, "button.set_off")
	case "press":
		return confirmT(lang, "button.press")
	case "run":
		return confirmT(lang, "button.run")
	case "set":
		return confirmT(lang, "button.set")
	case "restart":
		return confirmT(lang, "button.restart")
	case "create":
		return confirmT(lang, "button.create")
	case "change":
		return confirmT(lang, "button.change")
	case "delete":
		return confirmT(lang, "button.delete")
	case "restore":
		return confirmT(lang, "button.restore")
	case "start":
		return confirmT(lang, "button.start")
	case "install":
		return confirmT(lang, "button.install")
	case "uninstall":
		return confirmT(lang, "button.uninstall")
	case "auto_update_on":
		return confirmT(lang, "button.turn_on")
	case "auto_update_off":
		return confirmT(lang, "button.turn_off")
	case "app_settings":
		return confirmT(lang, "button.change")
	case "host_reboot":
		return confirmT(lang, "button.restart")
	case "host_shutdown":
		return confirmT(lang, "button.shut_down")
	case "zigbee_rename":
		return confirmT(lang, "button.rename")
	case "zigbee_remove", "zigbee_remove_block", "zigbee_remove_force", "zigbee_remove_force_block":
		return confirmT(lang, "button.remove")
	case "zigbee_permit", "zigbee_permit_via":
		return confirmT(lang, "button.allow")
	case "device_delete", "device_delete_children":
		return confirmT(lang, "button.delete")
	}
	return ""
}

// sayList: “A”, “B” and “C”, or the first three and "N more".
func sayList(lang string, names []string) string {
	switch n := len(names); {
	case n > 3:
		return confirmT(lang, "list.more", "list", strings.Join(names[:3], ", "), "count", n-3)
	case n <= 1:
		return strings.Join(names, "")
	default:
		return confirmT(lang, "list.and", "list", strings.Join(names[:n-1], ", "), "last", names[n-1])
	}
}

// upperFirst: "unlock “Door”" → "Unlock “Door”".
func upperFirst(s string) string {
	r := []rune(s)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
