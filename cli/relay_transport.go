package main

import (
	"context"
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
	profile string,
	relayURL string,
) string {
	action := "rejected"
	if status == http.StatusForbidden {
		action = "denied"
	}
	return fmt.Sprintf(
		"local Relay %s the saved local credential; run: %s",
		action,
		localRelayRepairCommand(profile, relayURL),
	)
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
	if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
		cred, ok, credErr := readDeviceCredential()
		if credErr != nil {
			return "", nil, "", false, credErr
		}
		if ok {
			return cfg.RelaySecureBaseURL, spkiPinnedClient(cfg.RelaySpkiPin), cred, true, nil
		}
		// Paired config but the device credential is gone: fail closed. In a paired
		// flow the device credential IS the auth, so never silently downgrade to the
		// shared token over the unpinned plain port. Re-pair to recover — via setup
		// for the default profile, via pair --server for named profiles (setup
		// refuses those).
		if profile := activeServerProfile(); profile != defaultServerProfileName {
			return "", nil, "", false, fmt.Errorf(
				"device credential unavailable for a paired relay; re-pair with: %s",
				namedRelayPairCommand(profile, cfg.RelayBaseURL),
			)
		}
		return "", nil, "", false, errors.New("device credential unavailable for a paired relay; run 'ha-nova setup' to re-pair")
	}
	if profile := activeServerProfile(); profile != defaultServerProfileName {
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
