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
		"Found Home Assistant as ha-box.local",
		"This name can stop working",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("discovery output missing %q:\n%s", want, output.String())
		}
	}
}

// Lane D3: one Home Assistant found by IP is asked about, never taken
// silently (a test HA, a neighbour's, or one over a VPN may answer).
func TestSelectDefaultHAHostWithFeedbackConfirmsASingleFoundAddress(t *testing.T) {
	originalDiscover := discoverReachableHAHostsForSetup
	t.Cleanup(func() { discoverReachableHAHostsForSetup = originalDiscover })
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return []setupDiscoveryCandidate{{Host: "10.85.1.2", HAURL: "http://10.85.1.2:8123", Source: "mDNS"}}, ""
	}
	for _, tc := range []struct {
		input    string
		selected bool
		host     string
	}{
		{"\n", true, "10.85.1.2"}, // Enter keeps the found address
		{"2\n", false, ""},        // "Enter a different address"
	} {
		output := &strings.Builder{}
		candidate, selected, err := selectDefaultHAHostWithFeedback(bufio.NewReader(strings.NewReader(tc.input)), output, runtimeConfig{})
		if err != nil || selected != tc.selected || candidate.Host != tc.host {
			t.Fatalf("input %q: (%+v, %v, %v)", tc.input, candidate, selected, err)
		}
		if !strings.Contains(output.String(), "Choose your Home Assistant:") || !strings.Contains(output.String(), "1) 10.85.1.2 (mDNS)") {
			t.Fatalf("input %q: no question asked:\n%s", tc.input, output.String())
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
	if !strings.Contains(output.String(), "Starting from your saved address saved-ha.local") {
		t.Fatalf("missing manual entry guidance:\n%s", output.String())
	}
}

func TestSelectDefaultHAHostWithFeedbackListsEveryReachableInstanceAndSource(t *testing.T) {
	originalDiscover := discoverReachableHAHostsForSetup
	t.Cleanup(func() { discoverReachableHAHostsForSetup = originalDiscover })
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return []setupDiscoveryCandidate{
			{Host: "192.168.1.20", HAURL: "http://192.168.1.20:8123", Via: "homeassistant.local", Source: "mDNS"},
			{Host: "192.168.1.30", HAURL: "https://192.168.1.30", Source: "mDNS"},
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
		"192.168.1.30 (mDNS)",
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
		{Host: "192.168.1.30", Source: "mDNS"},
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
	candidate, selected, err := selectDefaultHAHostWithFeedback(bufio.NewReader(strings.NewReader("1\n")), output, runtimeConfig{})
	elapsed := time.Since(start)
	if err != nil || !selected || candidate.Host != "192.168.1.123" {
		t.Fatalf("selection = (%+v, %v), err = %v", candidate, selected, err)
	}
	if elapsed >= 40*time.Millisecond {
		t.Fatalf("elapsed = %s, want less than %s for a fast task", elapsed, 40*time.Millisecond)
	}
	if !strings.Contains(output.String(), "Found Home Assistant at 192.168.1.123 (homeassistant.local)") {
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
		return []setupDiscoveryCandidate{{Host: "192.168.1.124", HAURL: "http://192.168.1.124:8123", Source: "mDNS"}}, ""
	}
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	uiEnvLookup = func(string) string { return "" }
	setupDiscoveryMinimumVisibleDuration = 10 * time.Millisecond
	setupDiscoveryOverallTimeout = 2 * time.Second

	output := &strings.Builder{}
	candidate, selected, err := selectDefaultHAHostWithFeedback(bufio.NewReader(strings.NewReader("1\n")), output, runtimeConfig{})
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

// rc2 lane D4: the mDNS search is the only non-loopback listener; the first
// Windows run explains the firewall prompt it raises, later runs stay quiet.
func TestSetupDiscoveryExplainsWindowsFirewallPromptOnFirstRun(t *testing.T) {
	originalDiscover, originalOS := discoverReachableHAHostsForSetup, setupDiscoveryPlatformOS
	t.Cleanup(func() { discoverReachableHAHostsForSetup, setupDiscoveryPlatformOS = originalDiscover, originalOS })
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) { return nil, "" }
	originalWindow := windowAvailable
	t.Cleanup(func() { windowAvailable = originalWindow })

	for _, tc := range []struct {
		os   string
		cfg  runtimeConfig
		want bool
	}{
		{"windows", runtimeConfig{}, true},
		{"windows", runtimeConfig{HAURL: "http://192.168.1.5:8123"}, false},
		{"darwin", runtimeConfig{}, false},
		{"windows-ssh", runtimeConfig{}, false},
	} {
		desktop := tc.os != "windows-ssh" // SSH runs in session 0: no window, no prompt
		windowAvailable = func() bool { return desktop }
		if !desktop {
			tc.os = "windows"
		}
		setupDiscoveryPlatformOS = tc.os
		output := &strings.Builder{}
		runSetupDiscoveryWithFeedback(output, tc.cfg)
		if got := strings.Contains(output.String(), `Windows may now ask whether "ha-nova" may use your network`); got != tc.want {
			t.Fatalf("%s %+v: firewall note shown=%v, want %v:\n%s", tc.os, tc.cfg, got, tc.want, output.String())
		}
	}
}
