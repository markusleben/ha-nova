package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	deviceCredentialRetirementSchema   = 2
	deviceCredentialRetirementMaxBytes = 4096
	deviceCredentialRetirementPrepared = "prepared"
	deviceCredentialRetirementRevoked  = "revoked"
)

type deviceCredentialRetirementCheckpoint struct {
	SchemaVersion        int    `json:"schema_version"`
	Phase                string `json:"phase"`
	Profile              string `json:"profile"`
	ProfileID            string `json:"profile_id,omitempty"`
	RelayInstanceID      string `json:"relay_instance_id,omitempty"`
	RelaySecureBaseURL   string `json:"relay_secure_base_url,omitempty"`
	RelaySpkiPin         string `json:"relay_spki_pin,omitempty"`
	PendingSecureBaseURL string `json:"pending_secure_base_url,omitempty"`
	PendingSpkiPin       string `json:"pending_spki_pin,omitempty"`
	CurrentCredentialSHA string `json:"current_credential_sha256,omitempty"`
	PendingCredentialSHA string `json:"pending_credential_sha256,omitempty"`
	PendingSource        string `json:"pending_source,omitempty"`
	PendingRelayID       string `json:"pending_relay_instance_id,omitempty"`
}

func deviceCredentialRetirementCheckpointPath(
	paths runtimePaths,
) (string, error) {
	return deviceCredentialRetirementCheckpointPathForProfile(
		paths,
		activeServerProfile(),
	)
}

func deviceCredentialRetirementCheckpointPathForProfile(
	paths runtimePaths,
	profile string,
) (string, error) {
	if paths.ConfigDir == "" {
		return "", fmt.Errorf(
			"device retirement requires a configuration directory",
		)
	}
	if err := validateServerProfileName(profile); err != nil {
		return "", fmt.Errorf("device retirement profile: %w", err)
	}
	return filepath.Join(
		paths.ConfigDir,
		".device-retirement."+profile+".json",
	), nil
}

func writeDeviceCredentialRetirementCheckpoint(
	paths runtimePaths,
	previous runtimeConfig,
) error {
	path, err := deviceCredentialRetirementCheckpointPath(paths)
	if err != nil {
		return err
	}
	current, currentExists, err := readDeviceCredential()
	if err != nil {
		return fmt.Errorf(
			"read device credential for retirement checkpoint: %w",
			err,
		)
	}
	pending, pendingExists, err := readPendingDeviceCredentialRecord()
	if err != nil {
		return fmt.Errorf(
			"read pending device credential for retirement checkpoint: %w",
			err,
		)
	}
	checkpoint := deviceCredentialRetirementCheckpoint{
		SchemaVersion:        deviceCredentialRetirementSchema,
		Phase:                deviceCredentialRetirementPrepared,
		Profile:              activeServerProfile(),
		ProfileID:            previous.ProfileID,
		RelayInstanceID:      previous.RelayInstanceID,
		RelaySecureBaseURL:   previous.RelaySecureBaseURL,
		RelaySpkiPin:         previous.RelaySpkiPin,
		PendingSecureBaseURL: previous.PendingSecureBaseURL,
		PendingSpkiPin:       previous.PendingSpkiPin,
	}
	if currentExists {
		checkpoint.CurrentCredentialSHA =
			deviceCredentialRetirementFingerprint(current)
	}
	if pendingExists {
		checkpoint.PendingCredentialSHA =
			deviceCredentialRetirementFingerprint(pending.Credential)
		checkpoint.PendingSource = pending.Source
		checkpoint.PendingRelayID = pending.RelayInstanceID
	}
	if err := writeJSONFile(path, checkpoint, 0o600); err != nil {
		return fmt.Errorf(
			"checkpoint device credential retirement: %w",
			err,
		)
	}
	return nil
}

func readDeviceCredentialRetirementCheckpoint(
	paths runtimePaths,
) (deviceCredentialRetirementCheckpoint, bool, error) {
	return readDeviceCredentialRetirementCheckpointForProfile(
		paths,
		activeServerProfile(),
	)
}

func readDeviceCredentialRetirementCheckpointForProfile(
	paths runtimePaths,
	profile string,
) (deviceCredentialRetirementCheckpoint, bool, error) {
	path, err := deviceCredentialRetirementCheckpointPathForProfile(
		paths,
		profile,
	)
	if err != nil {
		return deviceCredentialRetirementCheckpoint{}, false, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return deviceCredentialRetirementCheckpoint{}, false, nil
		}
		return deviceCredentialRetirementCheckpoint{}, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(
		file,
		deviceCredentialRetirementMaxBytes+1,
	))
	if err != nil {
		return deviceCredentialRetirementCheckpoint{}, false, err
	}
	if len(data) > deviceCredentialRetirementMaxBytes {
		return deviceCredentialRetirementCheckpoint{}, false, fmt.Errorf(
			"device retirement checkpoint exceeds size limit",
		)
	}
	var checkpoint deviceCredentialRetirementCheckpoint
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&checkpoint); err != nil {
		return deviceCredentialRetirementCheckpoint{}, false, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return deviceCredentialRetirementCheckpoint{}, false, err
	}
	if checkpoint.SchemaVersion != deviceCredentialRetirementSchema ||
		checkpoint.Profile != profile ||
		(checkpoint.Phase != deviceCredentialRetirementPrepared &&
			checkpoint.Phase != deviceCredentialRetirementRevoked) {
		return deviceCredentialRetirementCheckpoint{}, false, fmt.Errorf(
			"invalid device retirement checkpoint",
		)
	}
	return checkpoint, true, nil
}

func clearDeviceCredentialRetirementCheckpoint(paths runtimePaths) error {
	path, err := deviceCredentialRetirementCheckpointPath(paths)
	if err != nil {
		return err
	}
	if err := removeDeviceResiduePath(path); err != nil {
		return fmt.Errorf("clear device retirement checkpoint: %w", err)
	}
	return nil
}

func markDeviceCredentialRetirementRevoked(
	paths runtimePaths,
	checkpoint deviceCredentialRetirementCheckpoint,
) (deviceCredentialRetirementCheckpoint, error) {
	checkpoint.Phase = deviceCredentialRetirementRevoked
	path, err := deviceCredentialRetirementCheckpointPath(paths)
	if err != nil {
		return checkpoint, err
	}
	if err := writeJSONFile(path, checkpoint, 0o600); err != nil {
		return checkpoint, fmt.Errorf(
			"persist completed device revocation: %w",
			err,
		)
	}
	return checkpoint, nil
}

func resumeDeviceCredentialRetirementCheckpoint(
	paths runtimePaths,
	current runtimeConfig,
) (bool, error) {
	checkpoint, exists, err :=
		readDeviceCredentialRetirementCheckpoint(paths)
	if err != nil || !exists {
		return false, err
	}
	previous := runtimeConfig{
		ProfileID:            checkpoint.ProfileID,
		RelayInstanceID:      checkpoint.RelayInstanceID,
		RelaySecureBaseURL:   checkpoint.RelaySecureBaseURL,
		RelaySpkiPin:         checkpoint.RelaySpkiPin,
		PendingSecureBaseURL: checkpoint.PendingSecureBaseURL,
		PendingSpkiPin:       checkpoint.PendingSpkiPin,
	}
	if current.ProfileID != checkpoint.ProfileID {
		return false, fmt.Errorf(
			"device retirement checkpoint profile identity changed; refusing automatic revocation",
		)
	}
	switch {
	case deviceRetirementEndpointsEqual(current, previous) &&
		checkpoint.Phase == deviceCredentialRetirementPrepared:
		// The config rollback succeeded. The retirement was abandoned; remove
		// only its stale checkpoint and leave the working credential untouched.
		return false, clearDeviceCredentialRetirementCheckpoint(paths)
	case deviceRetirementEndpointsEqual(current, previous):
		return false, fmt.Errorf(
			"device revocation already completed, but the retired endpoint was restored; refusing to discard the recovery checkpoint",
		)
	case deviceRetirementEndpointsCleared(current):
		if current.Cloud != nil {
			return false, fmt.Errorf(
				"Home Assistant Cloud state appeared after device retirement was interrupted; refusing automatic revocation",
			)
		}
		if strings.TrimSpace(current.RelayInstanceID) != "" {
			return false, fmt.Errorf(
				"Relay identity changed after device retirement was interrupted; refusing automatic revocation",
			)
		}
		if err := validateDeviceCredentialRetirementBindings(
			checkpoint,
		); err != nil {
			return false, err
		}
		if _, err := completeCheckpointedDeviceCredentialRetirement(
			paths,
			previous,
			checkpoint,
		); err != nil {
			return false, fmt.Errorf(
				"resume device credential retirement: %w",
				err,
			)
		}
		return true, nil
	default:
		return false, fmt.Errorf(
			"device retirement checkpoint does not match the current server profile; refusing automatic revocation",
		)
	}
}

func validateDeviceCredentialRetirementBindings(
	checkpoint deviceCredentialRetirementCheckpoint,
) error {
	current, currentExists, err := readDeviceCredential()
	if err != nil {
		return fmt.Errorf(
			"inspect current credential before retirement retry: %w",
			err,
		)
	}
	currentExpected := checkpoint.CurrentCredentialSHA != ""
	if checkpoint.Phase == deviceCredentialRetirementPrepared &&
		currentExists != currentExpected {
		return fmt.Errorf(
			"current device credential presence changed after retirement was interrupted; refusing automatic revocation",
		)
	}
	if checkpoint.Phase == deviceCredentialRetirementRevoked &&
		currentExists && !currentExpected {
		return fmt.Errorf(
			"current device credential appeared after retirement completed; refusing automatic deletion",
		)
	}
	if currentExists &&
		deviceCredentialRetirementFingerprint(current) !=
			checkpoint.CurrentCredentialSHA {
		return fmt.Errorf(
			"current device credential changed after retirement was interrupted; refusing automatic revocation",
		)
	}
	pending, pendingExists, err := readPendingDeviceCredentialRecord()
	if err != nil {
		return fmt.Errorf(
			"inspect pending credential before retirement retry: %w",
			err,
		)
	}
	pendingExpected := checkpoint.PendingCredentialSHA != ""
	if checkpoint.Phase == deviceCredentialRetirementPrepared &&
		pendingExists != pendingExpected {
		return fmt.Errorf(
			"pending device credential presence changed after retirement was interrupted; refusing automatic revocation",
		)
	}
	if checkpoint.Phase == deviceCredentialRetirementRevoked &&
		pendingExists && !pendingExpected {
		return fmt.Errorf(
			"pending device credential appeared after retirement completed; refusing automatic deletion",
		)
	}
	if pendingExists &&
		(deviceCredentialRetirementFingerprint(pending.Credential) !=
			checkpoint.PendingCredentialSHA ||
			pending.Source != checkpoint.PendingSource ||
			pending.RelayInstanceID != checkpoint.PendingRelayID) {
		return fmt.Errorf(
			"pending device credential changed after retirement was interrupted; refusing automatic revocation",
		)
	}
	return nil
}

func completeCheckpointedDeviceCredentialRetirement(
	paths runtimePaths,
	previous runtimeConfig,
	checkpoint deviceCredentialRetirementCheckpoint,
) (bool, error) {
	if checkpoint.Phase == deviceCredentialRetirementPrepared {
		started, err := revokeDeviceCredentialsForRetirement(previous)
		if err != nil {
			return started, err
		}
		checkpoint, err = markDeviceCredentialRetirementRevoked(
			paths,
			checkpoint,
		)
		if err != nil {
			return true, err
		}
	}
	if err := validateDeviceCredentialRetirementBindings(checkpoint); err != nil {
		return true, err
	}
	if err := deleteDeviceCredentialsForRetirement(); err != nil {
		return true, err
	}
	return true, clearDeviceCredentialRetirementCheckpoint(paths)
}

func deviceCredentialRetirementCheckpointExistsForProfile(
	paths runtimePaths,
	profile string,
) (bool, error) {
	path, err := deviceCredentialRetirementCheckpointPathForProfile(
		paths,
		profile,
	)
	if err != nil {
		return false, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf(
			"device retirement checkpoint is not a regular file",
		)
	}
	return true, nil
}

func deviceRetirementEndpointsEqual(a, b runtimeConfig) bool {
	return a.RelaySecureBaseURL == b.RelaySecureBaseURL &&
		a.RelaySpkiPin == b.RelaySpkiPin &&
		a.PendingSecureBaseURL == b.PendingSecureBaseURL &&
		a.PendingSpkiPin == b.PendingSpkiPin
}

func deviceRetirementEndpointsCleared(cfg runtimeConfig) bool {
	return cfg.RelaySecureBaseURL == "" &&
		cfg.RelaySpkiPin == "" &&
		cfg.PendingSecureBaseURL == "" &&
		cfg.PendingSpkiPin == ""
}
