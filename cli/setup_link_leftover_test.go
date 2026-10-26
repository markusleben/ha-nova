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
func stubLeftoverLinkChoice(t *testing.T, terminal bool, finish func(runtimePaths, runtimeConfig) int, remove func(runtimePaths) int) {
	t.Helper()
	available, previousFinish, previousRemove := leftoverLinkChoiceAvailable, finishLeftoverLink, removeLeftoverLink
	t.Cleanup(func() {
		leftoverLinkChoiceAvailable, finishLeftoverLink, removeLeftoverLink = available, previousFinish, previousRemove
	})
	leftoverLinkChoiceAvailable = func(io.Writer) bool { return terminal }
	finishLeftoverLink = finish
	if finish == nil {
		finishLeftoverLink = func(runtimePaths, runtimeConfig) int { t.Fatal("Finish ran"); return 1 }
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

// A safety hold: its own first line, and no Finish.
func TestSetupLinkLeftoverHoldOffersOnlyRemove(t *testing.T) {
	var out strings.Builder
	choice, err := promptLeftoverLink(bufio.NewReader(strings.NewReader("1\n")), &out, true, true)
	if err != nil || choice != "remove" || !strings.Contains(out.String(), setupT("setup.link.leftover_hold")) ||
		strings.Contains(out.String(), setupT("setup.link.choice_finish")) {
		t.Fatalf("choice %q err %v:\n%s", choice, err, out.String())
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
	if _, done, _ := flow.settleLeftoverLink(); done || removed != 1 || strings.Contains(out.String(), setupT("setup.link.choice_finish")) {
		t.Fatalf("done=%v removed=%d:\n%s", done, removed, out.String())
	}
}
