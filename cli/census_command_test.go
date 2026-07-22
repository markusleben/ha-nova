package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
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
