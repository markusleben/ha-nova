package main

import (
	"bufio"
	"strings"
	"testing"
)

// interactiveSetupUI: a terminal setup with someone to ask.
type interactiveSetupUI struct{ setupTextUI }

func (interactiveSetupUI) interactive() bool { return true }

// Once setup works in a terminal, a pending NOVA App update gets the offer
// ha-nova update makes; not on the page, in
// service mode, with Link paused, or with no one to ask.
func TestSetupOffersTheAppUpdateOnceItWorks(t *testing.T) {
	notice := relayUpdateNoticeForSetup
	t.Cleanup(func() { relayUpdateNoticeForSetup = notice })
	reader := bufio.NewReader(strings.NewReader(""))
	terminal := interactiveSetupUI{setupTextUI{reader: reader, out: &strings.Builder{}}}
	for _, tc := range []struct {
		name   string
		flow   setupFlow
		offers bool
	}{
		{"terminal", setupFlow{ui: terminal}, true},
		{"page", setupFlow{ui: terminal, pageControlled: true}, false},
		{"service mode", setupFlow{ui: terminal, serviceMode: true}, false},
		{"Link paused", setupFlow{ui: terminal, cloudSetupPaused: true}, false},
		{"no one to ask", setupFlow{ui: setupTextUI{reader: reader, out: &strings.Builder{}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			offered := false
			relayUpdateNoticeForSetup = func(runtimePaths) humanNotice { offered = true; return humanNotice{} }
			tc.flow.offerAppUpdate()
			if offered != tc.offers {
				t.Fatalf("offered %v, want %v", offered, tc.offers)
			}
		})
	}
}
