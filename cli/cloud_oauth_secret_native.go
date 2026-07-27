package main

import (
	"context"
	"errors"
	"time"
)

var nativeOAuthSecretAllowUITimeout = 2 * time.Minute
var nativeOAuthSecretNoUITimeout = 15 * time.Second

func boundedNativeOAuthSecretContext(
	ctx context.Context,
	ui SecretStoreUIPolicy,
) (context.Context, context.CancelFunc) {
	timeout := nativeOAuthSecretNoUITimeout
	if ui == SecretStoreAllowUI {
		timeout = nativeOAuthSecretAllowUITimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func classifyOAuthNativeStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return newCloudError(CloudErrTimeout, "access OAuth secret store", err)
	case isDesktopKeyringLockedError(err), isDesktopKeyringInitializationRequiredError(err):
		return newCloudError(CloudErrSecretStoreLocked, "access OAuth secret store", err)
	case isDesktopKeyringSessionUnavailableError(err), isDesktopKeyringUnavailableError(err),
		isDesktopKeyringSetupRequiredError(err), isWindowsNetworkLogonSessionError(err):
		return newCloudError(CloudErrSecretStore, "access OAuth secret store", err)
	default:
		return newCloudError(CloudErrSecretStore, "access OAuth secret store", err)
	}
}
