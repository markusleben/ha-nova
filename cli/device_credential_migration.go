package main

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// Explicit file-backend opt-in and keyring-to-file migration for the device
// credential (`setup --service`, `pair --credential-store=file`). Split from
// device_credential_storage.go, which keeps the router/probe; this file owns
// the owner-decision path: forcing file mode for a process and moving live
// keyring credentials across without ever masking or losing one.

// deviceCredentialFileModeExplicit records that file mode came from an explicit
// owner opt-in (`setup --service`, `pair --credential-store=file`) rather than
// headless auto-detection. Only the explicit form persists the backend marker
// mid-pairing — once the pending credential AND endpoint are durable (see
// runSecurePairing) — so an interrupted activation stays resumable on a
// machine whose keyring never unlocks. The auto-detected path keeps its
// stricter "nothing persists before promotion" contract.
var deviceCredentialFileModeExplicit = false

// forceDeviceCredentialFileMode routes THIS process to the file backend before
// the storage probe runs — the explicit owner opt-in behind `setup --service`
// and `pair --credential-store=file`, for machines whose desktop keyring is
// present but never unlocked. A canceled run before pairing persists nothing;
// the marker lands either at credential promotion (deviceSecretFileSet) or,
// for this explicit mode, once the pending pairing state is durable.
func forceDeviceCredentialFileMode() {
	deviceCredentialFileModeForced = true
	deviceCredentialFileModeExplicit = true
}

// migrateKeyringDeviceCredentialToFile moves a READABLE keyring-held device
// credential into the private-file backend. Both explicit opt-ins run it
// before flipping the backend: `setup --service` re-runs with a healthy
// pairing never reach the pairing stage (deviceAlreadyPaired short-circuits
// to verify), and `pair --credential-store=file` on a desktop install must
// never mask the live credential mid-flip. A locked/absent keyring or an
// unpaired install is a normal no-op (false, nil) — the pairing path takes
// over. A failed FILE WRITE is a real error: continuing would silently leave
// the credential in the keyring despite the opt-in, so callers must abort.
func migrateKeyringDeviceCredentialToFile() (bool, error) {
	if deviceSecretFileBacked() {
		return false, nil // already on the file backend
	}
	credential, ok, err := readDeviceCredential()
	if err != nil || !ok {
		if err != nil {
			return false, nil // keyring unreadable (locked/absent): the pairing path takes over
		}
		// CURRENT is cleanly absent, but an interrupted FIRST pairing may have
		// left a pending keyring credential (with the pending endpoint in the
		// config). Resume would promote it back into the desktop keyring before
		// service mode forces files — so move the pending itself and commit the
		// backend, the same durable state an explicit file-mode pairing leaves
		// behind; resume then finishes file-backed.
		pending, pendingOK, pendingErr := readPendingKeyringSlotDirect()
		if pendingErr != nil {
			return false, fmt.Errorf("cannot read the pending device credential from the keyring: %w", pendingErr)
		}
		if !pendingOK {
			return false, nil // never paired: nothing to migrate
		}
		if err := deviceSecretFileSet(deviceCredentialPendingService, pending); err != nil {
			return false, fmt.Errorf("cannot write the pending device credential file: %w", err)
		}
		if err := writeDeviceFileBackendMarker(); err != nil {
			return false, fmt.Errorf("cannot persist the file-backend decision: %w", err)
		}
		_ = keyring.Delete(deviceCredentialPendingService, secretUser())
		return true, nil
	}
	// A pending credential from an interrupted re-pair must move with the
	// install — once the marker exists, pending reads resolve to files, and a
	// keyring-stranded (or unreadable) pending slot would be invisible to
	// resume even though activation may already have replaced the current
	// credential server-side. So read AND copy it BEFORE the backend commits:
	// every failure here aborts while nothing is flipped yet. (A pending FILE
	// without a marker is a documented harmless orphan — reads never consult
	// it.)
	pending, pendingOK, pendingErr := readPendingKeyringSlotDirect()
	if pendingErr != nil {
		return false, fmt.Errorf("cannot read the pending device credential from the keyring: %w", pendingErr)
	}
	if pendingOK {
		if err := deviceSecretFileSet(deviceCredentialPendingService, pending); err != nil {
			return false, fmt.Errorf("cannot write the pending device credential file: %w", err)
		}
	}
	// Mirrors promotePendingFileCredential: the explicit current-file write lays
	// down the file-backend marker on first commit, flipping the install.
	if err := deviceSecretFileSet(deviceCredentialService, credential); err != nil {
		return false, fmt.Errorf("cannot write the device credential file: %w", err)
	}
	// The migrated copies are authoritative now. The keyring originals are the
	// SAME live credentials, not inert leftovers — best-effort removal keeps a
	// single storage location. (File reads win via the marker either way.)
	user := secretUser()
	_ = keyring.Delete(deviceCredentialService, user)
	_ = keyring.Delete(deviceCredentialPendingService, user)
	return true, nil
}

// readPendingKeyringSlotDirect reads the pending slot from the OS keyring
// bypassing the backend router — used only by the keyring→file migration,
// whose reads must stay pinned to the keyring regardless of routing state.
func readPendingKeyringSlotDirect() (string, bool, error) {
	value, err := keyring.Get(deviceCredentialPendingService, secretUser())
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	if parseDeviceCredential(value) == nil {
		return "", false, fmt.Errorf("pending keyring credential is malformed")
	}
	return value, true, nil
}
