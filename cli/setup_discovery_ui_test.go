package main

import (
	"bufio"
	"io"
	"strings"
	"testing"
	"time"
)

func TestSelectDefaultHAHostWithFeedbackKeepsOverridePromptForSingleLocalResult(t *testing.T) {
	originalDiscover := discoverReachableHAHostsForSetup
	t.Cleanup(func() { discoverReachableHAHostsForSetup = originalDiscover })
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return []setupDiscoveryCandidate{{
			Host:   "ha-box.local",
			HAURL:  "http://ha-box.local:8123",
			Source: "mDNS",
		}}, ""
	}

	output := &strings.Builder{}
	candidate, selected, err := selectDefaultHAHostWithFeedback(bufio.NewReader(strings.NewReader("")), output, runtimeConfig{})
	if err != nil {
		t.Fatalf("selectDefaultHAHostWithFeedback() error = %v", err)
	}
	if selected || candidate.Host != "ha-box.local" || candidate.HAURL != "http://ha-box.local:8123" {
		t.Fatalf("selection = (%+v, %v), want unconfirmed .local default", candidate, selected)
	}
	for _, want := range []string{
		"Discovering Home Assistant on your network...",
		"Found Home Assistant via the network name ha-box.local",
		"this name can stop working",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("discovery output missing %q:\n%s", want, output.String())
		}
	}
}

func TestSelectDefaultHAHostWithFeedbackKeepsManualPathWhenNothingWasConfirmed(t *testing.T) {
	originalDiscover := discoverReachableHAHostsForSetup
	t.Cleanup(func() { discoverReachableHAHostsForSetup = originalDiscover })
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return nil, "saved-ha.local"
	}

	output := &strings.Builder{}
	candidate, selected, err := selectDefaultHAHostWithFeedback(bufio.NewReader(strings.NewReader("")), output, runtimeConfig{})
	if err != nil {
		t.Fatalf("selectDefaultHAHostWithFeedback() error = %v", err)
	}
	if selected || candidate.Host != "saved-ha.local" {
		t.Fatalf("selection = (%+v, %v), want saved manual default", candidate, selected)
	}
	if !strings.Contains(output.String(), "using your saved address as a starting point: saved-ha.local") {
		t.Fatalf("missing manual entry guidance:\n%s", output.String())
	}
}

func TestSelectDefaultHAHostWithFeedbackListsEveryReachableInstanceAndSource(t *testing.T) {
	originalDiscover := discoverReachableHAHostsForSetup
	t.Cleanup(func() { discoverReachableHAHostsForSetup = originalDiscover })
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return []setupDiscoveryCandidate{
			{Host: "192.168.1.20", HAURL: "http://192.168.1.20:8123", Via: "homeassistant.local", Source: "mDNS"},
			{Host: "192.168.1.30", HAURL: "https://192.168.1.30", Source: "local network cache"},
		}, ""
	}

	output := &strings.Builder{}
	candidate, selected, err := selectDefaultHAHostWithFeedback(bufio.NewReader(strings.NewReader("2\n")), output, runtimeConfig{})
	if err != nil {
		t.Fatalf("selectDefaultHAHostWithFeedback() error = %v", err)
	}
	if !selected || candidate.Host != "192.168.1.30" || candidate.HAURL != "https://192.168.1.30" {
		t.Fatalf("selection = (%+v, %v), want second discovered host", candidate, selected)
	}
	for _, want := range []string{
		"Found 2 reachable Home Assistant instances",
		"192.168.1.20 (mDNS via homeassistant.local)",
		"192.168.1.30 (local network cache)",
		"Enter a different address",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("pick list missing %q:\n%s", want, output.String())
		}
	}
}

func TestPromptSetupDiscoveryCandidateSkipsStaleEnterBeforeDefault(t *testing.T) {
	clearSetupNextPromptSkipsStaleBlankInput()
	t.Cleanup(clearSetupNextPromptSkipsStaleBlankInput)
	armSetupNextPromptSkipsStaleBlankInput()

	candidates := []setupDiscoveryCandidate{
		{Host: "192.168.1.20", Source: "mDNS"},
		{Host: "192.168.1.30", Source: "local network cache"},
	}
	output := &strings.Builder{}
	answer, err := promptSetupDiscoveryCandidateFromReader(
		bufio.NewReader(strings.NewReader("\n2\n")),
		output,
		candidates,
	)
	if err != nil {
		t.Fatalf("promptSetupDiscoveryCandidateFromReader() error = %v", err)
	}
	if answer != "1" {
		t.Fatalf("answer = %q, want second candidate", answer)
	}
	if strings.Count(output.String(), "Choose your Home Assistant:") != 2 {
		t.Fatalf("expected picker to be re-rendered after stale Enter:\n%s", output.String())
	}
}

func TestSelectDefaultHAHostWithFeedbackDoesNotDelayFastTTYDiscovery(t *testing.T) {
	originalDiscover := discoverReachableHAHostsForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalMin := setupDiscoveryMinimumVisibleDuration
	t.Cleanup(func() {
		discoverReachableHAHostsForSetup = originalDiscover
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		setupDiscoveryMinimumVisibleDuration = originalMin
	})
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return []setupDiscoveryCandidate{{Host: "192.168.1.123", HAURL: "http://192.168.1.123:8123", Via: "homeassistant.local", Source: "mDNS"}}, ""
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	setupDiscoveryMinimumVisibleDuration = 40 * time.Millisecond

	output := &strings.Builder{}
	start := time.Now()
	candidate, selected, err := selectDefaultHAHostWithFeedback(bufio.NewReader(strings.NewReader("")), output, runtimeConfig{})
	elapsed := time.Since(start)
	if err != nil || !selected || candidate.Host != "192.168.1.123" {
		t.Fatalf("selection = (%+v, %v), err = %v", candidate, selected, err)
	}
	if elapsed >= 40*time.Millisecond {
		t.Fatalf("elapsed = %s, want less than %s for a fast task", elapsed, 40*time.Millisecond)
	}
	if !strings.Contains(output.String(), "Found Home Assistant at 192.168.1.123 (discovered via homeassistant.local)") {
		t.Fatalf("missing discovery result:\n%s", output.String())
	}
}

func TestSelectDefaultHAHostWithFeedbackShowsSpinnerAfterDebounceForSlowTTYDiscovery(t *testing.T) {
	originalDiscover := discoverReachableHAHostsForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	originalEnv := uiEnvLookup
	originalMin := setupDiscoveryMinimumVisibleDuration
	originalTimeout := setupDiscoveryOverallTimeout
	t.Cleanup(func() {
		discoverReachableHAHostsForSetup = originalDiscover
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
		uiEnvLookup = originalEnv
		setupDiscoveryMinimumVisibleDuration = originalMin
		setupDiscoveryOverallTimeout = originalTimeout
	})
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		time.Sleep(1200 * time.Millisecond)
		return []setupDiscoveryCandidate{{Host: "192.168.1.124", HAURL: "http://192.168.1.124:8123", Source: "local network cache"}}, ""
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	uiEnvLookup = func(string) string { return "" }
	setupDiscoveryMinimumVisibleDuration = 10 * time.Millisecond
	setupDiscoveryOverallTimeout = 2 * time.Second

	output := &strings.Builder{}
	candidate, selected, err := selectDefaultHAHostWithFeedback(bufio.NewReader(strings.NewReader("")), output, runtimeConfig{})
	if err != nil || !selected || candidate.Host != "192.168.1.124" {
		t.Fatalf("selection = (%+v, %v), err = %v", candidate, selected, err)
	}
	rendered := output.String()
	if !strings.Contains(rendered, "Discovering Home Assistant on your network...") {
		t.Fatalf("missing discovery spinner label:\n%s", rendered)
	}
	if !strings.Contains(rendered, "2s left") || !strings.Contains(rendered, "1s left") {
		t.Fatalf("missing countdown label updates:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Found Home Assistant at 192.168.1.124") {
		t.Fatalf("missing discovery result:\n%s", rendered)
	}
}
