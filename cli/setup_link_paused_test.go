package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkPausedLegacyFixture: an old shared-access install (0.24) whose Link
// sign-in never finished next to a working Link connection, as the
// maintainer's upgrade found it.
func linkPausedLegacyFixture(t *testing.T) (runtimePaths, runtimeConfig, installState) {
	t.Helper()
	paths, cfg, state, _ := setupCompletedInstallFixture(t)
	restore := setCloudFeatureTestBuild(t, true)
	t.Cleanup(restore)
	installCloudSetupTestSeams(t, successfulCloudCoordinatorForTest(), true, true)
	stubLegacySwitchProbe(t, true)
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	pending := cloudMetadataForTest(strings.Repeat("a", 32))
	cfg, err := loadConfig(paths) // its saved profile id
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelayInstanceID = "relay-instance-1"
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing, Current: &current, Pending: &pending}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths, cfg, state
}

// credentialSnapshot: config, the test keyring file, and every secret file.
func credentialSnapshot(t *testing.T, paths runtimePaths) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	read := func(path string) {
		data, err := os.ReadFile(path)
		if err == nil {
			files[path] = data
		} else if !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
	}
	read(paths.ConfigFile)
	read(os.Getenv("HA_NOVA_TEST_KEYRING_FILE"))
	_ = filepath.WalkDir(os.Getenv("HA_NOVA_TEST_SECRET_DIR"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			read(path)
		}
		return nil
	})
	return files
}

func sameSnapshot(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for path, data := range a {
		if !bytes.Equal(data, b[path]) {
			return false
		}
	}
	return true
}

func runLinkPausedSetup(t *testing.T, paths runtimePaths, cfg runtimeConfig, state installState) (int, string) {
	t.Helper()
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state, connectionMode: setupConnectionLocal,
		pairingBackStep: setupStepRelayInstall, ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("1\n")), out: &out}}
	var code int
	_, stderr := captureCommandOutput(t, func() int { code = flow.run(); return code })
	return code, out.String() + stderr
}

// Link waits; setup still checks this computer and ends on the paused
// outcome with the exact commands. No credential or Link byte changes.
func TestSetupLinkPausedKeepsCredentialsAndShowsCommands(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	before := credentialSnapshot(t, paths)

	code, out := runLinkPausedSetup(t, paths, cfg, state)
	for _, want := range []string{
		"Link setup paused",
		"To finish it, run: ha-nova cloud reconnect --server default",
		"To remove it, run: ha-nova cloud remove --server default",
		"Switch to your own access after Link is fixed or removed: run ha-nova setup again.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q (exit %d):\n%s", want, code, out)
		}
	}
	if code != 0 || strings.Contains(out, "Switch to your own access now?") || strings.Contains(out, "shown above") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !sameSnapshot(before, credentialSnapshot(t, paths)) {
		t.Fatal("a Link-paused setup changed config or secure storage")
	}
}

// Without the shared token the run would pair: it stops first, before any
// secure-storage write, and says why.
func TestSetupLinkPausedStopsBeforePairing(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	if err := os.Remove(os.Getenv("HA_NOVA_TEST_KEYRING_FILE")); err != nil {
		t.Fatal(err)
	}
	before := credentialSnapshot(t, paths)

	code, out := runLinkPausedSetup(t, paths, cfg, state)
	if code != 1 || !strings.Contains(out, setupT("setup.link.paused_stop")) ||
		!strings.Contains(out, "ha-nova cloud remove --server default") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if !sameSnapshot(before, credentialSnapshot(t, paths)) {
		t.Fatal("a Link-paused setup wrote credentials before stopping")
	}
}

// A safety hold: `cloud remove` decides. When it can't check the saved
// sign-in (here a second one with nothing stored), the owner steps with
// links on the local address end in the manual cleanup command; in output
// an AI assistant may read, step 3 is the plain remove. When everything
// checks out, only the plain remove (the steps would revoke needlessly).
func TestCloudSecurityHoldAdviceFollowsWhatRemoveFinds(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()
	paths, store, _, _ := cloudRemoveCommandFixture(t)
	installCloudRemoveStore(t, store, nil)
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Cloud.RecoveryHold = &cloudRecoveryHold{Code: cloudProblemAuthorization, Remediation: cloudRemediationSecurityStop}
	render := func(cfg runtimeConfig, terminal bool) string {
		var out strings.Builder
		renderCloudSecurityHold(&out, cfg, "default", terminal)
		return out.String()
	}

	if out := render(cfg, true); !strings.Contains(out, "To remove Link, run: ha-nova cloud remove --server default") ||
		strings.Contains(out, "Refresh tokens") {
		t.Fatalf("checkable hold:\n%s", out)
	}

	pending := cloudMetadataForTest(strings.Repeat("e", 32)) // never stored
	cfg.Cloud.State, cfg.Cloud.Pending = cloudStateAuthorizing, &pending
	out := render(cfg, true)
	for _, want := range []string{
		"It can't tell if an earlier Link sign-in still works.",
		"1. As the owner, open the NOVA App page.",
		"Open: http://ha:8123/app/" + HAOfficialNOVAAppSlug,
		"Open: http://ha:8123/profile/security",
		"3. Run: ha-nova cloud remove --server default --yes --confirm-remote-access-revoked default",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Verified cleanup") || strings.Contains(out, "still works until") {
		t.Fatalf("unclear hold:\n%s", out)
	}
	if out := render(cfg, false); strings.Contains(out, "--confirm-remote-access-revoked") ||
		!strings.Contains(out, "3. In your own terminal, run: ha-nova cloud remove --server default") {
		t.Fatalf("agent-readable hold:\n%s", out)
	}
}

func TestOwnerStepLinksUseOnlyAPlainLocalAddress(t *testing.T) {
	restore := setCloudFeatureTestBuild(t, false)
	defer restore()
	panel, security := ownerStepLinks("http://192.168.1.5:8123/")
	if panel != "http://192.168.1.5:8123/app/"+HAOfficialNOVAAppSlug || security != "http://192.168.1.5:8123/profile/security" {
		t.Fatalf("links = %q %q", panel, security)
	}
	for _, unsafe := range []string{"", "https://abc.ui.nabu.casa", "https://abc.ui.nabu.casa.", "http://user:pw@ha:8123", "http://ha:8123/?token=x", "ftp://ha", "ha:8123"} {
		if panel, security := ownerStepLinks(unsafe); panel != "" || security != "" {
			t.Fatalf("%q gave %q %q", unsafe, panel, security)
		}
	}
	if steps := cloudAuthorizationRecoverySteps("default", "", ""); strings.Contains(steps, "Open:") {
		t.Fatalf("no address, still links:\n%s", steps)
	}
}

// A second sign-in next to a working connection: only that sign-in is in
// doubt, so the stop says so (not "can't check if still signed in").
func TestUnfinishedSecondSignInStopNamesIt(t *testing.T) {
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())
	stored := establishProductionCloudCurrent(t, store, "current")
	cfg, pending := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	origin, err := cloudOriginFromCanonical(stored.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	current := cloudMetadataFromEnvelope(origin, stored)
	cfg.Cloud.Current = &current
	cfg.ProfileID, cfg.RelayInstanceID = stored.ProfileID, stored.RelayInstanceID
	pending.CredentialGeneration = strings.Repeat("e", 32) // never stored
	cfg.Cloud.Pending = &pending

	_, err = inspectCloudAuthorizationCleanup(context.Background(), cfg, store)
	problem := cloudAuthorizationStopProblem(err)
	if problem == nil || !strings.Contains(problem.Detail, "didn't finish") {
		t.Fatalf("err = %v", err)
	}
}

// Exit at the client choice still leaves the Link commands on screen.
func TestSetupLinkPausedExitStillShowsCommands(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state, connectionMode: setupConnectionLocal,
		pairingBackStep: setupStepRelayInstall, ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("exit\n")), out: &out}}
	captureCommandOutput(t, func() int { return flow.run() })
	if !strings.Contains(out.String(), "To remove it, run: ha-nova cloud remove --server default") ||
		strings.Contains(out.String(), "Link setup paused") {
		t.Fatalf("exit:\n%s", out.String())
	}
}

// A paired install: "Not now" or "back" at the resume question, and the
// reconnect state (Current saved), each go on with one line; nothing is
// added to Link.
func TestPairedLinkPausedGoesOnWithOneLine(t *testing.T) {
	for _, test := range []struct {
		name    string
		current bool
		input   string
	}{{"not now", false, "n\n"}, {"back", false, "back\n"}, {"reconnect owns it", true, ""}} {
		t.Run(test.name, func(t *testing.T) {
			restore := setCloudFeatureTestBuild(t, true)
			defer restore()
			paths, cfg := saveHybridCheckpointUXProfile(t, defaultServerProfileName, hybridCheckpointUXConfig(cloudStateTokenStored, test.current))
			coordinator := successfulCloudCoordinatorForTest()
			installCloudSetupTestSeams(t, coordinator, true, true)
			var out strings.Builder
			_, handled, code, paused := handleInteractiveCloudRecoveryBeforeClients(
				bufio.NewReader(strings.NewReader(test.input)), &out, paths, cfg, false, false)
			if handled || code != 0 || !paused || coordinator.addCalls != 0 ||
				!strings.Contains(out.String(), "Setup goes on without it.") || strings.Contains(out.String(), "Verified cleanup") {
				t.Fatalf("handled=%v code=%d paused=%v adds=%d:\n%s", handled, code, paused, coordinator.addCalls, out.String())
			}
		})
	}
}

// End to end on a paired install: "Not now", then the device check, the
// skills, and the paused outcome; no Link add and no credential change.
func TestPairedLinkPausedEndsOnPausedOutcome(t *testing.T) {
	paths, cfg, state, _ := setupCompletedInstallFixture(t)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	t.Setenv("PATH", installClaudeMock(t, paths.Home, filepath.Join(t.TempDir(), "claude.log"))+string(os.PathListSeparator)+os.Getenv("PATH"))
	restore := setCloudFeatureTestBuild(t, true)
	defer restore()
	coordinator := successfulCloudCoordinatorForTest()
	installCloudSetupTestSeams(t, coordinator, true, true)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	pending := cloudMetadataForTest(strings.Repeat("a", 32))
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin = "https://192.168.1.5:8792", "PIN"
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateTokenStored, Pending: &pending}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredential(validCredential(91)); err != nil {
		t.Fatal(err)
	}
	previous := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceOK }
	t.Cleanup(func() { verifyDeviceHealth = previous })
	before := credentialSnapshot(t, paths)

	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state, connectionMode: setupConnectionLocal,
		pairingBackStep: setupStepRelayInstall, ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("n\n1\n")), out: &out}}
	var code int
	_, stderr := captureCommandOutput(t, func() int { code = flow.run(); return code })
	output := out.String() + stderr
	if code != 0 || coordinator.addCalls != 0 || !strings.Contains(output, "Link setup paused") ||
		!strings.Contains(output, "To finish it, run: ha-nova cloud add --server default") ||
		strings.Contains(output, "Switch to your own access") {
		t.Fatalf("exit %d adds %d:\n%s", code, coordinator.addCalls, output)
	}
	after := credentialSnapshot(t, paths)
	delete(before, paths.ConfigFile) // the device check saves the setup state; Link data stays
	delete(after, paths.ConfigFile)
	if !sameSnapshot(before, after) {
		t.Fatal("a paused paired run changed secure storage")
	}
	saved, err := loadConfig(paths)
	if err != nil || saved.Cloud == nil || saved.Cloud.State != cloudStateTokenStored || saved.Cloud.Pending == nil {
		t.Fatalf("Link data changed: %+v %v", saved.Cloud, err)
	}
}
