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

// A plan whose checks spend the whole read budget (LIMIT_CUT on a large
// home) still asks the linked phone: the phone lookup reads with a budget of
// its own, alone and beside the window.
func TestPhoneLookupAfterChecksSpentTheReadBudget(t *testing.T) {
	for _, window := range []bool{false, true} {
		t.Run(map[bool]string{false: "phone", true: "window and phone"}[window], func(t *testing.T) {
			h, p, _ := familyLink(t)
			seedLink(t, h, &phoneLink{Service: "mobile_app_markuss_iphone", DeviceID: "dev-markus", UserID: "owner-id", Name: "Markus’s iPhone", Platform: phoneIOS})
			if window {
				useRaceWindow(t, windowNoAnswer, make(chan struct{}), false)
			}
			previous := relaySessionOverallTimeout
			relaySessionOverallTimeout = 300 * time.Millisecond
			t.Cleanup(func() { relaySessionOverallTimeout = previous })
			h.f.hang["/api/events"] = true
			p.tapBy = tapFrom("mobile_app_markuss_iphone", "owner-id", true)
			exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
			if exit != 3 || env.Plan == nil || !strings.Contains(reasonCodes(env), reasonLimitCut) {
				t.Fatalf("plan: exit %d %+v reasons %s", exit, env.Error, reasonCodes(env))
			}
			if want := map[bool]string{false: channelPhone, true: channelWindow}[window]; env.Plan.Channel != want {
				t.Fatalf("channel %q, want %q", env.Plan.Channel, want)
			}
			if exit, env := h.run(runApplyCommand, "--plan", env.Plan.ID); exit != 0 || env.Status != "verified" || len(p.sent) == 0 || calls(h) != 1 {
				t.Fatalf("apply: exit %d %s %+v sent %d calls %d", exit, env.Status, env.Error, len(p.sent), calls(h))
			}
		})
	}
}

// The lookup's own budget ends with the lookup: the session keeps the budget
// it had (a verb that looks up the phone several times stays bounded), and a
// lookup that still can't read refuses as before.
func TestPhoneLookupBudgetEndsWithTheLookup(t *testing.T) {
	h, _, _ := familyLink(t)
	seedLink(t, h, &phoneLink{Service: "mobile_app_markuss_iphone", DeviceID: "dev-markus", UserID: "owner-id", Name: "Markus’s iPhone", Platform: phoneIOS})
	previous := relaySessionOverallTimeout
	relaySessionOverallTimeout = 300 * time.Millisecond
	t.Cleanup(func() { relaySessionOverallTimeout = previous })
	s, err := newRelaySession(h.paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	time.Sleep(400 * time.Millisecond)
	if _, status := discoverPhone(s); status != phoneReady {
		t.Fatalf("lookup after the budget: status %d", status)
	}
	var current haUser
	if err := s.phoneRead("ws", map[string]any{"type": "auth/current_user"}, &current); err == nil {
		t.Fatal("the session's spent budget came back renewed")
	}
	h.f.hang["config/auth/list"] = true
	if setup, status := discoverPhone(s); status != phoneMissing || setup.why != whyRead {
		t.Fatalf("hung lookup: status %d why %d", status, setup.why)
	}
}
