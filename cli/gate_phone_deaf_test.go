package main

import (
	"strings"
	"testing"
)

// On a gated App this computer asks no phone itself (phoneDeaf): the gate
// refuses its phone-event subscriptions. A plan that keeps local asks (a
// dashboard plan) takes the window, else a code, and the Relay asks for
// its typed request; a phone change needs the window.

func heldWithoutGrant(send gateSend) any {
	if send.grant == "" {
		return fakeRelayStatus{428, "GATE_APPROVAL_REQUIRED", "held"}
	}
	return nil
}

func gatedDashboard(t *testing.T, g *fakeGate) (*writeHarness, *fakePhones) {
	t.Helper()
	f := dashboardFake()
	newFamilyHome().install(t, f)
	h := gatedHarness(t, f, g)
	seedLink(t, h, &annaLink)
	p := usePhones(t)
	phoneDiscover = discoverPhone
	return h, p
}

func subscribed(h *writeHarness) bool {
	for _, c := range h.log.all() {
		if c.payload["type"] == "subscribe_events" || c.payload["type"] == "subscribe_trigger" {
			return true
		}
	}
	return false
}

func TestGatedLocalPlanNeverAsksThePhone(t *testing.T) {
	deleteHome := []string{"delete", "--domain", "dashboard", "--id", "home-main"}
	t.Run("headless: a code, then the Relay's ask", func(t *testing.T) {
		g := &fakeGate{wsHeld: true, hold: heldWithoutGrant}
		h, p := gatedDashboard(t, g)
		exit, env := h.run(runPlanCommand, deleteHome...)
		if exit != 3 || env.Tier != "typed" || env.Plan.Channel != channelChat || !fourDigits.MatchString(env.Plan.Code) || strings.Contains(env.Say, "phone") {
			t.Fatalf("plan: exit %d %+v say %q", exit, env.Plan, env.Say)
		}
		exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
		asks, _, sends := g.snapshot()
		if exit != 0 || applied.Status != "verified" || len(asks) != 1 || len(sends) != 2 || sends[0].grant != "" || sends[1].grant != "grant-ask1" ||
			!strings.Contains(asks[0][0], "lovelace/dashboards/delete") || len(p.sent) != 0 || subscribed(h) {
			t.Fatalf("apply: exit %d %+v asks %q sends %+v phone %v", exit, applied.Error, asks, sends, p.sent)
		}
	})
	t.Run("a window, then the Relay's ask", func(t *testing.T) {
		g := &fakeGate{wsHeld: true, hold: heldWithoutGrant}
		h, p := gatedDashboard(t, g)
		useWindow(t, windowConfirmed)
		exit, env := h.run(runPlanCommand, deleteHome...)
		if exit != 3 || env.Plan.Channel != channelWindow || !strings.HasSuffix(env.Say, sayWindowAsk("en")) || strings.Contains(env.NextStep, "phone") {
			t.Fatalf("plan: exit %d %+v say %q next %q", exit, env.Plan, env.Say, env.NextStep)
		}
		exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
		asks, _, sends := g.snapshot()
		if exit != 0 || applied.Status != "verified" || len(asks) != 1 || len(sends) != 2 || sends[1].grant != "grant-ask1" || len(p.sent) != 0 || subscribed(h) {
			t.Fatalf("apply: exit %d %+v asks %q sends %+v phone %v", exit, applied.Error, asks, sends, p.sent)
		}
	})
	t.Run("ungated: the phone as today", func(t *testing.T) {
		f := dashboardFake()
		newFamilyHome().install(t, f)
		h := newWriteHarness(t, f)
		seedLink(t, h, &annaLink)
		usePhones(t)
		phoneDiscover = discoverPhone
		if exit, env := h.run(runPlanCommand, deleteHome...); exit != 3 || env.Plan.Channel != channelPhone {
			t.Fatalf("plan: exit %d %+v", exit, env.Plan)
		}
	})
}

func TestGatedPhoneOffNeedsTheWindow(t *testing.T) {
	h := gatedFamily(t, &fakeGate{}, true)
	p := usePhones(t)
	phoneDiscover = discoverPhone
	exit, out := runUnlink(t, h)
	if exit != 1 || !strings.Contains(out, phoneDeafLine) || !strings.Contains(out, "No confirmation window can open here") || !strings.Contains(out, "Nothing changed") ||
		linked(t, h) == nil || len(p.sent) != 0 || subscribed(h) {
		t.Fatalf("headless off: %d %q sent %v", exit, out, p.sent)
	}
	w := useWindow(t, windowCancelled, windowConfirmed)
	if exit, out := runUnlink(t, h); exit != 1 || linked(t, h) == nil || !strings.Contains(out, "the confirmation window was cancelled") {
		t.Fatalf("off, window cancelled: %d %q", exit, out)
	}
	if exit, out := runUnlink(t, h); exit != 0 || linked(t, h) != nil || !strings.Contains(out, phoneOffGated) || len(w.messages) != 2 || len(p.sent) != 0 || subscribed(h) ||
		!strings.Contains(w.messages[1], confirmT("en", "settings.stop_ask_gated")) {
		t.Fatalf("off, window confirmed: %d %q", exit, out)
	}
}
