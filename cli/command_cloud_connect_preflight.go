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
		if err := validateCloudConnectSavedConfig(paths, cfg); err != nil {
			return err
		}
		return validateCloudConnectIntent(cfg, reconnect)
	}
	if !reconnect &&
		options.server != "" &&
		errors.Is(err, errUnknownServerProfile) {
		return validateCloudConnectSavedDocument(paths)
	}
	if errors.Is(err, os.ErrNotExist) {
		if reconnect {
			return err
		}
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
		return nil
	}
	return err
}

func validateCloudConnectSavedDocument(paths runtimePaths) error {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return err
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return err
	}
	servers, err := documentServersCopy(doc)
	if err != nil {
		return err
	}
	if err := normalizeServerProfilesV3(servers); err != nil {
		return fmt.Errorf(
			"cannot safely continue Home Assistant Cloud setup with the saved server configuration: %w",
			err,
		)
	}
	return nil
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
