//go:build !darwin && !linux && !windows

package main

func newNativeOAuthSecretBackend() (OAuthSecretBackend, error) {
	return nil, newCloudError(CloudErrUnsupportedPlatform, "initialize OAuth secret store", nil)
}
