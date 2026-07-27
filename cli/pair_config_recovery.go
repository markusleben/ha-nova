package main

import (
	"errors"
	"fmt"
	"os"
)

// recoverPairConfigForExplicitRelayURL permits only the two intentional fresh
// pairing cases: no config exists yet, or an explicit --server names a new
// profile in an otherwise valid document. A present selected profile may be
// incomplete, but it still has to pass every schema and semantic validation so
// Cloud and pending-pairing guards always see its complete durable state.
func recoverPairConfigForExplicitRelayURL(
	paths runtimePaths,
	bootstrapURL string,
	loadErr error,
	newProfile bool,
	hadConfigSnapshot bool,
) (runtimeConfig, error) {
	if loadErr == nil {
		return runtimeConfig{}, errors.New("pair config recovery requires a load error")
	}
	if newProfile || !hadConfigSnapshot {
		cfg := runtimeConfig{RelayBaseURL: bootstrapURL}
		if raw, err := loadRawDefaultProfileConfig(paths.ConfigFile); err == nil {
			cfg.ClientInstallID = raw.ClientInstallID
		} else if hadConfigSnapshot && !errors.Is(err, os.ErrNotExist) {
			return runtimeConfig{}, loadErr
		}
		return cfg, nil
	}

	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		return runtimeConfig{}, loadErr
	}
	if err := validateLoadedRuntimeConfig(&cfg); err != nil {
		return runtimeConfig{}, fmt.Errorf(
			"invalid selected server profile: %w",
			err,
		)
	}
	return cfg, nil
}

// validateRuntimeConfigSave performs the same document merge as a durable
// config checkpoint without writing it. This catches an invalid sibling,
// immutable install identity conflict, or other document-wide save failure
// before callers begin external or credential-storage mutation.
func validateRuntimeConfigSave(
	paths runtimePaths,
	cfg runtimeConfig,
) error {
	doc, err := loadConfigDocumentOrEmpty(paths.ConfigFile)
	if err != nil {
		return err
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return err
	}
	name, err := saveTargetProfileName(doc)
	if err != nil {
		return err
	}
	_, err = doc.withProfile(name, cfg)
	return err
}
