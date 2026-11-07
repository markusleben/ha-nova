package main

import (
	"strings"
	"testing"
)

// Over Home Assistant Link doctor can't ask Home Assistant to look for a
// newer App, and Home Assistant may not list it yet: the floor words name
// the "Check for updates" step too.
func TestDoctorFloorNoticeOverLinkNamesCheckForUpdates(t *testing.T) {
	floor := humanNotice{level: humanNoticeNote, kind: humanNoticeKindRelayOutdated,
		message: "The NOVA App in Home Assistant needs an update: it has 0.13.20, HA NOVA needs 0.13.21 or newer. " + relayAppUpdatePath}
	got := doctorFloorNotice(runtimePaths{}, floor, true, false)
	want := floor.message + " No Update button yet? In Home Assistant: Settings → ⋮ (top right) → Check for updates first."
	if got.message != want || got.kind != humanNoticeKindRelayOutdated || got.level != humanNoticeNote {
		t.Fatalf("over Link: %+v, want message %q", got, want)
	}
	if home := doctorFloorNotice(runtimePaths{}, floor, false, true); home.message != floor.message || strings.Contains(home.message, "Check for updates") {
		t.Fatalf("a diagnostic-only route keeps the floor words: %q", home.message)
	}
}
