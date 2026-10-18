package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// The phone's notification (Companion app): a short question as the title,
// one line naming the computer that asked, and the hint last — a plain tap
// only opens the app; the buttons show when it is held (iOS) or expanded
// (Android). Everything else (what it means, "only if you started this")
// is in the terminal, not on the lock screen.

type phoneAsk struct {
	title, body, button string
	urgent              bool // a pending action: iOS time-sensitive; else active
	unlock              bool // the phone must be unlocked to answer (link, move, stop)
}

const (
	phoneHint       = "Hold or expand to answer."
	phoneSentText   = "Sent to “%s”. Hold or expand the notification and tap “%s”, only if you started this."
	phoneTitleLimit = 80  // runes: one lock-screen line
	phoneWhyLimit   = 140 // runes: the reason line
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
	return phoneAsk{title: phoneTitle("Move HA NOVA confirmations to", "“"+newPhone+"”"), body: phoneRequested(), button: "Move", unlock: true}
}

// phonePlanAsk: "Unlock “Front Door”?" with an Unlock button, and the
// reason it needs a confirmation where the title doesn't say it (sayAsk).
func phonePlanAsk(goal [3]string, noSnapshot bool) phoneAsk {
	body := phoneRequested()
	if goal[2] != "" {
		body = phoneCut(goal[2], phoneWhyLimit, "") + ". " + body
	}
	if noSnapshot {
		body = "Can't be undone. " + body
	}
	return phoneAsk{title: phoneTitle(goal[0], goal[1]), body: body, button: phoneButton(goal[0]), urgent: true}
}

// phoneButton: the goal's verb ("Unlock", "Turn on"); a longer one such as
// "Set the position of", or one that reads like the other button, is Confirm.
func phoneButton(verb string) string {
	if verb == "" || strings.Count(verb, " ") > 1 || strings.EqualFold(verb, "Cancel") {
		return "Confirm"
	}
	return verb
}

func phoneRequested() string { return "Requested by “" + phoneHost() + "”. " + phoneHint }

// phoneTitle: verb + object + "?" in one line. A long list keeps its end
// ("… and 4 more"), and a cut name never leaves an open quote.
func phoneTitle(verb, object string) string {
	more := ""
	if i := strings.LastIndex(object, " and "); i > 0 && strings.HasSuffix(object, " more") { // sayList's count
		object, more = object[:i], object[i:]
	}
	return phoneCut(verb+" "+object, phoneTitleLimit-1-len([]rune(more)), "”") + more + "?"
}

// phoneCut shortens text to limit runes with "…", back to the last end
// mark (a closing quote) when one fits.
func phoneCut(text string, limit int, end string) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := string(runes[:max(limit-1, 0)])
	if i := strings.LastIndex(cut, end); end != "" && i > 0 {
		cut = cut[:i+len(end)]
	}
	return cut + "…"
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
