package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCensusOnEnablesAndPingsSuccessStampsAttempt(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	exit := 0
	out := captureStdout(t, func() { exit = runCensusCommand(paths, []string{"on"}) })
	if exit != 0 {
		t.Fatalf("census on exit = %d, want 0\n%s", exit, out)
	}
	state := loadCensusState(paths)
	if !state.Enabled || state.Answer != "yes" || state.AskedAt == "" || state.AskedVia != "command" ||
		state.ConsentVersion != censusConsentVersion || !censusInstallationIDPattern.MatchString(state.InstallationID) {
		t.Fatalf("census on state: %+v", state)
	}
	if len(*payloads) != 1 {
		t.Fatalf("census on must ping immediately, got %d attempts", len(*payloads))
	}
	if state.LastAttemptAt == "" {
		t.Fatal("successful immediate ping must stamp the attempt")
	}
	if !strings.Contains(out, `"schema":2`) || !strings.Contains(out, `"installation_id"`) {
		t.Fatalf("census on should echo the sent payload, got %q", out)
	}
}

func TestCensusOnTwiceInsideSevenDaysSendsExactlyOnce(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	captureStdout(t, func() { runCensusCommand(paths, []string{"on"}) })
	out := captureStdout(t, func() {
		if exit := runCensusCommand(paths, []string{"on"}); exit != 0 {
			t.Fatalf("second census on exit != 0")
		}
	})
	if len(*payloads) != 1 {
		t.Fatalf("census on twice inside seven days must send exactly once, got %d", len(*payloads))
	}
	if !strings.Contains(out, "less than seven days ago") {
		t.Fatalf("second census on should explain the rolling cadence:\n%s", out)
	}
}

func TestCensusOnSkipsSendOnDevBuild(t *testing.T) {
	paths := setupCensusTest(t)
	originalChannel := BuildChannel
	BuildChannel = "dev"
	t.Cleanup(func() { BuildChannel = originalChannel })
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	captureStdout(t, func() {
		if exit := runCensusCommand(paths, []string{"on"}); exit != 0 {
			t.Fatalf("census on exit != 0 on dev build")
		}
	})
	if len(*payloads) != 0 {
		t.Fatalf("census on must not ping from a dev build, got %d attempts", len(*payloads))
	}
	if state := loadCensusState(paths); !state.Enabled {
		t.Fatal("census on must still record the opt-in on a dev build")
	}
}

// Repeating `on` while already enabled preserves cadence. A fresh explicit Yes
// from disabled state starts a new participation period and reports now.
func TestCensusOnPreservesCadenceButFreshYesResetsIt(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	state := optedInCensusState()
	futureAttempt := time.Now().UTC().Add(21 * 24 * time.Hour).Format(time.RFC3339Nano)
	state.AskedAt = "2026-07-01T00:00:00Z"
	state.LastAttemptAt = futureAttempt
	if err := saveCensusState(paths, state); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}

	exit := 0
	out := captureStdout(t, func() { exit = runCensusCommand(paths, []string{"on"}) })
	if exit != 0 {
		t.Fatalf("census on exit = %d, want 0\n%s", exit, out)
	}
	if len(*payloads) != 0 {
		t.Fatalf("a future stamp must not let the manual path send, got %d attempts", len(*payloads))
	}
	if state := loadCensusState(paths); state.LastAttemptAt != futureAttempt {
		t.Fatalf("manual path changed the recorded attempt: got %q, want %q", state.LastAttemptAt, futureAttempt)
	}

	// A fresh ask-Yes from disabled state clears stale cadence and sends now.
	unanswered := censusState{Schema: censusStateSchemaVersion, InstallationID: testCensusInstallationID, LastAttemptAt: futureAttempt}
	if err := saveCensusState(paths, unanswered); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	stubCensusTTY(t, true, true)
	askCensusWithInput(t, paths, "1\n")
	if len(*payloads) != 1 {
		t.Fatalf("fresh ask-Yes must send despite stale disabled-state cadence, got %d attempts", len(*payloads))
	}
	if state := loadCensusState(paths); state.LastAttemptAt == futureAttempt || state.LastAttemptAt == "" {
		t.Fatalf("fresh ask-Yes did not replace stale cadence: %+v", state)
	}
}

func TestCensusOnPingFailureStampsAttemptNoRetry(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))

	exit := 0
	captureStdout(t, func() { exit = runCensusCommand(paths, []string{"on"}) })
	if exit != 0 {
		t.Fatalf("census on must still succeed when the ping fails, exit = %d", exit)
	}
	state := loadCensusState(paths)
	if !state.Enabled {
		t.Fatal("census on must enable despite a failed ping")
	}
	if state.LastAttemptAt == "" {
		t.Fatal("failed ping must stay stamped to prevent an ambiguous retry")
	}
}

func TestCensusOffDisablesAndStampsAnswer(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	captureStdout(t, func() { runCensusCommand(paths, []string{"on"}) })

	exit := 0
	captureStdout(t, func() { exit = runCensusCommand(paths, []string{"off"}) })
	if exit != 0 {
		t.Fatalf("census off exit = %d, want 0", exit)
	}
	state := loadCensusState(paths)
	if state.Enabled || state.Answer != "no" || state.AskedAt == "" {
		t.Fatalf("census off state: %+v", state)
	}

	// And nothing sends afterwards.
	before := len(*payloads)
	maybeCensusPing(paths)
	if len(*payloads) != before {
		t.Fatal("census off must stop all sends")
	}
}

func TestCensusOffWithdrawsTheDedicatedInstallationID(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusEndpoint(t)
	var requests []struct {
		path string
		body string
	}
	original := censusHTTPClient
	censusHTTPClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		requests = append(requests, struct {
			path string
			body string
		}{path: request.URL.Path, body: string(body)})
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	t.Cleanup(func() { censusHTTPClient = original })

	if exit := captureCensusCommandExit(t, func() int { return runCensusOn(paths) }); exit != 0 {
		t.Fatalf("census on exit = %d", exit)
	}
	state := loadCensusState(paths)
	if exit := captureCensusCommandExit(t, func() int { return runCensusOff(paths) }); exit != 0 {
		t.Fatalf("census off exit = %d", exit)
	}
	if len(requests) != 2 || requests[0].path != "/ping" || requests[1].path != "/withdraw" {
		t.Fatalf("request sequence = %+v, want /ping then /withdraw", requests)
	}
	want := fmt.Sprintf(`{"schema":2,"installation_id":%q}`, state.InstallationID)
	if requests[1].body != want {
		t.Fatalf("withdrawal body = %s, want %s", requests[1].body, want)
	}
	withdrawn := loadCensusState(paths)
	if withdrawn.WithdrawalPending || withdrawn.InstallationID != "" || withdrawn.LastAttemptAt != "" {
		t.Fatalf("confirmed withdrawal must clear server-linkage state: %+v", withdrawn)
	}
	if exit := captureCensusCommandExit(t, func() int { return runCensusOn(paths) }); exit != 0 {
		t.Fatalf("re-opt-in exit = %d", exit)
	}
	if len(requests) != 3 || requests[2].path != "/ping" {
		t.Fatalf("re-opt-in must report immediately after confirmed deletion: %+v", requests)
	}
	if current := loadCensusState(paths); current.InstallationID == state.InstallationID ||
		!censusInstallationIDPattern.MatchString(current.InstallationID) {
		t.Fatalf("re-opt-in must use a fresh dedicated ID after confirmed deletion: old=%q current=%+v", state.InstallationID, current)
	}
}

func TestCensusOffKeepsPendingDeletionAfterUnconfirmedWithdrawal(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	state := optedInCensusState()
	state.LastAttemptAt = censusNow().UTC().Format(time.RFC3339Nano)
	if err := saveCensusState(paths, state); err != nil {
		t.Fatal(err)
	}
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))

	if exit := captureCensusCommandExit(t, func() int { return runCensusOff(paths) }); exit != 0 {
		t.Fatalf("census off exit = %d", exit)
	}
	loaded := loadCensusState(paths)
	if loaded.Enabled || loaded.Answer != "no" || !loaded.WithdrawalPending {
		t.Fatalf("unconfirmed withdrawal state = %+v", loaded)
	}
}

func TestCensusStatusPrintsLiteralApplicationJSONAndURLs(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusEndpoint(t)

	exit := 0
	out := captureStdout(t, func() { exit = runCensusCommand(paths, []string{"status"}) })
	if exit != 0 {
		t.Fatalf("census status exit = %d, want 0\n%s", exit, out)
	}
	state := loadCensusState(paths)
	body := fmt.Sprintf(`{"schema":2,"installation_id":%q,"version":"0.21.0","os":%q}`, state.InstallationID, censusOS())
	for _, want := range []string{
		"Census: off",
		"Exact application JSON body:",
		body, // the LITERAL application-body bytes, not a description
		"reports are attempted no sooner than seven days apart",
		"Last attempted: never",
		"Purpose: voluntary reports provide a rough picture",
		"helping prioritize compatibility work, tests, bug fixes, and new features",
		"not a roadmap vote, verified installed-base count, or feature promise",
		censusStatsURL(),
		"ha-nova census off",
		"HA_NOVA_NO_CENSUS",
		"not verified people or the complete installed base",
		"Cloudflare hosts the census endpoint and processes the source IP and connection metadata",
		"HA NOVA ingest code does not read the source IP",
		"application storage does not store it",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("census status missing %q:\n%s", want, out)
		}
	}
}

func TestCensusStatusCreatesOnlyLocalIDWithoutChangingConsent(t *testing.T) {
	t.Run("pristine install", func(t *testing.T) {
		paths := setupCensusTest(t)
		stubCensusVersion(t, "0.21.0")

		if exit := captureCensusCommandExit(t, func() int { return runCensusStatus(paths) }); exit != 0 {
			t.Fatalf("census status exit = %d, want 0", exit)
		}
		state := loadCensusState(paths)
		if !censusInstallationIDPattern.MatchString(state.InstallationID) || state.Enabled || state.Answer != "" {
			t.Fatalf("status must create only a local census id, got %+v", state)
		}
	})

	t.Run("existing unanswered state", func(t *testing.T) {
		paths := setupCensusTest(t)
		stubCensusVersion(t, "0.21.0")
		state := censusState{
			Schema:             censusStateSchemaVersion,
			AskedAt:            "2026-07-23T20:00:00Z",
			AskedVia:           "skill",
			Answer:             "none",
			SkillPresentations: 1,
		}
		if err := saveCensusState(paths, state); err != nil {
			t.Fatal(err)
		}
		if exit := captureCensusCommandExit(t, func() int { return runCensusStatus(paths) }); exit != 0 {
			t.Fatalf("census status exit = %d, want 0", exit)
		}
		after := loadCensusState(paths)
		if !censusInstallationIDPattern.MatchString(after.InstallationID) ||
			after.Enabled || after.Answer != "none" || after.AskedAt != state.AskedAt ||
			after.AskedVia != state.AskedVia || after.SkillPresentations != state.SkillPresentations {
			t.Fatalf("status changed consent metadata: before=%+v after=%+v", state, after)
		}
	})
}

func TestCensusCommandFailureAfterFinalNoticePreservesUnansweredState(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	raw := []byte(`{"schema":4,"asked_at":"2026-07-23T20:00:00Z","asked_via":"skill","answer":"none","enabled":false,"skill_presentations":3}`)
	if err := os.WriteFile(paths.CensusFile, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	exit := captureCensusCommandExit(t, func() int { return runCensusOn(paths) })
	if exit == 0 {
		t.Fatal("census on unexpectedly overwrote newer unanswered state")
	}
	after, err := os.ReadFile(paths.CensusFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(raw) {
		t.Fatalf("failed consent command changed final-notice state:\nbefore: %s\nafter:  %s", raw, after)
	}
}

func TestCensusUnknownSubcommandFails(t *testing.T) {
	paths := setupCensusTest(t)
	exit := 0
	captureStdout(t, func() { exit = runCensusCommand(paths, []string{"purge"}) })
	if exit != 1 {
		t.Fatalf("unknown subcommand exit = %d, want 1", exit)
	}
	if exit := runCensusCommand(paths, nil); exit != 1 {
		t.Fatalf("bare census exit = %d, want 1", exit)
	}
}

func TestUninstallBaseListIncludesCensusFile(t *testing.T) {
	paths := runtimePaths{
		ConfigDir:  "/cfg",
		StateFile:  filepath.Join("/cfg", "state.json"),
		CensusFile: filepath.Join("/cfg", "census.json"),
	}
	want := filepath.Join("/cfg", "census.json")
	found := false
	for _, path := range managedConfigArtifactPaths(paths, false) {
		if path == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("uninstall base list must remove census.json, got %v", managedConfigArtifactPaths(paths, false))
	}
}

func TestCensusOnFailedFirstPingKeepsTheAttemptClaimed(t *testing.T) {
	// A failed immediate ping is ambiguous: the Worker may have counted the
	// request before the response was lost. Keep the locked state stamp so a
	// later update check cannot create a duplicate.
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))
	captureStdout(t, func() { runCensusCommand(paths, []string{"on"}) })

	if state := loadCensusState(paths); state.LastAttemptAt == "" {
		t.Fatal("failed first ping must keep the attempt stamp")
	}
	// A later carrier does not retry inside seven days.
	payloads := stubCensusTransport(t, 204, nil)
	maybeCensusPing(paths)
	if got := len(*payloads); got != 0 {
		t.Fatalf("carrier retry attempts = %d, want 0", got)
	}
}

func TestCensusStatusReportsFutureAttemptClockClamp(t *testing.T) {
	paths := setupCensusTest(t)
	state := optedInCensusState()
	future := censusNow().UTC().Add(21 * 24 * time.Hour)
	state.LastAttemptAt = future.Format(time.RFC3339Nano)
	if err := saveCensusState(paths, state); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { runCensusStatus(paths) })
	want := fmt.Sprintf("Next possible report: %s", future.Add(censusSendInterval).Format(time.RFC3339))
	if !strings.Contains(out, want) {
		t.Fatalf("future-attempt status missing %q:\n%s", want, out)
	}
}

func TestCensusPendingNoCannotOverrideExplicitOn(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTTY(t, true, true)
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	answer, done := startBlockedCensusAsk(t, paths)
	exit := 0
	captureStdout(t, func() { exit = runCensusOn(paths) })
	if exit != 0 {
		t.Fatalf("census on exit = %d, want 0", exit)
	}
	out := finishBlockedCensusAsk(t, answer, done, "2\n")
	state := loadCensusState(paths)
	if !state.Enabled || state.Answer != "yes" {
		t.Fatalf("stale prompt no overrode explicit opt-in: %+v", state)
	}
	if len(*payloads) != 1 {
		t.Fatalf("explicit opt-in sent %d requests, want 1", len(*payloads))
	}
	if strings.Contains(out, "This install will not send census pings") {
		t.Fatalf("stale prompt must not claim that opt-out was applied:\n%s", out)
	}
}

func TestCensusUninstallSentinelBlocksEveryWriterAndNotice(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTTY(t, true, true)
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	if err := saveCensusState(paths, optedInCensusState()); err != nil {
		t.Fatal(err)
	}

	previousHook := censusPreMutateHook
	var cleanupErr error
	censusPreMutateHook = func() {
		censusPreMutateHook = previousHook
		cleanupErr = removeManagedConfigArtifacts(paths, &uninstallReport{}, true)
		if cleanupErr == nil {
			cleanupErr = removeManagedCacheArtifacts(paths, &uninstallReport{})
		}
	}
	t.Cleanup(func() { censusPreMutateHook = previousHook })
	// Exercise a relay writer that passed its unlocked enabled pre-check before
	// uninstall removed the install sentinel.
	stampCensusRelayVersion(paths, "0.7.1")
	if cleanupErr != nil {
		t.Fatalf("uninstall cleanup: %v", cleanupErr)
	}

	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err == nil {
		t.Fatal("direct census save succeeded after uninstall")
	}
	if err := mutateCensusState(paths, func(state *censusState) { state.Enabled = true }); err == nil {
		t.Fatal("locked census mutation succeeded after uninstall")
	}
	if out := askCensusWithInput(t, paths, "1\n"); out != "" {
		t.Fatalf("interactive ask surfaced after uninstall: %q", out)
	}
	if out := captureStdout(t, func() { maybeEmitCensusSkillNotice(paths) }); out != "" {
		t.Fatalf("skill notice surfaced after uninstall: %q", out)
	}
	if exit := captureCensusCommandExit(t, func() int { return runCensusOn(paths) }); exit == 0 {
		t.Fatal("census on succeeded after uninstall")
	}
	if exit := captureCensusCommandExit(t, func() int { return runCensusOff(paths) }); exit == 0 {
		t.Fatal("census off recreated state after uninstall")
	}
	if result := sendCensusPingOnce(paths); result.Attempted {
		t.Fatalf("carrier attempted after uninstall: %+v", result)
	}
	if len(*payloads) != 0 {
		t.Fatalf("post-uninstall writer sweep sent %d requests", len(*payloads))
	}
	for _, path := range []string{paths.StateFile, paths.CensusFile, paths.ConfigDir, paths.CacheDir} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("post-uninstall writer sweep recreated %s (err=%v)", path, err)
		}
	}
}

func captureCensusCommandExit(t *testing.T, command func() int) int {
	t.Helper()
	exit := 0
	captureStdout(t, func() { exit = command() })
	return exit
}
