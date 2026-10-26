package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// A leftover Home Assistant Link at the start of a plain interactive setup:
// say what it is, then let a person at a terminal finish it, remove it, or
// go on without it (docs/work/2026-10-26-link-leftover-setup-doctor-spec.md).

// Test seams: the menu needs a terminal; the choices run the existing
// commands in this process. exited: the person left with Ctrl+C.
var (
	leftoverLinkChoiceAvailable = func(out io.Writer) bool {
		return uiInputSupportsTTY() && writerSupportsTTYForSetup(out)
	}
	finishLeftoverLink = func(paths runtimePaths, cfg runtimeConfig) (code int, exited bool) {
		return runCloudConnect(paths, nil, cloudResumeAction(cfg) == "reconnect")
	}
	removeLeftoverLink = func(paths runtimePaths) int {
		return runCloudRemove(paths, cloudCommandFlags{confirmed: true}) // choosing Remove is the confirmation
	}
)

// pageFitsWithoutLink: everything the setup page needs except Link. Flags,
// service installs, named profiles, and a custom Relay address keep the
// text wizard.
func (f *setupFlow) pageFitsWithoutLink() bool {
	return f.target == "" && !f.serviceMode &&
		f.hostFlag == "" && f.haURLFlag == "" && f.relayURLFlag == "" && f.relayTokenFlag == "" &&
		(f.cfg.RelayBaseURL == "" || f.cfg.RelayBaseURL == deriveRelayURLFromHA(f.cfg.HAURL, f.cfg.HAHost)) && // a custom Relay address: the terminal keeps it
		activeServerProfile() == defaultServerProfileName && setupPageAvailable()
}

// settleLeftoverLink runs before handleInteractiveCloudRecoveryBeforeClients
// for a plain setup with a home connection whose Link isn't finished or is
// held. done: setup ends with result. paused: setup goes on with Link as it
// is (what that function would decide). Neither: that function decides,
// with the settings saved now.
func (f *setupFlow) settleLeftoverLink() (result setupStepResult, done, paused bool) {
	cloud := f.cfg.Cloud
	hold := cloudRecoveryHoldProblem(f.cfg) != nil
	if cloud == nil || f.serviceMode || f.explicitLocalSetup || activeServerProfile() != defaultServerProfileName ||
		remoteOnlyCloudSetup(f.cfg) || !cloudRemoteFeatureAvailable() || (!hold && cloud.ready()) {
		return setupStepResult{}, false, false
	}
	out := f.ui.termOut()
	if !leftoverLinkChoiceAvailable(out) {
		if hold {
			return setupStepResult{}, false, false // the hold's steps, exit 1
		}
		if hybridCloudSetupPending(f.cfg) {
			renderSetupParagraph(out, leftoverLinkText(f.cfg))
			return setupStepResult{}, false, false // its resume offer or commands, as before (#813)
		}
		renderSetupParagraph(out, leftoverLinkText(f.cfg), setupT("setup.link.goes_on"))
		return setupStepResult{}, false, true
	}
	canFinish := !hold && !cloud.cleanupPending() && cloudSetupPromptEligible(out)
	choice, err := promptLeftoverLink(f.ui.termIn(), out, leftoverLinkText(f.cfg), canFinish)
	switch {
	case errors.Is(err, errSetupExit):
		return f.leftoverLinkExit(), true, false
	case err != nil:
		return f.failErr(err), true, false
	case choice == "finish" || choice == "remove":
		if err := ensureSetupLifecycleCurrent(f.paths, f.lifecycleMarker...); err != nil {
			return f.failErr(err), true, false
		}
		exited := false
		if choice == "finish" {
			_, exited = finishLeftoverLink(f.paths, f.cfg)
		} else {
			removeLeftoverLink(f.paths)
		}
		// The command printed how it went; setup goes on with what it saved.
		if err := refreshSetupConfigSnapshot(f.paths, f.lifecycleMarker); err != nil {
			return f.failErr(err), true, false
		}
		cfg, err := loadConfig(f.paths)
		if err != nil {
			return f.failErr(err), true, false
		}
		f.cfg = cfg
		if exited {
			return f.leftoverLinkExit(), true, false
		}
		if cfg.Cloud == nil || cfg.Cloud.ready() || cloudRecoveryHoldProblem(cfg) != nil {
			return setupStepResult{}, false, false
		}
		// Still unfinished: go on without asking again.
	case hold:
		return setupStepResult{}, false, false // Not now: the hold's steps, exit 1
	}
	renderSetupParagraph(out, setupT("setup.link.goes_on"))
	return setupStepResult{}, false, true
}

// leftoverLinkExit: Ctrl+C at the choice or inside Finish. The commands
// stay on screen, then setup ends as on any Ctrl+C.
func (f *setupFlow) leftoverLinkExit() setupStepResult {
	if f.cfg.Cloud != nil {
		fmt.Fprint(f.ui.termOut(), f.linkPausedOutcome().LinkSteps)
	}
	return f.cancelled()
}

// leftoverLinkText says what the saved Link is: held, a removal that didn't
// finish, or a setup that didn't.
func leftoverLinkText(cfg runtimeConfig) string {
	switch {
	case cloudRecoveryHoldProblem(cfg) != nil:
		return setupT("setup.link.leftover_hold")
	case cfg.Cloud.cleanupPending():
		return setupT("setup.link.removal_unfinished", "command", cloudProfileCommandFor("remove", defaultServerProfileName))
	}
	return setupT("setup.link.leftover")
}

// promptLeftoverLink: "finish", "remove", or "later". Enter picks Finish
// when it's offered, else Not now: Remove never runs on a bare Enter.
func promptLeftoverLink(reader *bufio.Reader, out io.Writer, title string, canFinish bool) (string, error) {
	spec := setupMenuSpec{Title: title, Prompt: "Use ↑/↓ or j/k, Enter to select, Ctrl+C to exit", DefaultValue: "later"}
	if canFinish {
		spec.Options = append(spec.Options, setupMenuOption{Value: "finish", Label: setupT("setup.link.choice_finish")})
		spec.DefaultValue = "finish"
	}
	spec.Options = append(spec.Options,
		setupMenuOption{Value: "remove", Label: setupT("setup.link.choice_remove"), Detail: setupT("setup.link.choice_remove_detail")},
		setupMenuOption{Value: "later", Label: setupT("setup.link.choice_later"), Detail: setupT("setup.link.choice_later_detail")},
	)
	return promptSetupMenu(reader, out, spec, func() (string, error) {
		fmt.Fprintf(out, "\n  %s\n", spec.Title)
		for i, option := range spec.Options {
			fmt.Fprintf(out, "  %d) %s\n", i+1, option.Label)
		}
		byDefault := slices.IndexFunc(spec.Options, func(option setupMenuOption) bool { return option.Value == spec.DefaultValue }) + 1
		answer, err := promptWizardLineFromReader(reader, out, "Choose", strconv.Itoa(byDefault))
		if errors.Is(err, errSetupBack) {
			return "later", nil
		}
		if err != nil {
			return "", err
		}
		if number, convErr := strconv.Atoi(strings.TrimSpace(answer)); convErr == nil && number >= 1 && number <= len(spec.Options) {
			return spec.Options[number-1].Value, nil
		}
		return "later", nil
	})
}
