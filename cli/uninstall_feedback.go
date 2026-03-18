package main

import (
	"os"
	"strings"
)

var readRelayAuthTokenForUninstall = readRelayAuthToken
var deleteRelayAuthTokenForUninstall = deleteRelayAuthToken

type uninstallReport struct {
	removed []string
	notes   []string
}

func (r *uninstallReport) addRemoved(item string) {
	if r == nil || item == "" {
		return
	}
	r.removed = append(r.removed, item)
}

func (r *uninstallReport) addNote(note string) {
	if r == nil || note == "" {
		return
	}
	r.notes = append(r.notes, note)
}

func (r *uninstallReport) print() bool {
	if r == nil {
		return false
	}
	r.printDetails()
	if len(r.removed) > 0 {
		printHumanInfo("Done. Removed %d items.", len(r.removed))
		return true
	}
	printHumanInfo("Nothing to remove — HA NOVA was not installed.")
	return false
}

func (r *uninstallReport) printDetails() {
	if r == nil {
		return
	}
	for _, item := range r.removed {
		printHumanInfo("Removed: %s", item)
	}
	for _, note := range r.notes {
		printHumanInfo("%s", note)
	}
}

func removePathWithReport(path string, report *uninstallReport) error {
	if path == "" {
		return nil
	}
	if _, err := os.Lstat(path); err != nil {
		if isNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(path); err != nil {
		return err
	}
	report.addRemoved(path)
	return nil
}

func applyUninstallTokenPolicy(report *uninstallReport) error {
	if shouldDeleteRelayAuthTokenOnUninstall() {
		token, err := readRelayAuthTokenForUninstall()
		if err != nil {
			if isMissingRelayAuthTokenError(err) {
				return nil
			}
			return err
		}
		if strings.TrimSpace(token) == "" {
			return nil
		}
		if err := deleteRelayAuthTokenForUninstall(); err != nil {
			return err
		}
		report.addRemoved("relay auth token")
	}
	return nil
}
