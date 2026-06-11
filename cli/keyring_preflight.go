package main

var relayAuthTokenSetupPreflightForSetup = relayAuthTokenSetupPreflight
var relayAuthTokenPlatformSetupPreflight = relayAuthTokenPlatformSetupPreflightImpl

func relayAuthTokenSetupPreflight() error {
	if relayAuthTokenTestFile() != "" {
		return nil
	}
	if relayAuthTokenFileEnabled() {
		return nil
	}
	return relayAuthTokenPlatformSetupPreflight()
}
