package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// verifyDeviceHealth confirms the paired device credential reaches the relay's
// secure endpoint. Pairing already activated the credential, so this is a final
// end-to-end check over the pinned TLS transport rather than a token probe.
var verifyDeviceHealth = func(cfg runtimeConfig) bool {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(defaultRelayMaxTimeSeconds*float64(time.Second)),
	)
	defer cancel()
	selected, err := selectRelayTransport(ctx, cfg, "", false)
	if err != nil || !selected.DeviceMode {
		return false
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(selected.BaseURL, "/")+"/health",
		nil,
	)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+selected.Credential)
	resp, err := selected.Client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
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

// renderDeviceVerifyIntro keeps the wizard copy honest about what was verified.
func renderDeviceVerifyIntro(out io.Writer) {
	renderSetupParagraphTight(out, "Checking the secure device connection to NOVA Relay.")
}
