package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// The outcome after the optional questions keeps their answers on screen
// (a failed phone link, Link's guidance); without them it starts a fresh
// screen as before.
func TestSetupOutcomeKeepsTheOptionalAnswersOnScreen(t *testing.T) {
	originalTTY, originalInput, originalEnv, originalANSI := writerSupportsTTYForSetup, uiInputSupportsTTY, uiEnvLookup, uiOutputSupportsANSI
	t.Cleanup(func() {
		writerSupportsTTYForSetup, uiInputSupportsTTY, uiEnvLookup, uiOutputSupportsANSI = originalTTY, originalInput, originalEnv, originalANSI
	})
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiEnvLookup = func(string) string { return "" }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	for _, keep := range []bool{false, true} {
		var out bytes.Buffer
		out.WriteString("  Nothing linked: Allow was not tapped in time.\n")
		setupTextUI{out: &out}.outcome(setupOutcome{Kind: setupOutcomeComplete, Clients: []string{"claude"}, Keep: keep})
		cleared := strings.Contains(out.String(), "\x1b[2J")
		if cleared == keep || !strings.Contains(out.String(), setupT("setup.done.complete")) {
			t.Fatalf("keep=%v cleared=%v:\n%q", keep, cleared, out.String())
		}
	}
	if setupKeepScreen {
		t.Fatal("the keep outlived its outcome")
	}
}
