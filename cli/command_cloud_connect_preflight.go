package main

import (
	"errors"
	"fmt"
	"os"
)

// preflightSavedCloudConnectConfig inspects only durable existing state. A
// missing profile selected explicitly for `cloud add` remains creation intent;
// this preflight must not synthesize or persist it merely to decide whether an
// interactive desktop is needed.
func preflightSavedCloudConnectConfig(
	paths runtimePaths,
	options cloudCommandFlags,
	reconnect bool,
) error {
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err == nil {
		return validateCloudConnectSavedConfig(paths, cfg)
	}
	if !reconnect &&
		options.server != "" &&
		errors.Is(err, errUnknownServerProfile) {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if !reconnect {
			selected, source := requestedServerSelection()
			if source == serverSelectionEnvVar &&
				selected != "" && selected != defaultServerProfileName {
				return fmt.Errorf(
					"creating server profile %q requires the explicit flag --server %s; %s alone is not accepted for a fresh Cloud setup",
					selected,
					selected,
					serverSelectionEnvVar,
				)
			}
		}
		return nil
	}
	return err
}

func validateCloudConnectSavedConfig(
	paths runtimePaths,
	cfg runtimeConfig,
) error {
	if err := validateRuntimeConfigSave(paths, cfg); err != nil {
		return fmt.Errorf(
			"cannot safely continue Home Assistant Cloud setup with the saved server configuration: %w",
			err,
		)
	}
	return nil
}
