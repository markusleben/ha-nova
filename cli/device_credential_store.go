package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Device-credential storage for the secure-pairing flow. Two OS-keyring slots:
//   - current: the active credential every AI client on this install uses;
//   - pending: a freshly paired credential held locally BEFORE activation, so a
//     re-pair never destroys the working credential until the new one is proven.
// The relay stores only a digest; the plaintext lives here, owner-only.

const (
	deviceCredentialService        = "ha-nova.device-credential"
	deviceCredentialPendingService = "ha-nova.device-credential.pending"
)

func readDeviceCredential() (string, bool, error) {
	return readCredentialSlot(deviceCredentialService)
}

func writeDeviceCredential(credential string) error {
	if parseDeviceCredential(credential) == nil {
		return fmt.Errorf("refusing to store a malformed device credential")
	}
	return secretSet(deviceCredentialService, credential)
}

func deleteDeviceCredential() error {
	return secretDelete(deviceCredentialService)
}

func readPendingDeviceCredential() (string, bool, error) {
	return readCredentialSlot(deviceCredentialPendingService)
}

func writePendingDeviceCredential(credential string) error {
	if parseDeviceCredential(credential) == nil {
		return fmt.Errorf("refusing to store a malformed pending credential")
	}
	return secretSet(deviceCredentialPendingService, credential)
}

func deletePendingDeviceCredential() error {
	return secretDelete(deviceCredentialPendingService)
}

// promotePendingDeviceCredential makes the pending credential current and clears
// the pending slot. Called only AFTER the pending credential has been activated
// and verified against the relay, so the swap is safe.
func promotePendingDeviceCredential() error {
	pending, ok, err := readPendingDeviceCredential()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no pending credential to promote")
	}
	// Backend is install-wide (see device_credential_storage.go), so current and
	// pending always resolve to the same store — a plain write+delete promotes
	// correctly whether this install uses the keyring or the file backend.
	if err := writeDeviceCredential(pending); err != nil {
		return err
	}
	return deletePendingDeviceCredential()
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
	if err := deviceSecretFileSet(deviceCredentialService, pending); err != nil {
		return err
	}
	return deviceSecretFileDelete(deviceCredentialPendingService)
}

func readCredentialSlot(service string) (string, bool, error) {
	value, err := secretGet(service)
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

// getOrCreateClientInstallID returns the stable install id, generating and
// persisting one on first use. persist is the caller's config saver.
func getOrCreateClientInstallID(cfg *runtimeConfig, persist func(*runtimeConfig) error) (string, error) {
	if cfg.ClientInstallID != "" {
		return cfg.ClientInstallID, nil
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	cfg.ClientInstallID = "inst-" + hex.EncodeToString(buf)
	if err := persist(cfg); err != nil {
		return "", err
	}
	return cfg.ClientInstallID, nil
}
