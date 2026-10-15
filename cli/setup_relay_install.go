package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Bounds the "is the Relay already running?" probe so a silent firewall never
// stalls the wizard longer than this.
var installedRelayProbeTimeout = 5 * time.Second

// Test seam.
var probeInstalledRelayForSetup = probeInstalledRelay

// probeInstalledRelay asks the unauthenticated GET /pair/v1/info. Only HTTP 200
// with the relay's own {"ok":true,"data":{"protocol_version":"v1"}} envelope
// counts as running; a closed port, timeout, HA or proxy page, other status,
// or an old relay all read as "not running". It also returns relay_version.
func probeInstalledRelay(relayURL string) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), installedRelayProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(relayURL, "/")+"/pair/v1/info", nil)
	if err != nil {
		return false, ""
	}
	req.URL.User = nil // never forward copied URL userinfo as Basic auth
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, ""
	}
	body, err := readAllLimited(resp.Body, maxRelayPairingResponseBytes)
	if err != nil || !decodePairInfo(body) {
		return false, ""
	}
	var info struct {
		Data struct {
			RelayVersion string `json:"relay_version"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &info) // decodePairInfo already proved valid JSON
	return true, strings.TrimSpace(info.Data.RelayVersion)
}

type installedRelayResult struct {
	running bool
	version string
}

// detectInstalledRelay reports whether the NOVA Relay already runs on the
// chosen Home Assistant, so a second computer can skip the App install, and
// returns the note to show under the pairing step. Only the relay address
// derived from that Home Assistant proves it: a --relay-url or a kept custom
// address may point at another machine, so those always show the steps.
func detectInstalledRelay(out io.Writer, paths runtimePaths, cfg runtimeConfig, relayURLFlag string) (bool, string) {
	derived := strings.TrimRight(deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost), "/")
	if strings.TrimSpace(relayURLFlag) != "" || derived == "" ||
		strings.TrimRight(strings.TrimSpace(cfg.RelayBaseURL), "/") != derived {
		return false, ""
	}
	result, _ := runSetupCountdownSpinnerWithResult(out,
		"Checking whether NOVA Relay is already running...",
		installedRelayProbeTimeout,
		setupDiscoveryMinimumVisibleDuration,
		func() (installedRelayResult, error) {
			running, version := probeInstalledRelayForSetup(derived)
			return installedRelayResult{running: running, version: version}, nil
		},
	)
	if !result.running {
		return false, ""
	}
	note := "NOVA Relay is already running in Home Assistant. Skipping the install step."
	if minimum := relayVersionBelowMinimum(paths, result.version); minimum != "" {
		note += fmt.Sprintf(" It runs v%s; this HA NOVA needs v%s or newer: update NOVA Relay in Home Assistant (Settings > Apps > NOVA Relay) after pairing.", result.version, minimum)
	}
	return true, note
}

// relayVersionBelowMinimum returns min_relay_version when the running relay is
// older, else "". An unknown version is no evidence either way.
func relayVersionBelowMinimum(paths runtimePaths, relayVersion string) string {
	if relayVersion == "" {
		return ""
	}
	v, err := readVersionJSON(paths.VersionFile)
	if err != nil || v.MinRelayVersion == "" {
		return ""
	}
	if cmp, err := compareReleaseVersions(relayVersion, v.MinRelayVersion); err != nil || cmp >= 0 {
		return ""
	}
	return v.MinRelayVersion
}

// runRelayInstallSteps announces the install link, opens it on Enter, lists
// the clicks, and waits until the user says the App runs. It returns the
// prompt's errSetupBack/errSetupExit unchanged for the caller's navigation.
func runRelayInstallSteps(reader *bufio.Reader, out io.Writer, haURL string) error {
	link := haInstallRelayAppURL(haURL)
	renderSetupParagraph(out, "Next, install the NOVA Relay App in your Home Assistant.")
	renderSetupLink(out, "This will open:", link)
	if _, err := promptWizardLineFromReader(reader, out, "Press Enter to open your browser", ""); err != nil {
		return err
	}
	openAnnouncedBrowserURL(out, link)
	renderSetupParagraphTight(out, `In the browser: log in to Home Assistant if asked. When it asks to add the app repository, click "Add".`)
	renderSetupIndentedBlock(out, "On the NOVA Relay page:", "    ",
		"1. Click Install and wait for it to finish",
		"2. Click Start",
	)
	_, err := promptWizardLineFromReader(reader, out, "Press Enter when the app is running", "")
	return err
}
