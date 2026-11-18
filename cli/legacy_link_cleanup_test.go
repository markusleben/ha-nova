package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

type legacyCleanupFixture struct {
	paths    runtimePaths // its receipts (legacy_link_copy_receipt.go)
	backend  *memoryOAuthSecretBackend
	legacy   map[string]string // 0.31 items by service
	locked   bool              // reading an old item needs a macOS prompt
	reads    []string          // "<ui>:<service>"
	revoked  []string          // refresh tokens, in order
	reported int
	events   []string
	cause    error // why the last run stopped
}

func newLegacyCleanupFixture(t *testing.T) *legacyCleanupFixture {
	t.Helper()
	f := &legacyCleanupFixture{paths: runtimePaths{ConfigDir: t.TempDir()}, backend: newMemoryOAuthSecretBackend(), legacy: map[string]string{}}
	saved := []any{legacyCleanupApplies, legacyLinkSecretRead, legacyCleanupBackend, legacyLinkRevoke, legacyCleanupPending, legacyCleanupReport, legacyCleanupPromptContext}
	keep, onOS := legacyLinkCopyKeep, legacyCopiesOnThisOS
	legacyLinkCopyKeep = func(_ runtimePaths, _ string, write func() error) error { return write() } // its guard: TestLegacyLinkCopyIsKeptOnlyForALiveProfile
	t.Cleanup(func() {
		legacyLinkCopyKeep, legacyCopiesOnThisOS = keep, onOS
		legacyCleanupPromptContext = saved[6].(func() bool)
		legacyCleanupApplies = saved[0].(func() bool)
		legacyLinkSecretRead = saved[1].(func(context.Context, string, string, SecretStoreUIPolicy) (string, bool, error))
		legacyCleanupBackend = saved[2].(func() (OAuthSecretBackend, error))
		legacyLinkRevoke = saved[3].(func(context.Context, OAuthSecretEnvelope) error)
		legacyCleanupPending = saved[4].(func(runtimePaths, runtimeConfig) (bool, error))
		legacyCleanupReport = saved[5].(func(runtimePaths, runtimeConfig) error)
	})
	legacyCleanupApplies = func() bool { return true }
	legacyCleanupPromptContext = func() bool { return true }
	legacyCleanupBackend = func() (OAuthSecretBackend, error) { return f.backend, nil }
	legacyCleanupPending = func(runtimePaths, runtimeConfig) (bool, error) { return true, nil }
	legacyLinkSecretRead = func(_ context.Context, service, account string, ui SecretStoreUIPolicy) (string, bool, error) {
		f.reads = append(f.reads, string(ui)+":"+service)
		value, ok := f.legacy[service]
		if ok && f.locked && ui != SecretStoreAllowUI {
			return "", false, newCloudError(CloudErrSecretUIForbidden, "read", nil)
		}
		return value, ok, nil
	}
	legacyLinkRevoke = func(_ context.Context, envelope OAuthSecretEnvelope) error {
		if _, kept, _ := f.backend.Get(context.Background(), oauthLegacyCleanupServicePrefix+string(envelope.State), envelope.ProfileID, SecretStoreForbidUI); !kept {
			t.Fatalf("revoked %s before keeping its copy", envelope.State)
		}
		f.revoked = append(f.revoked, envelope.RefreshToken)
		return nil
	}
	legacyCleanupReport = func(runtimePaths, runtimeConfig) error { f.reported++; return nil }
	return f
}

func legacyTestEnvelope(state OAuthSecretState, account, token string) string {
	envelope := testOAuthEnvelope(token, "token-id", "user-id", "relay-id")
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	envelope.SchemaVersion, envelope.State, envelope.Generation, envelope.ProfileID = oauthSecretSchema, state, strings.Repeat("ab", 16), account
	envelope.CreatedAt, envelope.UpdatedAt = at, at
	raw, _ := json.Marshal(envelope)
	return string(raw)
}

func (f *legacyCleanupFixture) run(prompt bool) (legacyCleanupOutcome, int, int) {
	intros := 0
	outcome, revoked, cause := finishLegacyLinkCleanup(f.paths, runtimeConfig{ProfileID: "default"}, prompt, func() { intros++ })
	f.cause = cause
	return outcome, revoked, intros
}

func TestLegacyLinkCleanupSkipsWhereNothingTurnsOn(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	legacyCleanupApplies = func() bool { return false }
	if outcome, _, _ := f.run(true); outcome != legacyCleanupNotNeeded || f.reported != 0 || len(f.reads) != 0 {
		t.Fatalf("not a Keychain pairing: outcome %d, reported %d, reads %v", outcome, f.reported, f.reads)
	}
	legacyCleanupApplies = func() bool { return true }
	legacyCleanupPending = func(runtimePaths, runtimeConfig) (bool, error) { return false, nil }
	if outcome, _, _ := f.run(true); outcome != legacyCleanupNotNeeded || f.reported != 0 || len(f.reads) != 0 {
		t.Fatalf("cleanup not pending on the App: outcome %d, reported %d, reads %v", outcome, f.reported, f.reads)
	}
	legacyCleanupPending = func(runtimePaths, runtimeConfig) (bool, error) { return false, errors.New("offline") }
	if outcome, _, _ := f.run(true); outcome != legacyCleanupIncomplete || f.reported != 0 {
		t.Fatalf("unreachable App: outcome %d, reported %d", outcome, f.reported)
	}
}

func TestLegacyLinkCleanupReportsAProfileWithoutOldSignInsWithoutAsking(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	outcome, revoked, intros := f.run(false)
	if outcome != legacyCleanupComplete || revoked != 0 || intros != 0 || f.reported != 1 {
		t.Fatalf("outcome %d, revoked %d, intros %d, reported %d", outcome, revoked, intros, f.reported)
	}
	for _, read := range f.reads {
		if !strings.HasPrefix(read, string(SecretStoreForbidUI)+":") {
			t.Fatalf("an absent item was read with a prompt: %v", f.reads)
		}
	}
	if len(f.reads) != 3 {
		t.Fatalf("reads %v, want the three old Link items", f.reads)
	}
	f.reads = nil
	if outcome, _, _ := finishLegacyLinkCleanup(f.paths, runtimeConfig{}, false, nil); outcome != legacyCleanupComplete || len(f.reads) != 0 {
		t.Fatalf("a profile that never had Link: outcome %d, reads %v", outcome, f.reads)
	}
}

func TestLegacyLinkCleanupNeverAsksOutsideSetup(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	f.locked = true
	outcome, revoked, intros := f.run(false)
	if outcome != legacyCleanupIncomplete || revoked != 0 || intros != 0 || f.reported != 0 || len(f.revoked) != 0 {
		t.Fatalf("outcome %d, revoked %d, intros %d, reported %d", outcome, revoked, intros, f.reported)
	}
	for _, read := range f.reads {
		if strings.HasPrefix(read, string(SecretStoreAllowUI)) {
			t.Fatalf("asked macOS outside setup: %v", f.reads)
		}
	}
}

func TestLegacyLinkCleanupNeverAsksWithoutADesktopSession(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	f.locked = true
	legacyCleanupPromptContext = func() bool { return false }
	if outcome, _, intros := f.run(true); outcome != legacyCleanupIncomplete || intros != 0 || f.reported != 0 {
		t.Fatalf("over SSH: outcome %d, intros %d, reported %d", outcome, intros, f.reported)
	}
	for _, read := range f.reads {
		if strings.HasPrefix(read, string(SecretStoreAllowUI)) {
			t.Fatalf("asked macOS without a desktop session: %v", f.reads)
		}
	}
}

func TestLegacyLinkCleanupRevokesEveryOldSignInOnceAndRemovesItsCopyOnceRecorded(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	f.legacy[oauthSecretRetiringService] = legacyTestEnvelope(OAuthSecretRetiring, "default", "old-retiring")
	f.locked = true
	outcome, revoked, intros := f.run(true)
	if outcome != legacyCleanupComplete || revoked != 2 || intros != 1 || f.reported != 1 {
		t.Fatalf("outcome %d, revoked %d, intros %d, reported %d", outcome, revoked, intros, f.reported)
	}
	if strings.Join(f.revoked, ",") != "old-current,old-retiring" {
		t.Fatalf("revoked %v", f.revoked)
	}
	prompts := 0
	for _, read := range f.reads {
		if strings.HasPrefix(read, string(SecretStoreAllowUI)) {
			prompts++
		}
	}
	if prompts != 2 {
		t.Fatalf("prompts %d, want one per present item: %v", prompts, f.reads)
	}

	// Recorded by the App: the copies go, and no receipt is left.
	if f.copies() != 0 || f.receipt() {
		t.Fatalf("copies %d, receipt %v after the App recorded the cleanup", f.copies(), f.receipt())
	}
}

func TestLegacyLinkCleanupRetryAfterAFailedRevocationAsksNothing(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	f.locked = true
	revoke := legacyLinkRevoke
	legacyLinkRevoke = func(context.Context, OAuthSecretEnvelope) error { return errors.New("Home Assistant did not answer") }
	if outcome, _, intros := f.run(true); outcome != legacyCleanupIncomplete || intros != 1 || f.reported != 0 {
		t.Fatalf("failed revoke: outcome %d, intros %d, reported %d", outcome, intros, f.reported)
	}
	legacyLinkRevoke = revoke
	f.reads = nil
	outcome, revoked, intros := f.run(true)
	if outcome != legacyCleanupComplete || revoked != 1 || intros != 0 || f.reported != 1 {
		t.Fatalf("retry: outcome %d, revoked %d, intros %d, reported %d", outcome, revoked, intros, f.reported)
	}
	for _, read := range f.reads {
		if strings.HasSuffix(read, oauthSecretCurrentService) {
			t.Fatalf("retry asked for the old item again: %v", f.reads)
		}
	}
}

func TestLegacyLinkCleanupFailsClosed(t *testing.T) {
	cases := map[string]func(*legacyCleanupFixture){
		"denied prompt": func(f *legacyCleanupFixture) {
			f.legacy[oauthSecretCurrentService] = "x"
			legacyLinkSecretRead = func(context.Context, string, string, SecretStoreUIPolicy) (string, bool, error) {
				return "", false, newCloudError(CloudErrSecretUIForbidden, "read", nil)
			}
		},
		"unreadable store": func(f *legacyCleanupFixture) {
			legacyLinkSecretRead = func(context.Context, string, string, SecretStoreUIPolicy) (string, bool, error) {
				return "", false, newCloudError(CloudErrSecretStore, "read", nil)
			}
		},
		"corrupt item": func(f *legacyCleanupFixture) {
			f.legacy[oauthSecretPendingService] = "{"
		},
		"another profile's item": func(f *legacyCleanupFixture) {
			f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "other", "old")
		},
		"item in the wrong state": func(f *legacyCleanupFixture) {
			f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretRetiring, "default", "old")
		},
		"copy not kept": func(f *legacyCleanupFixture) {
			f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old")
			f.backend.fail = func(op, _ string) error {
				if op == "set" {
					return errors.New("full")
				}
				return nil
			}
		},
		"report refused": func(f *legacyCleanupFixture) {
			legacyCleanupReport = func(runtimePaths, runtimeConfig) error { return errors.New("404") }
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newLegacyCleanupFixture(t)
			setup(f)
			if outcome, _, _ := f.run(true); outcome != legacyCleanupIncomplete {
				t.Fatalf("outcome %d, want incomplete", outcome)
			}
			if name != "report refused" && f.reported != 0 {
				t.Fatalf("reported complete")
			}
			if name != "report refused" && len(f.revoked) != 0 {
				t.Fatalf("revoked %v from an item it couldn't keep or trust", f.revoked)
			}
		})
	}
}

func TestLegacyLinkCleanupTellsTheRetry(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	legacyCleanupReport = func(runtimePaths, runtimeConfig) error { return errors.New("offline") }
	var out strings.Builder
	note := printLegacyLinkCleanup(&out, f.paths, runtimeConfig{ProfileID: "default"}, true).plain()
	if out.Len() != 0 || !strings.Contains(note, "run: ha-nova setup") || !strings.Contains(note, "Everything else works") ||
		!strings.Contains(note, "What stopped it: the NOVA App didn't record it (offline).") {
		t.Fatalf("printed %q, closing line %q", out.String(), note)
	}
}

// macOS refuses the old version's item without a prompt by more than one
// code (errSecAuthFailed maps to a locked store): in a person's setup, the
// one prompt follows any refusal, after the introduction (rc4 on a real Mac
// stopped silently here).
func TestLegacyLinkCleanupAsksAfterAnyRefusalWithoutUI(t *testing.T) {
	for _, refusal := range []CloudErrorCode{CloudErrSecretStoreLocked, CloudErrTimeout, CloudErrSecretStore, CloudErrSecretUIForbidden} {
		f := newLegacyCleanupFixture(t)
		f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
		read := legacyLinkSecretRead
		legacyLinkSecretRead = func(ctx context.Context, service, account string, ui SecretStoreUIPolicy) (string, bool, error) {
			if ui == SecretStoreForbidUI && f.legacy[service] != "" {
				f.reads = append(f.reads, string(ui)+":"+service)
				return "", false, newCloudError(refusal, "read", nil)
			}
			return read(ctx, service, account, ui)
		}
		outcome, revoked, intros := f.run(true)
		if outcome != legacyCleanupComplete || revoked != 1 || intros != 1 || f.reported != 1 {
			t.Fatalf("%s: outcome %d, revoked %d, intros %d, reported %d, reads %v", refusal, outcome, revoked, intros, f.reported, f.reads)
		}
		if outcome, _, intros := f.runFresh(t, refusal, false); outcome != legacyCleanupIncomplete || intros != 0 ||
			!strings.Contains(f.cause.Error(), "macOS didn't hand over the old sign-in") {
			t.Fatalf("%s outside setup: outcome %d, intros %d, cause %v", refusal, outcome, intros, f.cause)
		}
	}
}

// runFresh: a new run on the same fixture whose old item macOS refuses with code.
func (f *legacyCleanupFixture) runFresh(t *testing.T, code CloudErrorCode, prompt bool) (legacyCleanupOutcome, int, int) {
	t.Helper()
	_ = f.backend.Delete(context.Background(), oauthLegacyCleanupServicePrefix+string(OAuthSecretCurrent), "default", SecretStoreForbidUI)
	legacyLinkSecretRead = func(context.Context, string, string, SecretStoreUIPolicy) (string, bool, error) {
		return "", false, newCloudError(code, "read", nil)
	}
	return f.run(prompt)
}

// Every incomplete run names what stopped it.
func TestLegacyLinkCleanupNamesWhatStoppedIt(t *testing.T) {
	cases := map[string]struct {
		setup func(*legacyCleanupFixture)
		want  string
	}{
		"App answer": {func(*legacyCleanupFixture) {
			legacyCleanupPending = func(runtimePaths, runtimeConfig) (bool, error) { return false, errors.New("timeout") }
		}, "the NOVA App's answer couldn't be read (timeout)"},
		"no desktop": {func(f *legacyCleanupFixture) {
			f.legacy[oauthSecretCurrentService], f.locked = legacyTestEnvelope(OAuthSecretCurrent, "default", "old"), true
			legacyCleanupPromptContext = func() bool { return false }
		}, "only in a terminal on this Mac's own desktop"},
		"revocation": {func(f *legacyCleanupFixture) {
			f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old")
			legacyLinkRevoke = func(context.Context, OAuthSecretEnvelope) error { return errors.New("offline") }
		}, "Home Assistant didn't confirm the old sign-in is signed out (offline)"},
		"canceled": {func(f *legacyCleanupFixture) {
			f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old")
			legacyLinkSecretRead = func(context.Context, string, string, SecretStoreUIPolicy) (string, bool, error) {
				return "", false, newCloudError(CloudErrSecretPromptCanceled, "read", nil)
			}
		}, "the macOS password prompt was canceled"},
	}
	for name, c := range cases {
		f := newLegacyCleanupFixture(t)
		c.setup(f)
		if outcome, _, _ := f.run(true); outcome != legacyCleanupIncomplete || f.cause == nil || !strings.Contains(f.cause.Error(), c.want) {
			t.Fatalf("%s: outcome %d, cause %v", name, outcome, f.cause)
		}
	}
}

func TestLegacyLinkCleanupWorkerReadsOnlyTheOldLinkItems(t *testing.T) {
	request := nativeSecretWorkerRequest{SchemaVersion: nativeSecretWorkerSchema, Operation: nativeSecretLegacyGet, UI: SecretStoreForbidUI, Account: "default"}
	for _, service := range legacyLinkServices {
		request.Service = service
		if err := validateNativeSecretWorkerRequest(request); err != nil {
			t.Fatalf("%s refused: %v", service, err)
		}
	}
	for _, service := range []string{oauthLegacyCleanupServicePrefix + "current", oauthSecretPreflightServicePrefix + strings.Repeat("ab", 16), deviceCredentialService, keyringServiceName} {
		request.Service = service
		if validateNativeSecretWorkerRequest(request) == nil {
			t.Fatalf("legacy_get accepted %s", service)
		}
	}
	request.Service, request.Value = oauthSecretCurrentService, []byte("x")
	if validateNativeSecretWorkerRequest(request) == nil {
		t.Fatal("legacy_get accepted a value")
	}
}

func TestLegacyLinkCleanupCopiesLiveInTheNewRoot(t *testing.T) {
	for _, state := range []string{"current", "pending", "retiring"} {
		service := oauthLegacyCleanupServicePrefix + state
		if !validOAuthSecretBackendService(service) {
			t.Fatalf("%s refused", service)
		}
		if got, want := darwinKeychainSlot(service, "default"), darwinKeychainRoot+"/link-legacy-"+state+"/default"; got != want {
			t.Fatalf("slot %q, want %q", got, want)
		}
	}
	if validOAuthSecretBackendService(oauthLegacyCleanupServicePrefix + "other") {
		t.Fatal("an unknown copy accepted")
	}
}

func (f *legacyCleanupFixture) copies() int {
	n := 0
	for _, service := range legacyLinkServices {
		if _, found, _ := f.backend.Get(context.Background(), oauthLegacyCleanupServicePrefix+string(legacyLinkState(service)), "default", SecretStoreForbidUI); found {
			n++
		}
	}
	return n
}

func (f *legacyCleanupFixture) receipt() bool {
	receipts, err := legacyCopyReceipts(f.paths)
	return err != nil || len(receipts["default"]) > 0
}

func TestLegacyLinkCleanupKeepsTheCopyUntilTheAppRecordsIt(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	legacyCleanupReport = func(runtimePaths, runtimeConfig) error { return errors.New("the NOVA App did not answer") }
	if outcome, _, _ := f.run(true); outcome != legacyCleanupIncomplete || f.copies() != 1 || f.receipt() {
		t.Fatalf("outcome %d, copies %d, receipt %v: a retry resumes from the copy", outcome, f.copies(), f.receipt())
	}
}

type failingDeleteBackend struct{ *memoryOAuthSecretBackend }

func (failingDeleteBackend) DeleteExact(context.Context, string, string, string, SecretStoreUIPolicy) error {
	return errors.New("the Keychain is locked")
}

func TestLegacyLinkCleanupLeavesAReceiptAndRetriesWhateverTheAppSays(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	legacyCleanupBackend = func() (OAuthSecretBackend, error) { return failingDeleteBackend{f.backend}, nil }
	if outcome, _, _ := f.run(true); outcome != legacyCleanupComplete || f.copies() != 1 || !f.receipt() {
		t.Fatalf("outcome %d, copies %d, receipt %v: a copy setup couldn't remove leaves a receipt", outcome, f.copies(), f.receipt())
	}
	raw, _ := os.ReadFile(legacyCopyReceiptsFile(f.paths))
	if strings.Contains(string(raw), "old-current") || !strings.Contains(string(raw), oauthLegacyCleanupServicePrefix+string(OAuthSecretCurrent)) {
		t.Fatalf("the receipt names the item, never its secret: %s", raw)
	}
	legacyCleanupBackend = func() (OAuthSecretBackend, error) { return f.backend, nil }
	legacyCleanupPending = func(runtimePaths, runtimeConfig) (bool, error) {
		return false, errors.New("the NOVA App did not answer")
	}
	f.revoked = nil
	if f.run(true); f.copies() != 0 || f.receipt() || len(f.revoked) != 0 {
		t.Fatalf("copies %d, receipt %v, revoked again %v after the retry", f.copies(), f.receipt(), f.revoked)
	}
	if _, err := os.Stat(legacyCopyReceiptsFile(f.paths)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an empty receipts file stayed: %v", err)
	}
}

func TestLegacyLinkCleanupRemovesTheCopiesARecordedCleanupKept(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	if err := f.backend.Set(context.Background(), oauthLegacyCleanupServicePrefix+string(OAuthSecretCurrent), "default", legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current"), SecretStoreForbidUI); err != nil {
		t.Fatal(err)
	}
	legacyCleanupPending = func(runtimePaths, runtimeConfig) (bool, error) { return false, nil } // 0.32 recorded it and kept the copy
	if outcome, _, intros := f.run(true); outcome != legacyCleanupNotNeeded || intros != 0 || f.copies() != 0 || strings.Join(f.revoked, ",") != "old-current" {
		t.Fatalf("outcome %d, intros %d, copies %d, revoked %v", outcome, intros, f.copies(), f.revoked)
	}
}
