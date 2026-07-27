package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"regexp"
	"time"
)

const (
	oauthSecretSchema          = 1
	oauthSecretMaxEncodedSize  = 2300
	oauthSecretCurrentService  = "ha-nova.oauth.home-assistant-cloud.current"
	oauthSecretPendingService  = "ha-nova.oauth.home-assistant-cloud.pending"
	oauthSecretRetiringService = "ha-nova.oauth.home-assistant-cloud.retiring"
)

type SecretStoreUIPolicy string

const (
	SecretStoreAllowUI  SecretStoreUIPolicy = "allow_ui"
	SecretStoreForbidUI SecretStoreUIPolicy = "forbid_ui"
)

type OAuthSecretState string

const (
	OAuthSecretCurrent  OAuthSecretState = "current"
	OAuthSecretPending  OAuthSecretState = "pending"
	OAuthSecretRetiring OAuthSecretState = "retiring"
)

type OAuthSecretEnvelope struct {
	SchemaVersion         int              `json:"schema_version"`
	State                 OAuthSecretState `json:"state"`
	Generation            string           `json:"generation"`
	ProfileID             string           `json:"profile_id"`
	CanonicalOrigin       string           `json:"canonical_origin"`
	ClientID              string           `json:"client_id"`
	RefreshToken          string           `json:"refresh_token"`
	RefreshTokenID        string           `json:"refresh_token_id,omitempty"`
	RefreshTokenExpiresAt *time.Time       `json:"refresh_token_expires_at,omitempty"`
	HAUserID              string           `json:"ha_user_id,omitempty"`
	RelayInstanceID       string           `json:"relay_instance_id,omitempty"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

// OAuthSecretBackend is deliberately narrower than the existing Relay/device
// secret router. Implementations must use only a native OS credential store and
// must honor ForbidUI without falling back to files, environment, or argv.
type OAuthSecretBackend interface {
	Get(ctx context.Context, service, account string, ui SecretStoreUIPolicy) (string, bool, error)
	Set(ctx context.Context, service, account, value string, ui SecretStoreUIPolicy) error
	Delete(ctx context.Context, service, account string, ui SecretStoreUIPolicy) error
}

type OAuthSecretStore interface {
	LoadCurrent(context.Context, SecretStoreUIPolicy) (OAuthSecretEnvelope, bool, error)
	LoadPending(context.Context, SecretStoreUIPolicy) (OAuthSecretEnvelope, bool, error)
	LoadRetiring(context.Context, SecretStoreUIPolicy) (OAuthSecretEnvelope, bool, error)
	CreatePending(context.Context, OAuthSecretEnvelope, SecretStoreUIPolicy) (OAuthSecretEnvelope, error)
	UpdatePending(context.Context, OAuthSecretEnvelope, string, SecretStoreUIPolicy) error
	UpdateCurrent(context.Context, OAuthSecretEnvelope, string, SecretStoreUIPolicy) error
	PromotePending(context.Context, string, SecretStoreUIPolicy) (OAuthSecretEnvelope, error)
	RevokeRetiring(context.Context, string, SecretStoreUIPolicy, OAuthSecretRevoker) error
	DeleteCurrent(context.Context, SecretStoreUIPolicy) error
	DeletePending(context.Context, string, SecretStoreUIPolicy) error
}

type OAuthSecretRevoker func(context.Context, OAuthSecretEnvelope) error

type KeyringOAuthSecretStore struct {
	backend OAuthSecretBackend
	account string
	now     func() time.Time
	random  io.Reader
}

var oauthSecretAccountPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func NewOAuthSecretStore(backend OAuthSecretBackend, account string) (*KeyringOAuthSecretStore, error) {
	if backend == nil {
		return nil, newCloudError(CloudErrInvalidInput, "initialize OAuth secret store", nil)
	}
	if !oauthSecretAccountPattern.MatchString(account) {
		return nil, newCloudError(CloudErrInvalidInput, "initialize OAuth secret store", nil)
	}
	return &KeyringOAuthSecretStore{
		backend: backend,
		account: account,
		now:     time.Now,
		random:  rand.Reader,
	}, nil
}

func NewSystemOAuthSecretStore(account string) (*KeyringOAuthSecretStore, error) {
	backend, err := newNativeOAuthSecretBackend()
	if err != nil {
		return nil, err
	}
	return NewOAuthSecretStore(backend, account)
}

func (s *KeyringOAuthSecretStore) LoadCurrent(ctx context.Context, ui SecretStoreUIPolicy) (OAuthSecretEnvelope, bool, error) {
	return s.load(ctx, oauthSecretCurrentService, OAuthSecretCurrent, ui)
}

func (s *KeyringOAuthSecretStore) LoadPending(ctx context.Context, ui SecretStoreUIPolicy) (OAuthSecretEnvelope, bool, error) {
	return s.load(ctx, oauthSecretPendingService, OAuthSecretPending, ui)
}

func (s *KeyringOAuthSecretStore) LoadRetiring(ctx context.Context, ui SecretStoreUIPolicy) (OAuthSecretEnvelope, bool, error) {
	return s.load(ctx, oauthSecretRetiringService, OAuthSecretRetiring, ui)
}

func (s *KeyringOAuthSecretStore) CreatePending(ctx context.Context, envelope OAuthSecretEnvelope, ui SecretStoreUIPolicy) (OAuthSecretEnvelope, error) {
	if err := validateSecretUIPolicy(ui); err != nil {
		return OAuthSecretEnvelope{}, err
	}
	if envelope.Generation == "" {
		generation, err := newOAuthSecretGeneration(s.random)
		if err != nil {
			return OAuthSecretEnvelope{}, err
		}
		envelope.Generation = generation
	} else if !oauthSecretGenerationPattern.MatchString(envelope.Generation) {
		return OAuthSecretEnvelope{}, newCloudError(
			CloudErrInvalidInput,
			"create pending OAuth secret",
			nil,
		)
	}
	now := s.now().UTC()
	envelope.SchemaVersion = oauthSecretSchema
	envelope.State = OAuthSecretPending
	envelope.ProfileID = s.account
	envelope.CreatedAt = now
	envelope.UpdatedAt = now
	if err := validateOAuthSecretEnvelope(envelope, false); err != nil {
		return OAuthSecretEnvelope{}, err
	}
	if _, exists, err := s.LoadPending(ctx, ui); err != nil {
		return OAuthSecretEnvelope{}, err
	} else if exists {
		return OAuthSecretEnvelope{}, newCloudError(CloudErrSecretConflict, "create pending OAuth secret", nil)
	}
	if _, exists, err := s.LoadRetiring(ctx, ui); err != nil {
		return OAuthSecretEnvelope{}, err
	} else if exists {
		return OAuthSecretEnvelope{}, newCloudError(CloudErrSecretConflict, "create pending OAuth secret", nil)
	}
	if err := s.write(ctx, oauthSecretPendingService, envelope, ui); err != nil {
		return OAuthSecretEnvelope{}, err
	}
	return envelope, nil
}

func (s *KeyringOAuthSecretStore) UpdatePending(ctx context.Context, envelope OAuthSecretEnvelope, expectedGeneration string, ui SecretStoreUIPolicy) error {
	pending, exists, err := s.LoadPending(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return newCloudError(CloudErrSecretNotFound, "update pending OAuth secret", nil)
	}
	if expectedGeneration == "" || pending.Generation != expectedGeneration {
		return newCloudError(CloudErrSecretConflict, "update pending OAuth secret", nil)
	}
	envelope.SchemaVersion = oauthSecretSchema
	envelope.State = OAuthSecretPending
	envelope.Generation = pending.Generation
	envelope.ProfileID = s.account
	envelope.CreatedAt = pending.CreatedAt
	envelope.UpdatedAt = s.now().UTC()
	if err := validateOAuthSecretEnvelope(envelope, false); err != nil {
		return err
	}
	return s.write(ctx, oauthSecretPendingService, envelope, ui)
}

func (s *KeyringOAuthSecretStore) UpdateCurrent(
	ctx context.Context,
	envelope OAuthSecretEnvelope,
	expectedGeneration string,
	ui SecretStoreUIPolicy,
) error {
	current, exists, err := s.LoadCurrent(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return newCloudError(CloudErrSecretNotFound, "update current OAuth secret", nil)
	}
	if expectedGeneration == "" || current.Generation != expectedGeneration {
		return newCloudError(CloudErrSecretConflict, "update current OAuth secret", nil)
	}
	// Refresh verification may advance expiry and other verified metadata, but
	// cannot rotate the actual credential under the same generation.
	if envelope.RefreshToken != current.RefreshToken ||
		envelope.CanonicalOrigin != current.CanonicalOrigin ||
		envelope.ClientID != current.ClientID ||
		envelope.RefreshTokenID != current.RefreshTokenID ||
		envelope.HAUserID != current.HAUserID ||
		envelope.RelayInstanceID != current.RelayInstanceID {
		return newCloudError(CloudErrSecretConflict, "update current OAuth secret", nil)
	}
	envelope.SchemaVersion = oauthSecretSchema
	envelope.State = OAuthSecretCurrent
	envelope.Generation = current.Generation
	envelope.ProfileID = s.account
	envelope.CreatedAt = current.CreatedAt
	envelope.UpdatedAt = s.now().UTC()
	if err := validateOAuthSecretEnvelope(envelope, true); err != nil {
		return err
	}
	return s.write(ctx, oauthSecretCurrentService, envelope, ui)
}

func (s *KeyringOAuthSecretStore) PromotePending(ctx context.Context, expectedGeneration string, ui SecretStoreUIPolicy) (OAuthSecretEnvelope, error) {
	pending, exists, err := s.LoadPending(ctx, ui)
	if err != nil {
		return OAuthSecretEnvelope{}, err
	}
	if !exists {
		return OAuthSecretEnvelope{}, newCloudError(CloudErrSecretNotFound, "promote pending OAuth secret", nil)
	}
	if expectedGeneration == "" || pending.Generation != expectedGeneration {
		return OAuthSecretEnvelope{}, newCloudError(CloudErrSecretConflict, "promote pending OAuth secret", nil)
	}
	current, hasCurrent, err := s.LoadCurrent(ctx, ui)
	if err != nil {
		return OAuthSecretEnvelope{}, err
	}
	retiring, hasRetiring, err := s.LoadRetiring(ctx, ui)
	if err != nil {
		return OAuthSecretEnvelope{}, err
	}
	// Resume after current was written but pending deletion failed.
	if hasCurrent && current.Generation == pending.Generation {
		if err := s.backend.Delete(ctx, oauthSecretPendingService, s.account, ui); err != nil {
			return OAuthSecretEnvelope{}, wrapOAuthSecretBackendError("clear promoted OAuth secret", err)
		}
		return current, nil
	}
	if hasRetiring && (!hasCurrent || retiring.Generation != current.Generation) {
		return OAuthSecretEnvelope{}, newCloudError(CloudErrSecretConflict, "promote pending OAuth secret", nil)
	}
	if hasCurrent {
		// Preserve the working current before overwriting it. If this write or any
		// later step fails, a retry sees the same generation and resumes safely.
		if !hasRetiring {
			retiring = current
			retiring.State = OAuthSecretRetiring
			retiring.UpdatedAt = s.now().UTC()
			if err := s.write(ctx, oauthSecretRetiringService, retiring, ui); err != nil {
				return OAuthSecretEnvelope{}, err
			}
		}
	}
	pending.State = OAuthSecretCurrent
	pending.UpdatedAt = s.now().UTC()
	if err := validateOAuthSecretEnvelope(pending, true); err != nil {
		return OAuthSecretEnvelope{}, err
	}
	// Current is written first. A crash can leave an inert duplicate pending
	// envelope, but never a missing current credential after successful promotion.
	if err := s.write(ctx, oauthSecretCurrentService, pending, ui); err != nil {
		return OAuthSecretEnvelope{}, err
	}
	if err := s.backend.Delete(ctx, oauthSecretPendingService, s.account, ui); err != nil {
		return OAuthSecretEnvelope{}, wrapOAuthSecretBackendError("clear promoted OAuth secret", err)
	}
	return pending, nil
}

func (s *KeyringOAuthSecretStore) RevokeRetiring(
	ctx context.Context,
	expectedGeneration string,
	ui SecretStoreUIPolicy,
	revoke OAuthSecretRevoker,
) error {
	if revoke == nil {
		return newCloudError(CloudErrInvalidInput, "revoke retiring OAuth secret", nil)
	}
	retiring, exists, err := s.LoadRetiring(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if expectedGeneration == "" || retiring.Generation != expectedGeneration {
		return newCloudError(CloudErrSecretConflict, "revoke retiring OAuth secret", nil)
	}
	if err := revoke(ctx, retiring); err != nil {
		return err
	}
	if err := s.backend.Delete(ctx, oauthSecretRetiringService, s.account, ui); err != nil {
		return wrapOAuthSecretBackendError("clear revoked OAuth secret", err)
	}
	return nil
}

func (s *KeyringOAuthSecretStore) DeleteCurrent(ctx context.Context, ui SecretStoreUIPolicy) error {
	return s.delete(ctx, oauthSecretCurrentService, "delete current OAuth secret", ui)
}

func (s *KeyringOAuthSecretStore) DeletePending(ctx context.Context, expectedGeneration string, ui SecretStoreUIPolicy) error {
	pending, exists, err := s.LoadPending(ctx, ui)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if expectedGeneration == "" || pending.Generation != expectedGeneration {
		return newCloudError(CloudErrSecretConflict, "delete pending OAuth secret", nil)
	}
	return s.delete(ctx, oauthSecretPendingService, "delete pending OAuth secret", ui)
}

func (s *KeyringOAuthSecretStore) load(ctx context.Context, service string, state OAuthSecretState, ui SecretStoreUIPolicy) (OAuthSecretEnvelope, bool, error) {
	if err := validateSecretUIPolicy(ui); err != nil {
		return OAuthSecretEnvelope{}, false, err
	}
	value, exists, err := s.backend.Get(ctx, service, s.account, ui)
	if err != nil {
		return OAuthSecretEnvelope{}, false, wrapOAuthSecretBackendError("read OAuth secret", err)
	}
	if !exists {
		return OAuthSecretEnvelope{}, false, nil
	}
	if len(value) == 0 || len(value) > oauthSecretMaxEncodedSize {
		return OAuthSecretEnvelope{}, false, newCloudError(CloudErrSecretCorrupt, "read OAuth secret", nil)
	}
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.DisallowUnknownFields()
	var envelope OAuthSecretEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return OAuthSecretEnvelope{}, false, newCloudError(CloudErrSecretCorrupt, "read OAuth secret", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return OAuthSecretEnvelope{}, false, newCloudError(CloudErrSecretCorrupt, "read OAuth secret", err)
	}
	if envelope.State != state {
		return OAuthSecretEnvelope{}, false, newCloudError(CloudErrSecretCorrupt, "read OAuth secret", nil)
	}
	requireComplete := state == OAuthSecretCurrent || state == OAuthSecretRetiring
	if err := validateOAuthSecretEnvelope(envelope, requireComplete); err != nil {
		return OAuthSecretEnvelope{}, false, newCloudError(CloudErrSecretCorrupt, "read OAuth secret", err)
	}
	if envelope.ProfileID != s.account {
		return OAuthSecretEnvelope{}, false, newCloudError(CloudErrSecretCorrupt, "read OAuth secret", nil)
	}
	return envelope, true, nil
}

func (s *KeyringOAuthSecretStore) write(ctx context.Context, service string, envelope OAuthSecretEnvelope, ui SecretStoreUIPolicy) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if envelope.ProfileID != s.account {
		return newCloudError(CloudErrSecretCorrupt, "encode OAuth secret", nil)
	}
	data, err := json.Marshal(envelope)
	if err != nil || len(data) > oauthSecretMaxEncodedSize {
		return newCloudError(CloudErrSecretCorrupt, "encode OAuth secret", err)
	}
	if err := s.backend.Set(ctx, service, s.account, string(data), ui); err != nil {
		return wrapOAuthSecretBackendError("write OAuth secret", err)
	}
	return nil
}

func (s *KeyringOAuthSecretStore) delete(ctx context.Context, service, op string, ui SecretStoreUIPolicy) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if err := s.backend.Delete(ctx, service, s.account, ui); err != nil {
		return wrapOAuthSecretBackendError(op, err)
	}
	return nil
}
