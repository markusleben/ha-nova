package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The phone's notification (Companion app): a short question as the title,
// never naming the device it shows on (a watch shows it too),
// one line naming the computer that asked, and the hint last (first on
// Android, whose collapsed notification shows one line) — a plain tap
// only opens the app; the buttons show once it is opened up: press and hold
// on an iPhone (a watch shows them at once); on Android a pop-up or an
// opened one shows them, a collapsed one needs a pull down (a long-press
// there opens settings), so its hint is true either way, and where the text
// says more than who asked, it asks to read it all before a tap from the
// pop-up. Android's pop-up cuts a title after about 30 characters: the
// fixed titles fit. Everything else (what it means, "only if you started
// this") is in the terminal, not on the lock screen.

type phoneAsk struct {
	lang                string // the texts' language (phonePayload's hint and Cancel speak it too)
	title, body, button string
	urgent              bool // a pending action: iOS time-sensitive; else active
	unlock              bool // the phone must be unlocked to answer (link, move, stop)
}

const (
	phoneSentText   = "Sent to “%s”. %s Do it only if you started this."
	phoneTitleLimit = 80  // runes: one lock-screen line
	phoneNameRoom   = 8   // runes a title keeps for a cut name, its quotes included
	phoneWhyLimit   = 300 // runes: the reasons (an expanded notification shows them all)
)

// phoneLinkTitle and phoneLinkButton: the link question's, which the
// terminal and next steps quote. The button is not "Link": the setup screen
// also shows Home Assistant Link.
func phoneLinkTitle(lang string) string  { return confirmT(lang, "phone.link.title") }
func phoneLinkButton(lang string) string { return confirmT(lang, "phone.link.button") }

// phoneLinkQuestion: the window's question before a link (the phone linked
// now asks phoneChangeAsk instead). The phone's name stands in quotes, so
// its quote marks become apostrophes; an apostrophe stays.
func phoneLinkQuestion(lang, phone string) string {
	phone = strings.Map(func(r rune) rune {
		if r != '’' && sayQuoteMark(r) {
			return '\''
		}
		return r
	}, phone)
	return confirmT(lang, "settings.link_ask", "phone", phone, "host", phoneHost(lang))
}

func phoneLinkAsk(lang string) phoneAsk {
	return phoneAsk{lang: lang, title: phoneLinkTitle(lang), body: phoneRequested(lang), button: phoneLinkButton(lang), unlock: true}
}

// phoneChangeAsk goes to the phone linked now: stop (newPhone "") or move
// HA NOVA's confirmations to newPhone. Stopping says what it gives up.
func phoneChangeAsk(lang, newPhone string) phoneAsk {
	if newPhone == "" {
		return phoneAsk{lang: lang, title: confirmT(lang, "phone.stop.title"), body: confirmT(lang, "phone.stop.body", "requested", phoneRequested(lang)), button: confirmT(lang, "phone.stop.button"), unlock: true}
	}
	return phoneAsk{lang: lang, title: confirmT(lang, "phone.move.title"), body: confirmT(lang, "phone.move.body", "phone", sayText(newPhone, true), "requested", phoneRequested(lang)),
		button: confirmT(lang, "phone.move.button"), unlock: true}
}

// phonePlanAsk: "Unlock “Front Door”?" with an Unlock button, and the
// reason it needs a confirmation where the title doesn't say it (sayAsk).
func phonePlanAsk(lang string, goal actionGoal, noSnapshot bool) phoneAsk {
	body := phoneRequested(lang)
	if goal.why != "" {
		why := goal.why
		if len([]rune(why)) > phoneWhyLimit {
			why = phoneCut(why, phoneWhyLimit-25) + " " + map[bool]string{true: confirmT(lang, "phone.more_in_chat"), false: confirmT(lang, "phone.more_on_computer")}[chatCall != nil]
		}
		body = why + ". " + body
	}
	if noSnapshot {
		body = confirmT(lang, "phone.no_undo") + " " + body
	}
	return phoneAsk{lang: lang, title: goal.title(lang, ""), body: body, button: goal.button(lang), urgent: true}
}

func phoneRequested(lang string) string { return phoneRequestedBy(lang, chatCall != nil) }

func phoneRequestedBy(lang string, chat bool) string {
	if chat { // ha-nova serve: the host is the NOVA App, not the user's computer
		return confirmT(lang, "phone.requested_chat")
	}
	return confirmT(lang, "phone.requested_by", "host", phoneHost(lang))
}

// phoneHint ends the notification: how to show its buttons on this phone.
// Android's fits the collapsed line (at most 38 characters; an emulator
// showed 39); with more to read than who asked (a hidden effect, "Can't be
// undone.", the new phone), it asks to read that first, as the pop-up shows
// the buttons but one line.
func phoneHint(lang, platform, button string, more bool) string {
	switch platform {
	case phoneIOS:
		return confirmT(lang, "phone.hint.ios")
	case phoneAndroid:
		if more {
			return confirmT(lang, "phone.hint.android_more")
		}
		if hint := confirmT(lang, "phone.hint.android_tap", "button", button); len([]rune(hint)) <= 38 {
			return hint
		}
		return confirmT(lang, "phone.hint.android")
	}
	return confirmT(lang, "phone.hint.other")
}

// phoneTapLine: how to answer on this phone, for the computer and the chat.
func phoneTapLine(lang, platform, button string) string {
	switch platform {
	case phoneIOS:
		return confirmT(lang, "phone.tap.ios", "button", button)
	case phoneAndroid:
		return confirmT(lang, "phone.tap.android", "button", button)
	}
	return confirmT(lang, "phone.tap.other", "button", button, "buttons", phoneButtonsLine(lang))
}

// phoneButtonsLine: how to show the buttons, true on both platforms.
func phoneButtonsLine(lang string) string { return confirmT(lang, "phone.buttons_line") }

// phoneNothingHint: where to turn on notifications when none showed (after
// phoneNothingAfter, and when the ask timed out).
func phoneNothingHint(platform string) string {
	return "Nothing on your phone? " + phoneNothingSteps(platform)
}

func phoneNothingSteps(platform string) string {
	switch platform {
	case phoneIOS:
		return "Open Settings → Notifications → Home Assistant, and turn on Allow Notifications."
	case phoneAndroid:
		return "Open your phone's settings → Apps → Home Assistant → Notifications → HA NOVA, and turn on notifications and pop-ups."
	}
	return "Open your phone's settings, find the Home Assistant app, and turn on its notifications."
}

// phoneCut shortens text to limit runes with "…", closing a quote it cut
// open (“Front Do…”, German „Haustü…“).
func phoneCut(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := string(runes[:max(limit-1, 0)]) + "…"
	open, closing := "“", "”"
	if strings.Contains(text, "„") { // German marks (confirmQuote)
		open, closing = "„", "“"
	}
	if strings.Count(cut, open) > strings.Count(cut, closing) {
		kept := string(runes[:max(limit-2, 0)])
		cut = kept + "…"
		if strings.Count(kept, open) > strings.Count(kept, closing) { // only a mark it kept is closed
			cut += closing
		}
	}
	return cut
}

// phonePayload: the first button is styled destructive (iOS). A plan's
// Confirm needs no device unlock — an Apple Watch never fires an
// authenticationRequired action, and the watch must be able to confirm
// (maintainer decision) — so whoever holds the linked phone can answer that
// one open request. Linking, moving, and stopping last beyond one request:
// they need the unlocked phone (iOS, Android 12+), asked at the computer.
// Android's push relay copies that flag into FCM data, which takes strings
// only: a bool there fails the whole push, and Home Assistant only logs it.
// iOS reads a bool (a string there would drop the unlock), and so does an
// unknown phone: no notification beats one without the unlock.
// The ids die with the wait (Android timeout and FCM ttl in milliseconds,
// iOS apns-expiration; the relay drops a ttl of 0).
// A pending action is time-sensitive (iOS shows it through Focus); linking
// and its changes are asked while the person is at the computer: active.
func phonePayload(ask phoneAsk, platform, tag, ok, no string, deadline time.Time) map[string]any {
	left := max(time.Until(deadline), time.Second)
	level, confirm := "active", map[string]any{"action": ok, "title": ask.button, "destructive": true}
	if ask.urgent {
		level = "time-sensitive"
	}
	switch {
	case ask.unlock && platform == phoneAndroid:
		confirm["authenticationRequired"] = "true"
	case ask.unlock:
		confirm["authenticationRequired"] = true
	}
	hint := phoneHint(ask.lang, platform, ask.button, ask.body != phoneRequestedBy(ask.lang, true) && ask.body != phoneRequestedBy(ask.lang, false)) // never chatCall: ha-nova serve sends outside the verb lock
	message := ask.body + " " + hint
	if platform == phoneAndroid { // a collapsed Android notification shows one line of text: the hint first
		message = hint + " " + ask.body
	}
	return map[string]any{"title": ask.title, "message": message, "data": map[string]any{
		"tag": tag,
		"actions": []any{
			confirm,
			map[string]any{"action": no, "title": confirmT(ask.lang, "phone.cancel")},
		},
		"push":         map[string]any{"interruption-level": level},
		"apns_headers": map[string]any{"apns-expiration": strconv.FormatInt(deadline.Unix(), 10)},
		"priority":     "high", "ttl": left.Milliseconds(), "channel": "HA NOVA", "importance": "high", "timeout": int(left.Seconds()),
	}}
}

func phoneHost(lang string) string {
	if host, err := os.Hostname(); err == nil && terminalSafeName(host) != "" {
		return terminalSafeName(host)
	}
	return confirmT(lang, "phone.your_computer") // it shows on the phone too, where "this" is the phone
}

// missingLine: why the linked phone can't be asked now, and what to do.
func (h phoneHome) missingLine(prev phoneEntry, why phoneWhy) string {
	name, person := "the linked phone", "its user"
	if prev.link != nil {
		name = "“" + terminalSafeName(prev.link.Name) + "”"
		if u := h.users[prev.link.UserID]; u.Name != "" {
			person = terminalSafeName(u.Name)
		}
	}
	switch why {
	case whyRemoved:
		return "Linked now: " + name + ", which is no longer in Home Assistant. You can link another phone."
	case whyDemoted:
		return "Linked now: " + name + ", but " + person + " is no longer a Home Assistant administrator, so it can't confirm. You can link an administrator's phone, or ask the owner to make " + person + " an administrator again."
	case whyRelayUser:
		return "Linked now: " + name + ", but the NOVA App now signs in to Home Assistant as " + person + ", so a tap there can't be told apart from HA NOVA itself. You can link another person's phone."
	case whyShared:
		return "Linked now: " + name + ". " + phoneSharedLine(strings.Trim(name, "“”"))
	case whyUnreachable:
		return "Linked now: " + name + ", which can't get notifications now (its Home Assistant app is signed out, or its entry is disabled). Sign in to the app on it again. If that phone is lost or replaced, remove it in Home Assistant (Settings → Devices & services → Mobile App), then run this again."
	}
	return phoneReadFailed
}

func phoneSharedLine(name string) string {
	return fmt.Sprintf("Two devices in Home Assistant are called “%s”: remove the old one or rename one in its Home Assistant app, then try again.", terminalSafeName(name))
}

// phoneReply closes an ask on the phone: the result replaces the
// notification (same tag, so it replaces instead of stacking), with no
// buttons and no ids, so a tap on a watch or a locked phone gets an answer.
// Answered on the computer, withdraw takes the question off instead, with
// nothing in its place (confirm_both.go). Only the first say or withdraw
// counts; the zero value (nothing was sent) says nothing.
type phoneReply struct {
	once  *sync.Once
	tell  func(title, body string)
	clear func()
}

func (r phoneReply) say(title, body string) {
	if r.once != nil {
		r.once.Do(func() { r.tell(phoneCut(title, phoneTitleLimit), body) })
	}
}

func (r phoneReply) withdraw() {
	if r.once != nil {
		r.once.Do(r.clear)
	}
}

// phoneResultPayload: a passive-looking result (active, so a watch shows
// it), gone on Android after ten minutes. Android fixes a channel's
// importance when its first notification arrives, and a result can come
// first (the question never arrived): both payloads create it as high, so
// pop-ups are on.
func phoneResultPayload(title, body, tag string) map[string]any {
	return map[string]any{"title": title, "message": body, "data": map[string]any{
		"tag": tag, "push": map[string]any{"interruption-level": "active"}, "channel": "HA NOVA", "importance": "high", "timeout": 600,
	}}
}

// phoneNothing: the result of an ask that changed nothing (a title
// prefix and the line).
func phoneNothing(lang string, answer phoneAnswer) (string, string) {
	switch answer {
	case phoneDeclined:
		return confirmT(lang, "outcome.cancelled"), confirmT(lang, "outcome.line.nothing_changed")
	case phoneRejected:
		return confirmT(lang, "outcome.not_confirmed"), confirmT(lang, "outcome.line.other_user")
	case phoneConfirmed:
		return confirmT(lang, "outcome.not_done"), confirmT(lang, "outcome.line.stopped", "why", phoneSaysWhy(lang))
	case phoneFailed:
		return confirmT(lang, "outcome.not_asked"), confirmT(lang, "outcome.line.not_asked")
	}
	return confirmT(lang, "outcome.not_confirmed"), confirmT(lang, "outcome.line.no_answer")
}

// phoneApplyResult: how a confirmed plan ended, for the phone (a title
// prefix and the line). "Done" only for a verified result.
func phoneApplyResult(lang string, env verbEnvelope, exit int) (string, string) {
	if prefix, line := batchPhoneResult(env, exit); prefix != "" {
		return prefix, line
	}
	code := ""
	if env.Error != nil {
		code = env.Error.Code
	}
	switch {
	case exit == verbExitOK && env.Status == "verified":
		return confirmT(lang, "outcome.done"), confirmT(lang, "outcome.line.done")
	case exit == verbExitOK && env.Status == "no_change":
		return confirmT(lang, "outcome.already_done"), confirmT(lang, "outcome.line.already_done")
	case exit == verbExitOK:
		return confirmT(lang, "outcome.sent"), confirmT(lang, "outcome.line.sent")
	case exit == verbExitUnverified && env.Status == "failed":
		return confirmT(lang, "outcome.failed"), confirmT(lang, "outcome.line.failed")
	case exit == verbExitUnverified:
		return confirmT(lang, "outcome.check"), confirmT(lang, "outcome.line.check")
	case exit == verbExitUnknown || code == "PLAN_CLAIMED" || code == "PLAN_NOT_FOUND":
		return confirmT(lang, "outcome.unclear"), confirmT(lang, "outcome.line.unclear")
	}
	return confirmT(lang, "outcome.not_done"), confirmT(lang, "outcome.line.not_done", "why", phoneSaysWhy(lang))
}

// phoneSaysWhy: where the reason shows, the chat in a chat app, else the
// computer (as phonePlanAsk's "more in the chat").
func phoneSaysWhy(lang string) string {
	if chatCall != nil {
		return confirmT(lang, "outcome.line.why_chat")
	}
	return confirmT(lang, "outcome.line.why_computer")
}

// phoneResultTitle: "Done: Unlock “A” and 6 more", counted like the ask.
func phoneResultTitle(lang, prefix string, goal actionGoal) string {
	return goal.title(lang, prefix)
}

// phoneStillLinked re-reads the link after a tap: unlinked, replaced, or its
// user demoted while the notification was open, the tap no longer counts.
func phoneStillLinked(s *relaySession, setup phoneSetup) (linked, unread bool) {
	again, status := discoverConfirmer(s)
	if status == phoneMissing && again.why == whyRead { // unknown, never "changed"
		return false, true
	}
	return status == phoneReady && again.confirmer == setup.confirmer && len(setup.targets) == 1 &&
		again.targets[0].DeviceID == setup.targets[0].DeviceID, false
}

const (
	phoneMissingWhy  = "this change is confirmed on the phone linked to HA NOVA, and HA NOVA can't reach it now"
	phoneMissingNext = "Tell the user nothing changed: HA NOVA could not reach their linked phone. `ha-nova setup confirm-phone list` (it sends nothing; in a terminal if you have no shell) says why and what to do. They can also make this change in Home Assistant itself, or try again later. Never ask them for a code."
	phoneRejectedWhy = "the tap came from a device signed in to Home Assistant as someone other than the person whose phone is linked"
	// #719's pin: no phone chosen yet (phoneUnlinkedNext).
	phoneUnlinkedWhy = "HA NOVA now confirms on one phone the user chooses, and none is chosen on this computer yet"
)

// phoneUnlinkedNext: the agent may run the listing itself; only a tap on
// the chosen phone links it. It quotes the link question in the language
// that phone will show (lang).
func phoneUnlinkedNext(lang string) string {
	return "Tell the user nothing changed: HA NOVA now asks on one phone they choose. With a shell: run ha-nova setup confirm-phone list (it sends nothing), ask which phone is theirs, tell them to watch it for “" + phoneLinkTitle(lang) + "” and tap “" + phoneLinkButton(lang) + "” (" + phoneButtonsLine("en") + "), then run ha-nova setup confirm-phone \"<that phone's name>\" (it waits up to 90 seconds) and plan again. Without a shell, they run ha-nova setup confirm-phone in a terminal. Never ask them for a code."
}
