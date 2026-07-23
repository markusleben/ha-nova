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
	if !strings.Contains(out, "May this installation contribute to HA NOVA's public version statistics?") {
		t.Fatalf("expected the ask copy before the prompt, got %q", out)
	}
	state := loadCensusState(paths)
	if state.AskedAt == "" || state.Answer != "none" || state.Enabled {
		t.Fatalf("aborted prompt must leave asked_at stamped with answer=none: %+v", state)
	}

	// A second occasion must stay silent forever.
	if out := askCensusWithInput(t, paths, "1\n"); out != "" {
		t.Fatalf("already-asked install must never be asked again, got %q", out)
	}
}

func TestCensusAskNonTTYSkipsWithoutStamping(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	for _, tc := range []struct{ in, out bool }{{false, true}, {true, false}, {false, false}} {
		stubCensusTTY(t, tc.in, tc.out)
		if out := askCensusWithInput(t, paths, "1\n"); out != "" {
			t.Fatalf("non-TTY must not print the ask, got %q", out)
		}
		if _, err := os.Stat(paths.CensusFile); !os.IsNotExist(err) {
			t.Fatalf("non-TTY must not stamp census.json (err=%v)", err)
		}
	}
}

func TestCensusAskBlankChangesNothing(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	out := askCensusWithInput(t, paths, "\n")
	if !strings.Contains(out, "No choice saved. Enter 1, 2, or 3.") {
		t.Fatalf("expected an explicit-choice reminder, got %q", out)
	}
	state := loadCensusState(paths)
	if state.Enabled || state.Answer != "none" {
		t.Fatalf("blank input must change no consent state: %+v", state)
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

	out := askCensusWithInput(t, paths, "1\n")
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
	if !strings.Contains(out, "Your Yes choice was saved.") || !strings.Contains(out, "First ping sent:") {
		t.Fatalf("yes must distinguish saved consent and confirmed first ping:\n%s", out)
	}
}

func TestCensusAskYesWithFailingTransportStampsWeekNoRetry(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))

	out := askCensusWithInput(t, paths, "1\n")
	state := loadCensusState(paths)
	if !state.Enabled || state.Answer != "yes" {
		t.Fatalf("yes must enable even when the ping fails: %+v", state)
	}
	currentWeek := censusISOWeek(censusNow().UTC())
	if state.LastPingWeek != currentWeek {
		t.Fatalf("failed first ping must stay stamped to prevent an ambiguous retry: got %q, want %q", state.LastPingWeek, currentWeek)
	}
	if !strings.Contains(out, "Your Yes choice was saved.") || !strings.Contains(out, "Ping result was not confirmed") {
		t.Fatalf("failed transport must distinguish saved consent from an unconfirmed ping:\n%s", out)
	}
}

func TestCensusAskGatesDevChannelAndEnvVar(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusTTY(t, true, true)

	originalChannel := BuildChannel
	BuildChannel = "dev"
	if out := askCensusWithInput(t, paths, "1\n"); out != "" {
		t.Fatalf("dev builds must never ask, got %q", out)
	}
	BuildChannel = originalChannel

	stubCensusVersion(t, "0.9.0")
	t.Setenv(censusOptOutEnv, "1")
	if out := askCensusWithInput(t, paths, "1\n"); out != "" {
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

	out := askCensusWithInput(t, paths, "2\n")
	for _, want := range []string{
		"One-time privacy choice",
		"May this installation contribute to HA NOVA's public version statistics?",
		"HA NOVA sends no behavioral or feature-use analytics.",
		"If you agree, HA NOVA sends this version information now",
		"The message content (JSON) contains only:",
		"payload schema  ·  HA NOVA version  ·  operating system",
		"recently observed relay version (when available)",
		"No installation, device, or user ID",
		"no usage or Home Assistant data",
		"Cloudflare is the hosting provider for the census endpoint",
		"It processes the",
		"HA NOVA does not read the source IP",
		"The public numbers show general trends, not a verified installation count.",
		"Inspect exact JSON: ha-nova census status",
		"1. Yes — contribute",
		"2. No — do not contribute",
		"3. Show exact data",
		"Select 1, 2, or 3",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("ask copy missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.ToLower(out), "anonymous") {
		t.Fatalf("ask copy must not describe the HTTPS request as anonymous:\n%s", out)
	}
}

func TestCensusAskShowExactDataKeepsConsentOpenAndRendersAgain(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	stubCensusEndpoint(t)

	out := askCensusWithInput(t, paths, "3\n2\n")
	body := fmt.Sprintf(`{"schema":1,"version":"0.9.0","os":%q}`, censusOS())
	if !strings.Contains(out, "Exact application JSON body: "+body) {
		t.Fatalf("details must display the literal JSON object verbatim:\n%s", out)
	}
	if strings.Count(out, "1. Yes — contribute") != 2 {
		t.Fatalf("details must immediately render the same choice again:\n%s", out)
	}
	if state := loadCensusState(paths); state.Answer != "no" || state.Enabled {
		t.Fatalf("details must not change consent before the explicit No: %+v", state)
	}
}

func TestCensusAskFreeFormInputChangesNothing(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	out := askCensusWithInput(t, paths, "yellow\n")
	if !strings.Contains(out, "No choice saved. Enter 1, 2, or 3.") {
		t.Fatalf("free-form input must be rejected explicitly:\n%s", out)
	}
	if state := loadCensusState(paths); state.Answer != "none" || state.Enabled {
		t.Fatalf("free-form input must change nothing: %+v", state)
	}
	if len(*payloads) != 0 {
		t.Fatalf("free-form input must not send, got %d attempts", len(*payloads))
	}
}

func TestCensusSkillNoticeCapCountsPresentationsNotDeferredEmissions(t *testing.T) {
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
		for _, want := range []string{
			"response's only active choice",
			"native selectable menu",
			"numbered fallback",
			"UI requires a default, use No",
			"Cloudflare is the hosting provider",
			"at most once per week",
			"ha-nova census notice-presented",
			"ha-nova census status without changing consent",
			"display the literal JSON object verbatim",
			"If census status fails",
			"consent is unchanged",
			"Only a selection of the displayed Yes or No effect",
			"Missing, dismissed, free-form, or ambiguous input runs nothing",
			"consent command fails",
			"choice was not saved",
		} {
			if !strings.Contains(out, want) {
				t.Fatalf("emission %d missing consent instruction %q:\n%s", i, want, out)
			}
		}
	}
	if state := loadCensusState(paths); state.SkillNotices != 0 || state.AskedAt != "" {
		t.Fatalf("deferred machine notices must not consume presentations: %+v", state)
	}

	for i := 1; i <= censusSkillNoticeCap; i++ {
		out := captureStdout(t, func() {
			if exit := runCensusCommand(paths, []string{"notice-presented"}); exit != 0 {
				t.Fatalf("notice-presented %d exit = %d", i, exit)
			}
		})
		want := fmt.Sprintf("CENSUS NOTICE PRESENT %d/%d", i, censusSkillNoticeCap)
		if !strings.Contains(out, want) {
			t.Fatalf("presentation %d missing %q: %s", i, want, out)
		}
	}
	state := loadCensusState(paths)
	if state.SkillNotices != 3 || state.AskedAt == "" || state.Answer != "none" || state.AskedVia != "skill" {
		t.Fatalf("third actual presentation must close the question: %+v", state)
	}
	if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); out != "" {
		t.Fatalf("notice after the third presentation must be silent, got %q", out)
	}
}

func TestCensusSkillNoticeConcurrentDeliveryDoesNotConsumePresentations(t *testing.T) {
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
	if state.SkillNotices != 0 || state.AskedAt != "" {
		t.Fatalf("delivery without a visible choice must leave presentation state untouched: %+v", state)
	}
	if emitted := strings.Count(out, "CENSUS ASK PENDING"); emitted != 24 {
		t.Fatalf("printed notices = %d, want 24 independent deliveries", emitted)
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
		t.Fatalf("notice delivery must not require cache storage, got %q", out)
	}
	if state := loadCensusState(paths); state.SkillNotices != 0 {
		t.Fatalf("delivery alone must not persist a presentation slot: %+v", state)
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
		stubServerCommandStdin(t, "2\n")
		exit := 0
		out := captureStdout(t, func() { exit = runCheckUpdate(paths, nil) })
		if exit != 0 {
			t.Fatalf("exit = %d, want 0\n%s", exit, out)
		}
		if strings.Contains(out, "CENSUS ASK PENDING") {
			t.Fatalf("TTY plain human must not get the machine block:\n%s", out)
		}
		if !strings.Contains(out, "1. Yes — contribute") || !strings.Contains(out, "3. Show exact data") {
			t.Fatalf("TTY plain human should get the direct question:\n%s", out)
		}
		if state := loadCensusState(paths); state.AskedAt == "" || state.Answer != "no" {
			t.Fatalf("explicit option 2 must record No via the direct ask: %+v", state)
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
	askCensusIfEligible(paths, "doctor", bufio.NewReader(strings.NewReader("1\n")), &out)
	if out.String() != "" {
		t.Fatalf("loser must not prompt, got output:\n%s", out.String())
	}
	state := loadCensusState(paths)
	if state.AskedVia != "update" || state.Enabled {
		t.Fatalf("loser must not overwrite the winner's stamp or enable: %+v", state)
	}
}
