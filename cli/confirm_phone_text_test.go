package main

import (
	"strings"
	"testing"
)

// Every notification: a short question, one line naming the computer, the
// hint last; only a pending action is time-sensitive.
func TestPhoneAskTexts(t *testing.T) {
	host := "“" + phoneHost() + "”"
	cases := []struct {
		got                 phoneAsk
		title, body, button string
		urgent              bool
	}{
		{phoneLinkAsk(), "Link this phone to HA NOVA?", "For confirmations from " + host + ". " + phoneHint, "Link", false},
		{phoneChangeAsk(""), "Stop HA NOVA confirmations here?", "Requested by " + host + ". " + phoneHint, "Stop", false},
		{phoneChangeAsk("Anna’s Pixel"), "Move HA NOVA confirmations to “Anna’s Pixel”?", "Requested by " + host + ". " + phoneHint, "Move", false},
		{phonePlanAsk([2]string{"Delete", "the automation “Night”"}, true), "Delete the automation “Night”?", "Can't be undone. Requested by " + host + ". " + phoneHint, "Delete", true},
		{phonePlanAsk([2]string{"Turn off", "“Alarm”"}, false), "Turn off “Alarm”?", "Requested by " + host + ". " + phoneHint, "Turn off", true},
		{phonePlanAsk([2]string{"Set the position of", "“Gate”"}, false), "Set the position of “Gate”?", "Requested by " + host + ". " + phoneHint, "Confirm", true},
	}
	for _, c := range cases {
		if c.got != (phoneAsk{c.title, c.body, c.button, c.urgent}) {
			t.Errorf("ask %+v, want %q %q %q %v", c.got, c.title, c.body, c.button, c.urgent)
		}
	}
	long := phonePlanAsk([2]string{"Unlock", strings.Repeat("“Door” and ", 20)}, false).title
	if n := len([]rune(long)); n != phoneTitleLimit || !strings.HasSuffix(long, "…?") {
		t.Errorf("long title %d %q", n, long)
	}
}
