package main

import (
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
)

// hostLabel is a short, human-friendly device name for the NOVA device list.
func hostLabel() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "This computer"
	}
	// Trim a trailing ".local" and cap the length the relay accepts (<=64).
	name = strings.TrimSuffix(name, ".local")
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// Orchestrates the safe pairing/re-pairing flow: the new credential is stored
// PENDING first, activated over pinned TLS, and only then promoted to current.
// A re-pair therefore never invalidates the working credential until the new one
// is proven active — and an interrupted run is resumable from the pending slot.

type pairingClientInfo struct {
	name     string
	platform string
	client   string
}

func defaultPairingClientInfo() pairingClientInfo {
	return pairingClientInfo{name: hostLabel(), platform: runtime.GOOS, client: "cli"}
}

// runSecurePairing pairs against the relay's bootstrap URL using the one-time
// code, then activates and promotes. It persists the secure endpoint + pin via
// saveCfg. Returns the device id on success.
func runSecurePairing(bootstrapURL, code string, cfg *runtimeConfig, saveCfg func(*runtimeConfig) error, info pairingClientInfo) (string, error) {
	installID, err := getOrCreateClientInstallID(cfg, saveCfg)
	if err != nil {
		return "", fmt.Errorf("could not establish an install id: %w", err)
	}

	prov, err := pairDeviceV1(nil, bootstrapURL, code, deviceMetadata{
		Name:            info.name,
		Platform:        info.platform,
		Client:          info.client,
		ClientInstallID: installID,
	})
	if err != nil {
		return "", err
	}

	// Local-first: persist the pending credential BEFORE activating, so a crash
	// after activation can still resume (the credential is not lost).
	if err := writePendingDeviceCredential(prov.Credential); err != nil {
		return "", fmt.Errorf("could not store the new credential securely: %w", err)
	}

	secureBase, err := secureBaseFromBootstrap(bootstrapURL, prov.SecurePort)
	if err != nil {
		return "", err
	}

	if err := activateDeviceV1(secureBase, prov.SpkiPin, prov.Credential); err != nil {
		return "", fmt.Errorf("could not activate the new device: %w", err)
	}

	// Activation succeeded: promote pending -> current and remember the endpoint.
	if err := promotePendingDeviceCredential(); err != nil {
		return "", fmt.Errorf("activated but could not finalize the credential: %w", err)
	}
	cfg.RelaySecureBaseURL = secureBase
	cfg.RelaySpkiPin = prov.SpkiPin
	if err := saveCfg(cfg); err != nil {
		return "", fmt.Errorf("paired but could not save the secure endpoint: %w", err)
	}
	return prov.DeviceID, nil
}

// resumePendingActivation completes an interrupted pairing whose credential is
// already stored pending (e.g. a crash between activate and promote). Safe to
// call at setup/doctor start; a no-op when there is no pending credential.
func resumePendingActivation(cfg *runtimeConfig, saveCfg func(*runtimeConfig) error) (bool, error) {
	pending, ok, err := readPendingDeviceCredential()
	if err != nil || !ok {
		return false, err
	}
	if cfg.RelaySecureBaseURL == "" || cfg.RelaySpkiPin == "" {
		// No known secure endpoint to activate against; leave the pending slot
		// for a full re-pair rather than guessing.
		return false, nil
	}
	if err := activateDeviceV1(cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, pending); err != nil {
		return false, err
	}
	if err := promotePendingDeviceCredential(); err != nil {
		return false, err
	}
	return true, saveCfg(cfg)
}

func secureBaseFromBootstrap(bootstrapURL string, securePort int) (string, error) {
	u, err := url.Parse(bootstrapURL)
	if err != nil {
		return "", fmt.Errorf("invalid relay URL: %w", err)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("relay URL has no host")
	}
	if securePort <= 0 || securePort > 65535 {
		return "", fmt.Errorf("relay returned an invalid secure port %d", securePort)
	}
	return fmt.Sprintf("https://%s:%d", u.Hostname(), securePort), nil
}

// Hook for tests (the revoke would otherwise dial a real endpoint).
var revokeSelfDeviceV1ForRetire = revokeSelfDeviceV1

// retireDeviceCredential removes this install's device pairing after the user
// completed setup on the legacy token path: the verified token is now the
// working credential, and a leftover (usually dead) pairing would win transport
// resolution and wedge doctor and every skill call on the next run. The revoke
// is best-effort — the relay may still know the device even when the local
// pairing stopped working.
func retireDeviceCredential(cfg *runtimeConfig) {
	if cfg.RelaySecureBaseURL == "" && cfg.RelaySpkiPin == "" {
		return
	}
	if cred, ok, err := readDeviceCredential(); err == nil && ok && cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		_ = revokeSelfDeviceV1ForRetire(cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cred)
	}
	_ = deleteDeviceCredential()
	_ = deletePendingDeviceCredential()
	cfg.RelaySecureBaseURL = ""
	cfg.RelaySpkiPin = ""
}
