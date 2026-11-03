package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Commands that make several Relay calls or wait on one
// (docs/work/2026-11-03-link-route-timeouts-spec.md).

func stubLocalRelaySelection(t *testing.T, delay time.Duration, answer func(*http.Request) (*http.Response, error)) *int {
	t.Helper()
	selections := 0
	previous := resolveLocalRelayTransportForCLI
	resolveLocalRelayTransportForCLI = func(context.Context, runtimeConfig) (relayTransportSelection, error) {
		selections++
		time.Sleep(delay)
		return relayTransportSelection{
			BaseURL:    "http://relay.invalid",
			Client:     &http.Client{Transport: roundTripFunc(answer)},
			Credential: "device-credential",
			DeviceMode: true,
			Via:        relayViaLocal,
		}, nil
	}
	t.Cleanup(func() { resolveLocalRelayTransportForCLI = previous })
	return &selections
}

// A trace command selects its route (on Link: logs in) once, not per call.
func TestTraceSelectsItsRouteOnce(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	selections := stubLocalRelaySelection(t, 0, func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		data := `{"context":{"id":"c1"},"run_id":"r1","timestamp":{"start":"2026-11-03T10:00:00Z"}}`
		switch {
		case bytes.Contains(body, []byte("entity_registry/get")):
			data = `{"unique_id":"u1"}`
		case bytes.Contains(body, []byte("trace/list")):
			data = `[{"run_id":"r1","timestamp":{"start":"2026-11-03T10:00:00Z"}}]`
		}
		return relayAnswer(http.StatusOK, `{"ok":true,"data":`+data+`}`), nil
	})
	if _, err := loadLatestTrace(paths, "automation.hall", "automation"); err != nil {
		t.Fatal(err)
	}
	if *selections != 1 {
		t.Fatalf("route selections = %d, want 1 for three calls", *selections)
	}
}

// relay ws|core: the request gets its full time after the route is chosen,
// so a 10 s collect_events window still fits after a slow Link login.
func TestRelayRequestGetsItsOwnTimeAfterTheRouteIsChosen(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	remaining := make(chan time.Duration, 1)
	stubLocalRelaySelection(t, time.Second, func(request *http.Request) (*http.Response, error) {
		deadline, _ := request.Context().Deadline()
		remaining <- time.Until(deadline)
		return relayAnswer(http.StatusOK, `{"ok":true,"data":{}}`), nil
	})
	exit, output := captureCommandOutput(t, func() int {
		return runRelayProxy(paths, "ws", []string{"--data", `{"type":"ping"}`})
	})
	if exit != 0 {
		t.Fatalf("relay ws exit = %d: %s", exit, strings.TrimSpace(output))
	}
	full := time.Duration(defaultRelayMaxTimeSeconds * float64(time.Second))
	if got := <-remaining; got < full-500*time.Millisecond {
		t.Fatalf("request time left after a 1 s route choice = %s, want about %s", got, full)
	}
}
