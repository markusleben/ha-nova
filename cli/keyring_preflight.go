package main

var relayAuthTokenSetupPreflightForSetup = relayAuthTokenSetupPreflight
var relayAuthTokenPlatformSetupPreflight = relayAuthTokenPlatformSetupPreflightImpl

func relayAuthTokenSetupPreflight() error {
	if relayAuthTokenTestFile() != "" {
		return nil
	}
	return relayAuthTokenPlatformSetupPreflight()
}
