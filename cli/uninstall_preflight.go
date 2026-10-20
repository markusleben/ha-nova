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

// renderUninstallPreflight previews the removal. Without the menu (--yes, no
// terminal) the mode is already fixed, so the second list says what happens.
func renderUninstallPreflight(out io.Writer, paths runtimePaths, source string, menu bool, mode uninstallMode) {
	session := resolveStatusUISession(out)
	second := "Also removed unless you keep the pairing:"
	switch {
	case !menu && mode == uninstallModePurge:
		second = "Also removed:"
	case !menu:
		second = "Kept for a reinstall:"
	}
	renderSimpleHeader(out, session, "HA NOVA Uninstall")
	fmt.Fprintf(out, "  %s\n", session.style("strong", "Removes from this computer:"))
	fmt.Fprintf(out, "    %s HA NOVA's skills in your AI apps\n", session.bullet())
	fmt.Fprintf(out, "    %s %s\n", session.bullet(), uninstallRuntimeLineLabel(paths, source))
	fmt.Fprintf(out, "    %s HA NOVA's own files on this computer\n", session.bullet())
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", session.style("strong", second))
	fmt.Fprintf(out, "    %s HA NOVA's saved Home Assistant address\n", session.bullet())
	if label := uninstallTokenLineLabel(paths); label != "" {
		fmt.Fprintf(out, "    %s %s\n", session.bullet(), label)
	}
	fmt.Fprintf(out, "    %s Your Home Assistant Link sign-in, if you use it\n", session.bullet())
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
		return "The ha-nova program"
	default:
		return "HA NOVA's connection in your AI apps"
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
		return "HA NOVA's key file"
	}
	keys := "This computer's HA NOVA key"
	if names := uninstallCredentialNames(paths); len(names) > 1 {
		keys = fmt.Sprintf("This computer's %d HA NOVA keys", len(names))
	}
	if deviceFileBackendMarkerExists() { // headless: files, not the keyring
		return keys + " (in files)"
	}
	store := map[string]string{"darwin": "the Keychain", "windows": "Credential Manager"}[runtime.GOOS]
	if store == "" {
		store = "the keyring"
	}
	return keys + " (in " + store + ")"
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
	return "On Windows the last step finishes in the background a few seconds after this. You can close this window; wait a moment before you reinstall."
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
	// A locked keyring stops a full removal first; a kept pairing just stays.
	if _, err := readRelayAuthToken(); err != nil && !isMissingRelayAuthTokenError(err) && !isDesktopKeyringLockedError(err) {
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
// (or a standalone Relay) stays, and the notes say how to remove it by hand.
func preflightNoteLines(preflight uninstallPreflight) []string {
	derived := deriveRelayURLFromHA(preflight.haURL, preflight.config.HAHost)
	var notes []string
	if preflight.haURL != "" && derived != "" &&
		strings.TrimRight(strings.TrimSpace(preflight.config.RelayBaseURL), "/") == derived {
		notes = []string{
			"The NOVA App in Home Assistant stays. Other computers may use it: removing it disconnects every paired computer. To remove it anyway, open: " + haRelayAppPageURL(preflight.haURL),
			"Then remove where it came from: Settings → Apps → App Store → ⋮ → Repositories → " + haNovaRepositoryURL,
		}
	} else {
		notes = []string{"The NOVA Relay stays. If no other computer uses HA NOVA, remove it, and delete any \"NOVA\" token in your Home Assistant profile (Security tab)."}
	}
	if preflight.tokenUnavailable != "" {
		notes = append(notes, preflight.tokenUnavailable)
	}
	return notes
}
