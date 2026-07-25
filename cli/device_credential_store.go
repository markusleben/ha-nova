package main

import (
	"context"
	"encoding/json"
	"fmt"
)

// Device-credential storage for the secure-pairing flow. Two OS-keyring slots
// per server profile:
//   - current: the active credential every AI client on this install uses;
//   - pending: a freshly paired credential held locally BEFORE activation, so a
//     re-pair never destroys the working credential until the new one is proven.
// The relay stores only a digest; the plaintext lives here, owner-only.
// The default profile keeps the historic slot names (no re-pairing on upgrade);
// other profiles suffix the profile name. The zero-arg API below routes through
// the process-global selected profile (config_selection.go), so call sites stay
// profile-agnostic.

const (
	deviceCredentialService        = "ha-nova.device-credential"
	deviceCredentialPendingService = "ha-nova.device-credential.pending"

	pendingDeviceCredentialEnvelopeVersion = 1
	pendingDeviceCredentialSourceLocal     = "local"
	pendingDeviceCredentialSourceCloud     = "cloud"
)

// pendingDeviceCredentialEnvelope gives an interrupted Cloud activation
// provenance. Historic/local v1 pending values remain raw credentials, so
// existing installs keep working. Cloud v2 never writes a raw pending value:
// its exact Relay binding travels with the credential and prevents either
// pairing protocol from activating the other protocol's provisional secret.
type pendingDeviceCredentialEnvelope struct {
	Version         int    `json:"version"`
	Source          string `json:"source"`
	Credential      string `json:"credential"`
	RelayInstanceID string `json:"relay_instance_id"`
}

type pendingDeviceCredentialRecord struct {
	Credential      string
	Source          string
	RelayInstanceID string
}

func deviceCredentialServiceForProfile(profile string) string {
	if profile == "" || profile == defaultServerProfileName {
		return deviceCredentialService
	}
	return deviceCredentialService + "." + profile
}

func deviceCredentialPendingServiceForProfile(profile string) string {
	if profile == "" || profile == defaultServerProfileName {
		return deviceCredentialPendingService
	}
	return deviceCredentialPendingService + "." + profile
}

func activeDeviceCredentialService() string {
	return deviceCredentialServiceForProfile(activeServerProfile())
}

func activeDeviceCredentialPendingService() string {
	return deviceCredentialPendingServiceForProfile(activeServerProfile())
}

func readDeviceCredential() (string, bool, error) {
	return readCredentialSlot(activeDeviceCredentialService())
}

func readDeviceCredentialWithPolicy(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) (string, bool, error) {
	return readCredentialSlotWithPolicy(
		ctx,
		activeDeviceCredentialService(),
		ui,
	)
}

func writeDeviceCredential(credential string) error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return writeDeviceCredentialWithPolicy(
		ctx,
		credential,
		SecretStoreForbidUI,
	)
}

func writeDeviceCredentialWithPolicy(
	ctx context.Context,
	credential string,
	ui SecretStoreUIPolicy,
) error {
	if parseDeviceCredential(credential) == nil {
		return fmt.Errorf("refusing to store a malformed device credential")
	}
	return secretSetWithPolicy(
		ctx,
		activeDeviceCredentialService(),
		credential,
		ui,
	)
}

func deleteDeviceCredential() error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return deleteDeviceCredentialWithPolicy(ctx, SecretStoreForbidUI)
}

func deleteDeviceCredentialWithPolicy(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	return secretDeleteWithPolicy(ctx, activeDeviceCredentialService(), ui)
}

func readPendingDeviceCredential() (string, bool, error) {
	record, ok, err := readPendingDeviceCredentialRecord()
	return record.Credential, ok, err
}

func writePendingDeviceCredential(credential string) error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return writePendingDeviceCredentialWithPolicy(
		ctx,
		credential,
		SecretStoreForbidUI,
	)
}

func writePendingDeviceCredentialWithPolicy(
	ctx context.Context,
	credential string,
	ui SecretStoreUIPolicy,
) error {
	if parseDeviceCredential(credential) == nil {
		return fmt.Errorf("refusing to store a malformed pending credential")
	}
	return secretSetWithPolicy(
		ctx,
		activeDeviceCredentialPendingService(),
		credential,
		ui,
	)
}

func writePendingCloudDeviceCredential(
	credential, relayInstanceID string,
) error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return writePendingCloudDeviceCredentialWithPolicy(
		ctx,
		credential,
		relayInstanceID,
		SecretStoreForbidUI,
	)
}

func writePendingCloudDeviceCredentialWithPolicy(
	ctx context.Context,
	credential, relayInstanceID string,
	ui SecretStoreUIPolicy,
) error {
	if parseDeviceCredential(credential) == nil {
		return fmt.Errorf("refusing to store a malformed pending credential")
	}
	if !validIdentifier(relayInstanceID, 256) {
		return fmt.Errorf("refusing to store a pending Cloud credential without a valid Relay identity")
	}
	encoded, err := json.Marshal(pendingDeviceCredentialEnvelope{
		Version:         pendingDeviceCredentialEnvelopeVersion,
		Source:          pendingDeviceCredentialSourceCloud,
		Credential:      credential,
		RelayInstanceID: relayInstanceID,
	})
	if err != nil {
		return fmt.Errorf("encode pending Cloud credential: %w", err)
	}
	return secretSetWithPolicy(
		ctx,
		activeDeviceCredentialPendingService(),
		string(encoded),
		ui,
	)
}

func deletePendingDeviceCredential() error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return deletePendingDeviceCredentialWithPolicy(
		ctx,
		SecretStoreForbidUI,
	)
}

func deletePendingDeviceCredentialWithPolicy(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	return secretDeleteWithPolicy(
		ctx,
		activeDeviceCredentialPendingService(),
		ui,
	)
}

// promotePendingDeviceCredential makes the pending credential current and clears
// the pending slot. Called only AFTER the pending credential has been activated
// and verified against the relay, so the swap is safe.
func promotePendingDeviceCredential() error {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		SecretStoreForbidUI,
	)
	defer cancel()
	return promotePendingDeviceCredentialWithPolicy(
		ctx,
		SecretStoreForbidUI,
	)
}

func promotePendingDeviceCredentialWithPolicy(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	pending, ok, err := readPendingDeviceCredentialRecordWithPolicy(ctx, ui)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no pending credential to promote")
	}
	// Backend is install-wide (see device_credential_storage.go), so current and
	// pending always resolve to the same store — a plain write+delete promotes
	// correctly whether this install uses the keyring or the file backend.
	if err := writeDeviceCredentialWithPolicy(
		ctx,
		pending.Credential,
		ui,
	); err != nil {
		return err
	}
	return deletePendingDeviceCredentialWithPolicy(ctx, ui)
}

// promotePendingFileCredential finalizes a FILE-backed pending credential
// explicitly (used by resume): it writes the current credential file — which
// also lays down the file-backend marker on first commit — and drops the pending
// file. This finishes a headless-interrupted pairing in file mode without a
// storage probe or the process-forced flag, so a now-usable keyring can neither
// reroute nor delete the credential.
func promotePendingFileCredential(pending string) error {
	if parseDeviceCredential(pending) == nil {
		return fmt.Errorf("refusing to store a malformed device credential")
	}
	if err := deviceSecretFileSet(activeDeviceCredentialService(), pending); err != nil {
		return err
	}
	return deviceSecretFileDelete(activeDeviceCredentialPendingService())
}

func readCredentialSlot(service string) (string, bool, error) {
	value, err := secretGet(service)
	return decodeDeviceCredentialSlot(service, value, err)
}

func readCredentialSlotWithPolicy(
	ctx context.Context,
	service string,
	ui SecretStoreUIPolicy,
) (string, bool, error) {
	value, err := secretGetWithPolicy(ctx, service, ui)
	return decodeDeviceCredentialSlot(service, value, err)
}

func decodeDeviceCredentialSlot(
	service, value string,
	readErr error,
) (string, bool, error) {
	err := readErr
	if err != nil {
		if err == errSecretNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	if parseDeviceCredential(value) == nil {
		return "", false, fmt.Errorf("stored credential in %s is malformed", service)
	}
	return value, true, nil
}
