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

	revoke := legacyLinkRevoke
	legacyLinkRevoke = func(context.Context, OAuthSecretEnvelope) error { return errors.New("Home Assistant did not answer") }
	if err := purgeLegacyLinkCopiesForUninstall(runtimePaths{ConfigFile: config}); err == nil {
		t.Fatal("purge went on with a copy it couldn't revoke")
	}
	if _, kept, _ := f.backend.Get(ctx, copyService, "default", SecretStoreForbidUI); !kept {
		t.Fatal("removed a copy whose sign-in may still be live")
	}
	legacyLinkRevoke = revoke
	if err := purgeLegacyLinkCopiesForUninstall(runtimePaths{ConfigFile: config}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if _, kept, _ := f.backend.Get(ctx, copyService, "default", SecretStoreForbidUI); kept || strings.Join(f.revoked, ",") != "old-current" {
		t.Fatalf("copy kept %v, revoked %v", kept, f.revoked)
	}
	for _, read := range f.reads {
		t.Fatalf("purge read an old 0.31 item: %v", read)
	}
}
