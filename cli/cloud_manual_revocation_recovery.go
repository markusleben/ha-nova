package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

func validateManualRemoteAccessRecoveryRequest(
	confirmation string,
	yes bool,
	profileName string,
) error {
	if confirmation == "" {
		return nil
	}
	if !yes {
		return errors.New(
			"--confirm-remote-access-revoked requires --yes",
		)
	}
	if confirmation != profileName {
		return fmt.Errorf(
			"--confirm-remote-access-revoked must exactly match selected server profile %q",
			profileName,
		)
	}
	return nil
}

func manualRemoteAccessRecoveryCommand(profileName string) string {
	return fmt.Sprintf(
		"ha-nova cloud remove --server %s --yes --confirm-remote-access-revoked %s",
		profileName,
		profileName,
	)
}

func cloudAuthorizationCleanupErrorWithRecoveryCommand(
	err error,
	profileName string,
) error {
	if !errors.Is(err, errCloudAuthorizationCleanupUnverifiable) {
		return err
	}
	problem := cloudProblemForError(err)
	copy := *problem
	copy.Detail += fmt.Sprintf(
		"; after both remote revocations are complete, run: %s",
		manualRemoteAccessRecoveryCommand(profileName),
	)
	copy.Cause = err
	return &copy
}

func manualRemoteAccessRecoveryAllowed(
	confirmation string,
	err error,
) bool {
	return confirmation != "" &&
		errors.Is(err, errCloudAuthorizationCleanupUnverifiable)
}

// confirmRemoteAccessRevokedBeforeOAuth records the user's exact, profile-bound
// attestation that the matching NOVA device pairing was already revoked in
// Home Assistant. It validates and checkpoints the same exact local device IDs
// as automatic revocation, but deliberately makes no remote request.
func confirmRemoteAccessRevokedBeforeOAuth(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
	report *uninstallReport,
	checkpointRevocation cloudDeviceRevocationCheckpointer,
) (bool, error) {
	if checkpointRevocation == nil {
		return false, newCloudError(
			CloudErrInvalidInput,
			"checkpoint manually revoked Cloud device",
			nil,
		)
	}
	remoteOnly, err := isRemoteOnlyCloudProfile(cfg)
	if err != nil {
		return false, err
	}
	if cfg.Cloud == nil {
		return false, newCloudError(
			CloudErrSecretCorrupt,
			"confirm manually revoked Cloud device",
			nil,
		)
	}
	if cfg.Cloud.DeviceRevocationCompleted != nil {
		return finishCheckpointedCloudDeviceRevocation(
			ctx,
			cfg,
			profileName,
			remoteOnly,
			report,
		)
	}
	_, checkpoint, err := planCloudDeviceRevocation(
		ctx,
		cfg,
		profileName,
		remoteOnly,
	)
	if err != nil {
		return false, err
	}
	if err := validateManualCloudDeviceSlots(
		ctx,
		profileName,
		remoteOnly,
		checkpoint,
	); err != nil {
		return false, err
	}
	if checkpoint == nil {
		return false, nil
	}
	if err := checkpointRevocation(*checkpoint); err != nil {
		return false, fmt.Errorf(
			"save manually confirmed Cloud device revocation for server %q: %w",
			profileName,
			err,
		)
	}
	currentRemoved, err := deleteCheckpointedCloudDeviceCredentials(
		ctx,
		profileName,
		*checkpoint,
	)
	if err != nil {
		return false, err
	}
	reportCloudDeviceRevocation(
		report,
		profileName,
		remoteOnly,
		false,
	)
	if report != nil {
		report.addNote(fmt.Sprintf(
			"Recorded the Owner-confirmed remote revocation for server %q.",
			profileName,
		))
	}
	return currentRemoved, nil
}

func validateManualCloudDeviceSlots(
	ctx context.Context,
	profileName string,
	remoteOnly bool,
	checkpoint *cloudDeviceRevocationCheckpoint,
) error {
	if remoteOnly {
		current, currentExists, err := readCredentialSlotWithPolicy(
			ctx,
			deviceCredentialServiceForProfile(profileName),
			SecretStoreForbidUI,
		)
		if err != nil {
			return err
		}
		currentCheckpointed := checkpoint != nil &&
			checkpoint.CurrentDeviceID != "" &&
			checkpoint.CurrentDeviceID == deviceIDOf(current)
		if currentExists && !currentCheckpointed {
			return unidentifiedManualCloudDeviceCredential(
				profileName,
				"current",
			)
		}
	}
	pending, pendingExists, err :=
		readPendingDeviceCredentialRecordWithPolicy(
			ctx,
			SecretStoreForbidUI,
		)
	if err != nil {
		return err
	}
	if pendingExists &&
		pending.Source == pendingDeviceCredentialSourceCloud {
		pendingCheckpointed := checkpoint != nil &&
			checkpoint.PendingDeviceID != "" &&
			checkpoint.PendingDeviceID ==
				deviceIDOf(pending.Credential)
		if pendingCheckpointed {
			return nil
		}
		return unidentifiedManualCloudDeviceCredential(
			profileName,
			"pending",
		)
	}
	return nil
}

func unidentifiedManualCloudDeviceCredential(
	profileName string,
	slot string,
) error {
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: fmt.Sprintf(
			"the %s Cloud device credential for server %q cannot be bound to an exact durable activation checkpoint; manual recovery stopped without deleting it",
			slot,
			profileName,
		),
		Cause: newCloudError(
			CloudErrIdentityMismatch,
			"validate manually revoked Cloud device credential",
			nil,
		),
	}
}

func deleteManuallyRevokedCloudAuthorizationPlan(
	ctx context.Context,
	store OAuthSecretStore,
	plan cloudAuthorizationCleanupPlan,
) error {
	if plan.hasRetiring {
		if err := store.RevokeRetiring(
			ctx,
			plan.retiring.Generation,
			SecretStoreForbidUI,
			func(_ context.Context, actual OAuthSecretEnvelope) error {
				if !sameOAuthSecretEnvelope(actual, plan.retiring) {
					return newCloudError(
						CloudErrSecretConflict,
						"remove manually revoked retiring OAuth secret",
						nil,
					)
				}
				return nil
			},
		); err != nil {
			return err
		}
	}
	return deleteRevokedCloudAuthorizationPlan(ctx, store, plan)
}

func sameCloudAuthorizationCleanupPlan(
	left cloudAuthorizationCleanupPlan,
	right cloudAuthorizationCleanupPlan,
) bool {
	return left.hasCurrent == right.hasCurrent &&
		left.hasPending == right.hasPending &&
		left.hasRetiring == right.hasRetiring &&
		(!left.hasCurrent ||
			sameOAuthSecretEnvelope(left.current, right.current)) &&
		(!left.hasPending ||
			sameOAuthSecretEnvelope(left.pending, right.pending)) &&
		(!left.hasRetiring ||
			sameOAuthSecretEnvelope(left.retiring, right.retiring))
}

func sameOAuthSecretEnvelope(
	left OAuthSecretEnvelope,
	right OAuthSecretEnvelope,
) bool {
	return left.SchemaVersion == right.SchemaVersion &&
		left.State == right.State &&
		left.Generation == right.Generation &&
		left.ProfileID == right.ProfileID &&
		left.CanonicalOrigin == right.CanonicalOrigin &&
		left.ClientID == right.ClientID &&
		left.RefreshToken == right.RefreshToken &&
		left.RefreshTokenID == right.RefreshTokenID &&
		sameOptionalTime(
			left.RefreshTokenExpiresAt,
			right.RefreshTokenExpiresAt,
		) &&
		left.HAUserID == right.HAUserID &&
		left.RelayInstanceID == right.RelayInstanceID &&
		left.CreatedAt.Equal(right.CreatedAt) &&
		left.UpdatedAt.Equal(right.UpdatedAt)
}

func sameOptionalTime(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
