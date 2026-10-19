package main

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
)

// Golden renders of the setup screens in plain and styled terminal mode, so
// the catalog/renderer split keeps the styled output (markers, emphasis,
// step dots) byte-identical too.
func TestSetupGoldenScreens(t *testing.T) {
	for _, mode := range []string{"plain", "styled"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "styled" {
				forceSetupStyledUI(t)
			} else {
				t.Setenv("HA_NOVA_PLAIN_UI", "1")
			}
			out := &bytes.Buffer{}
			section := func(name string) { out.WriteString("\n=== " + name + "\n") }

			section("header")
			renderSetupHeader(out)
			section("intro")
			renderSetupIntro(out)
			section("step")
			renderSetupStep(out, 2, 4, "Connect this computer")
			section("status summary: nothing")
			renderSetupStatusSummary(out, setupState{})
			section("status summary: token only")
			renderSetupStatusSummary(out, setupState{TokenOK: true})
			section("status summary: all")
			renderSetupStatusSummary(out, setupState{RelayOK: true, TokenOK: true, WSOK: true, SkillsOK: true})
			section("discovery results")
			renderSetupDiscoveryResult(out, "ha.lan", "mDNS", true)
			renderSetupDiscoveryResult(out, "homeassistant.local", "", true)
			renderSetupDiscoveryResult(out, "10.0.0.2", "", true)
			renderSetupDiscoveryResult(out, "10.0.0.3", "", false)
			renderSetupDiscoveryResult(out, "", "", false)
			section("complete: one client")
			renderSetupCompleteBanner(out, []string{"claude"}, nil)
			section("complete: two clients")
			renderSetupCompleteBanner(out, []string{"claude", "codex"}, nil)
			section("complete: no client")
			renderSetupCompleteBanner(out, nil, nil)
			section("complete: first look")
			renderSetupCompleteBanner(out, []string{"hermes"}, lookOf(firstLookResult{Home: map[string]int{"devices": 3}}, nil))
			section("already done: legacy token")
			renderSetupAlreadyDoneBanner(out, true)
			section("already done")
			renderSetupAlreadyDoneBanner(out, false)
			section("link fallback ready")
			renderSetupCloudFallbackReadyBanner(out, nil)
			section("link fallback ready: first look")
			renderSetupCloudFallbackReadyBanner(out, lookOf(firstLookResult{Home: map[string]int{"areas": 1}}, nil))
			for _, issue := range []string{setupIssueGeneric, setupIssueRelayUnreachable, setupIssueWSDegraded, setupIssueSkillsInstall, setupIssueCloudAccess} {
				section("incomplete: " + issue)
				renderSetupIncompleteBanner(out, issue)
			}
			section("outcome: link incomplete")
			renderSetupCompletionOutcomeWithCloudPause(out, []string{"codex"}, true, false)
			section("outcome: complete")
			renderSetupCompletionOutcomeWithCloudPause(out, []string{"codex"}, false, false)
			section("cancelled")
			renderSetupCancelledNote(out)

			choices := []setupClientChoice{
				{Number: "1", Value: "claude", Label: "Claude Code"},
				{Number: "2", Value: "codex", Label: "Codex CLI", Disabled: true, DisabledReason: "install Codex CLI first"},
				{Number: "3", Value: "all", Label: "All available clients"},
			}
			section("client menu: invalid, disabled, back, pick")
			answer, err := promptSetupClientInteractive(goldenReader("9\n2\nback\n"), out, choices, "claude")
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")
			answer, err = promptSetupClientInteractive(goldenReader("3\n"), out, choices, "claude")
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")
			section("client menu: default and exit")
			answer, err = promptSetupClientInteractive(goldenReader("\n"), out, choices, "codex")
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")
			answer, err = promptSetupClientInteractive(goldenReader("exit\n"), out, choices, "claude")
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")
			section("client menu: nothing available")
			answer, err = promptSetupClientInteractive(goldenReader("\n"), out, []setupClientChoice{
				{Number: "1", Value: "codex", Label: "Codex CLI", Disabled: true, DisabledReason: "install Codex CLI first"},
				{Number: "2", Value: "all", Label: "All available clients", Disabled: true, DisabledReason: "install a supported AI client first"},
			}, "claude")
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")
			section("token menu: fresh")
			answer, err = promptSetupTokenChoiceInteractive(goldenReader("x\n\n"), out, false)
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")
			section("token menu: saved token")
			answer, err = promptSetupTokenChoiceInteractive(goldenReader("paste\n"), out, true)
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")
			answer, err = promptSetupTokenChoiceInteractive(goldenReader("back\n"), out, true)
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")
			answer, err = promptSetupTokenChoiceInteractive(goldenReader("exit\n"), out, true)
			out.WriteString("\n→ " + answer + " " + errString(err) + "\n")

			assertSetupGolden(t, "screens-"+mode, strings.ReplaceAll(out.String(), "\033", "<ESC>"))
		})
	}
}

func goldenReader(input string) *bufio.Reader {
	return bufio.NewReader(strings.NewReader(input))
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// forceSetupStyledUI makes setup output styled (ANSI, markers, step dots)
// into any writer; the arrow menu still falls back to the numbered prompt
// because the writer is not the real stdout.
func forceSetupStyledUI(t *testing.T) {
	t.Helper()
	t.Setenv("HA_NOVA_PLAIN_UI", "")
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	oldInput, oldTTY, oldANSI := uiInputSupportsTTY, writerSupportsTTYForSetup, uiOutputSupportsANSI
	uiInputSupportsTTY = func() bool { return true }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	t.Cleanup(func() {
		uiInputSupportsTTY, writerSupportsTTYForSetup, uiOutputSupportsANSI = oldInput, oldTTY, oldANSI
	})
}
