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
		"connects your AI assistant",
		"This setup will:",
		"connect locally or through Home Assistant Cloud",
		"for local setup:",
		"for Cloud-only setup:",
		`the "skills"`,
		"press Enter — that opens a",
		"come back to this window",
		"You'll need:",
		"NOVA App already installed",
		"Home Assistant Owner",
		"one-time six-digit code",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("intro missing %q:\n%s", want, rendered)
		}
	}
	// Pairing is one code per device; the default path creates no token.
	if strings.Contains(rendered, "access token") {
		t.Fatalf("intro still promises access tokens:\n%s", rendered)
	}
}
