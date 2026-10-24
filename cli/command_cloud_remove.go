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

var readPendingDeviceCredentialForCloudRemove = readPendingDeviceCredentialRecordWithPolicy

func runCloudRemoveCommand(paths runtimePaths, args []string) int {
	options, err := parseCloudCommandFlags("remove", args)
	if errors.Is(err, errHelpShown) {
		return 0
	}
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	initialSnapshot, err := loadCloudRecoverySnapshotUnchecked(paths)
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	cfg := initialSnapshot.Config
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
		printHumanErr("this server has no Home Assistant Link sign-in to remove")
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
		fmt.Fprintln(os.Stdout, "This revokes HA NOVA's Home Assistant Link authorization.")
		if remoteOnly {
			fmt.Fprintln(
				os.Stdout,
				"The Cloud-only NOVA device pairing is revoked too; the Home Assistant Link subscription stays unchanged.",
			)
		} else {
			fmt.Fprintln(
				os.Stdout,
				"The local NOVA device pairing and Home Assistant Link subscription stay unchanged.",
			)
		}
		confirmed, promptErr := promptYesNoMenu(
			bufio.NewReader(os.Stdin),
			os.Stdout,
			"Remove Home Assistant Link access?",
			"Yes, remove it",
			"No, keep it",
			false,
		)
		if promptErr != nil || !confirmed {
			printHumanInfo("Home Assistant Link stays. Nothing was changed.")
			return 0
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var updated runtimeConfig
	unfinishedSignInOnly, cleanupStarted := false, false
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
				cloudCleanupRecoveryCheckpointCause(
					operationErr,
				),
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
		if _, _, err := readPendingDeviceCredentialForCloudRemove(
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
		deviceValidationErr := validateCloudDeviceRevocationPlan(
			ctx,
			cloudSource,
			activeServerProfile(),
		)
		manualRecovery := manualRemoteAccessRecoveryAllowed(
			options.confirmRemoteAccessRevoked,
			authorizationErr,
			deviceValidationErr,
			cloudSource.Cloud != nil &&
				(cloudSource.Cloud.DeviceRevocationCompleted != nil ||
					cloudSource.Cloud.AuthorizationRevocationCompleted != nil),
		)
		if authorizationErr != nil && !manualRecovery {
			return cloudAuthorizationCleanupErrorWithRecoveryCommand(authorizationErr, activeServerProfile(), cloudSource.HAURL)
		}
		if deviceValidationErr != nil && !manualRecovery {
			return cloudAuthorizationCleanupErrorWithRecoveryCommand(deviceValidationErr, activeServerProfile(), cloudSource.HAURL)
		}
		if !manualRecovery &&
			authorizationErr == nil &&
			deviceValidationErr == nil &&
			options.confirmRemoteAccessRevoked != "" {
			recoveryExpectedCaptured = false
			return &cloudProblem{
				Code:        cloudProblemAuthorization,
				Remediation: cloudRemediationSecurityStop,
				Detail: "HA NOVA can check this Link access itself, so " +
					"--confirm-remote-access-revoked isn't needed.\nRun: " +
					cloudProfileCommandFor("remove", activeServerProfile()),
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
		cleanupStarted = true
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
		checkpointAuthorizationRevocation := func(
			plan cloudAuthorizationCleanupPlan,
			ownerConfirmed bool,
		) error {
			checkpointed, expected, err :=
				checkpointCloudAuthorizationRevocationUnlocked(
					paths,
					recoveryExpected,
					plan,
					ownerConfirmed,
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
		if manualRecovery {
			latestPlan, latestErr := inspectCloudAuthorizationCleanup(
				ctx,
				cloudSource,
				store,
			)
			sameAuthorizationOutcome :=
				(authorizationErr == nil && latestErr == nil) ||
					(errors.Is(
						authorizationErr,
						errCloudAuthorizationCleanupUnverifiable,
					) &&
						errors.Is(
							latestErr,
							errCloudAuthorizationCleanupUnverifiable,
						))
			if !sameAuthorizationOutcome ||
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
			if err := checkpointAuthorizationRevocation(
				latestPlan,
				true,
			); err != nil {
				return err
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
				authorizationPlan,
			); err != nil {
				return err
			}
			if err := checkpointAuthorizationRevocation(
				authorizationPlan,
				false,
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
		if !hadConfig {
			return errors.New(
				"Cloud configuration disappeared before cleanup commit",
			)
		}
		if err := writeJSONFileIfUnchanged(
			paths.ConfigFile,
			prepared,
			0o600,
			configSnapshot,
		); err != nil {
			return fmt.Errorf("save Cloud removal checkpoint: %w", err)
		}
		updated = current
		unfinishedSignInOnly = authorizationPlan.unfinishedSignIn && !authorizationPlan.hasAuthorization()
		return nil
	})
	if err != nil {
		printCloudCommandProblem(err)
		if !cleanupStarted && errors.Is(err, errCloudAuthorizationCleanupUnverifiable) { // stopped before any revoke
			printHumanInfo("Nothing was removed yet.")
		} else {
			printHumanInfo("Home Assistant Link settings were kept, unless its sign-in was already turned off.")
		}
		return 1
	}
	if nextCommand, needsRecovery, recoveryErr :=
		invalidInstallIdentityRecoveryCommand(
			paths,
			activeServerProfile(),
		); needsRecovery {
		printHumanInfo("Home Assistant Link access was removed.")
		if nextCommand != "" {
			printHumanInfo("Continue recovery with: %s", nextCommand)
		} else if recoveryErr != nil {
			printHumanErr(
				"Install-identity recovery is paused because remaining Cloud cleanup inventory cannot be resolved safely (%v). Preserve config.json for manual review.",
				recoveryErr,
			)
		}
		return 0
	}
	removed := "Home Assistant Link access was removed."
	if unfinishedSignInOnly {
		removed = unfinishedSignInRemovedText
	}
	if updated.RelaySecureBaseURL != "" && updated.RelaySpkiPin != "" {
		printHumanInfo("%s Local access remains ready.", removed)
	} else if legacySharedAccess(updated) && legacyTokenReadable() {
		printHumanInfo("%s HA NOVA still works here with the old shared access. Run ha-nova setup to switch to your own access.", removed)
	} else {
		printHumanInfo("%s This profile now needs a local pairing before it can be used.", removed)
	}
	return 0
}

func cloudCleanupRecoveryCheckpointCause(cause error) error {
	problem := cloudProblemForError(cause)
	if problem.Code != cloudProblemSecureStorage ||
		problem.Remediation != cloudRemediationUnlockStorage {
		return cause
	}
	return &cloudProblem{
		Code:        cloudProblemSecureStorage,
		Remediation: cloudRemediationVerifyState,
		Detail:      "native secure storage must be verified before Cloud cleanup can continue",
		Cause:       cause,
	}
}
