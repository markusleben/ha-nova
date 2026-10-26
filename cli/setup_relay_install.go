package main

import (
	"bufio"
	"context"
	"encoding/json"
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
func detectInstalledRelay(out io.Writer, paths runtimePaths, cfg runtimeConfig, relayURLFlag string) (bool, setupMsg) {
	derived := strings.TrimRight(deriveRelayURLFromHA(cfg.HAURL, cfg.HAHost), "/")
	if strings.TrimSpace(relayURLFlag) != "" || derived == "" ||
		strings.TrimRight(strings.TrimSpace(cfg.RelayBaseURL), "/") != derived {
		return false, setupMsg{}
	}
	result, _ := runSetupCountdownSpinnerWithResult(out,
		"Checking for the NOVA App...",
		installedRelayProbeTimeout,
		setupDiscoveryMinimumVisibleDuration,
		func() (installedRelayResult, error) {
			running, version := probeInstalledRelayForSetup(derived)
			return installedRelayResult{running: running, version: version}, nil
		},
	)
	if !result.running {
		return false, setupMsg{}
	}
	note := setupText("setup.app.running")
	if minimum := relayVersionBelowMinimum(paths, result.version); minimum != "" {
		if haAppInstallOffered(out, cfg) {
			return false, setupMsg{} // the App step offers "Sign in and update NOVA"
		}
		note = outdatedRelayNote(result.version, minimum)
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

func outdatedRelayNote(version, minimum string) setupMsg {
	return setupText("setup.app.running_outdated", "version", version, "minimum", minimum)
}

// runRelayInstallSteps first offers "Sign in and install NOVA" (or "Sign in
// and update NOVA" for an App below min_relay_version) where a desktop
// sign-in is possible and pairing follows (pairNext). Otherwise, or when that
// falls back, it announces the install link, opens it on Enter, lists the
// clicks (Update for an App below min_relay_version), and waits until the
// user says the App runs. note is the
// outdated-App reminder for the pairing step. It returns the prompt's
// errSetupBack/errSetupExit unchanged for the caller's navigation.
func runRelayInstallSteps(reader *bufio.Reader, out io.Writer, paths runtimePaths, cfg runtimeConfig, pairNext bool) (note setupMsg, err error) {
	// After the page's "Use a six-digit code" the sign-in isn't offered again,
	// but an App below min_relay_version still gets the update steps below.
	update := false
	if pairNext && (setupPairWithCode || haAppInstallOffered(out, cfg)) {
		running, version := probeInstalledRelayForSetup(cfg.RelayBaseURL)
		minimum := ""
		if running {
			minimum = relayVersionBelowMinimum(paths, version)
		}
		update = minimum != ""
		if !setupPairWithCode && (!running || update) { // a running, current App needs nothing
			result, err := runSignInAppInstall(reader, out, cfg, minimum != "", version, minimum)
			if err != nil || result == appInstallDone {
				return setupMsg{}, err
			}
			if result == appInstallContinue {
				if minimum != "" {
					note = outdatedRelayNote(version, minimum)
				}
				return note, nil
			}
		}
	}
	link := haInstallRelayAppURL(cfg.HAURL)
	renderSetupParagraph(out, "Your browser opens the NOVA App in Home Assistant (listed as “NOVA Relay”).")
	renderSetupLink(out, "This will open:", link)
	if _, err := promptWizardLineFromReader(reader, out, "Press Enter to open your browser", ""); err != nil {
		return setupMsg{}, err
	}
	openAnnouncedBrowserURL(out, link)
	if update {
		renderSetupParagraphTight(out, "In the browser: sign in if asked, then click Update on NOVA Relay.")
		_, err = promptWizardLineFromReader(reader, out, "Press Enter when the App is updated", "")
		return setupMsg{}, err
	}
	renderSetupParagraphTight(out, `In the browser: sign in and click "Add" if asked, then Install, then Start.`)
	_, err = promptWizardLineFromReader(reader, out, "Press Enter when the App is running", "")
	return setupMsg{}, err
}
