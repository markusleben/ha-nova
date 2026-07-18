package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
)

func verifySetupConnection(reader *bufio.Reader, out io.Writer, cfg runtimeConfig, token string, reuseToken bool, credentialRepair setupCredentialRepairMode, forceWSPing bool) (string, bool, error) {
	lastIssue := setupIssueRelayUnreachable
	for attempt := 0; attempt < 3; attempt++ {
		readiness, issue, ok := verifySetupConnectionOnce(out, cfg, token, forceWSPing)
		if ok {
			return "", true, nil
		}
		lastIssue = issue
		// Connection and classified credential failures get the repair menu on
		// first runs too. A wrong address or rejected inbound/upstream token
		// must route to its own recovery surface instead of generic retries.
		repairMode := detectSetupRepairMode(readiness, issue)
		classifiedCredentialFailure := repairMode == setupRepairModeRelayAuth || repairMode == setupRepairModeUpstreamAuth
		if reuseToken || issue == setupIssueRelayUnreachable || classifiedCredentialFailure {
			action, repairErr := runSetupRepairFlow(reader, out, cfg, readiness, issue, credentialRepair)
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
			case setupRepairActionBackToPairing:
				return issue, false, errSetupPairingStep
			case setupRepairActionChangeHost:
				return issue, false, errSetupHostStep
			case setupRepairActionRunInstall:
				return issue, false, errSetupInstallStep
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

func verifySetupConnectionOnce(out io.Writer, cfg runtimeConfig, token string, forceWSPing bool) (relayReadiness, string, bool) {
	if err := probeHTTPForSetup(cfg.HAURL); err != nil {
		renderSetupErrorLine(out, "Home Assistant unreachable: %s", err)
		fmt.Fprintln(out, "  Check that Home Assistant is running and reachable from this computer.")
		return relayReadiness{}, setupIssueRelayUnreachable, false
	}

	readiness := checkRelayReadinessWithProbes(cfg.RelayBaseURL, token, fetchRelayHealthForSetup, probeRelayWSPingForSetup, forceWSPing)
	if readiness.HealthErr != nil {
		renderSetupErrorLine(out, "Relay health failed: %s", readiness.HealthErr)
		fmt.Fprintln(out, "  Check NOVA Relay app status and the saved relay token in Home Assistant.")
		return readiness, setupIssueRelayUnreachable, false
	}

	renderSetupSuccessLine(out, "Home Assistant reachable: %s", cfg.HAURL)
	renderSetupSuccessLine(out, "Relay health reachable: %s/health", cfg.RelayBaseURL)
	if readiness.UsedWSPing && readiness.WSReady {
		renderSetupSuccessLine(out, "Relay /ws ping succeeded")
	}
	if readiness.WSReady {
		renderSetupSuccessLine(out, "Connected to Home Assistant")
		return readiness, "", true
	}

	renderSetupErrorLine(out, "Home Assistant WebSocket is not connected yet.")
	if readiness.WSPingErr == nil {
		switch {
		case readiness.UpstreamAuthIssue:
			fmt.Fprintln(out, `  NOVA Relay's upstream Home Assistant access was rejected.`)
			fmt.Fprintln(out, `  App install: update or restart NOVA Relay. Standalone Container/Core: replace HA_LLAT in the server environment.`)
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
