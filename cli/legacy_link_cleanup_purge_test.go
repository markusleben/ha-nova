package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An App without the window contract: setup never reads /gate/v1/me, an old
// item, or reports a cleanup (spec §5.3, a 0.32 CLI before the App update).
func TestLegacyLinkCleanupAsksNothingOfAnAppWithoutTheWindow(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	f.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	legacyCleanupPending = legacyCleanupPendingOnApp
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/health" {
			_, _ = w.Write([]byte(`{"ok":true,"data":{"gate":1,"window":0}}`))
			return
		}
		http.Error(w, "unexpected", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	saved := legacyCleanupSession
	t.Cleanup(func() { legacyCleanupSession = saved })
	legacyCleanupSession = func(paths runtimePaths, cfg runtimeConfig) (*relaySession, error) {
		ctx, cancel := context.WithCancel(context.Background())
		return relaySessionOn(paths, cfg, relayTransportSelection{BaseURL: server.URL, Client: server.Client(), Credential: "key", DeviceMode: true, Via: relayViaLocal}, ctx, cancel), nil
	}
	outcome, _, intros := f.run(true)
	if outcome != legacyCleanupNotNeeded || intros != 0 || len(f.reads) != 0 || f.reported != 0 {
		t.Fatalf("outcome %d, intros %d, reads %v, reported %d", outcome, intros, f.reads, f.reported)
	}
	if strings.Join(paths, ",") != "/health" {
		t.Fatalf("requests %v, want only /health", paths)
	}
}

func TestLegacyLinkCopiesArePurgedOnlyOnceRevoked(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"servers":{"home":{"profile_id":"default"},"work":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	copyService := oauthLegacyCleanupServicePrefix + "current"
	value := legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current")
	_ = f.backend.Set(ctx, copyService, "default", value, SecretStoreForbidUI)

	paths := runtimePaths{ConfigFile: config}
	purge := func() error {
		copies, err := legacyLinkCopiesForUninstall(paths)
		if err != nil {
			return err
		}
		return revokeLegacyLinkCopies(copies)
	}
	if err := requireLegacyLinkCopiesAbsent(paths); err == nil {
		t.Fatal("final proof missed a copy")
	}
	revoke := legacyLinkRevoke
	legacyLinkRevoke = func(context.Context, OAuthSecretEnvelope) error { return errors.New("Home Assistant did not answer") }
	if err := purge(); err == nil {
		t.Fatal("purge went on with a copy it couldn't revoke")
	}
	if _, kept, _ := f.backend.Get(ctx, copyService, "default", SecretStoreForbidUI); !kept {
		t.Fatal("removed a copy whose sign-in may still be live")
	}
	legacyLinkRevoke = revoke
	if err := purge(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := requireLegacyLinkCopiesAbsent(paths); err != nil {
		t.Fatalf("final proof after the purge: %v", err)
	}
	if _, kept, _ := f.backend.Get(ctx, copyService, "default", SecretStoreForbidUI); kept || strings.Join(f.revoked, ",") != "old-current" {
		t.Fatalf("copy kept %v, revoked %v", kept, f.revoked)
	}
	for _, read := range f.reads {
		t.Fatalf("purge read an old 0.31 item: %v", read)
	}

	// A copy it can't read stops the purge before anything is revoked.
	_ = f.backend.Set(ctx, oauthLegacyCleanupServicePrefix+"pending", "default", "{", SecretStoreForbidUI)
	f.revoked = nil
	if _, err := legacyLinkCopiesForUninstall(paths); err == nil {
		t.Fatal("read a corrupt copy as nothing to revoke")
	}
}

// A Mac that moved its key to a file after a cut cleanup still holds the
// copy in the Keychain: purge and server remove find it there.
func TestLegacyLinkCopiesAreFoundAfterTheKeyMovedToAFile(t *testing.T) {
	f := newLegacyCleanupFixture(t)
	legacyCleanupApplies = func() bool { return false }
	dir := t.TempDir()
	paths := runtimePaths{ConfigFile: filepath.Join(dir, "config.json")}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"servers":{"home":{"profile_id":"default"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = f.backend.Set(context.Background(), oauthLegacyCleanupServicePrefix+"current", "default", legacyTestEnvelope(OAuthSecretCurrent, "default", "old-current"), SecretStoreForbidUI)
	legacyCopiesOnThisOS = false // Windows and Linux: no Keychain, nothing to find
	if copies, err := legacyLinkCopiesForUninstall(paths); err != nil || len(copies) != 0 {
		t.Fatalf("off a Mac: %v %v", copies, err)
	}
	legacyCopiesOnThisOS = true
	if copies, err := legacyLinkCopiesForUninstall(paths); err != nil || len(copies) != 1 {
		t.Fatalf("a Mac on a file key: %v %v", copies, err)
	}
	if err := requireLegacyLinkCopiesAbsent(paths); err == nil {
		t.Fatal("the final proof missed the copy")
	}
}

// A copy is written under the client mutation lock and only for a profile
// that still exists, so a purge or server remove that finished meanwhile
// leaves no copy behind it.
func TestLegacyLinkCopyIsKeptOnlyForALiveProfile(t *testing.T) {
	keep := legacyLinkCopyKeep
	newLegacyCleanupFixture(t)
	dir := t.TempDir()
	paths := runtimePaths{Home: dir, ConfigDir: filepath.Join(dir, "config", "ha-nova"), LocalDataDir: filepath.Join(dir, "local", "ha-nova")}
	paths.ConfigFile = filepath.Join(paths.ConfigDir, "config.json")
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(`{"servers":{"home":{"profile_id":"default"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	writes := 0
	write := func() error { writes++; return nil }
	if err := keep(paths, "default", write); err != nil || writes != 1 {
		t.Fatalf("a live profile: %v, %d writes", err, writes)
	}
	if err := keep(paths, "gone", write); err == nil || writes != 1 {
		t.Fatalf("a removed profile: %v, %d writes", err, writes)
	}
	release, ok := acquireAutoRepairLock(paths)
	if !ok {
		t.Fatal("lock")
	}
	if err := keep(paths, "default", write); err == nil || writes != 1 {
		t.Fatalf("while a removal holds the lock: %v, %d writes", err, writes)
	}
	release()
	if err := markInstallLifecycleStopped(paths); err != nil { // a setup reinstalling after a plain uninstall keeps its copy
		t.Fatal(err)
	}
	if err := keep(paths, "default", write); err != nil || writes != 2 {
		t.Fatalf("a reinstall setup: %v, %d writes", err, writes)
	}
}

type lockedOAuthSecretBackend struct{ OAuthSecretBackend }

func (lockedOAuthSecretBackend) Get(context.Context, string, string, SecretStoreUIPolicy) (string, bool, error) {
	return "", false, errors.New("the login keychain is locked")
}

// A copy server remove can't read (a locked Keychain over SSH) stops it
// before the pairing is revoked or deleted, so the profile keeps working.
func TestServerRemoveStopsBeforeThePairingWhenACopyCantBeRead(t *testing.T) {
	config := strings.Replace(testV2TwoProfileConfig, `"default_server": "default"`, `"default_server": "cabin"`, 1)
	paths := setupServerCommandTest(t, config)
	if err := secretSet(deviceCredentialServiceForProfile("cabin"), testProfileCredentialB); err != nil {
		t.Fatal(err)
	}
	backend, onOS := legacyCleanupBackend, legacyCopiesOnThisOS
	t.Cleanup(func() { legacyCleanupBackend, legacyCopiesOnThisOS = backend, onOS })
	legacyCopiesOnThisOS = true
	legacyCleanupBackend = func() (OAuthSecretBackend, error) {
		return lockedOAuthSecretBackend{newMemoryOAuthSecretBackend()}, nil
	}
	revokedAt := stubServerRevoke(t)
	stubServerCommandStdin(t, "cabin\n")
	exit, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"remove", "cabin"}) })
	if exit != 1 || !strings.Contains(out, "old HA NOVA version out of Home Assistant Link") {
		t.Fatalf("exit %d, output:\n%s", exit, out)
	}
	if len(*revokedAt) != 0 {
		t.Fatalf("revoked the pairing before the copy check: %v", *revokedAt)
	}
	if _, ok, _ := readCredentialSlot(deviceCredentialServiceForProfile("cabin")); !ok {
		t.Fatal("deleted the pairing before the copy check")
	}
}
