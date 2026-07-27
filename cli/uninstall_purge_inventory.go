package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type purgeProfileIdentity struct {
	name      string
	profileID string
}

type fullPurgeInventory struct {
	initialDevice []purgeProfileIdentity
	initialCloud  []purgeProfileIdentity
	config        []byte
	configExists  bool
}

func newFullPurgeInventory(
	deviceTargets []profilePurgeTarget,
	cloudTargets []cloudPurgeTarget,
) fullPurgeInventory {
	return fullPurgeInventory{
		initialDevice: devicePurgeProfileInventory(deviceTargets),
		initialCloud:  cloudPurgeProfileInventory(cloudTargets),
	}
}

func (inventory *fullPurgeInventory) captureFinalConfig(
	paths runtimePaths,
) error {
	config, exists, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		return fmt.Errorf(
			"failed to snapshot final purge configuration: %w",
			err,
		)
	}
	inventory.config = config
	inventory.configExists = exists
	return nil
}

func (inventory fullPurgeInventory) verifyFinalConfigAndTargets(
	paths runtimePaths,
) ([]profilePurgeTarget, error) {
	if err := ensureOptionalFileSnapshotCurrent(
		paths.ConfigFile,
		inventory.config,
		inventory.configExists,
	); err != nil {
		return nil, fmt.Errorf(
			"configuration changed before config cleanup: %w",
			err,
		)
	}
	deviceTargets, err := collectProfilePurgeTargets(paths)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to re-inventory device credentials before config cleanup: %w",
			err,
		)
	}
	if err := requireSamePurgeProfileInventory(
		inventory.initialDevice,
		devicePurgeProfileInventory(deviceTargets),
		"device credential",
	); err != nil {
		return nil, fmt.Errorf(
			"configuration inventory changed before config cleanup: %w",
			err,
		)
	}
	cloudTargets, err := collectCloudPurgeTargets(paths.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to re-inventory Cloud credentials before config cleanup: %w",
			err,
		)
	}
	if err := requireSamePurgeProfileInventory(
		inventory.initialCloud,
		cloudPurgeProfileInventory(cloudTargets),
		"Cloud credential",
	); err != nil {
		return nil, fmt.Errorf(
			"configuration inventory changed before config cleanup: %w",
			err,
		)
	}
	if err := requirePurgedCloudCredentialsAbsent(
		cloudTargets,
	); err != nil {
		return nil, err
	}
	return deviceTargets, nil
}

func requirePurgedCloudCredentialsAbsent(
	targets []cloudPurgeTarget,
) error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancel()
	for _, target := range targets {
		store, err := newCloudSecretStoreForCLI(target.profileID)
		if err != nil {
			return fmt.Errorf(
				"open final Cloud credential proof for server %q: %w",
				target.profileName,
				err,
			)
		}
		for _, load := range []func(
			context.Context,
			SecretStoreUIPolicy,
		) (OAuthSecretEnvelope, bool, error){
			store.LoadCurrent,
			store.LoadPending,
			store.LoadRetiring,
		} {
			_, exists, err := load(ctx, SecretStoreForbidUI)
			if err != nil {
				return fmt.Errorf(
					"verify final Cloud credential absence for server %q: %w",
					target.profileName,
					err,
				)
			}
			if exists {
				return fmt.Errorf(
					"Cloud credentials reappeared before config cleanup for server %q",
					target.profileName,
				)
			}
		}
	}
	return nil
}

func devicePurgeProfileInventory(
	targets []profilePurgeTarget,
) []purgeProfileIdentity {
	inventory := make([]purgeProfileIdentity, 0, len(targets))
	for _, target := range targets {
		inventory = append(inventory, purgeProfileIdentity{
			name:      target.name,
			profileID: target.profileID,
		})
	}
	return inventory
}

func cloudPurgeProfileInventory(
	targets []cloudPurgeTarget,
) []purgeProfileIdentity {
	inventory := make([]purgeProfileIdentity, 0, len(targets))
	for _, target := range targets {
		inventory = append(inventory, purgeProfileIdentity{
			name:      target.profileName,
			profileID: target.profileID,
		})
	}
	return inventory
}

func requireSamePurgeProfileInventory(
	expected []purgeProfileIdentity,
	actual []purgeProfileIdentity,
	kind string,
) error {
	if len(actual) != len(expected) {
		return fmt.Errorf(
			"%s profile inventory changed from %d to %d entries",
			kind,
			len(expected),
			len(actual),
		)
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return fmt.Errorf(
				"%s profile inventory changed at entry %d",
				kind,
				index,
			)
		}
	}
	return nil
}

func resetVerifiedCloudStorageAfterPurgeRelock(
	paths runtimePaths,
	cause error,
) error {
	problem := cloudProblemForError(cause)
	if problem.Code != cloudProblemSecureStorage ||
		problem.Remediation != cloudRemediationUnlockStorage {
		return nil
	}
	initial, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	for _, profileName := range initial.profileNames() {
		doc, err := loadConfigDocument(paths.ConfigFile)
		if err != nil {
			return err
		}
		cfg, ok := doc.flatProfile(profileName)
		if !ok ||
			cfg.Cloud == nil ||
			cfg.Cloud.RecoveryHold == nil ||
			!cfg.Cloud.RecoveryHold.StorageVerified ||
			!cloudRecoveryHoldClearsAfterUnlock(
				cfg.Cloud.RecoveryHold,
			) {
			continue
		}
		profileRaw, err := cloudRecoveryProfileRaw(
			doc,
			profileName,
		)
		if err != nil {
			return err
		}
		unverified := *cfg.Cloud.RecoveryHold
		unverified.StorageVerified = false
		if err := writeCloudRecoveryHoldRaw(
			paths,
			doc,
			profileName,
			profileRaw,
			&unverified,
		); err != nil {
			return err
		}
	}
	return nil
}

func resetPurgeStorageProofAfterError(
	paths runtimePaths,
	cause error,
) error {
	if cause == nil {
		return nil
	}
	resetErr := resetVerifiedCloudStorageAfterPurgeRelock(
		paths,
		cause,
	)
	if resetErr == nil {
		return cause
	}
	return errors.Join(
		cause,
		fmt.Errorf(
			"reset Cloud secure-storage recovery proof: %w",
			resetErr,
		),
	)
}
