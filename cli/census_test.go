package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Send-path contracts: exact application JSON shape, the opt-in guard, the
// rolling seven-day gate with stamp-before-send, clock-rollback safety, env opt-out, and the
// byte-clean --json carrier.

const testCensusInstallationID = "cns-0123456789abcdef0123456789abcdef"

func optedInCensusState() censusState {
	return censusState{
		Schema:         censusStateSchemaVersion,
		ConsentVersion: censusConsentVersion,
		InstallationID: testCensusInstallationID,
		Enabled:        true,
		Answer:         "yes",
	}
}

func setupCensusTest(t *testing.T) runtimePaths {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(censusOptOutEnv, "")
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	if err := saveState(paths, defaultInstallState()); err != nil {
		t.Fatalf("save install lifecycle sentinel: %v", err)
	}
	return paths
}

// stubCensusEndpoint points the endpoint at an isolated configured test host;
// separate tests still prove that any PLACEHOLDER build stays inert.
func stubCensusEndpoint(t *testing.T) {
	t.Helper()
	original := censusEndpointURL
	censusEndpointURL = "https://ha-nova-census.test-suite.workers.dev"
	t.Cleanup(func() { censusEndpointURL = original })
}

func stubCensusTransport(t *testing.T, status int, sendErr error) *[]string {
	t.Helper()
	stubCensusEndpoint(t)
	var payloads []string
	original := censusHTTPClient
	censusHTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(req.Body)
			payloads = append(payloads, string(body))
			if sendErr != nil {
				return nil, sendErr
			}
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}
	t.Cleanup(func() { censusHTTPClient = original })
	return &payloads
}

func stubCensusVersion(t *testing.T, version string) {
	t.Helper()
	originalVersion := Version
	originalChannel := BuildChannel
	Version = version
	BuildChannel = ""
	t.Cleanup(func() {
		Version = originalVersion
		BuildChannel = originalChannel
	})
}

func TestCensusWirePayloadExactShape(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	now := time.Now().UTC()
	state := optedInCensusState()
	state.RelayVersion = "0.7.0"
	state.RelayVersionObservedAt = now.Add(-time.Hour).Format(time.RFC3339)
	got := string(censusApplicationJSONBytes(buildCensusPayload(paths, state, now)))
	want := fmt.Sprintf(`{"schema":2,"installation_id":%q,"version":"0.21.0","relay":"0.7.0","os":%q}`, testCensusInstallationID, censusOS())
	if got != want {
		t.Fatalf("application JSON = %s, want %s", got, want)
	}
	switch censusOS() {
	case "macos", "linux", "windows":
	default:
		t.Fatalf("censusOS() = %q, want one of macos/linux/windows", censusOS())
	}
}

func TestCensusPayloadOmitsStaleOrMissingRelay(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	now := time.Now().UTC()
	cases := []struct {
		name  string
		state censusState
	}{
		{"never observed", optedInCensusState()},
		{"stale beyond fourteen days", func() censusState {
			state := optedInCensusState()
			state.RelayVersion = "0.7.0"
			state.RelayVersionObservedAt = now.Add(-15 * 24 * time.Hour).Format(time.RFC3339)
			return state
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(censusApplicationJSONBytes(buildCensusPayload(paths, tc.state, now)))
			if strings.Contains(got, "relay") {
				t.Fatalf("payload must omit relay, got %s", got)
			}
			want := fmt.Sprintf(`{"schema":2,"installation_id":%q,"version":"0.21.0","os":%q}`, testCensusInstallationID, censusOS())
			if got != want {
				t.Fatalf("application JSON = %s, want %s", got, want)
			}
		})
	}
}

func TestCensusPayloadIncludesRelayObservedEightDaysAgo(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	now := time.Now().UTC()
	state := optedInCensusState()
	state.RelayVersion = "0.7.1"
	state.RelayVersionObservedAt = now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)
	got := string(censusApplicationJSONBytes(buildCensusPayload(paths, state, now)))
	if !strings.Contains(got, `"relay":"0.7.1"`) {
		t.Fatalf("relay observed eight days ago must remain fresh for the 14-day window: %s", got)
	}
}

func TestCensusPayloadOmitsInvalidRelayVersion(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	now := time.Now().UTC()
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	for _, invalid := range []string{"dev", "1.2", "0.7.0-beta1", "0.7", "v0.7.0", "0.7.0-rc", "1.0.0.0", "99999999999999999999999999999.0.0"} {
		state := optedInCensusState()
		state.RelayVersion = invalid
		state.RelayVersionObservedAt = fresh
		got := string(censusApplicationJSONBytes(buildCensusPayload(paths, state, now)))
		if strings.Contains(got, "relay") {
			t.Fatalf("relay %q would be 400-rejected by the worker and must be omitted, got %s", invalid, got)
		}
	}
	// The worker-accepted rc shape stays included.
	state := optedInCensusState()
	state.RelayVersion = "0.7.0-rc2"
	state.RelayVersionObservedAt = fresh
	if got := string(censusApplicationJSONBytes(buildCensusPayload(paths, state, now))); !strings.Contains(got, `"relay":"0.7.0-rc2"`) {
		t.Fatalf("valid rc relay version must be included, got %s", got)
	}
}

func TestCensusNeverSendsWithoutEnabled(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	// No census.json at all.
	maybeCensusPing(paths)
	// Explicitly disabled (answered no).
	if err := saveCensusState(paths, censusState{Enabled: false, Answer: "no", AskedAt: "2026-07-01T00:00:00Z"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	maybeCensusPing(paths)

	if len(*payloads) != 0 {
		t.Fatalf("expected zero sends without enabled=true, got %d: %v", len(*payloads), *payloads)
	}
}

func TestCensusStateWritersFailClosedWithoutInstallSentinel(t *testing.T) {
	paths := setupCensusTest(t)
	if err := os.Remove(paths.StateFile); err != nil {
		t.Fatalf("remove install sentinel: %v", err)
	}
	if err := os.Remove(paths.ConfigDir); err != nil {
		t.Fatalf("remove empty config directory: %v", err)
	}

	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err == nil {
		t.Fatal("saveCensusState succeeded without state.json")
	}
	if err := mutateCensusState(paths, func(state *censusState) { state.Enabled = true }); err == nil {
		t.Fatal("mutateCensusState succeeded without state.json")
	}
	for _, path := range []string{paths.CensusFile, paths.ConfigDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("missing-sentinel writer recreated %s (err=%v)", path, err)
		}
	}
}

func TestCensusPingStampsAttemptBeforeSendAndWaitsSevenDays(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, 0, fmt.Errorf("transport down"))
	if err := saveCensusState(paths, optedInCensusState()); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	maybeCensusPing(paths)
	if len(*payloads) != 1 {
		t.Fatalf("expected exactly one send attempt, got %d", len(*payloads))
	}
	state := loadCensusState(paths)
	if state.LastAttemptAt == "" {
		t.Fatal("attempt must be stamped BEFORE the send")
	}

	// Less than seven days later — even though the first send failed, never double-send.
	maybeCensusPing(paths)
	if len(*payloads) != 1 {
		t.Fatalf("expected no second send inside seven days, got %d attempts", len(*payloads))
	}
}

// A future attempt timestamp (clock rollback) suppresses the send and remains
// intact, avoiding ambiguous duplicates until the local clock recovers.
func TestCensusCadenceClockRollbackSuppressesWithoutDowngrade(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	state := optedInCensusState()
	futureAttempt := time.Now().UTC().Add(21 * 24 * time.Hour).Format(time.RFC3339Nano)
	state.LastAttemptAt = futureAttempt
	if err := saveCensusState(paths, state); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	maybeCensusPing(paths)
	if len(*payloads) != 0 {
		t.Fatalf("a future attempt timestamp must not send, got %d attempts", len(*payloads))
	}
	loaded := loadCensusState(paths)
	if loaded.LastAttemptAt != futureAttempt {
		t.Fatalf("recorded attempt was changed: got %q, want %q", loaded.LastAttemptAt, futureAttempt)
	}
}

func TestCensusEnvVarSuppressesPing(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, optedInCensusState()); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	t.Setenv(censusOptOutEnv, "1")

	maybeCensusPing(paths)
	if len(*payloads) != 0 {
		t.Fatalf("%s=1 must suppress the ping, got %d attempts", censusOptOutEnv, len(*payloads))
	}
	if state := loadCensusState(paths); state.LastAttemptAt != "" {
		t.Fatalf("suppressed ping must not stamp an attempt, got %q", state.LastAttemptAt)
	}
}

func TestCensusPayloadOmitsFutureDatedRelayObservation(t *testing.T) {
	// A future-dated observation (clock was ahead when stamped, then
	// corrected) is not "observed within the last 14 days" — omit until a
	// fresh observation lands.
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	state := optedInCensusState()
	state.RelayVersion = "0.7.0"
	state.RelayVersionObservedAt = censusNow().UTC().Add(48 * time.Hour).Format(time.RFC3339)
	payload := buildCensusPayload(paths, state, censusNow().UTC())
	if payload.Relay != "" {
		t.Fatalf("future-dated relay observation must be omitted, got %q", payload.Relay)
	}
}

func TestCensusCIGuardProtectsProductionEndpointOnly(t *testing.T) {
	// CI + untouched built-in endpoint: the ping skips and the send layer
	// refuses — production statistics stay clean during installer smokes.
	t.Setenv("CI", "true")
	paths := setupCensusTest(t)
	result := sendCensusPingOnce(paths)
	if result.Skipped != censusPingSkipCI {
		t.Fatalf("expected CI skip against the built-in endpoint, got %+v", result)
	}
	if err := postCensusJSON("/ping", []byte("{}")); err == nil {
		t.Fatal("send layer must refuse the production endpoint in CI")
	}

	// CI + stubbed endpoint (every other test): fully unaffected.
	payloads := stubCensusTransport(t, 204, nil)
	if err := postCensusJSON("/ping", []byte("{}")); err != nil {
		t.Fatalf("stubbed endpoint must pass in CI: %v", err)
	}
	if len(*payloads) != 1 {
		t.Fatalf("stubbed send did not happen: %d payloads", len(*payloads))
	}
}
