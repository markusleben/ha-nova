package main

import "strings"

func validateCheckpointedProfilePurgeSlot(
	service string,
	expected string,
	pending bool,
) error {
	if expected == "" {
		return nil
	}
	value, err := secretGet(service)
	if err == errSecretNotFound {
		return nil
	}
	if err != nil {
		return err
	}
	actualEvidence := credentialEvidenceID(value)
	if !strings.HasPrefix(expected, "credential-") {
		if pending {
			if record, decodeErr :=
				decodePendingDeviceCredentialRecord(
					value,
				); decodeErr == nil {
				value = record.Credential
			}
		}
		if parsed := parseDeviceCredential(value); parsed != nil {
			actualEvidence = parsed.deviceID
		}
	}
	if actualEvidence != expected {
		return newCloudError(
			CloudErrIdentityMismatch,
			"match checkpointed device credential before deletion",
			nil,
		)
	}
	return nil
}

func readPendingCredentialRecordFromService(
	service string,
) (pendingDeviceCredentialRecord, bool, error) {
	value, err := secretGet(service)
	if err != nil {
		if err == errSecretNotFound {
			return pendingDeviceCredentialRecord{}, false, nil
		}
		return pendingDeviceCredentialRecord{}, false, err
	}
	record, err := decodePendingDeviceCredentialRecord(value)
	if err != nil {
		return pendingDeviceCredentialRecord{}, false, err
	}
	return record, true, nil
}
