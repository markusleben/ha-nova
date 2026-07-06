package main

import (
	"bufio"
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestVerifySetupConnectionOnceKeepsTransportFailureGeneric(t *testing.T) {
	originalProbeHTTP := probeHTTPForSetup
	originalFetchRelayHealth := fetchRelayHealthForSetup
	originalProbeRelayWSPing := probeRelayWSPingForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
		fetchRelayHealthForSetup = originalFetchRelayHealth
		probeRelayWSPingForSetup = originalProbeRelayWSPing
	}()

	probeHTTPForSetup = func(string) error { return nil }
	fetchRelayHealthForSetup = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForSetup = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{}, errors.New("upstream unavailable")
	}

	output := &bytes.Buffer{}
	_, issue, ok := verifySetupConnectionOnce(output, runtimeConfig{
		HAURL:        "http://ha",
		RelayBaseURL: "http://relay",
	}, "token")
	if ok {
		t.Fatal("did not expect ready state")
	}
	if issue != setupIssueWSDegraded {
		t.Fatalf("expected ws degraded issue, got %q", issue)
	}
	if strings.Contains(output.String(), "Home Assistant Access Token") {
		t.Fatalf("expected generic transport guidance, got:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Verify the app settings and restart the App if needed.") {
		t.Fatalf("expected generic troubleshooting guidance, got:\n%s", output.String())
	}
}

func TestVerifySetupConnectionReuseTokenLLATIssueOffersRepairActions(t *testing.T) {
	originalProbeHTTP := probeHTTPForSetup
	originalFetchRelayHealth := fetchRelayHealthForSetup
	originalProbeRelayWSPing := probeRelayWSPingForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
		fetchRelayHealthForSetup = originalFetchRelayHealth
		probeRelayWSPingForSetup = originalProbeRelayWSPing
	}()

	probeHTTPForSetup = func(string) error { return nil }
	fetchRelayHealthForSetup = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForSetup = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: 502, Body: []byte("LLAT is required")}, nil
	}

	output := &bytes.Buffer{}
	issue, ok, err := verifySetupConnection(bufio.NewReader(strings.NewReader("back\n")), output, runtimeConfig{
		HAURL:        "http://ha",
		RelayBaseURL: "http://relay",
	}, "token", true, true)
	if err != errSetupBack {
		t.Fatalf("expected errSetupBack, got %v", err)
	}
	if ok {
		t.Fatal("did not expect ready state")
	}
	if issue != setupIssueWSDegraded {
		t.Fatalf("expected ws degraded issue, got %q", issue)
	}
	for _, want := range []string{
		"This device's Relay Auth Token worked.",
		"Only the Home Assistant access token still needs attention.",
		"Open Home Assistant Security page",
		"Open NOVA Relay settings",
		"Retry now",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("expected repair guidance %q in output:\n%s", want, output.String())
		}
	}
}

func TestVerifySetupConnectionReuseTokenRelayAuthIssueCanRouteBackToTokenStep(t *testing.T) {
	originalProbeHTTP := probeHTTPForSetup
	originalFetchRelayHealth := fetchRelayHealthForSetup
	originalProbeRelayWSPing := probeRelayWSPingForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
		fetchRelayHealthForSetup = originalFetchRelayHealth
		probeRelayWSPingForSetup = originalProbeRelayWSPing
	}()

	probeHTTPForSetup = func(string) error { return nil }
	fetchRelayHealthForSetup = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForSetup = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: 401, Body: []byte("unauthorized")}, nil
	}

	output := &bytes.Buffer{}
	issue, ok, err := verifySetupConnection(bufio.NewReader(strings.NewReader("1\n")), output, runtimeConfig{
		HAURL:        "http://ha",
		RelayBaseURL: "http://relay",
	}, "token", true, true)
	if err != errSetupRelayTokenStep {
		t.Fatalf("expected errSetupRelayTokenStep, got %v", err)
	}
	if ok {
		t.Fatal("did not expect ready state")
	}
	if issue != setupIssueWSDegraded {
		t.Fatalf("expected ws degraded issue, got %q", issue)
	}
	if !strings.Contains(output.String(), "Back to Relay token step") {
		t.Fatalf("expected relay-token repair choice in output:\n%s", output.String())
	}
}

func TestVerifySetupConnectionReuseTokenConnectionIssueCanRouteToHostStep(t *testing.T) {
	originalProbeHTTP := probeHTTPForSetup
	originalFetchRelayHealth := fetchRelayHealthForSetup
	originalProbeRelayWSPing := probeRelayWSPingForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
		fetchRelayHealthForSetup = originalFetchRelayHealth
		probeRelayWSPingForSetup = originalProbeRelayWSPing
	}()

	probeHTTPForSetup = func(string) error { return errors.New("no such host") }
	fetchRelayHealthForSetup = func(string, string) ([]byte, error) {
		return nil, errors.New("unreachable")
	}
	probeRelayWSPingForSetup = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{}, nil
	}

	output := &bytes.Buffer{}
	issue, ok, err := verifySetupConnection(bufio.NewReader(strings.NewReader("2\n")), output, runtimeConfig{
		HAURL:        "http://homeassistant.local:8123",
		RelayBaseURL: "http://homeassistant.local:8791",
	}, "token", true, true)
	if err != errSetupHostStep {
		t.Fatalf("expected errSetupHostStep, got %v", err)
	}
	if ok {
		t.Fatal("did not expect ready state")
	}
	if issue != setupIssueRelayUnreachable {
		t.Fatalf("expected relay unreachable issue, got %q", issue)
	}
	for _, want := range []string{
		"Change Home Assistant address",
		`names like "homeassistant.local" can stop working`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("expected %q in output:\n%s", want, output.String())
		}
	}
}

func TestVerifySetupConnectionReuseTokenRelayUnreachableKeepsRepairCopyTruthful(t *testing.T) {
	originalProbeHTTP := probeHTTPForSetup
	originalFetchRelayHealth := fetchRelayHealthForSetup
	originalProbeRelayWSPing := probeRelayWSPingForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
		fetchRelayHealthForSetup = originalFetchRelayHealth
		probeRelayWSPingForSetup = originalProbeRelayWSPing
	}()

	probeHTTPForSetup = func(string) error { return nil }
	fetchRelayHealthForSetup = func(string, string) ([]byte, error) {
		return nil, errors.New("connect: connection refused")
	}
	probeRelayWSPingForSetup = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{}, nil
	}

	output := &bytes.Buffer{}
	issue, ok, err := verifySetupConnection(bufio.NewReader(strings.NewReader("back\n")), output, runtimeConfig{
		HAURL:        "http://ha",
		RelayBaseURL: "http://relay",
	}, "token", true, true)
	if err != errSetupBack {
		t.Fatalf("expected errSetupBack, got %v", err)
	}
	if ok {
		t.Fatal("did not expect ready state")
	}
	if issue != setupIssueRelayUnreachable {
		t.Fatalf("expected relay unreachable issue, got %q", issue)
	}
	if strings.Contains(output.String(), "Home Assistant and NOVA Relay are reachable.") {
		t.Fatalf("did not expect reachable fallback copy in output:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Home Assistant or NOVA Relay is still unreachable from this device.") {
		t.Fatalf("expected truthful connection repair copy in output:\n%s", output.String())
	}
}

func TestVerifySetupConnectionReuseTokenAmbiguousIssueUsesFallbackRepairPage(t *testing.T) {
	originalProbeHTTP := probeHTTPForSetup
	originalFetchRelayHealth := fetchRelayHealthForSetup
	originalProbeRelayWSPing := probeRelayWSPingForSetup
	defer func() {
		probeHTTPForSetup = originalProbeHTTP
		fetchRelayHealthForSetup = originalFetchRelayHealth
		probeRelayWSPingForSetup = originalProbeRelayWSPing
	}()

	probeHTTPForSetup = func(string) error { return nil }
	fetchRelayHealthForSetup = func(string, string) ([]byte, error) {
		return []byte(`{"status":"ok","data":{"ha_ws_connected":false}}`), nil
	}
	probeRelayWSPingForSetup = func(string, string) (relayWSPingResponse, error) {
		return relayWSPingResponse{StatusCode: 502, Body: []byte("upstream unavailable")}, nil
	}

	output := &bytes.Buffer{}
	issue, ok, err := verifySetupConnection(bufio.NewReader(strings.NewReader("back\n")), output, runtimeConfig{
		HAURL:        "http://ha",
		RelayBaseURL: "http://relay",
	}, "token", true, true)
	if err != errSetupBack {
		t.Fatalf("expected errSetupBack, got %v", err)
	}
	if ok {
		t.Fatal("did not expect ready state")
	}
	if issue != setupIssueWSDegraded {
		t.Fatalf("expected ws degraded issue, got %q", issue)
	}
	for _, want := range []string{
		"Home Assistant and NOVA Relay are reachable.",
		"Setup still needs one more app-side fix before this device can finish connecting.",
		"Open Home Assistant Security page",
		"Open NOVA Relay settings",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("expected ambiguous repair guidance %q in output:\n%s", want, output.String())
		}
	}
}
