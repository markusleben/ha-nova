package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// The fixed shapes of every line the CLI writes for a person to read: the
// confirmation (`say`, the window and form text) and the result. One voice,
// first person, one place; the sentences that fill them stay in
// confirm_say.go and confirm_say_text.go. A confirmation speaks its plan's
// language (cli/confirm_strings.*.json); a result line stays English, and
// the agent words it in the user's language.
//
//	Confirmation: I'm about to <action>. <consequences> <status> <ask>
//	              (a chat code: the ask is sayCodeBlock, always last)
//	Result:       Done: <result>. I checked it.
//	              Sent to <target>. I can't check the result.  (only sent, not read back)
//	              Not done: <reason>. Nothing changed.
//	              Not sure: / Partly done: <what is known>.

// sayAbout opens every confirmation: "I'm about to unlock “Front Door”."
// action continues the sentence (actionGoal.text).
func sayAbout(lang, action string) string { return confirmT(lang, "say.about", "action", action) }

func sayNotRun(lang string) string     { return confirmT(lang, "say.not_run") }
func sayNotSaved(lang string) string   { return confirmT(lang, "say.not_saved") }
func sayNotDeleted(lang string) string { return confirmT(lang, "say.not_deleted") }
func sayCopyNote(lang string) string   { return confirmT(lang, "say.copy_note") }
func sayNoCopyNote(lang string) string { return confirmT(lang, "say.no_copy_note") }

// sayNeedsYou: the first reason of a typed plan, and why it asks.
func sayNeedsYou(lang, reason string) string {
	return confirmT(lang, "say.needs_you", "reason", reason)
}

func sayYesAsk(lang string) string   { return confirmT(lang, "say.yes_ask") }
func saySaveAsk(lang string) string  { return confirmT(lang, "say.save_ask") }
func sayRoomsAsk(lang string) string { return confirmT(lang, "say.rooms_ask") }

// sayChanged and sayNoWindowHere put a fresh confirmation in context, one
// space before it.
func sayChanged(lang string) string      { return confirmT(lang, "say.changed") }
func sayNoWindowHere(lang string) string { return confirmT(lang, "say.no_window_here") }

// sayWindowAsk: the code is never shown.
func sayWindowAsk(lang string) string { return confirmT(lang, "say.window_ask") }

// sayFormAsk replaces sayCodeBlock where the client shows a confirmation
// form: the code is never in a tool result there (mcp_confirm.go).
func sayFormAsk(lang string) string { return confirmT(lang, "say.form_ask") }

// sayChatNote: the honest limit of fallback E, one line before a chat
// plan's code block. Chat apps (ha-nova serve) have none: the server
// instructions carry it there (mcpInstructions).
func sayChatNote(lang string) string { return confirmT(lang, "say.chat_note") }

// sayCodeLead introduces the code, on the line above it.
func sayCodeLead(lang string) string { return confirmT(lang, "say.code_lead") }

// sayCodeAsk: the code's ask in a confirmation form (mcp_confirm.go).
func sayCodeAsk(lang, code string) string { return confirmT(lang, "form.code_ask", "code", code) }

// sayCodeBlock ends a chat plan's `say`, after a blank line so it stands
// apart: the lead, then the code alone and bold on the last line. A hard
// line break (two spaces) keeps the two lines apart where a chat app joins
// a plain newline; the blank line is a paragraph break every client keeps.
// The say's first sentence names the action the code confirms. The same
// shape as a code a skill mints (skills/ha-nova/index.md → Say it).
func sayCodeBlock(lang, code string) string { return codeBlock(sayCodeLead(lang), code) }

// codeBlock: sayCodeBlock with another lead (a chat app's has the expiry).
func codeBlock(lead, code string) string { return "\n\n" + lead + "  \n**" + code + "**" }

// sayCodeHead: a chat plan's say without its closing sayCodeBlock, and the
// language that block (so the whole say) speaks; found is false without one.
func sayCodeHead(say, code string) (head, lang string, found bool) {
	for _, lang := range slices.Sorted(maps.Keys(confirmCatalogs)) {
		if head, found := strings.CutSuffix(say, sayCodeBlock(lang, code)); found {
			return head, lang, true
		}
	}
	return say, "", false
}

// sayWindowPhoneAsk: a phone is linked here, so it asks too (confirm_both.go).
func sayWindowPhoneAsk(lang, button, platform string) string {
	return confirmT(lang, "say.window_phone_ask", "tap", phoneTapLine(lang, platform, button))
}

// sayPhoneAsk: the code is never shown.
func sayPhoneAsk(lang, button, platform string) string {
	return confirmT(lang, "say.phone_ask", "tap", phoneTapLine(lang, platform, button))
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
	case env.Status == "accepted" && isCall && verified > 0: // part of it only sent: never "Done"
		return callOutcome(env) + " I checked what I could."
	case env.Status == "accepted" && isCall: // each part says what is known ("Sent to …")
		return callOutcome(env) + " I can't check the result."
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
		parts = append(parts, sayList("en", ran)+" ran.")
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
	return sayList("en", named)
}

// plainStates: the end states a result may name, in plain words; any other
// state (free text, select options, attribute changes) is "changed as asked".
var plainStates = map[string]string{"on": "on", "off": "off", "locked": "locked", "unlocked": "unlocked", "open": "open", "closed": "closed",
	"jammed": "jammed", "disarmed": "disarmed", "triggered": "triggered", "armed_away": "armed away", "armed_home": "armed home",
	"armed_night": "armed night", "armed_vacation": "armed vacation", "armed_custom_bypass": "armed custom bypass",
	"opening": "opening", "closing": "closing", "locking": "locking", "unlocking": "unlocking", "arming": "arming", "disarming": "disarming", "pending": "pending"}

// sayReadBackDiffersText: saved, but not what the user approved.
const sayReadBackDiffersText = "Not sure: it was saved, but Home Assistant kept something different from what you approved. Let's look at what changed before you count on it."

// sayPast: a config plan's goal in the past for its result line, which
// stays English (the agent words results in the user's language): "deleted
// the automation “Night”". A verb without a past stays as it is.
func sayPast(g actionGoal) string {
	past := map[string]string{"create": "created", "change": "changed", "delete": "deleted", "restore": "restored", "revert": "undid the last change to"}[g.verb]
	if past == "" {
		return g.text("en")
	}
	return past + " " + g.object("en", sayList("en", g.names))
}
