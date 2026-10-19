package main

import (
	"strings"
	"testing"
)

func TestTeardownCompletedNoteLinesByMode(t *testing.T) {
	standard := teardownCompletedNoteLines(uninstallModeStandard)
	if len(standard) != 2 || !strings.Contains(standard[1], "--purge") {
		t.Fatalf("standard mode must hint at the kept config: %#v", standard)
	}
	purge := teardownCompletedNoteLines(uninstallModePurge)
	if len(purge) != 1 || purge[0] != "Removed the NOVA App from Home Assistant." {
		t.Fatalf("purge mode needs no kept-config hint: %#v", purge)
	}
}

// Without the menu's App choice the shared App always stays, and the notes
// say how to remove it by hand: the App and its source, or a standalone Relay.
func TestPreflightNotesKeepTheSharedApp(t *testing.T) {
	app := uninstallPreflight{haURL: "http://ha.local:8123", tokenUnavailable: "token problem",
		config: runtimeConfig{RelayBaseURL: "http://ha.local:8791"}}
	notes := preflightNoteLines(app)
	if len(notes) != 3 || !strings.Contains(notes[0], "NOVA App in Home Assistant stays") ||
		!strings.Contains(notes[0], "disconnects every paired computer") ||
		!strings.Contains(notes[0], haRelayAppPageURL("http://ha.local:8123")) ||
		!strings.Contains(notes[1], haNovaRepositoryURL) || notes[2] != "token problem" {
		t.Fatalf("App notes = %#v", notes)
	}
	standalone := preflightNoteLines(uninstallPreflight{haURL: "http://ha.local:8123", config: runtimeConfig{RelayBaseURL: "http://nas.local:8791"}})
	if len(standalone) != 1 || !strings.Contains(standalone[0], "The NOVA Relay stays") || strings.Contains(standalone[0], "http") {
		t.Fatalf("standalone notes = %#v", standalone)
	}
}
