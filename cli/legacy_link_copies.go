package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
)

// A cleanup copy (legacy_link_cleanup.go) is kept before its old sign-in is
// revoked, so a cut cleanup can leave a live sign-in in it. Removing a
// profile's settings (uninstall --purge, server remove) therefore revokes
// each copy first, requires invalid_grant, and only then removes exactly that
// copy; a copy it can't read or revoke stops the removal with the settings
// kept, so running it again retries. The 0.31 items themselves stay: removing
// them could make macOS ask.

type legacyLinkCopy struct {
	server, account, service, value string
	envelope                        OAuthSecretEnvelope
}

// legacyCopiesOnThisOS: copies live in the macOS Keychain whatever store the
// device key uses now (a Mac may have moved to a file after a cut cleanup).
var legacyCopiesOnThisOS = runtime.GOOS == "darwin"

// legacyLinkCopyKeep writes a copy under the client mutation lock, and only
// while a profile still holds account. Purge and server remove hold that
// lock while they inventory copies and remove the profile, so a copy is
// either seen by them or never written.
var legacyLinkCopyKeep = func(paths runtimePaths, account string, write func() error) error {
	return withClientMutationLock(paths, func() error {
		doc, err := loadConfigDocumentOrEmpty(paths.ConfigFile)
		if err != nil {
			return err
		}
		for _, name := range doc.profileNames() {
			if cfg, _ := doc.flatProfile(name); cfg.ProfileID == account {
				return write()
			}
		}
		return errors.New("this server was removed meanwhile; no sign-in copy is kept")
	})
}

// legacyLinkCopiesOf reads, without a prompt, every copy one profile holds.
func legacyLinkCopiesOf(server, account string) ([]legacyLinkCopy, error) {
	if !legacyCopiesOnThisOS && !legacyCleanupApplies() || account == "" {
		return nil, nil
	}
	backend, err := legacyCleanupBackend()
	if err != nil {
		return nil, err
	}
	var copies []legacyLinkCopy
	for _, service := range legacyLinkServices {
		state := legacyLinkState(service)
		copyService := oauthLegacyCleanupServicePrefix + string(state)
		value, found, err := backend.Get(context.Background(), copyService, account, SecretStoreForbidUI)
		if err != nil {
			return nil, fmt.Errorf("server %q: %w", server, err)
		}
		if !found {
			continue
		}
		envelope, err := decodeOAuthSecretEnvelope(value, state, account)
		if err != nil {
			return nil, fmt.Errorf("server %q: %w", server, err)
		}
		copies = append(copies, legacyLinkCopy{server: server, account: account, service: copyService, value: value, envelope: envelope})
	}
	return copies, nil
}

// legacyLinkCopiesForUninstall reads every profile's copies (Link or not).
func legacyLinkCopiesForUninstall(paths runtimePaths) ([]legacyLinkCopy, error) {
	doc, err := loadConfigDocumentOrEmpty(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	var copies []legacyLinkCopy
	for _, name := range doc.profileNames() {
		cfg, _ := doc.flatProfile(name)
		found, err := legacyLinkCopiesOf(name, cfg.ProfileID)
		if err != nil {
			return nil, err
		}
		copies = append(copies, found...)
	}
	return copies, nil
}

func revokeLegacyLinkCopies(copies []legacyLinkCopy) error {
	if len(copies) == 0 {
		return nil
	}
	backend, err := legacyCleanupBackend()
	if err != nil {
		return err
	}
	exact, ok := backend.(exactOAuthSecretBackend)
	if !ok {
		return errors.New("this secure storage can't remove a sign-in copy exactly")
	}
	for _, c := range copies {
		ctx, cancel := context.WithTimeout(context.Background(), legacyCleanupRevokeTimeout)
		err := legacyLinkRevoke(ctx, c.envelope)
		cancel()
		if err != nil {
			return fmt.Errorf("server %q: %w", c.server, err)
		}
		if err := exact.DeleteExact(context.Background(), c.service, c.account, c.value, SecretStoreForbidUI); err != nil {
			return fmt.Errorf("server %q: %w", c.server, err)
		}
	}
	return nil
}

// requireLegacyLinkCopiesAbsent: purge's final proof before config.json goes;
// a copy a setup kept meanwhile stops it for the retry.
func requireLegacyLinkCopiesAbsent(paths runtimePaths) error {
	copies, err := legacyLinkCopiesForUninstall(paths)
	if err != nil {
		return fmt.Errorf("verify the old version's sign-in copies are gone: %w", err)
	}
	if len(copies) != 0 {
		return fmt.Errorf("a copy of the old version's Home Assistant Link sign-in appeared before config cleanup for server %q", copies[0].server)
	}
	return nil
}
