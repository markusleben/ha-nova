package main

import (
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

type uninstallPreflight struct {
	relayStillRunning bool
	tokenUnavailable  string
	haURL             string
	relayToken        string
	config            runtimeConfig
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
	fmt.Fprintf(out, "  %s\n", session.style("strong", "Standard remove:"))
	fmt.Fprintf(out, "    %s Skills from installed AI clients\n", session.bullet())
	fmt.Fprintf(out, "    %s %s\n", session.bullet(), uninstallRuntimeLineLabel(paths, source))
	fmt.Fprintf(out, "    %s Managed local state and cache\n", session.bullet())
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", session.style("strong", "Full purge also removes:"))
	fmt.Fprintf(out, "    %s Home Assistant connection config\n", session.bullet())
	if label := uninstallTokenLineLabel(paths); label != "" {
		fmt.Fprintf(out, "    %s %s\n", session.bullet(), label)
	}
	fmt.Fprintf(out, "    %s Home Assistant Cloud authorization (when configured)\n", session.bullet())
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
	switch runtime.GOOS {
	case "darwin":
		return "Keychain entry (ha-nova.relay-auth-token)"
	case "windows":
		return "Credential Manager token (ha-nova.relay-auth-token)"
	default:
		return "secure storage token (ha-nova.relay-auth-token)"
	}
}

func uninstallForeignTokenFileWarning(paths runtimePaths) string {
	path, managed := configuredRelayTokenFile(paths)
	if path == "" || managed {
		return ""
	}
	return fmt.Sprintf("The relay token file is not HA NOVA's managed token file (%s). Full purge will stop: first remove the `relay_token_file` line from %s.", path, paths.ConfigFile)
}

func uninstallWindowsBundleNote() string {
	return "Windows bundle note: a short-lived helper finishes the uninstall after the running ha-nova.exe exits. Please wait a moment for the final removal to complete."
}

// collectUninstallPreflight reads the raw default profile (not loadConfig:
// partial setups must still yield the HA URL for the server-side checklist,
// and uninstall is install-wide, so no runtime selection applies) and probes
// whether the relay still answers.
func collectUninstallPreflight(paths runtimePaths) uninstallPreflight {
	preflight := uninstallPreflight{}

	cfg, err := loadRawDefaultProfileConfig(paths.ConfigFile)
	if err != nil {
		return preflight
	}
	preflight.config = cfg
	preflight.haURL = strings.TrimSpace(cfg.HAURL)
	if strings.TrimSpace(cfg.RelaySecureBaseURL) != "" &&
		strings.TrimSpace(cfg.RelaySpkiPin) != "" &&
		defaultUninstallDeviceCredentialExists() &&
		verifyDefaultUninstallDeviceHealth(cfg) {
		preflight.relayStillRunning = true
	}
	if cfg.RelayBaseURL == "" {
		return preflight
	}

	token, err := readRelayAuthToken()
	if err != nil {
		if !isMissingRelayAuthTokenError(err) {
			preflight.tokenUnavailable = relayAuthTokenProblemMessage(err)
		}
		return preflight
	}
	if token == "" {
		return preflight
	}
	preflight.relayToken = token

	if !preflight.relayStillRunning {
		_, err = fetchRelayHealth(cfg.RelayBaseURL, token)
	}
	if err == nil {
		preflight.relayStillRunning = true
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

// preflightNoteLines always returns the full server-side cleanup checklist.
// The CLI cannot remove the app, the repository, or the LLAT itself, and a
// currently unreachable relay is no evidence the server side is gone — the
// old reachability-gated single note left users with a running app and a
// valid token they were never told about.
func preflightNoteLines(preflight uninstallPreflight) []string {
	lead := "Server side (if still installed, finish these to fully remove HA NOVA):"
	if preflight.relayStillRunning {
		lead = "The NOVA Relay is still running. To fully remove HA NOVA:"
	}
	notes := []string{lead}
	if preflight.haURL != "" {
		notes = append(notes,
			"1. Remove the NOVA Relay: uninstall the app at "+haRelayAppPageURL(preflight.haURL)+", or stop and remove the container of a standalone relay",
			"2. App installs: remove the repository: "+haAppStoreURL(preflight.haURL)+" > three-dot menu > Repositories > remove "+haNovaRepositoryURL,
			"3. Standalone or legacy installs: revoke the \"NOVA\" access token: "+haProfileSecurityURL(preflight.haURL),
		)
	} else {
		notes = append(notes,
			"1. Remove the NOVA Relay: Settings > Apps > NOVA Relay > Uninstall (older Home Assistant: Settings > Add-ons), or stop and remove the container of a standalone relay",
			"2. App installs: remove the repository: Settings > Apps > App Store > three-dot menu > Repositories > remove "+haNovaRepositoryURL,
			"3. Standalone or legacy installs: revoke the \"NOVA\" access token: Profile > Security > Long-lived access tokens",
		)
	}
	if preflight.tokenUnavailable != "" {
		notes = append(notes, preflight.tokenUnavailable)
	}
	return notes
}
