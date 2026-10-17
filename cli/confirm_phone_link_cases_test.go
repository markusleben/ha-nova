package main

import (
	"bufio"
	"strings"
	"testing"
	"time"
)

// More linking cases (confirm_phone_link_test.go has the family home).

// A linked phone that can never answer again (removed from Home Assistant,
// or its user no longer an administrator; a model can cause both) is
// replaced or unlinked only in the window — never by a tap alone, never
// when the window can't open.
func TestReplacingAPhoneThatCantAnswerNeedsTheWindow(t *testing.T) {
	h, p, home := familyLink(t)
	seedLink(t, h, &phoneLink{Service: "mobile_app_old", DeviceID: "dev-gone", UserID: "owner-id", Name: "Old Phone"})
	if plan, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); plan != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" {
		t.Fatalf("plan with a removed phone: %d %+v", plan, env.Error)
	}
	p.tapBy = tapFrom("mobile_app_markuss_iphone", "owner-id", true)
	for _, run := range []func() (int, string){
		func() (int, string) { return runUnlink(t, h) },
		func() (int, string) { return runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}) },
	} {
		if exit, out := run(); exit != 1 || !strings.Contains(out, "No confirmation window can open") || linked(t, h).DeviceID != "dev-gone" || len(p.sent) != 0 {
			t.Fatalf("without a window: %d %q %v", exit, out, p.services)
		}
	}
	useWindow(t, windowFailed)
	if exit, _ := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || linked(t, h).DeviceID != "dev-gone" || len(p.sent) != 0 {
		t.Fatalf("a window that can't open replaced the phone: %d", exit)
	}
	w := useWindow(t, windowCancelled, windowConfirmed)
	if exit, _ := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || len(p.sent) != 0 || linked(t, h).DeviceID != "dev-gone" {
		t.Fatalf("window cancelled: %d %v", exit, p.services)
	}
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 0 || linked(t, h).DeviceID != "dev-markus" || !strings.Contains(w.messages[1], "Link “Markus’s iPhone”") ||
		!strings.Contains(out, "no longer in Home Assistant") || !strings.Contains(out, "A confirmation window opens on this computer first") {
		t.Fatalf("window confirmed: %d %q %v", exit, out, w.messages)
	}
	// Markus demoted: his linked phone can't confirm, and the window may approve a change.
	home.users[1]["is_owner"], home.users[1]["group_ids"] = false, []string{"system-users"}
	home.users[2]["is_owner"] = true // Anna owns this home now
	h.f.mu.Lock()
	home.install(t, h.f)
	h.f.mu.Unlock()
	links, _ := phoneLinks(h.paths)
	if err := setPhoneLink(h.paths, "anna-id", links["anna-id"], links["owner-id"].link); err != nil {
		t.Fatal(err)
	}
	p.services, p.sent = nil, nil
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	w = useWindow(t, windowConfirmed)
	s, _ := newRelaySession(h.paths, verbOptions{})
	defer s.close()
	var out strings.Builder
	if exit := linkPhone(s, bufio.NewReader(strings.NewReader("")), &out, phoneLinkRun{pick: "Anna’s Pixel"}); exit != 0 || !strings.Contains(out.String(), "Markus is no longer a Home Assistant administrator") ||
		strings.Contains(out.String(), "remove it in Home Assistant") || strings.Join(p.services, ",") != "mobile_app_annas_pixel,mobile_app_annas_pixel" {
		t.Fatalf("demoted: %d %q %v", exit, out.String(), p.services)
	}
}

// A link approved against one state never overwrites a change made
// meanwhile without that change's own approval.
func TestPhoneLinkWriteNeedsTheApprovedState(t *testing.T) {
	h, _, _ := familyLink(t)
	before, _ := phoneLinks(h.paths)
	seedLink(t, h, &annaLink)
	if err := setPhoneLink(h.paths, "owner-id", before["owner-id"], nil); err != errPhoneLinkChanged || linked(t, h) == nil {
		t.Fatalf("stale unlink: %v", err)
	}
}

// Two devices with one name share a notify service, so neither is offered,
// and the user is told how to fix it instead of "install the app"; a
// device whose entities are gone still counts (the device registry).
func TestSharedPhoneNamesAreNamed(t *testing.T) {
	h, p, home := familyLink(t)
	home.users = home.users[:2] // only Markus is an administrator
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_markuss_iphone", Name: "Markus’s iPhone", DeviceID: "dev-old"},
		phoneTarget{Service: "mobile_app_family_ipad", Name: "Family iPad", DeviceID: "dev-old-ipad"})
	exit, out := runLink(t, h, "", phoneLinkRun{})
	if exit != 1 || !strings.Contains(out, "Two devices in Home Assistant are called “Markus’s iPhone”") || strings.Contains(out, "install the Home Assistant app") || len(p.sent) != 0 {
		t.Fatalf("shared: %d %q", exit, out)
	}
}

// Without a terminal, old phone plus new phone stay within one budget.
func TestPhoneLinkWithoutTerminalStaysInItsBudget(t *testing.T) {
	h, p, _ := familyLink(t)
	saved := phoneLinkBudget
	phoneLinkBudget = 3 * time.Second
	t.Cleanup(func() { phoneLinkBudget = saved })
	seedLink(t, h, &annaLink)
	p.tapBy = func(service, ok, no string) []phoneEvent {
		if service == "mobile_app_annas_pixel" {
			return []phoneEvent{tap(ok, "anna-id", "REMOTE")}
		}
		return nil // the new phone never answers
	}
	start := time.Now()
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"}); exit != 1 || time.Since(start) > 4*time.Second || !strings.Contains(out, "was not tapped in time") {
		t.Fatalf("budget: %d after %v: %q", exit, time.Since(start), out)
	}
}

// Discovery: no link means no phone reads; a link refuses (never moves)
// when the owner is unreadable, the Relay acts as the owner, or the linked
// user is no longer an administrator; a name two devices share is never
// used or listed.
func TestDiscoverLinkedPhone(t *testing.T) {
	h, p, home := familyLink(t)
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelChat || home.reads != 0 {
		t.Fatalf("no link: %+v reads %d", plan, home.reads)
	}
	seedLink(t, h, &annaLink)
	refused := func(why string) {
		t.Helper()
		if exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); exit != 4 || env.Error.Code != "CONFIRM_UNAVAILABLE" {
			t.Fatalf("%s: exit %d %+v", why, exit, env.Error)
		}
	}
	home.usersOK = false
	refused("owner unreadable")
	home.usersOK = true
	home.relay = "owner-id"
	refused("relay is the owner")
	if exit, out := runLink(t, h, "", phoneLinkRun{pick: "Anna’s Pixel"}); exit != 1 || !strings.Contains(out, "signs in to Home Assistant as the owner") {
		t.Fatalf("link on the owner's token: %d %q", exit, out)
	}
	home.relay = "anna-id"
	refused("relay is the linked user")
	home.relay = "supervisor-id"
	home.users[2]["group_ids"] = []string{"system-users"}
	refused("linked user demoted")
	home.users[2]["group_ids"] = []string{"system-admin"}
	home.devices = append(home.devices, phoneTarget{Service: "mobile_app_annas_pixel", Name: "Anna’s Pixel", DeviceID: "dev-anna-2"})
	refused("name shared with another device")
	if exit, out := runLink(t, h, "", phoneLinkRun{}); exit != 0 || strings.Contains(out, ") Anna") || !strings.Contains(out, "Two devices in Home Assistant are called “Anna’s Pixel”") {
		t.Fatalf("shared name listed: %q", out)
	}
	home.devices = home.devices[:len(home.devices)-1]
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelPhone || len(p.sent) != 0 {
		t.Fatalf("usable again: %+v", plan)
	}
}
