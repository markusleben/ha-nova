package main

import (
	"strings"
	"testing"
)

func TestRenderSetupIntroExplainsProjectAndBrowserBehavior(t *testing.T) {
	output := &strings.Builder{}
	renderSetupIntro(output)

	rendered := output.String()
	for _, want := range []string{
		"lets your AI assistant run your Home Assistant",
		"Steps: pick your AI app, find Home Assistant, add the NOVA App, connect this computer, add the skills.",
		"When a browser page opens, finish there and come back here.",
		"You need the Home Assistant owner's account.",
		"Home Assistant Link only: the NOVA App must already be installed.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("intro missing %q:\n%s", want, rendered)
		}
	}
	// Ratchet: the intro only shrinks; details live in --help and the docs.
	if words := len(strings.Fields(rendered)); words > 58 {
		t.Fatalf("intro has %d words, budget 58:\n%s", words, rendered)
	}
	// Pairing is one code per device; the default path creates no token.
	if strings.Contains(rendered, "access token") {
		t.Fatalf("intro still promises access tokens:\n%s", rendered)
	}
}
