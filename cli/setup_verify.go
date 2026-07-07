package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

func verifySetupConnection(reader *bufio.Reader, out io.Writer, cfg runtimeConfig, token string, reuseToken bool, allowRelayTokenStep bool) (string, bool, error) {
	lastIssue := setupIssueRelayUnreachable
	for attempt := 0; attempt < 3; attempt++ {
		readiness, issue, ok := verifySetupConnectionOnce(out, cfg, token)
		if ok {
			return "", true, nil
		}
		lastIssue = issue
		// Connection-level failures get the repair menu on first runs too, so
		// a wrong/stale Home Assistant address is recoverable right where it
		// fails instead of only via multi-step back navigation.
		if reuseToken || issue == setupIssueRelayUnreachable {
			action, repairErr := runSetupRepairFlow(reader, out, cfg, readiness, issue, allowRelayTokenStep)
			if repairErr != nil {
				if errors.Is(repairErr, io.EOF) || errors.Is(repairErr, io.ErrUnexpectedEOF) {
					// Input ended at the repair prompt (piped/aborted stdin).
					// Behave like "Stop for now": the caller persists progress
					// and shows the incomplete banner instead of a hard error
					// that would skip saving tokens/config gathered so far.
					return issue, false, nil
				}
				return "", false, repairErr
			}
			switch action {
			case setupRepairActionRetry:
				continue
			case setupRepairActionBackToRelayToken:
				return issue, false, errSetupRelayTokenStep
			case setupRepairActionChangeHost:
				return issue, false, errSetupHostStep
			case setupRepairActionStop:
				return issue, false, nil
			case setupRepairActionBack:
				return issue, false, errSetupBack
			default:
				return issue, false, nil
			}
		}
		retryLabel := "Retry WebSocket check?"
		if issue == setupIssueRelayUnreachable {
			retryLabel = "Retry connection check?"
		}
		retry, retryErr := promptWizardYesNoFromReader(reader, out, retryLabel, true)
		if retryErr != nil {
			return "", false, retryErr
		}
		if !retry {
			return issue, false, nil
		}
	}

	if lastIssue == "" {
		lastIssue = setupIssueGeneric
	}
	return lastIssue, false, nil
}

func verifySetupConnectionOnce(out io.Writer, cfg runtimeConfig, token string) (relayReadiness, string, bool) {
	if err := probeHTTPForSetup(cfg.HAURL); err != nil {
		renderSetupErrorLine(out, "Home Assistant unreachable: %s", err)
		fmt.Fprintln(out, "  Check that Home Assistant is running and reachable from this computer.")
		return relayReadiness{}, setupIssueRelayUnreachable, false
	}

	readiness := checkRelayReadinessWithProbes(cfg.RelayBaseURL, token, fetchRelayHealthForSetup, probeRelayWSPingForSetup)
	if readiness.HealthErr != nil {
		renderSetupErrorLine(out, "Relay health failed: %s", readiness.HealthErr)
		fmt.Fprintln(out, "  Check NOVA Relay app status and the saved relay token in Home Assistant.")
		return readiness, setupIssueRelayUnreachable, false
	}

	printHumanInfo("Home Assistant reachable: %s", cfg.HAURL)
	printHumanInfo("Relay health reachable: %s/health", cfg.RelayBaseURL)
	if readiness.UsedWSPing && readiness.WSReady {
		printHumanInfo("Relay /ws ping succeeded")
	}
	if readiness.WSReady {
		printHumanInfo("Connected to Home Assistant")
		return readiness, "", true
	}

	renderSetupErrorLine(out, "Home Assistant WebSocket is not connected yet.")
	if readiness.WSPingErr == nil {
		switch {
		case readiness.LLATIssue:
			fmt.Fprintln(out, `  The Home Assistant Access Token in NOVA Relay still needs to be checked.`)
			fmt.Fprintln(out, `  Set the "Home Assistant Access Token" field ("ha_llat") to a valid Long-Lived Access Token, save, and restart the App.`)
		case readiness.RelayAuthIssue:
			fmt.Fprintln(out, `  The Relay Auth Token in NOVA Relay still needs to be checked.`)
			fmt.Fprintln(out, `  Verify the "Relay Auth Token" field ("relay_auth_token"), save, and restart the App.`)
		default:
			fmt.Fprintln(out, "  NOVA Relay is running, but it still can't finish the Home Assistant connection.")
			fmt.Fprintln(out, "  Verify the app settings and restart the App if needed.")
		}
	} else {
		fmt.Fprintln(out, "  NOVA Relay is reachable, but the Home Assistant WebSocket probe failed before it could prove the exact cause.")
		fmt.Fprintln(out, "  Verify the app settings and restart the App if needed.")
	}
	return readiness, setupIssueWSDegraded, false
}
