package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// A batch's texts (docs/work/2026-10-29-batch-confirmation-spec.md §6): one
// list for the window, the form, and the chat; the phone's count and most
// sensitive item; the result lines. Every sentence is a catalog key
// (confirm_strings.en.json, batch.*); names come from Home Assistant through
// sayText, a member's reasons from sayReason, as its own preview words them.

// The longest batch window that shows uncut; provisional until the window
// lanes pass (the spec → Lanes for the window budget). Over it, plan batch
// refuses the list: it is never shown cut.
const (
	batchWindowLines = 34
	batchWindowChars = 2400
)

// batchAccessCodes: what makes a member the most sensitive one for the
// phone, ahead of any delete.
var batchAccessCodes = map[string]bool{reasonAccessLock: true, reasonAccessAlarm: true, reasonAccessCover: true, reasonCoverClassUnknown: true,
	reasonDisruptive: true, reasonDestructiveButton: true, reasonDeviceAccess: true, reasonDeviceAccessSibling: true, reasonSwitchAsX: true, reasonSceneAccess: true}

// op: create, update, or delete (a write of an id that has no config yet
// creates it, as its preview says).
func (m batchMember) op() string {
	switch {
	case m.stored.ConfigAction.Op == "delete":
		return "delete"
	case m.stored.Basis["current"] == "absent" || m.stored.ConfigRequest.Create:
		return "create"
	}
	return "update"
}

// name: the item's name as Home Assistant has it now, else the draft's,
// quoted and defanged.
func (m batchMember) name() string {
	action := m.stored.ConfigAction
	return cmp.Or(sayText(configAlias(m.ctx.current, m.stored.ConfigRequest.Draft), true), sayText(action.EntityID, true), sayText(action.Domain+"."+action.ConfigID, true))
}

func (m batchMember) goal(lang string) string {
	name := m.name()
	switch m.op() + "." + m.stored.ConfigAction.Domain {
	case "create.automation":
		return confirmT(lang, "batch.goal.create.automation", "name", name)
	case "create.script":
		return confirmT(lang, "batch.goal.create.script", "name", name)
	case "update.automation":
		return confirmT(lang, "batch.goal.change.automation", "name", name)
	case "update.script":
		return confirmT(lang, "batch.goal.change.script", "name", name)
	case "delete.automation":
		return confirmT(lang, "batch.goal.delete.automation", "name", name)
	}
	return confirmT(lang, "batch.goal.delete.script", "name", name)
}

// reasons: the member's sentences that need the user, beyond its goal (a
// delete's own "removes it" is the goal), as its preview words them.
func (m batchMember) reasons(lang string) []string {
	explained := map[string]bool{}
	for _, r := range m.env.Reasons {
		if _, isTyped, ok := sayActionPhrase(lang, m.e, &m.env, r); ok && isTyped {
			explained[r.EntityID+"|"+r.Via] = true
		}
	}
	var out []string
	for _, r := range m.env.Reasons {
		if r.Code == reasonConfigDelete || sayUnseenCodes[r.Code] && explained[r.EntityID+"|"+r.Via] {
			continue
		}
		if text, typed := sayReason(lang, m.e, &m.env, m.fresh, r); typed && text != "" && !slices.Contains(out, text+".") {
			out = append(out, text+".")
		}
	}
	if len(out) == 0 && m.fresh.Tier == tierTyped.String() && m.op() != "delete" {
		out = append(out, confirmT(lang, "batch.unchecked"))
	}
	return out
}

// sensitivity ranks members for the phone: an access or disruptive effect,
// then a delete, then another typed one, then a preview.
func (m batchMember) sensitivity() int {
	switch {
	case slices.ContainsFunc(m.env.Reasons, func(r tierReason) bool { return batchAccessCodes[r.Code] }):
		return 3
	case m.op() == "delete":
		return 2
	case m.fresh.Tier == tierTyped.String():
		return 1
	}
	return 0
}

// batchText: the list in order, one line per member with what happens to
// it and why it needs the user, then what one confirmation covers.
func batchText(rp batchReplan, typed bool) string {
	lang, n := rp.lang, len(rp.members)
	lines := []string{confirmT(lang, "batch.head", "count", n)}
	deletes := false
	for i, m := range rp.members {
		lines = append(lines, strings.Join(append([]string{confirmT(lang, "batch.item", "n", i+1, "goal", m.goal(lang))}, m.reasons(lang)...), " "))
		deletes = deletes || m.op() == "delete"
	}
	var tail []string
	if typed {
		tail = append(tail, confirmT(lang, "batch.needs_you", "count", n))
	}
	if deletes {
		tail = append(tail, confirmT(lang, "batch.copies"))
	}
	tail = append(tail, confirmT(lang, "batch.order"), confirmT(lang, "batch.status"))
	return strings.Join(lines, "\n") + "\n" + strings.Join(tail, " ")
}

// batchSay: the list and the plan's ask, as a single plan's say ends.
func batchSay(s *relaySession, plan storedPlan, goal actionGoal, text string, n int) string {
	lang := textLang(plan)
	switch {
	case plan.Confirm == "code" && plan.Channel == channelWindow && phoneLinkedHere(s.paths):
		return demoPrefix(lang, text+" "+sayWindowPhoneAsk(lang, goal.button(lang), linkedPlatform(s.paths)))
	case plan.Confirm == "code" && plan.Channel == channelWindow:
		return demoPrefix(lang, text+" "+sayWindowAsk(lang))
	case plan.Confirm == "code" && plan.Channel == channelPhone:
		return demoPrefix(lang, text+" "+sayPhoneAsk(lang, goal.button(lang), linkedPlatform(s.paths)))
	case plan.Confirm == "code":
		if plan.Channel == channelChat && chatCall == nil {
			text += "\n" + sayChatNote(lang)
		}
		return demoPrefix(lang, text+sayCodeBlock(lang, plan.code))
	}
	return demoPrefix(lang, text+" "+confirmT(lang, "batch.ask", "count", n))
}

// batchNextStep: a batch's apply takes longer than one plan's.
func batchNextStep(step string, plan storedPlan) string {
	if step == "" {
		step = fmt.Sprintf("Show `say`. Only after a reply that is only that code, run ha-nova apply --confirm <the user's text> --plan %s.", plan.ID)
	}
	return strings.Replace(step, " (give the command up to 2 minutes)", "", 1) + " Give that apply up to 10 minutes: it checks each one again, sends it, and checks it after, in turn."
}

// batchPhoneGoal: the phone's question, one goal with the count (no
// names), and the most sensitive member with its reasons; its button is
// Confirm.
func batchPhoneGoal(rp batchReplan) actionGoal {
	best := 0
	for i, m := range rp.members {
		if m.sensitivity() > rp.members[best].sensitivity() {
			best = i
		}
	}
	m := rp.members[best]
	why := append([]string{confirmT(rp.lang, "batch.phone.most", "goal", m.goal(rp.lang))}, m.reasons(rp.lang)...)
	return actionGoal{whole: batchPhoneTitle(rp), why: strings.TrimSuffix(strings.Join(why, " "), ".")}
}

func batchPhoneTitle(rp batchReplan) string {
	lang, n := rp.lang, len(rp.members)
	deletes, writes, kinds := false, false, map[string]bool{}
	for _, m := range rp.members {
		deletes, writes = deletes || m.op() == "delete", writes || m.op() != "delete"
		kinds[m.stored.ConfigAction.Domain] = true
	}
	op := "change"
	switch {
	case deletes && writes:
		op = "either"
	case deletes:
		op = "delete"
	}
	kind := rp.members[0].stored.ConfigAction.Domain
	if len(kinds) > 1 {
		kind = "mixed"
	}
	switch op + "." + kind {
	case "delete.automation":
		return confirmT(lang, "batch.phone.delete.automation", "count", n)
	case "delete.script":
		return confirmT(lang, "batch.phone.delete.script", "count", n)
	case "delete.mixed":
		return confirmT(lang, "batch.phone.delete.mixed", "count", n)
	case "change.automation":
		return confirmT(lang, "batch.phone.change.automation", "count", n)
	case "change.script":
		return confirmT(lang, "batch.phone.change.script", "count", n)
	case "change.mixed":
		return confirmT(lang, "batch.phone.change.mixed", "count", n)
	case "either.automation":
		return confirmT(lang, "batch.phone.either.automation", "count", n)
	case "either.script":
		return confirmT(lang, "batch.phone.either.script", "count", n)
	}
	return confirmT(lang, "batch.phone.either.mixed", "count", n)
}

// batchWindowFits refuses a window batch too long for one window, with the
// longer copy note and the phone's line: it is never cut.
func batchWindowFits(plan storedPlan, verb string) error {
	text := plan.Prompt
	if noCopies := batchNoCopies(plan.Lang, text); utf8.RuneCountInString(noCopies) > utf8.RuneCountInString(text) {
		text = noCopies
	}
	text += "\n\n" + windowBoth(textLang(plan)) + "\n" + windowAsked(textLang(plan))
	lines, chars := strings.Count(text, "\n")+1, utf8.RuneCountInString(text)
	if lines <= batchWindowLines && chars <= batchWindowChars {
		return nil
	}
	next := "Split the list into two batches (each gets its own confirmation), then make them again with the same plan ids."
	if verb != "plan" {
		next = "Nothing was sent, and this batch is used up (a name in it grew longer): plan every item again, then make two shorter batches, each with its own confirmation."
	}
	return refusedWith("BATCH_TOO_LONG", fmt.Sprintf("the confirmation of these %d changes is too long for one window (%d lines and %d characters; at most %d and %d)", len(plan.BatchMembers), lines, chars, batchWindowLines, batchWindowChars), next)
}

// batchCopyText: a batch's window and form text; after the user chose to go
// on without copies, it says so instead.
func batchCopyText(plan storedPlan, noSnapshot bool) string {
	if !noSnapshot || plan.Lang == "" {
		return plan.Prompt
	}
	return batchNoCopies(plan.Lang, plan.Prompt)
}

// batchNoCopies swaps the copy note for the no-copy one (a no-op for any
// text without it).
func batchNoCopies(lang, text string) string {
	if lang == "" {
		return text
	}
	return strings.Replace(text, confirmT(lang, "batch.copies"), confirmT(lang, "batch.no_copies"), 1)
}

func batchDoneSay(lang string, n int, noCopies bool) string {
	say := confirmT(lang, "batch.result.done", "count", n)
	if noCopies {
		say += " " + confirmT(lang, "batch.result.no_copies")
	}
	return say
}

// batchStopSay: how far the batch got, what stopped it, and that the rest
// were not sent. Every member before the stop was verified.
func batchStopSay(lang string, result batchResult, stop int) string {
	done, n, name := result.Counts.Verified, len(result.Items), result.Items[stop].quoted
	var say string
	switch status := result.Items[stop].Status; {
	case status == "unknown" && done == 0:
		say = confirmT(lang, "batch.result.none_unknown", "name", name)
	case status == "unknown":
		say = confirmT(lang, "batch.result.unknown", "count", n, "done", done, "name", name)
	case status == "unverified" && done == 0:
		say = confirmT(lang, "batch.result.none_unverified", "name", name)
	case status == "unverified":
		say = confirmT(lang, "batch.result.unverified", "count", n, "done", done, "name", name)
	case status == "failed" && done == 0:
		say = confirmT(lang, "batch.result.none_failed", "name", name)
	case status == "failed":
		say = confirmT(lang, "batch.result.failed", "count", n, "done", done, "name", name)
	case status == "drift":
		say = confirmT(lang, "batch.result.changed", "count", n, "done", done, "name", name)
	default:
		say = confirmT(lang, "batch.result.not_sent", "count", n, "done", done, "name", name)
	}
	if stop+1 < n {
		say += " " + confirmT(lang, "batch.result.rest")
	}
	return say
}

// batchPhoneResult: how a batch ended, for the phone (a title prefix and the
// line); "" where a single plan's words fit (phoneApplyResult).
func batchPhoneResult(env verbEnvelope, exit int) (string, string) {
	result, ok := env.Result.(batchResult)
	if !ok || result.Counts == nil {
		return "", ""
	}
	lang, partly := result.lang, confirmT(result.lang, "batch.phone.partly", "count", len(result.Items), "done", result.Counts.Verified)
	switch {
	case exit == verbExitOK && env.Status == "verified":
		return confirmT(lang, "outcome.done"), confirmT(lang, "batch.phone.done")
	case exit == verbExitUnverified && result.Counts.Verified > 0:
		return confirmT(lang, "batch.phone.partly_title"), partly + " " + phoneSaysWhy(lang)
	case exit == verbExitUnknown && result.Counts.Verified > 0:
		return confirmT(lang, "outcome.unclear"), partly + " " + phoneSaysWhy(lang)
	}
	return "", ""
}
