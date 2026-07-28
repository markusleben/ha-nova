package main

import "context"

func runSecurePairing(
	bootstrapURL, code string,
	cfg *runtimeConfig,
	saveCfg func(*runtimeConfig) error,
	info pairingClientInfo,
) (string, error) {
	return runSecurePairingWithValidationPolicy(
		bootstrapURL,
		code,
		cfg,
		saveCfg,
		info,
		explicitPairingSecretStoreUIPolicy(),
	)
}

func runSecurePairingAfterInteractivePreflight(
	bootstrapURL, code string,
	cfg *runtimeConfig,
	saveCfg func(*runtimeConfig) error,
	info pairingClientInfo,
) (string, error) {
	return runSecurePairingWithValidationPolicy(
		bootstrapURL,
		code,
		cfg,
		saveCfg,
		info,
		SecretStoreForbidUI,
	)
}

func pairCommandSecretStoreUIPolicy(
	credentialStore string,
) SecretStoreUIPolicy {
	if credentialStore == "file" {
		return SecretStoreForbidUI
	}
	return explicitPairingSecretStoreUIPolicy()
}

func probePairCommandDeviceStorage(
	ui SecretStoreUIPolicy,
) (deviceStorageProbe, error) {
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		ui,
	)
	defer cancel()
	return probeDeviceCredentialStorageWithPolicy(ctx, ui)
}
