//go:build !linux

package main

func relayAuthTokenPlatformSetupPreflightImpl() error {
	return nil
}
