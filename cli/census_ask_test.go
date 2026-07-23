package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
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

type blockedCensusPromptReader struct {
	started chan struct{}
	answer  <-chan []byte
	once    sync.Once
}

func (r *blockedCensusPromptReader) Read(buffer []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	answer, ok := <-r.answer
	if !ok || answer == nil {
		return 0, io.EOF
	}
	return copy(buffer, answer), nil
}

func startBlockedCensusAsk(t *testing.T, paths runtimePaths) (chan []byte, <-chan string) {
	t.Helper()
	answer := make(chan []byte, 1)
	started := make(chan struct{})
	done := make(chan string, 1)
	reader := &blockedCensusPromptReader{started: started, answer: answer}
	go func() {
		var out bytes.Buffer
		askCensusIfEligible(paths, "update", bufio.NewReader(reader), &out)
		done <- out.String()
	}()
	t.Cleanup(func() {
		select {
		case answer <- nil:
		default:
		}
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("census ask did not reach the blocked prompt")
	}
	return answer, done
}

func finishBlockedCensusAsk(t *testing.T, answer chan []byte, done <-chan string, value string) string {
	t.Helper()
	answer <- []byte(value)
	select {
	case out := <-done:
		return out
	case <-time.After(2 * time.Second):
		t.Fatal("census ask did not finish after receiving an answer")
		return ""
	}
}

func TestCensusAskStampsAskedBeforePromptAbortedPromptNeverReasks(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)

	// Empty input: the prompt aborts with EOF (the Ctrl-C/crash shape).
	out := askCensusWithInput(t, paths, "")
	if !strings.Contains(out, "May HA NOVA include this install in its public census?") {
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
	if !strings.Contains(out, "Include this install? [y/N]") {
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

func TestCensusAskYesWithFailingTransportStampsWeekNoRetry(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))

	askCensusWithInput(t, paths, "yes\n")
	state := loadCensusState(paths)
	if !state.Enabled || state.Answer != "yes" {
		t.Fatalf("yes must enable even when the ping fails: %+v", state)
	}
	currentWeek := censusISOWeek(censusNow().UTC())
	if state.LastPingWeek != currentWeek {
		t.Fatalf("failed first ping must stay stamped to prevent an ambiguous retry: got %q, want %q", state.LastPingWeek, currentWeek)
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
		"May HA NOVA include this install in its public census?",
		"HA NOVA sends no behavioral or feature-use analytics.",
		"A yes permits one anonymous ping attempt per week while local census state remains intact:",
		"HA NOVA version  ·  relay version  ·  operating system",
		"No ID, no IP in HA NOVA storage, nothing about your home.",
		"directional accepted-ping counts, not verified unique installs.",
		"Details: docs/reference/census.md   Change anytime: ha-nova census on|off",
		"Include this install? [y/N]",
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

func TestCensusSkillNoticeConcurrentEmittersStayCappedWithoutCacheMarkers(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")

	start := make(chan struct{})
	out := captureStdout(t, func() {
		var wait sync.WaitGroup
		for range 24 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				maybeEmitCensusSkillNotice(paths)
			}()
		}
		close(start)
		wait.Wait()
	})
	state := loadCensusState(paths)
	if state.SkillNotices < 1 || state.SkillNotices > censusSkillNoticeCap {
		t.Fatalf("concurrent notice count = %d, want 1..%d", state.SkillNotices, censusSkillNoticeCap)
	}
	if emitted := strings.Count(out, "CENSUS ASK PENDING"); emitted != state.SkillNotices {
		t.Fatalf("printed notices = %d, persisted notices = %d", emitted, state.SkillNotices)
	}
	if _, err := os.Stat(paths.CacheDir); !os.IsNotExist(err) {
		t.Fatalf("marker-free notice coordination must not create CacheDir (err=%v)", err)
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

func TestCensusSkillNoticeDoesNotRequireCacheStorage(t *testing.T) {
	paths := setupCensusTest(t)
	paths.CacheDir = ""
	stubCensusVersion(t, "0.9.0")

	if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); !strings.Contains(out, "CENSUS ASK PENDING") {
		t.Fatalf("notice must use the census lock instead of cache markers, got %q", out)
	}
	if state := loadCensusState(paths); state.SkillNotices != 1 {
		t.Fatalf("notice without cache storage must persist slot 1: %+v", state)
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
		if !strings.Contains(out, "Include this install? [y/N]") {
			t.Fatalf("TTY plain human should get the direct question:\n%s", out)
		}
		if state := loadCensusState(paths); state.AskedAt == "" || state.Answer != "no" {
			t.Fatalf("Enter must record No via the direct ask: %+v", state)
		}
	})
}

func TestCensusAskOnlyTheStampWinnerPrompts(t *testing.T) {
	// Two interactive entry points racing past the unlocked pre-check must
	// resolve to exactly one prompt: the loser sees the stamp inside the
	// locked mutation and stays silent.
	paths := setupCensusTest(t)
	stubCensusTTY(t, true, true)

	// Simulate the racing winner: stamp lands after this process's pre-check
	// but before its locked mutation runs.
	if err := mutateCensusState(paths, func(s *censusState) {
		s.AskedAt = "2026-07-22T10:00:00Z"
		s.AskedVia = "update"
		s.Answer = "none"
	}); err != nil {
		t.Fatal(err)
	}
	// The pre-check is bypassed by handing maybeAskCensus a fresh-looking view:
	// easiest honest simulation is to call the mutation-race directly — the
	// asked file exists, so the in-lock re-check must refuse the prompt.
	var out strings.Builder
	askCensusIfEligible(paths, "doctor", bufio.NewReader(strings.NewReader("y\n")), &out)
	if strings.Contains(out.String(), "Include this install?") {
		t.Fatalf("loser must not prompt, got output:\n%s", out.String())
	}
	state := loadCensusState(paths)
	if state.AskedVia != "update" || state.Enabled {
		t.Fatalf("loser must not overwrite the winner's stamp or enable: %+v", state)
	}
}
