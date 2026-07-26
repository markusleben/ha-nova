package main

import (
	"fmt"
	"strings"
)

// Hook for tests (the revoke would otherwise dial a real endpoint).
var revokeSelfDeviceV1ForRetire = revokeSelfDeviceV1

func prepareDeviceCredentialRetirement(
	cfg *runtimeConfig,
) (runtimeConfig, error) {
	previous := *cfg
	if cfg.Cloud != nil {
		return previous, fmt.Errorf(
			"Home Assistant Cloud access is configured; remove it before switching away from the paired device transport",
		)
	}
	cfg.RelaySecureBaseURL = ""
	cfg.RelaySpkiPin = ""
	cfg.PendingSecureBaseURL = ""
	cfg.PendingSpkiPin = ""
	cfg.RelayInstanceID = ""
	return previous, nil
}

func revokeDeviceCredentialsForRetirement(
	previous runtimeConfig,
) (bool, error) {
	revocationStarted := false
	if _, err := resumeKeyringDeviceCredentialCleanup(); err != nil {
		return false, fmt.Errorf(
			"finish device credential migration cleanup before retirement: %w",
			err,
		)
	}
	if cred, ok, err := readDeviceCredential(); err != nil {
		return false, fmt.Errorf(
			"read retiring device credential: %w",
			err,
		)
	} else if ok {
		if previous.RelaySecureBaseURL == "" || previous.RelaySpkiPin == "" {
			return false, fmt.Errorf(
				"cannot securely retire the active device credential without its complete pinned endpoint",
			)
		}
		revocationStarted = true
		if err := revokeSelfDeviceV1ForRetire(
			previous.RelaySecureBaseURL,
			previous.RelaySpkiPin,
			cred,
		); err != nil {
			return true, fmt.Errorf(
				"revoke retiring device credential: %w",
				err,
			)
		}
	}
	pending, pendingExists, err := readPendingDeviceCredentialRecord()
	if err != nil {
		return revocationStarted, fmt.Errorf(
			"read retiring pending device credential: %w",
			err,
		)
	}
	if pendingExists {
		if pending.Source != pendingDeviceCredentialSourceLocal {
			return revocationStarted, fmt.Errorf(
				"cannot retire a pending Cloud device credential through the local pairing endpoint",
			)
		}
		pendingBaseURL := strings.TrimSpace(previous.PendingSecureBaseURL)
		pendingPin := strings.TrimSpace(previous.PendingSpkiPin)
		switch {
		case pendingBaseURL != "" && pendingPin != "":
			revocationStarted = true
			if err := revokeSelfDeviceV1ForRetire(
				pendingBaseURL,
				pendingPin,
				pending.Credential,
			); err != nil {
				return true, fmt.Errorf(
					"revoke retiring pending device credential: %w",
					err,
				)
			}
		case pendingBaseURL != "" || pendingPin != "":
			return revocationStarted, fmt.Errorf(
				"cannot securely retire the pending device credential with an incomplete pinned endpoint",
			)
		}
	}
	return revocationStarted, nil
}

func deleteDeviceCredentialsForRetirement() error {
	if err := deleteDeviceCredential(); err != nil {
		return fmt.Errorf("remove retired device credential: %w", err)
	}
	if err := deletePendingDeviceCredential(); err != nil {
		return fmt.Errorf(
			"remove retired pending device credential: %w",
			err,
		)
	}
	if err := removeDeviceFileStorageResidueForProfile(
		activeServerProfile(),
	); err != nil {
		return fmt.Errorf(
			"remove retired device credential residue: %w",
			err,
		)
	}
	return nil
}
