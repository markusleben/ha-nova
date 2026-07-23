package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// The opt-in census (docs/reference/census.md, PRIVACY.md): a voluntary,
// ID-free weekly ping — {schema, version, relay?, os} and nothing else. This
// file is the ONLY place allowed to know the endpoint or perform the send;
// scripts/check-docs.sh check [12] fails the build if sendCensusPing or
// censusEndpointURL appear outside cli/census*.go, or if the opt-in guard
// below disappears.

// censusEndpointURL is the single deploy-time constant for the census worker
// (a var only so tests can point it at a mock host).
// The endpoint URL is live; the release-bound Worker deployment is a separate
// pre-final gate documented in census-worker/README.md and docs/releasing.md.
var censusEndpointURL = "https://ha-nova-census.markusleben.workers.dev"

// censusEndpointConfigured reports whether this build carries a real census
// endpoint. A build still on the PLACEHOLDER is inert by construction: every
// send path skips silently BEFORE stamping the week, so an unconfigured build
// can neither phone a dead host nor burn a
// week that a properly configured build could have counted.
func censusEndpointConfigured() bool {
	return !strings.Contains(censusEndpointURL, "PLACEHOLDER")
}

const censusOptOutEnv = "HA_NOVA_NO_CENSUS"

const censusRequestTimeout = 1500 * time.Millisecond

// censusHTTPClient is dedicated and short-fused: the ping may never make an
// update check feel slow, and it never retries. 1.5s total: the send already
// runs AFTER all command output, so the only cost of a dead endpoint is a
// short delay of process exit — kept deliberately small. A detached
// fire-and-forget goroutine would be worse, not better: the process exits
// right after this call, silently dropping the send and breaking the
// at-most-once accounting (the week is already stamped). Overridable for
// tests. Redirects are returned as responses instead of followed: 307/308
// would otherwise replay the POST body and violate the single-attempt rule.
var censusHTTPClient = &http.Client{
	Timeout: censusRequestTimeout,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

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

// censusVersionPattern mirrors the worker's accepted version format
// (census-worker/src/census.ts VERSION_PATTERN) — contract-tested so the two
// sides cannot drift. An observed relay version that the worker would reject
// is OMITTED from the payload instead of getting the whole ping 400-rejected
// after the week was already stamped.
var censusVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-rc\d+)?$`)

const censusMaxVersionLength = 32

func censusValidVersion(v string) bool {
	return len(v) <= censusMaxVersionLength && censusVersionPattern.MatchString(v)
}

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
	// stamp from normal relay traffic — never a relay call for the census)
	// AND shaped like a version the worker accepts.
	if state.RelayVersion != "" && state.RelayVersionObservedAt != "" && censusValidVersion(state.RelayVersion) {
		if observed, err := time.Parse(time.RFC3339, state.RelayVersionObservedAt); err == nil {
			// Age must be non-negative: a future-dated observation (clock was
			// ahead when stamped, then corrected) is not "observed within the
			// last 7 days" — omit until a fresh observation lands.
			if age := now.Sub(observed); age >= 0 && age <= censusRelayFreshness {
				payload.Relay = state.RelayVersion
			}
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
// alters exit codes, never retries, and records at most one attempt per ISO
// week while local census state remains intact.
// Callers invoke it AFTER their own output is complete, so a hanging endpoint
// can never delay what the user (or a hook) is waiting for.
func maybeCensusPing(paths runtimePaths) {
	_ = sendCensusPingOnce(paths)
}

// censusWeekSendable is the ONE week gate shared by every send path (weekly
// carrier, `census on`, ask-yes): a send may proceed only when the current
// week is not yet stamped. A stamp in the FUTURE (clock rollback) suppresses
// the send WITHOUT downgrading the recorded week: rewriting it would re-open
// an already-counted week once the clock recovers. The stamp was written
// from this machine's own clock, so normal gating resumes when the clock
// reaches that week again.
func censusWeekSendable(state censusState, currentWeek string) bool {
	return state.LastPingWeek < currentWeek || state.LastPingWeek == ""
}

const (
	censusPingSkipEndpoint = "endpoint"
	censusPingSkipEnv      = "env"
	censusPingSkipDisabled = "disabled"
	censusPingSkipDev      = "dev"
	censusPingSkipOS       = "os"
	censusPingSkipWeek     = "week"
)

type censusPingResult struct {
	Payload   []byte
	Week      string
	Attempted bool
	Skipped   string
	Err       error
}

// sendCensusPingOnce is the sole send coordinator for carrier, command, and
// interactive-ask paths. It holds the same process/cross-process lock used by
// `census off` across consent/week re-check, stamp, and the bounded POST. That
// gives both promises one serialization point: at most one request attempt per
// client ISO week while local census state remains intact, and no new request
// can begin after a successful opt-out returns.
func sendCensusPingOnce(paths runtimePaths) censusPingResult {
	return sendCensusPingOnceWithClock(paths, censusNow)
}

func sendCensusPingOnceWithClock(paths runtimePaths, now func() time.Time) censusPingResult {
	if !censusEndpointConfigured() {
		return censusPingResult{Skipped: censusPingSkipEndpoint}
	}
	if censusOptedOutByEnv() {
		return censusPingResult{Skipped: censusPingSkipEnv}
	}
	if BuildChannel == "dev" || localVersion(paths) == "dev" {
		return censusPingResult{Skipped: censusPingSkipDev}
	}
	if censusOS() == "" {
		return censusPingResult{Skipped: censusPingSkipOS}
	}

	release, ok := acquireCensusLock(paths)
	if !ok {
		return censusPingResult{Err: fmt.Errorf("cannot acquire census state lock")}
	}
	defer release()

	// Re-check every mutable consent gate only after the lock is held. An
	// opt-out that won first is final for this attempt; an opt-out that arrives
	// during a POST waits for this bounded critical section before returning.
	if censusOptedOutByEnv() {
		return censusPingResult{Skipped: censusPingSkipEnv}
	}
	state := loadCensusState(paths)
	if !state.Enabled {
		return censusPingResult{Skipped: censusPingSkipDisabled}
	}
	stampTime := now().UTC()
	currentWeek := censusISOWeek(stampTime)
	if !censusWeekSendable(state, currentWeek) {
		return censusPingResult{Week: currentWeek, Skipped: censusPingSkipWeek}
	}

	payload := censusWireBytes(buildCensusPayload(paths, state, stampTime))
	if len(payload) == 0 {
		return censusPingResult{Week: currentWeek, Err: fmt.Errorf("empty census payload")}
	}
	state.LastPingWeek = currentWeek
	if err := saveCensusState(paths, state); err != nil {
		return censusPingResult{Week: currentWeek, Err: err}
	}
	result := censusPingResult{Payload: payload, Week: currentWeek, Attempted: true}
	result.Err = postCensusPing(payload)
	return result
}

// postCensusPing performs exactly one HTTP exchange. The caller has already
// stamped the week and holds the census lock. A hard request context remains
// in force even when tests replace the client with one that has no Timeout.
func postCensusPing(payload []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), censusRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, censusEndpointURL+"/ping", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	// NewRequest populates GetBody for a bytes.Reader. Leaving it set lets the
	// standard HTTP/1 and HTTP/2 transports replay this non-idempotent POST
	// after selected connection failures. Keep ContentLength, but make the body
	// non-rewindable so a request that may have reached the Worker is never
	// transmitted a second time.
	req.GetBody = nil
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
