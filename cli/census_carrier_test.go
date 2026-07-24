package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Placeholder-inertness, platform/dev gating, and the
// byte-clean --json carrier — split from census_test.go per the <~400 LOC
// file guideline.

// A build still carrying the PLACEHOLDER endpoint must be inert by
// construction: no send and no week stamp — so a later
// properly configured build can still count the week.
func TestPlaceholderEndpointBuildIsInert(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	// Transport mock WITHOUT endpoint stubbing: restore the placeholder value.
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	censusEndpointURL = "https://ha-nova-census.PLACEHOLDER.workers.dev"
	if censusEndpointConfigured() {
		t.Fatal("placeholder endpoint must report unconfigured")
	}
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	maybeCensusPing(paths)
	captureStdout(t, func() { runCensusCommand(paths, []string{"on"}) })
	stubCensusTTY(t, true, true)
	// Fresh unasked state for the ask path: reset, then answer yes.
	if err := saveCensusState(paths, censusState{}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	askCensusWithInput(t, paths, "1\n")

	if len(*payloads) != 0 {
		t.Fatalf("placeholder build must never send, got %d attempts", len(*payloads))
	}
	if state := loadCensusState(paths); state.LastPingWeek != "" {
		t.Fatalf("placeholder build must not burn the week, got %q", state.LastPingWeek)
	}
	out := captureStdout(t, func() { runCensusCommand(paths, []string{"status"}) })
	if !strings.Contains(out, "census endpoint not configured in this build — nothing is sent") {
		t.Fatalf("status must say the endpoint is unconfigured:\n%s", out)
	}
	if strings.Contains(out, "workers.dev") {
		t.Fatalf("status must not print the placeholder URL:\n%s", out)
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
		if err := saveState(paths, defaultInstallState()); err != nil {
			t.Fatalf("save install lifecycle sentinel: %v", err)
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
