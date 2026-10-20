package main

import (
	"context"
	"errors"
	"fmt"
)

// unfinishedSignInRemovedText is what purge and `cloud remove` say after they
// drop a Link sign-in that never reached the code exchange.
const unfinishedSignInRemovedText = "Removed a Home Assistant Link sign-in " +
	"that never finished. Nothing was signed in, so there was nothing to sign out."

var errCloudAuthorizationCleanupUnverifiable = errors.New(
	"Cloud authorization cleanup is not locally verifiable",
)

// cloudAuthorizationCleanupPlan is a complete, immutable view of every native
// OAuth slot. Cleanup must build and validate this plan before revoking any
// device or authorization so a missing or corrupt later slot cannot strand a
// potentially live grant after partial teardown.
type cloudAuthorizationCleanupPlan struct {
	current                OAuthSecretEnvelope
	hasCurrent             bool
	pending                OAuthSecretEnvelope
	hasPending             bool
	retiring               OAuthSecretEnvelope
	hasRetiring            bool
	revocationCheckpointed bool
	unfinishedSignIn       bool // for the plain "removed" line only
}

func inspectCloudAuthorizationCleanup(
	ctx context.Context,
	cfg runtimeConfig,
	store OAuthSecretStore,
) (cloudAuthorizationCleanupPlan, error) {
	var plan cloudAuthorizationCleanupPlan
	var err error

	plan.retiring, plan.hasRetiring, err = store.LoadRetiring(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil {
		return cloudAuthorizationCleanupPlan{}, err
	}
	plan.pending, plan.hasPending, err = store.LoadPending(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil {
		return cloudAuthorizationCleanupPlan{}, err
	}
	plan.current, plan.hasCurrent, err = store.LoadCurrent(
		ctx,
		SecretStoreForbidUI,
	)
	if err != nil {
		return cloudAuthorizationCleanupPlan{}, err
	}
	if cfg.Cloud != nil &&
		cfg.Cloud.AuthorizationRevocationCompleted != nil {
		if err := plan.validateRevocationCheckpoint(
			cfg.Cloud.AuthorizationRevocationCompleted,
		); err != nil {
			return plan, err
		}
		plan.revocationCheckpointed = true
		return plan, nil
	}
	plan.unfinishedSignIn = cfg.Cloud.unfinishedSignIn()
	if err := plan.validateConfiguredGrants(cfg); err != nil {
		return plan, plan.explainCloudAuthorizationStop(cfg, err)
	}
	return plan, nil
}

// explainCloudAuthorizationStop names the sign-in to delete in Home
// Assistant, and the common case: a sign-in with no stored credential whose
// code may still have been exchanged (for example one an older version
// started).
func (plan cloudAuthorizationCleanupPlan) explainCloudAuthorizationStop(
	cfg runtimeConfig,
	err error,
) error {
	var problem *cloudProblem
	if cfg.Cloud == nil ||
		!errors.Is(err, errCloudAuthorizationCleanupUnverifiable) ||
		!errors.As(err, &problem) {
		return err
	}
	copy := *problem
	copy.linkClient = plan.onlyLinkClient(cfg)
	if cfg.Cloud.State == cloudStateAuthorizing &&
		cfg.Cloud.Current == nil &&
		!plan.hasAuthorization() {
		copy.Detail = "Your sign-in on this computer didn't finish. HA NOVA " +
			"can't tell if Home Assistant saved it, so it stopped."
	}
	return &copy
}

// onlyLinkClient is the one sign-in address every configured and stored
// grant uses, or "" when there may be more than one to delete. A Pending
// entry with no stored credential may have replaced the address of an
// exchange that ran before a crash, so it never names one.
func (plan cloudAuthorizationCleanupPlan) onlyLinkClient(cfg runtimeConfig) string {
	if cfg.Cloud.Pending != nil && !plan.storesGeneration(cfg.Cloud.Pending.CredentialGeneration) {
		return ""
	}
	var clients []string
	for _, metadata := range []*cloudConnectionMetadata{cfg.Cloud.Pending, cfg.Cloud.Current} {
		if metadata != nil {
			clients = append(clients, metadata.OAuthClientID)
		}
	}
	for _, envelope := range plan.envelopes() {
		clients = append(clients, envelope.ClientID)
	}
	if len(clients) == 0 {
		return ""
	}
	for _, client := range clients[1:] {
		if client != clients[0] {
			return ""
		}
	}
	return clients[0]
}

func (plan cloudAuthorizationCleanupPlan) storesGeneration(generation string) bool {
	for _, envelope := range plan.envelopes() {
		if envelope.Generation == generation {
			return true
		}
	}
	return false
}

// unfinishedSignIn: a Link sign-in that never reached the code exchange.
// Home Assistant creates a token only at that exchange, and setup saves
// Pending right before it, so there is nothing to revoke. Only the plain
// "removed" line uses this; the checks below stay the same.
func (m *cloudLifecycleMetadata) unfinishedSignIn() bool {
	return m != nil &&
		m.State == cloudStateAuthorizing &&
		m.Pending == nil &&
		m.Current == nil &&
		m.RecoveryHold == nil && // an unknown outcome may have been revoked by hand
		!m.cleanupPending()
}

func (plan cloudAuthorizationCleanupPlan) hasAuthorization() bool {
	return plan.hasCurrent || plan.hasPending || plan.hasRetiring
}

func (plan cloudAuthorizationCleanupPlan) validateConfiguredGrants(
	cfg runtimeConfig,
) error {
	if cfg.Cloud == nil {
		return nil
	}
	if cfg.Cloud.Current != nil {
		if err := plan.requireMetadataMatch(
			cfg,
			*cfg.Cloud.Current,
		); err != nil {
			return err
		}
	}

	authorizationOutcomeUnknown := cfg.Cloud.RecoveryHold != nil &&
		cfg.Cloud.RecoveryHold.Code == cloudProblemAuthorization &&
		cfg.Cloud.RecoveryHold.Remediation == cloudRemediationSecurityStop
	if authorizationOutcomeUnknown &&
		cfg.Cloud.Pending == nil &&
		cfg.Cloud.Current == nil {
		return unverifiableCloudAuthorizationCleanup(nil)
	}
	if cfg.Cloud.Pending != nil {
		if err := plan.requireMetadataMatch(
			cfg,
			*cfg.Cloud.Pending,
		); err != nil {
			return err
		}
	}
	return nil
}

func (plan cloudAuthorizationCleanupPlan) requireMetadataMatch(
	cfg runtimeConfig,
	metadata cloudConnectionMetadata,
) error {
	origin, err := cloudOriginFromMetadata(metadata)
	if err != nil {
		return unverifiableCloudAuthorizationCleanup(err)
	}
	candidates := plan.envelopes()
	var bindingErr error
	for _, envelope := range candidates {
		if envelope.Generation != metadata.CredentialGeneration {
			continue
		}
		bindingErr = validateResumableCloudBinding(
			cfg,
			metadata,
			envelope,
			origin,
		)
		if bindingErr == nil {
			return nil
		}
	}
	return unverifiableCloudAuthorizationCleanup(bindingErr)
}

func (plan cloudAuthorizationCleanupPlan) envelopes() []OAuthSecretEnvelope {
	envelopes := make([]OAuthSecretEnvelope, 0, 3)
	if plan.hasRetiring {
		envelopes = append(envelopes, plan.retiring)
	}
	if plan.hasPending {
		envelopes = append(envelopes, plan.pending)
	}
	if plan.hasCurrent {
		envelopes = append(envelopes, plan.current)
	}
	return envelopes
}

// unverifiableCloudAuthorizationCleanup is the plain first line; the next
// step with the exact recovery command is added per profile by
// cloudAuthorizationCleanupErrorWithRecoveryCommand.
func unverifiableCloudAuthorizationCleanup(cause error) error {
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: "HA NOVA can't check if this computer is still signed " +
			"in, so it stopped.",
		Cause: errors.Join(
			errCloudAuthorizationCleanupUnverifiable,
			cause,
		),
	}
}

func revokeCloudAuthorizationCleanupPlan(
	ctx context.Context,
	plan cloudAuthorizationCleanupPlan,
) error {
	if plan.revocationCheckpointed {
		return nil
	}
	revoked := make([]OAuthSecretEnvelope, 0, 3)
	for _, envelope := range plan.envelopes() {
		if cloudAuthorizationGrantAlreadyRevoked(
			revoked,
			envelope,
		) {
			continue
		}
		if err := revokeAndVerifyCloudAuthorizationForCLI(
			ctx,
			envelope,
		); err != nil {
			return err
		}
		revoked = append(revoked, envelope)
	}
	return nil
}

func (plan cloudAuthorizationCleanupPlan) validateRevocationCheckpoint(
	checkpoint *cloudAuthorizationRevocationCheckpoint,
) error {
	if err := validateCloudAuthorizationRevocationCheckpoint(
		checkpoint,
	); err != nil {
		return newCloudError(
			CloudErrSecretCorrupt,
			"validate authorization revocation checkpoint",
			err,
		)
	}
	for _, candidate := range []struct {
		name   string
		exists bool
		value  OAuthSecretEnvelope
		saved  *cloudAuthorizationSlotCheckpoint
	}{
		{"current", plan.hasCurrent, plan.current, checkpoint.Current},
		{"pending", plan.hasPending, plan.pending, checkpoint.Pending},
		{"retiring", plan.hasRetiring, plan.retiring, checkpoint.Retiring},
	} {
		if !candidate.exists {
			continue
		}
		if candidate.saved == nil ||
			!candidate.saved.matches(candidate.value) {
			return newCloudError(
				CloudErrIdentityMismatch,
				"validate checkpointed "+candidate.name+
					" Cloud authorization",
				nil,
			)
		}
	}
	return nil
}

func cloudAuthorizationGrantAlreadyRevoked(
	revoked []OAuthSecretEnvelope,
	candidate OAuthSecretEnvelope,
) bool {
	for _, envelope := range revoked {
		if envelope.CanonicalOrigin == candidate.CanonicalOrigin &&
			envelope.ClientID == candidate.ClientID &&
			envelope.RefreshToken == candidate.RefreshToken {
			return true
		}
	}
	return false
}

func deleteRevokedCloudAuthorizationPlan(
	ctx context.Context,
	store OAuthSecretStore,
	plan cloudAuthorizationCleanupPlan,
) error {
	exact, err := exactOAuthCleanupStoreFor(store)
	if err != nil {
		return err
	}
	if plan.hasRetiring {
		if err := exact.DeleteRetiringExact(
			ctx,
			plan.retiring,
			SecretStoreForbidUI,
		); err != nil {
			return err
		}
	}
	if plan.hasPending {
		if err := exact.DeletePendingExact(
			ctx,
			plan.pending,
			SecretStoreForbidUI,
		); err != nil {
			return fmt.Errorf("remove pending Cloud authorization: %w", err)
		}
	}
	if plan.hasCurrent {
		if err := exact.DeleteCurrentExact(
			ctx,
			plan.current,
			SecretStoreForbidUI,
		); err != nil {
			return fmt.Errorf("remove current Cloud authorization: %w", err)
		}
	}
	return nil
}
