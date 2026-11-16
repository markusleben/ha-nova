package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// A Mac updated from 0.31 keeps its Home Assistant Link sign-ins in the old
// Keychain items, which the old signed binary can still read without asking.
// One-click confirming stays off for this computer until each one is revoked
// and Home Assistant answers invalid_grant for it; then the computer reports
// the cleanup to the NOVA App (spec §5.3). The old items stay where they are:
// only setup, which a person starts, ever asks macOS for them.

// oauthLegacyCleanupServicePrefix names the copy of an old sign-in, kept in
// the new root before it is revoked, so a retry verifies it without asking
// for the old item again.
const oauthLegacyCleanupServicePrefix = "ha-nova.oauth.home-assistant-cloud.legacy-cleanup."

// legacyLinkServices: each 0.31 Link item, by the state it held.
var legacyLinkServices = []string{oauthSecretCurrentService, oauthSecretPendingService, oauthSecretRetiringService}

func legacyLinkState(service string) OAuthSecretState {
	return map[string]OAuthSecretState{
		oauthSecretCurrentService:  OAuthSecretCurrent,
		oauthSecretPendingService:  OAuthSecretPending,
		oauthSecretRetiringService: OAuthSecretRetiring,
	}[service]
}

type legacyCleanupOutcome uint8

const (
	legacyCleanupNotNeeded legacyCleanupOutcome = iota // not a Mac Keychain pairing, or an App without the window
	legacyCleanupComplete
	legacyCleanupIncomplete
)

const legacyCleanupRevokeTimeout = 30 * time.Second

var (
	legacyCleanupApplies = func() bool { return pairingCredentialStore() == "keychain" }
	legacyLinkSecretRead = readLegacyLinkSecret
	legacyCleanupBackend = newNativeOAuthSecretBackend
	legacyLinkRevoke     = revokeAndVerifyCloudAuthorization
	legacyCleanupPending = legacyCleanupPendingOnApp
	legacyCleanupReport  = reportLegacyCleanupComplete
)

// finishLegacyLinkCleanup signs this profile's old version out of Home
// Assistant and reports it. Only a person-started setup passes prompt;
// intro runs once, right before macOS first asks. revoked counts the old
// sign-ins verified revoked in this run.
func finishLegacyLinkCleanup(paths runtimePaths, cfg runtimeConfig, prompt bool, intro func()) (outcome legacyCleanupOutcome, revoked int) {
	if !legacyCleanupApplies() {
		return legacyCleanupNotNeeded, 0
	}
	pending, err := legacyCleanupPending(paths, cfg)
	if err != nil {
		return legacyCleanupIncomplete, 0
	}
	if !pending {
		return legacyCleanupNotNeeded, 0
	}
	if cfg.ProfileID != "" { // Link stored its sign-ins under the profile's id, never its name
		backend, err := legacyCleanupBackend()
		if err != nil {
			return legacyCleanupIncomplete, 0
		}
		for _, service := range legacyLinkServices {
			envelope, found, err := recoverLegacyLinkGrant(backend, service, cfg.ProfileID, prompt, &intro)
			if err != nil {
				return legacyCleanupIncomplete, revoked
			}
			if !found {
				continue
			}
			ctx, cancel := context.WithTimeout(context.Background(), legacyCleanupRevokeTimeout)
			err = legacyLinkRevoke(ctx, envelope)
			cancel()
			if err != nil {
				return legacyCleanupIncomplete, revoked
			}
			revoked++
		}
	}
	if legacyCleanupReport(paths, cfg) != nil {
		return legacyCleanupIncomplete, revoked
	}
	return legacyCleanupComplete, revoked
}

// recoverLegacyLinkGrant returns the old sign-in held under service: the
// copy an earlier attempt kept, else the old item, read without asking when
// macOS allows it and otherwise (prompt only) with one macOS prompt. The copy
// is kept before anything is revoked. An absent item asks nothing.
func recoverLegacyLinkGrant(backend OAuthSecretBackend, service, account string, prompt bool, intro *func()) (OAuthSecretEnvelope, bool, error) {
	ctx, state := context.Background(), legacyLinkState(service)
	copyService := oauthLegacyCleanupServicePrefix + string(state)
	value, found, err := backend.Get(ctx, copyService, account, SecretStoreForbidUI)
	if err != nil || found {
		envelope, decodeErr := decodeOAuthSecretEnvelope(value, state, account)
		return envelope, found, errors.Join(err, decodeErr)
	}
	value, found, err = legacyLinkSecretRead(ctx, service, account, SecretStoreForbidUI)
	if IsCloudErrorCode(err, CloudErrSecretUIForbidden) && prompt {
		if *intro != nil {
			(*intro)()
			*intro = nil
		}
		value, found, err = legacyLinkSecretRead(ctx, service, account, SecretStoreAllowUI)
	}
	if err != nil || !found {
		return OAuthSecretEnvelope{}, false, err
	}
	envelope, err := decodeOAuthSecretEnvelope(value, state, account)
	if err != nil {
		return OAuthSecretEnvelope{}, false, err
	}
	if err := backend.Set(ctx, copyService, account, value, SecretStoreForbidUI); err != nil {
		return OAuthSecretEnvelope{}, false, err
	}
	return envelope, true, nil
}

// readLegacyLinkSecret reads one 0.31 Link item by its own name through the
// native secret worker (macOS only).
func readLegacyLinkSecret(ctx context.Context, service, account string, ui SecretStoreUIPolicy) (string, bool, error) {
	operationCtx, cancel := boundedNativeOAuthSecretContext(ctx, ui)
	defer cancel()
	response, err := runNativeSecretWorkerProcess(operationCtx, nativeSecretWorkerRequest{
		SchemaVersion: nativeSecretWorkerSchema,
		Operation:     nativeSecretLegacyGet,
		UI:            ui,
		Service:       service,
		Account:       account,
	})
	if err != nil {
		return "", false, err
	}
	secret := string(response.Value)
	zeroSecretBytes(response.Value)
	return secret, response.Found, nil
}

func legacyCleanupSession(paths runtimePaths, cfg runtimeConfig) (*relaySession, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	transport, err := selectRelayTransport(ctx, cfg, "", false)
	if err != nil {
		cancel()
		return nil, err
	}
	return relaySessionOn(paths, cfg, transport, ctx, cancel), nil
}

// legacyCleanupPendingOnApp: the NOVA App still holds this paired computer's
// cleanup open (/gate/v1/me). A computer whose cleanup is complete, an App
// without the window contract, and the old shared token have nothing the
// cleanup would turn on, so setup asks nothing there.
func legacyCleanupPendingOnApp(paths runtimePaths, cfg runtimeConfig) (bool, error) {
	s, err := legacyCleanupSession(paths, cfg)
	if err != nil {
		return false, err
	}
	defer s.close()
	if !s.transport.DeviceMode {
		return false, nil
	}
	on, window, err := s.readGate()
	if err != nil {
		return false, err
	}
	s.gate = &relayGate{checked: true, on: on, window: on && window}
	return s.readGateMe().why == "legacy_cleanup_pending", nil
}

func reportLegacyCleanupComplete(paths runtimePaths, cfg runtimeConfig) error {
	s, err := legacyCleanupSession(paths, cfg)
	if err != nil {
		return err
	}
	defer s.close()
	var answer struct {
		LegacyCleanup string `json:"legacy_cleanup"`
	}
	if err := s.gateCall(s.ctx, gateAskRoute, 10*time.Second, map[string]any{"op": "legacy_cleanup_complete"}, &answer); err != nil {
		return err
	}
	if answer.LegacyCleanup != "complete" {
		return errors.New("the NOVA App did not record the cleanup")
	}
	return nil
}

// legacyLinkCleanupInSetup runs the cleanup in a setup a person started,
// showing the introduction right before macOS asks, and returns the line
// setup ends on: what it did, or the retry when it couldn't finish (empty:
// nothing to say).
func legacyLinkCleanupInSetup(show func(setupBlock), paths runtimePaths, cfg runtimeConfig, prompt bool) setupMsg {
	outcome, revoked := finishLegacyLinkCleanup(paths, cfg, prompt, func() {
		show(setupParagraph(setupText("setup.cleanup.intro")))
	})
	switch {
	case outcome == legacyCleanupIncomplete:
		return setupText("setup.cleanup.incomplete", "command", gateCleanupCommand(cfg.profileName()))
	case outcome == legacyCleanupComplete && revoked > 0:
		return setupText("setup.cleanup.done")
	}
	return setupMsg{}
}

// printLegacyLinkCleanup: the same for the terminal-only setup paths.
func printLegacyLinkCleanup(out io.Writer, paths runtimePaths, cfg runtimeConfig, prompt bool) string {
	return legacyLinkCleanupInSetup(func(b setupBlock) {
		for _, line := range b.Lines {
			fmt.Fprintln(out, "  "+line.plain())
		}
	}, paths, cfg, prompt).plain()
}
