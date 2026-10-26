package main

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// deviceHealth: what the paired device's secure endpoint said.
type deviceHealth int

const (
	deviceDown    deviceHealth = iota // no answer, or not a device transport
	deviceOK                          // 200
	deviceRevoked                     // 401/403: the App no longer accepts this pairing
)

// verifyDeviceHealth confirms the paired device credential reaches the relay's
// secure endpoint. Pairing already activated the credential, so this is a final
// end-to-end check over the pinned TLS transport rather than a token probe.
var verifyDeviceHealth = func(cfg runtimeConfig) deviceHealth {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)),
	)
	defer cancel()
	selected, err := selectRelayTransport(ctx, cfg, "", false)
	if IsCloudErrorCode(err, CloudErrUnauthorized) || IsCloudErrorCode(err, CloudErrForbidden) { // the automatic route's preflight
		return deviceRevoked
	}
	if err != nil || !selected.DeviceMode {
		return deviceDown
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(selected.BaseURL, "/")+"/health",
		nil,
	)
	if err != nil {
		return deviceDown
	}
	req.Header.Set("Authorization", "Bearer "+selected.Credential)
	resp, err := selected.Client.Do(req)
	if err != nil {
		return deviceDown
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return deviceOK
	case http.StatusUnauthorized, http.StatusForbidden: // as doctor reads it
		return deviceRevoked
	}
	return deviceDown
}

// persistDeviceSetupState saves the config (secure endpoint + install id, already
// written during pairing) and the install state. It deliberately does NOT touch
// the relay auth token: device mode has no shared token, and the device
// credential already lives in its own keyring slot.
func persistDeviceSetupState(paths runtimePaths, cfg runtimeConfig, state *installState, lifecycleMarker ...[]byte) error {
	return withSetupLifecycleLock(paths, lifecycleMarker, func() error {
		return persistDeviceSetupStateUnlocked(paths, cfg, state)
	})
}

func persistDeviceSetupStateUnlocked(paths runtimePaths, cfg runtimeConfig, state *installState) error {
	if err := saveConfig(paths, cfg); err != nil {
		return err
	}
	// Stamp the setup version/source like the interactive path, or state.Version
	// stays empty and later ensureClientsVerifiedForCurrentVersion treats this
	// paired install as pre-setup.
	state.Version = localVersion(paths)
	state.InstallSource = detectInstallSource(paths, *state)
	if err := mergeLatestSetupState(paths, state); err != nil {
		return err
	}
	return saveState(paths, *state)
}

// forgetUnprovenRelayIdentity: without Link, setup's device check proves
// the endpoint, not the Relay's Link identity. As after a fresh local
// pairing, a later `cloud add` reads it again, so remove → setup → add
// works after a reinstalled App. Reports whether cfg changed.
func forgetUnprovenRelayIdentity(cfg *runtimeConfig) bool {
	if cfg.Cloud != nil || cfg.RelayInstanceID == "" {
		return false
	}
	cfg.RelayInstanceID = ""
	return true
}
