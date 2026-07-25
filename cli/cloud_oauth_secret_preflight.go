package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"io"
)

const oauthSecretPreflightServicePrefix = "ha-nova.oauth.home-assistant-cloud.preflight."

type oauthSecretStorePreflighter interface {
	Preflight(context.Context, SecretStoreUIPolicy) error
}

// NewOAuthSecretGeneration reserves a cryptographically random, non-secret
// generation that config can durably checkpoint before OAuth opens.
func NewOAuthSecretGeneration() (string, error) {
	return newOAuthSecretGeneration(rand.Reader)
}

func newOAuthSecretGeneration(random io.Reader) (string, error) {
	generation := make([]byte, 16)
	if _, err := io.ReadFull(random, generation); err != nil {
		return "", newCloudError(
			CloudErrSecretStore,
			"create OAuth secret generation",
			err,
		)
	}
	return hex.EncodeToString(generation), nil
}

// PreflightOAuthSecretStore performs a real write/read/delete probe. This is
// the setup flow's only operation that may allow native credential-store UI.
func PreflightOAuthSecretStore(
	ctx context.Context,
	store OAuthSecretStore,
	ui SecretStoreUIPolicy,
) error {
	preflighter, ok := store.(oauthSecretStorePreflighter)
	if !ok || preflighter == nil {
		return newCloudError(
			CloudErrUnsupportedPlatform,
			"preflight OAuth secret store",
			nil,
		)
	}
	return preflighter.Preflight(ctx, ui)
}

func (s *KeyringOAuthSecretStore) Preflight(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) error {
	if s == nil || s.backend == nil {
		return newCloudError(CloudErrInvalidInput, "preflight OAuth secret store", nil)
	}
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	generation, err := newOAuthSecretGeneration(s.random)
	if err != nil {
		return err
	}
	service := oauthSecretPreflightServicePrefix + generation
	value := "probe-" + generation
	if err := s.backend.Set(ctx, service, s.account, value, ui); err != nil {
		return wrapOAuthSecretBackendError("preflight OAuth secret store", err)
	}
	// At most the first operation may open native secure-storage UI. If the
	// provider relocks during the probe, fail closed instead of prompting again.
	followupUI := ui
	if ui == SecretStoreAllowUI {
		followupUI = SecretStoreForbidUI
	}
	stored, exists, getErr := s.backend.Get(ctx, service, s.account, followupUI)
	deleteErr := s.backend.Delete(ctx, service, s.account, followupUI)
	if getErr != nil {
		return wrapOAuthSecretBackendError("preflight OAuth secret store", getErr)
	}
	if deleteErr != nil {
		return wrapOAuthSecretBackendError("preflight OAuth secret store", deleteErr)
	}
	if !exists || len(stored) != len(value) ||
		subtle.ConstantTimeCompare([]byte(stored), []byte(value)) != 1 {
		return newCloudError(
			CloudErrSecretStore,
			"preflight OAuth secret store",
			nil,
		)
	}
	return nil
}
