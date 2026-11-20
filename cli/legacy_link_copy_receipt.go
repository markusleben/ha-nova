package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// Once Home Assistant answered invalid_grant for a cleanup copy's sign-in and
// the NOVA App recorded the cleanup (legacy_cleanup_complete), setup removes
// exactly the copy bytes it verified, without a prompt (MAR-242). Before that
// record the copy stays: a retry resumes from it. A verified copy setup can't
// remove leaves a receipt: its item's name and the digest of those exact
// bytes, never the copy. Every later setup removes the copy while it still
// matches that digest, and doctor names it. Nothing else is delete authority.
// Before the report, each copy's receipt is written with revoked false,
// awaiting the App's record: no delete authority until the App answers that
// it holds the record, so a report that landed never leaves a copy with
// neither removal nor receipt.

type legacyCopyReceipt struct {
	Service string `json:"service"`
	Digest  string `json:"digest"`  // sha256 of the copy proven revoked and recorded
	Revoked bool   `json:"revoked"` // false: awaiting the App's record
}

func legacyCopyReceiptsFile(paths runtimePaths) string {
	return filepath.Join(paths.ConfigDir, "legacy-link-copy-receipts.json")
}

func legacyCopyDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// legacyCopyReceipts: each profile id's copies still to remove.
func legacyCopyReceipts(paths runtimePaths) (map[string][]legacyCopyReceipt, error) {
	receipts := map[string][]legacyCopyReceipt{}
	if paths.ConfigDir == "" {
		return receipts, nil
	}
	raw, err := os.ReadFile(legacyCopyReceiptsFile(paths))
	if errors.Is(err, os.ErrNotExist) {
		return receipts, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &receipts); err != nil {
		return nil, fmt.Errorf("%s: %w", legacyCopyReceiptsFile(paths), err)
	}
	if receipts == nil { // null
		receipts = map[string][]legacyCopyReceipt{}
	}
	return receipts, nil
}

// deleteProvenLegacyLinkCopies removes exactly each copy (its verified bytes);
// the ones it can't remove, and keep, become account's receipt.
func deleteProvenLegacyLinkCopies(paths runtimePaths, account string, proven []legacyLinkCopy, keep []legacyCopyReceipt) error {
	backend, err := legacyCleanupBackend()
	exact, ok := backend.(exactOAuthSecretBackend)
	if err == nil && !ok {
		err = errors.New("this secure storage can't remove a sign-in copy exactly")
	}
	left := slices.Clone(keep)
	var failed []error
	for _, c := range proven {
		deleteErr := err
		if deleteErr == nil {
			deleteErr = exact.DeleteExact(context.Background(), c.service, c.account, c.value, SecretStoreForbidUI)
		}
		if deleteErr != nil {
			left = append(left, legacyCopyReceipt{Service: c.service, Digest: legacyCopyDigest(c.value), Revoked: true})
			failed = append(failed, fmt.Errorf("%s: %w", c.service, deleteErr))
		}
	}
	return errors.Join(append(failed, saveLegacyCopyReceipt(paths, account, left))...)
}

// retryLegacyCopyReceipts removes each receipt's copy while it still holds
// the bytes the receipt names; a copy changed or gone since only drops it.
func retryLegacyCopyReceipts(paths runtimePaths, account string) error {
	receipts, err := legacyCopyReceipts(paths)
	if err != nil || len(receipts[account]) == 0 {
		return err
	}
	backend, err := legacyCleanupBackend()
	if err != nil {
		return err
	}
	var proven []legacyLinkCopy
	var awaiting []legacyCopyReceipt
	for _, r := range receipts[account] {
		switch {
		case r.Digest == "" || !slices.ContainsFunc(legacyLinkServices, func(s string) bool {
			return r.Service == oauthLegacyCleanupServicePrefix+string(legacyLinkState(s))
		}):
			continue
		case !r.Revoked:
			awaiting = append(awaiting, r)
			continue
		}
		value, found, err := backend.Get(context.Background(), r.Service, account, SecretStoreForbidUI)
		if err != nil {
			return err
		}
		if found && legacyCopyDigest(value) == r.Digest {
			proven = append(proven, legacyLinkCopy{account: account, service: r.Service, value: value})
		}
	}
	return deleteProvenLegacyLinkCopies(paths, account, proven, awaiting)
}

// awaitLegacyCopyRecord replaces account's receipt with one awaiting the
// App's record for each copy proven revoked.
func awaitLegacyCopyRecord(paths runtimePaths, account string, proven []legacyLinkCopy) error {
	if len(proven) == 0 {
		return nil
	}
	awaiting := make([]legacyCopyReceipt, len(proven))
	for i, c := range proven {
		awaiting[i] = legacyCopyReceipt{Service: c.service, Digest: legacyCopyDigest(c.value)}
	}
	return saveLegacyCopyReceipt(paths, account, awaiting)
}

// confirmAwaitedLegacyCopies reports again a sign-out an earlier setup proved
// (each copy awaiting the App's record was revoked before it was written;
// legacy_cleanup_complete is idempotent on the App). Once the App answers with
// its record, those receipts become delete authority and their copies go;
// recorded counts them. An App that can't answer (one rolled back) keeps them.
func confirmAwaitedLegacyCopies(paths runtimePaths, cfg runtimeConfig) (recorded int, err error) {
	receipts, err := legacyCopyReceipts(paths)
	awaiting := slices.ContainsFunc(receipts[cfg.ProfileID], func(r legacyCopyReceipt) bool { return !r.Revoked })
	if err != nil || !awaiting {
		return 0, err
	}
	if err := legacyCleanupReport(paths, cfg); err != nil {
		return 0, err
	}
	confirmed := slices.Clone(receipts[cfg.ProfileID])
	for i := range confirmed {
		if !confirmed[i].Revoked {
			confirmed[i].Revoked = true
			recorded++
		}
	}
	if err := saveLegacyCopyReceipt(paths, cfg.ProfileID, confirmed); err != nil {
		return recorded, err
	}
	return recorded, retryLegacyCopyReceipts(paths, cfg.ProfileID)
}

// saveLegacyCopyReceipt replaces account's receipt with left, under the
// client mutation lock and only while a profile still holds account (purge
// and server remove hold that lock while they remove one).
func saveLegacyCopyReceipt(paths runtimePaths, account string, left []legacyCopyReceipt) error {
	if paths.ConfigDir == "" {
		return errors.New("no configuration folder for the receipt")
	}
	return withClientMutationLockWaiting(paths, func() error {
		receipts, err := legacyCopyReceipts(paths)
		if err != nil {
			return err
		}
		if _, had := receipts[account]; !had && len(left) == 0 {
			return nil
		}
		delete(receipts, account)
		if len(left) > 0 {
			doc, err := loadConfigDocumentOrEmpty(paths.ConfigFile)
			if err != nil {
				return err
			}
			if slices.ContainsFunc(doc.profileNames(), func(name string) bool { cfg, _ := doc.flatProfile(name); return cfg.ProfileID == account }) {
				receipts[account] = left
			}
		}
		if len(receipts) == 0 {
			if err := os.Remove(legacyCopyReceiptsFile(paths)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			return nil
		}
		raw, err := json.MarshalIndent(receipts, "", "  ")
		if err != nil {
			return err
		}
		return writeFileAtomic(legacyCopyReceiptsFile(paths), append(raw, '\n'), 0o600)
	})
}
