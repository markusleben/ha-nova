package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCensusOnEnablesAndPingsSuccessStampsWeek(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)

	exit := 0
	out := captureStdout(t, func() { exit = runCensusCommand(paths, []string{"on"}) })
	if exit != 0 {
		t.Fatalf("census on exit = %d, want 0\n%s", exit, out)
	}
	state := loadCensusState(paths)
	if !state.Enabled || state.Answer != "yes" || state.AskedAt == "" || state.AskedVia != "command" {
		t.Fatalf("census on state: %+v", state)
	}
	if len(*payloads) != 1 {
		t.Fatalf("census on must ping immediately, got %d attempts", len(*payloads))
	}
	if state.LastPingWeek == "" {
		t.Fatal("successful immediate ping must stamp the week")
	}
	if !strings.Contains(out, `"schema":1`) {
		t.Fatalf("census on should echo the sent payload, got %q", out)
	}
}

func TestCensusOnTwiceInOneWeekSendsExactlyOnce(t *testing.T) {
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
		t.Fatalf("census on twice in one week must send exactly once, got %d", len(*payloads))
	}
	if !strings.Contains(out, "recorded ping attempt") {
		t.Fatalf("second census on should say the week already has a recorded attempt:\n%s", out)
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

// The manual send path must treat a clock-rollback future stamp exactly like
// the weekly carrier: suppress the send, keep the recorded week — never a
// second count for the same real week. All send paths share censusWeekSendable.
func TestCensusOnSuppressesFutureWeekStampLikeTheCarrier(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	futureWeek := censusISOWeek(time.Now().UTC().Add(21 * 24 * time.Hour))
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes", AskedAt: "2026-07-01T00:00:00Z", LastPingWeek: futureWeek}); err != nil {
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
	if state := loadCensusState(paths); state.LastPingWeek != futureWeek {
		t.Fatalf("manual path must keep the recorded week like the carrier: got %q, want %q", state.LastPingWeek, futureWeek)
	}

	// Same for the ask-yes immediate ping.
	if err := saveCensusState(paths, censusState{LastPingWeek: futureWeek}); err != nil {
		t.Fatalf("saveCensusState() error: %v", err)
	}
	stubCensusTTY(t, true, true)
	askCensusWithInput(t, paths, "y\n")
	if len(*payloads) != 0 {
		t.Fatalf("a future stamp must not let the ask-yes path send, got %d attempts", len(*payloads))
	}
	if state := loadCensusState(paths); state.LastPingWeek != futureWeek {
		t.Fatalf("ask-yes path must keep the recorded week like the carrier: got %q, want %q", state.LastPingWeek, futureWeek)
	}
}

func TestCensusOnPingFailureStampsWeekNoRetry(t *testing.T) {
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
	currentWeek := censusISOWeek(censusNow().UTC())
	if state.LastPingWeek != currentWeek {
		t.Fatalf("failed ping must stay stamped to prevent an ambiguous retry: got %q, want %q", state.LastPingWeek, currentWeek)
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

func TestCensusStatusPrintsLiteralWirePayloadAndURLs(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusEndpoint(t)

	exit := 0
	out := captureStdout(t, func() { exit = runCensusCommand(paths, []string{"status"}) })
	if exit != 0 {
		t.Fatalf("census status exit = %d, want 0\n%s", exit, out)
	}
	wire := fmt.Sprintf(`{"schema":1,"version":"0.21.0","os":%q}`, censusOS())
	for _, want := range []string{
		"Census: off",
		wire, // the LITERAL bytes, not a description
		"one recorded attempt per ISO week (UTC) while local census state remains intact",
		"Last attempted week: never",
		censusStatsURL(),
		"ha-nova census off",
		"HA_NOVA_NO_CENSUS",
		"not verified unique installs",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("census status missing %q:\n%s", want, out)
		}
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

func TestCensusOnFailedFirstPingKeepsTheWeekClaimed(t *testing.T) {
	// A failed immediate ping is ambiguous: the Worker may have counted the
	// request before the response was lost. Keep the locked state stamp so a
	// later update check cannot create a duplicate.
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))
	captureStdout(t, func() { runCensusCommand(paths, []string{"on"}) })

	week := censusISOWeek(censusNow().UTC())
	if state := loadCensusState(paths); state.LastPingWeek != week {
		t.Fatalf("failed first ping must keep the week stamp, got %q", state.LastPingWeek)
	}
	// A later carrier does not retry in the same week.
	payloads := stubCensusTransport(t, 204, nil)
	maybeCensusPing(paths)
	if got := len(*payloads); got != 0 {
		t.Fatalf("carrier retry attempts = %d, want 0", got)
	}
}

func TestCensusStatusReportsFutureWeekClockClamp(t *testing.T) {
	paths := setupCensusTest(t)
	current := censusISOWeek(censusNow().UTC())
	future := censusISOWeek(censusNow().UTC().Add(21 * 24 * time.Hour))
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes", LastPingWeek: future}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { runCensusStatus(paths) })
	want := fmt.Sprintf("Next possible ping: after recorded week %s (local clock is currently in %s)", future, current)
	if !strings.Contains(out, want) {
		t.Fatalf("future-week status missing %q:\n%s", want, out)
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
	out := finishBlockedCensusAsk(t, answer, done, "\n")
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
	if err := saveCensusState(paths, censusState{Enabled: true, Answer: "yes"}); err != nil {
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
	if out := askCensusWithInput(t, paths, "y\n"); out != "" {
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
