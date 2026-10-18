package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// "Sign in and install NOVA" in the App step of setup: the owner signs in
// once, HA NOVA installs (or updates) and starts its App through the
// Supervisor (ha_supervisor_app.go), and the same sign-in then connects this
// computer. Only an explicit menu choice starts it; without a terminal and a
// desktop browser it is never offered.

const relayContainerGuideURL = haNovaRepositoryURL + "/blob/main/docs/reference/relay-container.md"

// setupInstalledSignIn hands the owner's ingress session from the App step to
// the pairing step. Memory only; the pairing step takes it once.
var setupInstalledSignIn *haSignInSession

// Test seams.
var (
	openHASignInInstallForSetup = openHASignInIngressWith
	haAppInstallOffered         = defaultHAAppInstallOffered
)

// defaultHAAppInstallOffered: a desktop sign-in is possible, the relay address
// is the one derived from this Home Assistant (a custom address may be another
// machine), and this build installs the official App (developer builds run a
// local App).
func defaultHAAppInstallOffered(out io.Writer, cfg runtimeConfig) bool {
	derived := strings.TrimRight(deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost), "/")
	slug, err := selectedCloudNOVAAppSlug()
	return err == nil && slug == HAOfficialNOVAAppSlug && derived != "" &&
		strings.TrimRight(strings.TrimSpace(cfg.RelayBaseURL), "/") == derived &&
		haSignInPossible(out, cfg)
}

type appInstallResult int

const (
	appInstallManual   appInstallResult = iota // show today's App page steps
	appInstallDone                             // App ready; the sign-in waits for pairing
	appInstallContinue                         // go on to pairing without the App page steps
)

// runSignInAppInstall offers the install (or update) and runs it after an
// explicit choice. Every failure prints one line, what Home Assistant holds
// now, and the next step; nothing is ever uninstalled.
func runSignInAppInstall(reader *bufio.Reader, out io.Writer, cfg runtimeConfig, update bool, installedVersion, minimum string) (appInstallResult, error) {
	skip := appInstallManual
	if update {
		skip = appInstallContinue
	}
	origin, err := resolveHASignInOriginForSetup(context.Background(), cfg.HAURL)
	if err != nil {
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, HACurrentUser{}))
		return skip, nil
	}
	lead := "HA NOVA can add its App to your Home Assistant for you."
	does := "HA NOVA then adds its App source, installs or updates and starts only its own App, and connects this computer."
	if update {
		lead = fmt.Sprintf("Your NOVA App (v%s) is older than this HA NOVA needs (v%s).", installedVersion, minimum)
		does = "HA NOVA then updates only its own App and connects this computer."
	}
	renderSetupParagraph(out, lead,
		"Your browser opens Home Assistant: sign in as the owner.",
		"(Home Assistant asks to give “http://127.0.0.1:…” access: that's this computer.)",
		does,
	)
	again := false
	for {
		choice, err := promptAppInstallChoice(reader, out, update, again)
		if err != nil || choice != "signin" {
			return skip, err
		}
		var state novaAppState
		prepare := func(ctx context.Context, socket haWSCaller, user HACurrentUser) error {
			renderSetupSuccessLine(out, "Signed in to Home Assistant as %s (owner)", haSignInUserName(user))
			if err := newNOVAAppSupervisor(socket).ensure(ctx, out, &state, update, minimum); err != nil {
				return err
			}
			return waitForNOVAAppSignIn(ctx, out, socket, cfg.RelayBaseURL)
		}
		session, err := openHASignInInstallForSetup(context.Background(), out, origin.String(), prepare)
		if err == nil {
			setupInstalledSignIn = &session
			renderSetupSuccessLine(out, "NOVA App is ready")
			return appInstallDone, nil
		}
		var failure *novaAppInstallError
		switch {
		case errors.Is(err, errAppInstallNoSupervisor) || errors.Is(err, errAppInstallUnsupported):
			line := "This Home Assistant has no Apps (a Container or Core install)."
			if errors.As(err, &failure) {
				line = failure.line
			}
			renderSetupWarningLine(out, "%s", line)
			return appInstallContinue, promptContainerGuide(reader, out)
		case errors.As(err, &failure):
			renderSetupWarningLine(out, "%s", failure.line)
		case errors.Is(err, errSignInStopped) && state != (novaAppState{}):
			renderSetupWarningLine(out, "Stopped.")
		case errors.Is(err, errSignInNotOwner):
			renderSetupWarningLine(out, "Signed in as %s, who is not the Home Assistant owner. Only the owner can install the NOVA App.", haSignInUserName(session.user))
		default:
			renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, session.user))
		}
		renderSetupParagraphTight(out, describeNOVAAppState(state))
		// Retry only while nothing has been changed yet: a stopped or wrong
		// sign-in, never a half-done install.
		if haSignInRetryable(err) && state == (novaAppState{}) {
			again = true
			continue
		}
		switch {
		case state.running && state.busy == "":
			renderSetupParagraph(out, "The NOVA App is running. Connect this computer next.")
			return appInstallContinue, nil
		case state.busy != "":
			renderSetupParagraph(out, "Check the App page: wait until Home Assistant finishes, then click Start.")
			return appInstallManual, nil
		case update && !state.installed:
			return appInstallContinue, nil
		default:
			renderSetupParagraph(out, "Finish it on the App page instead.")
			return appInstallManual, nil
		}
	}
}

// promptContainerGuide: without Apps, NOVA runs as its own container.
func promptContainerGuide(reader *bufio.Reader, out io.Writer) error {
	renderSetupParagraph(out, "Run NOVA as a container instead; the guide shows how.")
	renderSetupLink(out, "Guide:", relayContainerGuideURL)
	_, err := promptWizardLineFromReader(reader, out, "Press Enter when the NOVA container is running", "")
	return err
}

// waitForNOVAAppSignIn waits until the started App serves sign-in pairing.
// Each poll also reads the sign-in WebSocket so Home Assistant's heartbeat
// keeps it open for the ingress session afterwards.
func waitForNOVAAppSignIn(ctx context.Context, out io.Writer, socket haWSCaller, relayURL string) error {
	ready, _ := runSetupCountdownSpinnerWithResult(out, "Waiting for the NOVA App to get ready...", haAppReadyTimeout, setupSpinnerDebounce, func() (bool, error) {
		deadline := time.After(haAppReadyTimeout)
		for !relayAdvertisesSignInForSetup(relayURL) {
			_ = socket.Call(ctx, "auth/current_user", nil, nil)
			select {
			case <-ctx.Done():
				return false, nil
			case <-deadline:
				return false, nil
			case <-time.After(haAppReadyPoll):
			}
		}
		return true, nil
	})
	if !ready {
		return &novaAppInstallError{line: "The NOVA App started but didn't get ready to connect computers within two minutes."}
	}
	return nil
}

var relayAdvertisesSignInForSetup = relayAdvertisesSignIn

// describeNOVAAppState says what Home Assistant holds after a failure.
func describeNOVAAppState(state novaAppState) string {
	version := ""
	if state.version != "" {
		version = " (v" + state.version + ")"
	}
	switch {
	case state.busy != "":
		return "Home Assistant may still be " + state.busy + "."
	case state.running:
		return "The NOVA App" + version + " is installed and running."
	case state.installed:
		return "The NOVA App" + version + " is installed but not running."
	case state.sourceListed && state.known:
		return "Home Assistant lists the NOVA App source; the App is not installed."
	case state.sourceListed:
		return "Home Assistant lists the NOVA App source."
	}
	return "Nothing was changed in Home Assistant."
}

// promptAppInstallChoice returns "signin" or "manual"; the consent is the
// named choice itself.
func promptAppInstallChoice(reader *bufio.Reader, out io.Writer, update, again bool) (string, error) {
	signIn, other, otherWord := "Sign in and install NOVA", "Install it myself in the browser", "manual"
	if update {
		signIn, other, otherWord = "Sign in and update NOVA", "Update later", "later"
	}
	if again {
		signIn = strings.Replace(signIn, "Sign in", "Sign in again", 1)
	}
	spec := setupMenuSpec{
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit",
		AllowBack:    true,
		DefaultValue: "signin",
		Options: []setupMenuOption{
			{Value: "signin", Label: signIn},
			{Value: "manual", Label: other},
		},
	}
	label := fmt.Sprintf("Type 'yes' to %s, or '%s'", strings.ToLower(signIn[:1])+signIn[1:], otherWord)
	return promptSetupMenu(reader, out, spec, func() (string, error) {
		for {
			answer, err := promptWizardLineFromReader(reader, out, label, "")
			if err != nil {
				return "", err
			}
			switch strings.ToLower(strings.TrimSpace(answer)) {
			case "y", "yes":
				return "signin", nil
			case otherWord, "no", "n":
				return "manual", nil
			}
			renderSetupErrorLine(out, "Type 'yes' or '%s'.", otherWord)
		}
	})
}
