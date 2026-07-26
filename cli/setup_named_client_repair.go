package main

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
