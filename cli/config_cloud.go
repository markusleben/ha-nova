package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type routePolicy string

const (
	routePolicyLocal     routePolicy = "local"
	routePolicyAutomatic routePolicy = "automatic"
	routePolicyCloud     routePolicy = "cloud"
)

func parseRoutePolicy(value string) (routePolicy, error) {
	policy := routePolicy(strings.ToLower(strings.TrimSpace(value)))
	switch policy {
	case routePolicyLocal, routePolicyAutomatic, routePolicyCloud:
		return policy, nil
	default:
		return "", fmt.Errorf("invalid route policy %q: use local, automatic, or cloud", value)
	}
}

// effectiveRoutePolicy keeps v1/v2 profiles local during their lazy v3
// migration. A missing value is never interpreted as permission to use Cloud.
func effectiveRoutePolicy(value routePolicy) routePolicy {
	if value == "" {
		return routePolicyLocal
	}
	return value
}

type cloudConnectionMetadata struct {
	Origin               string `json:"origin"`
	CanonicalOrigin      string `json:"canonical_origin,omitempty"`
	OAuthClientID        string `json:"oauth_client_id"`
	CredentialGeneration string `json:"credential_generation"`
	HAUserID             string `json:"ha_user_id,omitempty"`
}

type cloudLifecycleState string

const (
	cloudStateAuthorizing         cloudLifecycleState = "authorizing"
	cloudStateTokenStored         cloudLifecycleState = "token_stored"
	cloudStateCloudVerified       cloudLifecycleState = "cloud_verified"
	cloudStateDeviceBoundOrPaired cloudLifecycleState = "device_bound_or_paired"
	cloudStateRollingBack         cloudLifecycleState = "rolling_back"
	cloudStateCommitted           cloudLifecycleState = "committed"
	cloudStateRetiringPrevious    cloudLifecycleState = "retiring_previous"
	cloudStateReady               cloudLifecycleState = "ready"
)

type cloudLifecycleMetadata struct {
	State                     cloudLifecycleState              `json:"state,omitempty"`
	Current                   *cloudConnectionMetadata         `json:"current,omitempty"`
	Pending                   *cloudConnectionMetadata         `json:"pending,omitempty"`
	DeviceActivationStarted   bool                             `json:"device_activation_started,omitempty"`
	DeviceActivationDeviceID  string                           `json:"device_activation_device_id,omitempty"`
	DeviceRevocationCompleted *cloudDeviceRevocationCheckpoint `json:"device_revocation_completed,omitempty"`
	RecoveryHold              *cloudRecoveryHold               `json:"recovery_hold,omitempty"`
}

type cloudDeviceRevocationCheckpoint struct {
	CurrentDeviceID string `json:"current_device_id,omitempty"`
	PendingDeviceID string `json:"pending_device_id,omitempty"`
}

func (m *cloudLifecycleMetadata) configured() bool {
	return m != nil && m.Current != nil
}

func (m *cloudLifecycleMetadata) ready() bool {
	return m != nil && m.Current != nil && m.Pending == nil &&
		m.DeviceRevocationCompleted == nil &&
		(m.State == cloudStateReady || m.State == "")
}

func validateCloudConnectionMetadata(metadata cloudConnectionMetadata) error {
	switch {
	case strings.TrimSpace(metadata.Origin) == "":
		return errors.New("cloud connection origin is missing")
	case strings.TrimSpace(metadata.CanonicalOrigin) == "":
		return errors.New("canonical cloud origin is missing")
	case strings.TrimSpace(metadata.OAuthClientID) == "":
		return errors.New("cloud OAuth client id is missing")
	case !oauthSecretGenerationPattern.MatchString(metadata.CredentialGeneration):
		return errors.New("cloud credential generation is missing or invalid")
	}
	if _, err := parseStrictCloudOrigin(metadata.Origin); err != nil {
		return fmt.Errorf("invalid cloud origin: %w", err)
	}
	if _, err := ParseCanonicalNabuOrigin(metadata.CanonicalOrigin); err != nil {
		return fmt.Errorf("invalid canonical cloud origin: %w", err)
	}
	if err := ValidateOAuthLoopbackClientID(metadata.OAuthClientID); err != nil {
		return fmt.Errorf("invalid cloud OAuth client id: %w", err)
	}
	if metadata.HAUserID != "" && !validIdentifier(metadata.HAUserID, 256) {
		return errors.New("invalid Home Assistant user id")
	}
	return nil
}

func validateCurrentCloudConnectionMetadata(metadata cloudConnectionMetadata) error {
	if err := validateCloudConnectionMetadata(metadata); err != nil {
		return err
	}
	if !validIdentifier(metadata.HAUserID, 256) {
		return errors.New("current cloud metadata requires a valid Home Assistant user id")
	}
	return nil
}

func normalizeCloudLifecycle(metadata **cloudLifecycleMetadata) error {
	if *metadata == nil {
		return nil
	}
	value := *metadata
	if value.State == "" {
		switch {
		case value.Current != nil && value.Pending == nil:
			value.State = cloudStateReady
		case value.Current == nil &&
			value.Pending == nil &&
			!value.DeviceActivationStarted &&
			value.DeviceActivationDeviceID == "" &&
			value.DeviceRevocationCompleted == nil &&
			value.RecoveryHold == nil:
			*metadata = nil
			return nil
		default:
			return errors.New("cloud lifecycle state is missing for pending metadata")
		}
	}
	if err := validateCloudLifecycle(*value); err != nil {
		return err
	}
	return nil
}

func validateCloudLifecycle(metadata cloudLifecycleMetadata) error {
	if err := validateCloudRecoveryHold(metadata.RecoveryHold); err != nil {
		return err
	}
	if err := validateCloudDeviceActivationCheckpoint(metadata); err != nil {
		return err
	}
	if err := validateCloudDeviceRevocationCheckpoint(metadata); err != nil {
		return err
	}
	return validateCloudLifecycleSlots(metadata)
}

func validateCloudDeviceActivationCheckpoint(
	metadata cloudLifecycleMetadata,
) error {
	if metadata.DeviceActivationStarted !=
		(metadata.DeviceActivationDeviceID != "") {
		return errors.New(
			"device activation checkpoint requires both marker and device id",
		)
	}
	if metadata.DeviceActivationStarted &&
		(metadata.Pending == nil ||
			(metadata.State != cloudStateCloudVerified &&
				metadata.State != cloudStateDeviceBoundOrPaired)) {
		return errors.New(
			"device_activation_started requires pending metadata at a Cloud device activation checkpoint",
		)
	}
	if metadata.DeviceActivationStarted &&
		!validDeviceID(metadata.DeviceActivationDeviceID) {
		return errors.New("device activation checkpoint has an invalid device id")
	}
	return nil
}

func validateCloudLifecycleSlots(metadata cloudLifecycleMetadata) error {
	switch metadata.State {
	case cloudStateAuthorizing:
		// Pending non-secret metadata may be checkpointed before OAuth opens.
	case cloudStateTokenStored, cloudStateCloudVerified, cloudStateDeviceBoundOrPaired:
		if metadata.Pending == nil {
			return fmt.Errorf("%s cloud lifecycle requires pending metadata", metadata.State)
		}
	case cloudStateRollingBack:
		if metadata.Current == nil || metadata.Pending == nil {
			return errors.New("rolling_back cloud lifecycle requires current and pending metadata")
		}
	case cloudStateCommitted:
		if metadata.Current == nil || metadata.Pending == nil {
			return errors.New("committed cloud lifecycle requires current and pending metadata")
		}
		if metadata.Current.CredentialGeneration != metadata.Pending.CredentialGeneration {
			return errors.New("committed cloud lifecycle requires matching current and pending generations")
		}
		if *metadata.Current != *metadata.Pending {
			return errors.New("committed cloud lifecycle requires identical current and pending metadata")
		}
	case cloudStateRetiringPrevious, cloudStateReady:
		if metadata.Current == nil || metadata.Pending != nil {
			return fmt.Errorf("%s cloud lifecycle requires only current metadata", metadata.State)
		}
	default:
		return fmt.Errorf("invalid cloud lifecycle state %q", metadata.State)
	}
	if metadata.Current != nil {
		if err := validateCurrentCloudConnectionMetadata(*metadata.Current); err != nil {
			return fmt.Errorf("invalid current cloud metadata: %w", err)
		}
	}
	if metadata.Pending != nil {
		if err := validateCloudConnectionMetadata(*metadata.Pending); err != nil {
			return fmt.Errorf("invalid pending cloud metadata: %w", err)
		}
		if metadata.State == cloudStateCloudVerified ||
			metadata.State == cloudStateDeviceBoundOrPaired ||
			metadata.State == cloudStateRollingBack ||
			metadata.State == cloudStateCommitted {
			if err := validateCurrentCloudConnectionMetadata(*metadata.Pending); err != nil {
				return fmt.Errorf("%s cloud lifecycle requires verified pending metadata: %w", metadata.State, err)
			}
		}
	}
	return nil
}

func validateCloudDeviceRevocationCheckpoint(
	metadata cloudLifecycleMetadata,
) error {
	checkpoint := metadata.DeviceRevocationCompleted
	if checkpoint == nil {
		return nil
	}
	if checkpoint.CurrentDeviceID == "" &&
		checkpoint.PendingDeviceID == "" {
		return errors.New(
			"device revocation checkpoint requires an exact device id",
		)
	}
	if checkpoint.CurrentDeviceID != "" {
		currentIsPromotedActivation := metadata.DeviceActivationStarted &&
			metadata.State == cloudStateDeviceBoundOrPaired &&
			checkpoint.CurrentDeviceID ==
				metadata.DeviceActivationDeviceID
		if (metadata.Current == nil && !currentIsPromotedActivation) ||
			!validDeviceID(checkpoint.CurrentDeviceID) {
			return errors.New(
				"current device revocation checkpoint is invalid",
			)
		}
	}
	if checkpoint.PendingDeviceID != "" {
		if metadata.Pending == nil ||
			!metadata.DeviceActivationStarted ||
			checkpoint.PendingDeviceID != metadata.DeviceActivationDeviceID ||
			!validDeviceID(checkpoint.PendingDeviceID) {
			return errors.New(
				"pending device revocation checkpoint is invalid",
			)
		}
	}
	if metadata.DeviceActivationStarted &&
		checkpoint.PendingDeviceID == "" &&
		(metadata.State != cloudStateDeviceBoundOrPaired ||
			checkpoint.CurrentDeviceID !=
				metadata.DeviceActivationDeviceID) {
		return errors.New(
			"activated pending device is missing from revocation checkpoint",
		)
	}
	return nil
}

var profileIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func validateProfileID(value string) error {
	if !profileIDPattern.MatchString(value) {
		return fmt.Errorf("invalid profile_id %q", value)
	}
	return nil
}

var generateProfileID = func() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate profile id: %w", err)
	}
	return "profile-" + hex.EncodeToString(raw), nil
}

func ensureProfileID(cfg *runtimeConfig) error {
	if cfg.ProfileID != "" {
		return validateProfileID(cfg.ProfileID)
	}
	id, err := generateProfileID()
	if err != nil {
		return err
	}
	cfg.ProfileID = id
	return nil
}

func validateLoadedRuntimeConfig(cfg *runtimeConfig) error {
	if cfg == nil {
		return errors.New("server profile config is missing")
	}
	if err := validateClientInstallID(cfg.ClientInstallID); err != nil {
		return err
	}
	if cfg.ProfileID != "" {
		if err := validateProfileID(cfg.ProfileID); err != nil {
			return err
		}
	}
	if cfg.RelayInstanceID != "" && !validIdentifier(cfg.RelayInstanceID, 256) {
		return fmt.Errorf("invalid relay_instance_id")
	}
	policy, err := parseRoutePolicy(string(effectiveRoutePolicy(cfg.RoutePolicy)))
	if err != nil {
		return err
	}
	cfg.RoutePolicy = policy
	if policy == routePolicyAutomatic &&
		(strings.TrimSpace(cfg.RelaySecureBaseURL) == "" || strings.TrimSpace(cfg.RelaySpkiPin) == "") {
		return errors.New("automatic route policy requires a completed local device pairing")
	}
	if err := normalizeCloudLifecycle(&cfg.Cloud); err != nil {
		return err
	}
	if cfg.Cloud != nil && cfg.ProfileID == "" {
		return errors.New("cloud metadata requires a valid profile_id")
	}
	if policy != routePolicyLocal && !cfg.Cloud.configured() {
		return fmt.Errorf("%s route policy requires completed cloud metadata", policy)
	}
	if cfg.Cloud.configured() && cfg.RelayInstanceID == "" {
		return errors.New("completed cloud metadata requires relay_instance_id")
	}
	return nil
}
