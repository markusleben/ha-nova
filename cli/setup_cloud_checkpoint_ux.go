package main

import (
	"fmt"
	"io"
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
			"  Home Assistant Cloud setup is incomplete, but no recovery command can be shown until the server profile selection is repaired.",
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
			"  Home Assistant Cloud setup is incomplete, but no recovery command can be shown until the server profile selection is repaired.",
		)
		renderCloudSelectionRepair(out, err)
		return
	}
	if cfg.Cloud == nil {
		return
	}
	if cfg.Cloud.ready() {
		fmt.Fprintln(
			out,
			"  Home Assistant Cloud access remains saved for this server profile.",
		)
	} else {
		fmt.Fprintf(
			out,
			"  Home Assistant Cloud setup remains incomplete at the saved checkpoint %q.\n",
			cfg.Cloud.State,
		)
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

func renderSetupCloudPausedOutcome(out io.Writer) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "  Home Assistant Cloud setup paused")
	fmt.Fprintln(out)
	fmt.Fprintln(
		out,
		"  Local access and AI client skills are ready. The saved Cloud checkpoint was not changed.",
	)
	fmt.Fprintln(
		out,
		"  Use the exact resume or verified-cleanup command shown above when ready.",
	)
	fmt.Fprintln(out)
}
