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
	if !strings.Contains(out, "already counted") {
		t.Fatalf("second census on should say the week is already counted:\n%s", out)
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

// The manual send path must heal a clock-rollback future stamp exactly like
// the weekly carrier: clamp, no send — never a second count for the same
// real week. All send paths share censusWeekSendable.
func TestCensusOnClampsFutureWeekStampLikeTheCarrier(t *testing.T) {
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	payloads := stubCensusTransport(t, http.StatusNoContent, nil)
	currentWeek := censusISOWeek(time.Now().UTC())
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
	if state := loadCensusState(paths); state.LastPingWeek != currentWeek {
		t.Fatalf("manual path must clamp like the carrier: got %q, want %q", state.LastPingWeek, currentWeek)
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
	if state := loadCensusState(paths); state.LastPingWeek != currentWeek {
		t.Fatalf("ask-yes path must clamp like the carrier: got %q, want %q", state.LastPingWeek, currentWeek)
	}
}

func TestCensusOnPingFailureLeavesWeekEmpty(t *testing.T) {
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
	if state.LastPingWeek != "" {
		t.Fatalf("failed ping must leave the week empty for a retry, got %q", state.LastPingWeek)
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
		"at most one ping per ISO week (UTC)",
		censusStatsURL(),
		"ha-nova census off",
		"HA_NOVA_NO_CENSUS",
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

func TestCensusOnFailedFirstPingFreesTheWeekForRetry(t *testing.T) {
	// A failed immediate ping must release the claimed week marker: without
	// that, the next update check would pass the week gate, lose the marker
	// claim, and silently skip — breaking the promised retry.
	paths := setupCensusTest(t)
	stubCensusVersion(t, "0.21.0")
	stubCensusTransport(t, 0, fmt.Errorf("endpoint down"))
	captureStdout(t, func() { runCensusCommand(paths, []string{"on"}) })

	week := censusISOWeek(censusNow().UTC())
	if _, err := os.Stat(filepath.Join(paths.CacheDir, "census-ping-"+week)); !os.IsNotExist(err) {
		t.Fatalf("failed first ping must release the week marker (stat err=%v)", err)
	}
	// The later carrier retry succeeds and stamps the week.
	payloads := stubCensusTransport(t, 204, nil)
	maybeCensusPing(paths)
	if got := len(*payloads); got != 1 {
		t.Fatalf("carrier retry attempts = %d, want 1", got)
	}
	if state := loadCensusState(paths); state.LastPingWeek != week {
		t.Fatalf("retry must stamp the week, got %q", state.LastPingWeek)
	}
}
