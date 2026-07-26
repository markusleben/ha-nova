package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
)

const serverRemovalCheckpointSchema = 1

type serverRemovalCheckpoint struct {
	Schema             int    `json:"schema"`
	ProfileID          string `json:"profile_id"`
	CurrentService     string `json:"current_service"`
	PendingService     string `json:"pending_service"`
	CurrentDeviceID    string `json:"current_device_id,omitempty"`
	PendingDeviceID    string `json:"pending_device_id,omitempty"`
	OriginalProfileSHA string `json:"original_profile_sha256"`
	RelayInstanceID    string `json:"relay_instance_id,omitempty"`
	SecureBaseURL      string `json:"secure_base_url,omitempty"`
	SPKIPin            string `json:"spki_pin,omitempty"`
	PendingSecureURL   string `json:"pending_secure_base_url,omitempty"`
	PendingSPKIPin     string `json:"pending_spki_pin,omitempty"`
}

func newServerRemovalCheckpoint(
	name string,
	cfg runtimeConfig,
	profileRaw json.RawMessage,
	currentCredential string,
	pendingCredential string,
) serverRemovalCheckpoint {
	checkpoint := serverRemovalCheckpoint{
		Schema:             serverRemovalCheckpointSchema,
		ProfileID:          cfg.ProfileID,
		CurrentService:     deviceCredentialServiceForProfile(name),
		PendingService:     deviceCredentialPendingServiceForProfile(name),
		OriginalProfileSHA: jsonContentSHA256(profileRaw),
		RelayInstanceID:    cfg.RelayInstanceID,
		SecureBaseURL:      cfg.RelaySecureBaseURL,
		SPKIPin:            cfg.RelaySpkiPin,
		PendingSecureURL:   cfg.PendingSecureBaseURL,
		PendingSPKIPin:     cfg.PendingSpkiPin,
	}
	if currentCredential != "" {
		checkpoint.CurrentDeviceID = credentialEvidenceID(
			currentCredential,
			false,
		)
	}
	if pendingCredential != "" {
		checkpoint.PendingDeviceID = credentialEvidenceID(
			pendingCredential,
			true,
		)
	}
	return checkpoint
}

func validateServerRemovalCheckpoint(
	name string,
	cfg runtimeConfig,
) error {
	checkpoint := cfg.ServerRemoval
	if checkpoint == nil {
		return errors.New("missing server removal checkpoint")
	}
	if checkpoint.Schema != serverRemovalCheckpointSchema {
		return fmt.Errorf(
			"unsupported server removal checkpoint schema %d",
			checkpoint.Schema,
		)
	}
	if cfg.ProfileID == "" ||
		checkpoint.ProfileID != cfg.ProfileID {
		return errors.New(
			"server removal checkpoint profile identity mismatch",
		)
	}
	if checkpoint.CurrentService !=
		deviceCredentialServiceForProfile(name) ||
		checkpoint.PendingService !=
			deviceCredentialPendingServiceForProfile(name) {
		return errors.New(
			"server removal checkpoint credential namespace mismatch",
		)
	}
	if checkpoint.OriginalProfileSHA == "" {
		return errors.New(
			"server removal checkpoint lacks its original profile generation",
		)
	}
	if checkpoint.RelayInstanceID != cfg.RelayInstanceID ||
		checkpoint.SecureBaseURL != cfg.RelaySecureBaseURL ||
		checkpoint.SPKIPin != cfg.RelaySpkiPin ||
		checkpoint.PendingSecureURL !=
			cfg.PendingSecureBaseURL ||
		checkpoint.PendingSPKIPin != cfg.PendingSpkiPin {
		return errors.New(
			"server removal checkpoint endpoint identity mismatch",
		)
	}
	return nil
}

func validateServerRemovalCheckpointDocument(
	doc *configDocument,
	name string,
	cfg runtimeConfig,
) error {
	if err := validateServerRemovalCheckpoint(name, cfg); err != nil {
		return err
	}
	raw, err := cloudRecoveryProfileRaw(doc, name)
	if err != nil {
		return err
	}
	generation, err := serverRemovalProfileGeneration(raw)
	if err != nil {
		return err
	}
	if generation != cfg.ServerRemoval.OriginalProfileSHA {
		return errors.New(
			"server removal checkpoint profile generation mismatch",
		)
	}
	return nil
}

func serverRemovalProfileGeneration(
	raw json.RawMessage,
) (string, error) {
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(raw, &profile); err != nil ||
		profile == nil {
		return "", errors.New("invalid server removal profile")
	}
	delete(profile, "server_removal")
	canonical, err := json.Marshal(profile)
	if err != nil {
		return "", err
	}
	return jsonContentSHA256(canonical), nil
}

func credentialEvidenceID(value string, pending bool) string {
	if pending {
		record, err := decodePendingDeviceCredentialRecord(value)
		if err == nil {
			value = record.Credential
		}
	}
	if parsed := parseDeviceCredential(value); parsed != nil {
		return parsed.deviceID
	}
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("unreadable-%x", sum[:8])
}

func rejectPendingServerRemoval(
	name string,
	cfg runtimeConfig,
) error {
	if cfg.ServerRemoval == nil {
		return nil
	}
	if err := validateServerRemovalCheckpoint(name, cfg); err != nil {
		return fmt.Errorf(
			"invalid server removal checkpoint for %q: %w",
			name,
			err,
		)
	}
	return fmt.Errorf(
		"server profile %q is pending verified removal; resume with: ha-nova server remove %s",
		name,
		name,
	)
}

func writeServerRemovalCheckpoint(
	paths runtimePaths,
	doc *configDocument,
	name string,
	checkpoint serverRemovalCheckpoint,
) error {
	servers, err := documentServersCopy(doc)
	if err != nil {
		return err
	}
	if err := normalizeServerProfilesV3(servers); err != nil {
		return err
	}
	raw, exists := servers[name]
	if !exists {
		return fmt.Errorf("server profile %q does not exist", name)
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(raw, &profile); err != nil ||
		profile == nil {
		return fmt.Errorf("invalid server profile %q", name)
	}
	if checkpoint.ProfileID == "" {
		if err := json.Unmarshal(
			profile["profile_id"],
			&checkpoint.ProfileID,
		); err != nil ||
			checkpoint.ProfileID == "" {
			return errors.New(
				"cannot establish a stable profile identity for removal",
			)
		}
	}
	checkpoint.OriginalProfileSHA, err =
		serverRemovalProfileGeneration(raw)
	if err != nil {
		return err
	}
	checkpointRaw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	profile["server_removal"] = checkpointRaw
	updatedRaw, err := json.Marshal(profile)
	if err != nil {
		return err
	}
	servers[name] = updatedRaw
	return writeServersDocument(
		paths,
		doc,
		servers,
		doc.defaultServerName(),
	)
}
