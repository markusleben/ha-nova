package main

import (
	"encoding/json"
	"errors"
)

// repairInvalidClientInstallIdentityForSetup is the only transition allowed
// to replace the otherwise immutable install identity. It requires an explicit
// setup invocation, an exact setup-lifecycle snapshot, and proof that no
// profile retains Cloud lifecycle metadata after verified cleanup.
func repairInvalidClientInstallIdentityForSetup(
	paths runtimePaths,
	loadErr error,
	lifecycleMarker [][]byte,
) (bool, error) {
	if !errors.Is(loadErr, errInvalidClientInstallID) {
		return false, nil
	}
	repaired := false
	err := withClientMutationLock(paths, func() error {
		if err := ensureSetupLifecycleCurrent(
			paths,
			lifecycleMarker...,
		); err != nil {
			return err
		}
		doc, err := loadConfigDocument(paths.ConfigFile)
		if err != nil {
			return err
		}
		if err := validateSupportedConfigSchema(doc); err != nil {
			return err
		}
		if err := validateExistingServerProfileIDs(doc.servers); err != nil {
			return err
		}
		if err := validateClientInstallID(doc.meta.ClientInstallID); err == nil {
			return errors.New(
				"client_install_id changed before guarded repair; rerun setup",
			)
		}
		if _, cloudRemains, err := remainingCloudCleanupProfile(doc); err != nil {
			return err
		} else if cloudRemains {
			return nil
		}
		id, err := newClientInstallID()
		if err != nil {
			return err
		}
		top := make(map[string]json.RawMessage, len(doc.top))
		for key, value := range doc.top {
			top[key] = value
		}
		top["client_install_id"], err = json.Marshal(id)
		if err != nil {
			return err
		}
		if err := writeJSONFileIfUnchanged(
			paths.ConfigFile,
			top,
			0o600,
			doc.source,
		); err != nil {
			return err
		}
		if err := refreshSetupConfigSnapshot(paths, lifecycleMarker); err != nil {
			return err
		}
		repaired = true
		return nil
	})
	return repaired, err
}
