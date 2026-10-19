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
// listening (its ids die with the ask) while its notification is replaced
// by the result. A later answer on either changes nothing: no one reads it.
// Settings changes (confirm-window, confirm-phone) never race: each keeps
// its single channel (confirm_window.go, confirm_phone_link.go).

const phoneQuietText = "HA NOVA can't reach your linked phone now, so only the window on this computer asks. `ha-nova setup confirm-phone list` says why."

// humanVote is one channel's answer.
type humanVote struct {
	phone  bool
	window windowAnswer
	answer phoneAnswer
}

// decisive: Confirm or Cancel (on the phone also a tap from someone else,
// which stops it). No answer, a window that couldn't open, or a phone that
// couldn't be asked leaves it to the other.
func (v humanVote) decisive() bool {
	if v.phone {
		return v.answer == phoneConfirmed || v.answer == phoneDeclined || v.answer == phoneRejected
	}
	return v.window == windowConfirmed || v.window == windowCancelled
}

// confirmBoth asks in the window (text) and on a's phone, and returns like
// confirmByWindow. Every path tells the phone how it ended (phoneApplied,
// which also closes a.s).
func confirmBoth(a *phoneAsking, text, again string) (verbEnvelope, int, bool) {
	ctx, cancel := context.WithDeadline(context.Background(), a.deadline)
	defer cancel()
	votes := make(chan humanVote, 2) // the first vote received decides: the claim
	go func() { votes <- humanVote{window: askWindow(ctx, text, time.Until(a.deadline))} }()
	go func() {
		a.answer, a.reply = askPhone(ctx, a.s, a.setup, "ha-nova-"+a.plan.ID, a.ask, time.Until(a.deadline), os.Stderr)
		votes <- humanVote{phone: true, answer: a.answer}
	}()
	var first humanVote
	decided, windowIn, phoneIn, window := false, false, false, windowNoAnswer
	for !decided && !(windowIn && phoneIn) {
		v := <-votes
		if v.phone {
			phoneIn = true
		} else {
			windowIn, window = true, v.window
		}
		first, decided = v, v.decisive()
	}
	cancel()                    // withdraw the other: the window closes, the phone stops listening
	for !windowIn || !phoneIn { // both end promptly once cancelled; a late answer is dropped here
		if (<-votes).phone {
			phoneIn = true
		} else {
			windowIn = true
		}
	}
	s, reply, goal := a.s, a.reply, a.goal
	tell := func(prefix, line string) {
		phoneApplied = func(verbEnvelope, int) { reply.say(phoneResultTitle(prefix, goal), line); s.close() }
	}
	switch {
	case decided && first.phone:
		fmt.Fprintln(os.Stderr, "Answered on the phone; the window on this computer closed.")
		a.note = "Approved on this phone. "
		return a.finish()
	case decided && first.window == windowConfirmed:
		fmt.Fprintln(os.Stderr, "Confirmed in the window on this computer; the phone's notification is withdrawn.")
		humanAnswer, humanWhere = confirmCode(s.transport.Credential, a.idHex, a.plan.Channel, a.plan.ExpiresAt), channelWindow
		phoneApplied = func(env verbEnvelope, exit int) {
			prefix, line := phoneApplyResult(env, exit)
			reply.say(phoneResultTitle(prefix, goal), "Approved on the computer. "+line)
			s.close()
		}
		return verbEnvelope{}, 0, false
	case decided:
		tell("Cancelled", "Cancelled on the computer. Nothing changed.")
		return windowStopped(s, "declined", "CONFIRM_DECLINED", "the confirmation window was cancelled",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case window == windowFailed: // it never showed: the phone's own outcome
		return a.finish()
	}
	tell(phoneNothing(a.answer))
	return windowStopped(s, "cancelled", "CONFIRM_CANCELLED", "neither the confirmation window nor the phone got an answer in time", again)
}

// phoneLinkedHere: a phone is linked on this computer (for any Home
// Assistant: a local read for a window plan's `say`; apply asks the phone
// only where discovery finds it ready).
func phoneLinkedHere(paths runtimePaths) bool {
	links, _ := phoneLinks(paths)
	for _, entry := range links {
		if entry.link != nil {
			return true
		}
	}
	return false
}
