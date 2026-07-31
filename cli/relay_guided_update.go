package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// After the relay-outdated warning on an interactive terminal, `ha-nova
// update` and `ha-nova doctor` can show the currently observed App-update
// preview and ask for confirmation to install the latest available version —
// never automatic.
// Supervisor App updates cannot bind a target version. Everything here is
// client-side and generic HA transport through the existing /core proxy; the
// relay stays dumb.

const (
	relayUpdateUniqueID        = haNovaRelayAppSlug + "_version_latest"
	updateFeatureInstall int64 = 1
	updateFeatureBackup  int64 = 8
)

// Poll pacing is a variable so tests can shrink the restart wait.
var (
	relayUpdatePollInterval = 5 * time.Second
	relayUpdatePollTimeout  = 3 * time.Minute
	relayUpdateInputIsTTY   = isInteractiveTTY
	relayUpdateOutputIsTTY  = stdoutIsInteractiveTTY
	relayUpdateInput        = func() *bufio.Reader { return bufio.NewReader(os.Stdin) }
	relayUpdateOutput       = func() io.Writer { return os.Stdout }
)

const relayUpdateManualPath = "Manual path: open Home Assistant > Settings > Apps > NOVA Relay (older HA calls it Add-ons) and install the update there; a standalone container needs a manual image pull."

// maybeOfferGuidedRelayUpdate follows a printed relay update notice and
// reports whether the relay ended up updated AND verified — doctor uses that
// to not fail a run whose below-floor problem the user just fixed. It is
// deliberately best-effort: a non-TTY session, a "no", a missing update
// entity (standalone container), or any transport problem falls back to the
// manual path without touching the caller's exit code.
func maybeOfferGuidedRelayUpdate(paths runtimePaths, notice humanNotice) bool {
	// Both ends must be a terminal: with stdout redirected the question would
	// land in a file while the command silently blocks on stdin.
	if (notice.kind != humanNoticeKindRelayOutdated && notice.kind != humanNoticeKindRelayUpdateAvailable) ||
		!relayUpdateInputIsTTY() || !relayUpdateOutputIsTTY() {
		return false
	}
	cfg, err := loadConfig(paths)
	if err != nil || cfg.RelayBaseURL == "" {
		return false
	}
	// Paired devices use their device credential; legacy installs their token.
	_, _, credential, deviceMode, err := relayFunctionalTransportForDoctor(cfg)
	if err != nil || credential == "" {
		if deviceMode {
			return false
		}
		// A paired config must not downgrade to the legacy token over the plain,
		// unpinned port when the device credential is missing — respect the
		// fail-closed contract and skip the guided update.
		if cfg.RelaySecureBaseURL != "" && cfg.RelaySpkiPin != "" {
			return false
		}
		token, tokenErr := readRelayAuthTokenForDoctor()
		if tokenErr != nil || token == "" {
			return false
		}
		credential = token
	}
	return runGuidedRelayUpdate(paths, cfg, credential, relayUpdateInput(), relayUpdateOutput())
}

func runGuidedRelayUpdate(paths runtimePaths, cfg config, token string, in *bufio.Reader, out io.Writer) bool {
	candidate, reason := resolveRelayUpdateCandidate(cfg, token)
	if candidate.EntityID == "" {
		fmt.Fprintf(out, "Cannot start the App update from here: %s\n%s\n", reason, relayUpdateManualPath)
		return false
	}
	if !candidate.updateAvailable() {
		fmt.Fprintf(out, "Cannot start the App update from here: the exact NOVA Relay update entity has no newer pending version.\n%s\n", relayUpdateManualPath)
		return false
	}
	if !candidate.guidedInstallReady() {
		fmt.Fprintf(out, "Cannot start the guided App update: the exact entity does not support both install and partial backup. Nothing was installed.\n%s\n", relayUpdateManualPath)
		return false
	}
	fmt.Fprintf(
		out,
		"NOVA Relay App update preview: v%s → v%s (%s).\nPlanned change: install the latest version available at execution time with a partial App backup; the relay restarts during the install. Home Assistant does not support binding App installs to a specific version.\n",
		candidate.InstalledVersion,
		candidate.LatestVersion,
		candidate.EntityID,
	)
	yes, err := promptWizardYesNoFromReader(in, out, "Install the latest available NOVA Relay update now?", true)
	if err != nil || !yes {
		return false
	}
	current, reason := resolveRelayUpdateCandidate(cfg, token)
	if !current.updateAvailable() ||
		!current.guidedInstallReady() ||
		!current.samePreview(candidate) {
		if reason == "" {
			reason = "the pending update changed after the preview"
		}
		fmt.Fprintf(out, "The observed NOVA Relay App update changed after the preview: %s. Nothing was installed; run the check again.\n", reason)
		return false
	}
	candidate = current
	fmt.Fprintf(out, "Installing the NOVA Relay App update (%s) with a partial backup — the relay restarts during the install.\n", candidate.EntityID)
	// The relay dies mid-call when the install lands, so a dropped response
	// is the EXPECTED shape of success here; polling decides the outcome.
	// An ok:false envelope is a relay-side transport problem (the relay's
	// upstream client times out at 10s — a backup easily exceeds that) and
	// polls too. Only Home Assistant itself answering >= 400 (ok:true) is an
	// explicit rejection that skips the three-minute wait.
	installPayload := map[string]interface{}{
		"entity_id": candidate.EntityID,
		"backup":    true,
	}
	installBody, marshalErr := json.Marshal(installPayload)
	if marshalErr != nil {
		fmt.Fprintln(out, "Could not build the confirmed install request. Nothing was installed.")
		return false
	}
	body, err := relayCoreRequest(cfg, token, "POST", "/api/services/update/install", installBody)
	if err == nil {
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				Status int `json:"status"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &envelope) == nil && envelope.OK && envelope.Data.Status >= 400 {
			fmt.Fprintf(out, "Home Assistant rejected the install call.\n%s\n", relayUpdateManualPath)
			return false
		}
	}
	version, ok := waitForRelayVersion(paths, cfg, token, candidate.LatestVersion)
	if !ok {
		fmt.Fprintf(out, "The relay did not report a new version within %s.\n%s\n", relayUpdatePollTimeout, relayUpdateManualPath)
		return false
	}
	if !waitForRelayUpdateCompletion(cfg, token, candidate) {
		fmt.Fprintf(out, "The Relay reached v%s, but Home Assistant did not confirm the App update as complete within %s.\n%s\n", version, relayUpdatePollTimeout, relayUpdateManualPath)
		return false
	}
	readiness, readinessVersion := verifyUpdatedRelayReadiness(paths, cfg, token, candidate.LatestVersion)
	if !readiness {
		fmt.Fprintf(out, "The Relay reached v%s, but its Home Assistant WebSocket readiness could not be verified.\n%s\n", version, relayUpdateManualPath)
		return false
	}
	version = readinessVersion
	fmt.Fprintf(out, "Relay updated and verified: v%s is running.\n", version)
	return true
}

type relayUpdateCandidate struct {
	EntityID          string
	Platform          string
	UniqueID          string
	State             string
	InstalledVersion  string
	LatestVersion     string
	SupportedFeatures int64
	InProgress        bool
}

func (candidate relayUpdateCandidate) updateAvailable() bool {
	if candidate.State != "on" || candidate.InProgress || candidate.InstalledVersion == "" || candidate.LatestVersion == "" {
		return false
	}
	cmp, err := compareReleaseVersions(candidate.InstalledVersion, candidate.LatestVersion)
	return err == nil && cmp < 0
}

func (candidate relayUpdateCandidate) guidedInstallReady() bool {
	required := updateFeatureInstall | updateFeatureBackup
	return candidate.SupportedFeatures&required == required
}

func (candidate relayUpdateCandidate) samePreview(other relayUpdateCandidate) bool {
	return candidate.EntityID == other.EntityID &&
		candidate.Platform == other.Platform &&
		candidate.UniqueID == other.UniqueID &&
		candidate.State == other.State &&
		candidate.InstalledVersion == other.InstalledVersion &&
		candidate.LatestVersion == other.LatestVersion &&
		candidate.SupportedFeatures == other.SupportedFeatures &&
		candidate.InProgress == other.InProgress
}

func (candidate relayUpdateCandidate) completedFrom(before relayUpdateCandidate) bool {
	if candidate.EntityID != before.EntityID ||
		candidate.Platform != before.Platform ||
		candidate.UniqueID != before.UniqueID ||
		candidate.State != "off" ||
		candidate.InProgress {
		return false
	}
	cmp, err := compareReleaseVersions(candidate.InstalledVersion, before.LatestVersion)
	return err == nil && cmp >= 0
}

func relayRegistryEntryMatchesNOVA(uniqueID string) bool {
	return uniqueID == relayUpdateUniqueID
}

// resolveRelayUpdateCandidate joins state with immutable entity-registry
// provenance. States alone cannot distinguish the Supervisor App entity from a
// custom/MQTT update entity carrying the same title.
func resolveRelayUpdateCandidate(cfg config, token string) (relayUpdateCandidate, string) {
	base, client, credential, endpointErr := functionalEndpoint(cfg, token)
	if endpointErr != nil {
		return relayUpdateCandidate{}, fmt.Sprintf("could not resolve Relay transport (%s)", endpointErr)
	}
	return resolveRelayUpdateCandidateWithTransport(context.Background(), base, client, credential)
}

func resolveRelayUpdateCandidateWithTransport(
	ctx context.Context,
	base string,
	client *http.Client,
	credential string,
) (relayUpdateCandidate, string) {
	body, err := relayCoreRequestWithTransport(ctx, base, client, credential, "GET", "/api/states", nil)
	if err != nil {
		return relayUpdateCandidate{}, fmt.Sprintf("could not read Home Assistant states (%s)", err)
	}
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Status int             `json:"status"`
			Body   json.RawMessage `json:"body"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || !envelope.OK || envelope.Data.Status != http.StatusOK {
		return relayUpdateCandidate{}, "could not read Home Assistant states"
	}
	var states []struct {
		EntityID   string `json:"entity_id"`
		State      string `json:"state"`
		Attributes struct {
			InstalledVersion  string `json:"installed_version"`
			LatestVersion     string `json:"latest_version"`
			SupportedFeatures int64  `json:"supported_features"`
			InProgress        bool   `json:"in_progress"`
		} `json:"attributes"`
	}
	if err := json.Unmarshal(envelope.Data.Body, &states); err != nil {
		return relayUpdateCandidate{}, "could not read Home Assistant states"
	}
	stateByEntityID := make(map[string]relayUpdateCandidate, len(states))
	for _, s := range states {
		if strings.HasPrefix(s.EntityID, "update.") {
			stateByEntityID[s.EntityID] = relayUpdateCandidate{
				EntityID:          s.EntityID,
				State:             s.State,
				InstalledVersion:  s.Attributes.InstalledVersion,
				LatestVersion:     s.Attributes.LatestVersion,
				SupportedFeatures: s.Attributes.SupportedFeatures,
				InProgress:        s.Attributes.InProgress,
			}
		}
	}

	registryBody, err := relayWSRequestWithTransport(
		ctx,
		base,
		client,
		credential,
		[]byte(`{"type":"config/entity_registry/list"}`),
	)
	if err != nil {
		return relayUpdateCandidate{}, fmt.Sprintf("could not read Home Assistant entity registry (%s)", err)
	}
	var registryEnvelope struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(registryBody, &registryEnvelope); err != nil || !registryEnvelope.OK {
		return relayUpdateCandidate{}, "could not read Home Assistant entity registry"
	}
	var registryEntries []struct {
		EntityID string `json:"entity_id"`
		Platform string `json:"platform"`
		UniqueID string `json:"unique_id"`
	}
	if err := json.Unmarshal(registryEnvelope.Data, &registryEntries); err != nil {
		return relayUpdateCandidate{}, "could not read Home Assistant entity registry"
	}
	var matches []relayUpdateCandidate
	for _, entry := range registryEntries {
		if entry.Platform != "hassio" || !strings.HasPrefix(entry.EntityID, "update.") ||
			!relayRegistryEntryMatchesNOVA(entry.UniqueID) {
			continue
		}
		if candidate, ok := stateByEntityID[entry.EntityID]; ok {
			candidate.Platform = entry.Platform
			candidate.UniqueID = entry.UniqueID
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], ""
	case 0:
		return relayUpdateCandidate{}, "no registry-proven NOVA Relay App update entity found (standalone container, or the App is not installed)"
	default:
		return relayUpdateCandidate{}, "several registry-proven NOVA Relay App update entities were found — not guessing"
	}
}

// waitForRelayVersion polls GET /health until the reported version reaches the
// update entity's offered target. Older update entities without version
// metadata retain the floor-based verification used before above-floor update
// offers existed. Connection errors are the normal restart window.
func waitForRelayVersion(paths runtimePaths, cfg config, token, targetVersion string) (string, bool) {
	deadline := time.Now().Add(relayUpdatePollTimeout)
	base, client, credential, endpointErr := functionalEndpoint(cfg, token)
	if endpointErr != nil {
		return "", false
	}
	for {
		body, err := fetchRelayHealthWith(client, base, credential)
		if err == nil {
			version := parseRelayHealthVersion(body)
			if relayVersionMeetsUpdateGate(paths, version, targetVersion) {
				return version, true
			}
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(relayUpdatePollInterval)
	}
}

func relayVersionMeetsUpdateGate(paths runtimePaths, version, targetVersion string) bool {
	if version == "" || !checkRelayVersionValue(paths, version).empty() {
		return false
	}
	if targetVersion == "" {
		return true
	}
	cmp, err := compareReleaseVersions(version, targetVersion)
	return err == nil && cmp >= 0
}

func waitForRelayUpdateCompletion(cfg config, token string, before relayUpdateCandidate) bool {
	deadline := time.Now().Add(relayUpdatePollTimeout)
	for {
		current, _ := resolveRelayUpdateCandidate(cfg, token)
		if current.completedFrom(before) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(relayUpdatePollInterval)
	}
}

func verifyUpdatedRelayReadiness(paths runtimePaths, cfg config, token, targetVersion string) (bool, string) {
	base, client, credential, err := functionalEndpoint(cfg, token)
	if err != nil {
		return false, ""
	}
	readiness := checkRelayReadinessWithProbes(
		base,
		credential,
		func(u, t string) ([]byte, error) { return fetchRelayHealthWith(client, u, t) },
		func(u, t string) (relayWSPingResponse, error) { return probeRelayWSPingWith(client, u, t) },
		true,
	)
	version := parseRelayHealthVersion(readiness.HealthBody)
	return readiness.WSReady && relayVersionMeetsUpdateGate(paths, version, targetVersion), version
}
