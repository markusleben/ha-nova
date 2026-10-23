package main

import (
	"fmt"
	"os"
	"slices"
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
// on an iPhone (a watch shows them at once), pull it down or tap its arrow
// on Android (a long-press there opens settings). Everything else (what it
// means, "only if you started this") is in the terminal, not on the lock screen.

type phoneAsk struct {
	title, body, button string
	urgent              bool // a pending action: iOS time-sensitive; else active
	unlock              bool // the phone must be unlocked to answer (link, move, stop)
}

const (
	phoneLinkTitle  = "Use this for HA NOVA confirmations?"
	phoneSentText   = "Sent to “%s”. %s Do it only if you started this."
	phoneTitleLimit = 80  // runes: one lock-screen line
	phoneWhyLimit   = 300 // runes: the reasons (an expanded notification shows them all)
)

func phoneLinkAsk() phoneAsk {
	return phoneAsk{title: phoneLinkTitle, body: phoneRequested(), button: phoneLinkButton, unlock: true}
}

// phoneChangeAsk goes to the phone linked now: stop (newPhone "") or move
// HA NOVA's confirmations to newPhone. Stopping says what it gives up.
func phoneChangeAsk(newPhone string) phoneAsk {
	if newPhone == "" {
		return phoneAsk{title: "Stop getting HA NOVA confirmations?", body: "After this, HA NOVA may ask in chat, where your AI assistant can answer. " + phoneRequested(), button: "Stop", unlock: true}
	}
	return phoneAsk{title: phoneTitle("Move HA NOVA confirmations to", sayText(newPhone, true)), body: phoneRequested(), button: "Move", unlock: true}
}

// phonePlanAsk: "Unlock “Front Door”?" with an Unlock button, and the
// reason it needs a confirmation where the title doesn't say it (sayAsk).
func phonePlanAsk(goal [3]string, noSnapshot bool) phoneAsk {
	body := phoneRequested()
	if goal[2] != "" {
		why := goal[2]
		if len([]rune(why)) > phoneWhyLimit {
			why = phoneCut(why, phoneWhyLimit-25) + map[bool]string{true: " (more in the chat)", false: " (more on the computer)"}[chatCall != nil]
		}
		body = why + ". " + body
	}
	if noSnapshot {
		body = "Can't be undone. " + body
	}
	return phoneAsk{title: phoneTitle(goal[0], goal[1]), body: body, button: phoneButton(goal[0]), urgent: true}
}

// phoneButton: the goal's verb ("Unlock", "Turn on"); a longer one such as
// "Set the position of", or one that reads like dismissing (Cancel, Close,
// Stop), is Confirm.
func phoneButton(verb string) string {
	if verb == "" || strings.Count(verb, " ") > 1 || slices.ContainsFunc([]string{"Cancel", "Close", "Stop", "Dismiss"}, func(v string) bool { return strings.EqualFold(v, verb) }) {
		return "Confirm"
	}
	return verb
}

func phoneRequested() string {
	if chatCall != nil { // ha-nova serve: the host is the NOVA App, not the user's computer
		return "Requested in your AI chat app."
	}
	return "Requested by “" + phoneHost() + "”."
}

// phoneHint ends the notification: how to show its buttons on this phone.
func phoneHint(platform string) string {
	switch platform {
	case phoneIOS:
		return "Press and hold to answer."
	case phoneAndroid:
		return "Pull down to answer."
	}
	return "Press and hold (iPhone) or pull down (Android) to answer."
}

// phoneTapLine: how to answer on this phone, for the computer and the chat.
func phoneTapLine(platform, button string) string {
	switch platform {
	case phoneIOS:
		return "Press and hold the notification, then tap “" + button + "”."
	case phoneAndroid:
		return "Pull the notification down, or tap its arrow, then tap “" + button + "”."
	}
	return "Tap “" + button + "” in the notification. " + phoneButtonsLine
}

// phoneButtonsLine: how to show the buttons, true on both platforms.
const phoneButtonsLine = "To see the buttons, press and hold the notification on an iPhone, or pull it down on Android."

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
		return "Open your phone's settings → Apps → Home Assistant → Notifications, and turn on HA NOVA."
	}
	return "Open your phone's settings, find the Home Assistant app, and turn on its notifications."
}

// phoneTitle: verb + object + "?" in one line. Too long with several
// names, it keeps the first and counts every other one ("Unlock “A” and 6
// more?"), so the title never understates what a tap confirms. Names are
// sayText-quoted: “ marks each one, and "and N more" counts the rest.
func phoneTitle(verb, object string) string {
	if title := verb + " " + object + "?"; len([]rune(title)) <= phoneTitleLimit {
		return title
	}
	total, rest := strings.Count(object, "“"), 0
	if i := strings.LastIndex(object, " and "); i > 0 && !strings.HasSuffix(object, "”") { // sayList's own count, never a name
		if _, err := fmt.Sscanf(object[i:], " and %d more", &rest); err == nil && rest > 0 {
			total += rest
		}
	}
	head, more := object, ""
	if j := strings.Index(object, "”"); total > 1 && j > 0 {
		head, more = object[:j+len("”")], fmt.Sprintf(" and %d more", total-1)
	}
	return verb + " " + phoneCut(head, phoneTitleLimit-len([]rune(verb+more))-2) + more + "?"
}

// phoneCut shortens text to limit runes with "…", closing a quote it cut
// open (“Front Do…”).
func phoneCut(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := string(runes[:max(limit-1, 0)]) + "…"
	if strings.Count(cut, "“") > strings.Count(cut, "”") {
		cut = string(runes[:max(limit-2, 0)]) + "…”"
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
	message := ask.body + " " + phoneHint(platform)
	if platform == phoneAndroid { // a collapsed Android notification shows one line of text: the hint first
		message = phoneHint(platform) + " " + ask.body
	}
	return map[string]any{"title": ask.title, "message": message, "data": map[string]any{
		"tag": tag,
		"actions": []any{
			confirm,
			map[string]any{"action": no, "title": "Cancel"},
		},
		"push":         map[string]any{"interruption-level": level},
		"apns_headers": map[string]any{"apns-expiration": strconv.FormatInt(deadline.Unix(), 10)},
		"priority":     "high", "ttl": left.Milliseconds(), "channel": "HA NOVA", "importance": "high", "timeout": int(left.Seconds()),
	}}
}

func phoneHost() string {
	if host, err := os.Hostname(); err == nil && terminalSafeName(host) != "" {
		return terminalSafeName(host)
	}
	return "your computer" // it shows on the phone too, where "this" is the phone
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
// Only the first say counts; the zero value (nothing was sent) says nothing.
type phoneReply struct {
	once *sync.Once
	tell func(title, body string)
}

func (r phoneReply) say(title, body string) {
	if r.once != nil {
		r.once.Do(func() { r.tell(phoneCut(title, phoneTitleLimit), body) })
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
func phoneNothing(answer phoneAnswer) (string, string) {
	switch answer {
	case phoneDeclined:
		return "Cancelled", "Nothing changed."
	case phoneRejected:
		return "Not confirmed", "The tap came from another Home Assistant user. Nothing changed."
	case phoneConfirmed:
		return "Not done", "Your tap arrived, but HA NOVA stopped. Nothing changed. " + phoneSaysWhy()
	case phoneFailed:
		return "Not asked", "HA NOVA couldn't ask. Nothing changed."
	}
	return "Not confirmed", "No answer reached Home Assistant. Nothing changed."
}

// phoneApplyResult: how a confirmed plan ended, for the phone (a title
// prefix and the line). "Done" only for a verified result.
func phoneApplyResult(env verbEnvelope, exit int) (string, string) {
	code := ""
	if env.Error != nil {
		code = env.Error.Code
	}
	switch {
	case exit == verbExitOK && env.Status == "verified":
		return "Done", "Home Assistant confirms it."
	case exit == verbExitOK && env.Status == "no_change":
		return "Already done", "It was already that way; nothing was sent."
	case exit == verbExitOK:
		return "Sent", "Home Assistant took it; HA NOVA can't confirm the result yet."
	case exit == verbExitUnverified && env.Status == "failed":
		return "Didn't work", "Check Home Assistant."
	case exit == verbExitUnverified:
		return "Check", "Sent, but HA NOVA couldn't verify it. Check Home Assistant."
	case exit == verbExitUnknown || code == "PLAN_CLAIMED" || code == "PLAN_NOT_FOUND":
		return "Unclear", "It may have run. Check Home Assistant."
	}
	return "Not done", "Nothing changed. " + phoneSaysWhy()
}

// phoneSaysWhy: where the reason shows, the chat in a chat app, else the
// computer (as phonePlanAsk's "more in the chat").
func phoneSaysWhy() string {
	if chatCall != nil {
		return "The chat says why."
	}
	return "The computer says why."
}

// phoneResultTitle: "Done: Unlock “A” and 6 more", counted like the ask.
func phoneResultTitle(prefix string, goal [3]string) string {
	return strings.TrimSuffix(phoneTitle(prefix+": "+goal[0], goal[1]), "?")
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
	// #719's pin: no phone chosen yet. The agent may run the listing itself;
	// only a tap on the chosen phone links it.
	phoneUnlinkedWhy  = "HA NOVA now confirms on one phone the user chooses, and none is chosen on this computer yet"
	phoneUnlinkedNext = "Tell the user nothing changed: HA NOVA now asks on one phone they choose. With a shell: run ha-nova setup confirm-phone list (it sends nothing), ask which phone is theirs, tell them to watch it for “" + phoneLinkTitle + "” and tap “" + phoneLinkButton + "” (" + phoneButtonsLine + "), then run ha-nova setup confirm-phone \"<that phone's name>\" (it waits up to 90 seconds) and plan again. Without a shell, they run ha-nova setup confirm-phone in a terminal. Never ask them for a code."
)
