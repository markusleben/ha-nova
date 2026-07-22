package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Send-path contracts: exact wire shape, the opt-in guard, the ISO-week gate
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
	return paths
}

// stubCensusEndpoint points the endpoint at a configured (non-placeholder)
// test host; the default build value stays PLACEHOLDER and is inert.
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
	got := string(censusWireBytes(buildCensusPayload(paths, state, now)))
	want := fmt.Sprintf(`{"schema":1,"version":"0.21.0","relay":"0.7.0","os":%q}`, censusOS())
	if got != want {
		t.Fatalf("wire payload = %s, want %s", got, want)
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
			got := string(censusWireBytes(buildCensusPayload(paths, tc.state, now)))
			if strings.Contains(got, "relay") {
				t.Fatalf("payload must omit relay, got %s", got)
			}
			want := fmt.Sprintf(`{"schema":1,"version":"0.21.0","os":%q}`, censusOS())
			if got != want {
				t.Fatalf("wire payload = %s, want %s", got, want)
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
		got := string(censusWireBytes(buildCensusPayload(paths, state, now)))
		if strings.Contains(got, "relay") {
			t.Fatalf("relay %q would be 400-rejected by the worker and must be omitted, got %s", invalid, got)
		}
	}
	// The worker-accepted rc shape stays included.
	state := censusState{Enabled: true, RelayVersion: "0.7.0-rc2", RelayVersionObservedAt: fresh}
	if got := string(censusWireBytes(buildCensusPayload(paths, state, now))); !strings.Contains(got, `"relay":"0.7.0-rc2"`) {
		t.Fatalf("valid rc relay version must be included, got %s", got)
	}
}

// Defense in depth: consent is re-checked inside sendCensusPing — after the
// week marker was claimed, immediately before the POST — so a `census off`
// (or the env kill switch) landing in that window still prevents the send.
func TestSendCensusPingRefusesAfterConsentRevoked(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	payload := censusWireBytes(buildCensusPayload(paths, loadCensusState(paths), time.Now().UTC()))

	// The window between marker claim and POST: the user opts out.
	if !claimCensusWeekMarker(paths, censusISOWeek(time.Now().UTC())) {
		t.Fatal("marker claim failed in a fresh cache dir")
	}
	captureStdout(t, func() { runCensusCommand(paths, []string{"off"}) })
	if err := sendCensusPing(paths, payload); err == nil {
		t.Fatal("sendCensusPing must refuse after the opt-out")
	}
	if len(*payloads) != 0 {
		t.Fatalf("no bytes may leave after an opt-out, got %d attempts", len(*payloads))
	}

	// Same for the env kill switch on a still-enabled install.
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	t.Setenv(censusOptOutEnv, "1")
	if err := sendCensusPing(paths, payload); err == nil {
		t.Fatal("sendCensusPing must refuse under the env kill switch")
	}
	if len(*payloads) != 0 {
		t.Fatalf("no bytes may leave under the kill switch, got %d attempts", len(*payloads))
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

func TestCensusWeekGateClockRollbackSelfHeals(t *testing.T) {
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
	if want := censusISOWeek(time.Now().UTC()); state.LastPingWeek != want {
		t.Fatalf("rollback self-heal must clamp the stamp to the current week: got %q, want %q", state.LastPingWeek, want)
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
