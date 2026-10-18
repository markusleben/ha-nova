package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var errInvalidClientInstallID = errors.New("invalid client_install_id")

func readPendingDeviceCredentialRecord() (
	pendingDeviceCredentialRecord,
	bool,
	error,
) {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return readPendingDeviceCredentialRecordWithPolicy(
		ctx,
		SecretStoreForbidUI,
	)
}

func readPendingDeviceCredentialRecordWithPolicy(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) (
	pendingDeviceCredentialRecord,
	bool,
	error,
) {
	value, err := secretGetWithPolicy(
		ctx,
		activeDeviceCredentialPendingService(),
		ui,
	)
	if err != nil {
		if err == errSecretNotFound {
			return pendingDeviceCredentialRecord{}, false, nil
		}
		return pendingDeviceCredentialRecord{}, false, err
	}
	record, err := decodePendingDeviceCredentialRecord(value)
	if err != nil {
		return pendingDeviceCredentialRecord{}, false, fmt.Errorf(
			"stored pending credential is malformed: %w",
			err,
		)
	}
	return record, true, nil
}

func decodePendingDeviceCredentialRecord(
	raw string,
) (pendingDeviceCredentialRecord, error) {
	raw = strings.TrimSpace(raw)
	if parseDeviceCredential(raw) != nil {
		return pendingDeviceCredentialRecord{
			Credential: raw,
			Source:     pendingDeviceCredentialSourceLocal,
		}, nil
	}

	var envelope pendingDeviceCredentialEnvelope
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return pendingDeviceCredentialRecord{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return pendingDeviceCredentialRecord{}, err
	}
	if envelope.Version != pendingDeviceCredentialEnvelopeVersion ||
		envelope.Source != pendingDeviceCredentialSourceCloud ||
		parseDeviceCredential(envelope.Credential) == nil ||
		!validIdentifier(envelope.RelayInstanceID, 256) {
		return pendingDeviceCredentialRecord{}, fmt.Errorf(
			"invalid pending credential envelope",
		)
	}
	return pendingDeviceCredentialRecord{
		Credential:      envelope.Credential,
		Source:          envelope.Source,
		RelayInstanceID: envelope.RelayInstanceID,
	}, nil
}

// getOrCreateClientInstallID returns the stable install id, generating and
// persisting one on first use. persist is the caller's config saver.
func getOrCreateClientInstallID(
	cfg *runtimeConfig,
	persist func(*runtimeConfig) error,
) (string, error) {
	if cfg.ClientInstallID != "" {
		if err := validateClientInstallID(cfg.ClientInstallID); err != nil {
			return "", err
		}
		return cfg.ClientInstallID, nil
	}
	id, err := newClientInstallID()
	if err != nil {
		return "", err
	}
	cfg.ClientInstallID = id
	if err := persist(cfg); err != nil {
		return "", err
	}
	return cfg.ClientInstallID, nil
}

// pairingClientInstallID is the install id one profile sends when it pairs.
// The Relay drops every other active device with the same id on activation, so
// two profiles of this install paired with one Relay must not share it. The
// default profile keeps the plain id (re-pairing it still replaces its own old
// device); every other profile appends its name. Profile names are the
// credential-slot key and a paired profile cannot be renamed, so the derived id
// is stable while the device exists.
func pairingClientInstallID(installID, profile string) (string, error) {
	if profile == defaultServerProfileName {
		return installID, nil
	}
	if err := validateServerProfileName(profile); err != nil {
		return "", err
	}
	id := installID + "-" + profile
	if !validIdentifier(id, 128) {
		return "", fmt.Errorf("%w: too long for server profile %q", errInvalidClientInstallID, profile)
	}
	return id, nil
}

func newClientInstallID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "inst-" + hex.EncodeToString(buf), nil
}

func validateClientInstallID(value string) error {
	if value == "" {
		return nil
	}
	if !validIdentifier(value, 128) {
		return errInvalidClientInstallID
	}
	return nil
}
