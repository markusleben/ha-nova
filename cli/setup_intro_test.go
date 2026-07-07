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
		`the "skills"`,
		"press Enter — that opens a",
		"come back to this window",
		"You'll need:",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("intro missing %q:\n%s", want, rendered)
		}
	}
}
