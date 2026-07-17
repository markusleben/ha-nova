package main

import (
	"errors"
	"net/http"
	"testing"
)

func TestCheckRelayReadinessAcceptsWSPingSuccess(t *testing.T) {
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()

	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusOK, Body: []byte(`{"type":"pong"}`)}, nil
	}

	readiness := checkRelayReadiness("http://relay", "token")
	if !readiness.RelayReachable {
		t.Fatal("expected relay to be reachable")
	}
	if !readiness.UsedWSPing {
		t.Fatal("expected ws ping fallback to be used")
	}
	if !readiness.WSReady {
		t.Fatal("expected ws ping success to mark readiness as ready")
	}
	if readiness.UpstreamAuthIssue || readiness.RelayAuthIssue {
		t.Fatalf("unexpected issue flags: %+v", readiness)
	}
}

func TestCheckRelayReadinessMarksUpstreamAuthIssue(t *testing.T) {
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()

	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: http.StatusBadGateway, Body: []byte("LLAT is required")}, nil
	}

	readiness := checkRelayReadiness("http://relay", "token")
	if readiness.WSReady {
		t.Fatal("did not expect ready state")
	}
	if !readiness.UpstreamAuthIssue {
		t.Fatalf("expected upstream auth issue flag: %+v", readiness)
	}
}

func TestCheckRelayReadinessKeepsGenericWSFailureGeneric(t *testing.T) {
	originalHealth := fetchRelayHealthForReadiness
	originalWSPing := probeRelayWSPingForReadiness
	defer func() {
		fetchRelayHealthForReadiness = originalHealth
		probeRelayWSPingForReadiness = originalWSPing
	}()

	fetchRelayHealthForReadiness = func(relayBaseURL, token string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForReadiness = func(relayBaseURL, token string) (relayWSPingResponse, error) {
		return relayWSPingResponse{}, errors.New("upstream unavailable")
	}

	readiness := checkRelayReadiness("http://relay", "token")
	if readiness.WSReady {
		t.Fatal("did not expect ready state")
	}
	if readiness.UpstreamAuthIssue || readiness.RelayAuthIssue {
		t.Fatalf("expected generic failure only: %+v", readiness)
	}
	if readiness.WSPingErr == nil {
		t.Fatal("expected ws ping error to be recorded")
	}
}
