package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type legacyCleanupFixture struct {
	backend  *memoryOAuthSecretBackend
	legacy   map[string]string // 0.31 items by service
	locked   bool              // reading an old item needs a macOS prompt
	reads    []string          // "<ui>:<service>"
	revoked  []string          // refresh tokens, in order
	reported int
	events   []string
}

func newLegacyCleanupFixture(t *testing.T) *legacyCleanupFixture {
	t.Helper()
	f := &legacyCleanupFixture{backend: newMemoryOAuthSecretBackend(), legacy: map[string]string{}}
	saved := []any{legacyCleanupApplies, legacyLinkSecretRead, legacyCleanupBackend, legacyLinkRevoke, legacyCleanupPending, legacyCleanupReport, legacyCleanupPromptContext}
	t.Cleanup(func() {
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
	outcome, revoked := finishLegacyLinkCleanup(runtimePaths{}, runtimeConfig{ProfileID: "default"}, prompt, func() { intros++ })
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
	if outcome, _ := finishLegacyLinkCleanup(runtimePaths{}, runtimeConfig{}, false, nil); outcome != legacyCleanupComplete || len(f.reads) != 0 {
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

func TestLegacyLinkCleanupRevokesEveryOldSignInOnceAndKeepsCopies(t *testing.T) {
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

	// A later setup verifies the kept copies again and never asks for the
	// old items, which stay where they are.
	f.reads, f.revoked = nil, nil
	outcome, revoked, intros = f.run(true)
	if outcome != legacyCleanupComplete || revoked != 2 || intros != 0 {
		t.Fatalf("rerun: outcome %d, revoked %d, intros %d", outcome, revoked, intros)
	}
	for _, read := range f.reads {
		if read != string(SecretStoreForbidUI)+":"+oauthSecretPendingService {
			t.Fatalf("rerun read an old item that has a copy: %v", f.reads)
		}
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
	newLegacyCleanupFixture(t)
	legacyCleanupReport = func(runtimePaths, runtimeConfig) error { return errors.New("offline") }
	var out strings.Builder
	note := printLegacyLinkCleanup(&out, runtimePaths{}, runtimeConfig{ProfileID: "default"}, true)
	if out.Len() != 0 || !strings.Contains(note, "run: ha-nova setup") || !strings.Contains(note, "Everything else works") {
		t.Fatalf("printed %q, closing line %q", out.String(), note)
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
