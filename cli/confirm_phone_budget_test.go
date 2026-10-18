package main

import (
	"strings"
	"testing"
	"time"
)

// A tap that comes after the session's 30 s read budget (a person picks the
// phone, confirms the window, finds and holds the notification): the
// re-check after the answer must still read Home Assistant. Before the fix
// it read through the expired budget and reported "the phone changed".
// One listening lane is still in flight when the answer arrives.
func TestPhoneAnswerAfterTheReadBudgetStillRechecks(t *testing.T) {
	h, p, home := familyLink(t)
	previous := relaySessionOverallTimeout
	relaySessionOverallTimeout = time.Second
	t.Cleanup(func() { relaySessionOverallTimeout = previous })
	users := map[string]string{"mobile_app_markuss_iphone": "owner-id", "mobile_app_annas_pixel": "anna-id"}
	p.tapBy = func(service, ok, _ string) []phoneEvent {
		time.AfterFunc(1300*time.Millisecond, func() { p.events <- tap(ok, users[service], "REMOTE") })
		return nil
	}
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 0 || linked(t, h) == nil || linked(t, h).DeviceID != "dev-markus" {
		t.Fatalf("slow tap on link: %d %q", exit, out)
	}
	// Relinking: the old phone's slow Confirm, then the new phone's slow Link.
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Anna’s Pixel"}); exit != 0 || linked(t, h).DeviceID != "dev-anna" {
		t.Fatalf("slow taps on relink: %d %q", exit, out)
	}
	// A typed plan confirmed by a slow tap runs.
	relaySessionOverallTimeout = previous
	plan := h.plan("lock.unlock", "lock.front").Plan
	relaySessionOverallTimeout = time.Second
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); plan.Channel != channelPhone || exit != 0 || env.Status != "verified" || calls(h) != 1 {
		t.Fatalf("slow tap on apply: %d %+v", exit, env.Error)
	}
	if exit, out := runUnlink(t, h); exit != 0 || linked(t, h) != nil {
		t.Fatalf("slow tap on unlink: %d %q", exit, out)
	}
	// A re-check that can't read Home Assistant says so; it never claims the phone changed.
	p.tapBy = func(service, ok, _ string) []phoneEvent {
		h.f.mu.Lock()
		home.usersOK = false
		h.f.mu.Unlock()
		return []phoneEvent{tap(ok, users[service], "REMOTE")}
	}
	exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"})
	if exit != 1 || linked(t, h) != nil || !strings.Contains(out, "couldn't re-check") || strings.Contains(out, "changed while") {
		t.Fatalf("re-check read failure: %d %q", exit, out)
	}
}
