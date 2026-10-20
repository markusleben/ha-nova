package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

var readRelayAuthTokenForUninstall = readRelayAuthToken
var deleteRelayAuthTokenForUninstall = deleteRelayAuthToken

// uninstallArea groups removed items into the few plain lines the summary
// shows. The zero value is HA NOVA's own files, so no item goes unlisted.
type uninstallArea int

const (
	uninstallAreaFiles uninstallArea = iota
	uninstallAreaSkills
	uninstallAreaProgram
	uninstallAreaSettings
	uninstallAreaPairing
	uninstallAreaLink
	uninstallAreaApp
)

// uninstallAreaLines: the summary's order and words, the same as the
// preview's (renderUninstallPreflight).
var uninstallAreaLines = []struct {
	area uninstallArea
	line string
}{
	{uninstallAreaApp, "The NOVA App in Home Assistant"},
	{uninstallAreaSkills, "HA NOVA's skills in your AI apps"},
	{uninstallAreaProgram, "The ha-nova program"},
	{uninstallAreaFiles, "HA NOVA's own files on this computer"},
	{uninstallAreaSettings, "HA NOVA's saved Home Assistant address"},
	{uninstallAreaPairing, "This computer's HA NOVA key"},
	{uninstallAreaLink, "Your Home Assistant Link sign-in"},
}

// What "keep the pairing" keeps, in the preview's words.
const (
	uninstallKeptPairing     = "HA NOVA's saved Home Assistant address and this computer's key, for a reinstall"
	uninstallKeptPairingLink = "HA NOVA's saved Home Assistant address, this computer's key, and your Link sign-in, for a reinstall"
	uninstallKeptLink        = "HA NOVA's saved Home Assistant address and your Link sign-in, for a reinstall"
)

// uninstallReport collects what a removal did. The summary names areas, not
// paths; the paths and success details print only with --verbose. Notes are
// problems and next steps, so they always print.
type uninstallReport struct {
	removed []string
	areas   map[uninstallArea]bool
	area    uninstallArea // where addRemoved files the next item
	notes   []string
	details []string    // --verbose only
	kept    [][2]string // what stays, in plain words, and its next step
	verbose bool
}

// setArea files the next removed items under area.
func (r *uninstallReport) setArea(area uninstallArea) {
	if r != nil {
		r.area = area
	}
}

func (r *uninstallReport) addRemoved(item string) {
	r.addRemovedIn(r.currentArea(), item)
}

func (r *uninstallReport) currentArea() uninstallArea {
	if r == nil {
		return uninstallAreaFiles
	}
	return r.area
}

func (r *uninstallReport) addRemovedIn(area uninstallArea, item string) {
	if r == nil || item == "" {
		return
	}
	r.removed = append(r.removed, item)
	if r.areas == nil {
		r.areas = map[uninstallArea]bool{}
	}
	r.areas[area] = true
}

func (r *uninstallReport) addNote(note string) {
	if r == nil || note == "" || slices.Contains(r.notes, note) {
		return
	}
	r.notes = append(r.notes, note)
}

// addDetail records a success detail (a revoke, a path) for --verbose.
func (r *uninstallReport) addDetail(detail string) {
	if r == nil || detail == "" {
		return
	}
	r.details = append(r.details, detail)
}

func (r *uninstallReport) addKept(item, next string) {
	if r == nil || item == "" {
		return
	}
	r.kept = append(r.kept, [2]string{item, next})
}

// print shows the plain summary: what went, what stays, any problem.
func (r *uninstallReport) print() bool {
	if r == nil {
		return false
	}
	if r.verbose {
		fmt.Fprintln(os.Stdout)
		r.printDetails()
	}
	if len(r.removed) == 0 && len(r.kept) == 0 {
		r.printNotes()
		printHumanInfo("Nothing to remove — HA NOVA was not installed.")
		return false
	}
	out := os.Stdout
	session := resolveStatusUISession(out)
	fmt.Fprintln(out)
	if len(r.removed) > 0 {
		fmt.Fprintf(out, "  %s\n", session.style("strong", "Removed:"))
	}
	for _, area := range uninstallAreaLines {
		if r.areas[area.area] {
			fmt.Fprintf(out, "    %s %s\n", session.bullet(), area.line)
		}
	}
	r.printStays(out)
	fmt.Fprintln(out)
	r.printNotes()
	if len(r.removed) == 0 {
		printHumanInfo("Nothing else to remove.")
		return false
	}
	printHumanInfo("HA NOVA removed from this computer.")
	return true
}

func (r *uninstallReport) printStays(out io.Writer) {
	if len(r.kept) == 0 {
		return
	}
	session := resolveStatusUISession(out)
	fmt.Fprintf(out, "  %s\n", session.style("strong", "Stays:"))
	for _, kept := range r.kept {
		fmt.Fprintf(out, "    %s %s\n", session.bullet(), kept[0])
		if kept[1] != "" {
			fmt.Fprintf(out, "      %s\n", kept[1])
		}
	}
}

// printDetails prints every removed item and note as is (server remove,
// failed runs, --verbose).
func (r *uninstallReport) printDetails() {
	if r == nil {
		return
	}
	for _, item := range r.removed {
		printHumanInfo("Removed: %s", item)
	}
	for _, detail := range r.details {
		printHumanInfo("%s", detail)
	}
	if !r.verbose { // --verbose prints the notes after the summary
		r.printNotes()
	}
}

func (r *uninstallReport) printNotes() {
	for _, note := range r.notes {
		printHumanInfo("%s", note)
	}
}

// reportKeptPairing: what "keep the pairing" kept, in the summary's words;
// the exact wording, with the --purge hint, for --verbose.
func reportKeptPairing(paths runtimePaths, report *uninstallReport) {
	if cloudConfigurationExistsForUninstall(paths.ConfigFile) &&
		deviceCredentialExistsForUninstall(paths.ConfigFile) {
		report.addKept(uninstallKeptPairingLink, "")
		report.addDetail("Kept Home Assistant connection config, this device's pairing, and its Cloud authorization. Use 'ha-nova uninstall --purge' to remove and revoke them too.")
	} else if cloudConfigurationExistsForUninstall(paths.ConfigFile) {
		report.addKept(uninstallKeptLink, "")
		report.addDetail("Kept Home Assistant connection config and its Cloud authorization. Use 'ha-nova uninstall --purge' to remove and revoke them too.")
	} else if deviceCredentialExistsForUninstall(paths.ConfigFile) {
		report.addKept(uninstallKeptPairing, "")
		report.addDetail("Kept Home Assistant connection config and this device's pairing. Use 'ha-nova uninstall --purge' to remove and revoke them too.")
	} else if fileExists(paths.ConfigFile) || relayAuthTokenExistsForUninstall() {
		report.addKept(uninstallKeptPairing, "")
		report.addDetail("Kept Home Assistant connection config and stored relay token. Use 'ha-nova uninstall --purge' to remove them too.")
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
			if purgeSkipsRelayTokenError(err) {
				report.addNote(fmt.Sprintf("HA NOVA couldn't check this computer's keyring for its old relay key from here. If this computer has a desktop, open Passwords and Keys there and delete the '%s' entry.", relayAuthTokenServiceName()))
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

// managedServiceTokenFilePath resolves a configured relay token file and
// accepts only HA NOVA's own managed file; full purge never deletes any other.
func managedServiceTokenFilePath(paths runtimePaths, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(paths.ConfigDir, path)
	}
	path = filepath.Clean(path)
	managedTokenPath := filepath.Clean(defaultRelayAuthTokenFile(paths))
	if !uninstallPathsEqual(path, managedTokenPath) {
		return "", fmt.Errorf(
			"configured relay token file %s is not the managed service-token path %s; refusing to delete it. Nothing was removed: delete or move that file yourself and remove `relay_token_file` from %s, then rerun the purge",
			path,
			managedTokenPath,
			paths.ConfigFile,
		)
	}
	return path, nil
}

// validatePurgeServiceTokenFile refuses a full purge up front — before any
// revoke or removal, never halfway through — when the configured relay token
// file is not HA NOVA's managed one.
func validatePurgeServiceTokenFile(paths runtimePaths) error {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return nil // no readable config: no token file to protect
	}
	cfg, ok := doc.flatProfile(defaultServerProfileName)
	if !ok || strings.TrimSpace(cfg.RelayTokenFile) == "" {
		return nil
	}
	_, err = managedServiceTokenFilePath(paths, strings.TrimSpace(cfg.RelayTokenFile))
	return err
}

func applyUninstallServiceTokenFilePolicy(paths runtimePaths, path string, report *uninstallReport) (bool, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return false, nil
	}
	path, err := managedServiceTokenFilePath(paths, path)
	if err != nil {
		return true, err
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
