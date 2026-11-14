package main

import (
	"context"
	"errors"
	"time"
)

// One more try for the checks before a phone is asked (2026-11-02: over a
// slow Home Assistant Link one failed read gave up on the linked phone, and
// the agent sent its user to the phone's settings). The phone lookup, the
// listening probe, and the listening lanes' first moment run once more
// after phoneRetryPause when a read failed, on a fresh read budget, inside
// the ask's deadline, leaving windowMinWait to answer (a plan's lookup
// inside phonePlanRetryBudget). Only checks before the notification run
// again: it is never sent twice.

var (
	phoneRetryPause = 2 * time.Second // the short pause before the second try
	// phonePlanRetryBudget: the second lookup's reads where no ask bounds
	// it (a plan, the window's fallback), so ha_plan stays bounded.
	phonePlanRetryBudget = 15 * time.Second
	phoneLanePause       = time.Second     // a listening lane that failed subscribes again after it
	phoneProbeTime       = 5 * time.Second // one probe's longest read (collectPhoneEvents: span + 5 s)
)

// readFailed: a probe or a lane failed for time or transport (no answer, a
// timeout, the Relay's link to Home Assistant), so it may pass once more.
// Any other refusal from the Relay (a subscription Home Assistant refuses)
// is no slow read: phoneFailed.
func readFailed(err error) bool {
	var refused *relayCallError
	if !errors.As(err, &refused) {
		return true
	}
	switch refused.code {
	case "UPSTREAM_WS_TIMEOUT", "UPSTREAM_WS_CONNECT_ERROR", "UPSTREAM_WS_ERROR":
		return true
	}
	return false
}

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
	return findPhone(s, until, true)
}

// findPhone is discoverPhone; asks=false finds a phone this computer can't
// ask (phoneDeaf) too, for a reminder that only notifies it.
func findPhone(s *relaySession, until time.Time, asks bool) (phoneSetup, phoneStatus) {
	if s.cfg.demoToken != "" {
		return phoneSetup{}, phoneNone // links belong to real homes
	}
	links, readable := linksForCall(s.paths, s.call.chatPolicy())
	if !readable {
		return phoneSetup{why: whyRead}, phoneMissing
	}
	if len(links) == 0 || asks && s.phoneDeaf() { // gated: the window, else a code; the Relay asks for the typed requests
		return phoneSetup{}, phoneNone
	}
	done := s.ownBudget(until)
	home, err := lookupPhones(s, links)
	done()
	return home.status(links, err)
}

// discoverAgain is discoverConfirmer, run once more (onceMore) by until.
func discoverAgain(s *relaySession, until time.Time) (phoneSetup, phoneStatus, bool) {
	setup, status := discoverConfirmer(s)
	return onceMore(s, setup, status, until)
}

// onceMore: a lookup that failed on a read (whyRead) runs once more after
// the pause, its read budget ending by until. ok=false: it was due, but
// until left no time.
func onceMore(s *relaySession, setup phoneSetup, status phoneStatus, until time.Time) (phoneSetup, phoneStatus, bool) {
	if status != phoneMissing || setup.why != whyRead {
		return setup, status, true
	}
	if !phoneRetryWait(context.Background(), until) {
		return setup, status, false
	}
	setup, status = chatConfirmer(phoneDiscover(s, until))
	return setup, status, true
}

// detectAfterWindow picks the channel where no window opens: the linked
// phone, else a form, else chat. A linked phone that can't be asked now
// refuses (channelPhoneMissing; channelPhoneUnread where its lookup failed
// on a read twice), so breaking its setup in Home Assistant can't move a
// plan to a channel the model can answer. The second lookup's reads end
// phonePlanRetryBudget after the pause, and by deadline: the window's
// (its fallback), else phonePlanRetryBudget after the plan's own read
// budget, so ha_plan stays well under an MCP call's 60 s. Past it, no
// second lookup.
func detectAfterWindow(s *relaySession, deadline time.Time) string {
	setup, status := discoverConfirmer(s)
	if deadline.IsZero() {
		s.mu.Lock()
		deadline, _ = s.ctx.Deadline()
		s.mu.Unlock()
		deadline = deadline.Add(phonePlanRetryBudget)
	}
	until := time.Now().Add(phoneRetryPause + phonePlanRetryBudget)
	if deadline.Before(until) {
		until = deadline
	}
	setup, status, _ = onceMore(s, setup, status, until)
	switch {
	case status == phoneReady:
		return channelPhone
	case status == phoneMissing && setup.why == whyRead:
		return channelPhoneUnread
	case status == phoneMissing:
		return channelPhoneMissing
	case status == phoneLegacy:
		return channelPhoneUnlinked
	}
	return codeChannel()
}

// phoneListens: a short probe window proves the Relay can subscribe at all
// before any notification goes out, so buttons no listener can hear are
// never sent. A probe that failed on a read runs once more, ending by
// until. phoneIgnored: it listens; else why not (phoneUnread, phoneFailed).
func phoneListens(ctx context.Context, s *relaySession, collect func(context.Context, *relaySession, time.Duration) ([]phoneEvent, error), until time.Time) phoneAnswer {
	_, err := collect(ctx, s, time.Millisecond)
	if err == nil {
		return phoneIgnored
	}
	if !readFailed(err) {
		return phoneFailed
	}
	ctx, cancel := context.WithDeadline(ctx, until)
	defer cancel()
	if !phoneRetryWait(ctx, until) {
		return phoneUnread
	}
	if _, err = collect(ctx, s, time.Millisecond); err == nil {
		return phoneIgnored
	}
	if !readFailed(err) {
		return phoneFailed
	}
	return phoneUnread
}

// phoneCanListen: the listening probe before a window opens beside the
// linked phone, ending by until, so the window says the phone asks too only
// where it can (confirmBoth).
func phoneCanListen(s *relaySession, until time.Time) phoneAnswer {
	ctx, cancel := context.WithDeadline(context.Background(), until)
	defer cancel()
	return phoneListens(ctx, s, phoneCollect, until)
}

// phoneSettled: the listening lanes have started, and one that fails within
// phoneSettle can't hear a tap. After a failed read it subscribes again
// after phoneLanePause; a second one within that and the next phoneSettle
// gives up (phoneUnread), as does the ask's deadline. Nothing has been sent
// yet. ok=false: answer says why not.
func phoneSettled(ctx context.Context, failed <-chan error) (answer phoneAnswer, ok bool) {
	wait := phoneSettle
	for range 2 {
		select {
		case err := <-failed:
			if !readFailed(err) {
				return phoneFailed, false
			}
			wait = phoneLanePause + phoneSettle
		case <-time.After(wait):
			return phoneIgnored, true
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return phoneUnread, false
			}
			return phoneNoAnswer, false
		}
	}
	return phoneUnread, false
}

// The agent's texts where the checks before the send failed on a read (or
// left too little time to answer): the phone got nothing, and nothing on it
// needs fixing.
const (
	phoneUnreadWhy  = "HA NOVA couldn't check the linked phone in Home Assistant just now (Home Assistant answered too slowly or a read failed), so the phone wasn't asked"
	phoneUnreadNext = "Tell the user nothing changed and nothing was sent to their phone: HA NOVA couldn't check their linked phone in Home Assistant just now, because Home Assistant answered too slowly or a read failed. Nothing on the phone needs fixing (no notification settings, no limits). They can try again in a moment, or make this change in Home Assistant itself. Never ask them for a code."
	// phoneUnreadLine: the terminal's line in the link, Move and Stop flows.
	phoneUnreadLine = "HA NOVA couldn't check the phone in Home Assistant just now. Nothing on the phone needs fixing. Run this again in a moment."
)

// phoneMissingRefusal: why a phone plan stops before its ask (the linked
// phone isn't ready), plainer where its lookup failed on a read.
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
