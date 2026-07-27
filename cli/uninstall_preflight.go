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
	fmt.Fprintf(out, "    %s %s\n", session.bullet(), uninstallTokenLineLabel())
	fmt.Fprintf(out, "    %s Home Assistant Cloud authorization (when configured)\n", session.bullet())
	fmt.Fprintln(out)
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

func uninstallTokenLineLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "Keychain entry (ha-nova.relay-auth-token)"
	case "windows":
		return "Credential Manager token (ha-nova.relay-auth-token)"
	default:
		return "secure storage token (ha-nova.relay-auth-token)"
	}
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
	lead := "Home Assistant side (if still installed, finish these to fully remove HA NOVA):"
	if preflight.relayStillRunning {
		lead = "The NOVA Relay app is still running in Home Assistant. To fully remove HA NOVA:"
	}
	notes := []string{lead}
	if preflight.haURL != "" {
		notes = append(notes,
			"1. Remove the NOVA Relay app: "+haRelayAppPageURL(preflight.haURL),
			"2. Remove the repository: "+haAppStoreURL(preflight.haURL)+" > three-dot menu > Repositories > remove "+haNovaRepositoryURL,
			"3. If this was a legacy/standalone install, revoke its \"NOVA\" access token: "+haProfileSecurityURL(preflight.haURL),
		)
	} else {
		notes = append(notes,
			"1. Remove the NOVA Relay app: Settings > Apps > NOVA Relay > Uninstall (older Home Assistant: Settings > Add-ons)",
			"2. Remove the repository: Settings > Apps > App Store > three-dot menu > Repositories > remove "+haNovaRepositoryURL,
			"3. Revoke the \"NOVA\" access token: Profile > Security > Long-lived access tokens",
		)
	}
	if preflight.tokenUnavailable != "" {
		notes = append(notes, preflight.tokenUnavailable)
	}
	return notes
}
