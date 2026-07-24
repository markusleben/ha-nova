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

const censusAskQuestionPrefix = "May this HA NOVA installation contribute to the maintainer's private"

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
	if !strings.Contains(out, censusAskQuestionPrefix) {
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
	if !state.Enabled || state.Answer != "yes" || state.AskedVia != "update" ||
		state.ConsentVersion != censusConsentVersion || !censusInstallationIDPattern.MatchString(state.InstallationID) {
		t.Fatalf("yes must enable: %+v", state)
	}
	if len(*payloads) != 1 {
		t.Fatalf("yes must ping immediately, got %d attempts", len(*payloads))
	}
	if state.LastAttemptAt == "" {
		t.Fatal("a successful first ping must stamp the attempt")
	}
	if !strings.Contains(out, "Your Yes choice was saved.") || !strings.Contains(out, "Installation report sent:") {
		t.Fatalf("yes must distinguish saved consent and confirmed first ping:\n%s", out)
	}
}

func TestCensusAskYesWithFailingTransportStampsAttemptNoRetry(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	stubCensusTTY(t, true, true)
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))

	out := askCensusWithInput(t, paths, "1\n")
	state := loadCensusState(paths)
	if !state.Enabled || state.Answer != "yes" {
		t.Fatalf("yes must enable even when the ping fails: %+v", state)
	}
	if state.LastAttemptAt == "" {
		t.Fatal("failed first ping must stay stamped to prevent an ambiguous retry")
	}
	if !strings.Contains(out, "Your Yes choice was saved.") || !strings.Contains(out, "Report result was not confirmed") {
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
		censusAskQuestionPrefix,
		"installation and version statistics",
		"HA NOVA sends no behavioral or feature-use analytics.",
		"Why this helps: by contributing, you give the maintainer a rough picture",
		"which HA NOVA and Relay versions they use",
		"how operating systems are distributed",
		"This helps prioritize compatibility",
		"work, tests, bug fixes, and new features",
		"If you agree, HA NOVA sends the first report now",
		"no sooner than seven days later",
		"The message content (JSON) contains only:",
		"payload schema  ·  random Census installation ID",
		"HA NOVA version  ·  operating system",
		"recently observed relay version (when available)",
		"The random ID only lets the same participating installation count once",
		"is not derived from or reused from a hardware or device identifier",
		"HA NOVA attaches no device data",
		"No usage or Home Assistant data",
		"Cloudflare is the hosting provider for the census endpoint",
		"It processes the",
		"HA NOVA ingest code does not read or store the source IP",
		"Counts are voluntary and self-reported",
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
	state := loadCensusState(paths)
	body := fmt.Sprintf(`{"schema":2,"installation_id":%q,"version":"0.9.0","os":%q}`, state.InstallationID, censusOS())
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

func TestCensusSkillNoticeUsesOneBoundChoiceToken(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")

	out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) })
	for _, want := range []string{
		"CENSUS ASK PENDING",
		"response's only active choice",
		"native selectable menu",
		"numbered fallback",
		"UI requires a default, use No",
		"by contributing, the user helps the maintainer get a rough picture",
		"prioritize compatibility work, tests, bug fixes, and new features",
		"not a roadmap vote or feature promise",
		"must not use guilt, pressure, or recommend opt-in",
		"Use at most five short visible lines in this order",
		"purpose/planning value; cadence; exact JSON field categories",
		"Cloudflare is the hosting provider",
		"no sooner than seven days later",
		"ha-nova census notice-presented",
		"ha-nova census choose <choice-id> yes",
		"ha-nova census choose <choice-id> no",
		"same choice ID",
		"display the literal JSON object verbatim",
		"If census status fails",
		"consent is unchanged",
		"Only a selection of the displayed Yes or No effect",
		"Missing, dismissed, free-form, or ambiguous input runs nothing",
		"stale choice was not saved",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("pending notice missing consent instruction %q:\n%s", want, out)
		}
	}
	if state := loadCensusState(paths); state.SkillPresentations != 0 || state.AskedAt != "" {
		t.Fatalf("deferred machine notice must not claim the choice: %+v", state)
	}

	claim := captureStdout(t, func() {
		if exit := runCensusCommand(paths, []string{"notice-presented"}); exit != 0 {
			t.Fatalf("notice-presented exit = %d", exit)
		}
	})
	token := strings.TrimSpace(strings.TrimPrefix(claim, "CENSUS NOTICE PRESENT "))
	if !censusChoiceIDPattern.MatchString(token) {
		t.Fatalf("presentation returned invalid choice ID: %q", claim)
	}
	state := loadCensusState(paths)
	if state.SkillPresentations != 1 || state.AskedAt == "" || state.Answer != "none" ||
		state.AskedVia != "skill" || state.PendingChoiceID != token {
		t.Fatalf("one-time presentation state mismatch: %+v", state)
	}
	if again := captureStdout(t, func() { runCensusCommand(paths, []string{"notice-presented"}) }); !strings.Contains(again, "CENSUS NOTICE SKIP") {
		t.Fatalf("second presentation must be skipped, got %q", again)
	}
	if exit := captureCensusCommandExit(t, func() int {
		return runCensusCommand(paths, []string{"choose", token, "no"})
	}); exit != 0 {
		t.Fatalf("bound No exit = %d", exit)
	}
	if state := loadCensusState(paths); state.Answer != "no" || state.Enabled || state.PendingChoiceID != "" {
		t.Fatalf("bound No was not applied exactly once: %+v", state)
	}
	if exit := captureCensusCommandExit(t, func() int {
		return runCensusCommand(paths, []string{"choose", token, "yes"})
	}); exit == 0 {
		t.Fatal("stale Yes must not overwrite the newer No")
	}
}

func TestCensusLegacySkillNoticesDoNotConsumePresentationSlots(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"one legacy emission", `{"schema":1,"enabled":false,"skill_notices":1}`},
		{"two legacy emissions", `{"schema":1,"enabled":false,"skill_notices":2}`},
		{"legacy auto-close", `{"schema":1,"asked_at":"2026-07-23T20:00:00Z","asked_via":"skill","answer":"none","enabled":false,"skill_notices":3}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := setupCensusTest(t)
			stubCensusVersion(t, "0.9.0")
			if err := os.WriteFile(paths.CensusFile, []byte(tc.raw), 0o600); err != nil {
				t.Fatal(err)
			}

			if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); !strings.Contains(out, "CENSUS ASK PENDING") {
				t.Fatalf("legacy machine emissions suppressed the pending choice: %q", out)
			}
			out := captureStdout(t, func() {
				if exit := runCensusCommand(paths, []string{"notice-presented"}); exit != 0 {
					t.Fatalf("notice-presented exit = %d", exit)
				}
			})
			if !strings.Contains(out, "CENSUS NOTICE PRESENT cns-choice-") {
				t.Fatalf("fresh visible choice did not return a token: %q", out)
			}
			state := loadCensusState(paths)
			if state.Schema != censusStateSchemaVersion || state.SkillNotices != 0 ||
				state.SkillPresentations != 1 || state.AskedAt == "" ||
				!censusChoiceIDPattern.MatchString(state.PendingChoiceID) {
				t.Fatalf("legacy notice migration mismatch: %+v", state)
			}
		})
	}
}

func TestCensusSkillChoiceCannotOverrideNewerManualConsent(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	claim := captureStdout(t, func() {
		if exit := runCensusCommand(paths, []string{"notice-presented"}); exit != 0 {
			t.Fatalf("notice-presented exit = %d", exit)
		}
	})
	token := strings.TrimSpace(strings.TrimPrefix(claim, "CENSUS NOTICE PRESENT "))
	if exit := captureCensusCommandExit(t, func() int { return runCensusOff(paths) }); exit != 0 {
		t.Fatalf("manual off exit = %d", exit)
	}
	if exit := captureCensusCommandExit(t, func() int {
		return runCensusCommand(paths, []string{"choose", token, "yes"})
	}); exit == 0 {
		t.Fatal("stale skill Yes must not override newer manual off")
	}
	if state := loadCensusState(paths); state.Enabled || state.Answer != "no" {
		t.Fatalf("stale skill action changed manual consent: %+v", state)
	}
}

func TestCensusLegacyYesRequiresFreshConsent(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	raw := `{"schema":1,"asked_at":"2026-07-23T20:00:00Z","asked_via":"skill","answer":"yes","enabled":true,"skill_notices":3}`
	if err := os.WriteFile(paths.CensusFile, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}

	if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); !strings.Contains(out, "CENSUS ASK PENDING") {
		t.Fatalf("legacy Yes must reopen the changed privacy choice, got: %q", out)
	}
	state := loadCensusState(paths)
	if state.Enabled || state.Answer != "" || state.AskedAt != "" ||
		state.ConsentVersion != 0 || state.InstallationID != "" {
		t.Fatalf("legacy Yes migration must fail closed and reopen consent: %+v", state)
	}
}

func TestCensusLegacyNoRemainsFinal(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.9.0")
	raw := `{"schema":1,"asked_at":"2026-07-23T20:00:00Z","asked_via":"skill","answer":"no","enabled":false}`
	if err := os.WriteFile(paths.CensusFile, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); out != "" {
		t.Fatalf("legacy No must remain final, got notice: %q", out)
	}
	state := loadCensusState(paths)
	if state.Enabled || state.Answer != "no" || state.AskedAt == "" ||
		state.ConsentVersion != censusConsentVersion {
		t.Fatalf("legacy No migration mismatch: %+v", state)
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
	if state.SkillPresentations != 0 || state.AskedAt != "" {
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
	if state := loadCensusState(paths); state.SkillPresentations != 0 {
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
