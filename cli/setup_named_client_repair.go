package main

import "fmt"

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
	if !namedClientRepairRequested(
		cfg,
		target,
		serviceMode,
		host,
		haURL,
		relayURL,
		relayToken,
	) {
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
	profile := activeServerProfile()
	printHumanErr(
		"cannot repair client integration while the local installation identity is invalid; no configuration was changed",
	)
	printHumanInfo(
		"Repair the local installation identity first: %s",
		cloudSetupCommandFor(profile),
	)
	printHumanInfo(
		"Then retry: %s",
		fmt.Sprintf(
			"ha-nova setup --server %s --non-interactive %s",
			profile,
			target,
		),
	)
	return true
}
