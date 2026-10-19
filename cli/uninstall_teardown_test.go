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

// Without the menu's App choice the shared App always stays, and the note
// names its page and what removing it costs.
func TestPreflightNotesKeepTheSharedApp(t *testing.T) {
	notes := preflightNoteLines(uninstallPreflight{haURL: "http://ha.local:8123", tokenUnavailable: "token problem"})
	if len(notes) != 2 || !strings.Contains(notes[0], "stays") ||
		!strings.Contains(notes[0], "disconnects every paired computer") ||
		!strings.Contains(notes[0], haRelayAppPageURL("http://ha.local:8123")) || notes[1] != "token problem" {
		t.Fatalf("notes = %#v", notes)
	}
	if notes := preflightNoteLines(uninstallPreflight{}); len(notes) != 1 || strings.Contains(notes[0], "http") {
		t.Fatalf("notes without an HA URL = %#v", notes)
	}
}
