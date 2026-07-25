package main

import (
	"bufio"
	"context"
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
	if err := validateManualRemoteAccessRecoveryRequest(
		options.confirmRemoteAccessRevoked,
		options.yes,
		activeServerProfile(),
	); err != nil {
		printHumanErr("%s", err)
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
		authorizationPlan, authorizationErr :=
			inspectCloudAuthorizationCleanup(
				ctx,
				cloudSource,
				store,
			)
		manualRecovery := manualRemoteAccessRecoveryAllowed(
			options.confirmRemoteAccessRevoked,
			authorizationErr,
		)
		if authorizationErr != nil && !manualRecovery {
			return cloudAuthorizationCleanupErrorWithRecoveryCommand(
				authorizationErr,
				activeServerProfile(),
			)
		}
		if authorizationErr == nil &&
			options.confirmRemoteAccessRevoked != "" {
			recoveryExpectedCaptured = false
			return &cloudProblem{
				Code:        cloudProblemAuthorization,
				Remediation: cloudRemediationSecurityStop,
				Detail: "--confirm-remote-access-revoked is accepted " +
					"only when automatic cleanup cannot verify the " +
					"saved remote access",
			}
		}
		checkpointDeviceRevocation := func(
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
		}
		var deviceRemoved bool
		if manualRecovery {
			deviceRemoved, err = confirmRemoteAccessRevokedBeforeOAuth(
				ctx,
				cloudSource,
				activeServerProfile(),
				nil,
				checkpointDeviceRevocation,
			)
		} else {
			deviceRemoved, err = revokeRemoteOnlyCloudDeviceBeforeOAuth(
				ctx,
				cloudSource,
				activeServerProfile(),
				store,
				nil,
				checkpointDeviceRevocation,
			)
		}
		if err != nil {
			return err
		}
		if deviceRemoved {
			current = currentWithoutDevice
			prepared = deviceRemovedPrepared
		}
		if manualRecovery {
			latestPlan, latestErr := inspectCloudAuthorizationCleanup(
				ctx,
				cloudSource,
				store,
			)
			if !errors.Is(
				latestErr,
				errCloudAuthorizationCleanupUnverifiable,
			) ||
				!sameCloudAuthorizationCleanupPlan(
					authorizationPlan,
					latestPlan,
				) {
				return newCloudError(
					CloudErrSecretConflict,
					"revalidate manually revoked Cloud authorizations",
					latestErr,
				)
			}
			if err := deleteManuallyRevokedCloudAuthorizationPlan(
				ctx,
				store,
				latestPlan,
			); err != nil {
				return err
			}
		} else {
			if err := revokeCloudAuthorizationCleanupPlan(
				ctx,
				store,
				authorizationPlan,
			); err != nil {
				return err
			}
			if err := deleteRevokedCloudAuthorizationPlan(
				ctx,
				store,
				authorizationPlan,
			); err != nil {
				return err
			}
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
