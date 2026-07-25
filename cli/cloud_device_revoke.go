package main

import (
	"context"
	"fmt"
	"strings"
)

var revokeRemoteCloudDeviceForCLI = revokeRemoteCloudDevice

type remoteCloudDeviceCredential struct {
	value           string
	service         string
	relayInstanceID string
}

type remoteCloudDeviceRevocationTarget struct {
	credential remoteCloudDeviceCredential
	config     runtimeConfig
}

func currentCloudDeviceRevocationConfig(
	cfg runtimeConfig,
) (runtimeConfig, error) {
	if cfg.Cloud == nil || cfg.Cloud.Current == nil {
		return runtimeConfig{}, newCloudError(
			CloudErrSecretCorrupt,
			"select current Cloud authorization for device revocation",
			nil,
		)
	}
	currentConfig := cfg
	lifecycle := *cfg.Cloud
	lifecycle.State = cloudStateReady
	lifecycle.Pending = nil
	lifecycle.DeviceActivationStarted = false
	lifecycle.DeviceActivationDeviceID = ""
	lifecycle.DeviceRevocationCompleted = nil
	currentConfig.Cloud = &lifecycle
	return currentConfig, nil
}

func selectRemoteCloudDeviceCredential(
	ctx context.Context,
	cfg runtimeConfig,
	profileName string,
) (remoteCloudDeviceCredential, bool, error) {
	if cloudLifecycleMayHaveActivatedPendingDevice(cfg.Cloud) {
		selected, exists, err := selectPendingRemoteCloudDeviceCredential(
			ctx,
			cfg,
			profileName,
		)
		if err != nil || exists {
			return selected, exists, err
		}
	}
	service := deviceCredentialServiceForProfile(profileName)
	credential, exists, err := readCredentialSlotWithPolicy(
		ctx,
		service,
		SecretStoreForbidUI,
	)
	return remoteCloudDeviceCredential{
		value:   credential,
		service: service,
	}, exists, err
}

func cloudLifecycleMayHaveActivatedPendingDevice(
	lifecycle *cloudLifecycleMetadata,
) bool {
	return lifecycle != nil && lifecycle.DeviceActivationStarted
}

func cloudDeviceActivationUncertain(
	lifecycle *cloudLifecycleMetadata,
) bool {
	return lifecycle != nil &&
		lifecycle.State == cloudStateCloudVerified
}

func isRemoteOnlyCloudProfile(cfg runtimeConfig) (bool, error) {
	hasSecureURL := strings.TrimSpace(cfg.RelaySecureBaseURL) != ""
	hasPin := strings.TrimSpace(cfg.RelaySpkiPin) != ""
	if hasSecureURL != hasPin {
		return false, newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud device provenance",
			nil,
		)
	}
	return !hasSecureURL, nil
}

func cloudDeviceCredentialLabel(
	profileName string,
	remoteOnly bool,
) string {
	kind := "Cloud device credential"
	if remoteOnly {
		kind = "Cloud-only device credential"
	}
	if profileName == "" || profileName == defaultServerProfileName {
		return kind + " (secure storage)"
	}
	return fmt.Sprintf(
		"%s (secure storage, server %q)",
		kind,
		profileName,
	)
}

func revokeRemoteCloudDevice(
	ctx context.Context,
	cfg runtimeConfig,
	store OAuthSecretStore,
	deviceCredential string,
) error {
	if store == nil || cfg.Cloud == nil {
		return newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud device provenance",
			nil,
		)
	}
	if err := validateProfileID(cfg.ProfileID); err != nil {
		return newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud-only profile identity",
			err,
		)
	}
	if !validIdentifier(cfg.RelayInstanceID, 256) {
		return newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud-only Relay identity",
			nil,
		)
	}
	metadata, envelope, err := cloudDeviceRevocationAuthorization(
		ctx,
		cfg,
		store,
	)
	if err != nil {
		return err
	}
	if err := validateCurrentCloudConnectionMetadata(metadata); err != nil {
		return newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud-only authorization metadata",
			err,
		)
	}
	if err := matchCloudConfigToSecret(cfg, metadata, envelope); err != nil {
		return err
	}
	origin, err := cloudOriginFromMetadata(metadata)
	if err != nil {
		return err
	}
	oauth, err := NewHAOAuthClient(
		envelope.CanonicalOrigin,
		cloudHTTPClientForCLI,
	)
	if err != nil {
		return err
	}
	token, err := oauth.Refresh(
		ctx,
		envelope.RefreshToken,
		envelope.ClientID,
	)
	if err != nil {
		return err
	}
	session, err := verifyCloudAccess(
		ctx,
		envelope,
		token.AccessToken,
		origin,
		cloudHTTPClientForCLI,
	)
	if err != nil {
		return err
	}
	return revokeRemoteCloudDeviceWithVerifiedSession(
		ctx,
		cfg,
		metadata,
		deviceCredential,
		session,
	)
}

func cloudDeviceRevocationAuthorization(
	ctx context.Context,
	cfg runtimeConfig,
	store OAuthSecretStore,
) (cloudConnectionMetadata, OAuthSecretEnvelope, error) {
	var (
		metadata *cloudConnectionMetadata
		envelope OAuthSecretEnvelope
		exists   bool
		err      error
	)
	if cloudLifecycleMayHaveActivatedPendingDevice(cfg.Cloud) {
		metadata = cfg.Cloud.Pending
		if metadata == nil {
			return cloudConnectionMetadata{}, OAuthSecretEnvelope{},
				newCloudError(
					CloudErrSecretCorrupt,
					"validate bound Cloud authorization metadata",
					nil,
				)
		}
		origin, originErr := cloudOriginFromMetadata(*metadata)
		if originErr != nil {
			return cloudConnectionMetadata{}, OAuthSecretEnvelope{},
				originErr
		}
		envelope, _, err = resumableCloudEnvelope(
			ctx,
			store,
			cfg,
			origin,
			SecretStoreForbidUI,
		)
		exists = envelope.Generation != ""
	} else {
		if cfg.Cloud != nil {
			metadata = cfg.Cloud.Current
		}
		if metadata == nil {
			return cloudConnectionMetadata{}, OAuthSecretEnvelope{},
				newCloudError(
					CloudErrSecretCorrupt,
					"validate current Cloud authorization metadata",
					nil,
				)
		}
		envelope, exists, err = store.LoadCurrent(
			ctx,
			SecretStoreForbidUI,
		)
	}
	if err != nil {
		return cloudConnectionMetadata{}, OAuthSecretEnvelope{}, err
	}
	if !exists {
		return cloudConnectionMetadata{}, OAuthSecretEnvelope{},
			newCloudError(
				CloudErrSecretNotFound,
				"load Cloud authorization for device revocation",
				nil,
			)
	}
	return *metadata, envelope, nil
}

// This is the only production disclosure point for an active Cloud-only device
// bearer. Every identity established without that bearer is checked again
// immediately before the request is dispatched.
func revokeRemoteCloudDeviceWithVerifiedSession(
	ctx context.Context,
	cfg runtimeConfig,
	metadata cloudConnectionMetadata,
	deviceCredential string,
	session cloudVerifiedSession,
) error {
	var configuredMetadata *cloudConnectionMetadata
	if cfg.Cloud != nil {
		configuredMetadata = cfg.Cloud.Current
		if cloudLifecycleMayHaveActivatedPendingDevice(cfg.Cloud) {
			configuredMetadata = cfg.Cloud.Pending
		}
	}
	if configuredMetadata == nil ||
		*configuredMetadata != metadata ||
		parseDeviceCredential(deviceCredential) == nil {
		return newCloudError(
			CloudErrSecretCorrupt,
			"validate Cloud device revocation provenance",
			nil,
		)
	}
	if session.Ingress == nil ||
		session.User.ID != metadata.HAUserID ||
		session.Envelope.HAUserID != metadata.HAUserID {
		return newCloudError(
			CloudErrIdentityMismatch,
			"verify Cloud device revocation user",
			nil,
		)
	}
	if session.Relay.RelayInstanceID != cfg.RelayInstanceID ||
		session.Envelope.RelayInstanceID != cfg.RelayInstanceID {
		return newCloudError(
			CloudErrRelayInstance,
			"verify Cloud device revocation Relay",
			nil,
		)
	}
	if session.Envelope.ProfileID != cfg.ProfileID ||
		session.Envelope.CanonicalOrigin != metadata.CanonicalOrigin ||
		session.Envelope.ClientID != metadata.OAuthClientID ||
		session.Envelope.Generation != metadata.CredentialGeneration {
		return newCloudError(
			CloudErrIdentityMismatch,
			"verify Cloud device revocation authorization",
			nil,
		)
	}
	_, err := session.Ingress.RevokeDevice(
		ctx,
		deviceCredential,
		cfg.RelayInstanceID,
	)
	return err
}
