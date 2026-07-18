package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type setupRepairMode string
type setupCredentialRepairMode string

const (
	setupRepairModeConnection   setupRepairMode = "connection"
	setupRepairModeUpstreamAuth setupRepairMode = "upstream_auth"
	setupRepairModeRelayAuth    setupRepairMode = "relay_auth"
	setupRepairModeAmbiguous    setupRepairMode = "ambiguous"

	setupCredentialRepairNone    setupCredentialRepairMode = "none"
	setupCredentialRepairToken   setupCredentialRepairMode = "token"
	setupCredentialRepairPairing setupCredentialRepairMode = "pairing"
)

type setupRepairAction string

const (
	setupRepairActionOpenSecurity      setupRepairAction = "security"
	setupRepairActionOpenRelaySettings setupRepairAction = "relay"
	setupRepairActionRetry             setupRepairAction = "retry"
	setupRepairActionBack              setupRepairAction = "back"
	setupRepairActionBackToRelayToken  setupRepairAction = "relay_token"
	setupRepairActionBackToPairing     setupRepairAction = "pairing"
	setupRepairActionChangeHost        setupRepairAction = "change_host"
	setupRepairActionRunInstall        setupRepairAction = "run_install"
	setupRepairActionStop              setupRepairAction = "stop"
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
	if readiness.UpstreamAuthIssue {
		return setupRepairModeUpstreamAuth
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

func runSetupRepairFlow(reader *bufio.Reader, out io.Writer, cfg runtimeConfig, readiness relayReadiness, issue string, credentialRepair setupCredentialRepairMode) (setupRepairAction, error) {
	mode := detectSetupRepairMode(readiness, issue)
	for {
		renderSetupRepairPage(out, mode, cfg.HAHost)
		action, err := promptSetupRepairActionInteractive(reader, out, mode, credentialRepair)
		if err != nil {
			return "", err
		}

		switch action {
		case setupRepairActionOpenSecurity:
			openBrowserShowingURL(out, haProfileSecurityURL(cfg.HAURL))
		case setupRepairActionOpenRelaySettings:
			openBrowserShowingURL(out, haRelayAppPageURL(cfg.HAURL))
			renderSetupParagraphTight(out, `Update or restart the App there. Explicit legacy Relay tokens live on its "Configuration" tab.`)
		case setupRepairActionBackToPairing:
			openBrowserShowingURL(out, haRelayAppPageURL(cfg.HAURL))
			renderSetupParagraphTight(out, `Open NOVA from the sidebar or choose "Open Web UI" on the NOVA Relay app page.`)
			return action, nil
		case setupRepairActionRetry, setupRepairActionBack, setupRepairActionBackToRelayToken, setupRepairActionChangeHost, setupRepairActionRunInstall, setupRepairActionStop:
			return action, nil
		}
	}
}

func renderSetupRepairPage(out io.Writer, mode setupRepairMode, haHost string) {
	renderSetupSectionTitle(out, "Repair this connection step")
	switch mode {
	case setupRepairModeConnection:
		lines := []string{
			"Home Assistant or NOVA Relay is still unreachable from this device.",
			"Fix the local connection first, then retry this step.",
		}
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(haHost)), ".local") {
			lines = append(lines,
				`Tip: names like "homeassistant.local" can stop working (especially on Windows).`,
				"Try the IP address instead — find it in your router, or in Home Assistant under Settings > System > Network.",
			)
		}
		renderSetupParagraph(out, lines...)
	case setupRepairModeUpstreamAuth:
		renderSetupParagraph(out,
			"This device's Relay Auth Token worked.",
			"Only the Relay's upstream Home Assistant authentication still needs attention.",
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

func promptSetupRepairActionFromReader(reader *bufio.Reader, out io.Writer, mode setupRepairMode, credentialRepair setupCredentialRepairMode) (setupRepairAction, error) {
	choices, defaultChoice := setupRepairChoices(mode, credentialRepair)

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
	return promptSetupRepairActionFromReader(reader, out, mode, credentialRepair)
}

func setupRepairChoices(mode setupRepairMode, credentialRepair setupCredentialRepairMode) ([]setupRepairChoice, string) {
	switch mode {
	case setupRepairModeConnection:
		return []setupRepairChoice{
			{Number: "1", Value: setupRepairActionRetry, Label: "Retry now"},
			{Number: "2", Value: setupRepairActionChangeHost, Label: "Change Home Assistant address"},
			{Number: "3", Value: setupRepairActionRunInstall, Label: "Run the app install steps for this address"},
			{Number: "4", Value: setupRepairActionStop, Label: "Stop for now (progress is saved)"},
			{Number: "5", Value: setupRepairActionBack, Label: "Back"},
		}, "1"
	case setupRepairModeUpstreamAuth:
		if credentialRepair == setupCredentialRepairPairing {
			return []setupRepairChoice{
				{Number: "1", Value: setupRepairActionOpenRelaySettings, Label: "Open NOVA Relay app page to update or restart"},
				{Number: "2", Value: setupRepairActionRetry, Label: "Retry now"},
				{Number: "3", Value: setupRepairActionBack, Label: "Back"},
			}, "1"
		}
		return []setupRepairChoice{
			{Number: "1", Value: setupRepairActionOpenRelaySettings, Label: "Open NOVA Relay settings"},
			{Number: "2", Value: setupRepairActionOpenSecurity, Label: "Open Security page (standalone LLAT only)"},
			{Number: "3", Value: setupRepairActionRetry, Label: "Retry now"},
			{Number: "4", Value: setupRepairActionBack, Label: "Back"},
		}, "1"
	case setupRepairModeRelayAuth:
		if credentialRepair == setupCredentialRepairPairing {
			return []setupRepairChoice{
				{Number: "1", Value: setupRepairActionBackToPairing, Label: "Open NOVA and pair this device again"},
				{Number: "2", Value: setupRepairActionOpenRelaySettings, Label: "Open NOVA Relay app page"},
				{Number: "3", Value: setupRepairActionRetry, Label: "Retry now"},
			}, "1"
		}
		if credentialRepair != setupCredentialRepairToken {
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
