package main

import (
	"bufio"
	"strings"
	"testing"
)

func runPhoneOffer(t *testing.T, h *writeHarness, input *string, pairedNow bool) string {
	t.Helper()
	var reader *bufio.Reader
	if input != nil {
		reader = bufio.NewReader(strings.NewReader(*input))
	}
	var out strings.Builder
	offerPhoneLink(reader, &out, h.paths, pairedNow, true)
	return out.String()
}

// MAR-244: a computer that can't confirm in its window gets the phone
// offer on every run, not only right after pairing, until a phone is linked.
// Enter skips and sends nothing; without a terminal one line names the
// commands; once linked, a re-run says nothing.
func TestPhoneOfferOnEveryRunUntilAPhoneIsLinked(t *testing.T) {
	h, p, _ := familyLink(t)
	p.tapBy = tapFrom("mobile_app_annas_pixel", "anna-id", true)
	enter, one := "\n", "1\n"
	out := runPhoneOffer(t, h, &enter, false)
	if !strings.Contains(out, "Confirm risky changes (doors, alarm, deletes) on your phone?") ||
		!strings.Contains(out, "No phone linked. You can link one later: ha-nova setup confirm-phone") || len(p.sent) != 0 {
		t.Fatalf("re-run offer: sent %v\n%s", p.sent, out)
	}
	if out := runPhoneOffer(t, h, nil, false); !strings.Contains(out, phoneNextStep(defaultServerProfileName)) || len(p.sent) != 0 {
		t.Fatalf("without a terminal: sent %v\n%s", p.sent, out)
	}
	if runPhoneOffer(t, h, &one, false); linked(t, h) == nil {
		t.Fatal("picking a phone on a re-run linked nothing")
	}
	p.services = nil
	for _, input := range []*string{&enter, nil} {
		if out := runPhoneOffer(t, h, input, false); out != "" || len(p.services) != 0 {
			t.Fatalf("a re-run with a linked phone: %q", out)
		}
	}
	if out := runPhoneOffer(t, h, &enter, true); !strings.Contains(out, "Linked now: “Anna’s Pixel”") || !strings.Contains(out, phoneLostLine) {
		t.Fatalf("right after pairing: %q", out)
	}
}

// The offer on a window-capable App reads the gate as a gated send does: a
// phone linked there records that this App gates (gate_seen), so a rollback
// after it still refuses.
func TestPhoneOfferOnAGatedAppRecordsTheGate(t *testing.T) {
	g := &fakeGate{phones: true}
	h := gatedFamily(t, g, false)
	usePhones(t)
	phoneDiscover = discoverPhone
	if gateSeen(h.paths, activeServerProfile()) {
		t.Fatal("gate seen before the offer")
	}
	one := "1\n"
	if out := runPhoneOffer(t, h, &one, true); linked(t, h) == nil || len(g.links) != 1 || !gateSeen(h.paths, activeServerProfile()) {
		t.Fatalf("offer: links %v seen %v\n%s", g.links, gateSeen(h.paths, activeServerProfile()), out)
	}
	h.log.gate.Store(false) // the App was rolled back
	if _, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front"); env.Error == nil || env.Error.Code != "GATE_ROLLED_BACK" {
		t.Fatalf("after a rollback: %+v", env)
	}
}

// A computer the App lets confirm in its window still gets the offer on a
// re-run where no window can open here (SSH); with a window, it doesn't.
func TestPhoneOfferWhereTheWindowCantOpenHere(t *testing.T) {
	g := &fakeGate{phones: true}
	h := gatedFamily(t, g, false)
	usePhones(t)
	phoneDiscover = discoverPhone
	h.log.me.Store(map[string]any{"window": true, "why": "on"})
	saved := windowAvailable
	t.Cleanup(func() { windowAvailable = saved })
	enter := "\n"
	windowAvailable = func() bool { return true }
	if out := runPhoneOffer(t, h, &enter, false); out != "" {
		t.Fatalf("window here: %q", out)
	}
	windowAvailable = func() bool { return false }
	if out := runPhoneOffer(t, h, &enter, false); !strings.Contains(out, "Confirm risky changes (doors, alarm, deletes) on your phone?") || len(g.links) != 0 {
		t.Fatalf("over SSH: links %v\n%s", g.links, out)
	}
}

func TestPhoneNextStepNamesTheProfile(t *testing.T) {
	if got := phoneNextStep("cabin"); got != `No phone linked to this computer yet? To confirm risky changes on a phone, run ha-nova setup confirm-phone --server cabin list, then ha-nova setup confirm-phone --server cabin "<phone name>" and tap the button in the notification on that phone.` {
		t.Fatalf("%q", got)
	}
}

// An explicit pick of the default profile stays in the retry: a different
// default_server would reroute the bare command.
func TestConfirmPhoneCommandKeepsAnExplicitDefaultPick(t *testing.T) {
	saved := selectionOverride()
	t.Cleanup(func() { setServerSelectionOverride(saved) })
	setServerSelectionOverride("")
	t.Setenv("HA_NOVA_SERVER", "")
	if got := confirmPhoneCommand(defaultServerProfileName); got != "ha-nova setup confirm-phone" {
		t.Fatalf("no pick: %q", got)
	}
	t.Setenv("HA_NOVA_SERVER", defaultServerProfileName)
	if got := confirmPhoneCommand(defaultServerProfileName); got != "ha-nova setup confirm-phone --server default" {
		t.Fatalf("picked: %q", got)
	}
}

// MAR-244: the waiting line names the phone only when the App asks it.
func TestGateWaitingTextFollowsTheAsksChannels(t *testing.T) {
	if got := gateWaitingText("en", []string{"phone", "page"}); !strings.Contains(got, "notification on your phone") || !strings.Contains(got, "NOVA page") {
		t.Fatalf("phone: %q", got)
	}
	if got := gateWaitingText("en", []string{"phone"}); !strings.Contains(got, "notification on your phone") || strings.Contains(got, "NOVA page") {
		t.Fatalf("phone only: %q", got)
	}
	got := gateWaitingText("en", []string{"page"})
	if strings.Contains(got, "linked phone") || !strings.Contains(got, "NOVA page") || !strings.Contains(got, "ha-nova setup confirm-phone") {
		t.Fatalf("page only: %q", got)
	}
}
