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

func TestPhoneNextStepNamesTheProfile(t *testing.T) {
	if got := phoneNextStep("cabin"); got != `To confirm risky changes on a phone, run ha-nova setup confirm-phone --server cabin list, then ha-nova setup confirm-phone --server cabin "<phone name>" and tap Allow on that phone.` {
		t.Fatalf("%q", got)
	}
}

// MAR-244: the waiting line names the phone only when the App asks it.
func TestGateWaitingTextFollowsTheAsksChannels(t *testing.T) {
	if got := gateWaitingText("en", []string{"phone", "page"}); !strings.Contains(got, "notification on your phone") {
		t.Fatalf("phone: %q", got)
	}
	got := gateWaitingText("en", []string{"page"})
	if strings.Contains(got, "linked phone") || !strings.Contains(got, "NOVA page") || !strings.Contains(got, "ha-nova setup confirm-phone") {
		t.Fatalf("page only: %q", got)
	}
}
