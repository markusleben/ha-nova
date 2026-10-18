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
}

const (
	phoneHint       = "Hold or expand to answer."
	phoneSentText   = "Sent to “%s”. Hold or expand the notification and tap “%s”, only if you started this."
	phoneTitleLimit = 80 // runes: one lock-screen line
)

func phoneLinkAsk() phoneAsk {
	return phoneAsk{title: "Link this phone to HA NOVA?", body: "For confirmations from “" + phoneHost() + "”. " + phoneHint, button: phoneLinkButton}
}

// phoneChangeAsk goes to the phone linked now: stop (newPhone "") or move
// HA NOVA's confirmations to newPhone.
func phoneChangeAsk(newPhone string) phoneAsk {
	if newPhone == "" {
		return phoneAsk{title: "Stop HA NOVA confirmations here?", body: phoneRequested(), button: "Stop"}
	}
	return phoneAsk{title: phoneCut("Move HA NOVA confirmations to “" + newPhone + "”?"), body: phoneRequested(), button: "Move"}
}

// phonePlanAsk: "Unlock “Front Door”?" with an Unlock button (a long verb
// such as "Set the position of" is a Confirm button).
func phonePlanAsk(goal [2]string, noSnapshot bool) phoneAsk {
	ask := phoneAsk{title: phoneCut(goal[0] + " " + goal[1] + "?"), body: phoneRequested(), button: goal[0], urgent: true}
	if strings.Count(goal[0], " ") > 1 || goal[0] == "" {
		ask.button = "Confirm"
	}
	if noSnapshot {
		ask.body = "Can't be undone. " + ask.body
	}
	return ask
}

func phoneRequested() string { return "Requested by “" + phoneHost() + "”. " + phoneHint }

func phoneCut(title string) string {
	if runes := []rune(title); len(runes) > phoneTitleLimit {
		return string(runes[:phoneTitleLimit-2]) + "…?"
	}
	return title
}

// phonePayload: Confirm needs an unlocked phone (Face ID, fingerprint, or
// passcode; iOS, Android 12+) and is styled destructive (iOS); the
// notification expires with the wait (Android timeout, iOS apns-expiration).
// A pending action is time-sensitive (iOS shows it through Focus); linking
// and its changes are asked while the person is at the computer: active.
func phonePayload(ask phoneAsk, tag, ok, no string, deadline time.Time) map[string]any {
	seconds := max(int(time.Until(deadline).Seconds()), 1)
	level := "active"
	if ask.urgent {
		level = "time-sensitive"
	}
	return map[string]any{"title": ask.title, "message": ask.body, "data": map[string]any{
		"tag": tag,
		"actions": []any{
			map[string]any{"action": ok, "title": ask.button, "destructive": true, "authenticationRequired": true},
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
