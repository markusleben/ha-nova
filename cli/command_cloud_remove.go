package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

func runCloudRemoveCommand(paths runtimePaths, args []string) int {
	options, err := parseCloudCommandFlags("remove", args)
	if errors.Is(err, errHelpShown) {
		return 0
	}
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	if err := validateServerProfileName(activeServerProfile()); err != nil {
		printHumanErr("invalid selected server profile: %s", err)
		return 1
	}
	if cfg.ProfileID == "" {
		printHumanErr("the selected server profile has no Cloud credential identity")
		return 1
	}
	remoteOnly, err := isRemoteOnlyCloudProfile(cfg)
	if err != nil {
		printCloudCommandProblem(err)
		return 1
	}
	configSnapshot, hadConfig, err := readOptionalFile(paths.ConfigFile)
	if err != nil {
		printHumanErr("cannot inspect server configuration: %s", err)
		return 1
	}
	if !options.yes {
		if !uiInputSupportsTTY() || !stdoutIsInteractiveTTY() {
			printHumanErr("cloud remove requires confirmation; rerun with --yes")
			return 1
		}
		fmt.Fprintln(
			os.Stdout,
			"This revokes HA NOVA's Home Assistant Cloud authorization.",
		)
		if remoteOnly {
			fmt.Fprintln(
				os.Stdout,
				"The Cloud-only NOVA device pairing is revoked too; the Home Assistant Cloud subscription stays unchanged.",
			)
		} else {
			fmt.Fprintln(
				os.Stdout,
				"The local NOVA device pairing and Home Assistant Cloud subscription stay unchanged.",
			)
		}
		confirmed, promptErr := promptWizardYesNoFromReader(
			bufio.NewReader(os.Stdin),
			os.Stdout,
			"Remove Home Assistant Cloud access",
			false,
		)
		if promptErr != nil || !confirmed {
			printHumanInfo("Cloud removal cancelled — nothing was changed.")
			return 0
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var updated runtimeConfig
	var recoveryExpected cloudRecoveryCheckpointExpectation
	var recoveryExpectedCaptured bool
	err = withClientMutationLock(paths, func() (operationErr error) {
		defer func() {
			if operationErr == nil || !recoveryExpectedCaptured {
				return
			}
			if _, holdErr := checkpointCloudRecoveryHoldUnlocked(
				paths,
				recoveryExpected,
				operationErr,
			); holdErr != nil {
				operationErr = errors.Join(
					operationErr,
					fmt.Errorf(
						"persist Cloud recovery safety hold: %w",
						holdErr,
					),
				)
			}
		}()
		if err := ensureOptionalFileSnapshotCurrent(
			paths.ConfigFile,
			configSnapshot,
			hadConfig,
		); err != nil {
			return err
		}
		snapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
		if err != nil {
			return err
		}
		current := snapshot.Config
		recoveryExpected, err = snapshot.recoveryExpectation()
		if err != nil {
			return fmt.Errorf(
				"capture Cloud removal safety checkpoint: %w",
				err,
			)
		}
		recoveryExpectedCaptured = true
		cloudSource := current
		current.Cloud = nil
		current.RoutePolicy = routePolicyLocal
		if strings.TrimSpace(current.RelaySecureBaseURL) == "" ||
			strings.TrimSpace(current.RelaySpkiPin) == "" {
			// A Cloud-only profile cannot prove that a later app installation is
			// the same Relay. Clearing the stale identity lets the next explicit
			// Cloud authorization bind to the newly authenticated Relay. A
			// complete local pairing keeps the identity pin across reconnects.
			current.RelayInstanceID = ""
		}
		prepared, err := prepareCloudRemovalDocument(paths, current)
		if err != nil {
			return fmt.Errorf("prepare Cloud removal checkpoint: %w", err)
		}
		currentWithoutDevice := current
		currentWithoutDevice.RelaySecureBaseURL = ""
		currentWithoutDevice.RelaySpkiPin = ""
		currentWithoutDevice.PendingSecureBaseURL = ""
		currentWithoutDevice.PendingSpkiPin = ""
		currentWithoutDevice.RelayInstanceID = ""
		deviceRemovedPrepared, err := prepareCloudRemovalDocument(
			paths,
			currentWithoutDevice,
		)
		if err != nil {
			return fmt.Errorf(
				"prepare device-removal checkpoint: %w",
				err,
			)
		}
		store, err := newCloudSecretStoreForCLI(current.ProfileID)
		if err != nil {
			return err
		}
		// Prove the device pending slot is readable before revoking any external
		// authorization. A locked or corrupt device store must fail without
		// leaving a half-completed removal.
		if _, _, err := readPendingDeviceCredentialRecordWithPolicy(
			ctx,
			SecretStoreForbidUI,
		); err != nil {
			return err
		}
		deviceRemoved, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
			ctx,
			cloudSource,
			activeServerProfile(),
			store,
			nil,
			false,
			func(
				checkpoint cloudDeviceRevocationCheckpoint,
			) error {
				checkpointed, expected, err :=
					checkpointCloudDeviceRevocationUnlocked(
						paths,
						recoveryExpected,
						checkpoint,
					)
				if err != nil {
					return err
				}
				cloudSource = checkpointed
				recoveryExpected = expected
				configSnapshot, hadConfig, err = readOptionalFile(
					paths.ConfigFile,
				)
				return err
			},
		)
		if err != nil {
			return err
		}
		if deviceRemoved {
			current = currentWithoutDevice
			prepared = deviceRemovedPrepared
		}
		if err := revokeAllCloudAuthorizations(ctx, store); err != nil {
			return err
		}
		if err := deleteRevokedCloudAuthorizations(ctx, store); err != nil {
			return err
		}
		if err := deletePendingCloudDeviceCredentialWithContext(
			ctx,
		); err != nil {
			return err
		}
		// Revocation can take long enough for an out-of-process config editor to
		// race this command. Never overwrite that newer document with the
		// prepared snapshot. The authorization is already invalid and the old
		// Cloud metadata intentionally remains a visible recovery checkpoint.
		if err := ensureOptionalFileSnapshotCurrent(
			paths.ConfigFile,
			configSnapshot,
			hadConfig,
		); err != nil {
			return err
		}
		if err := writeJSONFile(paths.ConfigFile, prepared, 0o600); err != nil {
			return fmt.Errorf("save Cloud removal checkpoint: %w", err)
		}
		updated = current
		return nil
	})
	if err != nil {
		printCloudCommandProblem(err)
		printHumanInfo("Cloud configuration was kept unless revocation had already been verified.")
		return 1
	}
	if updated.RelaySecureBaseURL != "" && updated.RelaySpkiPin != "" {
		printHumanInfo("Home Assistant Cloud access was removed. Local access remains ready.")
	} else {
		printHumanInfo("Home Assistant Cloud access was removed. This profile now needs a local pairing before it can be used.")
	}
	return 0
}

func deletePendingCloudDeviceCredentialWithContext(
	ctx context.Context,
) error {
	pending, exists, err := readPendingDeviceCredentialRecordWithPolicy(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil || !exists {
		return err
	}
	if pending.Source != pendingDeviceCredentialSourceCloud {
		return nil
	}
	if err := deletePendingDeviceCredentialWithPolicy(
		ctx,
		SecretStoreForbidUI,
	); err != nil {
		return fmt.Errorf("remove pending Cloud device credential: %w", err)
	}
	return nil
}

func prepareCloudRemovalDocument(
	paths runtimePaths,
	updated runtimeConfig,
) (map[string]json.RawMessage, error) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return nil, err
	}
	if err := validateExistingServerProfileIDs(doc.servers); err != nil {
		return nil, err
	}
	name, err := resolveSelectedServerProfile(doc)
	if err != nil {
		return nil, err
	}
	if doc.servers != nil {
		if rawCloud, exists := doc.top["cloud"]; exists &&
			len(bytes.TrimSpace(rawCloud)) > 0 &&
			!bytes.Equal(bytes.TrimSpace(rawCloud), []byte("null")) {
			return nil, unknownCloudRemovalShape(name)
		}
	}
	var rawProfile json.RawMessage
	if doc.servers == nil {
		rawProfile, err = json.Marshal(doc.top)
	} else {
		rawProfile = doc.servers[name]
	}
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(rawProfile, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("inspect selected server profile")
	}
	if rawCloud, exists := fields["cloud"]; exists &&
		len(bytes.TrimSpace(rawCloud)) > 0 &&
		!bytes.Equal(bytes.TrimSpace(rawCloud), []byte("null")) {
		if err := validateKnownCloudRemovalShape(name, rawCloud); err != nil {
			return nil, err
		}
	}
	return doc.withProfilePreservingSiblings(name, updated)
}

func revokeAllCloudAuthorizations(
	ctx context.Context,
	store OAuthSecretStore,
) error {
	retiring, hasRetiring, err := store.LoadRetiring(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil {
		return err
	}
	if hasRetiring {
		if err := store.RevokeRetiring(
			ctx,
			retiring.Generation,
			SecretStoreForbidUI,
			revokeAndVerifyCloudAuthorizationForCLI,
		); err != nil {
			return err
		}
	}
	pending, hasPending, err := store.LoadPending(ctx, SecretStoreForbidUI)
	if err != nil {
		return err
	}
	if hasPending {
		if err := revokeAndVerifyCloudAuthorizationForCLI(ctx, pending); err != nil {
			return err
		}
	}
	current, hasCurrent, err := store.LoadCurrent(ctx, SecretStoreForbidUI)
	if err != nil {
		return err
	}
	if hasCurrent {
		if err := revokeAndVerifyCloudAuthorizationForCLI(ctx, current); err != nil {
			return err
		}
	}
	return nil
}

func deleteRevokedCloudAuthorizations(
	ctx context.Context,
	store OAuthSecretStore,
) error {
	pending, hasPending, err := store.LoadPending(ctx, SecretStoreForbidUI)
	if err != nil {
		return err
	}
	if hasPending {
		if err := store.DeletePending(
			ctx,
			pending.Generation,
			SecretStoreForbidUI,
		); err != nil {
			return err
		}
	}
	return store.DeleteCurrent(ctx, SecretStoreForbidUI)
}

func loadSelectedRuntimeConfigUnchecked(paths runtimePaths) (runtimeConfig, error) {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return runtimeConfig{}, fmt.Errorf(
				"cannot read HA NOVA server configuration %s: %w; restore or repair the file before retrying",
				paths.ConfigFile,
				err,
			)
		}
		return runtimeConfig{}, fmt.Errorf(
			"HA NOVA is not set up yet. Run: ha-nova setup: %w",
			err,
		)
	}
	if err := validateSupportedConfigDocument(doc); err != nil {
		return runtimeConfig{}, err
	}
	if err := validateExistingServerProfileIDs(doc.servers); err != nil {
		return runtimeConfig{}, fmt.Errorf(
			"invalid server profile identities: %w",
			err,
		)
	}
	name, err := resolveSelectedServerProfile(doc)
	if err != nil {
		return runtimeConfig{}, err
	}
	cfg, ok := doc.flatProfile(name)
	if !ok {
		return runtimeConfig{}, fmt.Errorf("server profile %q does not exist", name)
	}
	setActiveServerProfile(name)
	if cfg.ProfileID != "" {
		if err := validateProfileID(cfg.ProfileID); err != nil {
			return runtimeConfig{}, err
		}
	}
	return cfg, nil
}
