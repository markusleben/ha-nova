package main

import "context"

var revokeSelfDeviceV1ForPairing = revokeSelfDeviceV1

// profilePairingInstallID is the install id this profile's pairing sends.
func profilePairingInstallID(
	cfg *runtimeConfig,
	saveCfg func(*runtimeConfig) error,
) (string, error) {
	installID, err := getOrCreateClientInstallID(cfg, saveCfg)
	if err != nil {
		return "", err
	}
	return pairingClientInstallID(installID, activeServerProfile())
}

// previousLocalDevice is the credential and pinned endpoint a re-pair is about
// to replace. Only read for non-default profiles: one last paired by an older
// binary used the plain install id, so the Relay no longer retires its device
// when the new pairing activates.
type previousLocalDevice struct {
	credential, spkiPin string
}

func readPreviousLocalDevice(
	cfg runtimeConfig,
	ui SecretStoreUIPolicy,
) previousLocalDevice {
	if activeServerProfile() == defaultServerProfileName || cfg.RelaySpkiPin == "" {
		return previousLocalDevice{}
	}
	ctx, cancel := boundedNativeOAuthSecretContext(context.Background(), ui)
	defer cancel()
	credential, exists, err := readDeviceCredentialWithPolicy(ctx, ui)
	if err != nil || !exists || parseDeviceCredential(credential) == nil {
		return previousLocalDevice{}
	}
	return previousLocalDevice{credential, cfg.RelaySpkiPin}
}

// retireOnSameRelay revokes the replaced device after the new one is active,
// only on the same pinned Relay (same SPKI pin, so the new endpoint reaches it
// even after an address change) and only when it is a different device. A
// device the Relay already retired answers 401, which counts as done.
func (previous previousLocalDevice) retireOnSameRelay(
	secureBaseURL, newDeviceID, newSpkiPin string,
) {
	parsed := parseDeviceCredential(previous.credential)
	if parsed == nil || parsed.deviceID == newDeviceID ||
		previous.spkiPin != newSpkiPin {
		return
	}
	if err := revokeSelfDeviceV1ForPairing(
		secureBaseURL,
		previous.spkiPin,
		previous.credential,
	); err != nil {
		printHumanWarn(
			"This computer may be listed twice on the NOVA page in Home Assistant. Revoke the entry whose \"last used\" time is older.",
		)
	}
}
