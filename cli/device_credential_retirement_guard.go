package main

import "fmt"

// Profile mutations that can move or replace device credential slots must not
// bypass a durable retirement checkpoint. Setup and purge are the only flows
// allowed to settle that checkpoint.
func requireSettledDeviceCredentialRetirement(
	paths runtimePaths,
	profile string,
) error {
	pending, err :=
		deviceCredentialRetirementCheckpointExistsForProfile(paths, profile)
	if err != nil {
		return fmt.Errorf(
			"inspect pending device retirement for server profile %q: %w",
			profile,
			err,
		)
	}
	if pending {
		return fmt.Errorf(
			"server profile %q has a pending device retirement; run `%s` to finish it first",
			profile,
			deviceRetirementSetupCommand(profile),
		)
	}
	return nil
}
