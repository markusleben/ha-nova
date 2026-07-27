package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var oauthRefreshTokenDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// cloudAuthorizationRevocationCheckpoint is durable, non-secret proof that
// every native OAuth grant represented by these exact slots was remotely
// revoked before local secure-storage deletion started. A token digest binds a
// retry to the exact high-entropy token without persisting the token itself.
type cloudAuthorizationRevocationCheckpoint struct {
	Current                              *cloudAuthorizationSlotCheckpoint `json:"current,omitempty"`
	Pending                              *cloudAuthorizationSlotCheckpoint `json:"pending,omitempty"`
	Retiring                             *cloudAuthorizationSlotCheckpoint `json:"retiring,omitempty"`
	OwnerConfirmedAllRemoteAccessRevoked bool                              `json:"owner_confirmed_all_remote_access_revoked,omitempty"`
}

type cloudAuthorizationSlotCheckpoint struct {
	SchemaVersion         int              `json:"schema_version"`
	State                 OAuthSecretState `json:"state"`
	Generation            string           `json:"generation"`
	ProfileID             string           `json:"profile_id"`
	CanonicalOrigin       string           `json:"canonical_origin"`
	ClientID              string           `json:"client_id"`
	RefreshTokenDigest    string           `json:"refresh_token_sha256"`
	RefreshTokenID        string           `json:"refresh_token_id,omitempty"`
	RefreshTokenExpiresAt *time.Time       `json:"refresh_token_expires_at,omitempty"`
	HAUserID              string           `json:"ha_user_id,omitempty"`
	RelayInstanceID       string           `json:"relay_instance_id,omitempty"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
}

func (checkpoint *cloudAuthorizationRevocationCheckpoint) UnmarshalJSON(
	data []byte,
) error {
	type checkpointFields cloudAuthorizationRevocationCheckpoint
	var fields checkpointFields
	if err := decodeStrictAuthorizationCheckpointJSON(
		data,
		&fields,
	); err != nil {
		return err
	}
	parsed := cloudAuthorizationRevocationCheckpoint(fields)
	if err := validateCloudAuthorizationRevocationCheckpoint(
		&parsed,
	); err != nil {
		return err
	}
	*checkpoint = parsed
	return nil
}

func (slot *cloudAuthorizationSlotCheckpoint) UnmarshalJSON(
	data []byte,
) error {
	type slotFields cloudAuthorizationSlotCheckpoint
	var fields slotFields
	if err := decodeStrictAuthorizationCheckpointJSON(
		data,
		&fields,
	); err != nil {
		return err
	}
	parsed := cloudAuthorizationSlotCheckpoint(fields)
	if err := parsed.validate(parsed.State); err != nil {
		return err
	}
	*slot = parsed
	return nil
}

func decodeStrictAuthorizationCheckpointJSON(
	data []byte,
	target any,
) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func newCloudAuthorizationRevocationCheckpoint(
	plan cloudAuthorizationCleanupPlan,
	ownerConfirmed bool,
) *cloudAuthorizationRevocationCheckpoint {
	if !plan.hasAuthorization() && !ownerConfirmed {
		return nil
	}
	checkpoint := &cloudAuthorizationRevocationCheckpoint{
		OwnerConfirmedAllRemoteAccessRevoked: ownerConfirmed,
	}
	if plan.hasCurrent {
		slot := cloudAuthorizationCheckpointForEnvelope(plan.current)
		checkpoint.Current = &slot
	}
	if plan.hasPending {
		slot := cloudAuthorizationCheckpointForEnvelope(plan.pending)
		checkpoint.Pending = &slot
	}
	if plan.hasRetiring {
		slot := cloudAuthorizationCheckpointForEnvelope(plan.retiring)
		checkpoint.Retiring = &slot
	}
	return checkpoint
}

func cloudAuthorizationCheckpointForEnvelope(
	envelope OAuthSecretEnvelope,
) cloudAuthorizationSlotCheckpoint {
	digest := sha256.Sum256([]byte(envelope.RefreshToken))
	return cloudAuthorizationSlotCheckpoint{
		SchemaVersion:         envelope.SchemaVersion,
		State:                 envelope.State,
		Generation:            envelope.Generation,
		ProfileID:             envelope.ProfileID,
		CanonicalOrigin:       envelope.CanonicalOrigin,
		ClientID:              envelope.ClientID,
		RefreshTokenDigest:    hex.EncodeToString(digest[:]),
		RefreshTokenID:        envelope.RefreshTokenID,
		RefreshTokenExpiresAt: cloneOptionalTime(envelope.RefreshTokenExpiresAt),
		HAUserID:              envelope.HAUserID,
		RelayInstanceID:       envelope.RelayInstanceID,
		CreatedAt:             envelope.CreatedAt,
		UpdatedAt:             envelope.UpdatedAt,
	}
}

func validateCloudAuthorizationRevocationCheckpoint(
	checkpoint *cloudAuthorizationRevocationCheckpoint,
) error {
	if checkpoint == nil {
		return nil
	}
	if checkpoint.Current == nil &&
		checkpoint.Pending == nil &&
		checkpoint.Retiring == nil &&
		!checkpoint.OwnerConfirmedAllRemoteAccessRevoked {
		return errors.New(
			"authorization revocation checkpoint has no exact grant or Owner confirmation",
		)
	}
	for _, candidate := range []struct {
		name  string
		state OAuthSecretState
		slot  *cloudAuthorizationSlotCheckpoint
	}{
		{"current", OAuthSecretCurrent, checkpoint.Current},
		{"pending", OAuthSecretPending, checkpoint.Pending},
		{"retiring", OAuthSecretRetiring, checkpoint.Retiring},
	} {
		if candidate.slot == nil {
			continue
		}
		if err := candidate.slot.validate(candidate.state); err != nil {
			return fmt.Errorf(
				"invalid %s authorization revocation checkpoint: %w",
				candidate.name,
				err,
			)
		}
	}
	return nil
}

func (slot cloudAuthorizationSlotCheckpoint) validate(
	expectedState OAuthSecretState,
) error {
	if slot.State != expectedState ||
		!oauthRefreshTokenDigestPattern.MatchString(
			slot.RefreshTokenDigest,
		) {
		return errors.New("slot identity is invalid")
	}
	envelope := slot.validationEnvelope()
	return validateOAuthSecretEnvelope(
		envelope,
		expectedState != OAuthSecretPending,
	)
}

func (slot cloudAuthorizationSlotCheckpoint) validationEnvelope() OAuthSecretEnvelope {
	return OAuthSecretEnvelope{
		SchemaVersion:         slot.SchemaVersion,
		State:                 slot.State,
		Generation:            slot.Generation,
		ProfileID:             slot.ProfileID,
		CanonicalOrigin:       slot.CanonicalOrigin,
		ClientID:              slot.ClientID,
		RefreshToken:          "checkpoint-validation-token",
		RefreshTokenID:        slot.RefreshTokenID,
		RefreshTokenExpiresAt: cloneOptionalTime(slot.RefreshTokenExpiresAt),
		HAUserID:              slot.HAUserID,
		RelayInstanceID:       slot.RelayInstanceID,
		CreatedAt:             slot.CreatedAt,
		UpdatedAt:             slot.UpdatedAt,
	}
}

func (slot cloudAuthorizationSlotCheckpoint) matches(
	envelope OAuthSecretEnvelope,
) bool {
	expected := cloudAuthorizationCheckpointForEnvelope(envelope)
	return sameCloudAuthorizationSlotCheckpoint(slot, expected)
}

func sameCloudAuthorizationSlotCheckpoint(
	left cloudAuthorizationSlotCheckpoint,
	right cloudAuthorizationSlotCheckpoint,
) bool {
	return left.SchemaVersion == right.SchemaVersion &&
		left.State == right.State &&
		left.Generation == right.Generation &&
		left.ProfileID == right.ProfileID &&
		left.CanonicalOrigin == right.CanonicalOrigin &&
		left.ClientID == right.ClientID &&
		subtle.ConstantTimeCompare(
			[]byte(left.RefreshTokenDigest),
			[]byte(right.RefreshTokenDigest),
		) == 1 &&
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

func cloneOptionalTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
