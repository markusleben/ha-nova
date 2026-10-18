package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
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

// runSignInAppInstall returns done=true when the App runs and the sign-in
// session waits for pairing. done=false, err=nil means "show the manual steps"
// (install) or "continue without the update" (update).
func runSignInAppInstall(reader *bufio.Reader, out io.Writer, cfg runtimeConfig, update bool, installedVersion, minimum string) (bool, error) {
	origin, err := resolveHASignInOriginForSetup(context.Background(), cfg.HAURL)
	if err != nil {
		renderSetupWarningLine(out, "%s", haSignInFallbackLine(err, HACurrentUser{}))
		return false, nil
	}
	lead := "HA NOVA can add its App to your Home Assistant for you."
	does := "HA NOVA then installs and starts only its own App and connects this computer."
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
			return false, err
		}
		var state novaAppState
		prepare := func(ctx context.Context, socket haWSCaller, user HACurrentUser) error {
			renderSetupSuccessLine(out, "Signed in to Home Assistant as %s (owner)", haSignInUserName(user))
			if err := newNOVAAppSupervisor(socket).ensure(ctx, out, &state, update); err != nil {
				return err
			}
			return waitForNOVAAppSignIn(ctx, out, cfg.RelayBaseURL)
		}
		session, err := openHASignInInstallForSetup(context.Background(), out, origin.String(), prepare)
		if err == nil {
			setupInstalledSignIn = &session
			renderSetupSuccessLine(out, "NOVA App is ready")
			return true, nil
		}
		var failure *novaAppInstallError
		switch {
		case errors.As(err, &failure):
			renderSetupWarningLine(out, "%s", failure.line)
		case errors.Is(err, errAppInstallNoSupervisor):
			renderSetupWarningLine(out, "This Home Assistant has no Apps (a Container or Core install). Run NOVA as a container instead:")
			renderSetupLink(out, "Guide:", relayContainerGuideURL)
			return false, nil
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
		if update {
			renderSetupParagraph(out, "You can update it later in Home Assistant: Settings > Apps > NOVA Relay.")
		} else {
			renderSetupParagraph(out, "Finish it on the App page instead.")
		}
		return false, nil
	}
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
