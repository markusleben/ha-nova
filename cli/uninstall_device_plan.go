package main

import (
	"fmt"
	"os"
	"strings"
)

// profilePurgeTarget names one server profile's credential slots plus the
// pinned endpoint its revoke must go to — each profile's device entry lives on
// ITS relay, never on a sibling's.
type profilePurgeTarget struct {
	name                 string
	relayInstanceID      string
	secureBaseURL        string
	spkiPin              string
	pendingSecureBaseURL string
	pendingSpkiPin       string
	revokedCurrentID     string
	revokedPendingID     string
	observedCurrentID    string
	observedPendingID    string
	processedCurrentID   string
	processedPendingID   string
	checkpointProcessed  func(
		pending bool,
		evidenceID string,
		outcome serverRemovalCleanupOutcome,
	) error
}

func (target profilePurgeTarget) expectedCurrentID() string {
	if target.observedCurrentID != "" {
		return target.observedCurrentID
	}
	return target.revokedCurrentID
}

func (target profilePurgeTarget) expectedPendingID() string {
	if target.observedPendingID != "" {
		return target.observedPendingID
	}
	return target.revokedPendingID
}

func collectProfilePurgeTargets(
	paths runtimePaths,
) ([]profilePurgeTarget, error) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []profilePurgeTarget{{
				name: activeServerProfile(),
			}}, nil
		}
		return nil, fmt.Errorf(
			"read device cleanup configuration: %w",
			err,
		)
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return nil, err
	}
	if err := validateExistingServerProfileIDs(doc.servers); err != nil {
		return nil, fmt.Errorf(
			"invalid server profile identities: %w",
			err,
		)
	}
	targets := make([]profilePurgeTarget, 0, len(doc.profileNames()))
	for _, name := range doc.profileNames() {
		if err := validateServerProfileName(name); err != nil {
			return nil, err
		}
		cfg, ok := doc.flatProfile(name)
		if !ok {
			return nil, fmt.Errorf(
				"cannot safely inspect device cleanup for server %q",
				name,
			)
		}
		target, err := profilePurgeTargetFromConfig(name, cfg)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		targets = append(targets, profilePurgeTarget{
			name: activeServerProfile(),
		})
	}
	return targets, nil
}

func profilePurgeTargetFromConfig(
	name string,
	cfg runtimeConfig,
) (profilePurgeTarget, error) {
	target := profilePurgeTarget{
		name:                 name,
		relayInstanceID:      strings.TrimSpace(cfg.RelayInstanceID),
		secureBaseURL:        strings.TrimSpace(cfg.RelaySecureBaseURL),
		spkiPin:              strings.TrimSpace(cfg.RelaySpkiPin),
		pendingSecureBaseURL: strings.TrimSpace(cfg.PendingSecureBaseURL),
		pendingSpkiPin:       strings.TrimSpace(cfg.PendingSpkiPin),
	}
	if cfg.Cloud == nil || cfg.Cloud.DeviceRevocationCompleted == nil {
		if cfg.ServerRemoval != nil {
			target.observedCurrentID =
				cfg.ServerRemoval.ObservedCurrentID
			target.observedPendingID =
				cfg.ServerRemoval.ObservedPendingID
			target.processedCurrentID =
				cfg.ServerRemoval.ProcessedCurrentID
			target.processedPendingID =
				cfg.ServerRemoval.ProcessedPendingID
			target.revokedCurrentID =
				cfg.ServerRemoval.ProcessedCurrentID
			target.revokedPendingID =
				cfg.ServerRemoval.ProcessedPendingID
		}
		return target, nil
	}
	if err := validateCloudDeviceRevocationCheckpoint(*cfg.Cloud); err != nil {
		return profilePurgeTarget{}, fmt.Errorf(
			"invalid Cloud device revocation checkpoint for server %q: %w",
			name,
			err,
		)
	}
	target.revokedCurrentID =
		cfg.Cloud.DeviceRevocationCompleted.CurrentDeviceID
	target.revokedPendingID =
		cfg.Cloud.DeviceRevocationCompleted.PendingDeviceID
	if cfg.ServerRemoval != nil {
		target.observedCurrentID =
			cfg.ServerRemoval.ObservedCurrentID
		target.observedPendingID =
			cfg.ServerRemoval.ObservedPendingID
		target.processedCurrentID =
			cfg.ServerRemoval.ProcessedCurrentID
		target.processedPendingID =
			cfg.ServerRemoval.ProcessedPendingID
		if target.revokedCurrentID == "" {
			target.revokedCurrentID =
				cfg.ServerRemoval.ProcessedCurrentID
		}
		if target.revokedPendingID == "" {
			target.revokedPendingID =
				cfg.ServerRemoval.ProcessedPendingID
		}
	}
	return target, nil
}

// validateProfilePurgeTargets runs the deterministic credential and endpoint
// checks used by full purge without revoking or deleting anything.
func validateProfilePurgeTargets(targets []profilePurgeTarget) error {
	for _, target := range targets {
		slotState, err := inspectProfilePurgeSlotState(target)
		if err != nil {
			return err
		}
		if err := validateRequiredProfilePurgeSlots(
			target,
			slotState,
		); err != nil {
			return err
		}
		pending, pendingExists, err :=
			readPendingCredentialRecordFromService(
				deviceCredentialPendingServiceForProfile(target.name),
			)
		if err != nil {
			return relayAuthTokenSetupOperationError(
				fmt.Sprintf(
					"inspect pending device credential for server %q",
					target.name,
				),
				err,
			)
		}
		if pendingExists &&
			pending.Source == pendingDeviceCredentialSourceLocal &&
			((target.pendingSecureBaseURL == "") !=
				(target.pendingSpkiPin == "")) {
			return fmt.Errorf(
				"pending device endpoint for server %q is incomplete; refusing to delete a possibly active credential",
				target.name,
			)
		}
		_, currentExists, err := readCredentialSlot(
			deviceCredentialServiceForProfile(target.name),
		)
		if err != nil {
			return relayAuthTokenSetupOperationError(
				fmt.Sprintf(
					"inspect device credential for server %q",
					target.name,
				),
				err,
			)
		}
		if currentExists &&
			((target.secureBaseURL == "") != (target.spkiPin == "")) {
			return fmt.Errorf(
				"device endpoint for server %q is incomplete; refusing to delete an active credential",
				target.name,
			)
		}
	}
	return nil
}

type profilePurgeSlotState struct {
	currentExists       bool
	pendingExists       bool
	pendingLocal        bool
	pendingEndpointLive bool
}

func inspectProfilePurgeSlotState(
	target profilePurgeTarget,
) (profilePurgeSlotState, error) {
	state := profilePurgeSlotState{}
	pendingService :=
		deviceCredentialPendingServiceForProfile(target.name)
	pendingValue, err := secretGet(pendingService)
	switch {
	case err == nil:
		state.pendingExists = true
		if record, decodeErr :=
			decodePendingDeviceCredentialRecord(pendingValue); decodeErr == nil {
			state.pendingLocal =
				record.Source == pendingDeviceCredentialSourceLocal
		}
	case err == errSecretNotFound:
	default:
		return profilePurgeSlotState{}, relayAuthTokenSetupOperationError(
			fmt.Sprintf(
				"inspect pending device credential for server %q",
				target.name,
			),
			err,
		)
	}

	currentService := deviceCredentialServiceForProfile(target.name)
	_, err = secretGet(currentService)
	switch {
	case err == nil:
		state.currentExists = true
	case err == errSecretNotFound:
	default:
		return profilePurgeSlotState{}, relayAuthTokenSetupOperationError(
			fmt.Sprintf(
				"inspect device credential for server %q",
				target.name,
			),
			err,
		)
	}
	state.pendingEndpointLive =
		target.pendingSecureBaseURL != "" &&
			target.pendingSpkiPin != ""
	return state, nil
}

func validateRequiredProfilePurgeSlots(
	target profilePurgeTarget,
	state profilePurgeSlotState,
) error {
	if target.observedPendingID != "" &&
		target.processedPendingID == "" &&
		!state.pendingExists {
		return fmt.Errorf(
			"checkpointed pending credential for server %q is missing before its cleanup outcome was saved; refusing profile removal for manual review",
			target.name,
		)
	}
	if target.observedCurrentID != "" &&
		target.processedCurrentID == "" &&
		!state.currentExists {
		return fmt.Errorf(
			"checkpointed current credential for server %q is missing before its cleanup outcome was saved; refusing profile removal for manual review",
			target.name,
		)
	}
	pendingEndpointRecorded :=
		target.pendingSecureBaseURL != "" ||
			target.pendingSpkiPin != ""
	if pendingEndpointRecorded &&
		!state.pendingExists &&
		target.revokedPendingID == "" {
		return fmt.Errorf(
			"pending device endpoint for server %q indicates a possibly activated pairing, but its pending credential is missing; refusing cleanup because that relay pairing cannot be authenticated for revocation",
			target.name,
		)
	}

	currentEndpointRecorded :=
		target.secureBaseURL != "" ||
			target.spkiPin != ""
	interruptedFirstPromotion :=
		state.pendingExists &&
			state.pendingLocal &&
			state.pendingEndpointLive &&
			target.secureBaseURL == target.pendingSecureBaseURL &&
			target.spkiPin == target.pendingSpkiPin
	if currentEndpointRecorded &&
		!state.currentExists &&
		target.revokedCurrentID == "" &&
		!interruptedFirstPromotion {
		return fmt.Errorf(
			"device endpoint for server %q indicates an active pairing, but its current credential is missing; refusing cleanup because that relay pairing cannot be authenticated for revocation",
			target.name,
		)
	}
	return nil
}
