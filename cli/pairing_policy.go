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

func runSecurePairingWithValidationPolicy(
	bootstrapURL, code string,
	cfg *runtimeConfig,
	saveCfg func(*runtimeConfig) error,
	info pairingClientInfo,
	validationUI SecretStoreUIPolicy,
) (string, error) {
	provision := func(meta deviceMetadata) (*provisionedCredential, error) {
		return pairDeviceV1ForPairing(nil, bootstrapURL, code, meta)
	}
	return runSecurePairingWithProvisioner(
		bootstrapURL,
		provision,
		cfg,
		saveCfg,
		info,
		validationUI,
	)
}

// pairingProvisioner obtains a pending credential and the relay's TLS pin: a
// typed code over the LAN pairing port, or Home Assistant sign-in over ingress.
type pairingProvisioner func(deviceMetadata) (*provisionedCredential, error)

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
