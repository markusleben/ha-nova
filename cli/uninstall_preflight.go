package main

import (
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

type uninstallPreflight struct {
	tokenUnavailable string
	haURL            string
	config           runtimeConfig
}

// uninstallRelayRemovalEvidence records only Relay removals that guided
// teardown completed and identified exactly. A missing profile or Relay
// identity is never evidence that another Relay is gone.
type uninstallRelayRemovalEvidence map[string]string

func uninstallRelayRemovalEvidenceFromPreflight(
	preflight uninstallPreflight,
	teardownCompleted bool,
) uninstallRelayRemovalEvidence {
	return uninstallRelayRemovalEvidenceForDefault(
		preflight.config.RelayInstanceID,
		teardownCompleted,
	)
}

func uninstallRelayRemovalEvidenceForDefault(
	relayInstanceID string,
	teardownCompleted bool,
) uninstallRelayRemovalEvidence {
	relayInstanceID = strings.TrimSpace(relayInstanceID)
	if !teardownCompleted || relayInstanceID == "" {
		return nil
	}
	return uninstallRelayRemovalEvidence{
		defaultServerProfileName: relayInstanceID,
	}
}

func (evidence uninstallRelayRemovalEvidence) matches(
	profileName string,
	relayInstanceID string,
) bool {
	expectedRelayInstanceID, exists := evidence[profileName]
	return exists &&
		strings.TrimSpace(relayInstanceID) != "" &&
		expectedRelayInstanceID == strings.TrimSpace(relayInstanceID)
}

func renderUninstallPreflight(out io.Writer, paths runtimePaths, source string) {
	session := resolveStatusUISession(out)
	renderSimpleHeader(out, session, "HA NOVA Uninstall")
	fmt.Fprintf(out, "  %s\n", session.style("strong", "Removes from this computer:"))
	fmt.Fprintf(out, "    %s Skills from installed AI clients\n", session.bullet())
	fmt.Fprintf(out, "    %s %s\n", session.bullet(), uninstallRuntimeLineLabel(paths, source))
	fmt.Fprintf(out, "    %s Managed local state and cache\n", session.bullet())
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", session.style("strong", "Also removed unless this computer stays connected:"))
	fmt.Fprintf(out, "    %s Home Assistant connection config\n", session.bullet())
	if label := uninstallTokenLineLabel(paths); label != "" {
		fmt.Fprintf(out, "    %s %s\n", session.bullet(), label)
	}
	fmt.Fprintf(out, "    %s Home Assistant Link authorization (when configured)\n", session.bullet())
	fmt.Fprintln(out)
	if warning := uninstallForeignTokenFileWarning(paths); warning != "" {
		renderSetupParagraphTight(out, session.style("warning", warning))
	}
	if runtime.GOOS == "windows" && source == installSourceBundle {
		renderSetupParagraphTight(out, session.style("muted", uninstallWindowsBundleNote()))
	}
	fmt.Fprintln(out)
}

func uninstallRuntimeLineLabel(paths runtimePaths, source string) string {
	switch source {
	case installSourceLegacyWindowsPackage:
		return "Legacy Windows package install (remove via Installed Apps / App Installer)"
	case installSourceBundle:
		return "Installed CLI runtime (" + filepath.Join(paths.InstallRoot, publicBinaryName()) + ")"
	default:
		return "Installed AI client bindings and local HA NOVA state"
	}
}

// configuredRelayTokenFile returns the default profile's relay token file
// (absolute) and whether it is HA NOVA's managed one.
func configuredRelayTokenFile(paths runtimePaths) (string, bool) {
	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil {
		return "", false
	}
	path := strings.TrimSpace(cfg.RelayTokenFile)
	if path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(paths.ConfigDir, path)
	}
	_, err = managedServiceTokenFilePath(paths, path)
	return filepath.Clean(path), err == nil
}

// uninstallTokenLineLabel names the relay token store this install actually
// uses: a `setup --service` install keeps it in a file, not the keyring. A
// token file HA NOVA does not manage is never removed, so it gets no line
// (see uninstallForeignTokenFileWarning).
func uninstallTokenLineLabel(paths runtimePaths) string {
	if path, managed := configuredRelayTokenFile(paths); path != "" {
		if !managed {
			return ""
		}
		return "Relay token file (" + path + ")"
	}
	names := uninstallCredentialNames(paths)
	if dir, err := deviceSecretFileDir(); err == nil && deviceFileBackendMarkerExists() { // headless: files, not the keyring
		return "Credential files in " + dir + " (" + strings.Join(names, ", ") + ")"
	}
	store := map[string]string{"darwin": "Keychain", "windows": "Credential Manager"}[runtime.GOOS]
	if store == "" {
		store = "Secure storage"
	}
	if len(names) == 1 {
		return store + " entry (" + names[0] + ")"
	}
	return store + " entries (" + strings.Join(names, ", ") + ")"
}

// uninstallCredentialNames names the entries the user finds in secure
// storage: each paired server profile's device credential, and the shared
// legacy token where the default profile still connects with it.
func uninstallCredentialNames(paths runtimePaths) []string {
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		return []string{deviceCredentialService}
	}
	var names []string
	for _, name := range doc.profileNames() {
		cfg, ok := doc.flatProfile(name)
		if ok && name == defaultServerProfileName && cfg.RelaySecureBaseURL == "" && cfg.RelayBaseURL != "" {
			names = append(names, keyringServiceName)
			continue
		}
		names = append(names, deviceCredentialServiceForProfile(name))
	}
	if len(names) == 0 {
		return []string{deviceCredentialService}
	}
	return names
}

func uninstallForeignTokenFileWarning(paths runtimePaths) string {
	path, managed := configuredRelayTokenFile(paths)
	if path == "" || managed {
		return ""
	}
	return fmt.Sprintf("The relay token file is not HA NOVA's managed token file (%s). A full removal will stop: first remove the `relay_token_file` line from %s.", path, paths.ConfigFile)
}

func uninstallWindowsBundleNote() string {
	return "Windows bundle note: a short-lived helper finishes the uninstall after the running ha-nova.exe exits. Please wait a moment for the final removal to complete."
}

// collectUninstallPreflight reads the raw default profile (not loadConfig:
// partial setups must still yield the HA URL for the notes, and uninstall is
// install-wide, so no runtime selection applies).
func collectUninstallPreflight(paths runtimePaths) uninstallPreflight {
	preflight := uninstallPreflight{}

	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil {
		return preflight
	}
	preflight.config = cfg
	preflight.haURL = strings.TrimSpace(cfg.HAURL)
	if cfg.RelayBaseURL == "" {
		return preflight
	}
	if _, err := readRelayAuthToken(); err != nil && !isMissingRelayAuthTokenError(err) {
		preflight.tokenUnavailable = relayAuthTokenProblemMessage(err)
	}
	return preflight
}

func applyUninstallPreflightNotes(report *uninstallReport, preflight uninstallPreflight) {
	if report == nil {
		return
	}
	for _, note := range preflightNoteLines(preflight) {
		report.addNote(note)
	}
}

func printUninstallPreflightNotes(out io.Writer, preflight uninstallPreflight) {
	session := resolveStatusUISession(out)
	for _, note := range preflightNoteLines(preflight) {
		fmt.Fprintf(out, "  %s %s\n", session.style("warning", session.warningMarker()), note)
	}
}

// preflightNoteLines: uninstall removes the shared App only through the menu
// choice, after the Relay said this is its last computer. Otherwise the App
// stays, and the note says where to remove it and what that costs.
func preflightNoteLines(preflight uninstallPreflight) []string {
	note := "The NOVA App in Home Assistant stays. Remove it there only if no other computer uses HA NOVA: removing it disconnects every paired computer."
	if preflight.haURL != "" {
		note += " Its page: " + haRelayAppPageURL(preflight.haURL)
	}
	notes := []string{note}
	if preflight.tokenUnavailable != "" {
		notes = append(notes, preflight.tokenUnavailable)
	}
	return notes
}
