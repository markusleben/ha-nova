package main

import (
	"fmt"
	"strings"
)

func completeNamedProfileClientRepair(
	paths runtimePaths,
	cfg runtimeConfig,
	state installState,
	target string,
	lifecycleMarker ...[]byte,
) int {
	selectedClients, _, err := resolveSetupClientsWithState(
		paths,
		target,
		state,
	)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if !verifyDeviceHealth(cfg) {
		printHumanErr(
			"The selected server profile's existing secure connection could not be verified. No other server credential was used. Run 'ha-nova doctor' for the exact repair command.",
		)
		return 1
	}
	if err := withSetupLifecycleLock(
		paths,
		lifecycleMarker,
		func() error {
			if err := installClientsAndSaveStateUnlocked(
				paths,
				&state,
				selectedClients,
				saveStateForSetup,
				lifecycleMarker...,
			); err != nil {
				return err
			}
			return completeSetupLifecycleUnlocked(
				paths,
				lifecycleMarker...,
			)
		},
	); err != nil {
		printHumanErr(
			"client installation or state save failed: %s",
			err,
		)
		return 1
	}
	printHumanInfo(
		"Repaired client integration using the selected server profile's verified secure connection.",
	)
	return 0
}

func rejectInvalidIdentityNamedClientRepair(
	paths runtimePaths,
	retirementPending bool,
	target string,
	serviceMode bool,
	host string,
	haURL string,
	relayURL string,
	relayToken string,
) bool {
	if activeServerProfile() == defaultServerProfileName {
		return false
	}
	snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		return false
	}
	cfg := snapshot.Config
	clientOnlyRequest := target != "" &&
		!serviceMode &&
		strings.TrimSpace(host) == "" &&
		strings.TrimSpace(haURL) == "" &&
		strings.TrimSpace(relayURL) == "" &&
		strings.TrimSpace(relayToken) == ""
	if !clientOnlyRequest {
		if target != "" &&
			!namedSetupRequestAllowed(
				cfg,
				retirementPending,
				target,
				serviceMode,
				host,
				haURL,
				relayURL,
				relayToken,
			) {
			renderNamedSetupRequestError()
			return true
		}
		return false
	}
	state, err := loadStateOrDefaultChecked(paths)
	if err != nil {
		printHumanErr("%s", err)
		return true
	}
	if _, _, err := resolveSetupClientsWithState(
		paths,
		target,
		state,
	); err != nil {
		printHumanErr("%s", err)
		return true
	}
	recoveryCommand, needsRecovery, recoveryErr :=
		invalidInstallIdentityRecoveryCommand(
			paths,
			snapshot.ProfileName,
		)
	printHumanErr(
		"cannot repair client integration while the local installation identity is invalid; no configuration was changed",
	)
	canRetry := false
	switch {
	case recoveryErr != nil:
		printHumanErr(
			"installation recovery requires manual config review: %s",
			recoveryErr,
		)
	case needsRecovery && recoveryCommand != "":
		printHumanInfo(
			"Complete installation recovery first: %s",
			recoveryCommand,
		)
		canRetry = true
	default:
		printHumanErr(
			"installation recovery cannot select a safe mutating command; preserve config.json for manual review",
		)
	}
	if canRetry {
		printHumanInfo(
			"Then retry: %s",
			fmt.Sprintf(
				"ha-nova setup --server %s --non-interactive %s",
				snapshot.ProfileName,
				target,
			),
		)
	}
	return true
}
