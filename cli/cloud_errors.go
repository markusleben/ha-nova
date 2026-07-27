package main

import (
	"errors"
	"fmt"
)

// CloudErrorCode is stable protocol-facing error identity. Error strings never
// include response bodies, callback queries, cookies, or tokens.
type CloudErrorCode string

const (
	CloudErrInvalidInput          CloudErrorCode = "INVALID_INPUT"
	CloudErrUnsupportedPlatform   CloudErrorCode = "UNSUPPORTED_PLATFORM"
	CloudErrSecretNotFound        CloudErrorCode = "SECRET_NOT_FOUND"
	CloudErrSecretCorrupt         CloudErrorCode = "SECRET_CORRUPT"
	CloudErrSecretConflict        CloudErrorCode = "SECRET_CONFLICT"
	CloudErrSecretStore           CloudErrorCode = "SECRET_STORE_UNAVAILABLE"
	CloudErrSecretStoreLocked     CloudErrorCode = "SECRET_STORE_LOCKED"
	CloudErrSecretUIForbidden     CloudErrorCode = "SECRET_UI_FORBIDDEN"
	CloudErrSecretPromptCanceled  CloudErrorCode = "SECRET_PROMPT_CANCELED"
	CloudErrSecretOutcomeUnknown  CloudErrorCode = "SECRET_OUTCOME_UNKNOWN"
	CloudErrOAuthCanceled         CloudErrorCode = "OAUTH_CANCELED"
	CloudErrOAuthCallback         CloudErrorCode = "OAUTH_CALLBACK_INVALID"
	CloudErrOAuthInvalidGrant     CloudErrorCode = "OAUTH_INVALID_GRANT"
	CloudErrOAuthRejected         CloudErrorCode = "OAUTH_REJECTED"
	CloudErrOAuthProtocol         CloudErrorCode = "OAUTH_PROTOCOL_ERROR"
	CloudErrOAuthOutcomeUnknown   CloudErrorCode = "OAUTH_OUTCOME_UNKNOWN"
	CloudErrPairingInactive       CloudErrorCode = "PAIRING_INACTIVE"
	CloudErrPairingRejected       CloudErrorCode = "PAIRING_REJECTED"
	CloudErrPairingRateLimited    CloudErrorCode = "PAIRING_RATE_LIMITED"
	CloudErrRedirectRejected      CloudErrorCode = "REDIRECT_REJECTED"
	CloudErrTimeout               CloudErrorCode = "TIMEOUT"
	CloudErrNetwork               CloudErrorCode = "NETWORK_ERROR"
	CloudErrUnauthorized          CloudErrorCode = "UNAUTHORIZED"
	CloudErrForbidden             CloudErrorCode = "FORBIDDEN"
	CloudErrSubscriptionInactive  CloudErrorCode = "SUBSCRIPTION_INACTIVE"
	CloudErrHAProtocol            CloudErrorCode = "HA_PROTOCOL_ERROR"
	CloudErrCloudNotReady         CloudErrorCode = "CLOUD_NOT_READY"
	CloudErrAppNotReady           CloudErrorCode = "APP_NOT_READY"
	CloudErrOuterSessionExpired   CloudErrorCode = "OUTER_SESSION_EXPIRED"
	CloudErrDeviceRejected        CloudErrorCode = "DEVICE_CREDENTIAL_REJECTED"
	CloudErrDevicePendingConflict CloudErrorCode = "DEVICE_PENDING_CONFLICT"
	CloudErrDeviceUserConflict    CloudErrorCode = "DEVICE_USER_CONFLICT"
	CloudErrIdentityMismatch      CloudErrorCode = "IDENTITY_MISMATCH"
	CloudErrRelayInstance         CloudErrorCode = "RELAY_INSTANCE_MISMATCH"
	CloudErrIngressUnavailable    CloudErrorCode = "INGRESS_UNAVAILABLE"
	CloudErrResponseTooLarge      CloudErrorCode = "RESPONSE_TOO_LARGE"
	CloudErrOutcomeUnknown        CloudErrorCode = "OUTCOME_UNKNOWN"
)

type CloudError struct {
	Code       CloudErrorCode
	Op         string
	StatusCode int
	Retryable  bool
	cause      error
}

func (e *CloudError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s: %s (HTTP %d)", e.Op, e.Code, e.StatusCode)
	}
	return fmt.Sprintf("%s: %s", e.Op, e.Code)
}

func (e *CloudError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newCloudError(code CloudErrorCode, op string, cause error) error {
	return &CloudError{Code: code, Op: op, cause: cause}
}

func newCloudHTTPError(code CloudErrorCode, op string, status int, retryable bool) error {
	return &CloudError{Code: code, Op: op, StatusCode: status, Retryable: retryable}
}

func IsCloudErrorCode(err error, code CloudErrorCode) bool {
	var cloudErr *CloudError
	return errors.As(err, &cloudErr) && cloudErr.Code == code
}
