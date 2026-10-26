package main

import (
	"fmt"
	"io"
	"strings"
)

func renderCloudCheckpointActions(
	out io.Writer,
	paths runtimePaths,
	cfg runtimeConfig,
	allowResume bool,
) {
	profile, err := cloudRecoveryCommandProfile(paths)
	if err != nil {
		fmt.Fprintln(
			out,
			"  Home Assistant Link setup is incomplete, but no recovery command can be shown until the server profile selection is repaired.",
		)
		renderCloudSelectionRepair(out, err)
		return
	}
	renderCloudCheckpointActionsForProfile(
		out,
		cfg,
		allowResume,
		profile,
	)
}

func renderCloudCheckpointActionsForProfile(
	out io.Writer,
	cfg runtimeConfig,
	allowResume bool,
	profile string,
) {
	if err := validateServerProfileName(profile); err != nil {
		fmt.Fprintln(
			out,
			"  Home Assistant Link setup is incomplete, but no recovery command can be shown until the server profile selection is repaired.",
		)
		renderCloudSelectionRepair(out, err)
		return
	}
	if cfg.Cloud == nil {
		return
	}
	if cloudHoldIsSecurityStop(cfg) {
		renderCloudSecurityHold(out, cfg, profile, true)
		return
	}
	if cfg.Cloud.ready() {
		fmt.Fprintln(
			out,
			"  Home Assistant Link access remains saved for this server profile.",
		)
	} else {
		fmt.Fprintf(
			out,
			"  Home Assistant Link setup remains incomplete at the saved checkpoint %q.\n",
			cfg.Cloud.State,
		)
	}
	if hold := cloudRecoveryHoldProblem(cfg); hold != nil {
		fmt.Fprintf(
			out,
			"  Cloud recovery safety hold: %s\n",
			hold.Detail,
		)
		if cloudRecoveryHoldNeedsUnlockForConfig(cfg) {
			fmt.Fprintf(
				out,
				"  Verify native secure storage: %s\n",
				cloudProfileCommandFor("unlock", profile),
			)
		}
		fmt.Fprintf(
			out,
			"  Verified cleanup: %s\n",
			cloudProfileCommandFor("remove", profile),
		)
		return
	}
	if allowResume && !cfg.Cloud.cleanupPending() {
		fmt.Fprintf(
			out,
			"  Resume: %s\n",
			cloudResumeCommandFor(cfg, profile),
		)
	}
	fmt.Fprintf(
		out,
		"  Verified cleanup: %s\n",
		cloudProfileCommandFor("remove", profile),
	)
}

// renderSetupLinkPausedNotice: setup goes on with Link paused.
func renderSetupLinkPausedNotice(out io.Writer) {
	fmt.Fprintln(out, "  Home Assistant Link isn't finished. Setup goes on without it. You'll see what to do at the end.")
}

// renderSetupCloudPausedOutcome: local access works, Link waits. steps are
// its commands (setupCloudPausedSteps): an earlier screen may be gone.
func renderSetupCloudPausedOutcome(out io.Writer, steps string) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Home Assistant Link setup paused")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  HA NOVA works on this computer. Home Assistant Link isn't finished yet.")
	fmt.Fprint(out, steps)
	fmt.Fprintln(out)
}

// setupCloudPausedSteps: the exact commands to finish or remove Link for
// this server; legacyToken adds when to switch from the old shared access.
func setupCloudPausedSteps(paths runtimePaths, cfg runtimeConfig, legacyToken bool) string {
	var out strings.Builder
	profile, err := cloudRecoveryCommandProfile(paths)
	if err != nil || validateServerProfileName(profile) != nil || cfg.Cloud == nil {
		renderCloudCheckpointActions(&out, paths, cfg, true) // names the repair
	} else {
		if cloudRecoveryHoldProblem(cfg) == nil && !cfg.Cloud.cleanupPending() {
			fmt.Fprintf(&out, "  To finish it, run: %s\n", cloudResumeCommandFor(cfg, profile))
			fmt.Fprintln(&out, "    "+setupT("setup.link.finish_does"))
		}
		fmt.Fprintf(&out, "  To remove it, run: %s\n", cloudProfileCommandFor("remove", profile))
		fmt.Fprintln(&out, "    "+setupT("setup.link.remove_does"))
	}
	if legacyToken {
		fmt.Fprintln(&out, "  Switch to your own access after Link is fixed or removed: run ha-nova setup again.")
	}
	return out.String()
}
