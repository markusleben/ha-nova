package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type setupRepairMode string

const (
	setupRepairModeConnection setupRepairMode = "connection"
	setupRepairModeLLAT       setupRepairMode = "llat"
	setupRepairModeRelayAuth  setupRepairMode = "relay_auth"
	setupRepairModeAmbiguous  setupRepairMode = "ambiguous"
)

type setupRepairAction string

const (
	setupRepairActionOpenSecurity      setupRepairAction = "security"
	setupRepairActionOpenRelaySettings setupRepairAction = "relay"
	setupRepairActionRetry             setupRepairAction = "retry"
	setupRepairActionBack              setupRepairAction = "back"
	setupRepairActionBackToRelayToken  setupRepairAction = "relay_token"
)

type setupRepairChoice struct {
	Number string
	Value  setupRepairAction
	Label  string
}

func detectSetupRepairMode(readiness relayReadiness, issue string) setupRepairMode {
	if issue != setupIssueWSDegraded {
		if relayHealthIssueLooksLikeRelayAuth(readiness.HealthErr) || readiness.RelayAuthIssue {
			return setupRepairModeRelayAuth
		}
		return setupRepairModeConnection
	}
	if readiness.LLATIssue {
		return setupRepairModeLLAT
	}
	if readiness.RelayAuthIssue || relayHealthIssueLooksLikeRelayAuth(readiness.HealthErr) {
		return setupRepairModeRelayAuth
	}
	return setupRepairModeAmbiguous
}

func relayHealthIssueLooksLikeRelayAuth(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "http 401") || strings.Contains(text, "http 403") ||
		strings.Contains(text, "unauthorized") || strings.Contains(text, "forbidden")
}

func runSetupRepairFlow(reader *bufio.Reader, out io.Writer, cfg runtimeConfig, readiness relayReadiness, issue string, allowRelayTokenStep bool) (setupRepairAction, error) {
	mode := detectSetupRepairMode(readiness, issue)
	for {
		renderSetupRepairPage(out, mode)
		action, err := promptSetupRepairActionInteractive(reader, out, mode, allowRelayTokenStep)
		if err != nil {
			return "", err
		}

		switch action {
		case setupRepairActionOpenSecurity:
			if err := openBrowserForSetup(cfg.HAURL + "/profile/security"); err != nil {
				printHumanWarn("Browser launch skipped; open this URL manually if needed: %s/profile/security", cfg.HAURL)
			}
		case setupRepairActionOpenRelaySettings:
			if err := openBrowserForSetup(cfg.HAURL + "/hassio/addon/2368fcfa_ha_nova_relay/config"); err != nil {
				printHumanWarn("Browser launch skipped; open this URL manually if needed: %s/hassio/addon/2368fcfa_ha_nova_relay/config", cfg.HAURL)
			}
		case setupRepairActionRetry, setupRepairActionBack, setupRepairActionBackToRelayToken:
			return action, nil
		}
	}
}

func renderSetupRepairPage(out io.Writer, mode setupRepairMode) {
	renderSetupSectionTitle(out, "Repair this connection step")
	switch mode {
	case setupRepairModeConnection:
		renderSetupParagraph(out,
			"Home Assistant or NOVA Relay is still unreachable from this device.",
			"Fix the local connection first, then retry this step.",
		)
	case setupRepairModeLLAT:
		renderSetupParagraph(out,
			"This device's Relay Auth Token worked.",
			"Only the Home Assistant access token still needs attention.",
		)
	case setupRepairModeRelayAuth:
		renderSetupParagraph(out,
			"Home Assistant is reachable.",
			"NOVA Relay needs the Relay Auth Token on this device checked next.",
		)
	default:
		renderSetupParagraph(out,
			"Home Assistant and NOVA Relay are reachable.",
			"Setup still needs one more app-side fix before this device can finish connecting.",
		)
	}
}

func promptSetupRepairActionFromReader(reader *bufio.Reader, out io.Writer, mode setupRepairMode, allowRelayTokenStep bool) (setupRepairAction, error) {
	choices, defaultChoice := setupRepairChoices(mode, allowRelayTokenStep)

	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Next step:")
	fmt.Fprintln(out)
	for _, choice := range choices {
		fmt.Fprintf(out, "    %s) %s\n", choice.Number, choice.Label)
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  Enter [%s-%s] (default %s, or type 'back'/'exit'): ", choices[0].Number, choices[len(choices)-1].Number, defaultChoice)

	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	switch line {
	case "":
		line = defaultChoice
	case "back":
		return setupRepairActionBack, nil
	case "exit":
		return "", errSetupExit
	}

	for _, choice := range choices {
		if line == choice.Number || line == string(choice.Value) {
			return choice.Value, nil
		}
	}
	renderSetupErrorLine(out, "Invalid choice. Please enter one of the listed options.")
	return promptSetupRepairActionFromReader(reader, out, mode, allowRelayTokenStep)
}

func setupRepairChoices(mode setupRepairMode, allowRelayTokenStep bool) ([]setupRepairChoice, string) {
	switch mode {
	case setupRepairModeConnection:
		return []setupRepairChoice{
			{Number: "1", Value: setupRepairActionRetry, Label: "Retry now"},
			{Number: "2", Value: setupRepairActionBack, Label: "Back"},
		}, "1"
	case setupRepairModeLLAT:
		return []setupRepairChoice{
			{Number: "1", Value: setupRepairActionOpenSecurity, Label: "Open Home Assistant Security page"},
			{Number: "2", Value: setupRepairActionOpenRelaySettings, Label: "Open NOVA Relay settings"},
			{Number: "3", Value: setupRepairActionRetry, Label: "Retry now"},
			{Number: "4", Value: setupRepairActionBack, Label: "Back"},
		}, "3"
	case setupRepairModeRelayAuth:
		if !allowRelayTokenStep {
			return []setupRepairChoice{
				{Number: "1", Value: setupRepairActionOpenRelaySettings, Label: "Open NOVA Relay settings"},
				{Number: "2", Value: setupRepairActionRetry, Label: "Retry now"},
				{Number: "3", Value: setupRepairActionBack, Label: "Back"},
			}, "2"
		}
		return []setupRepairChoice{
			{Number: "1", Value: setupRepairActionBackToRelayToken, Label: "Back to Relay token step"},
			{Number: "2", Value: setupRepairActionOpenRelaySettings, Label: "Open NOVA Relay settings"},
			{Number: "3", Value: setupRepairActionRetry, Label: "Retry now"},
		}, "1"
	default:
		return []setupRepairChoice{
			{Number: "1", Value: setupRepairActionOpenSecurity, Label: "Open Home Assistant Security page"},
			{Number: "2", Value: setupRepairActionOpenRelaySettings, Label: "Open NOVA Relay settings"},
			{Number: "3", Value: setupRepairActionRetry, Label: "Retry now"},
			{Number: "4", Value: setupRepairActionBack, Label: "Back"},
		}, "3"
	}
}
