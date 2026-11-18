package main

import (
	"bufio"
	"os"
	"strings"
	"testing"
)

// A Mac paired while its App didn't know the window (the App answers
// pair_again) pairs again with setup although its key works: at home the
// local sign-in with the one-click question, Link again, then the old
// version's cleanup.
func TestWindowRePairAtHomePairsAgainWithAWorkingKey(t *testing.T) {
	f := newRelinkFixture(t, defaultServerProfileName, true, relinkHybridConfig(t))
	if err := writeDeviceCredential(generateTestDeviceCredential(t)); err != nil {
		t.Fatal(err)
	}
	code, output := f.runForced("\n")
	if code != 0 || len(f.armed) != 1 || !f.armed[0] || f.cleanup.reported != 1 {
		t.Fatalf("code %d, armed %v, cleanup reported %d\n%s", code, f.armed, f.cleanup.reported, output)
	}
	cfg := f.config()
	if !cfg.Cloud.ready() || cfg.LinkRePair != "" || cfg.RelaySecureBaseURL != "https://ha:18793" {
		t.Fatalf("config %+v", cfg)
	}
	for _, want := range []string{setupT("setup.relink.home"), setupT("setup.own.question"), setupT("setup.relink.home_done")} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q\n%s", want, output)
		}
	}
}

// A Mac without Link pairs again by the local sign-in only: no Link sign-in,
// no Link removal, no Link texts.
func TestWindowRePairWithoutLinkPairsOnlyLocally(t *testing.T) {
	cfg := relinkHybridConfig(t)
	cfg.Cloud, cfg.RoutePolicy = nil, routePolicyLocal
	f := newRelinkFixture(t, defaultServerProfileName, true, cfg)
	if err := writeDeviceCredential(generateTestDeviceCredential(t)); err != nil {
		t.Fatal(err)
	}
	code, output := f.runForced("\n")
	if code != 0 || len(f.armed) != 1 || !f.armed[0] || f.removes != 0 || len(f.coord.owns) != 0 || f.cleanup.reported != 1 {
		t.Fatalf("code %d, armed %v, removes %d, Link pairings %v, cleanup %d\n%s", code, f.armed, f.removes, f.coord.owns, f.cleanup.reported, output)
	}
	after := f.config()
	if after.Cloud != nil || after.LinkRePair != "" || after.LinkRePairOrigin != "" || after.RelaySecureBaseURL != "https://ha:18793" {
		t.Fatalf("config %+v", after)
	}
	if !strings.Contains(output, setupT("setup.relink.window")) || strings.Contains(output, "Home Assistant Link") {
		t.Fatalf("output:\n%s", output)
	}
}

// Away, a Mac without Link can't pair again: setup says to run it at home.
func TestWindowRePairWithoutLinkAwayNamesHome(t *testing.T) {
	cfg := relinkHybridConfig(t)
	cfg.Cloud, cfg.RoutePolicy = nil, routePolicyLocal
	f := newRelinkFixture(t, defaultServerProfileName, false, cfg)
	code, output := f.runForced("\n")
	if code != 1 || len(f.armed) != 0 || !strings.Contains(output, setupT("setup.relink.home_needed", "command", "ha-nova setup")) {
		t.Fatalf("code %d, armed %v\n%s", code, f.armed, output)
	}
}

func (f *relinkFixture) runForced(input string) (int, string) {
	f.t.Helper()
	cfg, err := loadSelectedRuntimeConfigUnchecked(f.paths)
	if err != nil {
		f.t.Fatal(err)
	}
	lifecycle := setupLifecycleOf(f.t, f.paths)
	return captureCommandOutput(f.t, func() int {
		return runLinkRePair(bufio.NewReader(strings.NewReader(input)), os.Stdout, f.paths, cfg, lifecycle, true)
	})
}

// A Mac paired again through Link only keeps its local address but no pinned
// one: its key is in the device slot, never the legacy token.
func TestServerListLinkOnlyIsNoLegacyToken(t *testing.T) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := relinkHybridConfig(t)
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cfg.RoutePolicy = "", "", routePolicyCloud
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredential(generateTestDeviceCredential(t)); err != nil {
		t.Fatal(err)
	}
	_, out := captureCommandOutput(t, func() int { return runServerCommand(paths, []string{"list"}) })
	if strings.Contains(out, "legacy token") || !strings.Contains(out, "yes") {
		t.Fatalf("list:\n%s", out)
	}
}

// Away, a working key never pairs again over Link: setup says to run it at
// home and changes nothing.
func TestWindowRePairAwayChangesNothing(t *testing.T) {
	f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
	before := f.config()
	code, output := f.runForced("\n")
	if code != 1 || f.removes != 0 || len(f.coord.owns) != 0 || len(f.armed) != 0 || !strings.Contains(output, setupT("setup.relink.home_needed", "command", "ha-nova setup")) {
		t.Fatalf("code %d, removes %d, Link pairings %v, armed %v\n%s", code, f.removes, f.coord.owns, f.armed, output)
	}
	if after := f.config(); after.LinkRePair != "" || !after.Cloud.ready() || after.RelaySecureBaseURL != before.RelaySecureBaseURL {
		t.Fatalf("config changed: %+v", after)
	}
}
