package main

import (
	"context"
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
var explicitPairingSecretStoreUIPolicy = func() SecretStoreUIPolicy {
	return SecretStoreForbidUI
}

func runSecurePairingWithValidationPolicy(
	bootstrapURL, code string,
	cfg *runtimeConfig,
	saveCfg func(*runtimeConfig) error,
	info pairingClientInfo,
	validationUI SecretStoreUIPolicy,
) (string, error) {
	pairingUI := explicitPairingSecretStoreUIPolicy()
	if err := validateLocalDeviceReplacementAllowedWithPolicy(
		*cfg,
		validationUI,
	); err != nil {
		return "", err
	}
	// Prove credential storage works BEFORE talking to the relay: /pair/v1/finish
	// consumes the owner's one-time code, so a broken keyring discovered after the
	// fact would burn the code with nothing stored. Callers probe earlier for the
	// user-facing message; this guard keeps the invariant for every caller.
	probeCtx, cancelProbe := boundedNativeOAuthSecretContext(
		context.Background(),
		pairingUI,
	)
	_, err := probeDeviceCredentialStorageWithPolicy(
		probeCtx,
		pairingUI,
	)
	cancelProbe()
	if err != nil {
		return "", fmt.Errorf("cannot store a device credential on this system (the code was not used): %w", err)
	}
	installID, err := getOrCreateClientInstallID(cfg, saveCfg)
	if err != nil {
		return "", fmt.Errorf("could not establish an install id: %w", err)
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
	pendingCtx, cancelPending := boundedNativeOAuthSecretContext(
		context.Background(),
		pairingUI,
	)
	err = writePendingDeviceCredentialWithPolicy(
		pendingCtx,
		prov.Credential,
		pairingUI,
	)
	cancelPending()
	if err != nil {
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

	// An EXPLICIT file-backend opt-in survives interruption: pending credential
	// AND endpoint are durable now, so persist the marker — without it, a crash
	// between activation and promotion leaves the next run keyring-routed, and a
	// locked keyring would strand the activated pairing (code burned). Ordered
	// after the endpoint save so an earlier crash never flips the install while
	// there is nothing resumable (any keyring credential a desktop install still
	// holds was already migrated to a current file before pairing began). The
	// headless AUTO-detected path deliberately stays unmarked until promotion.
	if deviceCredentialFileModeExplicit {
		if err := writeDeviceFileBackendMarker(); err != nil {
			return "", fmt.Errorf("could not persist the file-backend decision: %w", err)
		}
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
	// A local pairing does not yet prove the Relay's durable instance identity.
	// Clear any pre-Cloud residue so a later Cloud add must repopulate it from
	// authenticated local discovery instead of inheriting another Relay.
	cfg.RelayInstanceID = ""
	if err := saveCfg(cfg); err != nil {
		return "", fmt.Errorf("could not save the secure endpoint: %w", err)
	}
	promoteCtx, cancelPromote := boundedNativeOAuthSecretContext(
		context.Background(),
		pairingUI,
	)
	err = promotePendingDeviceCredentialWithPolicy(
		promoteCtx,
		pairingUI,
	)
	cancelPromote()
	if err != nil {
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

func validateLocalDeviceReplacementAllowed(cfg runtimeConfig) error {
	return validateLocalDeviceReplacementAllowedWithPolicy(
		cfg,
		SecretStoreForbidUI,
	)
}

func validateLocalDeviceReplacementAllowedWithPolicy(
	cfg runtimeConfig,
	ui SecretStoreUIPolicy,
) error {
	if cfg.Cloud != nil {
		return fmt.Errorf(
			"Home Assistant Cloud access is configured; remove it before replacing the local device pairing (the code was not used)",
		)
	}
	ctx, cancel := boundedNativeOAuthSecretContext(
		context.Background(),
		ui,
	)
	defer cancel()
	if pending, exists, err := readPendingDeviceCredentialRecordWithPolicy(
		ctx,
		ui,
	); err != nil {
		return fmt.Errorf(
			"cannot inspect the pending device credential before local pairing: %w",
			err,
		)
	} else if exists {
		switch {
		case pending.Source == pendingDeviceCredentialSourceCloud:
			return fmt.Errorf(
				"Home Assistant Cloud device pairing is still pending; resume or remove Cloud access before starting a local pairing (the code was not used)",
			)
		case pending.Source == pendingDeviceCredentialSourceLocal &&
			strings.TrimSpace(cfg.PendingSecureBaseURL) != "" &&
			strings.TrimSpace(cfg.PendingSpkiPin) != "":
			return fmt.Errorf(
				"a local device activation is still pending; resume it before starting another local pairing (the code was not used)",
			)
		}
	}
	return nil
}

// resumePendingActivation completes an interrupted pairing whose credential is
// already stored pending (e.g. a crash between activate and promote). Safe to
// call at setup/doctor start; a no-op when there is no pending credential.
func resumePendingActivation(cfg *runtimeConfig, saveCfg func(*runtimeConfig) error) (bool, error) {
	return resumePendingActivationWithPolicy(
		cfg,
		saveCfg,
		SecretStoreForbidUI,
	)
}

func resumePendingActivationWithPolicy(
	cfg *runtimeConfig,
	saveCfg func(*runtimeConfig) error,
	ui SecretStoreUIPolicy,
) (bool, error) {
	if err := validateSecretUIPolicy(ui); err != nil {
		return false, err
	}
	base, pin := cfg.PendingSecureBaseURL, cfg.PendingSpkiPin
	if base == "" || pin == "" {
		// No interrupted pairing to resume (cheap check first, before any keyring
		// access): leave any pending slot for a full re-pair rather than guessing.
		return false, nil
	}
	if cfg.Cloud != nil {
		return false, fmt.Errorf(
			"cannot resume a local device replacement while Home Assistant Cloud access is configured; remove Cloud access first",
		)
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
	pendingService := activeDeviceCredentialPendingService()
	if deviceSecretFileBacked() {
		// Established file-backed install (marker present, or forced this process):
		// the pending slot is a file. Do NOT probe the keyring — a locked/present
		// Secret Service on this box is irrelevant and must not block resuming a
		// valid file pending.
		if !deviceSecretFileExists(pendingService) {
			return false, nil
		}
		raw, err := deviceSecretFileGet(pendingService)
		if err != nil {
			return false, err
		}
		record, err := decodePendingDeviceCredentialRecord(raw)
		if err != nil {
			return false, nil // malformed residue: leave it for a full re-pair
		}
		if record.Source != pendingDeviceCredentialSourceLocal {
			return false, fmt.Errorf(
				"pending device credential belongs to Cloud setup and cannot be activated on the local pairing endpoint",
			)
		}
		pending = record.Credential
		fileMode = true
	} else {
		// No marker: the install may be keyring-backed (desktop, possibly with an
		// orphan file) or a headless-interrupted pairing whose marker is not
		// written until promotion. Prefer a real keyring pending over an orphan
		// file; only an absent/empty keyring falls back to the file.
		kp, kok, kerr := readKeyringDeviceSecretWithPolicy(
			context.Background(),
			pendingService,
			ui,
		)
		switch {
		case kok:
			record, decodeErr := decodePendingDeviceCredentialRecord(kp)
			if decodeErr != nil {
				return false, fmt.Errorf(
					"keyring pending credential is malformed: %w",
					decodeErr,
				)
			}
			if record.Source != pendingDeviceCredentialSourceLocal {
				return false, fmt.Errorf(
					"pending device credential belongs to Cloud setup and cannot be activated on the local pairing endpoint",
				)
			}
			pending = record.Credential
		case kerr != nil && !isDesktopKeyringSessionUnavailableError(kerr) && !isDesktopKeyringUnavailableError(kerr):
			// Keyring EXISTS but is unreadable (locked/uninitialized): a real
			// keyring pending may hide behind it — refuse to downgrade to a file.
			return false, kerr
		case deviceSecretFileExists(pendingService):
			// Keyring empty (not-found) or genuinely unreachable (headless): the
			// file holds the interrupted headless pairing.
			raw, err := deviceSecretFileGet(pendingService)
			if err != nil {
				return false, err
			}
			record, decodeErr := decodePendingDeviceCredentialRecord(raw)
			if decodeErr != nil {
				return false, nil
			}
			if record.Source != pendingDeviceCredentialSourceLocal {
				return false, fmt.Errorf(
					"pending device credential belongs to Cloud setup and cannot be activated on the local pairing endpoint",
				)
			}
			pending = record.Credential
			fileMode = true
		default:
			return false, kerr
		}
	}
	if err := activateDeviceV1ForPairing(base, pin, pending); err != nil {
		return false, err
	}
	// Same ordering as runSecurePairing: save the live endpoint before promoting
	// the credential, keep the pending endpoint until afterwards, so a crash
	// mid-way stays resumable.
	cfg.RelaySecureBaseURL = base
	cfg.RelaySpkiPin = pin
	// The resumed local pairing proves the endpoint and pin, but not the
	// Relay's durable Cloud instance identity. Drop any pre-reinstall residue
	// in the same live-config checkpoint as a fresh local pairing.
	cfg.RelayInstanceID = ""
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
		ctx, cancel := boundedNativeOAuthSecretContext(
			context.Background(),
			ui,
		)
		defer cancel()
		if err := writeDeviceCredentialWithPolicy(ctx, pending, ui); err != nil {
			return false, err
		}
		if err := deletePendingDeviceCredentialWithPolicy(ctx, ui); err != nil {
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
