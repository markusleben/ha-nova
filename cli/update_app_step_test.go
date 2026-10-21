package main

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// The NOVA App half of `ha-nova update`: at a desktop, an App below what this
// version needs, with its update pending in Home Assistant, opens the setup
// page (its "Update the NOVA App" button); a terminal gets the guided offer;
// an AI app running the command gets one plain line. Nothing installs here.
func TestUpdateAppStepOpensSetupPageOnlyAtADesktop(t *testing.T) {
	resetServerProfileSelection(t)
	previousAvailable, previousOpen, previousTargets, previousNotice := setupPageAvailable, openSetupPageForAppUpdate, haAppInstallTargetsThisHAForPage, relayUpdateNoticeForUpdate
	previousIn, previousOut := relayUpdateInputIsTTY, relayUpdateOutputIsTTY
	t.Cleanup(func() {
		setupPageAvailable, openSetupPageForAppUpdate, haAppInstallTargetsThisHAForPage, relayUpdateNoticeForUpdate = previousAvailable, previousOpen, previousTargets, previousNotice
		relayUpdateInputIsTTY, relayUpdateOutputIsTTY = previousIn, previousOut
	})
	relayUpdateInputIsTTY, relayUpdateOutputIsTTY = func() bool { return false }, func() bool { return false }
	haAppInstallTargetsThisHAForPage = func(runtimeConfig) bool { return true }
	opened := 0
	openSetupPageForAppUpdate = func(runtimePaths) error { opened++; return nil }
	stillDue := humanNotice{}
	relayUpdateNoticeForUpdate = func(runtimePaths) humanNotice { return stillDue }
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(paths, runtimeConfig{HAHost: "192.168.1.5", HAURL: "http://192.168.1.5:8123", RelayBaseURL: "http://192.168.1.5:8791"}); err != nil {
		t.Fatal(err)
	}
	tooOld := humanNotice{level: humanNoticeWarning, kind: humanNoticeKindRelayUpdateAvailable, message: "Too old. " + relayAppUpdatePath, appUpdate: true, belowFloor: true}
	newer := humanNotice{level: humanNoticeWarning, kind: humanNoticeKindRelayUpdateAvailable, message: "An update. " + relayAppUpdatePath, appUpdate: true}
	container := humanNotice{level: humanNoticeWarning, kind: humanNoticeKindRelayOutdated, message: "pull the new image"}
	run := func(notice humanNotice) string {
		_, output := captureCommandOutput(t, func() int { offerRelayUpdate(paths, notice); return 0 })
		return output
	}
	// Without the page: the notice says where to update, once.
	once := func(out string) bool {
		return strings.Count(out, "Settings > Apps > NOVA Relay") == 1 && strings.Contains(out, relayAppUpdatePath)
	}

	setupPageAvailable = func() bool { return true }
	if out := run(tooOld); opened != 1 || !strings.Contains(out, `click "Update the NOVA App" there`) || strings.Contains(out, relayUpdateManualLine) {
		t.Fatalf("desktop: opened=%d\n%s", opened, out)
	}
	// Closed the page without updating: where to do it later.
	stillDue = tooOld
	if out := run(tooOld); opened != 2 || !strings.Contains(out, relayUpdateManualLine) {
		t.Fatalf("closed page: opened=%d\n%s", opened, out)
	}
	// An above-floor App update keeps its guided offer with backup: no page.
	if out := run(newer); opened != 2 || !once(out) {
		t.Fatalf("above floor: opened=%d\n%s", opened, out)
	}
	// The page can't update this App (another address, Link only): no page.
	haAppInstallTargetsThisHAForPage = func(runtimeConfig) bool { return false }
	if out := run(tooOld); opened != 2 || !once(out) {
		t.Fatalf("page can't update: opened=%d\n%s", opened, out)
	}
	haAppInstallTargetsThisHAForPage = func(runtimeConfig) bool { return true }
	// SSH, no display, or an AI app running the command: no page.
	setupPageAvailable = func() bool { return false }
	if out := run(tooOld); opened != 2 || !once(out) {
		t.Fatalf("no desktop: opened=%d\n%s", opened, out)
	}
	// No App in Home Assistant to update (a standalone Relay): only its notice.
	if out := run(container); opened != 2 || strings.Contains(out, relayUpdateManualLine) || !strings.Contains(out, "pull the new image") {
		t.Fatalf("container: %s", out)
	}
	// No App notice: nothing at all.
	if out := run(humanNotice{}); opened != 2 || strings.TrimSpace(out) != "" {
		t.Fatalf("no notice: %q", out)
	}
}

// The quiet first-use check never holds a task up on a slow network: the
// cached release answers once the short timeout passes.
func TestQuietCheckUpdateIsBoundedOnASlowNetwork(t *testing.T) {
	paths, _ := nudgeTestEnv(t, "0.1.0")
	writeFreshReleaseCache(t, paths, "0.2.0")
	stale := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(paths.UpdateCacheFile, stale, stale); err != nil {
		t.Fatal(err)
	}
	previousClient, previousTimeout := httpClient, quietUpdateCheckTimeout
	t.Cleanup(func() { httpClient, quietUpdateCheckTimeout = previousClient, previousTimeout })
	quietUpdateCheckTimeout = 50 * time.Millisecond
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done() // a network that never answers
		return nil, r.Context().Err()
	})}

	started := time.Now()
	exitCode, output := captureCommandOutput(t, func() int { return runCheckUpdate(paths, []string{"--quiet"}) })
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("quiet check took %s", elapsed)
	}
	if exitCode != 0 || !strings.Contains(output, "HA NOVA 0.2.0 is ready (you have 0.1.0).") || strings.Contains(output, "WARNING") {
		t.Fatalf("exit %d\n%s", exitCode, output)
	}
}

// "…needs an update too" only when Home Assistant reports a pending App
// update: a standalone Relay or an App with nothing pending gets no promise.
func TestAppUpdateTooLineNeedsAPendingAppUpdate(t *testing.T) {
	cli := humanNotice{level: humanNoticeNote, kind: humanNoticeKindUpdateAvailable, message: "HA NOVA 0.2.0 is ready (you have 0.1.0).\nTo update, run: ha-nova update"}
	pending := humanNotice{level: humanNoticeWarning, kind: humanNoticeKindRelayUpdateAvailable, message: "x", appUpdate: true}
	if got := withAppUpdateToo(cli, pending).message; got != "HA NOVA 0.2.0 is ready (you have 0.1.0).\n"+appUpdateTooLine+"\nTo update, run: ha-nova update" {
		t.Fatalf("pending App update: %q", got)
	}
	for _, other := range []humanNotice{
		{level: humanNoticeWarning, kind: humanNoticeKindRelayOutdated, message: "pull and recreate a standalone Relay container"},
		{level: humanNoticeWarning, kind: humanNoticeKindRelayOutdated, message: "exposes no pending App update"},
		{},
	} {
		if got := withAppUpdateToo(cli, other).message; got != cli.message {
			t.Fatalf("no pending App update (%q): %q", other.message, got)
		}
	}
}
