package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// A leftover Home Assistant Link at the start of a plain interactive setup:
// say what it is, then let a person at a terminal finish it, remove it, or
// go on without it (docs/work/2026-10-26-link-leftover-setup-doctor-spec.md).

// Test seams: the menu needs a terminal; the choices run the existing
// commands in this process.
var (
	leftoverLinkChoiceAvailable = func(out io.Writer) bool {
		return uiInputSupportsTTY() && writerSupportsTTYForSetup(out)
	}
	finishLeftoverLink = func(paths runtimePaths, cfg runtimeConfig) int {
		return runCloudConnectCommand(paths, nil, cloudResumeAction(cfg) == "reconnect")
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
		remoteOnlyCloudSetup(f.cfg) || !cloudRemoteFeatureAvailable() ||
		!(hold || (!cloud.ready() && !hybridCloudSetupPending(f.cfg))) {
		return setupStepResult{}, false, false
	}
	out := f.ui.termOut()
	if !leftoverLinkChoiceAvailable(out) {
		if hold {
			return setupStepResult{}, false, false // the hold's steps, exit 1
		}
		renderSetupParagraph(out, setupT("setup.link.leftover"), setupT("setup.link.goes_on"))
		return setupStepResult{}, false, true
	}
	choice, err := promptLeftoverLink(f.ui.termIn(), out, hold, !cloud.cleanupPending() && cloudSetupPromptEligible(out))
	switch {
	case errors.Is(err, errSetupExit):
		fmt.Fprint(out, f.linkPausedOutcome().LinkSteps)
		return f.cancelled(), true, false
	case err != nil:
		return f.failErr(err), true, false
	case choice == "finish" || choice == "remove":
		if err := ensureSetupLifecycleCurrent(f.paths, f.lifecycleMarker...); err != nil {
			return f.failErr(err), true, false
		}
		if choice == "finish" {
			finishLeftoverLink(f.paths, f.cfg)
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
		return setupStepResult{}, false, false
	case hold:
		return setupStepResult{}, false, false // Not now: the hold's steps, exit 1
	}
	renderSetupParagraph(out, setupT("setup.link.goes_on"))
	return setupStepResult{}, false, true
}

// promptLeftoverLink: "finish", "remove", or "later".
func promptLeftoverLink(reader *bufio.Reader, out io.Writer, hold, canFinish bool) (string, error) {
	spec := setupMenuSpec{Title: setupT("setup.link.leftover"), Prompt: "Use ↑/↓ or j/k, Enter to select, Ctrl+C to exit"}
	if hold {
		spec.Title = setupT("setup.link.leftover_hold")
	} else if canFinish {
		spec.Options = append(spec.Options, setupMenuOption{Value: "finish", Label: setupT("setup.link.choice_finish")})
	}
	spec.Options = append(spec.Options,
		setupMenuOption{Value: "remove", Label: setupT("setup.link.choice_remove"), Detail: setupT("setup.link.choice_remove_detail")},
		setupMenuOption{Value: "later", Label: setupT("setup.link.choice_later"), Detail: setupT("setup.link.choice_later_detail")},
	)
	spec.DefaultValue = spec.Options[0].Value
	return promptSetupMenu(reader, out, spec, func() (string, error) {
		fmt.Fprintf(out, "\n  %s\n", spec.Title)
		for i, option := range spec.Options {
			fmt.Fprintf(out, "  %d) %s\n", i+1, option.Label)
		}
		answer, err := promptWizardLineFromReader(reader, out, "Choose", "1")
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
