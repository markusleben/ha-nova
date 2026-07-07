package main

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestDetectDefaultHAHostWithFeedbackReportsResultWithoutTTYSpinner(t *testing.T) {
	originalDetect := detectDefaultHAHostChoiceForSetup
	defer func() {
		detectDefaultHAHostChoiceForSetup = originalDetect
	}()

	detectDefaultHAHostChoiceForSetup = func(cfg runtimeConfig) (string, string, bool) {
		return "ha-box.local", "", true
	}

	output := &strings.Builder{}
	host, discovered := detectDefaultHAHostWithFeedback(output, runtimeConfig{})

	if host != "ha-box.local" {
		t.Fatalf("host = %q, want %q", host, "ha-box.local")
	}
	if !discovered {
		t.Fatal("expected discovered=true")
	}
	rendered := output.String()
	for _, want := range []string{
		"Discovering Home Assistant on your network...",
		"Found Home Assistant via the network name ha-box.local",
		"this name can stop working",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("discovery output missing %q:\n%s", want, rendered)
		}
	}
}

func TestDetectDefaultHAHostWithFeedbackAsksForManualAddressWhenNothingWasConfirmed(t *testing.T) {
	originalDetect := detectDefaultHAHostChoiceForSetup
	defer func() {
		detectDefaultHAHostChoiceForSetup = originalDetect
	}()

	detectDefaultHAHostChoiceForSetup = func(cfg runtimeConfig) (string, string, bool) {
		return "", "", false
	}

	output := &strings.Builder{}
	host, discovered := detectDefaultHAHostWithFeedback(output, runtimeConfig{})

	if host != "" {
		t.Fatalf("host = %q, want blank", host)
	}
	if discovered {
		t.Fatal("expected discovered=false")
	}
	if !strings.Contains(output.String(), "enter the Home Assistant address manually") {
		t.Fatalf("missing manual entry guidance:\n%s", output.String())
	}
}

func TestDetectDefaultHAHostWithFeedbackDoesNotDelayFastTTYDiscovery(t *testing.T) {
	originalDetect := detectDefaultHAHostChoiceForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalMin := setupDiscoveryMinimumVisibleDuration
	defer func() {
		detectDefaultHAHostChoiceForSetup = originalDetect
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		setupDiscoveryMinimumVisibleDuration = originalMin
	}()

	detectDefaultHAHostChoiceForSetup = func(cfg runtimeConfig) (string, string, bool) {
		return "192.168.1.123", "homeassistant.local", true
	}
	writerSupportsTTYForSetup = func(out io.Writer) bool {
		return true
	}
	uiInputSupportsTTY = func() bool { return true }
	setupDiscoveryMinimumVisibleDuration = 40 * time.Millisecond

	output := &strings.Builder{}
	start := time.Now()
	host, discovered := detectDefaultHAHostWithFeedback(output, runtimeConfig{})
	elapsed := time.Since(start)

	if host != "192.168.1.123" {
		t.Fatalf("host = %q, want %q", host, "192.168.1.123")
	}
	if !discovered {
		t.Fatal("expected discovered=true")
	}
	if elapsed >= 40*time.Millisecond {
		t.Fatalf("elapsed = %s, want less than %s for a fast task", elapsed, 40*time.Millisecond)
	}
	if !strings.Contains(output.String(), "Found Home Assistant at 192.168.1.123 (discovered via homeassistant.local)") {
		t.Fatalf("missing discovery result:\n%s", output.String())
	}
}

func TestDetectDefaultHAHostWithFeedbackShowsSpinnerAfterDebounceForSlowTTYDiscovery(t *testing.T) {
	originalDetect := detectDefaultHAHostChoiceForSetup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	originalEnv := uiEnvLookup
	originalMin := setupDiscoveryMinimumVisibleDuration
	originalTimeout := setupDiscoveryOverallTimeout
	defer func() {
		detectDefaultHAHostChoiceForSetup = originalDetect
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
		uiEnvLookup = originalEnv
		setupDiscoveryMinimumVisibleDuration = originalMin
		setupDiscoveryOverallTimeout = originalTimeout
	}()

	detectDefaultHAHostChoiceForSetup = func(cfg runtimeConfig) (string, string, bool) {
		time.Sleep(1200 * time.Millisecond)
		return "192.168.1.124", "", true
	}
	writerSupportsTTYForSetup = func(out io.Writer) bool {
		return true
	}
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	uiEnvLookup = func(string) string { return "" }
	setupDiscoveryMinimumVisibleDuration = 10 * time.Millisecond
	setupDiscoveryOverallTimeout = 2 * time.Second

	output := &strings.Builder{}
	host, discovered := detectDefaultHAHostWithFeedback(output, runtimeConfig{})

	if host != "192.168.1.124" {
		t.Fatalf("host = %q, want %q", host, "192.168.1.124")
	}
	if !discovered {
		t.Fatal("expected discovered=true")
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
