package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var oauthSecretGenerationPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func validateOAuthSecretEnvelope(envelope OAuthSecretEnvelope, requireComplete bool) error {
	if envelope.SchemaVersion != oauthSecretSchema ||
		!oauthSecretGenerationPattern.MatchString(envelope.Generation) ||
		envelope.CreatedAt.IsZero() || envelope.UpdatedAt.IsZero() ||
		envelope.UpdatedAt.Before(envelope.CreatedAt) {
		return newCloudError(CloudErrSecretCorrupt, "validate OAuth secret", nil)
	}
	if envelope.State != OAuthSecretCurrent &&
		envelope.State != OAuthSecretPending &&
		envelope.State != OAuthSecretRetiring {
		return newCloudError(CloudErrSecretCorrupt, "validate OAuth secret", nil)
	}
	if !oauthSecretAccountPattern.MatchString(envelope.ProfileID) {
		return newCloudError(CloudErrSecretCorrupt, "validate OAuth secret", nil)
	}
	if _, err := ParseCanonicalNabuOrigin(envelope.CanonicalOrigin); err != nil {
		return newCloudError(CloudErrSecretCorrupt, "validate OAuth secret", err)
	}
	if err := ValidateOAuthLoopbackClientID(envelope.ClientID); err != nil {
		return newCloudError(CloudErrSecretCorrupt, "validate OAuth secret", err)
	}
	if !validSecretText(envelope.RefreshToken, 2048) {
		return newCloudError(CloudErrSecretCorrupt, "validate OAuth secret", nil)
	}
	if (envelope.RefreshTokenID != "" &&
		!validIdentifier(envelope.RefreshTokenID, 256)) ||
		(envelope.HAUserID != "" && !validIdentifier(envelope.HAUserID, 256)) ||
		(envelope.RelayInstanceID != "" &&
			!validIdentifier(envelope.RelayInstanceID, 256)) ||
		(envelope.RefreshTokenExpiresAt != nil &&
			envelope.RefreshTokenExpiresAt.IsZero()) {
		return newCloudError(CloudErrSecretCorrupt, "validate OAuth secret", nil)
	}
	if requireComplete {
		if !validIdentifier(envelope.RefreshTokenID, 256) ||
			!validIdentifier(envelope.HAUserID, 256) ||
			!validIdentifier(envelope.RelayInstanceID, 256) ||
			envelope.RefreshTokenExpiresAt == nil {
			return newCloudError(CloudErrSecretCorrupt, "validate OAuth secret", nil)
		}
	}
	return nil
}

func validateSecretUIPolicy(ui SecretStoreUIPolicy) error {
	if ui != SecretStoreAllowUI && ui != SecretStoreForbidUI {
		return newCloudError(CloudErrInvalidInput, "access OAuth secret store", nil)
	}
	return nil
}

func validateOAuthSecretBackendKey(
	ctx context.Context,
	service, account string,
	ui SecretStoreUIPolicy,
) error {
	if err := validateSecretUIPolicy(ui); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return newCloudError(CloudErrTimeout, "access OAuth secret store", err)
	}
	if !oauthSecretAccountPattern.MatchString(account) ||
		!validOAuthSecretBackendService(service) {
		return newCloudError(
			CloudErrInvalidInput,
			"validate OAuth secret key",
			nil,
		)
	}
	return nil
}

func validOAuthSecretBackendService(service string) bool {
	switch service {
	case oauthSecretCurrentService,
		oauthSecretPendingService,
		oauthSecretRetiringService:
		return true
	}
	generation := strings.TrimPrefix(service, oauthSecretPreflightServicePrefix)
	return generation != service &&
		oauthSecretGenerationPattern.MatchString(generation)
}

func validSecretText(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value &&
		!strings.ContainsAny(value, "\x00\r\n")
}

func validIdentifier(value string, max int) bool {
	return validSecretText(value, max)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func wrapOAuthSecretBackendError(op string, err error) error {
	if err == nil {
		return nil
	}
	var cloudErr *CloudError
	if errors.As(err, &cloudErr) {
		return err
	}
	return newCloudError(CloudErrSecretStore, op, err)
}
