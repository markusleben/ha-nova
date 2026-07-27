package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

const (
	deviceCredentialKeyringCleanupMarker = ".keyring-migration-cleanup"
	deviceCredentialMigrationSchema      = 1
	deviceCredentialMigrationMaxBytes    = 4096
)

type deviceCredentialMigrationCleanup struct {
	SchemaVersion int      `json:"schema_version"`
	Services      []string `json:"services"`
}

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

// migrateKeyringDeviceCredentialToFile moves READABLE keyring-held device
// credentials — for EVERY known server profile — into the private-file backend.
// Both explicit opt-ins run it before flipping the backend: `setup --service`
// re-runs with a healthy pairing never reach the pairing stage
// (deviceAlreadyPaired short-circuits to verify), and
// `pair --credential-store=file` on a desktop install must never mask a live
// credential mid-flip. All profiles' slots move BEFORE the machine-wide marker
// commits: once the marker exists, reads resolve to files, and a
// keyring-stranded slot of any profile would be invisible afterwards. A
// locked/absent keyring or an unpaired install is a normal no-op (false, nil) —
// the pairing path takes over. A failed FILE WRITE is a real error: continuing
// would silently leave a credential in the keyring despite the opt-in, so
// callers must abort.
func migrateKeyringDeviceCredentialToFile() (bool, error) {
	if deviceSecretFileBacked() {
		return resumeKeyringDeviceCredentialCleanup()
	}
	type slotValue struct{ service, value string }
	var currents, pendings []slotValue
	for _, profile := range credentialProfileNames() {
		currentService := deviceCredentialServiceForProfile(profile)
		pendingService := deviceCredentialPendingServiceForProfile(profile)
		current, currentOK, currentErr := readCredentialSlot(currentService)
		if currentErr != nil {
			return false, nil // keyring unreadable (locked/absent): the pairing path takes over
		}
		if currentOK {
			currents = append(currents, slotValue{currentService, current})
		}
		// A pending credential from an interrupted pairing must move with the
		// install; an unreadable/malformed pending slot aborts BEFORE the flip.
		pending, pendingOK, pendingErr := readKeyringSlotDirect(pendingService)
		if pendingErr != nil {
			return false, fmt.Errorf("cannot read the pending device credential from the keyring: %w", pendingErr)
		}
		if pendingOK {
			pendings = append(pendings, slotValue{pendingService, pending})
		}
	}
	if len(currents) == 0 && len(pendings) == 0 {
		return false, nil // never paired: nothing to migrate
	}
	// Copy pendings first (provisional, never commits the backend), then EVERY
	// current file, and only then the machine-wide marker: once the marker
	// exists, reads resolve to files, so a failed later copy would leave that
	// profile's keyring credential invisible. A marker failure rolls the current
	// files back (a pending FILE without a marker is a documented harmless
	// orphan that reads never consult), so every abort leaves nothing flipped.
	for _, slot := range pendings {
		if err := deviceSecretFileSet(slot.service, slot.value); err != nil {
			return false, fmt.Errorf("cannot write the pending device credential file: %w", err)
		}
	}
	dir, err := deviceSecretFileDir()
	if err != nil {
		return false, fmt.Errorf("cannot write the device credential file: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false, fmt.Errorf("cannot write the device credential file: %w", err)
	}
	var writtenCurrents []string
	rollbackCurrents := func() error {
		var rollbackErr error
		for _, path := range writtenCurrents {
			rollbackErr = errors.Join(
				rollbackErr,
				removeDeviceResiduePath(path),
			)
		}
		return rollbackErr
	}
	for _, slot := range currents {
		path := testSecretPath(dir, slot.service)
		if err := writeSecretFile0600(path, slot.value); err != nil {
			return false, fmt.Errorf(
				"cannot write the device credential file: %w",
				errors.Join(err, rollbackCurrents()),
			)
		}
		writtenCurrents = append(writtenCurrents, path)
	}
	services := make([]string, 0, len(currents)+len(pendings))
	for _, slot := range append(currents, pendings...) {
		services = append(services, slot.service)
	}
	if err := writeKeyringDeviceCredentialCleanup(services); err != nil {
		return false, fmt.Errorf(
			"cannot checkpoint migrated keyring credential cleanup: %w",
			errors.Join(err, rollbackCurrents()),
		)
	}
	if err := writeDeviceFileBackendMarker(); err != nil {
		var cleanupErr error
		if cleanupPath, pathErr := keyringDeviceCredentialCleanupPath(); pathErr == nil {
			cleanupErr = removeDeviceResiduePath(cleanupPath)
		} else {
			cleanupErr = pathErr
		}
		return false, fmt.Errorf(
			"cannot persist the file-backend decision: %w",
			errors.Join(
				err,
				cleanupErr,
				rollbackCurrents(),
				rollbackFailedDeviceFileBackendMarkerWrite(),
			),
		)
	}
	if _, err := resumeKeyringDeviceCredentialCleanup(); err != nil {
		return true, err
	}
	return true, nil
}

func resumeKeyringDeviceCredentialCleanup() (bool, error) {
	cleanup, exists, err := readKeyringDeviceCredentialCleanup()
	if err != nil || !exists {
		return false, err
	}
	for _, service := range cleanup.Services {
		fileValue, err := deviceSecretFileGet(service)
		if err != nil {
			return true, fmt.Errorf(
				"cannot verify migrated device credential file %s: %w",
				service,
				err,
			)
		}
		keyringValue, exists, err := readKeyringDeviceSecret(service)
		if err != nil {
			return true, fmt.Errorf(
				"cannot verify migrated keyring credential %s: %w",
				service,
				err,
			)
		}
		if !exists {
			continue
		}
		if keyringValue != fileValue {
			return true, fmt.Errorf(
				"migrated keyring credential %s differs from its file copy",
				service,
			)
		}
		if err := deleteKeyringDeviceSecret(service); err != nil {
			return true, fmt.Errorf(
				"cannot remove migrated keyring credential %s: %w",
				service,
				err,
			)
		}
	}
	path, err := keyringDeviceCredentialCleanupPath()
	if err != nil {
		return true, err
	}
	if err := removeDeviceResiduePath(path); err != nil {
		return true, fmt.Errorf(
			"cannot clear migrated keyring credential checkpoint: %w",
			err,
		)
	}
	return true, nil
}

func writeKeyringDeviceCredentialCleanup(services []string) error {
	if len(services) == 0 {
		return fmt.Errorf("migration cleanup has no credential services")
	}
	sort.Strings(services)
	for index, service := range services {
		if !validNativeSecretWorkerKey(service, secretUser()) ||
			(index > 0 && services[index-1] == service) {
			return fmt.Errorf("invalid migration cleanup service %q", service)
		}
	}
	data, err := json.Marshal(deviceCredentialMigrationCleanup{
		SchemaVersion: deviceCredentialMigrationSchema,
		Services:      services,
	})
	if err != nil {
		return fmt.Errorf("encode migration cleanup checkpoint: %w", err)
	}
	if len(data) > deviceCredentialMigrationMaxBytes {
		return fmt.Errorf("migration cleanup checkpoint exceeds size limit")
	}
	path, err := keyringDeviceCredentialCleanupPath()
	if err != nil {
		return err
	}
	return writeSecretFile0600(path, string(data))
}

func readKeyringDeviceCredentialCleanup() (
	deviceCredentialMigrationCleanup,
	bool,
	error,
) {
	path, err := keyringDeviceCredentialCleanupPath()
	if err != nil {
		return deviceCredentialMigrationCleanup{}, false, err
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return deviceCredentialMigrationCleanup{}, false, nil
		}
		return deviceCredentialMigrationCleanup{}, false, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(
		file,
		deviceCredentialMigrationMaxBytes+1,
	))
	if err != nil {
		return deviceCredentialMigrationCleanup{}, false, fmt.Errorf(
			"read migration cleanup checkpoint: %w",
			err,
		)
	}
	if len(data) > deviceCredentialMigrationMaxBytes {
		return deviceCredentialMigrationCleanup{}, false, fmt.Errorf(
			"migration cleanup checkpoint exceeds size limit",
		)
	}
	var cleanup deviceCredentialMigrationCleanup
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cleanup); err != nil {
		return deviceCredentialMigrationCleanup{}, false, fmt.Errorf(
			"decode migration cleanup checkpoint: %w",
			err,
		)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return deviceCredentialMigrationCleanup{}, false, fmt.Errorf(
			"decode migration cleanup checkpoint: %w",
			err,
		)
	}
	if cleanup.SchemaVersion != deviceCredentialMigrationSchema ||
		len(cleanup.Services) == 0 {
		return deviceCredentialMigrationCleanup{}, false, fmt.Errorf(
			"invalid migration cleanup checkpoint",
		)
	}
	for index, service := range cleanup.Services {
		if !validNativeSecretWorkerKey(service, secretUser()) ||
			(index > 0 && cleanup.Services[index-1] >= service) {
			return deviceCredentialMigrationCleanup{}, false, fmt.Errorf(
				"invalid migration cleanup service %q",
				service,
			)
		}
	}
	return cleanup, true, nil
}

func keyringDeviceCredentialCleanupPath() (string, error) {
	dir, err := deviceSecretFileDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, deviceCredentialKeyringCleanupMarker), nil
}

// credentialProfileNames returns every server profile whose credential slots
// this machine may hold: the configured profiles plus the active one, always
// including the default. Best-effort — a missing config yields the defaults.
func credentialProfileNames() []string {
	names := map[string]bool{defaultServerProfileName: true, activeServerProfile(): true}
	if paths, err := detectPaths(); err == nil {
		if doc, err := loadConfigDocument(paths.ConfigFile); err == nil {
			for _, name := range doc.profileNames() {
				names[name] = true
			}
		}
	}
	list := make([]string, 0, len(names))
	for name := range names {
		list = append(list, name)
	}
	sort.Strings(list)
	return list
}

// readKeyringSlotDirect reads a credential slot from the OS keyring bypassing
// the backend router — used only by the keyring→file migration, whose reads
// must stay pinned to the keyring regardless of routing state.
func readKeyringSlotDirect(service string) (string, bool, error) {
	value, exists, err := readKeyringDeviceSecret(service)
	if err != nil || !exists {
		return "", exists, err
	}
	if _, err := decodePendingDeviceCredentialRecord(value); err != nil {
		return "", false, fmt.Errorf(
			"keyring pending credential in %s is malformed: %w",
			service,
			err,
		)
	}
	// Preserve the serialized Cloud provenance while copying the secret to the
	// file backend. readPendingDeviceCredentialRecord validates it again after
	// the backend flip.
	return value, true, nil
}

// readPendingKeyringSlotDirect keeps the historic default-profile helper name
// for the resume path and tests.
func readPendingKeyringSlotDirect() (string, bool, error) {
	return readKeyringSlotDirect(deviceCredentialPendingService)
}
