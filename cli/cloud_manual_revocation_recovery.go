package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
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

// cloudAuthorizationCleanupErrorWithRecoveryCommand adds the owner steps;
// haURL is the profile's saved local Home Assistant address, for their links.
func cloudAuthorizationCleanupErrorWithRecoveryCommand(
	err error,
	profileName string,
	haURL string,
) error {
	if !errors.Is(err, errCloudAuthorizationCleanupUnverifiable) &&
		!errors.Is(err, errCloudDeviceRevocationCredentialMissing) {
		return err
	}
	problem := cloudProblemForError(err)
	copy := *problem
	if errors.Is(err, errCloudAuthorizationCleanupUnverifiable) {
		copy.Detail += "\n" + cloudAuthorizationRecoverySteps(profileName, problem.linkClient, haURL)
	} else {
		copy.Detail += fmt.Sprintf(
			"; after both remote revocations are complete, run: %s",
			manualRemoteAccessRecoveryCommand(profileName),
		)
	}
	copy.Cause = err
	return &copy
}

// cloudAuthorizationStopText is a Link stop for `ha-nova uninstall`: plain
// text and its steps, without the wrapping steps ("failed to revoke …:
// inspect …:"). A joined failure (a safety hold that could not be saved)
// stays; the problem's own cause does not.
func cloudAuthorizationStopText(err error) (string, bool) {
	problem := cloudAuthorizationStopProblem(err)
	if problem == nil {
		return "", false
	}
	copy := *problem
	copy.Detail += "\n4. Then run ha-nova uninstall again."
	lines := []string{copy.humanText()}
	var other func(error)
	other = func(e error) {
		for ; e != nil && e != error(problem); e = errors.Unwrap(e) {
			joined, ok := e.(interface{ Unwrap() []error })
			if !ok {
				continue
			}
			for _, part := range joined.Unwrap() {
				if errors.Is(part, errCloudAuthorizationCleanupUnverifiable) {
					other(part)
				} else {
					lines = append(lines, part.Error())
				}
			}
			return
		}
	}
	other(err)
	return strings.Join(lines, "\n"), true
}

// cloudAuthorizationStopProblem finds the problem that carries the
// cannot-verify stop, not just the first problem in the chain.
func cloudAuthorizationStopProblem(err error) *cloudProblem {
	if !errors.Is(err, errCloudAuthorizationCleanupUnverifiable) {
		return nil
	}
	if problem, ok := err.(*cloudProblem); ok {
		return problem
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, part := range joined.Unwrap() {
			if problem := cloudAuthorizationStopProblem(part); problem != nil {
				return problem
			}
		}
		return nil
	}
	return cloudAuthorizationStopProblem(errors.Unwrap(err))
}

// cloudAuthorizationRecoverySteps are the steps that make the recovery
// command safe. Home Assistant lists a HA NOVA sign-in under Profile >
// Security > Refresh tokens by its address (127.0.0.1, a random port, and
// oauthLoopbackClientPath). haURL adds a direct link to each page
// (ownerStepLinks); without one the steps name the pages only.
func cloudAuthorizationRecoverySteps(profileName, client, haURL string) string {
	remove := fmt.Sprintf("delete %q.", client)
	if client == "" {
		remove = fmt.Sprintf(
			"delete every entry that ends in %q. This turns off Home Assistant Link on your other computers too. Their local connection keeps working.",
			oauthLoopbackClientPath,
		)
	}
	panel, security := ownerStepLinks(haURL)
	steps := []string{
		"Next, in Home Assistant:",
		"1. As the owner, open the NOVA App page. If this computer is listed with a \"cloud\" tag, click Revoke next to it.",
	}
	if panel != "" {
		steps = append(steps, "   Open: "+panel)
	}
	steps = append(steps, "2. Each person who uses HA NOVA: open Profile > Security, and under \"Refresh tokens\", "+remove)
	if security != "" {
		steps = append(steps, "   Open: "+security)
	}
	steps = append(steps,
		"   If you don't find any, that's fine. Go to step 3.",
		"3. Run: "+manualRemoteAccessRecoveryCommand(profileName),
	)
	return strings.Join(steps, "\n")
}

// ownerStepLinks: the NOVA App page and Profile > Security on the saved
// local Home Assistant address. Only a plain http(s) address with no user,
// query, or fragment, and never the Home Assistant Link domain; "" when
// there is none (or no official App page to link).
func ownerStepLinks(haURL string) (panel, security string) {
	parsed, err := url.Parse(strings.TrimSpace(haURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" ||
		strings.HasSuffix(strings.ToLower(parsed.Hostname()), "nabu.casa") {
		return "", ""
	}
	base := parsed.Scheme + "://" + parsed.Host
	if target, err := haNOVAAppPanelURL(base); err == nil && target.Direct {
		panel = target.URL
	}
	return panel, haProfileSecurityURL(base)
}

func manualRemoteAccessRecoveryAllowed(
	confirmation string,
	authorizationErr error,
	deviceErr error,
	remoteRevocationCheckpoint bool,
) bool {
	authorizationMissing := errors.Is(
		authorizationErr,
		errCloudAuthorizationCleanupUnverifiable,
	)
	deviceMissing := errors.Is(
		deviceErr,
		errCloudDeviceRevocationCredentialMissing,
	)
	return confirmation != "" &&
		(authorizationMissing ||
			deviceMissing ||
			remoteRevocationCheckpoint) &&
		(authorizationErr == nil || authorizationMissing) &&
		(deviceErr == nil || deviceMissing)
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
	checkpoint, err := planManuallyConfirmedCloudDeviceRevocation(
		ctx,
		cfg,
		profileName,
		remoteOnly,
	)
	if err != nil {
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
		report.addDetail(fmt.Sprintf(
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
	return deleteRevokedCloudAuthorizationPlan(ctx, store, plan)
}

func sameCloudAuthorizationCleanupPlan(
	left cloudAuthorizationCleanupPlan,
	right cloudAuthorizationCleanupPlan,
) bool {
	return left.hasCurrent == right.hasCurrent &&
		left.hasPending == right.hasPending &&
		left.hasRetiring == right.hasRetiring &&
		left.revocationCheckpointed == right.revocationCheckpointed &&
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
