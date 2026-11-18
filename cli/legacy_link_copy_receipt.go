package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Once Home Assistant answered invalid_grant for a cleanup copy's sign-in and
// the NOVA App recorded the cleanup (legacy_cleanup_complete), setup removes
// exactly that copy, without a prompt (MAR-242). Before that record the copy
// stays: a retry resumes from it. A copy setup can't remove leaves a receipt
// (its item's name and that its sign-in is revoked, never a secret); every
// later setup retries it, and doctor names it.

type legacyCopyReceipt struct {
	Service string `json:"service"`
	Revoked bool   `json:"revoked"`
}

func legacyCopyReceiptsFile(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "legacy-link-copy-receipts.json")
}

// legacyCopyReceipts: each profile id's copies still to remove.
func legacyCopyReceipts(paths runtimePaths) (map[string][]legacyCopyReceipt, error) {
	receipts := map[string][]legacyCopyReceipt{}
	raw, err := os.ReadFile(legacyCopyReceiptsFile(paths))
	if errors.Is(err, os.ErrNotExist) {
		return receipts, nil
	}
	if err != nil {
		return nil, err
	}
	return receipts, json.Unmarshal(raw, &receipts)
}

// removeRevokedLegacyLinkCopies removes exactly each of this profile's
// copies, signing each out again first (invalid_grant required) unless this
// run or a receipt already proved it; what's left becomes the receipt.
func removeRevokedLegacyLinkCopies(paths runtimePaths, cfg runtimeConfig, signOutFirst bool) error {
	if cfg.ProfileID == "" { // Link kept its sign-ins under the profile's id
		return nil
	}
	copies, err := legacyLinkCopiesOf(cfg.profileName(), cfg.ProfileID)
	switch {
	case err == nil && signOutFirst:
		err = revokeLegacyLinkCopies(copies)
	case err == nil && len(copies) > 0:
		err = deleteLegacyLinkCopies(copies)
	}
	var left []legacyCopyReceipt
	if err != nil {
		for _, service := range legacyLinkServices {
			left = append(left, legacyCopyReceipt{Service: oauthLegacyCleanupServicePrefix + string(legacyLinkState(service)), Revoked: true})
		}
	}
	return errors.Join(err, saveLegacyCopyReceipt(paths, cfg.ProfileID, left))
}

func deleteLegacyLinkCopies(copies []legacyLinkCopy) error {
	backend, err := legacyCleanupBackend()
	if err != nil {
		return err
	}
	exact, ok := backend.(exactOAuthSecretBackend)
	if !ok {
		return errors.New("this secure storage can't remove a sign-in copy exactly")
	}
	for _, c := range copies {
		if err := exact.DeleteExact(context.Background(), c.service, c.account, c.value, SecretStoreForbidUI); err != nil {
			return fmt.Errorf("server %q: %w", c.server, err)
		}
	}
	return nil
}

func saveLegacyCopyReceipt(paths runtimePaths, account string, left []legacyCopyReceipt) error {
	return withClientMutationLock(paths, func() error {
		receipts, err := legacyCopyReceipts(paths)
		if err != nil {
			return err
		}
		if _, had := receipts[account]; !had && len(left) == 0 {
			return nil
		}
		delete(receipts, account)
		if len(left) > 0 {
			receipts[account] = left
		}
		if len(receipts) == 0 {
			return os.Remove(legacyCopyReceiptsFile(paths))
		}
		raw, err := json.MarshalIndent(receipts, "", "  ")
		if err != nil {
			return err
		}
		return writeFileAtomic(legacyCopyReceiptsFile(paths), append(raw, '\n'), 0o600)
	})
}
