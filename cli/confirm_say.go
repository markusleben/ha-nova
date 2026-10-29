package main

import (
	"cmp"
	"strings"
)

// confirmationSay is the user-facing confirmation text of a plan that needs
// one (envelope field `say`): what would happen, why it needs the user, and
// how to go ahead. Skills relay it instead of writing their own reasons, so
// no agent has to explain the tier (and cites no rule file doing so). Every
// Home Assistant string in it is quoted and defanged (sayText). subject
// names a config item (alias); calls name their resolved targets. It speaks
// the plan's language (textLang).
func confirmationSay(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) string {
	lang := textLang(plan)
	if plan.ConfigAction == nil && frozeNothing(plan.Request, plan.Action) {
		// apply refuses such a plan (UNRESOLVED_TARGET): never ask for it.
		return confirmT(lang, "say.nothing_found")
	}
	parts := sayBody(e, env, plan, subject)
	if plan.Kind == "delete" {
		parts = append(parts, sayCopyNote(lang))
	}
	switch {
	case plan.Confirm == "code" && plan.Channel == channelWindow && phoneLinkedHere(e.s.paths):
		parts = append(parts, sayWindowPhoneAsk(lang, sayGoal(e, env, plan, subject).button(lang), linkedPlatform(e.s.paths)))
	case plan.Confirm == "code" && plan.Channel == channelWindow:
		parts = append(parts, sayWindowAsk(lang))
	case plan.Confirm == "code" && plan.Channel == channelPhone:
		parts = append(parts, sayPhoneAsk(lang, sayGoal(e, env, plan, subject).button(lang), linkedPlatform(e.s.paths)))
	case plan.Confirm == "code": // the code block is always last
		text := strings.Join(parts, " ")
		if plan.Channel == channelChat && chatCall == nil { // a chat app has no note: claude.ai moved it below the code (mcp_plain.go)
			text += "\n" + sayChatNote(lang)
		}
		return demoPrefix(lang, text+sayCodeBlock(lang, plan.code))
	case plan.ConfigAction != nil: // the write card's save question
		parts = append(parts, saySaveAsk(lang))
	default:
		parts = append(parts, sayYesAsk(lang))
	}
	return demoPrefix(lang, strings.Join(parts, " "))
}

// textLang: the language a plan's texts speak; a plan without one (it asks
// nothing) is English.
func textLang(plan storedPlan) string {
	return cmp.Or(plan.Lang, "en")
}

// sayBody: the action, its reasons, and what has not happened yet (the
// fixed shape of say_lines.go up to the ask).
func sayBody(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) []string {
	about, status := sayWhat(e, env, plan, subject)
	return append(append([]string{about}, sayReasons(e, env, plan)...), status)
}

// demoPrefix names the demo house in every confirmation it asks for.
func demoPrefix(lang, text string) string {
	if activeServerIsDemo() && text != "" {
		return confirmT(lang, "say.demo", "text", text)
	}
	return text
}

// confirmationPrompt is a typed plan's code-free text for a confirmation
// form or window (subject: the live item's alias, else the draft's; the
// copy note is added where it is shown, confirmText). Empty when apply
// would refuse the plan anyway.
func confirmationPrompt(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) string {
	if plan.Confirm != "code" || plan.ConfigAction == nil && frozeNothing(plan.Request, plan.Action) {
		return ""
	}
	env.ask = sayAsk(e, env, plan, subject) // a phone plan's question
	return demoPrefix(textLang(plan), strings.Join(sayBody(e, env, plan, subject), " "))
}

// sayGoal: unlock “Front Door”, delete the automation “Night” — the plan's
// action and objects only. subject names a config item.
func sayGoal(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) actionGoal {
	if plan.Kind == "raw" {
		return rawGoal(textLang(plan), env)
	}
	if action := plan.ConfigAction; action != nil {
		name := cmp.Or(sayText(subject, true), sayText(action.EntityID, true), sayText(action.Domain+"."+action.ConfigID, true))
		verb := map[string]string{"delete": "delete", "revert": "revert", "restore": "restore"}[action.Op]
		switch {
		case verb != "":
		case plan.Basis["current"] == "absent" || plan.ConfigRequest != nil && plan.ConfigRequest.Create: // a create (a dashboard's basis is a digest)
			verb = "create"
		default:
			verb = "change"
		}
		return actionGoal{verb: verb, frame: action.Domain, names: []string{name}}
	}
	if env.everyday != nil {
		return env.everyday.goal
	}
	service := plan.Action.Service
	domain, name := splitID(service)
	names := []string{}
	for _, id := range plan.Action.EntityIDs {
		names = append(names, sayText(e.sayName(env, id), true))
	}
	verb, label := sayVerb(name)
	switch {
	case domain == "script" && !scriptEntityServices[name]:
		return actionGoal{verb: "run", frame: "script", names: []string{sayText(e.sayName(env, service), true)}}
	case len(names) == 0 && domain == "homeassistant":
		return actionGoal{verb: verb, label: label, names: []string{"Home Assistant"}}
	case len(names) == 0:
		return actionGoal{verb: "run", names: []string{sayText(service, true)}}
	case service == "script.turn_on":
		return actionGoal{verb: "run", frame: "script", names: names}
	}
	return actionGoal{verb: verb, label: label, names: names}
}

// sayReasons: one sentence per distinct reason; a typed plan's first one
// ends "so I need you to confirm it" (sayNeedsYou).
func sayReasons(e *tierEngine, env *verbEnvelope, plan storedPlan) []string {
	lang := textLang(plan)
	var parts, typed, other []string
	seen := map[string]bool{}
	// "can't check how “Front Door” works" adds nothing once that entity's own
	// typed action ("This unlocks a door") already says why.
	explained := map[string]bool{}
	for _, r := range env.Reasons {
		if _, isTyped, ok := sayActionPhrase(lang, e, env, r); ok && isTyped {
			explained[r.EntityID+"|"+r.Via] = true
		}
	}
	for _, r := range env.Reasons {
		if sayUnseenCodes[r.Code] && explained[r.EntityID+"|"+r.Via] {
			continue
		}
		text, isTyped := sayReason(lang, e, env, plan, r)
		if text == "" || seen[text] {
			continue
		}
		seen[text] = true
		if isTyped {
			typed = append(typed, text)
		} else {
			other = append(other, text)
		}
	}
	if plan.Confirm == "code" {
		// The tier decides the ask, never the clauses: a typed plan whose
		// reasons have no typed sentence still asks for the one-time code.
		first := confirmT(lang, "reason.unchecked")
		if len(typed) > 0 {
			first, typed = typed[0], typed[1:]
		}
		parts = append(parts, sayNeedsYou(lang, first))
	}
	// Every code-tier clause is shown (up to 8); the milder ones fill up to
	// four sentences and the rest are counted, never dropped silently.
	if len(typed) > 8 {
		typed = append(typed[:8], sayMore(lang, len(typed)-8, true))
	}
	if room := max(4-len(typed), 1); len(other) > room {
		other = append(other[:room], sayMore(lang, len(other)-room, false))
	}
	rest := append(typed, other...)
	for _, text := range rest {
		parts = append(parts, text+".")
	}
	return parts
}

// sayWhat: "I'm about to unlock “Front Door” in “Hall”: locked → unlocked."
// and the status that closes the body ("Nothing has run yet.").
func sayWhat(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) (about, status string) {
	lang := textLang(plan)
	if plan.Kind == "raw" {
		return rawSayWhat(lang, env)
	}
	if action := plan.ConfigAction; action != nil {
		if action.Op == "delete" {
			return sayAbout(lang, sayGoal(e, env, plan, subject).text(lang)), sayNotDeleted(lang)
		}
		return sayAbout(lang, sayGoal(e, env, plan, subject).text(lang)), sayNotSaved(lang)
	}
	if line := env.everyday; line != nil {
		return sayAbout(lang, line.text(lang)) + " " + confirmT(lang, "detail.say", "detail", line.detail), sayNotRun(lang)
	}
	domain, verb := splitID(plan.Action.Service)
	targets := plan.Action.EntityIDs
	goal := sayGoal(e, env, plan, "")
	if len(targets) != 1 || domain == "script" && !scriptEntityServices[verb] {
		return sayAbout(lang, sayWithData(lang, goal.text(lang), plan.Action.Data)), sayNotRun(lang)
	}
	for _, t := range env.Resolved {
		if area := sayText(t.Area, true); t.EntityID == targets[0] && area != "" {
			goal.names = []string{confirmT(lang, "say.in_area", "name", goal.names[0], "area", area)}
		}
	}
	changes, flags, warnings := []string{}, "", ""
	for _, d := range env.Deltas {
		change := sayValue(d.Before) + " → " + sayValue(d.After)
		flag, isFlag := sayFlag(lang, d.Field, plan.Action.Data[d.Field])
		switch {
		case d.Field != "state" && d.Before != d.After && isFlag:
			flags += ", " + flag
		case d.Field != "state" && d.Before == "" && d.After != "":
			changes = append(changes, confirmT(lang, "say.field_set", "field", sayValue(d.Field), "value", sayValue(d.After)))
		case d.Field != "state" && d.Before != d.After: // an explicit "" clears the field: shown as “”
			changes = append(changes, confirmT(lang, "say.field_change", "field", sayValue(d.Field), "change", change))
		case d.Field == "state" && d.After != "" && d.Before != d.After && domain != "script":
			changes = append(changes, change)
		}
		if d.Warning != "" { // planDeltas warns of an unavailable or unknown state
			warnings = " " + map[bool]string{true: confirmT(lang, "say.warn_offline"), false: confirmT(lang, "say.warn_unknown")}[d.Before == "unavailable"]
		}
	}
	line := goal.text(lang)
	if len(changes) > 0 {
		line = confirmT(lang, "say.changes", "action", line, "changes", strings.Join(changes, ", "))
	}
	return sayAbout(lang, line+flags) + warnings, sayNotRun(lang)
}

// sayName is the friendly name the run already read, else the id.
func (e *tierEngine) sayName(env *verbEnvelope, id string) string {
	alias, _ := e.configs[id]["alias"].(string)
	names := []string{e.states[id].str("friendly_name"), alias, id}
	for _, t := range env.Resolved {
		if t.EntityID == id {
			names = append([]string{t.Name}, names...)
		}
	}
	for _, name := range names {
		if sayText(name, false) != "" {
			return name
		}
	}
	return id
}
