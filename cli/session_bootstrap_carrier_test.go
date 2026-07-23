package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionBootstrapCarrierClaimIsSingleWinner(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if err := os.WriteFile(marker, []byte("0.6.1"+sessionBootstrapCarrierPendingSuffix+"\n"), 0o644); err != nil {
		t.Fatalf("write carrier marker: %v", err)
	}

	if !claimSessionBootstrapCarrier(paths) {
		t.Fatal("first carrier claimant must win")
	}
	if claimSessionBootstrapCarrier(paths) {
		t.Fatal("second carrier claimant must not run the carrier")
	}
	if !markerHasCarrierRunning(marker, "0.6.1") {
		t.Fatal("winning claim did not persist running state")
	}

	stale := time.Now().Add(-sessionBootstrapCarrierClaimTTL - time.Minute).Unix()
	if err := os.WriteFile(
		marker,
		[]byte(fmt.Sprintf("0.6.1%s%d\n", sessionBootstrapCarrierRunningPrefix, stale)),
		0o644,
	); err != nil {
		t.Fatalf("write stale carrier claim: %v", err)
	}
	if !claimSessionBootstrapCarrier(paths) {
		t.Fatal("stale carrier claim must be recoverable")
	}
	if claimSessionBootstrapCarrier(paths) {
		t.Fatal("renewed carrier claim must again have one winner")
	}

	future := time.Now().Add(time.Hour).Unix()
	if err := os.WriteFile(
		marker,
		[]byte(fmt.Sprintf("0.6.1%s%d\n", sessionBootstrapCarrierRunningPrefix, future)),
		0o644,
	); err != nil {
		t.Fatalf("write future carrier claim: %v", err)
	}
	if !claimSessionBootstrapCarrier(paths) {
		t.Fatal("future-dated carrier claim must be recoverable after clock rollback")
	}

	if err := os.WriteFile(
		marker,
		[]byte("0.6.1"+sessionBootstrapCarrierRunningPrefix+"malformed\n"),
		0o644,
	); err != nil {
		t.Fatalf("write malformed carrier claim: %v", err)
	}
	if !claimSessionBootstrapCarrier(paths) {
		t.Fatal("malformed carrier claim must be recoverable")
	}
}

func TestValidatedRelayCommandRepairsBeforeConfigOrAuth(t *testing.T) {
	paths := setupHealableInstall(t)
	codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	if _, err := installTreeClient(filepath.Dir(codexRoot), filepath.Join(paths.InstallRoot, "skills"), false); err != nil {
		t.Fatalf("seed copied Codex tree: %v", err)
	}
	bootstrap := filepath.Join(codexRoot, "ha-nova", "session-bootstrap.md")
	if err := os.Remove(bootstrap); err != nil {
		t.Fatalf("remove bootstrap: %v", err)
	}
	if err := os.Remove(paths.ConfigFile); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove config: %v", err)
	}

	if exit := runRelayCommand(paths, []string{"health"}); exit == 0 {
		t.Fatal("health without config must fail")
	}
	if !fileExists(bootstrap) {
		t.Fatal("validated local migration must not depend on Relay config or auth")
	}
	if !markerHasCarrierPending(filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker), "0.6.1") {
		t.Fatal("failed Relay preflight must leave the first-use carrier pending")
	}
}

func TestMigratedFirstUseCarriesCensusAskOnCurrentTask(t *testing.T) {
	paths := setupHealableInstall(t)
	codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	if _, err := installTreeClient(filepath.Dir(codexRoot), filepath.Join(paths.InstallRoot, "skills"), false); err != nil {
		t.Fatalf("seed copied Codex tree: %v", err)
	}
	if err := os.Remove(filepath.Join(codexRoot, "ha-nova", "session-bootstrap.md")); err != nil {
		t.Fatalf("remove bootstrap: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
		Version: "0.6.1",
		HTMLURL: "https://example.invalid/releases/v0.6.1",
	}, 0o644); err != nil {
		t.Fatalf("write current update cache: %v", err)
	}
	if err := saveState(paths, installState{
		SchemaVersion: stateSchemaVersion,
		Version:       "0.6.1",
	}); err != nil {
		t.Fatalf("save installed-state sentinel: %v", err)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			fmt.Fprint(w, `{"ok":true,"data":{"status":"ok","version":"0.6.1"}}`)
		case "/core":
			w.Write(statesEnvelope(t, nil))
		case "/ws":
			w.Write(registryEnvelope(t, nil))
		default:
			http.NotFound(w, r)
		}
	}))
	defer relay.Close()
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: relay.URL}); err != nil {
		t.Fatalf("save relay config: %v", err)
	}
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(paths.Home, ".config", "ha-nova", ".test-relay-auth-token"))
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("write relay token: %v", err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runRelayCommand(paths, []string{"core", "--method", "GET", "--path", "/api/states"})
	})
	if exit != 0 {
		t.Fatalf("relay core failed:\n%s", output)
	}
	if !strings.Contains(output, censusSkillNoticeBlock) {
		t.Fatalf("transition task missed first-use census carrier:\n%s", output)
	}
	if strings.Count(output, censusSkillNoticeBlock) != 1 {
		t.Fatalf("transition task emitted census ask more than once:\n%s", output)
	}
	if !markerHasVersion(filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker), "0.6.1") {
		t.Fatal("successful current-task carrier did not finalize migration marker")
	}
}

func TestCheckUpdateDoesNotVerifyStaleLayoutWhenRepairLockIsBusy(t *testing.T) {
	paths := setupHealableInstall(t)
	codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
	if _, err := installTreeClient(filepath.Dir(codexRoot), filepath.Join(paths.InstallRoot, "skills"), false); err != nil {
		t.Fatalf("seed copied Codex tree: %v", err)
	}
	bootstrap := filepath.Join(codexRoot, "ha-nova", "session-bootstrap.md")
	if err := os.Remove(bootstrap); err != nil {
		t.Fatalf("remove bootstrap: %v", err)
	}
	if err := writeJSONFile(paths.UpdateCacheFile, releaseInfo{
		Version: "0.6.1",
		HTMLURL: "https://example.invalid/releases/v0.6.1",
	}, 0o644); err != nil {
		t.Fatalf("write current update cache: %v", err)
	}
	t.Setenv("HA_NOVA_NO_CENSUS", "1")
	if err := os.MkdirAll(paths.ConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		t.Fatal("acquire repair lock")
	}
	defer release()

	if exit := runCheckUpdate(paths, []string{"--quiet"}); exit != 0 {
		t.Fatalf("quiet check-update exit = %d", exit)
	}
	if fileExists(bootstrap) {
		t.Fatal("held lock should defer the repair")
	}
	if markerHasVersion(filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker), "0.6.1") {
		t.Fatal("stale layout must not receive the verified marker")
	}
}

func TestRelayHelpAndInvalidArgumentsDoNotRunSessionBootstrapRepair(t *testing.T) {
	cases := [][]string{
		{"health", "--help"},
		{"core", "--help"},
		{"core", "--method", "GET"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			paths := setupHealableInstall(t)
			codexRoot := filepath.Join(paths.Home, ".agents", "skills", "ha-nova")
			if _, err := installTreeClient(filepath.Dir(codexRoot), filepath.Join(paths.InstallRoot, "skills"), false); err != nil {
				t.Fatalf("seed copied Codex tree: %v", err)
			}
			bootstrap := filepath.Join(codexRoot, "ha-nova", "session-bootstrap.md")
			if err := os.Remove(bootstrap); err != nil {
				t.Fatalf("remove bootstrap: %v", err)
			}
			_ = runRelayCommand(paths, args)
			if fileExists(bootstrap) {
				t.Fatalf("non-operational relay invocation %v mutated client files", args)
			}
		})
	}
}

func TestRelayErrorEnvelopeLeavesMigratedCarrierPending(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if err := os.WriteFile(marker, []byte("0.6.1"+sessionBootstrapCarrierPendingSuffix+"\n"), 0o600); err != nil {
		t.Fatalf("write carrier: %v", err)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ok":false,"error":{"code":"failed"}}`)
	}))
	defer relay.Close()
	if err := saveConfig(paths, runtimeConfig{RelayBaseURL: relay.URL}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	t.Setenv("HA_NOVA_ALLOW_INSECURE_TEST_KEYRING", "1")
	t.Setenv("HA_NOVA_TEST_KEYRING_FILE", filepath.Join(paths.ConfigDir, ".test-relay-auth-token"))
	if err := writeRelayAuthToken("test-relay-token"); err != nil {
		t.Fatalf("write token: %v", err)
	}

	exit, _ := captureCommandOutput(t, func() int {
		return runRelayCommand(paths, []string{"core", "--method", "GET", "--path", "/api/states"})
	})
	if exit != 0 {
		t.Fatalf("Relay envelope remains caller-visible and historically exits zero, got %d", exit)
	}
	if !markerHasCarrierPending(marker, "0.6.1") {
		t.Fatal("failed Relay task must not consume the migrated first-use carrier")
	}
}

func TestRelayCoreUpstreamErrorLeavesMigratedCarrierPending(t *testing.T) {
	paths := setupRelayProxyTest(t, http.StatusNotFound)
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if err := os.WriteFile(marker, []byte("0.6.1"+sessionBootstrapCarrierPendingSuffix+"\n"), 0o600); err != nil {
		t.Fatalf("write carrier: %v", err)
	}

	exit, _ := captureCommandOutput(t, func() int {
		return runRelayCommand(paths, []string{"core", "--method", "GET", "--path", "/api/missing"})
	})
	if exit != 0 {
		t.Fatalf("non-strict upstream 404 changed the CLI exit contract: %d", exit)
	}
	if !markerHasCarrierPending(marker, "0.6.1") {
		t.Fatal("upstream 404 consumed the migrated first-use carrier")
	}
}

func TestQuietCheckFinalizesCarrierProducedByContendingRepair(t *testing.T) {
	paths := setupHealableInstall(t)
	t.Setenv("HA_NOVA_NO_CENSUS", "1")
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	cacheReleaseInfo(paths, releaseInfo{Version: "0.6.1"})
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	release, acquired := acquireAutoRepairLock(paths)
	if !acquired {
		t.Fatal("acquire simulated repair lock")
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = os.WriteFile(marker, []byte("0.6.1"+sessionBootstrapCarrierPendingSuffix+"\n"), 0o600)
		release()
		close(released)
	}()

	exit, output := captureCommandOutput(t, func() int {
		return runCheckUpdate(paths, []string{"--quiet"})
	})
	<-released
	if exit != 0 {
		t.Fatalf("quiet check-update exit = %d\n%s", exit, output)
	}
	if !markerHasVersion(marker, "0.6.1") {
		data, _ := os.ReadFile(marker)
		t.Fatalf("contended carrier was not finalized: %q", data)
	}
}

func TestCarrierCompletionCannotOverwriteNewerOwner(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	oldClaim := "0.6.1" + sessionBootstrapCarrierRunningPrefix + "1"
	newClaim := "0.6.1" + sessionBootstrapCarrierRunningPrefix + "2"
	if err := os.WriteFile(marker, []byte(newClaim+"\n"), 0o600); err != nil {
		t.Fatalf("write newer claim: %v", err)
	}
	deadline := time.Now().Add(time.Second)

	resetSessionBootstrapCarrier(paths, oldClaim, deadline)
	if finalizeSessionBootstrapCarrier(paths, oldClaim, deadline) {
		t.Fatal("old owner finalized a newer claim")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != newClaim+"\n" {
		t.Fatalf("new owner changed: %q %v", data, err)
	}
}

func TestMigratedCarrierCannotTouchReplacementInstallCensus(t *testing.T) {
	paths := setupHealableInstall(t)
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(paths.CacheDir, sessionBootstrapVerifiedMarker)
	if err := os.WriteFile(marker, []byte("0.6.1"+sessionBootstrapCarrierPendingSuffix+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveState(paths, installState{SchemaVersion: stateSchemaVersion, Version: "0.6.1"}); err != nil {
		t.Fatal(err)
	}
	if err := saveCensusState(paths, censusState{}); err != nil {
		t.Fatal(err)
	}

	previousRun := runMigratedFirstUseCheckForCarrier
	runMigratedFirstUseCheckForCarrier = func(paths runtimePaths, _ time.Duration) {
		if err := markCensusLifecycleStopped(paths); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(paths.CensusFile); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		replacementLifecycle := [][]byte{
			captureInstallLifecycleGeneration(paths),
			captureCensusLifecycleMarker(paths),
		}
		if err := completeSetupLifecycle(paths, replacementLifecycle...); err != nil {
			t.Fatal(err)
		}
		if err := saveCensusState(paths, censusState{}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { runMigratedFirstUseCheckForCarrier = previousRun })

	finishMigratedFirstUse(paths, true, false, time.Now().Add(10*time.Second))
	if state := loadCensusState(paths); state.SkillNotices != 0 || state.AskedAt != "" {
		t.Fatalf("stale carrier changed replacement census state: %+v", state)
	}
}
