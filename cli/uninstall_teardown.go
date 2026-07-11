package main

import (
	"bufio"
	"fmt"
	"io"
	"time"
)

type teardownOutcome int

const (
	teardownNotOffered teardownOutcome = iota
	teardownDeclined
	teardownCancelled
	teardownCompleted
	// teardownCompletedUnverified: the user walked all steps, but the relay
	// still answered the post-removal probes — the final report must keep the
	// full HA checklist instead of claiming the server side is gone.
	teardownCompletedUnverified
)

// teardownDeps isolates the guided teardown's side effects so tests can
// script the whole flow with a reader and a buffer.
type teardownDeps struct {
	openURL     func(io.Writer, string)
	relayHealth func(relayBaseURL, token string) ([]byte, error)
	probeHA     func(url string) error
	sleep       func(time.Duration)
}

func defaultTeardownDeps() teardownDeps {
	return teardownDeps{
		openURL:     openAnnouncedBrowserURL,
		relayHealth: fetchRelayHealth,
		probeHA:     probeHTTP,
		sleep:       time.Sleep,
	}
}

const (
	teardownStageOffer = iota
	teardownStageApp
	teardownStageRepo
	teardownStageLLAT
)

// maybeOfferGuidedTeardown walks the server-side removal (NOVA Relay app,
// HA NOVA repository, "NOVA" access token) before any local file is touched.
// It mirrors the setup wizard: announce a link, open the browser, guide the
// clicks, continue on Enter. `back` returns to the previous step, `exit`
// cancels the entire uninstall — both are always safe because this flow
// deletes nothing locally.
func maybeOfferGuidedTeardown(reader *bufio.Reader, out io.Writer, preflight uninstallPreflight, deps teardownDeps) (teardownOutcome, error) {
	if preflight.haURL == "" {
		return teardownNotOffered, nil
	}
	if err := deps.probeHA(preflight.haURL); err != nil {
		renderSetupParagraphTight(out, "Home Assistant is not reachable right now — the server-side cleanup checklist is included at the end for when it is back online.")
		return teardownNotOffered, nil
	}

	stage := teardownStageOffer
	// Trust-the-user default: only a probe that POSITIVELY shows the relay
	// still answering downgrades the outcome — repo removal and LLAT
	// revocation are unverifiable by design.
	relayVerifiedGone := true
	for {
		switch stage {
		case teardownStageOffer:
			renderSetupParagraph(out, "Home Assistant still hosts the server side: the NOVA Relay app, the HA NOVA repository, and the \"NOVA\" access token.")
			confirmed, err := promptWizardYesNoFromReader(reader, out, "Remove them now with a guided walkthrough?", true)
			if err == errSetupBack {
				return teardownDeclined, nil
			}
			if err == errSetupExit {
				return teardownCancelled, nil
			}
			if err != nil {
				return teardownNotOffered, err
			}
			if !confirmed {
				return teardownDeclined, nil
			}
			stage = teardownStageApp

		case teardownStageApp:
			renderSetupStep(out, 1, 3, "Uninstall the NOVA Relay app")
			renderSetupLink(out, "This will open:", haRelayAppPageURL(preflight.haURL))
			_, err := promptWizardLineFromReader(reader, out, "Press Enter to open your browser", "")
			if err == errSetupBack {
				stage = teardownStageOffer
				continue
			}
			if err == errSetupExit {
				return teardownCancelled, nil
			}
			if err != nil {
				return teardownNotOffered, err
			}
			deps.openURL(out, haRelayAppPageURL(preflight.haURL))
			renderSetupIndentedBlock(out, "On the NOVA Relay page:", "    ",
				"1. Click Stop if the app is running",
				"2. Click Uninstall and confirm",
			)
			_, err = promptWizardLineFromReader(reader, out, "Press Enter when the app is removed", "")
			if err == errSetupBack {
				stage = teardownStageOffer
				continue
			}
			if err == errSetupExit {
				return teardownCancelled, nil
			}
			if err != nil {
				return teardownNotOffered, err
			}
			relayVerifiedGone = verifyRelayGone(out, preflight, deps)
			stage = teardownStageRepo

		case teardownStageRepo:
			renderSetupStep(out, 2, 3, "Remove the HA NOVA repository")
			renderSetupLink(out, "This will open:", haAppStoreURL(preflight.haURL))
			_, err := promptWizardLineFromReader(reader, out, "Press Enter to open your browser", "")
			if err == errSetupBack {
				stage = teardownStageApp
				continue
			}
			if err == errSetupExit {
				return teardownCancelled, nil
			}
			if err != nil {
				return teardownNotOffered, err
			}
			deps.openURL(out, haAppStoreURL(preflight.haURL))
			renderSetupIndentedBlock(out, "In the app store:", "    ",
				"1. Open the three-dot menu in the top-right corner",
				"2. Choose \"Repositories\"",
				"3. Remove "+haNovaRepositoryURL,
			)
			renderSetupParagraphTight(out, "Home Assistant only allows removing the repository after the app itself is uninstalled.")
			_, err = promptWizardLineFromReader(reader, out, "Press Enter when the repository is removed", "")
			if err == errSetupBack {
				stage = teardownStageApp
				continue
			}
			if err == errSetupExit {
				return teardownCancelled, nil
			}
			if err != nil {
				return teardownNotOffered, err
			}
			stage = teardownStageLLAT

		case teardownStageLLAT:
			renderSetupStep(out, 3, 3, "Revoke the Home Assistant access token")
			renderSetupLink(out, "This will open:", haProfileSecurityURL(preflight.haURL))
			_, err := promptWizardLineFromReader(reader, out, "Press Enter to open your browser", "")
			if err == errSetupBack {
				stage = teardownStageRepo
				continue
			}
			if err == errSetupExit {
				return teardownCancelled, nil
			}
			if err != nil {
				return teardownNotOffered, err
			}
			deps.openURL(out, haProfileSecurityURL(preflight.haURL))
			renderSetupIndentedBlock(out, "On your profile's Security tab:", "    ",
				"1. Scroll to \"Long-lived access tokens\"",
				"2. Find the token named \"NOVA\"",
				"3. Delete it",
			)
			_, err = promptWizardLineFromReader(reader, out, "Press Enter when the token is revoked", "")
			if err == errSetupBack {
				stage = teardownStageRepo
				continue
			}
			if err == errSetupExit {
				return teardownCancelled, nil
			}
			if err != nil {
				return teardownNotOffered, err
			}
			if !relayVerifiedGone {
				return teardownCompletedUnverified, nil
			}
			return teardownCompleted, nil
		}
	}
}

// verifyRelayGone is the one verifiable teardown step: after the app is
// removed, the relay must stop answering. A still-answering relay only warns
// and never blocks — the user may legitimately run a second instance, and the
// CLI cannot tell the difference. It returns false only in that
// still-answering case so the final report keeps the full checklist instead
// of claiming the server side is gone; with nothing to probe (no base URL or
// token) it stays trust-the-user, like the repo and LLAT steps. The token
// revocation step stays deliberately unverifiable: the CLI never held the LLAT.
func verifyRelayGone(out io.Writer, preflight uninstallPreflight, deps teardownDeps) bool {
	if preflight.config.RelayBaseURL == "" || preflight.relayToken == "" {
		return true
	}
	session := resolveStatusUISession(out)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			deps.sleep(2 * time.Second)
		}
		if _, err := deps.relayHealth(preflight.config.RelayBaseURL, preflight.relayToken); err != nil {
			fmt.Fprintf(out, "  %s Relay no longer answers — app removed.\n", session.style("success", session.successMarker()))
			return true
		}
	}
	fmt.Fprintf(out, "  %s The relay still answers at %s. If you run more than one instance this is expected; otherwise finish the app removal in Home Assistant.\n", session.style("warning", session.warningMarker()), preflight.config.RelayBaseURL)
	return false
}

// teardownCompletedNoteLines replaces the server-side checklist after a
// completed guided teardown. Standard mode keeps the connection config on
// purpose, so it gets the pointing-at-nothing hint.
func teardownCompletedNoteLines(mode uninstallMode) []string {
	notes := []string{"Server side removed in Home Assistant (app, repository, access token)."}
	if mode != uninstallModePurge {
		notes = append(notes, "The kept connection config now points at nothing. Run 'ha-nova uninstall --purge' to clear it, or keep it for a reinstall.")
	}
	return notes
}

func printTeardownCompletedNotes(out io.Writer, mode uninstallMode) {
	session := resolveStatusUISession(out)
	for _, note := range teardownCompletedNoteLines(mode) {
		fmt.Fprintf(out, "  %s %s\n", session.style("success", session.successMarker()), note)
	}
}
