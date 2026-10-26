package main

import (
	"bufio"
	"io"
	"os"
	"strings"
	"testing"
)

// stubLeftoverLinkChoice: a terminal is there (or not); Finish and Remove
// are fakes, nil fails the test when called.
func stubLeftoverLinkChoice(t *testing.T, terminal bool, finish func(runtimePaths, runtimeConfig) (int, bool), remove func(runtimePaths) int) {
	t.Helper()
	available, previousFinish, previousRemove := leftoverLinkChoiceAvailable, finishLeftoverLink, removeLeftoverLink
	t.Cleanup(func() {
		leftoverLinkChoiceAvailable, finishLeftoverLink, removeLeftoverLink = available, previousFinish, previousRemove
	})
	leftoverLinkChoiceAvailable = func(io.Writer) bool { return terminal }
	finishLeftoverLink = finish
	if finish == nil {
		finishLeftoverLink = func(runtimePaths, runtimeConfig) (int, bool) { t.Fatal("Finish ran"); return 1, false }
	}
	removeLeftoverLink = remove
	if remove == nil {
		removeLeftoverLink = func(runtimePaths) int { t.Fatal("Remove ran"); return 1 }
	}
}

// "Remove" runs the remove flow in this process; setup then goes on with the
// settings it saved, and its own later saves still pass the lifecycle check.
func TestSetupLinkLeftoverRemoveRunsRemoveAndGoesOn(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	removed := 0
	stubLeftoverLinkChoice(t, true, nil, func(paths runtimePaths) int {
		removed++
		saved, err := loadConfig(paths)
		if err != nil {
			t.Fatal(err)
		}
		saved.Cloud, saved.RoutePolicy = nil, routePolicyLocal
		if err := saveConfig(paths, saved); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	snapshot, err := readSetupConfigSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	marker := [][]byte{captureInstallLifecycleGeneration(paths), captureUninstallMarker(paths), snapshot}
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state, lifecycleMarker: marker,
		ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("2\n")), out: &out}}

	_, done, paused := flow.settleLeftoverLink()
	if done || paused || removed != 1 || flow.cfg.Cloud != nil {
		t.Fatalf("done=%v paused=%v removed=%d cloud=%+v:\n%s", done, paused, removed, flow.cfg.Cloud, out.String())
	}
	for _, want := range []string{setupT("setup.link.leftover"), "1) " + setupT("setup.link.choice_finish"), "2) " + setupT("setup.link.choice_remove"), "3) Not now"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q:\n%s", want, out.String())
		}
	}
	if err := ensureSetupLifecycleCurrent(paths, marker...); err != nil {
		t.Fatalf("lifecycle after Remove: %v", err)
	}
}

// "Not now": setup goes on paused, exactly as before, and changes nothing.
func TestSetupLinkLeftoverNotNowKeepsConfigByteIdentical(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	stubLeftoverLinkChoice(t, true, nil, nil)
	before := credentialSnapshot(t, paths)
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state, connectionMode: setupConnectionLocal,
		pairingBackStep: setupStepRelayInstall, ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("3\n1\n")), out: &out}}
	var code int
	_, stderr := captureCommandOutput(t, func() int { code = flow.run(); return code })
	output := out.String() + stderr
	if code != 0 || !strings.Contains(output, "3) Not now") || !strings.Contains(output, setupT("setup.link.goes_on")) ||
		!strings.Contains(output, "Link setup paused") {
		t.Fatalf("exit %d:\n%s", code, output)
	}
	if !sameSnapshot(before, credentialSnapshot(t, paths)) {
		t.Fatal("Not now changed config or secure storage")
	}
}

// No terminal: no menu. The explanation, then at the stop the commands with
// what each does, and why setup stopped.
func TestSetupLinkLeftoverWithoutTerminalExplainsCommands(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	stubLeftoverLinkChoice(t, false, nil, nil)
	if err := os.Remove(os.Getenv("HA_NOVA_TEST_KEYRING_FILE")); err != nil { // the run would pair: it stops
		t.Fatal(err)
	}
	code, out := runLinkPausedSetup(t, paths, cfg, state)
	for _, want := range []string{
		setupT("setup.link.leftover"),
		setupT("setup.link.goes_on"),
		"To finish it, run: ha-nova cloud reconnect --server default\n    " + setupT("setup.link.finish_does"),
		"To remove it, run: ha-nova cloud remove --server default\n    " + setupT("setup.link.remove_does"),
		setupT("setup.link.paused_stop"),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q (exit %d):\n%s", want, code, out)
		}
	}
	if code != 1 || strings.Contains(out, setupT("setup.link.choice_finish")) {
		t.Fatalf("exit %d:\n%s", code, out)
	}
}

// A safety hold: its own first line, no Finish, and a bare Enter is Not now:
// Remove never runs without being chosen.
func TestSetupLinkLeftoverHoldEnterIsNotNow(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	stubLeftoverLinkChoice(t, true, nil, nil)
	cfg.Cloud.RecoveryHold = &cloudRecoveryHold{Code: cloudProblemAuthorization, Remediation: cloudRemediationSecurityStop}
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state,
		ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("\n")), out: &out}}
	_, done, paused := flow.settleLeftoverLink()
	if done || paused || !strings.Contains(out.String(), setupT("setup.link.leftover_hold")) ||
		strings.Contains(out.String(), setupT("setup.link.choice_finish")) {
		t.Fatalf("done=%v paused=%v:\n%s", done, paused, out.String())
	}
}

// An interrupted removal: add and reconnect refuse, so Finish isn't offered.
func TestSetupLinkLeftoverHidesFinishWhileCleanupPending(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	removed := 0
	stubLeftoverLinkChoice(t, true, nil, func(runtimePaths) int { removed++; return 1 })
	cfg.Cloud.DeviceRevocationCompleted = &cloudDeviceRevocationCheckpoint{}
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state,
		ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("1\n")), out: &out}}
	if _, done, _ := flow.settleLeftoverLink(); done || removed != 1 || strings.Contains(out.String(), setupT("setup.link.choice_finish")) ||
		!strings.Contains(out.String(), "Removing Home Assistant Link didn't finish. To finish removing: ha-nova cloud remove --server default.") {
		t.Fatalf("done=%v removed=%d:\n%s", done, removed, out.String())
	}
}

// Ctrl+C inside Finish (the pairing-code prompt) ends setup, with the
// commands on screen, instead of going on.
func TestSetupLinkLeftoverExitInsideFinishEndsSetup(t *testing.T) {
	paths, cfg, state := linkPausedLegacyFixture(t)
	stubLeftoverLinkChoice(t, true, func(runtimePaths, runtimeConfig) (int, bool) { return 0, true }, nil)
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state,
		ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("1\n")), out: &out}}
	var result setupStepResult
	var done bool
	captureCommandOutput(t, func() int { result, done, _ = flow.settleLeftoverLink(); return 0 })
	if !done || result.code != 0 || !strings.Contains(out.String(), "To remove it, run: ha-nova cloud remove --server default") {
		t.Fatalf("done=%v code=%d:\n%s", done, result.code, out.String())
	}
}

// A paired install whose Link never finished gets the same explanation and
// a Remove choice, not only the old resume question.
func TestSetupLinkLeftoverPairedInstallCanRemove(t *testing.T) {
	t.Cleanup(setCloudFeatureTestBuild(t, true))
	paths, cfg := saveHybridCheckpointUXProfile(t, defaultServerProfileName, hybridCheckpointUXConfig(cloudStateTokenStored, false))
	installCloudSetupTestSeams(t, successfulCloudCoordinatorForTest(), true, true)
	removed := 0
	stubLeftoverLinkChoice(t, true, nil, func(paths runtimePaths) int {
		removed++
		saved, err := loadConfig(paths)
		if err != nil {
			t.Fatal(err)
		}
		saved.Cloud, saved.RoutePolicy = nil, routePolicyLocal
		if err := saveConfig(paths, saved); err != nil {
			t.Fatal(err)
		}
		return 0
	})
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("2\n")), out: &out}}
	_, done, paused := flow.settleLeftoverLink()
	if done || paused || removed != 1 || flow.cfg.Cloud != nil ||
		!strings.Contains(out.String(), setupT("setup.link.leftover")) || !strings.Contains(out.String(), "1) "+setupT("setup.link.choice_finish")) {
		t.Fatalf("done=%v paused=%v removed=%d:\n%s", done, paused, removed, out.String())
	}
}

// After `cloud remove` a paired profile still names the old Relay. Setup's
// device check doesn't prove that identity, so it drops it, and the next
// `cloud add` reads it again from the App (a reinstalled one has a new id).
func TestSetupLinkLeftoverReinstalledAppNextAddReadsIdentityAgain(t *testing.T) {
	paths, _, state, _ := setupCompletedInstallFixture(t)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cfg.RelayInstanceID = "https://192.168.1.5:8792", "PIN", "relay-old"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	previous := verifyDeviceHealth
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceOK }
	t.Cleanup(func() { verifyDeviceHealth = previous })
	var out strings.Builder
	flow := &setupFlow{paths: paths, cfg: cfg, state: state, devicePaired: true, connectionMode: setupConnectionLocal,
		ui: setupTextUI{reader: bufio.NewReader(strings.NewReader("")), out: &out}}
	flow.verifyDevice()
	saved, err := loadConfig(paths)
	if err != nil || saved.RelayInstanceID != "" {
		t.Fatalf("saved relay id = %q, %v:\n%s", saved.RelayInstanceID, err, out.String())
	}
	linked := cfg
	linked.Cloud = &cloudLifecycleMetadata{}
	if forgetUnprovenRelayIdentity(&linked) || linked.RelayInstanceID != "relay-old" {
		t.Fatal("a saved Link keeps its Relay id")
	}
}

// Pairing refuses while Link is saved, so a finished Link on a shared-access
// install never leads to the switch offer (or the doctor notice) for it.
func TestSetupLinkLeftoverFinishedLinkOffersNoLocalSwitch(t *testing.T) {
	cfg := runtimeConfig{HAHost: "ha", HAURL: "http://ha:8123", RelayBaseURL: "http://ha:8791"}
	probe := func(string) bool { return true }
	if !legacyAccessSwitchable(cfg, probe) {
		t.Fatal("shared access without Link should be switchable")
	}
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateReady, Current: &current}
	if legacyAccessSwitchable(cfg, probe) {
		t.Fatal("offered a switch that pairing would refuse while Link is saved")
	}
}
