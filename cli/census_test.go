package main

import (
	"encoding/json"
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

func stubCensusTransport(t *testing.T, status int, sendErr error) *[]string {
	t.Helper()
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

func TestCensusWeekMarkerPreventsConcurrentDoubleSend(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	maybeCensusPing(paths)
	if len(*payloads) != 1 {
		t.Fatalf("first carrier must send, got %d attempts", len(*payloads))
	}
	// Simulate a racing process that loaded the pre-stamp state: it passes the
	// state week gate, but the exclusive week marker was already claimed.
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	maybeCensusPing(paths)
	if len(*payloads) != 1 {
		t.Fatalf("the week marker must block a concurrent second send, got %d attempts", len(*payloads))
	}
}

func TestCensusUnknownPlatformNeverSends(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	originalPlatform := censusPlatformOS
	censusPlatformOS = func() string { return "freebsd" }
	t.Cleanup(func() { censusPlatformOS = originalPlatform })
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	if got := censusOS(); got != "" {
		t.Fatalf("censusOS() on an unknown platform = %q, want empty", got)
	}
	maybeCensusPing(paths)
	captureStdout(t, func() {
		if exit := runCensusCommand(paths, []string{"on"}); exit != 0 {
			t.Fatalf("census on exit = %d, want 0", exit)
		}
	})
	if len(*payloads) != 0 {
		t.Fatalf("an unknown platform must never send a false os, got %d attempts", len(*payloads))
	}
}

func TestCensusPingSkipsDevBuild(t *testing.T) {
	paths := setupCensusTest(t)
	originalChannel := BuildChannel
	BuildChannel = "dev"
	t.Cleanup(func() { BuildChannel = originalChannel })
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	maybeCensusPing(paths)
	if len(*payloads) != 0 {
		t.Fatalf("dev builds must never ping, got %d attempts", len(*payloads))
	}
}

// The census must never alter one output byte of check-update — pinned on the
// strictest surface: `--quiet --json` (the detached refresh child's contract)
// with an enabled census whose transport is failing, versus no census at all.
func TestCheckUpdateJSONOutputByteIdenticalWithCensus(t *testing.T) {
	stubCensusVersion(t, "0.9.0")
	originalHTTPClient := httpClient
	t.Cleanup(func() { httpClient = originalHTTPClient })
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"tag_name":"v1.0.0","html_url":"https://example.test/release","published_at":"2026-07-01T00:00:00Z"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	run := func(withCensus bool) (string, int, int) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv(censusOptOutEnv, "")
		paths, err := detectPaths()
		if err != nil {
			t.Fatalf("detectPaths() error: %v", err)
		}
		attempts := stubCensusTransport(t, 0, fmt.Errorf("census endpoint down"))
		if withCensus {
			if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
				t.Fatalf("saveCensusState() error: %v", err)
			}
		}
		exit := 0
		out := captureStdout(t, func() {
			exit = runCheckUpdate(paths, []string{"--quiet", "--json"})
		})
		return out, exit, len(*attempts)
	}

	outWith, exitWith, attemptsWith := run(true)
	outWithout, exitWithout, attemptsWithout := run(false)

	if attemptsWith < 1 {
		t.Fatal("the ping must ride the --quiet --json path (no send attempt recorded)")
	}
	if attemptsWithout != 0 {
		t.Fatalf("disabled census must not attempt sends, got %d", attemptsWithout)
	}
	if outWith != outWithout {
		t.Fatalf("--json output must be byte-identical with and without census:\nwith:    %q\nwithout: %q", outWith, outWithout)
	}
	if exitWith != exitWithout {
		t.Fatalf("exit codes must match: with=%d without=%d", exitWith, exitWithout)
	}
	var decoded updateCheckResult
	if err := json.Unmarshal([]byte(outWith), &decoded); err != nil {
		t.Fatalf("--json output is not valid JSON: %v\n%s", err, outWith)
	}
}
