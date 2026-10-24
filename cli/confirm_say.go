package main

import (
	"cmp"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// confirmationSay is the user-facing confirmation text of a plan that needs
// one (envelope field `say`): what would happen, why it needs the user, and
// how to go ahead. Skills relay it instead of writing their own reasons, so
// no agent has to explain the tier (and cites no rule file doing so). Every
// Home Assistant string in it is quoted and defanged (sayText). subject
// names a config item (alias); calls name their resolved targets.
func confirmationSay(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) string {
	if plan.ConfigAction == nil && frozeNothing(plan.Request, plan.Action) {
		// apply refuses such a plan (UNRESOLVED_TARGET): never ask for it.
		return sayNotDone("I couldn't find what you named") + " Tell me which devices you mean."
	}
	parts := sayBody(e, env, plan, subject)
	if plan.Kind == "delete" {
		parts = append(parts, sayCopyNote)
	}
	switch {
	case plan.Confirm == "code" && plan.Channel == channelWindow && phoneLinkedHere(e.s.paths):
		verb, _ := sayGoal(e, env, plan, subject)
		parts = append(parts, sayWindowPhoneAsk(phoneButton(verb), linkedPlatform(e.s.paths)))
	case plan.Confirm == "code" && plan.Channel == channelWindow:
		parts = append(parts, sayWindowAsk)
	case plan.Confirm == "code" && plan.Channel == channelPhone:
		verb, _ := sayGoal(e, env, plan, subject)
		parts = append(parts, sayPhoneAsk(phoneButton(verb), linkedPlatform(e.s.paths)))
	case plan.Confirm == "code": // the code block is always last
		text := strings.Join(parts, " ")
		if plan.Channel == channelChat && chatCall == nil { // a chat app has no note: claude.ai moved it below the code (mcp_plain.go)
			text += "\n" + sayChatNote
		}
		return demoPrefix(text + sayCodeBlock(plan.code))
	case plan.ConfigAction != nil: // the write card's save question
		parts = append(parts, saySaveAsk)
	default:
		parts = append(parts, sayYesAsk)
	}
	return demoPrefix(strings.Join(parts, " "))
}

// sayBody: the action, its reasons, and what has not happened yet (the
// fixed shape of say_lines.go up to the ask).
func sayBody(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) []string {
	about, status := sayWhat(e, env, plan, subject)
	return append(append([]string{about}, sayReasons(e, env, plan)...), status)
}

// demoPrefix names the demo house in every confirmation it asks for.
func demoPrefix(text string) string {
	if activeServerIsDemo() && text != "" {
		return "In the demo house: " + text
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
	return demoPrefix(strings.Join(sayBody(e, env, plan, subject), " "))
}

// sayGoal: "Unlock" “Front Door”, "Delete" the automation “Night” — the
// plan's action and objects only. subject names a config item.
func sayGoal(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) (verb, object string) {
	if action := plan.ConfigAction; action != nil {
		name := cmp.Or(sayText(subject, true), sayText(action.EntityID, true), sayText(action.Domain+"."+action.ConfigID, true))
		verb := map[string]string{"delete": "Delete", "revert": "Undo the last change to", "restore": "Restore"}[action.Op]
		switch {
		case verb != "":
		case plan.Basis["current"] == "absent" || plan.ConfigRequest != nil && plan.ConfigRequest.Create: // a create (a dashboard's basis is a digest)
			verb = "Create"
		default:
			verb = "Change"
		}
		return verb, "the " + action.Domain + " " + name
	}
	service := plan.Action.Service
	domain, verb := splitID(service)
	names := []string{}
	for _, id := range plan.Action.EntityIDs {
		names = append(names, sayText(e.sayName(env, id), true))
	}
	switch {
	case domain == "script" && !scriptEntityServices[verb]:
		return "Run", "the script " + sayText(e.sayName(env, service), true)
	case len(names) == 0 && domain == "homeassistant":
		return sayVerb(verb), "Home Assistant"
	case len(names) == 0:
		return "Run", sayText(service, true)
	case service == "script.turn_on":
		return "Run", "the script " + sayList(names)
	}
	return sayVerb(verb), sayList(names)
}

// sayReasons: one sentence per distinct reason; a typed plan's first one
// ends "so I need you to confirm it" (sayNeedsYou).
func sayReasons(e *tierEngine, env *verbEnvelope, plan storedPlan) []string {
	var parts, typed, other []string
	seen := map[string]bool{}
	// "can't check how “Front Door” works" adds nothing once that entity's own
	// typed action ("This unlocks a door") already says why.
	explained := map[string]bool{}
	for _, r := range env.Reasons {
		if _, isTyped, ok := sayActionPhrase(e, env, r); ok && isTyped {
			explained[r.EntityID+"|"+r.Via] = true
		}
	}
	for _, r := range env.Reasons {
		if sayUnseenCodes[r.Code] && explained[r.EntityID+"|"+r.Via] {
			continue
		}
		text, isTyped := sayReason(e, env, plan, r)
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
		first := "HA NOVA can't fully check what this would do"
		if len(typed) > 0 {
			first, typed = typed[0], typed[1:]
		}
		parts = append(parts, first+sayNeedsYou)
	}
	// Every code-tier clause is shown (up to 8); the milder ones fill up to
	// four sentences and the rest are counted, never dropped silently.
	if len(typed) > 8 {
		typed = append(typed[:8], sayMore(len(typed)-8, "that need your confirmation; the preview lists them"))
	}
	if room := max(4-len(typed), 1); len(other) > room {
		other = append(other[:room], sayMore(len(other)-room, "the preview lists"))
	}
	rest := append(typed, other...)
	for _, text := range rest {
		parts = append(parts, text+".")
	}
	return parts
}

// sayWhat: "I'm about to unlock “Front Door” in Hall: locked → unlocked."
// and the status that closes the body ("Nothing has run yet.").
func sayWhat(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) (about, status string) {
	if action := plan.ConfigAction; action != nil {
		if action.Op == "delete" {
			return sayAbout(sayGoalText(e, env, plan, subject)), sayNotDeleted
		}
		return sayAbout(sayGoalText(e, env, plan, subject)), sayNotSaved
	}
	domain, verb := splitID(plan.Action.Service)
	targets := plan.Action.EntityIDs
	line := sayGoalText(e, env, plan, "")
	if len(targets) != 1 || domain == "script" && !scriptEntityServices[verb] {
		return sayAbout(line + sayData(plan.Action.Data)), sayNotRun
	}
	for _, t := range env.Resolved {
		if area := sayText(t.Area, true); t.EntityID == targets[0] && area != "" {
			line += " in " + area
		}
	}
	changes, flags, warnings := []string{}, "", ""
	for _, d := range env.Deltas {
		change := sayValue(d.Before) + " → " + sayValue(d.After)
		flag, isFlag := sayFlag(d.Field, d.After)
		switch {
		case d.Field != "state" && d.Before != d.After && isFlag:
			flags += ", " + flag
		case d.Field != "state" && d.Before == "" && d.After != "":
			changes = append(changes, sayValue(d.Field)+" set to "+sayValue(d.After))
		case d.Field != "state" && d.Before != d.After: // an explicit "" clears the field: shown as “”
			changes = append(changes, sayValue(d.Field)+" "+change)
		case d.Field == "state" && d.After != "" && d.Before != d.After && domain != "script":
			changes = append(changes, change)
		}
		if d.Warning != "" {
			warnings = " " + d.Warning
		}
	}
	if len(changes) > 0 {
		line += ": " + strings.Join(changes, ", ")
	}
	return sayAbout(line+flags) + warnings, sayNotRun
}

// sayReason is one plain clause per reason code; typed reports whether the
// code puts a plan on the code tier. Codes that only refuse never reach a
// plan and have no clause (TestConfirmationSayCoversEveryPlanReason).
func sayReason(e *tierEngine, env *verbEnvelope, plan storedPlan, r tierReason) (string, bool) {
	name := ""
	if r.EntityID != "" {
		name = sayText(e.sayName(env, r.EntityID), true)
	}
	if phrase, typed, ok := sayActionPhrase(e, env, r); ok {
		if via := sayVia(e, env, plan, r.Via); via != "" {
			object := phrase.noun
			if name != "" && phrase.named {
				object = name
			} else if name != "" {
				object = name + " (" + phrase.noun + ")" // the name never hides what it is
			}
			return "This also " + phrase.verb + " " + object + " through " + via, typed
		}
		return "This " + phrase.verb + " " + phrase.noun, typed
	}
	subject := "This"
	if name != "" {
		subject = name
	}
	via, through := sayVia(e, env, plan, r.Via), ""
	if via != "" {
		through = ", through " + via
	}
	if sayUnseenCodes[r.Code] {
		domain, _ := splitID(r.EntityID)
		return sayUnseen(name, via, sayPlainDomains[domain]), true
	}
	if checkGapCodes[r.Code] {
		return sayCheckGap, true
	}
	switch r.Code {
	case reasonUserService:
		return "This runs a service defined on the device itself, which HA NOVA can't read", true
	case reasonConfirmWatch:
		return "It watches service calls or phone notifications, so it could see another confirmation's buttons", true
	case reasonSceneAccess:
		return "HA NOVA can't read the scene " + name + ", and its integration also controls locks, alarms, gates, or doors", true
	case reasonReload:
		return "A reload or restart can change what runs in Home Assistant, and HA NOVA can't check that ahead", true
	case reasonDeviceAccess:
		return subject + " is on a device that also has a lock, alarm, or door" + through, true
	case reasonDeviceAccessSibling:
		return sayDeviceSibling(e, env, r, subject) + through, true
	case reasonSwitchAsX:
		if e.reg[r.EntityID].di == "" { // matched only by having no device either
			return subject + " may work a lock or a door" + through, true
		}
		return subject + " works a lock or a door" + through, true
	case reasonOwnedTypedMember:
		also := " also "
		if directOwnedCall(env, r) {
			also = " "
		}
		return "This" + also + sayOwnedAction(env, r) + ", which HA NOVA can't check" + through, true
	case reasonConfigDelete:
		return "This removes it from Home Assistant", true
	case reasonOwnedMember:
		if directOwnedCall(env, r) { // its preview says it all
			return "", false
		}
		return "This also " + sayOwnedEffect(env, r) + through, false
	case reasonExpanded, reasonBatch:
		if n := len(plan.Action.EntityIDs); n != 1 {
			return fmt.Sprintf("This acts on %d devices at once", n), false
		}
		if r.Via == "" { // a batch that named one entity several times
			return "", false
		}
		// A sibling of an access device carries DEVICE_ACCESS_SIBLING itself.
		return "HA NOVA picked it from the " + sayText(r.Via, false) + " you named", false
	case reasonStoredMembers:
		var names []string
		for _, m := range env.Members {
			if m.EntityID != "" {
				names = appendUnique(names, sayText(e.sayName(env, m.EntityID), true))
			}
		}
		if len(names) == 0 {
			return "Running it starts its own steps", false
		}
		return "Running it also controls " + sayList(names), false
	case reasonNoRegistryRow:
		return "HA NOVA can't see all details of " + name, false
	case reasonSceneUnread:
		return "HA NOVA can't see what the scene " + name + " changes", false
	case reasonListenerUnexpl, reasonRunListenerUnexpl, reasonManager:
		return "Something else may react to this that HA NOVA can't see", false
	case reasonIntegrationSvc:
		return "This runs an integration's own service, which HA NOVA can't fully check", false
	case reasonServiceNotFound:
		return "A step calls " + sayText(r.Detail, true) + ", which Home Assistant doesn't have: that step will fail when it runs", false
	case reasonConfigWrite, reasonConfigRevert, reasonConfigRestore, reasonAreaAssign:
		return "", false
	}
	return "HA NOVA can't fully check this", r.Code != "" // a code without a sentence never reads as harmless
}

// sayPhrase: named = the verb alone says what the named entity is
// (unlocks, disarms); otherwise a member keeps the noun beside its name.
type sayPhrase struct {
	verb, noun string
	named      bool
}

// sayActionPhrase words the access and disruptive codes after the action
// that raised them (a member's own action, or the direct service).
func sayActionPhrase(e *tierEngine, env *verbEnvelope, r tierReason) (sayPhrase, bool, bool) {
	// Several steps can act on one entity through one item (unlock, then
	// lock): the riskiest step names the clause, never the last one.
	// Only steps that raise this very code word it; another step through the
	// same item (disarm vs. trigger) never lends its action.
	action, data, rank := "", map[string]any(nil), -1
	for _, own := range []bool{true, false} {
		for _, m := range env.Members {
			if m.EntityID != r.EntityID || m.Via != r.Via {
				continue
			}
			if _, code := classifyOnEntity(m.EntityID, m.Action, m.data, e.states[m.EntityID]); own && code != r.Code {
				continue
			}
			if mr := sayActionRank(m); mr > rank {
				action, data, rank = m.Action, m.data, mr
			}
		}
		if action != "" {
			break
		}
	}
	if action == "" && env.Action != nil {
		entityDomain, _ := splitID(r.EntityID)
		action, data = effectiveService(env.Action.Service, entityDomain), env.Action.Data
	}
	_, name := splitID(action)
	switch r.Code {
	case reasonAccessLock:
		verb := map[string]string{"unlock": "unlocks", "open": "opens"}[name]
		if verb == "" {
			return sayPhrase{verb: "acts on", noun: "a door lock"}, true, true
		}
		return sayPhrase{verb: verb, noun: "a door", named: true}, true, true
	case reasonAccessAlarm:
		if name == "alarm_disarm" {
			return sayPhrase{verb: "disarms", noun: "the alarm", named: true}, true, true
		}
		return sayPhrase{verb: "acts on", noun: "the alarm"}, true, true
	case reasonAccessCover, reasonCoverClassUnknown:
		noun := map[string]string{"garage": "a garage door", "gate": "a gate", "door": "a door"}[e.states[r.EntityID].str("device_class")]
		if noun == "" {
			noun = "a cover that may be a door, gate, or garage"
		}
		return sayPhrase{verb: sayCoverVerb(name, data, e.states[r.EntityID]), noun: noun}, true, true
	case reasonDestructiveButton:
		return sayPhrase{verb: "presses", noun: "a button that can wipe a device's settings"}, true, true
	case reasonDisruptive:
		switch {
		case action == "homeassistant.restart":
			return sayPhrase{verb: "restarts", noun: "Home Assistant"}, false, true
		case action == "homeassistant.stop":
			return sayPhrase{verb: "stops", noun: "Home Assistant"}, false, true
		case strings.HasPrefix(action, "siren."):
			verb := "sounds"
			if name == "toggle" {
				verb = map[string]string{"on": "silences", "off": "sounds"}[e.states[r.EntityID].state()]
				if verb == "" {
					verb = "sounds or silences"
				}
			}
			return sayPhrase{verb: verb, noun: "a siren"}, false, true
		case name == "alarm_trigger":
			return sayPhrase{verb: "sets off", noun: "the alarm"}, false, true
		}
		return sayPhrase{verb: "presses", noun: sayButtonNoun(r.EntityID, e.states[r.EntityID])}, false, true
	}
	return sayPhrase{}, false, false
}

func sayActionRank(m tierMember) int {
	_, name := splitID(m.Action)
	switch {
	case name == "unlock" || name == "open" || name == "alarm_disarm" || coverCanOpen(name, m.data):
		return 2
	case m.Class != tierOrdinary.String():
		return 1
	}
	return 0
}

var sayUnseenCodes = map[string]bool{reasonUnregisteredActuator: true, reasonTemplateAction: true, reasonUserButton: true, reasonUnreadCommand: true}

// sayPlainDomains: what these do is plain from the kind (a lock locks, a
// garage door opens); what HA NOVA can't check is how it does it.
var sayPlainDomains = map[string]bool{"lock": true, "cover": true, "alarm_control_panel": true, "valve": true}

// sayUnseen: "HA NOVA can't see what “X” does" ("…can't check how “X”
// works" for a lock, cover, alarm, or valve); reached through an item, that
// item leads ("This automation uses “X”, and …").
func sayUnseen(name, via string, plain bool) string {
	unseen := "HA NOVA can't see what " + name + " does"
	that := "HA NOVA can't see what that does"
	if plain {
		unseen = "HA NOVA can't check how " + name + " works"
		that = "HA NOVA can't check how that works"
	}
	switch {
	case name == "" && via == "":
		return "HA NOVA can't check everything this would set off"
	case name == "":
		return "HA NOVA can't check everything " + via + " would set off"
	case via == "":
		return unseen
	}
	first, size := utf8.DecodeRuneInString(via)
	return string(unicode.ToUpper(first)) + via[size:] + " uses " + name + ", and " + that
}

// sayVia names the item a member reason runs through ("the automation
// “Guest mode”"); "" for a direct target or an area/device expansion.
func sayVia(e *tierEngine, env *verbEnvelope, plan storedPlan, via string) string {
	root := firstSegment(via)
	switch {
	case root == "draft" && plan.ConfigAction != nil:
		return "this " + plan.ConfigAction.Domain
	case root == "scene.apply":
		return "the scene it applies"
	case !entityIDPattern.MatchString(root):
		return ""
	}
	domain, _ := splitID(root)
	kind := map[string]string{"automation": "the automation ", "script": "the script ", "scene": "the scene "}[domain]
	return kind + sayText(e.sayName(env, root), true)
}

func firstSegment(path string) string {
	root, _, _ := strings.Cut(path, "/")
	return root
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
