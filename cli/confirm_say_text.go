package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Wording and defanging helpers of the confirmation text (confirm_say.go).

// sayData: the values a call sets, once, when no single-target delta shows them.
func sayData(data map[string]any) string {
	var parts []string
	for _, key := range sortedKeys(data) {
		parts = append(parts, sayValue(key)+" "+sayValue(deltaValue(data[key])))
	}
	if len(parts) == 0 {
		return ""
	}
	return " with " + strings.Join(parts, ", ")
}

func sayMore(n int, rest string) string {
	if n == 1 {
		return "There is 1 more thing " + strings.Replace(rest, "need ", "needs ", 1)
	}
	return fmt.Sprintf("There are %d more things %s", n, rest)
}

var sayVerbs = map[string]string{
	"turn_on": "Turn on", "turn_off": "Turn off", "open_cover": "Open", "close_cover": "Close", "stop_cover": "Stop",
	"set_cover_position": "Set the position of", "alarm_disarm": "Disarm", "alarm_trigger": "Set off", "press": "Press",
	"trigger": "Run", "set_value": "Set", "select_option": "Set",
}

// sayAsk: a typed plan's phone question — the goal's verb and object, and
// the first reason it needs a confirmation that the title doesn't already
// say (an automation that also unlocks, a device sibling, an unseen effect).
func sayAsk(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) [3]string {
	verb, object := sayGoal(e, env, plan, subject)
	for _, r := range env.Reasons {
		text, typed := sayReason(e, env, plan, r)
		_, _, phrased := sayActionPhrase(e, env, r)
		domain, _ := splitID(plan.Action.Service)
		target, _ := splitID(r.EntityID)
		said := r.Via == "" && phrased && slices.Equal(plan.Action.EntityIDs, []string{r.EntityID}) && domain == target && phoneButton(verb) != "Confirm"
		if typed && text != "" && !said {
			return [3]string{verb, object, text}
		}
	}
	return [3]string{verb, object, ""}
}

// sayGoalText: sayGoal as one phrase, "Unlock “Front Door”".
func sayGoalText(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) string {
	verb, object := sayGoal(e, env, plan, subject)
	return verb + " " + object
}

func sayVerb(name string) string {
	if v := sayVerbs[name]; v != "" {
		return v
	}
	if strings.HasPrefix(name, "alarm_arm_") {
		return "Arm"
	}
	words := []rune(strings.ReplaceAll(name, "_", " "))
	if len(words) == 0 {
		return "Run"
	}
	return string(unicode.ToUpper(words[0])) + string(words[1:])
}

// sayCoverVerb: only a direction the call proves is named; a position
// change of unknown direction stays "sets the position of".
func sayCoverVerb(name string, data map[string]any, st *entityState) string {
	switch name {
	case "open_cover":
		return "opens"
	case "toggle":
		return "opens or closes"
	case "set_cover_position":
		pos, isNumber := data["position"].(float64)
		current, known := st.attr("current_position").(float64)
		switch {
		case isNumber && pos == 0:
			return "closes"
		case isNumber && known && pos > current:
			return "opens"
		}
		return "sets the position of"
	}
	return "acts on"
}

// sayButtonNoun names what a disruptive button does only as far as its
// device class or name proves; a plain "reset" stays unspecific.
func sayButtonNoun(entityID string, st *entityState) string {
	tokens := " " + strings.Join(strings.FieldsFunc(strings.ToLower(entityID+" "+st.str("friendly_name")), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ") + " "
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(tokens, " "+w+" ") {
				return true
			}
		}
		return false
	}
	switch dc := st.str("device_class"); {
	case dc == "restart" || has("restart", "reboot"):
		return "a button that restarts a device"
	case dc == "update":
		return "a button that starts a device update"
	case has("shutdown", "poweroff", "shut down", "power off"):
		return "a button that switches a device off"
	}
	return "a reset button (HA NOVA can't tell what it resets)"
}

// sayPlainValue: a short token (state, number, unit) needs no quotes.
var sayPlainValue = regexp.MustCompile(`^[\p{L}\p{N}_%.+-]{1,24}$`)

func sayValue(s string) string {
	if t := sayText(s, false); t != "" && sayPlainValue.MatchString(t) {
		return t
	}
	if sayText(s, false) == "" {
		return "“”"
	}
	return sayText(s, true)
}

// sayText defangs a Home Assistant string for the confirmation text: no
// control characters or line breaks, no backticks or quote marks (a name
// cannot close its own quotes), at most 60 characters; quoted when asked.
func sayText(s string, quote bool) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r): // zero-width and bidi controls
			return -1
		case unicode.IsControl(r) || unicode.IsSpace(r):
			return ' '
		case unicode.In(r, unicode.Pi, unicode.Pf) || strings.ContainsRune("`\"″＂〝〞〟", r):
			return '\''
		case r == '[' || r == '<':
			return '('
		case r == ']' || r == '>':
			return ')'
		case strings.ContainsRune("*|#~", r): // no Markdown emphasis, tables, or headings
			return ' '
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "://", ": //") // no autolinks
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > 60 {
		s = string(runes[:59]) + "…"
	}
	if s == "" || !quote {
		return s
	}
	return "“" + s + "”"
}

// sayList: “A”, “B” and “C”, or the first three and "N more".
func sayList(names []string) string {
	if len(names) > 3 {
		return strings.Join(names[:3], ", ") + fmt.Sprintf(" and %d more", len(names)-3)
	}
	if len(names) <= 1 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func appendUnique(list []string, s string) []string {
	for _, have := range list {
		if have == s {
			return list
		}
	}
	return append(list, s)
}

// sayDeviceSibling: "“Gate relay” is on the same device as “Gate” (a gate):
// switching it on may open it". Detail names the access entity.
func sayDeviceSibling(e *tierEngine, env *verbEnvelope, r tierReason, subject string) string {
	action := ""
	for _, m := range env.Members {
		if m.EntityID == r.EntityID && m.Via == r.Via {
			action = m.Action
		}
	}
	if action == "" && env.Action != nil {
		domain, _ := splitID(r.EntityID)
		action = effectiveService(env.Action.Service, domain)
	}
	_, name := splitID(action)
	doing := map[string]string{"press": "pressing it", "turn_on": "switching it on", "toggle": "switching it on"}[name]
	found, other, effect := r.Detail, "a lock, alarm, or door", "may open it"
	if doing == "" {
		doing, effect = "opening it", "may also open it"
	}
	domain, _ := splitID(found)
	switch domain {
	case "lock":
		other, effect = "a lock", "may unlock it"
	case "alarm_control_panel":
		other, effect = "an alarm", "may disarm it"
	case "cover":
		other = map[string]string{"garage": "a garage door", "gate": "a gate", "door": "a door"}[e.states[found].str("device_class")]
		if other == "" {
			other = "a cover that may be a door, gate, or garage"
		}
	}
	if !entityIDPattern.MatchString(found) {
		return subject + " is on the same device as " + other + ": " + doing + " " + effect
	}
	return subject + " is on the same device as " + sayText(e.sayName(env, found), true) + " (" + other + "): " + doing + " " + effect
}

// sayOwnedAction words a Supervisor or raw-MQTT step after what it does.
func sayOwnedAction(env *verbEnvelope, r tierReason) string {
	action := ""
	if env.Action != nil && r.Via == "" {
		action = env.Action.Service
	}
	for _, m := range env.Members {
		if m.EntityID == r.EntityID && m.Via == r.Via {
			action = m.Action
		}
	}
	_, name := splitID(action)
	switch {
	case action == "mqtt.publish":
		return "sends a raw MQTT message"
	case action == "conversation.process":
		return "sends a sentence to Assist that can control any device it understands"
	case action == "recorder.purge" || action == "recorder.purge_entities":
		return "permanently deletes recorder history, which can't be undone"
	case name == "host_shutdown":
		return "shuts down the machine Home Assistant runs on"
	case name == "host_reboot":
		return "restarts the machine Home Assistant runs on"
	case strings.HasPrefix(name, "restore_"):
		return "restores a backup over the current setup"
	case strings.HasPrefix(name, "addon_"):
		return "starts, stops, or changes an App"
	}
	return "runs a Supervisor action"
}

// directOwnedCall: the reason is the call itself, made from its owning
// skill (--skill), not a member it runs.
func directOwnedCall(env *verbEnvelope, r tierReason) bool {
	if r.Via != "" || env.Action == nil {
		return false
	}
	domain, _ := splitID(env.Action.Service)
	return ownerSkill(env.Action.Service) != "" || domain == "hassio"
}
