package main

import (
	"fmt"
	"strings"
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
		return "Nothing could be resolved for the target you named, so this can't run. Name the entities instead."
	}
	parts := []string{sayWhat(e, env, plan, subject)}
	var typed, other []string
	seen := map[string]bool{}
	for _, r := range env.Reasons {
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
		// reasons have no typed sentence still asks for the code.
		first := "HA NOVA can't fully check what this would do"
		if len(typed) > 0 {
			first, typed = typed[0], typed[1:]
		}
		parts = append(parts, first+", so it needs a code.")
	}
	// Every code-tier clause is shown (up to 8); the milder ones fill up to
	// four sentences and the rest are counted, never dropped silently.
	if len(typed) > 8 {
		typed = append(typed[:8], sayMore(len(typed)-8, "that need a code; the preview lists them"))
	}
	if room := max(4-len(typed), 1); len(other) > room {
		other = append(other[:room], sayMore(len(other)-room, "the preview lists"))
	}
	rest := append(typed, other...)
	for _, text := range rest {
		parts = append(parts, text+".")
	}
	if plan.Kind == "delete" {
		parts = append(parts, "A copy is saved first, so it can be restored.")
	}
	switch {
	case plan.Confirm == "code":
		parts = append(parts, "Reply with `"+plan.Code+"` to go ahead — a plain yes can't confirm this.")
	case plan.ConfigAction != nil: // the write card's literal options line
		parts = append(parts, "Options: apply · show yaml · cancel")
	default:
		parts = append(parts, "Reply yes to go ahead.")
	}
	return strings.Join(parts, " ")
}

// sayWhat: "Unlock “Front Door” (Hall): locked → unlocked. Nothing has run yet."
func sayWhat(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) string {
	if action := plan.ConfigAction; action != nil {
		// The write card already names the item and shows the diff: `say`
		// is its closing block (save status, reasons, ask).
		if action.Op == "delete" {
			return "Nothing is deleted yet."
		}
		return "Nothing is saved yet."
	}
	service := plan.Action.Service
	domain, verb := splitID(service)
	targets := plan.Action.EntityIDs
	var line string
	switch {
	case domain == "script" && !scriptEntityServices[verb]:
		line = "Run the script " + sayText(e.sayName(env, service), true) + sayData(plan.Action.Data)
	case len(targets) == 0 && domain == "homeassistant":
		line = sayVerb(verb) + " Home Assistant" + sayData(plan.Action.Data)
	case len(targets) == 0:
		line = "Run " + sayText(service, true) + sayData(plan.Action.Data)
	default:
		names := []string{}
		for _, id := range targets {
			names = append(names, sayText(e.sayName(env, id), true))
		}
		line = sayVerb(verb) + " " + sayList(names)
		if service == "script.turn_on" {
			line = "Run the script " + sayList(names)
		}
		if len(targets) > 1 {
			line += sayData(plan.Action.Data)
		}
		if len(targets) == 1 {
			for _, t := range env.Resolved {
				if area := sayText(t.Area, true); t.EntityID == targets[0] && area != "" {
					line += " in " + area
				}
			}
			changes, warnings := []string{}, ""
			for _, d := range env.Deltas {
				change := sayValue(d.Before) + " → " + sayValue(d.After)
				switch {
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
			line += "." + warnings
			return line + " Nothing has run yet."
		}
	}
	return line + ". Nothing has run yet."
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
	unseen := "HA NOVA can't see what " + name + " does"
	if name == "" {
		unseen = "HA NOVA can't check everything this would set off"
	}
	subject := "This"
	if name != "" {
		subject = name
	}
	through := ""
	if via := sayVia(e, env, plan, r.Via); via != "" {
		through = ", through " + via
	}
	switch r.Code {
	case reasonUnregisteredActuator, reasonTemplateAction, reasonUserButton, reasonUnreadCommand:
		return unseen + through, true
	case reasonUserService:
		return "This runs a service defined on the device itself, which HA NOVA can't read", true
	case reasonTemplatedAction, reasonUnresolvedTarget, reasonStoredSelector, reasonBlueprint, reasonYAMLNoID,
		reasonUnreadConfig, reasonListener, reasonScanFailed, reasonLimitCut:
		return "HA NOVA can't check everything this would set off", true
	case reasonSceneAccess:
		return "HA NOVA can't read the scene " + name + ", and its integration also controls locks, alarms, gates, or doors", true
	case reasonReload:
		return "A reload can change what runs in Home Assistant, and HA NOVA can't check that ahead", true
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
		return "This also " + sayOwnedAction(env, r) + ", which HA NOVA can't check" + through, true
	case reasonConfigDelete:
		return "This removes it from Home Assistant", true
	case reasonOwnedMember:
		return "This also runs an action that normally gets its own check" + through, false
	case reasonExpanded, reasonBatch:
		if n := len(plan.Action.EntityIDs); n != 1 {
			return fmt.Sprintf("This acts on %d entities at once", n), false
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
	case reasonConfigWrite, reasonConfigRevert:
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
