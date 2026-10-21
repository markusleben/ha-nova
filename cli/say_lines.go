package main

import (
	"fmt"
	"strings"
)

// The fixed shapes of every line the CLI writes for a person to read: the
// confirmation (`say`, the window and form text) and the result. One voice,
// first person, one place; the sentences that fill them stay in
// confirm_say.go and confirm_say_text.go.
//
//	Confirmation: I'm about to <action>. <consequences> <status> <ask>
//	Result:       Done: <result>. I checked it.
//	              Not done: <reason>. Nothing changed.
//	              Not sure: / Partly done: <what is known>.

// sayAbout opens every confirmation: "I'm about to unlock “Front Door”."
func sayAbout(action string) string { return "I'm about to " + lowerFirst(action) + "." }

const (
	sayNotRun     = "Nothing has run yet."
	sayNotSaved   = "Nothing is saved yet."
	sayNotDeleted = "Nothing is deleted yet."
	sayCopyNote   = "A copy is saved first, so it can be restored."
	sayNoCopyNote = "No copy could be saved, so once deleted it can't be restored."
	// sayNeedsYou follows the first reason of a typed plan.
	sayNeedsYou = ", so I need you to confirm it."
	sayYesAsk   = "Reply yes to go ahead."
	saySaveAsk  = "Save it? I can also show the details."
	sayRoomsAsk = "Save these rooms?"
	// Prefixes that put a fresh confirmation in context.
	sayChanged      = "Something changed since the last preview. "
	sayNoWindowHere = "The confirmation window couldn't open on this computer. "
	// sayWindowAsk: the code is never shown.
	sayWindowAsk = "Confirm in the window on this computer."
	// sayFormAsk replaces sayTypedAsk where the client shows a confirmation
	// form: the code is never in a tool result there (mcp_confirm.go).
	sayFormAsk = "Type the one-time code from HA NOVA in the form your app shows — a plain yes can't confirm this."
	// sayChatNote closes a chat plan's ask: the honest limit of fallback E.
	sayChatNote = "I can see this code too, so it only counts if you type it yourself."
	// chatCodeNote is sayChatNote in a chat app (ha-nova serve).
	chatCodeNote = "I can see this code too, so it protects less than a tap on your phone. Link your phone in NOVA for real protection."
)

// sayCodeAsk: the code's ask in a confirmation form (mcp_confirm.go).
func sayCodeAsk(code string) string { return "Type " + code + " to confirm." }

// sayTypedAsk ends a chat plan's `say`: the code names its own action, so
// several plans in one preview can't swap codes; the same shape as a code
// a skill mints (skills/ha-nova/index.md → Say it).
func sayTypedAsk(code, goal string) string {
	return "Type " + code + " to " + lowerFirst(goal) + " — a plain yes can't confirm this."
}

// sayWindowPhoneAsk: a phone is linked here, so it asks too (confirm_both.go).
func sayWindowPhoneAsk(button string) string {
	return "Confirm in the window on this computer or on your phone (expand the notification and tap “" + button + "”). The first answer counts."
}

// sayPhoneAsk: the code is never shown.
func sayPhoneAsk(button string) string {
	return "Confirm on your phone: expand the notification and tap “" + button + "”."
}

func sayNotDone(reason string) string { return "Not done: " + reason + ". Nothing changed." }

// sayDone: "Done: <result>. <how I know>."
func sayDone(result, checked string) string { return "Done: " + result + ". " + checked }

// resultSay is the one result line of an apply or a direct call; it never
// replaces a line that already speaks (a drift's new preview, a no-op, a
// read-back difference). A call words each target's read-back ("“Kitchen”
// is off now."); a config or rooms apply its action (env.done). Only
// outcomes of a sent request get one: a refusal or a Relay error before
// sending keeps its next step, which may be a safe retry; a call that
// returns data answers with the data.
func resultSay(env *verbEnvelope) string {
	code := ""
	if env.Error != nil {
		code = env.Error.Code
	}
	isCall := env.Action != nil && env.Config == nil
	if env.Say != "" || !isCall && env.done == "" || isCall && env.Action.Response {
		return env.Say
	}
	detail, checked, verified, reached := env.done+".", "I checked that Home Assistant saved it.", 0, 0
	switch {
	case isCall:
		detail, checked = lowerFirst(callOutcome(env)), "I checked it."
		for _, row := range env.Verification {
			switch row.Outcome {
			case "verified":
				verified++
				reached++
			case "accepted": // sent to a script or scene: it may have run
				reached++
			}
		}
	case strings.HasPrefix(env.done, "deleted "):
		checked = "I checked that it's gone."
	case strings.HasPrefix(env.done, "put "): // rooms
		checked = "I checked it."
	}
	switch {
	case code != "" && code != "SERVICE_REJECTED" && code != "CONFIG_REJECTED" && code != "OUTCOME_UNKNOWN": // its message and next step speak
	case code == "SERVICE_REJECTED" && !callMayHavePartlyRun(env), code == "CONFIG_REJECTED" && env.Status == "failed" && !strings.HasPrefix(env.done, "created the dashboard"): // a new dashboard may stay, empty
		return sayNotDone("Home Assistant refused it")
	case code == "SERVICE_REJECTED" || code == "CONFIG_REJECTED" && env.Status == "failed":
		return "Not done: Home Assistant refused it. Part of it may have happened, so let's check before you count on it."
	case code == "CONFIG_REJECTED": // a dashboard that changed anyway
		return "Not sure: Home Assistant showed an error, but something may have changed. Let's check it before you count on it."
	case (env.OutcomeUnknown || code == "OUTCOME_UNKNOWN") && isCall:
		return "Not sure: it may have run, and I can't tell. I didn't try again."
	case env.OutcomeUnknown || code == "OUTCOME_UNKNOWN":
		return "Not sure: it may have been saved, and I can't tell. I didn't try again."
	case detail == "" || detail == ".": // nothing to name
	case env.Status == "verified":
		return sayDone(strings.TrimSuffix(detail, "."), checked)
	case env.Status == "accepted" && verified > 0:
		return "Done: " + detail + " I checked what I could."
	case env.Status == "accepted":
		return "Done: " + detail + " I can't check this kind of change."
	case env.Status == "unverified" && isCall:
		return "Not sure: " + detail
	case env.Status == "unverified":
		return "Not sure: I sent it, but I couldn't check that it worked."
	case env.Status == "failed" && reached > 0:
		return "Partly done: " + detail
	case env.Status == "failed" && isCall:
		return "Not done: " + detail
	case env.Status == "failed":
		return "Not done: I sent it, but it didn't take effect."
	}
	return ""
}

// callMayHavePartlyRun: a rejected call with several targets, or one that
// runs steps (a script, automation, or scene), may have acted before Home
// Assistant refused it.
func callMayHavePartlyRun(env *verbEnvelope) bool {
	if env.Action == nil || len(env.Action.EntityIDs) > 1 {
		return true
	}
	domain, _ := splitID(env.Action.Service)
	for _, id := range env.Action.EntityIDs {
		if target, _ := splitID(id); target == "script" || target == "automation" || target == "scene" {
			return true
		}
	}
	return domain == "script" || domain == "automation" || domain == "scene"
}

// callOutcome words a call's read-back per target: “Kitchen Lights” is on
// now. Rows without a name are counted, never shown by id.
func callOutcome(env *verbEnvelope) string {
	if len(env.Verification) == 0 { // a targetless call: nothing to read back
		if env.Status == "accepted" {
			return "Sent to Home Assistant."
		}
		return ""
	}
	names, seen := map[string]string{}, map[string]int{}
	for _, t := range env.Resolved {
		names[t.EntityID] = sayText(t.Name, true)
		seen[names[t.EntityID]]++
	}
	for id, name := range names { // two targets with one name: the id tells them apart (as in a preview row)
		if name != "" && seen[name] > 1 {
			names[id] = sayText(id, false) + " (" + name + ")"
		}
	}
	inRows := map[string]bool{}
	for _, row := range env.Verification {
		inRows[row.EntityID] = true
	}
	var ran []string // a scene checked through its members: name the scene
	for _, t := range env.Resolved {
		if !inRows[t.EntityID] && names[t.EntityID] != "" {
			ran = append(ran, names[t.EntityID])
		}
	}
	var order []string
	groups := map[string][]string{}
	unnamed := map[string]int{}
	for _, row := range env.Verification {
		key := row.Outcome + "\x00" + plainStates[row.Observed] // observed is "state, field=value…": a comma names no plain state
		if _, seen := groups[key]; !seen {
			order, groups[key] = append(order, key), nil
		}
		if names[row.EntityID] != "" {
			groups[key] = append(groups[key], names[row.EntityID])
		} else {
			unnamed[key]++
		}
	}
	var parts []string
	if len(ran) > 0 {
		parts = append(parts, sayList(ran)+" ran.")
	}
	for _, key := range order {
		who := groups[key]
		many := len(who)+unnamed[key] > 1
		subject, is, it := sayCount(who, unnamed[key]), map[bool]string{true: " are", false: " is"}[many], map[bool]string{true: "they", false: "it"}[many]
		outcome, state, _ := strings.Cut(key, "\x00")
		switch {
		case outcome == "verified" && state != "":
			parts = append(parts, subject+is+" "+state+" now.")
		case outcome == "verified":
			parts = append(parts, subject+" changed as asked.")
		case outcome == "failed" && state != "":
			parts = append(parts, subject+" did not change as asked; "+it+map[bool]string{true: " show ", false: " shows "}[many]+state+".")
		case outcome == "failed":
			parts = append(parts, subject+" did not change as asked.")
		case outcome == "accepted" && state != "":
			parts = append(parts, "Sent to "+subject+"; "+it+is+" still "+state+".")
		case outcome == "accepted":
			parts = append(parts, "Sent to "+subject+".")
		default:
			parts = append(parts, "Sent to "+subject+", but the change does not show yet.")
		}
	}
	return strings.Join(parts, " ")
}

// sayCount lists names plus devices without one: “A”, “B”, “C” and 3 more.
func sayCount(named []string, unnamed int) string {
	if unnamed > 0 && len(named)+unnamed > 3 {
		shown := named[:min(len(named), 3)]
		if len(shown) == 0 {
			return fmt.Sprint(unnamed, " devices")
		}
		return strings.Join(shown, ", ") + fmt.Sprintf(" and %d more", len(named)+unnamed-len(shown))
	}
	if unnamed > 0 {
		named = append(named, map[bool]string{true: "1 device", false: fmt.Sprint(unnamed, " devices")}[unnamed == 1])
	}
	return sayList(named)
}

// plainStates: the end states a result may name, in plain words; any other
// state (free text, select options, attribute changes) is "changed as asked".
var plainStates = map[string]string{"on": "on", "off": "off", "locked": "locked", "unlocked": "unlocked", "open": "open", "closed": "closed",
	"jammed": "jammed", "disarmed": "disarmed", "triggered": "triggered", "armed_away": "armed away", "armed_home": "armed home",
	"armed_night": "armed night", "armed_vacation": "armed vacation", "armed_custom_bypass": "armed custom bypass",
	"opening": "opening", "closing": "closing", "locking": "locking", "unlocking": "unlocking", "arming": "arming", "disarming": "disarming", "pending": "pending"}

// sayReadBackDiffersText: saved, but not what the user approved.
const sayReadBackDiffersText = "Not sure: it was saved, but Home Assistant kept something different from what you approved. Let's look at what changed before you count on it."

// sayPast: "Unlock" → "unlocked", for the result line; an unknown verb
// stays as it is ("Done: set temperature “Hall”").
func sayPast(verb, object string) string {
	past := map[string]string{
		"Turn on": "turned on", "Turn off": "turned off", "Toggle": "toggled", "Open": "opened", "Close": "closed",
		"Stop": "stopped", "Lock": "locked", "Unlock": "unlocked", "Arm": "armed", "Disarm": "disarmed", "Press": "pressed",
		"Run": "ran", "Set": "set", "Set off": "set off", "Set the position of": "set the position of",
		"Create": "created", "Change": "changed", "Delete": "deleted", "Restore": "restored", "Undo the last change to": "undid the last change to",
	}[verb]
	if past == "" {
		past = lowerFirst(verb)
	}
	return strings.TrimSpace(past + " " + object)
}
