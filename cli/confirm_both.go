package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// A window plan while a phone is linked (docs/work/2026-10-18-onboarding-
// 0.27-spec.md §7.4): the window and the phone ask at once, until one
// deadline, and the first decisive answer (Confirm or Cancel) wins. The
// other is withdrawn at once: the window closes, and the phone stops
// listening (its ids die with the ask). One answer, one place: answered on
// the computer, the phone's question is cleared with nothing in its place;
// answered on the phone, the phone shows the result; no answer anywhere,
// the phone says so. A later answer on either changes nothing: no one
// reads it. Settings changes (confirm-window, confirm-phone) never race:
// each keeps its single channel (confirm_window.go, confirm_phone_link.go).

const (
	phoneQuietText = "HA NOVA can't reach your linked phone now, so only the window on this computer asks. `ha-nova setup confirm-phone list` says why."
	phoneQuietNext = " Their linked phone couldn't be asked; `ha-nova setup confirm-phone list` (it sends nothing) says why."
)

// humanVote is one channel's answer.
type humanVote struct {
	phone  bool
	window windowAnswer
	answer phoneAnswer
	ended  os.Signal // the process is being stopped: the window closed
}

// decisive: Confirm or Cancel (on the phone also a tap from someone else,
// which stops it). No answer, a window that couldn't open, or a phone that
// couldn't be asked leaves it to the other.
func (v humanVote) decisive() bool {
	if v.phone {
		return v.answer == phoneConfirmed || v.answer == phoneDeclined || v.answer == phoneRejected
	}
	return v.ended != nil || v.window == windowConfirmed || v.window == windowCancelled
}

// confirmBoth asks in the window (text) and on a's phone, and returns like
// confirmByWindow. Every path tells the phone how it ended (phoneApplied,
// which also closes a.s).
func confirmBoth(a *phoneAsking, text string) (verbEnvelope, int, bool) {
	ctx, cancel := context.WithDeadline(context.Background(), a.deadline)
	defer cancel()
	votes := make(chan humanVote, 2) // the first vote received decides: the claim
	go func() {
		answer, ended := askWindowUntilSignal(ctx, text, a.plan.Lang, time.Until(a.deadline))
		votes <- humanVote{window: answer, ended: ended}
	}()
	go func() {
		a.answer, a.reply = askPhone(ctx, a.s, a.setup, "ha-nova-"+a.plan.ID, a.ask, time.Until(a.deadline), os.Stderr)
		votes <- humanVote{phone: true, answer: a.answer}
	}()
	var first humanVote
	var ended os.Signal // the process is being stopped: it ends after the phone hears it
	decided, windowIn, phoneIn, window := false, false, false, windowNoAnswer
	for !decided && !(windowIn && phoneIn) {
		v := <-votes
		if v.phone {
			phoneIn = true
		} else {
			windowIn, window, ended = true, v.window, v.ended
		}
		first, decided = v, v.decisive()
		if v.phone && v.answer == phoneFailed && !windowIn { // the window still asks: say why the phone is quiet
			fmt.Fprintln(os.Stderr, phoneQuietText)
		}
	}
	cancel()                    // withdraw the other: the window closes, the phone stops listening
	for !windowIn || !phoneIn { // both end promptly once cancelled; a late answer is dropped here
		if v := <-votes; v.phone {
			phoneIn = true
		} else {
			windowIn, ended = true, v.ended // a stop still wins over a phone's Confirm
		}
	}
	s, reply, goal := a.s, a.reply, a.goal
	if ended != nil { // stopped on the computer, then the process ends (lane E2)
		if decided && first.phone { // the phone had answered: it hears what came of it
			reply.say(phoneResultTitle("Cancelled", goal), "Stopped on the computer. Nothing changed.")
		} else {
			reply.withdraw()
		}
		s.close()
		endBySignal(ended)
		return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", "HA NOVA was stopped while the confirmation window was open", "Tell the user nothing changed.")
	}
	tell := func(prefix, line string) {
		phoneApplied = func(verbEnvelope, int) { reply.say(phoneResultTitle(prefix, goal), line); s.close() }
	}
	// Answered on the computer: the phone's question goes at once, and
	// nothing takes its place (one answer, one place). Apply doesn't wait
	// for it; the session closes once it went.
	withdraw := func() {
		gone := make(chan struct{})
		go func() { reply.withdraw(); close(gone) }()
		phoneApplied = func(verbEnvelope, int) { <-gone; s.close() }
	}
	phoneOut := a.answer == phoneFailed
	switch {
	case decided && first.phone:
		if first.answer != phoneRejected && window != windowFailed {
			fmt.Fprintln(os.Stderr, "Answered on the phone; the window on this computer is withdrawn.")
		}
		return a.finish()
	case decided && first.window == windowConfirmed:
		fmt.Fprintln(os.Stderr, map[bool]string{false: "Confirmed in the window on this computer; the phone's notification is withdrawn.", true: "Confirmed in the window on this computer."}[phoneOut])
		humanAnswer, humanWhere = confirmCode(s.transport.Credential, a.idHex, a.plan.Channel, a.plan.ExpiresAt), channelWindow
		withdraw()
		return verbEnvelope{}, 0, false
	case decided:
		withdraw()
		return windowStopped(s, "declined", "CONFIRM_DECLINED", "the confirmation window was cancelled",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case window == windowFailed && phoneOut:
		tell(phoneNothing(a.answer))
		return windowStopped(s, "refused", "CONFIRM_UNAVAILABLE", "the confirmation window couldn't open, and HA NOVA can't reach the linked phone either", phoneMissingStep())
	case window == windowFailed: // it never showed: the phone's own outcome
		return a.finish()
	}
	tell(phoneNothing(a.answer))
	if phoneOut {
		why := "the confirmation window got no answer in time"
		if window == windowClosed {
			why = "the confirmation window was closed"
		}
		return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", why+", and the phone couldn't be asked",
			fmt.Sprintf("Tell the user in plain words: the window closed without an answer, nothing changed, and they can ask again. Run ha-nova apply --plan %s again (the window opens again) only after they say they still want it; never ask them for a code.", a.id)+phoneQuietNext)
	}
	why := "neither the confirmation window nor the phone got an answer in time"
	if window == windowClosed {
		why = "the confirmation window was closed, and the phone got no answer in time"
	}
	return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", why,
		fmt.Sprintf("Tell the user nothing changed: neither the window on this computer nor their phone was answered in time. On the phone: %s A plain tap only opens the app. Run ha-nova apply --plan %s again (the window opens and a new notification goes to their phone) only after they say they still want it; never ask them for a code. %s They can also make this change in Home Assistant itself.", a.tapLine(), a.id, a.nothingLine()))
}

// discoverInTime finds the linked phone's setup within wait, which the
// lookup counts against; inTime=false: too little is left to ask anyone.
func discoverInTime(s *relaySession, wait time.Duration) (setup phoneSetup, status phoneStatus, deadline time.Time, inTime bool) {
	deadline = time.Now().Add(wait)
	if wait >= windowMinWait {
		setup, status = discoverConfirmer(s)
	}
	return setup, status, deadline, time.Until(deadline) >= windowMinWait
}

// linkedPlatform: the linked phones' one platform, for a plan's say before
// any lookup; "" when unknown or the homes' phones differ.
func linkedPlatform(paths runtimePaths) string {
	links, _ := linksForCall(paths)
	seen := map[string]bool{}
	for _, entry := range links {
		if entry.link != nil && entry.link.Service != "" {
			seen[entry.link.Platform] = true
		}
	}
	for platform := range seen {
		if len(seen) == 1 {
			return platform
		}
	}
	return ""
}

// phoneLinkedHere: a phone is linked on this computer (for any Home
// Assistant: a local read for a window plan's `say`; apply asks the phone
// only where discovery finds it ready).
func phoneLinkedHere(paths runtimePaths) bool {
	if activeServerIsDemo() {
		return false // a link belongs to a real home; the demo house never has one
	}
	links, _ := phoneLinks(paths)
	for _, entry := range links {
		if entry.link != nil && entry.link.Service != "" {
			return true
		}
	}
	return false
}
