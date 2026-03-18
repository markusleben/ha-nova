package main

import (
	"fmt"
	"io"
	"runtime"
)

type uninstallPreflight struct {
	relayStillRunning bool
	tokenUnavailable  string
}

func renderUninstallPreflight(out io.Writer) {
	session := resolveStatusUISession(out)
	renderSimpleHeader(out, session, "HA NOVA Uninstall")
	fmt.Fprintf(out, "  %s\n", session.style("strong", "This will remove:"))
	fmt.Fprintf(out, "    %s Skills from installed AI clients\n", session.bullet())
	fmt.Fprintf(out, "    %s Local install (~/.local/share/ha-nova)\n", session.bullet())
	fmt.Fprintf(out, "    %s %s\n", session.bullet(), uninstallCLILineLabel())
	fmt.Fprintf(out, "    %s Managed config files (~/.config/ha-nova/)\n", session.bullet())
	fmt.Fprintf(out, "    %s Managed cache files (~/.cache/ha-nova/)\n", session.bullet())
	fmt.Fprintf(out, "    %s %s\n", session.bullet(), uninstallTokenLineLabel())
	fmt.Fprintln(out)
	renderSetupParagraphTight(out, session.style("muted", "Windows note: a short-lived helper finishes the uninstall after the running ha-nova.exe exits."))
	fmt.Fprintln(out)
}

func uninstallCLILineLabel() string {
	return uninstallCLILineLabelForOS(runtime.GOOS)
}

func uninstallCLILineLabelForOS(goos string) string {
	if goos == "windows" {
		return "Installed CLI binary (~/.local/share/ha-nova/ha-nova.exe)"
	}
	return "CLI link (~/.local/bin/ha-nova)"
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

func collectUninstallPreflight(paths runtimePaths) uninstallPreflight {
	preflight := uninstallPreflight{}

	cfg, err := loadConfig(paths)
	if err != nil || cfg.RelayBaseURL == "" {
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

	if _, err := fetchRelayHealth(cfg.RelayBaseURL, token); err == nil {
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

func preflightNoteLines(preflight uninstallPreflight) []string {
	if !preflight.relayStillRunning {
		if preflight.tokenUnavailable == "" {
			return nil
		}
		return []string{preflight.tokenUnavailable}
	}
	notes := []string{
		"Note: The NOVA Relay app is still running in Home Assistant.",
		"To remove it: Settings > Apps > NOVA Relay > Uninstall",
	}
	if preflight.tokenUnavailable != "" {
		notes = append(notes, preflight.tokenUnavailable)
	}
	return notes
}
