package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func namedRelayPairCommand(profile string, relayURL string) string {
	relayURL = strings.TrimSpace(relayURL)
	if !relayURLSafeForCopyPaste(relayURL) {
		relayURL = "http://<ha-host>:8791"
	}
	return fmt.Sprintf(
		"ha-nova pair --server %s --relay-url %q",
		profile,
		relayURL,
	)
}

var errDeviceNeedsRePair = errors.New("this computer needs to be paired again")

// deviceNeedsRePair names the one next step: setup pairs the profile again
// with one Home Assistant sign-in.
func deviceNeedsRePair(profile string, link bool) error {
	return fmt.Errorf("%w (one Home Assistant sign-in, about a minute); run: %s", errDeviceNeedsRePair, rePairNextStep(profile, link))
}

// rePairNextStep: setup pairs a profile again by sign-in; a named profile
// that also keeps Home Assistant Link removes it first, since pairing
// refuses beside it (validateLocalDeviceReplacementAllowedWithPolicy).
func rePairNextStep(profile string, link bool) string {
	if profile == "" || profile == defaultServerProfileName {
		return "ha-nova setup"
	}
	if link {
		return "ha-nova cloud remove --server " + profile + ", then ha-nova setup --server " + profile
	}
	return "ha-nova setup --server " + profile
}

func localRelayRepairCommand(
	profile string,
	relayURL string,
) string {
	if profile != "" && profile != defaultServerProfileName {
		return namedRelayPairCommand(profile, relayURL)
	}
	return "ha-nova setup"
}

func localRelayAuthRepairMessage(
	status int,
	header http.Header,
	profile string,
	cfg runtimeConfig,
) string {
	if cfg.demoToken != "" {
		if status == http.StatusForbidden {
			return "The demo house doesn't allow this; it is a shared demo with some features turned off."
		}
		return "The demo house's access changed. Run: ha-nova try"
	}
	if bridgeRelay != nil { // ha-nova serve inside the NOVA App: no setup to run; a restart mints its sign-in again
		return "the NOVA Relay App refused the chat-app bridge's own built-in sign-in (not a setting); restart the NOVA Relay App in Home Assistant (Settings → Apps → NOVA Relay → Restart)"
	}
	if sharedAccessEnded(status, header) {
		return sharedAccessEndedFor(cfg, profile)
	}
	action := "rejected"
	if status == http.StatusForbidden {
		action = "denied"
	}
	return fmt.Sprintf(
		"local Relay %s the saved local credential; run: %s",
		action,
		localRelayRepairCommand(profile, cfg.RelayBaseURL),
	)
}

// localRelayRefusedCredential: a local Relay's answer that means the saved
// credential needs repair: a 401, or a 403 that names the credential or
// carries no Relay error code (the demo house: any 403). Every other 403 is
// the Relay refusing this one request (FILE_PATH_DENIED, FILE_TYPE_DENIED,
// FILE_ACCESS_READONLY, CORE_PATH_REFUSED, ...), and its own code and message
// must reach the caller, never a sign-in repair.
func localRelayRefusedCredential(status int, body []byte, demo bool) bool {
	if status == http.StatusUnauthorized {
		return true
	}
	if status != http.StatusForbidden {
		return false
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &envelope)
	switch envelope.Error.Code {
	case "", "UNAUTHORIZED", "SECURE_TRANSPORT_REQUIRED", "DEVICE_REQUIRED":
		return true
	}
	return demo
}

func relayURLSafeForCopyPaste(value string) bool {
	if validatePairRelayURL(value) != nil {
		return false
	}
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z',
			char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9',
			strings.ContainsRune(":/.[]_%+-", char):
		default:
			return false
		}
	}
	return true
}

// relayFunctionalTransport picks how the CLI talks to the relay for functional
// calls (/health, /ws, /core, /files, /backups):
//   - device mode: a paired device credential over the SPKI-pinned TLS secure
//     endpoint learned from pairing;
//   - legacy mode: the shared relay auth token over the plain bootstrap URL.
//
// Device mode wins whenever a device credential and a pinned secure endpoint are
// both present; this is the passwordless default after pairing. Legacy keeps
// existing installs and non-interactive/service setups working unchanged.
func relayFunctionalTransport(cfg runtimeConfig) (baseURL string, client *http.Client, token string, deviceMode bool, err error) {
	if cfg.demoToken != "" {
		// The demo house: its public token, only to its own relay URL. Reported
		// as device mode: a non-legacy credential with its own client.
		return cfg.RelayBaseURL, demoHTTPClient(), cfg.demoToken, true, nil
	}
	if cfg.bridgeCredential != "" && cfg.RelaySpkiPin == "" { // the standalone Relay's gate worker: its plain loopback port (loadGateRelay)
		return cfg.RelayBaseURL, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, cfg.bridgeCredential, true, nil
	}
	if cfg.bridgeCredential != "" { // the NOVA App's bridge: its internal device, pinned TLS
		return cfg.RelaySecureBaseURL, spkiPinnedClient(cfg.RelaySpkiPin), cfg.bridgeCredential, true, nil
	}
	if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		cred, ok, credErr := readCredentialSlot(deviceCredentialServiceForProfile(cfg.profileName()))
		if credErr != nil {
			return "", nil, "", false, credErr
		}
		if ok {
			if err := requireSealedDevicePin(cred, cfg.RelaySpkiPin, cfg.RelaySecureBaseURL); err != nil {
				return "", nil, "", false, fmt.Errorf("%w; pair again with: %s", err, localRelayRepairCommand(cfg.profileName(), cfg.RelayBaseURL))
			}
			return cfg.RelaySecureBaseURL, spkiPinnedClient(cfg.RelaySpkiPin), cred, true, nil
		}
		// Paired config but the device credential is gone (on a Mac after the
		// 0.32 update: its new slots start empty, spec §5.3): fail closed. In a
		// paired flow the device credential IS the auth, so never silently
		// downgrade to the shared token over the unpinned plain port.
		return "", nil, "", false, deviceNeedsRePair(cfg.profileName(), cfg.Cloud != nil)
	}
	if profile := cfg.profileName(); profile != defaultServerProfileName {
		// Non-default server profiles are device-credential-only: the machine-wide
		// legacy relay token belongs to the default profile, and a half-paired
		// profile must never send that token to another server's URL. Fail closed.
		return "", nil, "", false, fmt.Errorf(
			"server profile %q has no completed device pairing; run: %s",
			profile,
			namedRelayPairCommand(profile, cfg.RelayBaseURL),
		)
	}
	relayToken, tokenErr := readRelayAuthToken()
	if tokenErr != nil {
		return "", nil, "", false, tokenErr
	}
	return cfg.RelayBaseURL, httpClient, relayToken, false, nil
}

// Hook for doctor/guided-update tests.
var relayFunctionalTransportForDoctor = relayFunctionalTransport

// functionalEndpoint resolves the base URL, HTTP client, and credential for a
// functional relay call. Paired devices get the pinned secure endpoint. A
// paired config whose device credential cannot be resolved FAILS instead of
// downgrading: the caller's credential in a paired flow is the device
// credential, and it must never travel over the unpinned plain port. Legacy
// configs (no secure endpoint) keep the caller's token and error surfaces.
func functionalEndpoint(cfg runtimeConfig, legacyToken string) (string, *http.Client, string, error) {
	base, client, credential, device, err := relayFunctionalTransportForDoctor(cfg)
	if err == nil && device {
		return base, client, credential, nil
	}
	if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		if err == nil {
			err = fmt.Errorf("device credential unavailable")
		}
		return "", nil, "", fmt.Errorf("secure relay endpoint unavailable: %w", err)
	}
	// Non-default profiles are device-credential-only — same fail-closed
	// contract as relayFunctionalTransport: the caller's legacy token belongs
	// to the default profile and must never travel to another server's URL.
	if profile := activeServerProfile(); profile != defaultServerProfileName {
		return "", nil, "", fmt.Errorf(
			"server profile %q has no completed device pairing; run: %s",
			profile,
			namedRelayPairCommand(profile, cfg.RelayBaseURL),
		)
	}
	return cfg.RelayBaseURL, httpClient, legacyToken, nil
}

// checkRelayReadinessOverTransport runs the readiness probes over an explicit
// transport (the paired device path); the legacy path keeps checkRelayReadiness
// with its test-hookable probe variables.
func checkRelayReadinessOverTransport(base string, client *http.Client, credential string) relayReadiness {
	return checkRelayReadinessOverTransportContext(
		context.Background(),
		base,
		client,
		credential,
	)
}

func checkRelayReadinessOverTransportContext(
	ctx context.Context,
	base string,
	client *http.Client,
	credential string,
) relayReadiness {
	return checkRelayReadinessWithProbes(base, credential,
		func(u, t string) ([]byte, error) {
			return fetchRelayHealthWithContext(ctx, client, u, t)
		},
		func(u, t string) (relayWSPingResponse, error) {
			return probeRelayWSPingWithContext(ctx, client, u, t)
		},
		false)
}
