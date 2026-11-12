package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
// client-side and generic HA transport through the existing /core proxy, at
// home or over Home Assistant Link (its Ingress carries /core too); the relay
// stays dumb.

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

const relayUpdateManualPath = "To update it yourself: " + relayAppUpdatePath + " " + relayContainerUpdateLine

// relayUpdateCheckManualPath: Home Assistant shows no newer App, even after
// HA NOVA asked it to check, or HA NOVA didn't ask because something in the
// home reacts to that check.
const relayUpdateCheckManualPath = "To update it yourself: In Home Assistant: Settings → ⋮ (top right) → Check for updates, then Settings → Apps → NOVA Relay → Update."

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
	if err != nil || cfg.RelayBaseURL == "" && !remoteOnlyCloudSetup(cfg) {
		return false
	}
	// The notice's route: home or Home Assistant Link with the device
	// credential, or a legacy install's token. A paired or Link install never
	// writes with a legacy token, whatever its route's problem.
	selected, ok := relayNoticeTransport(context.Background(), cfg)
	if !ok || !selected.DeviceMode && (cfg.RelaySecureBaseURL != "" || cfg.Cloud.configured()) {
		return false
	}
	return runGuidedRelayUpdate(paths, &relayUpdateRoute{selected, cfg}, relayUpdateInput(), relayUpdateOutput())
}

func runGuidedRelayUpdate(paths runtimePaths, route *relayUpdateRoute, in *bufio.Reader, out io.Writer) bool {
	candidate, _ := route.candidate()
	if candidate.EntityID == "" {
		fmt.Fprintf(out, "HA NOVA can't start the NOVA App update from here. Nothing was installed.\n%s\n", relayUpdateManualPath)
		return false
	}
	candidate, checked := checkHomeAssistantForRelayUpdate(paths, route, candidate, out)
	if !candidate.updateAvailable() {
		manualPath := relayUpdateManualPath
		if checked {
			manualPath = relayUpdateCheckManualPath
		}
		fmt.Fprintf(out, "Home Assistant has no NOVA App update ready to install right now. Nothing was installed.\n%s\n", manualPath)
		return false
	}
	if !candidate.guidedInstallReady() {
		fmt.Fprintf(out, "HA NOVA updates the NOVA App only with a backup first, and this App can't make one. Nothing was installed.\n%s\n", relayUpdateManualPath)
		return false
	}
	install := planCallRequest{Service: "update.install", EntityIDs: []string{candidate.EntityID}, Data: map[string]any{"backup": true}, Skill: "ha-nova:updates"}
	rating, _ := runSetupSpinnerWithResult(out, "Checking what the update affects in your home...", setupSpinnerDebounce, func() (guidedRating, error) {
		return rateGuidedCall(paths, route, install), nil
	})
	if !rating.typed() && !rating.allows(tierPreview) {
		fmt.Fprintf(out, "HA NOVA couldn't check everything the update affects in your home, so it installs nothing (%s).\n%s\n", rating.why(), relayUpdateManualPath)
		return false
	}
	fmt.Fprintf(
		out,
		"HA NOVA installs the newest NOVA App (now v%s), with a backup of the App first. The App restarts while it installs.\n",
		candidate.LatestVersion,
	)
	if rating.typed() {
		return runTypedGuidedRelayUpdate(paths, route, candidate, in, out)
	}
	yes, err := promptYesNoMenu(in, out, "Update the NOVA App now?", "Yes, update now", "Not now", true)
	if err != nil || !yes {
		return false
	}
	current, _ := route.candidate()
	if !current.updateAvailable() ||
		!current.guidedInstallReady() ||
		!current.samePreview(candidate) {
		fmt.Fprintln(out, "The update changed, or HA NOVA couldn't check it again. Nothing was installed. Run the check again.")
		return false
	}
	candidate = current
	if !relayGuidedCallAllowed(paths, route, install, tierPreview) { // checked again after the yes: the home may have changed meanwhile
		fmt.Fprintln(out, guidedHomeChanged)
		return false
	}
	fmt.Fprintln(out, "Updating the NOVA App, with a backup first. The App restarts while it installs.")
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
	// Not a raw route (relay_raw_guard.go): no model input reaches it. The
	// service, entity (registry-proven NOVA Relay App), and payload are fixed,
	// it runs only on an interactive terminal after the person's yes, and only
	// when the tier engine finds it a preview at most (above), so this path
	// skips no tier. A typed install goes through apply instead
	// (relay_guided_update_typed.go).
	installBody, marshalErr := json.Marshal(installPayload)
	if marshalErr != nil {
		fmt.Fprintln(out, "HA NOVA couldn't start the update. Nothing was installed.")
		return false
	}
	body, err := route.core("POST", "/api/services/update/install", installBody)
	if err == nil {
		var envelope struct {
			OK   bool `json:"ok"`
			Data struct {
				Status int `json:"status"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &envelope) == nil && envelope.OK && envelope.Data.Status >= 400 {
			fmt.Fprintf(out, "Home Assistant didn't start the update.\n%s\n", relayUpdateManualPath)
			return false
		}
	}
	return finishGuidedRelayUpdate(paths, route, candidate, out)
}

// chatGPTRefreshLine follows an App update while chat apps are on
// (docs/reference/chat-apps.md).
const chatGPTRefreshLine = "If you use HA NOVA in ChatGPT: open chatgpt.com/plugins, choose HA NOVA, then Refresh."

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

// resolveRelayUpdateCandidateWithTransport joins state with immutable
// entity-registry provenance. States alone cannot distinguish the Supervisor
// App entity from a custom/MQTT update entity carrying the same title.
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
		EntityID string     `json:"entity_id"`
		Platform string     `json:"platform"`
		UniqueID registryID `json:"unique_id"`
	}
	if err := json.Unmarshal(registryEnvelope.Data, &registryEntries); err != nil {
		return relayUpdateCandidate{}, "could not read Home Assistant entity registry"
	}
	var matches []relayUpdateCandidate
	for _, entry := range registryEntries {
		if entry.Platform != "hassio" || !strings.HasPrefix(entry.EntityID, "update.") ||
			!relayRegistryEntryMatchesNOVA(string(entry.UniqueID)) {
			continue
		}
		if candidate, ok := stateByEntityID[entry.EntityID]; ok {
			candidate.Platform = entry.Platform
			candidate.UniqueID = string(entry.UniqueID)
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
// offers existed. Connection errors (over Link: Ingress without the App) are
// the normal restart window; a lost Link Ingress session gets a new route.
// An App that no longer takes this computer's old shared access ends the wait
// (errSharedAccessEnded).
func waitForRelayVersion(paths runtimePaths, route *relayUpdateRoute, targetVersion string) (string, error) {
	deadline := time.Now().Add(relayUpdatePollTimeout)
	for {
		body, err := fetchRelayHealthWith(route.Client, route.BaseURL, route.Credential)
		if err == nil {
			version := parseRelayHealthVersion(body)
			if relayVersionMeetsUpdateGate(paths, version, targetVersion) {
				return version, nil
			}
		}
		if errors.Is(err, errSharedAccessEnded) {
			return "", err
		}
		route.reopenAfter(err)
		if time.Now().After(deadline) {
			return "", errors.New("the NOVA App didn't show the new version in time")
		}
		time.Sleep(relayUpdatePollInterval)
	}
}

func relayVersionMeetsUpdateGate(paths runtimePaths, version, targetVersion string) bool {
	if version == "" || !checkRelayVersionValue(paths, version, activeServerIsDemo()).empty() {
		return false
	}
	if targetVersion == "" {
		return true
	}
	cmp, err := compareReleaseVersions(version, targetVersion)
	return err == nil && cmp >= 0
}

func waitForRelayUpdateCompletion(route *relayUpdateRoute, before relayUpdateCandidate) bool {
	deadline := time.Now().Add(relayUpdatePollTimeout)
	for {
		current, _ := route.candidate()
		if current.completedFrom(before) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(relayUpdatePollInterval)
	}
}

func verifyUpdatedRelayReadiness(paths runtimePaths, route *relayUpdateRoute, targetVersion string) (bool, string) {
	readiness := checkRelayReadinessWithProbes(
		route.BaseURL,
		route.Credential,
		func(u, t string) ([]byte, error) { return fetchRelayHealthWith(route.Client, u, t) },
		func(u, t string) (relayWSPingResponse, error) { return probeRelayWSPingWith(route.Client, u, t) },
		true,
	)
	version := parseRelayHealthVersion(readiness.HealthBody)
	return readiness.WSReady && relayVersionMeetsUpdateGate(paths, version, targetVersion), version
}
