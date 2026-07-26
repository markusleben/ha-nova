package main

import (
	"errors"
	"fmt"
)

type cloudProblemCode string

const (
	cloudProblemNotConfigured      cloudProblemCode = "cloud_not_configured"
	cloudProblemAdapterUnavailable cloudProblemCode = "cloud_adapter_unavailable"
	cloudProblemUnsupportedRuntime cloudProblemCode = "cloud_unsupported_runtime"
	cloudProblemSecureStorage      cloudProblemCode = "cloud_secure_storage"
	cloudProblemAuthorization      cloudProblemCode = "cloud_authorization"
	cloudProblemSubscription       cloudProblemCode = "cloud_subscription"
	cloudProblemRemoteDisabled     cloudProblemCode = "cloud_remote_disabled"
	cloudProblemUnavailable        cloudProblemCode = "cloud_unavailable"
	cloudProblemApp                cloudProblemCode = "cloud_app"
	cloudProblemIngress            cloudProblemCode = "cloud_ingress"
	cloudProblemDeviceRevoked      cloudProblemCode = "cloud_device_revoked"
	cloudProblemIdentityMismatch   cloudProblemCode = "cloud_identity_mismatch"
	cloudProblemConfigInvalid      cloudProblemCode = "cloud_config_invalid"
)

type cloudRemediation string

const (
	cloudRemediationAddAccess      cloudRemediation = "add_cloud_access"
	cloudRemediationRunInteractive cloudRemediation = "run_interactive_desktop_setup"
	cloudRemediationUnlockStorage  cloudRemediation = "unlock_secure_storage"
	cloudRemediationSignIn         cloudRemediation = "sign_in_again"
	cloudRemediationManagePlan     cloudRemediation = "manage_subscription"
	cloudRemediationEnableRemote   cloudRemediation = "enable_remote_access"
	cloudRemediationRetry          cloudRemediation = "retry_later"
	cloudRemediationManageApp      cloudRemediation = "manage_nova_app"
	cloudRemediationPair           cloudRemediation = "pair_device"
	cloudRemediationSecurityStop   cloudRemediation = "inspect_security"
	cloudRemediationVerifyState    cloudRemediation = "verify_state_without_retry"
)

// cloudProblem is the stable boundary between transport/OAuth adapters and CLI
// UX. Detail is safe user-facing context; secrets must never be put here.
type cloudProblem struct {
	Code        cloudProblemCode
	Remediation cloudRemediation
	Detail      string
	Cause       error
}

func (p *cloudProblem) Error() string {
	if p == nil {
		return ""
	}
	if p.Detail != "" {
		return fmt.Sprintf("Home Assistant Cloud: %s (code %s; next: %s)", p.Detail, p.Code, p.Remediation)
	}
	return fmt.Sprintf("Home Assistant Cloud failed (code %s; next: %s)", p.Code, p.Remediation)
}

func (p *cloudProblem) Unwrap() error {
	if p == nil {
		return nil
	}
	return p.Cause
}

func cloudNotConfiguredProblem() *cloudProblem {
	return &cloudProblem{
		Code:        cloudProblemNotConfigured,
		Remediation: cloudRemediationAddAccess,
		Detail:      "away-from-home access is not configured for this server profile",
	}
}

func cloudAdapterUnavailableProblem() *cloudProblem {
	return &cloudProblem{
		Code:        cloudProblemAdapterUnavailable,
		Remediation: cloudRemediationRetry,
		Detail:      "this build does not include the Cloud transport adapter",
	}
}

func cloudProblemForError(err error) *cloudProblem {
	var problem *cloudProblem
	if errors.As(err, &problem) {
		return problem
	}
	if errors.Is(err, errInvalidClientInstallID) {
		return invalidCloudInstallIdentityProblem(err)
	}
	var cloudErr *CloudError
	if !errors.As(err, &cloudErr) {
		return &cloudProblem{
			Code:        cloudProblemUnavailable,
			Remediation: cloudRemediationRetry,
			Detail:      "Cloud setup failed",
			Cause:       err,
		}
	}

	problem = &cloudProblem{Cause: err}
	switch cloudErr.Code {
	case CloudErrUnsupportedPlatform:
		problem.Code, problem.Remediation = cloudProblemUnsupportedRuntime, cloudRemediationRunInteractive
		problem.Detail = "this runtime cannot safely use native secure storage"
	case CloudErrSecretStoreLocked, CloudErrSecretUIForbidden, CloudErrSecretPromptCanceled:
		problem.Code, problem.Remediation = cloudProblemSecureStorage, cloudRemediationUnlockStorage
		problem.Detail = "native secure storage is locked or cannot show its unlock prompt"
	case CloudErrSecretNotFound, CloudErrSecretCorrupt, CloudErrSecretConflict:
		problem.Code, problem.Remediation = cloudProblemAuthorization, cloudRemediationSignIn
		problem.Detail = "the saved Cloud authorization is missing or inconsistent"
	case CloudErrSecretStore:
		problem.Code, problem.Remediation = cloudProblemSecureStorage, cloudRemediationUnlockStorage
		problem.Detail = "native secure storage is unavailable in this session"
	case CloudErrSecretOutcomeUnknown:
		problem.Code, problem.Remediation = cloudProblemSecureStorage, cloudRemediationVerifyState
		problem.Detail = "a secure-storage update may have completed; inspect the saved checkpoint before taking another action"
	case CloudErrOAuthCanceled, CloudErrOAuthCallback, CloudErrOAuthInvalidGrant,
		CloudErrOAuthRejected, CloudErrOAuthProtocol, CloudErrUnauthorized,
		CloudErrOuterSessionExpired:
		problem.Code, problem.Remediation = cloudProblemAuthorization, cloudRemediationSignIn
		problem.Detail = "authorization did not complete"
	case CloudErrOAuthOutcomeUnknown:
		problem.Code, problem.Remediation = cloudProblemAuthorization, cloudRemediationSecurityStop
		problem.Detail = "the authorization response was lost or invalid; review HA NOVA sessions in Home Assistant before retrying"
	case CloudErrForbidden, CloudErrSubscriptionInactive:
		problem.Code, problem.Remediation = cloudProblemSubscription, cloudRemediationManagePlan
		problem.Detail = "the Home Assistant Cloud account cannot use remote access"
	case CloudErrCloudNotReady:
		problem.Code, problem.Remediation = cloudProblemRemoteDisabled, cloudRemediationEnableRemote
		problem.Detail = "Home Assistant Cloud remote access is not ready"
	case CloudErrAppNotReady:
		problem.Code, problem.Remediation = cloudProblemApp, cloudRemediationManageApp
		problem.Detail = "the NOVA Relay App is not ready for Cloud access"
	case CloudErrIngressUnavailable:
		problem.Code, problem.Remediation = cloudProblemIngress, cloudRemediationRetry
		problem.Detail = "Home Assistant could not create a NOVA Relay Ingress session"
	case CloudErrDeviceRejected:
		problem.Code, problem.Remediation = cloudProblemDeviceRevoked, cloudRemediationPair
		problem.Detail = "the local device credential cannot be reused"
	case CloudErrDevicePendingConflict:
		problem.Code, problem.Remediation = cloudProblemIdentityMismatch, cloudRemediationSecurityStop
		problem.Detail = "another device pairing is pending; finish or clear it before Cloud setup"
	case CloudErrPairingInactive, CloudErrPairingRejected:
		problem.Code, problem.Remediation = cloudProblemDeviceRevoked, cloudRemediationPair
		problem.Detail = "Cloud device pairing is no longer active"
	case CloudErrRelayInstance:
		problem.Code, problem.Remediation = cloudProblemIdentityMismatch, cloudRemediationSecurityStop
		problem.Detail = "the NOVA Relay instance identity changed; if you intentionally reinstalled the App, remove Cloud access and explicitly pair again before re-adding it, otherwise stop and inspect the server"
	case CloudErrIdentityMismatch, CloudErrDeviceUserConflict:
		problem.Code, problem.Remediation = cloudProblemIdentityMismatch, cloudRemediationSecurityStop
		problem.Detail = "the verified Cloud identity did not match this local server profile"
	case CloudErrRedirectRejected:
		problem.Code, problem.Remediation = cloudProblemIdentityMismatch, cloudRemediationSecurityStop
		problem.Detail = "a credential-bearing Cloud request was redirected and stopped"
	case CloudErrPairingRateLimited:
		problem.Code, problem.Remediation = cloudProblemUnavailable, cloudRemediationRetry
		problem.Detail = "Cloud device pairing is temporarily rate limited"
	case CloudErrHAProtocol, CloudErrResponseTooLarge, CloudErrInvalidInput:
		problem.Code, problem.Remediation = cloudProblemApp, cloudRemediationManageApp
		problem.Detail = "Home Assistant returned an unsupported Cloud response"
	case CloudErrOutcomeUnknown:
		problem.Code, problem.Remediation = cloudProblemUnavailable, cloudRemediationVerifyState
		problem.Detail = "the request may have reached the Relay; verify the target state and do not retry automatically"
	default:
		problem.Code, problem.Remediation = cloudProblemUnavailable, cloudRemediationRetry
		problem.Detail = "Home Assistant Cloud is temporarily unavailable"
	}
	return problem
}

func invalidCloudInstallIdentityProblem(cause error) *cloudProblem {
	return &cloudProblem{
		Code:        cloudProblemConfigInvalid,
		Remediation: cloudRemediationSecurityStop,
		Detail: "client_install_id in config.json is invalid; " +
			"Cloud was not contacted",
		Cause: cause,
	}
}
