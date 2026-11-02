package main

import (
	"context"
	"time"
)

// One more try for the checks before a phone is asked (2026-11-02: over a
// slow Home Assistant Link one failed read gave up on the linked phone, and
// the agent sent its user to the phone's settings). The phone lookup and
// the listening probe run once more after phoneRetryPause when a read
// failed, on a fresh read budget, inside the ask's deadline. Only checks
// before the notification run again: it is never sent twice.

// phoneRetryPause: the short pause before the second try.
var phoneRetryPause = 2 * time.Second

// phoneRetryWait waits phoneRetryPause, at most half the time left before
// until; false when none is left or ctx ended.
func phoneRetryWait(ctx context.Context, until time.Time) bool {
	left := time.Until(until)
	if left <= 0 {
		return false
	}
	select {
	case <-time.After(min(phoneRetryPause, left/2)):
		return true
	case <-ctx.Done():
		return false
	}
}

// discoverPhone: the linked phone, read only when this computer has a link,
// on a read budget of its own that ends by until.
func discoverPhone(s *relaySession, until time.Time) (phoneSetup, phoneStatus) {
	if activeServerIsDemo() {
		return phoneSetup{}, phoneNone // links belong to real homes
	}
	links, readable := linksForCall(s.paths)
	if !readable {
		return phoneSetup{why: whyRead}, phoneMissing
	}
	if len(links) == 0 {
		return phoneSetup{}, phoneNone
	}
	done := s.ownBudget(until)
	home, err := lookupPhones(s, links)
	done()
	return home.status(links, err)
}

// discoverAgain is discoverConfirmer, run once more when a read failed
// (whyRead); the second lookup's budget ends by until. ok=false: it was due,
// but until left no time.
func discoverAgain(s *relaySession, until time.Time) (setup phoneSetup, status phoneStatus, ok bool) {
	if setup, status = discoverConfirmer(s); status != phoneMissing || setup.why != whyRead {
		return setup, status, true
	}
	if !phoneRetryWait(context.Background(), until) {
		return setup, status, false
	}
	setup, status = chatConfirmer(phoneDiscover(s, until))
	return setup, status, true
}

// phoneListens: a short probe window proves the Relay can subscribe at all
// before any notification goes out, so buttons no listener can hear are
// never sent. A failed probe runs once more, inside ctx.
func phoneListens(ctx context.Context, s *relaySession, collect func(context.Context, *relaySession, time.Duration) ([]phoneEvent, error)) bool {
	if _, err := collect(ctx, s, time.Millisecond); err == nil {
		return true
	}
	deadline, _ := ctx.Deadline()
	if !phoneRetryWait(ctx, deadline) {
		return false
	}
	_, err := collect(ctx, s, time.Millisecond)
	return err == nil
}

// The agent's texts where a check before the send failed on a read twice:
// the phone got nothing, and nothing on it needs fixing.
const (
	phoneUnreadWhy  = "HA NOVA couldn't check the linked phone in Home Assistant just now (Home Assistant answered too slowly or a read failed, twice), so the phone wasn't asked"
	phoneUnreadNext = "Tell the user nothing changed and nothing was sent to their phone: HA NOVA couldn't check their linked phone in Home Assistant just now, because Home Assistant answered too slowly or a read failed, twice. Nothing on the phone needs fixing (no notification settings, no limits). They can try again in a moment, or make this change in Home Assistant itself. Never ask them for a code."
)

// phoneMissingRefusal: why a phone plan stops before its ask (the linked
// phone isn't ready), plainer where the lookup failed on a read twice.
func phoneMissingRefusal(why phoneWhy) (code, message, next string) {
	if why == whyRead {
		return "CONFIRM_UNAVAILABLE", phoneUnreadWhy, phoneUnreadNext
	}
	return "CONFIRM_UNAVAILABLE", phoneMissingWhy, phoneMissingStep()
}

// notAsked: why the ask ended without the phone's answer (phoneFailed,
// phoneUnread), and what to tell the user.
func (a *phoneAsking) notAsked() (message, next string) {
	if a.answer == phoneUnread {
		return phoneUnreadWhy, phoneUnreadNext
	}
	return "the confirmation could not be sent to the phone", phoneMissingStep()
}
