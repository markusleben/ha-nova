package main

import (
	"errors"
	"os"
	"slices"
	"testing"
)

// awaiting: every receipt for the profile still waits for the App's record.
func (f *legacyCleanupFixture) awaiting() bool {
	receipts, _ := legacyCopyReceipts(f.paths)
	return len(receipts["default"]) > 0 && !slices.ContainsFunc(receipts["default"], func(r legacyCopyReceipt) bool { return r.Revoked })
}

// CLI-04: the App recorded the sign-out, then neither the copy's removal nor
// its receipt landed. The note written before the report keeps the retry:
// setup says the removal didn't finish, an App that can't answer keeps the
// note, and once the App answers the report again the copy goes, with
// nothing signed out again, even after the computer left the Keychain.
func TestLegacyLinkCleanupKeepsItsRetryWhenNeitherRemovalNorReceiptLands(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	file := legacyCopyReceiptsFile(f.paths)
	var noted []byte
	legacyCleanupBackend = func() (OAuthSecretBackend, error) { return failingDeleteBackend{f.backend}, nil }
	legacyCleanupReport = func(runtimePaths, runtimeConfig) error {
		f.reported++
		noted, _ = os.ReadFile(file)
		return os.WriteFile(file, []byte("{"), 0o600) // the receipt after the report can't be written
	}
	msg := legacyLinkCleanupInSetup(func(setupBlock) {}, f.paths, runtimeConfig{ProfileID: "default"}, true)
	if msg.Key != "setup.cleanup.copy_left" || f.reported != 1 || len(f.revoked) != 1 || f.copies() != 1 {
		t.Fatalf("note %+v, reported %d, revoked %v, copies %d", msg, f.reported, f.revoked, f.copies())
	}
	if err := os.WriteFile(file, noted, 0o600); err != nil { // what the failed write left
		t.Fatal(err)
	}
	if !f.awaiting() {
		t.Fatalf("no note awaiting the App: %s", noted)
	}

	legacyCleanupBackend = func() (OAuthSecretBackend, error) { return f.backend, nil }
	legacyCleanupPending = func(runtimePaths, runtimeConfig) (bool, error) { return false, nil }
	legacyCleanupReport = func(runtimePaths, runtimeConfig) error { f.reported++; return errors.New("no such route") }
	if outcome, _, _ := f.run(true); outcome != legacyCleanupNotNeeded || f.copies() != 1 || !f.awaiting() {
		t.Fatalf("an App that can't answer: outcome %d, copies %d, awaiting %v", outcome, f.copies(), f.awaiting())
	}

	legacyCleanupApplies, legacyCopiesOnThisOS = func() bool { return false }, true // now a file credential on this Mac
	legacyCleanupReport = func(runtimePaths, runtimeConfig) error { f.reported++; return nil }
	f.revoked, f.reported = nil, 0
	if outcome, recorded, _ := f.run(true); outcome != legacyCleanupComplete || recorded != 1 || f.copies() != 0 || f.receipt() || f.reported != 1 || len(f.revoked) != 0 {
		t.Fatalf("outcome %d, recorded %d, copies %d, receipt %v, reported %d, revoked again %v", outcome, recorded, f.copies(), f.receipt(), f.reported, f.revoked)
	}
}

// Without its note the report isn't sent: the App keeps the cleanup open and
// the next setup resumes from the copy.
func TestLegacyLinkCleanupReportsOnlyAfterItsNote(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	if err := os.WriteFile(legacyCopyReceiptsFile(f.paths), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if outcome, revoked, _ := f.run(true); outcome != legacyCleanupIncomplete || revoked != 1 || f.reported != 0 || f.copies() != 1 || f.cause == nil {
		t.Fatalf("outcome %d, revoked %d, reported %d, copies %d, cause %v", outcome, revoked, f.reported, f.copies(), f.cause)
	}
}
