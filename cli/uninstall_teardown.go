package main

import (
	"fmt"
	"io"
)

// teardownOutcome: whether uninstall removed the NOVA App from Home Assistant
// (uninstall_app_removal.go) before the local removal.
type teardownOutcome int

const (
	teardownNotOffered teardownOutcome = iota
	teardownCompleted
)

// teardownCompletedNoteLines replaces the shared-App note once the App is
// gone. Standard mode keeps the connection config on purpose, so it gets the
// pointing-at-nothing hint (a Windows recovery can carry that mode).
func teardownCompletedNoteLines(mode uninstallMode) []string {
	notes := []string{"Removed the NOVA App from Home Assistant."}
	if mode != uninstallModePurge {
		notes = append(notes, "The kept connection config now points at nothing. Run 'ha-nova uninstall --purge' to clear it, or keep it for a reinstall.")
	}
	return notes
}

func printTeardownCompletedNotes(out io.Writer, mode uninstallMode) {
	session := resolveStatusUISession(out)
	for _, note := range teardownCompletedNoteLines(mode) {
		fmt.Fprintf(out, "  %s %s\n", session.style("success", session.successMarker()), note)
	}
}
