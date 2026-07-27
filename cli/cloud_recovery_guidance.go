package main

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func renderDurableCloudRecoveryGuidance(
	out io.Writer,
	paths runtimePaths,
	problem *cloudProblem,
) {
	profile, selectionErr := cloudRecoveryCommandProfile(paths)
	if selectionErr != nil {
		fmt.Fprintln(
			out,
			"  No mutating Cloud recovery command is available because the selected server profile cannot be resolved safely.",
		)
		renderCloudSelectionRepair(out, selectionErr)
		return
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		if errors.Is(err, errUnknownServerProfile) {
			_, source := requestedServerSelection()
			if source != "--server" {
				fmt.Fprintln(
					out,
					"  No mutating Cloud recovery command is available because the selected server profile does not exist.",
				)
				renderCloudSelectionRepair(out, err)
				return
			}
		}
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, errUnknownServerProfile) {
			fmt.Fprintf(
				out,
				"  No Cloud checkpoint was saved for server profile %q.\n",
				profile,
			)
			if cloudProblemNeedsStorageUnlock(problem) {
				fmt.Fprintf(
					out,
					"  Unlock secure storage: %s\n",
					cloudProfileCommandFor("unlock", profile),
				)
			}
			if cloudMutationRecoveryAvailable(problem) {
				fmt.Fprintf(
					out,
					"  Start guided Cloud setup with: %s\n",
					cloudProfileCommandFor("add", profile),
				)
			} else if !cloudRemoteFeatureAvailable() {
				fmt.Fprintln(
					out,
					"  Cloud setup is disabled in this build. No Cloud cleanup is needed because no checkpoint exists; local-only setup remains available.",
				)
			}
			return
		}
		fmt.Fprintln(
			out,
			"  Cloud checkpoint storage could not be read. Restore or repair config.json before continuing.",
		)
		fmt.Fprintf(
			out,
			"  Verify after repair: %s\n",
			cloudProfileCommandFor("status", profile),
		)
		return
	}
	renderCloudRecoveryGuidance(out, cfg, problem)
}

func handlePausedCloudOwnerPairing(
	out io.Writer,
	paths runtimePaths,
	err error,
) bool {
	if !errors.Is(err, errSetupBack) && !errors.Is(err, errSetupExit) {
		return false
	}
	profile, selectionErr := cloudRecoveryCommandProfile(paths)
	if selectionErr != nil {
		fmt.Fprintln(
			out,
			"  Home Assistant Cloud setup is paused, but no recovery command can be shown until the server profile selection is repaired.",
		)
		renderCloudSelectionRepair(out, selectionErr)
		return true
	}
	cfg, loadErr := loadSelectedRuntimeConfigUnchecked(paths)
	if loadErr != nil {
		fmt.Fprintln(
			out,
			"  Home Assistant Cloud setup is paused, but its saved state could not be read.",
		)
		fmt.Fprintf(
			out,
			"  Verify after recovery: %s\n",
			cloudProfileCommandFor("status", profile),
		)
		return true
	}
	if cfg.Cloud == nil {
		fmt.Fprintln(
			out,
			"  Home Assistant Cloud setup was cancelled before a checkpoint was saved. No authorization was changed.",
		)
		return true
	}
	if hold := cloudRecoveryHoldProblem(cfg); hold != nil {
		fmt.Fprintf(
			out,
			"  Home Assistant Cloud setup remains paused at %q: %s\n",
			cfg.Cloud.State,
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
			"  Cleanup: %s\n",
			cloudProfileCommandFor("remove", profile),
		)
		return true
	}
	if cfg.Cloud.State == cloudStateCloudVerified && cfg.Cloud.Pending != nil {
		fmt.Fprintln(
			out,
			"  Home Assistant Cloud setup is paused. OAuth authorization is saved; Owner device pairing is still pending.",
		)
	} else {
		fmt.Fprintf(
			out,
			"  Home Assistant Cloud setup is paused at the saved checkpoint %q.\n",
			cfg.Cloud.State,
		)
	}
	fmt.Fprintf(out, "  Resume: %s\n", cloudResumeCommandFor(cfg, profile))
	fmt.Fprintf(
		out,
		"  Verified cleanup: %s\n",
		cloudProfileCommandFor("remove", profile),
	)
	return true
}

func renderCloudSelectionRepair(out io.Writer, cause error) {
	fmt.Fprintf(
		out,
		"  Repair default_server in config.json or select one valid profile explicitly with --server or HA_NOVA_SERVER before continuing: %v\n",
		cause,
	)
}

func renderCloudRecoveryGuidance(
	out io.Writer,
	cfg runtimeConfig,
	problem *cloudProblem,
) {
	if selectedCloudCommandProfile() == "" {
		fmt.Fprintln(
			out,
			"  Cloud recovery is paused, but no mutating command can be shown because the selected server profile name is invalid.",
		)
		fmt.Fprintln(
			out,
			"  Repair default_server in config.json or correct --server or HA_NOVA_SERVER before continuing.",
		)
		return
	}
	if hold := cloudRecoveryHoldProblem(cfg); hold != nil {
		fmt.Fprintf(
			out,
			"  Cloud recovery safety hold saved at %q: %s\n",
			cfg.Cloud.State,
			hold.Detail,
		)
		if cloudRecoveryHoldNeedsUnlockForConfig(cfg) {
			fmt.Fprintf(
				out,
				"  Verify native secure storage: %s\n",
				cloudUnlockCommand(),
			)
		}
		if cfg.Cloud.Current != nil {
			fmt.Fprintln(
				out,
				"  The previously saved Cloud connection was not overwritten.",
			)
		}
		fmt.Fprintf(out, "  Verified cleanup: %s\n", cloudRemoveCommand())
		return
	}
	needsStorageUnlock := cloudProblemNeedsStorageUnlock(problem)
	if cfg.Cloud != nil && cfg.Cloud.ready() {
		fmt.Fprintln(
			out,
			"  The saved Home Assistant Cloud configuration was not changed.",
		)
		if !cloudRemoteFeatureAvailable() {
			fmt.Fprintf(
				out,
				"  Cloud setup is disabled in this build. Verified cleanup: %s\n",
				cloudRemoveCommand(),
			)
			return
		}
		if needsStorageUnlock {
			fmt.Fprintf(out, "  Unlock secure storage: %s\n", cloudUnlockCommand())
		}
		if cloudMutationRecoveryAvailable(problem) &&
			cloudProblemNeedsReconnect(problem) {
			fmt.Fprintf(out, "  Reconnect: %s\n", cloudReconnectCommand())
		}
		fmt.Fprintf(
			out,
			"  Verified cleanup: %s\n",
			cloudRemoveCommand(),
		)
		return
	}
	if cfg.Cloud != nil {
		fmt.Fprintf(
			out,
			"  Cloud setup checkpoint saved at %q.\n",
			cfg.Cloud.State,
		)
		if needsStorageUnlock {
			fmt.Fprintf(out, "  Unlock secure storage: %s\n", cloudUnlockCommand())
		}
		if cloudMutationRecoveryAvailable(problem) {
			fmt.Fprintf(out, "  Resume: %s\n", cloudResumeCommand(cfg))
		} else if !cloudRemoteFeatureAvailable() {
			fmt.Fprintf(
				out,
				"  Cloud setup is disabled in this build. Verified cleanup: %s\n",
				cloudRemoveCommand(),
			)
			return
		}
		fmt.Fprintf(
			out,
			"  Verified cleanup: %s\n",
			cloudRemoveCommand(),
		)
	} else if needsStorageUnlock {
		fmt.Fprintf(out, "  Unlock secure storage: %s\n", cloudUnlockCommand())
	}
	if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		fmt.Fprintln(out, "  Your working local connection was not changed.")
		return
	}
	if cfg.Cloud == nil {
		fmt.Fprintln(
			out,
			"  No Cloud connection was saved. Rerun setup from an interactive desktop.",
		)
	}
}

func cloudProblemNeedsStorageUnlock(problem *cloudProblem) bool {
	return problem != nil &&
		problem.Remediation == cloudRemediationUnlockStorage
}

func renderCloudFailure(
	out io.Writer,
	paths runtimePaths,
	err error,
) *cloudProblem {
	problem := cloudProblemForCommandError(err)
	printHumanErr("%s", problem)
	renderDurableCloudRecoveryGuidance(out, paths, problem)
	return problem
}
