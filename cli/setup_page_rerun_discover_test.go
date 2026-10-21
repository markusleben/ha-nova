package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// A re-run whose saved Home Assistant doesn't answer looks for another one
// (it moved, or this is another home) and offers it; nothing switches
// without a click, and the click goes the usual way: step 1 with that address.

// stubRerunDown: the NOVA App doesn't answer; Home Assistant answers while
// haUp holds. Discovery returns found and counts its runs.
func stubRerunDown(t *testing.T, cfg *runtimeConfig, haUp *atomic.Bool, found []setupDiscoveryCandidate) *atomic.Int32 {
	t.Helper()
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	cfg.RelayBaseURL = dead.URL
	stubRerunApp(t, false, chatAppsHA{}, errors.New("unreachable"))
	previousIdle, previousProbe, previousDiscover, previousResolve := setupPageIdle, probeHTTPForSetup, discoverReachableHAHostsForSetup, resolveHAURLBaseForSetup
	t.Cleanup(func() {
		setupPageIdle, probeHTTPForSetup, discoverReachableHAHostsForSetup, resolveHAURLBaseForSetup = previousIdle, previousProbe, previousDiscover, previousResolve
	})
	setupPageIdle = 2 * time.Second
	probeHTTPForSetup = func(string) error {
		if haUp.Load() {
			return nil
		}
		return errors.New("no answer")
	}
	var runs atomic.Int32
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		runs.Add(1)
		return found, ""
	}
	resolveHAURLBaseForSetup = func(input string) (string, error) {
		if input == "http://192.0.2.30:8123" { // gone again by the click
			return "", errors.New("unreachable")
		}
		return input, nil
	}
	return &runs
}

func TestSetupPageRerunOffersAHomeAssistantFoundElsewhere(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	var haUp atomic.Bool
	runs := stubRerunDown(t, &cfg, &haUp, []setupDiscoveryCandidate{
		{Host: cfg.HAHost, HAURL: cfg.HAURL}, // the saved one: not offered
		{Host: "192.0.2.20", HAURL: "http://192.0.2.20:8123", Via: "homeassistant.local"},
		{Host: "192.0.2.30", HAURL: "http://192.0.2.30:8123", Via: "192.0.2.30"},
	})
	savedBefore, err := readSetupConfigSnapshot(paths)
	if err != nil {
		t.Fatal(err)
	}
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))

	offer := page.waitFor("the found Home Assistant", func(s setupPageState) bool { return len(s.Candidates) > 0 && slices.Contains(s.Accepts, "address") })
	if offer.Screen != "rerun_fix" || offer.Problem == nil || offer.Problem.Key != "setup.page.rerun.problem_app" ||
		!slices.Equal(offer.Candidates, []setupPageCandidate{{Host: "homeassistant.local (192.0.2.20)", URL: "http://192.0.2.20:8123"}, {Host: "192.0.2.30", URL: "http://192.0.2.30:8123"}}) ||
		!slices.Contains(offer.Accepts, "recheck") || !slices.Contains(offer.Accepts, "fix") {
		t.Fatalf("offer = %+v", offer)
	}
	// Only an address it found: anything else keeps the screen.
	page.click("address", "http://198.51.100.9:8123")
	page.waitFor("the screen again", func(s setupPageState) bool { return runs.Load() == 2 && slices.Contains(s.Accepts, "address") })
	if again := page.ui.snapshot(); again.Screen != "rerun_fix" {
		t.Fatalf("an address it didn't find was taken: %+v", again)
	}
	// One that doesn't answer by the click: said, and no longer offered.
	page.click("address", "http://192.0.2.30:8123")
	gone := page.waitFor("the pick that didn't answer", func(s setupPageState) bool {
		return s.Problem != nil && s.Problem.Key == "setup.page.problem.address" && len(s.Candidates) == 1 && slices.Contains(s.Accepts, "address")
	})
	if gone.Problem.Args["address"] != "192.0.2.30" || gone.Candidates[0].URL != "http://192.0.2.20:8123" || gone.Screen != "rerun_fix" {
		t.Fatalf("gone = %+v", gone)
	}
	time.Sleep(100 * time.Millisecond) // a later search's result: still without it
	if again := page.ui.snapshot(); len(again.Candidates) != 1 {
		t.Fatalf("a pick that didn't answer came back: %+v", again.Candidates)
	}
	page.click("address", "http://192.0.2.20:8123")
	step1 := page.waitFor("step 1 with the found address", func(s setupPageState) bool { return s.Screen == "found" && slices.Contains(s.Accepts, "address") })
	if step1.HA != "192.0.2.20" || step1.Candidates != nil || step1.Typed {
		t.Fatalf("step 1 = %+v", step1)
	}
	if savedAfter, err := readSetupConfigSnapshot(paths); err != nil || string(savedAfter) != string(savedBefore) {
		t.Fatalf("the click alone changed the saved config (err %v)", err)
	}
	if code := setupPageExit(t, done, stdout); code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}

// Home Assistant answers (only the App doesn't): no search, and what an
// earlier search found goes away.
func TestSetupPageRerunLooksOnlyWhileHomeAssistantIsAway(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	var haUp atomic.Bool
	haUp.Store(true)
	runs := stubRerunDown(t, &cfg, &haUp, []setupDiscoveryCandidate{{Host: "192.0.2.20", HAURL: "http://192.0.2.20:8123"}})
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))

	appOnly := page.waitFor("the re-run screen", accepting("recheck"))
	if appOnly.Problem == nil || appOnly.Problem.Key != "setup.page.rerun.problem_app_only" || slices.Contains(appOnly.Accepts, "address") || runs.Load() != 0 {
		t.Fatalf("app only = %+v, %d searches", appOnly, runs.Load())
	}
	haUp.Store(false)
	page.click("recheck", "")
	page.waitFor("the found Home Assistant", func(s setupPageState) bool { return len(s.Candidates) == 1 })
	haUp.Store(true)
	page.click("recheck", "")
	back := page.waitFor("Home Assistant back", func(s setupPageState) bool {
		return s.Problem != nil && s.Problem.Key == "setup.page.rerun.problem_app_only" && slices.Contains(s.Accepts, "recheck")
	})
	if back.Candidates != nil || slices.Contains(back.Accepts, "address") || runs.Load() != 1 {
		t.Fatalf("back = %+v, %d searches", back, runs.Load())
	}
	if code := setupPageExit(t, done, stdout); code != 0 {
		t.Fatalf("exit %d\n%s", code, stdout)
	}
}
