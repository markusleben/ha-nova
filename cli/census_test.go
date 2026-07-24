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

// Send-path contracts: exact application JSON shape, the opt-in guard, the ISO-week gate
// with stamp-before-send, clock-rollback self-heal, env opt-out, and the
// byte-clean --json carrier.

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
	state := censusState{
		Enabled:                true,
		RelayVersion:           "0.7.0",
		RelayVersionObservedAt: now.Add(-time.Hour).Format(time.RFC3339),
	}
	got := string(censusApplicationJSONBytes(buildCensusPayload(paths, state, now)))
	want := fmt.Sprintf(`{"schema":1,"version":"0.21.0","relay":"0.7.0","os":%q}`, censusOS())
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
		{"never observed", censusState{Enabled: true}},
		{"stale beyond seven days", censusState{
			Enabled:                true,
			RelayVersion:           "0.7.0",
			RelayVersionObservedAt: now.Add(-8 * 24 * time.Hour).Format(time.RFC3339),
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(censusApplicationJSONBytes(buildCensusPayload(paths, tc.state, now)))
			if strings.Contains(got, "relay") {
				t.Fatalf("payload must omit relay, got %s", got)
			}
			want := fmt.Sprintf(`{"schema":1,"version":"0.21.0","os":%q}`, censusOS())
			if got != want {
				t.Fatalf("application JSON = %s, want %s", got, want)
			}
		})
	}
}

func TestCensusPayloadOmitsInvalidRelayVersion(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	now := time.Now().UTC()
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	for _, invalid := range []string{"dev", "1.2", "0.7.0-beta1", "0.7", "v0.7.0", "0.7.0-rc", "1.0.0.0", "99999999999999999999999999999.0.0"} {
		state := censusState{Enabled: true, RelayVersion: invalid, RelayVersionObservedAt: fresh}
		got := string(censusApplicationJSONBytes(buildCensusPayload(paths, state, now)))
		if strings.Contains(got, "relay") {
			t.Fatalf("relay %q would be 400-rejected by the worker and must be omitted, got %s", invalid, got)
		}
	}
	// The worker-accepted rc shape stays included.
	state := censusState{Enabled: true, RelayVersion: "0.7.0-rc2", RelayVersionObservedAt: fresh}
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

func TestCensusPingStampsWeekBeforeSendAtMostOncePerWeek(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, 0, fmt.Errorf("transport down"))
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	maybeCensusPing(paths)
	if len(*payloads) != 1 {
		t.Fatalf("expected exactly one send attempt, got %d", len(*payloads))
	}
	state := loadCensusState(paths)
	currentWeek := censusISOWeek(time.Now().UTC())
	if state.LastPingWeek != currentWeek {
		t.Fatalf("week must be stamped BEFORE the send (at-most-once): got %q, want %q", state.LastPingWeek, currentWeek)
	}

	// Same week again — even though the first send failed, never double-send.
	maybeCensusPing(paths)
	if len(*payloads) != 1 {
		t.Fatalf("expected no second send in the same ISO week, got %d attempts", len(*payloads))
	}
}

// A future stamp (clock rollback) suppresses the send but keeps the recorded
// week intact — downgrading it would allow a double count after the clock
// recovers into the already-counted week.
func TestCensusWeekGateClockRollbackSuppressesWithoutDowngrade(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	futureWeek := censusISOWeek(time.Now().UTC().Add(21 * 24 * time.Hour))
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes", LastPingWeek: futureWeek}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	maybeCensusPing(paths)
	if len(*payloads) != 0 {
		t.Fatalf("a future-stamped week must not send (no double count), got %d attempts", len(*payloads))
	}
	state := loadCensusState(paths)
	if state.LastPingWeek != futureWeek {
		t.Fatalf("a recorded week must never be downgraded (it would re-open an already-counted week once the clock recovers): got %q, want %q", state.LastPingWeek, futureWeek)
	}
}

func TestCensusEnvVarSuppressesPing(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	t.Setenv(censusOptOutEnv, "1")

	maybeCensusPing(paths)
	if len(*payloads) != 0 {
		t.Fatalf("%s=1 must suppress the ping, got %d attempts", censusOptOutEnv, len(*payloads))
	}
	if state := loadCensusState(paths); state.LastPingWeek != "" {
		t.Fatalf("suppressed ping must not stamp a week, got %q", state.LastPingWeek)
	}
}

func TestCensusPayloadOmitsFutureDatedRelayObservation(t *testing.T) {
	// A future-dated observation (clock was ahead when stamped, then
	// corrected) is not "observed within the last 7 days" — omit until a
	// fresh observation lands.
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	state := censusState{
		Enabled:                true,
		RelayVersion:           "0.7.0",
		RelayVersionObservedAt: censusNow().UTC().Add(48 * time.Hour).Format(time.RFC3339),
	}
	payload := buildCensusPayload(paths, state, censusNow().UTC())
	if payload.Relay != "" {
		t.Fatalf("future-dated relay observation must be omitted, got %q", payload.Relay)
	}
}
