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
		retry, retryErr := promptWizardYesNoMenu(reader, out, "Check the connection again?", "Yes, check again", "No, stop for now", true)
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
		if hint := reachProblem(cfg.HAURL, err); hint != "" {
			renderSetupErrorLine(out, "%s", hint)
		} else {
			renderSetupErrorLine(out, "This computer can't reach Home Assistant. Details: %s", err)
			fmt.Fprintln(out, "  Check that Home Assistant is running.")
		}
		return relayReadiness{}, setupIssueRelayUnreachable, false
	}

	readiness := checkRelayReadinessWithProbes(cfg.RelayBaseURL, token, fetchRelayHealthForSetup, probeRelayWSPingForSetup, forceWSPing)
	if readiness.HealthErr != nil {
		renderSetupErrorLine(out, "HA NOVA couldn't check the NOVA App. Details: %s", readiness.HealthErr)
		fmt.Fprintln(out, `  Check that the NOVA App is running, and that the saved key matches its "Relay Auth Token".`)
		return readiness, setupIssueRelayUnreachable, false
	}

	renderSetupSuccessLine(out, "Home Assistant answers")
	renderSetupSuccessLine(out, "NOVA App answers")
	if readiness.WSReady {
		renderSetupSuccessLine(out, "Connected to Home Assistant")
		return readiness, "", true
	}

	renderSetupErrorLine(out, "The NOVA App isn't connected to Home Assistant yet.")
	if readiness.WSPingErr == nil {
		switch {
		case readiness.UpstreamAuthIssue:
			fmt.Fprintln(out, `  Home Assistant turned down the NOVA App's login.`)
			fmt.Fprintln(out, `  Restart or update the NOVA App. NOVA Relay in your own container? Give it a new Home Assistant token (HA_LLAT).`)
		case readiness.RelayAuthIssue:
			fmt.Fprintln(out, `  The NOVA App turned down this computer's shared access key.`)
			fmt.Fprintln(out, `  In the NOVA App's Configuration tab, check the "Relay Auth Token" field. Save, then restart the App.`)
		default:
			fmt.Fprintln(out, "  The NOVA App runs, but can't reach Home Assistant yet.")
			fmt.Fprintln(out, "  Check the App's settings. Restart the App if needed.")
		}
	} else {
		fmt.Fprintln(out, "  The NOVA App answers, but HA NOVA couldn't check its link to Home Assistant.")
		fmt.Fprintln(out, "  Check the App's settings. Restart the App if needed.")
	}
	return readiness, setupIssueWSDegraded, false
}
