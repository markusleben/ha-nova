package main

import (
	"strings"
	"testing"
)

func TestTeardownCompletedNoteLinesByMode(t *testing.T) {
	// The removal itself said "NOVA App removed from Home Assistant" as it
	// happened: the notes never repeat it (lane S4).
	standard := teardownCompletedNoteLines(uninstallModeStandard)
	if len(standard) != 1 || !strings.Contains(standard[0], "--purge") {
		t.Fatalf("standard mode must hint at the kept config: %#v", standard)
	}
	if purge := teardownCompletedNoteLines(uninstallModePurge); len(purge) != 0 {
		t.Fatalf("purge mode needs no note: %#v", purge)
	}
}

// Without the menu's App choice the shared App always stays: one plain line
// and next step, the exact links only for --verbose, or a standalone Relay.
func TestPreflightStaysKeepTheSharedApp(t *testing.T) {
	app := preflightStays(uninstallPreflight{haURL: "http://ha.local:8123", tokenUnavailable: "token problem",
		config: runtimeConfig{RelayBaseURL: "http://ha.local:8791"}})
	if app.kept != "The NOVA App in Home Assistant. Other computers may still use it." ||
		app.next != "If none do, remove it in Home Assistant: Settings → Apps → NOVA Relay → Uninstall." ||
		len(app.details) != 2 || !strings.Contains(app.details[0], haRelayAppPageURL("http://ha.local:8123")) ||
		!strings.Contains(app.details[1], "three-dot menu (top right) → Repositories → "+haNovaRepositoryURL) ||
		len(app.problems) != 1 || app.problems[0] != "token problem" {
		t.Fatalf("App stays = %#v", app)
	}
	standalone := preflightStays(uninstallPreflight{haURL: "http://ha.local:8123", config: runtimeConfig{RelayBaseURL: "http://nas.local:8791"}})
	if standalone.kept != "The NOVA Relay. Other computers may still use it." || strings.Contains(standalone.next, "http") || len(standalone.details) != 0 {
		t.Fatalf("standalone stays = %#v", standalone)
	}
	if never := preflightStays(uninstallPreflight{}); never.kept != "" || never.next != "" {
		t.Fatalf("never paired: nothing stays, got %#v", never)
	}
}
