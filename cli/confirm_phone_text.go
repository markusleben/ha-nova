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
// one line naming the computer that asked, and the hint last — a plain tap
// only opens the app; the buttons show when it is expanded (a long-press on
// iOS; on Android a long-press opens settings). Everything else (what it means, "only if you started this")
// is in the terminal, not on the lock screen.

type phoneAsk struct {
	title, body, button string
	urgent              bool // a pending action: iOS time-sensitive; else active
	unlock              bool // the phone must be unlocked to answer (link, move, stop)
}

const (
	phoneHint       = "Expand to answer."
	phoneSentText   = "Sent to “%s”. Expand the notification and tap “%s”, only if you started this."
	phoneTitleLimit = 80  // runes: one lock-screen line
	phoneWhyLimit   = 300 // runes: the reasons (an expanded notification shows them all)
)

func phoneLinkAsk() phoneAsk {
	return phoneAsk{title: "Link this phone to HA NOVA?", body: "For confirmations from “" + phoneHost() + "”. " + phoneHint, button: phoneLinkButton, unlock: true}
}

// phoneChangeAsk goes to the phone linked now: stop (newPhone "") or move
// HA NOVA's confirmations to newPhone. Stopping says what it gives up.
func phoneChangeAsk(newPhone string) phoneAsk {
	if newPhone == "" {
		return phoneAsk{title: "Stop HA NOVA confirmations here?", body: "After this, HA NOVA may ask in chat, where your AI assistant can answer. " + phoneRequested(), button: "Stop", unlock: true}
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
			why = phoneCut(why, phoneWhyLimit-25) + " (more on the computer)"
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

func phoneRequested() string { return "Requested by “" + phoneHost() + "”. " + phoneHint }

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
// The ids die with the wait (Android timeout, iOS apns-expiration).
// A pending action is time-sensitive (iOS shows it through Focus); linking
// and its changes are asked while the person is at the computer: active.
func phonePayload(ask phoneAsk, tag, ok, no string, deadline time.Time) map[string]any {
	seconds := max(int(time.Until(deadline).Seconds()), 1)
	level, confirm := "active", map[string]any{"action": ok, "title": ask.button, "destructive": true}
	if ask.urgent {
		level = "time-sensitive"
	}
	if ask.unlock {
		confirm["authenticationRequired"] = true
	}
	return map[string]any{"title": ask.title, "message": ask.body, "data": map[string]any{
		"tag": tag,
		"actions": []any{
			confirm,
			map[string]any{"action": no, "title": "Cancel"},
		},
		"push":         map[string]any{"interruption-level": level},
		"apns_headers": map[string]any{"apns-expiration": strconv.FormatInt(deadline.Unix(), 10)},
		"priority":     "high", "channel": "HA NOVA", "importance": "high", "timeout": seconds,
	}}
}

func phoneHost() string {
	if host, err := os.Hostname(); err == nil && terminalSafeName(host) != "" {
		return terminalSafeName(host)
	}
	return "this computer"
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
		return "Linked now: " + name + ", but HA NOVA's Relay now signs in to Home Assistant as " + person + ", so a tap there can't be told apart from HA NOVA itself. You can link another person's phone."
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
// it), gone on Android after ten minutes.
func phoneResultPayload(title, body, tag string) map[string]any {
	return map[string]any{"title": title, "message": body, "data": map[string]any{
		"tag": tag, "push": map[string]any{"interruption-level": "active"}, "channel": "HA NOVA", "timeout": 600,
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
		return "Not done", "Your tap arrived, but HA NOVA stopped. Nothing changed; the computer says why."
	case phoneFailed:
		return "Not asked", "HA NOVA couldn't ask here. Nothing changed."
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
	case exit == verbExitOK:
		return "Sent", "Home Assistant took it; HA NOVA can't confirm the result yet."
	case exit == verbExitUnverified && env.Status == "failed":
		return "Didn't work", "Check Home Assistant."
	case exit == verbExitUnverified:
		return "Check", "Sent, but HA NOVA couldn't verify it. Check Home Assistant."
	case exit == verbExitUnknown || code == "PLAN_CLAIMED" || code == "PLAN_NOT_FOUND":
		return "Unclear", "It may have run. Check Home Assistant."
	}
	return "Not done", "Nothing changed; the computer says why."
}

// phoneResultTitle: "Done: Unlock “A” and 6 more", counted like the ask.
func phoneResultTitle(prefix string, goal [3]string) string {
	return strings.TrimSuffix(phoneTitle(prefix+": "+goal[0], goal[1]), "?")
}
