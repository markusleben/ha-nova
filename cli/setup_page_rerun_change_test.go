package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A re-run that needs a fix (here: an App too old) offers "Use another Home
// Assistant": step 1 with the address field open, not the old home's
// sign-in. Nothing installs or updates on the old home.
func TestSetupPageRerunUseAnotherHomeAssistant(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, errors.New("not read"))
	previous := haAppInstallTargetsThisHAForPage
	t.Cleanup(func() { haAppInstallTargetsThisHAForPage = previous })
	haAppInstallTargetsThisHAForPage = func(runtimeConfig) bool { return true }
	previousIdle := setupPageIdle
	setupPageIdle = 2 * time.Second
	t.Cleanup(func() { setupPageIdle = previousIdle })
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.27.0","min_relay_version":"0.12.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))

	old := page.waitFor("the re-run screen", accepting("change"))
	if old.Screen != "rerun_fix" || old.Fix != "app_update" || !slices.Contains(old.Accepts, "fix") {
		t.Fatalf("old App = %+v", old)
	}
	page.click("change", "")
	ask := page.waitFor("step 1", accepting("address"))
	if ask.Screen != "found" || !ask.AskHA {
		t.Fatalf("step 1 = %+v", ask)
	}
	// The terminal from here asks for the address; it never asks for the
	// saved home's six-digit code (the old App's update).
	page.click("terminal", "")
	handed := page.waitFor("the terminal", func(s setupPageState) bool { return s.Screen == "terminal" })
	if handed.Handover != "" {
		t.Fatalf("handover = %q, want the address question", handed.Handover)
	}
	setupPageExit(t, done, stdout) // the terminal's question meets no input here
	asked := strings.Contains(stdout.String(), "Enter a different address") || strings.Contains(stdout.String(), "Home Assistant address (") // with or without a Home Assistant found on this network
	if out := stdout.String(); !asked || strings.Contains(out, "six-digit") {
		t.Fatalf("terminal:\n%s", out)
	}
}

// With Home Assistant Link set up for the saved home, another address stops
// before any sign-in there and says why; its button removes Link (as
// `ha-nova cloud remove`), and then the other address goes through.
func TestSetupPageRerunAnotherHomeRemovesLinkFirst(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, errors.New("not read"))
	previous, previousRemove := haAppInstallTargetsThisHAForPage, removeLeftoverLink
	t.Cleanup(func() { haAppInstallTargetsThisHAForPage, removeLeftoverLink = previous, previousRemove })
	haAppInstallTargetsThisHAForPage = func(runtimeConfig) bool { return true }
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateReady, Current: &current}
	cfg.RelayInstanceID = "relay-a"
	if !setupPageCloudOK(cfg) {
		t.Skip("Link isn't available on this platform: the page doesn't run with it")
	}
	previousResolve := resolveHAURLBaseForSetup
	t.Cleanup(func() { resolveHAURLBaseForSetup = previousResolve })
	resolveHAURLBaseForSetup = func(input string) (string, error) { return "http://" + input + ":8123", nil }
	removed := 0
	removeLeftoverLink = func(paths runtimePaths) int {
		removed++
		saved, err := loadConfig(paths)
		if err != nil {
			t.Fatal(err)
		}
		saved.Cloud = nil
		if err := saveConfig(paths, saved); err != nil {
			t.Fatal(err)
		}
		return 0
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	previousIdle := setupPageIdle
	setupPageIdle = 2 * time.Second
	t.Cleanup(func() { setupPageIdle = previousIdle })
	if err := os.MkdirAll(filepath.Dir(paths.VersionFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.VersionFile, []byte(`{"skill_version":"0.27.0","min_relay_version":"0.12.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))
	page.waitFor("the re-run screen", accepting("change"))
	page.click("change", "")
	page.waitFor("step 1", accepting("address"))
	page.click("address", "192.0.2.10")
	blocked := page.waitFor("the Link problem", accepting("link_remove"))
	if blocked.Screen != "found" || !blocked.AskHA || blocked.Problem.Key != "setup.page.problem.link_elsewhere" || removed != 0 {
		t.Fatalf("blocked = %+v removed=%d", blocked, removed)
	}
	page.click("link_remove", "")
	after := page.waitFor("Link removed", func(s setupPageState) bool { return !slices.Contains(s.Accepts, "link_remove") && s.Problem == nil })
	if after.AskHA || after.HA != "192.0.2.10" || removed != 1 {
		t.Fatalf("after = %+v removed=%d: not on with the address Link held back", after, removed)
	}
	page.click("terminal", "")
	setupPageExit(t, done, stdout)
}

// Another address starts unpaired: "Use a six-digit code" then asks for a
// code instead of checking the old home's device.
func TestSetupPageAnotherAddressStartsUnpaired(t *testing.T) {
	f := &setupFlow{cfg: runtimeConfig{HAHost: "a", HAURL: "http://a:8123"}, deviceAlreadyPaired: true, devicePaired: true}
	f.useSetupAddress("a", "http://a:8123")
	if !f.deviceAlreadyPaired || !f.devicePaired {
		t.Fatal("the same home forgot its pairing")
	}
	f.useSetupAddress("b", "http://b:8123")
	if f.deviceAlreadyPaired || f.devicePaired || f.cfg.HAURL != "http://b:8123" {
		t.Fatalf("flow = %+v", f)
	}
}
