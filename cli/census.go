package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The opt-in census (docs/reference/census.md, PRIVACY.md): a voluntary,
// ID-free weekly ping — {schema, version, relay?, os} and nothing else. This
// file is the ONLY place allowed to know the endpoint or perform the send;
// scripts/check-docs.sh check [12] fails the build if sendCensusPing or
// censusEndpointURL appear outside cli/census*.go, or if the opt-in guard
// below disappears.

// censusEndpointURL is the single deploy-time constant for the census worker.
// TODO(deploy): substitute real subdomain before release.
const censusEndpointURL = "https://ha-nova-census.PLACEHOLDER.workers.dev"

const censusOptOutEnv = "HA_NOVA_NO_CENSUS"

// censusHTTPClient is dedicated and short-fused: the ping may never make an
// update check feel slow, and it never retries. Overridable for tests.
var censusHTTPClient = &http.Client{Timeout: 3 * time.Second}

// censusPayload is the exact wire shape. Field order is wire order. The
// json tags below are contract-tested against the worker's accepted field
// set (tests/census-worker/worker.test.ts) so the payload cannot grow
// silently.
type censusPayload struct {
	Schema  int    `json:"schema"`
	Version string `json:"version"`
	Relay   string `json:"relay,omitempty"`
	OS      string `json:"os"`
}

const censusRelayFreshness = 7 * 24 * time.Hour

// censusPlatformOS is the raw platform source, overridable for tests.
var censusPlatformOS = bundlePlatformOS

// censusOS allows exactly the three documented buckets. Anything else returns
// "" and the callers skip the send entirely — an uncounted install beats a
// falsely counted one.
func censusOS() string {
	switch v := censusPlatformOS(); v {
	case "macos", "windows", "linux":
		return v
	default:
		return ""
	}
}

func buildCensusPayload(paths runtimePaths, state censusState, now time.Time) censusPayload {
	payload := censusPayload{
		Schema:  1,
		Version: localVersion(paths),
		OS:      censusOS(),
	}
	// Relay version rides along only when observed recently (opportunistic
	// stamp from normal relay traffic — never a relay call for the census).
	if state.RelayVersion != "" && state.RelayVersionObservedAt != "" {
		if observed, err := time.Parse(time.RFC3339, state.RelayVersionObservedAt); err == nil && now.Sub(observed) <= censusRelayFreshness {
			payload.Relay = state.RelayVersion
		}
	}
	return payload
}

// censusWireBytes renders the literal bytes that go on the wire — the same
// bytes `ha-nova census status` shows the user.
func censusWireBytes(payload censusPayload) []byte {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	return data
}

// maybeCensusPing is the carrier hook for every check-update path (including
// --quiet --json and the detached refresh child). It never prints, never
// alters exit codes, never retries, and sends at most once per ISO week.
// Callers invoke it AFTER their own output is complete, so a hanging endpoint
// can never delay what the user (or a hook) is waiting for.
func maybeCensusPing(paths runtimePaths) {
	if censusOptedOutByEnv() {
		return
	}
	state := loadCensusState(paths)
	if !state.Enabled {
		return
	}
	// Dev builds never count: their version is not a released one.
	if BuildChannel == "dev" || localVersion(paths) == "dev" {
		return
	}
	// Platforms outside the documented buckets are not counted at all.
	if censusOS() == "" {
		return
	}
	now := censusNow().UTC()
	currentWeek := censusISOWeek(now)
	if state.LastPingWeek == currentWeek {
		return
	}
	if state.LastPingWeek > currentWeek {
		// Clock rollback: the stamped week is in the future. Never double-count;
		// self-heal by clamping to the current week and staying silent.
		_ = mutateCensusState(paths, func(s *censusState) {
			if s.LastPingWeek > currentWeek {
				s.LastPingWeek = currentWeek
			}
		})
		return
	}
	// Stamp atomically BEFORE the send: at-most-once per week. A failed send
	// loses this week's count — it never doubles it. The mutate touches only
	// the week field, so concurrent writers keep their own fields.
	if err := mutateCensusState(paths, func(s *censusState) { s.LastPingWeek = currentWeek }); err != nil {
		return
	}
	// Second line of defense: session-start hooks can fan out several
	// check-updates at once, all loading the pre-stamp state. Exactly one of
	// them wins the exclusive week marker; the rest skip silently.
	if !claimCensusWeekMarker(paths, currentWeek) {
		return
	}
	_ = sendCensusPing(censusWireBytes(buildCensusPayload(paths, loadCensusState(paths), now)))
}

// claimCensusWeekMarker atomically claims this ISO week's single send across
// concurrent processes via O_CREATE|O_EXCL in the cache dir. Preparation
// failures degrade to "allow": the state-file week gate has already passed,
// and a rare duplicate attempt beats silently never sending. Stale markers
// from previous weeks are pruned so at most one marker file ever exists.
func claimCensusWeekMarker(paths runtimePaths, week string) bool {
	if paths.CacheDir == "" {
		return true
	}
	if err := os.MkdirAll(paths.CacheDir, 0o755); err != nil {
		return true
	}
	if entries, err := os.ReadDir(paths.CacheDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, "census-ping-") && name != "census-ping-"+week {
				_ = os.Remove(filepath.Join(paths.CacheDir, name))
			}
		}
	}
	marker := filepath.Join(paths.CacheDir, "census-ping-"+week)
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return !errors.Is(err, os.ErrExist)
	}
	_ = file.Close()
	return true
}

// sendCensusPing performs the single fire-and-forget POST. Callers decide
// whether the result matters (`census on` reports it; the weekly carrier
// ignores it).
func sendCensusPing(payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("empty census payload")
	}
	req, err := http.NewRequest(http.MethodPost, censusEndpointURL+"/ping", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := censusHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("census endpoint answered HTTP %d", resp.StatusCode)
	}
	return nil
}

func censusStatsURL() string {
	return censusEndpointURL + "/stats"
}

func censusPingURL() string {
	return censusEndpointURL + "/ping"
}
