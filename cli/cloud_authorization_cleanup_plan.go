package main

import (
	"context"
	"errors"
	"fmt"
)

var errCloudAuthorizationCleanupUnverifiable = errors.New(
	"Cloud authorization cleanup is not locally verifiable",
)

// cloudAuthorizationCleanupPlan is a complete, immutable view of every native
// OAuth slot. Cleanup must build and validate this plan before revoking any
// device or authorization so a missing or corrupt later slot cannot strand a
// potentially live grant after partial teardown.
type cloudAuthorizationCleanupPlan struct {
	current     OAuthSecretEnvelope
	hasCurrent  bool
	pending     OAuthSecretEnvelope
	hasPending  bool
	retiring    OAuthSecretEnvelope
	hasRetiring bool
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
	if err := plan.validateConfiguredGrants(cfg); err != nil {
		return plan, err
	}
	return plan, nil
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

func unverifiableCloudAuthorizationCleanup(cause error) error {
	return &cloudProblem{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
		Detail: "potential HA NOVA remote access cannot be verified because " +
			"its native authorization is missing or inconsistent; as a Home " +
			"Assistant Owner, revoke this computer under NOVA Devices and " +
			"revoke HA NOVA sessions for every user before using the " +
			"profile-bound recovery command",
		Cause: errors.Join(
			errCloudAuthorizationCleanupUnverifiable,
			cause,
		),
	}
}

func revokeCloudAuthorizationCleanupPlan(
	ctx context.Context,
	store OAuthSecretStore,
	plan cloudAuthorizationCleanupPlan,
) error {
	revoked := make([]OAuthSecretEnvelope, 0, 3)
	if plan.hasRetiring {
		if err := store.RevokeRetiring(
			ctx,
			plan.retiring.Generation,
			SecretStoreForbidUI,
			revokeAndVerifyCloudAuthorizationForCLI,
		); err != nil {
			return err
		}
		revoked = append(revoked, plan.retiring)
	}
	for _, envelope := range []OAuthSecretEnvelope{
		plan.pending,
		plan.current,
	} {
		if envelope.Generation == "" {
			continue
		}
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

func cloudAuthorizationGrantAlreadyRevoked(
	revoked []OAuthSecretEnvelope,
	candidate OAuthSecretEnvelope,
) bool {
	for _, envelope := range revoked {
		if envelope.ClientID == candidate.ClientID &&
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
	if plan.hasPending {
		if err := store.DeletePending(
			ctx,
			plan.pending.Generation,
			SecretStoreForbidUI,
		); err != nil {
			return fmt.Errorf("remove pending Cloud authorization: %w", err)
		}
	}
	if plan.hasCurrent {
		if err := store.DeleteCurrent(
			ctx,
			SecretStoreForbidUI,
		); err != nil {
			return fmt.Errorf("remove current Cloud authorization: %w", err)
		}
	}
	return nil
}
