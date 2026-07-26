package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
			if isDesktopKeyringUnavailableError(err) {
				report.addNote("Relay auth token cleanup skipped: system secure storage is unavailable on this machine.")
				return nil
			}
			return err
		}
		if strings.TrimSpace(token) == "" {
			return nil
		}
		if err := deleteRelayAuthTokenForUninstall(); err != nil {
			if isDesktopKeyringUnavailableError(err) {
				report.addNote("Relay auth token cleanup skipped after runtime removal: system secure storage is unavailable on this machine.")
				return nil
			}
			return err
		}
		report.addRemoved("relay auth token")
	}
	return nil
}

// applyUninstallKeyringTokenBestEffort removes a leftover relay token from
// the OS credential store after the service token file was already handled.
// An earlier desktop-mode setup may have stored a token there before the
// user switched to service mode. Service machines often run headless with a
// locked or absent keyring, so failures only add a note instead of aborting
// the purge.
func applyUninstallKeyringTokenBestEffort(report *uninstallReport) {
	if !shouldDeleteRelayAuthTokenOnUninstall() {
		return
	}
	token, err := readRelayAuthTokenForUninstall()
	if err != nil {
		if !isMissingRelayAuthTokenError(err) {
			report.addNote("Could not check the OS credential store for a leftover relay token. If an earlier desktop-mode setup stored one, remove the 'ha-nova' entry manually.")
		}
		return
	}
	if strings.TrimSpace(token) == "" {
		return
	}
	if err := deleteRelayAuthTokenForUninstall(); err != nil {
		report.addNote("Could not remove the leftover relay token from the OS credential store. Remove the 'ha-nova' entry manually.")
		return
	}
	report.addRemoved("relay auth token (OS credential store)")
}

func applyUninstallServiceTokenFilePolicy(paths runtimePaths, path string, report *uninstallReport) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(paths.ConfigDir, path)
	}
	path = filepath.Clean(path)
	configDir := filepath.Clean(paths.ConfigDir)
	rel, err := filepath.Rel(configDir, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false, nil
	}
	managedTokenPath := filepath.Clean(defaultRelayAuthTokenFile(paths))
	if !uninstallPathsEqual(path, managedTokenPath) {
		return true, fmt.Errorf(
			"configured relay token file is not the managed service-token path %s; refusing to delete it",
			managedTokenPath,
		)
	}
	info, err := os.Lstat(path)
	if err != nil {
		if isNotExist(err) {
			return true, nil
		}
		return true, err
	}
	if info.IsDir() {
		return true, nil
	}
	if err := os.Remove(path); err != nil && !isNotExist(err) {
		return true, err
	}
	report.addRemoved("relay auth token")
	return true, nil
}

func uninstallPathsEqual(left string, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func applyFullPurgeRelayTokenPolicy(
	paths runtimePaths,
	relayTokenFile string,
	report *uninstallReport,
) error {
	tokenFileHandled := false
	if relayTokenFile != "" {
		var err error
		tokenFileHandled, err = applyUninstallServiceTokenFilePolicy(
			paths,
			relayTokenFile,
			report,
		)
		if err != nil {
			return err
		}
	}
	if tokenFileHandled {
		restoreSuppression := withRelayAuthTokenFileSuppressed()
		applyUninstallKeyringTokenBestEffort(report)
		restoreSuppression()
		return nil
	}
	restoreSuppression := func() {}
	if relayTokenFile != "" {
		// The configured token file lies outside the managed config
		// directory. Never delete user-managed files; clean only the OS
		// keyring copy.
		restoreSuppression = withRelayAuthTokenFileSuppressed()
		report.addNote(fmt.Sprintf(
			"Kept the relay token file outside the HA NOVA config directory: %s",
			relayTokenFile,
		))
	}
	if err := applyUninstallTokenPolicy(report); err != nil {
		restoreSuppression()
		return err
	}
	restoreSuppression()
	return nil
}
