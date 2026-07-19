package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"strconv"
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
// Test seams so the pairing orchestration can be exercised without a live relay.
var pairDeviceV1ForPairing = pairDeviceV1
var activateDeviceV1ForPairing = activateDeviceV1

func runSecurePairing(bootstrapURL, code string, cfg *runtimeConfig, saveCfg func(*runtimeConfig) error, info pairingClientInfo) (string, error) {
	installID, err := getOrCreateClientInstallID(cfg, saveCfg)
	if err != nil {
		return "", fmt.Errorf("could not establish an install id: %w", err)
	}

	// Prove credential storage works BEFORE talking to the relay: /pair/v1/finish
	// consumes the owner's one-time code, so a broken keyring discovered after the
	// fact would burn the code with nothing stored. Callers probe earlier for the
	// user-facing message; this guard keeps the invariant for every caller.
	if _, err := probeDeviceCredentialStorage(); err != nil {
		return "", fmt.Errorf("cannot store a device credential on this system (the code was not used): %w", err)
	}

	prov, err := pairDeviceV1ForPairing(nil, bootstrapURL, code, deviceMetadata{
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

	// Persist the new endpoint as PENDING before activation — the live endpoint is
	// untouched, so a failed re-pair keeps the working install. With the pending
	// credential already stored, a crash between activation and promotion resumes:
	// resumePendingActivation reads this pending endpoint.
	cfg.PendingSecureBaseURL = secureBase
	cfg.PendingSpkiPin = prov.SpkiPin
	if err := saveCfg(cfg); err != nil {
		return "", fmt.Errorf("could not save the pending secure endpoint: %w", err)
	}

	if err := activateDeviceV1ForPairing(secureBase, prov.SpkiPin, prov.Credential); err != nil {
		return "", fmt.Errorf("could not activate the new device: %w", err)
	}

	// Save the live endpoint BEFORE promoting the credential (which deletes the
	// pending slot), and keep the pending endpoint until after promotion. A crash
	// before promotion then stays resumable: resumePendingActivation still finds a
	// pending credential + endpoint and completes idempotently. If this save fails,
	// nothing was promoted, so a re-run resumes cleanly.
	cfg.RelaySecureBaseURL = secureBase
	cfg.RelaySpkiPin = prov.SpkiPin
	if err := saveCfg(cfg); err != nil {
		return "", fmt.Errorf("could not save the secure endpoint: %w", err)
	}
	if err := promotePendingDeviceCredential(); err != nil {
		return "", fmt.Errorf("activated but could not finalize the credential: %w", err)
	}
	// Credential + live endpoint are now durable and working; clearing the stale
	// pending endpoint is best-effort (resume ignores it — the pending credential
	// is gone — so a failed clear leaves only an inert value).
	cfg.PendingSecureBaseURL = ""
	cfg.PendingSpkiPin = ""
	_ = saveCfg(cfg)
	return prov.DeviceID, nil
}

// resumePendingActivation completes an interrupted pairing whose credential is
// already stored pending (e.g. a crash between activate and promote). Safe to
// call at setup/doctor start; a no-op when there is no pending credential.
func resumePendingActivation(cfg *runtimeConfig, saveCfg func(*runtimeConfig) error) (bool, error) {
	base, pin := cfg.PendingSecureBaseURL, cfg.PendingSpkiPin
	if base == "" || pin == "" {
		// No interrupted pairing to resume (cheap check first, before any keyring
		// access): leave any pending slot for a full re-pair rather than guessing.
		return false, nil
	}
	// Choose the backend that actually holds the interrupted pairing, without a
	// storage probe (a probe could reroute reads to a now-usable keyring and lose
	// a headless file pending). Prefer a real KEYRING pending: a desktop re-pair
	// stores its pending there, and it must win over any orphan .pending FILE from
	// an aborted earlier headless attempt. Only when the keyring holds no pending
	// (or is unreachable, i.e. headless) do we resume from the file — that is the
	// genuine headless-interrupted pairing whose marker is not written until
	// promotion.
	var pending string
	fileMode := false
	kp, kok, kerr := readKeyringDeviceSecret(deviceCredentialPendingService)
	switch {
	case kok && parseDeviceCredential(kp) != nil:
		// A real keyring pending wins over any orphan .pending file.
		pending = kp
	case kok:
		// The keyring pending slot exists but is malformed (corrupted/partial):
		// surface it rather than silently resuming an orphan file behind it.
		return false, fmt.Errorf("keyring pending credential is malformed")
	case kerr != nil && !isDesktopKeyringSessionUnavailableError(kerr) && !isDesktopKeyringUnavailableError(kerr):
		// The keyring EXISTS but is unreadable (locked/uninitialized/other): a real
		// keyring pending may be hidden behind it, so refuse to resume a file and
		// silently downgrade. Only a genuinely absent keyring (headless) or an
		// empty-but-readable one (below) may fall back to the file.
		return false, kerr
	case deviceSecretFileExists(deviceCredentialPendingService):
		// Keyring empty (not-found) or genuinely unreachable (headless): the file
		// holds the interrupted headless pairing.
		raw, err := deviceSecretFileGet(deviceCredentialPendingService)
		if err != nil {
			return false, err
		}
		if parseDeviceCredential(raw) == nil {
			return false, nil // malformed residue: leave it for a full re-pair
		}
		pending = raw
		fileMode = true
	default:
		return false, kerr
	}
	if err := activateDeviceV1ForPairing(base, pin, pending); err != nil {
		return false, err
	}
	// Same ordering as runSecurePairing: save the live endpoint before promoting
	// the credential, keep the pending endpoint until afterwards, so a crash
	// mid-way stays resumable.
	cfg.RelaySecureBaseURL = base
	cfg.RelaySpkiPin = pin
	if err := saveCfg(cfg); err != nil {
		return false, err
	}
	if fileMode {
		if err := promotePendingFileCredential(pending); err != nil {
			return false, err
		}
	} else {
		// Keyring-backed: promote the pending we already read and drop the keyring
		// pending slot. An orphan .pending FILE (if any) is left untouched — it is
		// harmless residue that marker-based reads never consult.
		if err := writeDeviceCredential(pending); err != nil {
			return false, err
		}
		if err := deletePendingDeviceCredential(); err != nil {
			return false, err
		}
	}
	cfg.PendingSecureBaseURL = ""
	cfg.PendingSpkiPin = ""
	_ = saveCfg(cfg)
	return true, nil
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
	// net.JoinHostPort brackets IPv6 literals (u.Hostname() returns them
	// unbracketed), so an IPv6 relay yields a valid https://[addr]:port URL.
	return "https://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(securePort)), nil
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
	// Revoke the active device credential over the pinned transport when a live
	// endpoint + credential exist.
	if cred, ok, err := readDeviceCredential(); err == nil && ok && cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		_ = revokeSelfDeviceV1ForRetire(cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cred)
	}
	// Always clear BOTH the current and pending device credentials + endpoints
	// (idempotent), so a half-finished pairing — the pending slot saved before the
	// live endpoint — cannot be resumed after the user chose the legacy/manual path.
	_ = deleteDeviceCredential()
	_ = deletePendingDeviceCredential()
	cfg.RelaySecureBaseURL = ""
	cfg.RelaySpkiPin = ""
	cfg.PendingSecureBaseURL = ""
	cfg.PendingSpkiPin = ""
}
