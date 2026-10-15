package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// confirmationSay is the user-facing confirmation text of a plan that needs
// one (envelope field `say`): what would happen, why it needs the user, and
// how to go ahead. Skills relay it instead of writing their own reasons, so
// no agent has to explain the tier (and cites no rule file doing so). Every
// Home Assistant string in it is quoted and defanged (sayText). subject
// names a config item (alias); calls name their resolved targets.
func confirmationSay(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) string {
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
		typed = append(typed[:8], sayMore(len(typed)-8)+" that need a code; the preview lists them")
	}
	if room := max(4-len(typed), 1); len(other) > room {
		other = append(other[:room], sayMore(len(other)-room)+" the preview lists")
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
	case plan.ConfigAction != nil: // the write card's options, in words
		parts = append(parts, "Reply apply to save it, show yaml to see it first, or cancel.")
	default:
		parts = append(parts, "Reply yes to go ahead.")
	}
	return strings.Join(parts, " ")
}

func sayMore(n int) string {
	if n == 1 {
		return "It does 1 more thing"
	}
	return fmt.Sprintf("It does %d more things", n)
}

// sayWhat: "Unlock “Front Door” (Hall): locked → unlocked. Nothing has run yet."
func sayWhat(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) string {
	if action := plan.ConfigAction; action != nil {
		name := sayText(subject, true)
		if name == "" {
			name = sayText(action.Domain+"."+action.ConfigID, true)
		}
		switch {
		case action.Op == "delete":
			return fmt.Sprintf("Delete the %s %s. Nothing is deleted yet.", action.Domain, name)
		case action.Op == "revert":
			return fmt.Sprintf("Undo the last change to the %s %s. Nothing is saved yet.", action.Domain, name)
		case plan.Basis["current"] == "absent":
			return fmt.Sprintf("Create the %s %s. Nothing is saved yet.", action.Domain, name)
		}
		return fmt.Sprintf("Change the %s %s. Nothing is saved yet.", action.Domain, name)
	}
	service := plan.Action.Service
	domain, verb := splitID(service)
	targets := plan.Action.EntityIDs
	var line string
	switch {
	case domain == "script" && !scriptEntityServices[verb]:
		line = "Run the script " + sayText(e.sayName(env, service), true)
	case len(targets) == 0 && domain == "homeassistant":
		line = sayVerb(verb) + " Home Assistant"
	case len(targets) == 0:
		line = "Run " + sayText(service, true)
	default:
		names := []string{}
		for _, id := range targets {
			names = append(names, sayText(e.sayName(env, id), true))
		}
		line = sayVerb(verb) + " " + sayList(names)
		if service == "script.turn_on" {
			line = "Run the script " + sayList(names)
		} else if len(targets) == 1 {
			for _, t := range env.Resolved {
				if area := sayText(t.Area, true); t.EntityID == targets[0] && area != "" {
					line += " in " + area
				}
			}
			changes, warnings := []string{}, ""
			for _, d := range env.Deltas {
				change := sayValue(d.Before) + " → " + sayValue(d.After)
				if d.Field != "state" {
					change = sayValue(d.Field) + " " + change
				}
				if d.After != "" && d.Before != d.After {
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

var sayVerbs = map[string]string{
	"turn_on": "Turn on", "turn_off": "Turn off", "open_cover": "Open", "close_cover": "Close", "stop_cover": "Stop",
	"set_cover_position": "Set the position of", "alarm_disarm": "Disarm", "alarm_trigger": "Set off", "press": "Press",
	"trigger": "Run", "set_value": "Set", "select_option": "Set",
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
		return "HA NOVA can't see what the scene " + name + " does, and its integration also runs locks or doors", true
	case reasonReload:
		return "A reload can change what runs in Home Assistant, and HA NOVA can't check that ahead", true
	case reasonDeviceAccess:
		return subject + " is on a device that also has a lock, alarm, or door" + through, true
	case reasonSwitchAsX:
		return subject + " works a lock or a door" + through, true
	case reasonOwnedTypedMember:
		return "This also sends a raw device message or controls an App, which HA NOVA can't check" + through, true
	case reasonConfigDelete:
		return "This removes it from Home Assistant", true
	case reasonOwnedMember:
		return "This also runs an action that normally gets its own check" + through, false
	case reasonExpanded, reasonBatch:
		if n := len(plan.Action.EntityIDs); n != 1 {
			return fmt.Sprintf("This acts on %d devices at once", n), false
		}
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
	action, data, rank := "", map[string]any(nil), -1
	for _, m := range env.Members {
		if m.EntityID == r.EntityID && m.Via == r.Via {
			if mr := sayActionRank(m); mr > rank {
				action, data, rank = m.Action, m.data, mr
			}
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
		switch {
		case name == "toggle":
			return sayPhrase{verb: "opens or closes", noun: noun}, true, true
		case coverCanOpen(name, data):
			return sayPhrase{verb: "opens", noun: noun}, true, true
		}
		return sayPhrase{verb: "acts on", noun: noun}, true, true
	case reasonDestructiveButton:
		return sayPhrase{verb: "presses", noun: "a button that can wipe a device's settings"}, true, true
	case reasonDisruptive:
		switch {
		case action == "homeassistant.restart":
			return sayPhrase{verb: "restarts", noun: "Home Assistant"}, false, true
		case action == "homeassistant.stop":
			return sayPhrase{verb: "stops", noun: "Home Assistant"}, false, true
		case strings.HasPrefix(action, "siren."):
			return sayPhrase{verb: "sounds", noun: "a siren"}, false, true
		case name == "alarm_trigger":
			return sayPhrase{verb: "sets off", noun: "the alarm"}, false, true
		}
		return sayPhrase{verb: "presses", noun: "a button that restarts, updates, or switches off a device"}, false, true
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

// "confirm" followed by any colon-like mark, after invisible characters are gone.
var sayConfirmWord = regexp.MustCompile(`(?i)confirm\s*[^\p{L}\p{N}\s']`)

// sayPlainValue: a short token (state, number, unit) needs no quotes.
var sayPlainValue = regexp.MustCompile(`^[\p{L}\p{N}_%.+-]{1,24}$`)

func sayValue(s string) string {
	if t := sayText(s, false); t == "" || sayPlainValue.MatchString(t) {
		return t
	}
	return sayText(s, true)
}

// sayText defangs a Home Assistant string for the confirmation text: no
// control characters or line breaks, no backticks or quote marks (a name
// cannot fake the code or close its own quotes), no "confirm:" (a name
// cannot look like a code), at most 60 characters; quoted when asked.
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
	s = strings.Join(strings.Fields(sayConfirmWord.ReplaceAllString(s, "confirm ")), " ")
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
