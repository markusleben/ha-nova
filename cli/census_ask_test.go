package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func stubCensusTTY(t *testing.T, stdinTTY, stdoutTTY bool) {
	t.Helper()
	originalIn := censusStdinIsTTY
	originalOut := censusStdoutIsTTY
	censusStdinIsTTY = func() bool { return stdinTTY }
	censusStdoutIsTTY = func() bool { return stdoutTTY }
	t.Cleanup(func() {
		censusStdinIsTTY = originalIn
		censusStdoutIsTTY = originalOut
	})
}

func askCensusWithInput(t *testing.T, paths runtimePaths, input string) string {
	t.Helper()
	var out bytes.Buffer
	askCensusIfEligible(paths, "update", bufio.NewReader(strings.NewReader(input)), &out)
	return out.String()
}

func TestCensusAskStampsAskedBeforePromptAbortedPromptNeverReasks(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)

	// Empty input: the prompt aborts with EOF (the Ctrl-C/crash shape).
	out := askCensusWithInput(t, paths, "")
	if !strings.Contains(out, "May HA NOVA count your install?") {
		t.Fatalf("expected the ask copy before the prompt, got %q", out)
	}
	state := loadCensusState(paths)
	if state.AskedAt == "" || state.Answer != "none" || state.Enabled {
		t.Fatalf("aborted prompt must leave asked_at stamped with answer=none: %+v", state)
	}

	// A second occasion must stay silent forever.
	if out := askCensusWithInput(t, paths, "y\n"); out != "" {
		t.Fatalf("already-asked install must never be asked again, got %q", out)
	}
}

func TestCensusAskNonTTYSkipsWithoutStamping(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	for _, tc := range []struct{ in, out bool }{{false, true}, {true, false}, {false, false}} {
		stubCensusTTY(t, tc.in, tc.out)
		if out := askCensusWithInput(t, paths, "y\n"); out != "" {
			t.Fatalf("non-TTY must not print the ask, got %q", out)
		}
		if _, err := os.Stat(paths.CensusFile); !os.IsNotExist(err) {
			t.Fatalf("non-TTY must not stamp census.json (err=%v)", err)
		}
	}
}

func TestCensusAskEnterMeansNo(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	out := askCensusWithInput(t, paths, "\n")
	if !strings.Contains(out, "Count this install? [y/N]") {
		t.Fatalf("expected the y/N prompt with No default, got %q", out)
	}
	state := loadCensusState(paths)
	if state.Enabled || state.Answer != "no" {
		t.Fatalf("Enter must mean No: %+v", state)
	}
	if len(*payloads) != 0 {
		t.Fatalf("a No must never send, got %d attempts", len(*payloads))
	}
}

func TestCensusAskYesEnablesAndPingsImmediately(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	askCensusWithInput(t, paths, "y\n")
	state := loadCensusState(paths)
	if !state.Enabled || state.Answer != "yes" || state.AskedVia != "update" {
		t.Fatalf("yes must enable: %+v", state)
	}
	if len(*payloads) != 1 {
		t.Fatalf("yes must ping immediately, got %d attempts", len(*payloads))
	}
	if state.LastPingWeek == "" {
		t.Fatal("a successful first ping must stamp the week")
	}
}

func TestCensusAskYesWithFailingTransportLeavesWeekEmpty(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))

	askCensusWithInput(t, paths, "yes\n")
	state := loadCensusState(paths)
	if !state.Enabled || state.Answer != "yes" {
		t.Fatalf("yes must enable even when the ping fails: %+v", state)
	}
	if state.LastPingWeek != "" {
		t.Fatalf("failed first ping must leave the week empty for a retry, got %q", state.LastPingWeek)
	}
}

func TestCensusAskGatesDevChannelAndEnvVar(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusTTY(t, true, true)

	originalChannel := BuildChannel
	BuildChannel = "dev"
	if out := askCensusWithInput(t, paths, "y\n"); out != "" {
		t.Fatalf("dev builds must never ask, got %q", out)
	}
	BuildChannel = originalChannel

	stubCensusVersion(t, "0.9.0")
	t.Setenv(censusOptOutEnv, "1")
	if out := askCensusWithInput(t, paths, "y\n"); out != "" {
		t.Fatalf("%s must suppress the ask, got %q", censusOptOutEnv, out)
	}
	if _, err := os.Stat(paths.CensusFile); !os.IsNotExist(err) {
		t.Fatalf("suppressed ask must not stamp (err=%v)", err)
	}
}

func TestCensusAskCopyVerbatim(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)

	out := askCensusWithInput(t, paths, "\n")
	for _, want := range []string{
		"One-time question",
		"May HA NOVA count your install? HA NOVA has no telemetry — that stays.",
		"A yes sends one anonymous ping, at most once a week:",
		"HA NOVA version  ·  relay version  ·  operating system",
		"No ID, no IP stored, nothing about your home — and the resulting",
		"numbers are public for everyone.",
		"Details: docs/reference/census.md   Change anytime: ha-nova census on|off",
		"Count this install? [y/N]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ask copy missing %q:\n%s", want, out)
		}
	}
}

func TestCensusSkillNoticeCapThreeEmissionsThenPermanentSilence(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")

	for i := 1; i <= 3; i++ {
		out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) })
		if !strings.Contains(out, "CENSUS ASK PENDING") {
			t.Fatalf("emission %d missing the pending block:\n%s", i, out)
		}
		if !strings.Contains(out, "ha-nova census on") || !strings.Contains(out, "ha-nova census off") {
			t.Fatalf("emission %d must name both commands:\n%s", i, out)
		}
	}
	state := loadCensusState(paths)
	if state.SkillNotices != 3 || state.AskedAt == "" || state.Answer != "none" || state.AskedVia != "skill" {
		t.Fatalf("third emission must close the question (answer=none): %+v", state)
	}

	if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); out != "" {
		t.Fatalf("fourth call must be silent forever, got %q", out)
	}
}

func TestCensusSkillNoticeSilentOnceAskedOrOptedOut(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	if err := saveCensusState(paths, censusState{AskedAt: "2026-07-01T00:00:00Z", Answer: "no"}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); out != "" {
		t.Fatalf("an answered install must not be nagged, got %q", out)
	}
}

func TestCheckUpdateHumanPathsEmitCensusNoticeJSONStaysClean(t *testing.T) {
	stubCensusVersion(t, "0.9.0")
	originalHTTPClient := httpClient
	t.Cleanup(func() { httpClient = originalHTTPClient })
	httpClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			body := `{"tag_name":"v0.9.0","html_url":"https://example.test/release"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	// The machine-directed block belongs to the skill channel (--quiet) only;
	// --json stays byte-clean and the plain human path asks directly instead.
	for _, tc := range []struct {
		name   string
		args   []string
		expect bool
	}{
		{"quiet human", []string{"--quiet"}, true},
		{"plain human", nil, false},
		{"json", []string{"--json"}, false},
		{"quiet json", []string{"--quiet", "--json"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupCensusTest(t)
			exit := 0
			out := captureStdout(t, func() { exit = runCheckUpdate(paths, tc.args) })
			if exit != 0 {
				t.Fatalf("exit = %d, want 0\n%s", exit, out)
			}
			if got := strings.Contains(out, "CENSUS ASK PENDING"); got != tc.expect {
				t.Fatalf("CENSUS ASK PENDING presence = %v, want %v\n%s", got, tc.expect, out)
			}
		})
	}

	// On a real terminal, the plain human path asks the one-time question
	// directly instead of emitting the skill block.
	t.Run("plain human TTY asks directly", func(t *testing.T) {
		paths := setupCensusTest(t)
		stubCensusTTY(t, true, true)
		stubServerCommandStdin(t, "\n")
		exit := 0
		out := captureStdout(t, func() { exit = runCheckUpdate(paths, nil) })
		if exit != 0 {
			t.Fatalf("exit = %d, want 0\n%s", exit, out)
		}
		if strings.Contains(out, "CENSUS ASK PENDING") {
			t.Fatalf("TTY plain human must not get the machine block:\n%s", out)
		}
		if !strings.Contains(out, "Count this install? [y/N]") {
			t.Fatalf("TTY plain human should get the direct question:\n%s", out)
		}
		if state := loadCensusState(paths); state.AskedAt == "" || state.Answer != "no" {
			t.Fatalf("Enter must record No via the direct ask: %+v", state)
		}
	})
}
