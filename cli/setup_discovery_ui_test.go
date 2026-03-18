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

	detectDefaultHAHostChoiceForSetup = func(cfg runtimeConfig) (string, bool) {
		return "ha-box.local", true
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
		"Found Home Assistant candidate: ha-box.local",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("discovery output missing %q:\n%s", want, rendered)
		}
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

	detectDefaultHAHostChoiceForSetup = func(cfg runtimeConfig) (string, bool) {
		return "192.168.1.123", true
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
	if !strings.Contains(output.String(), "Found Home Assistant candidate: 192.168.1.123") {
		t.Fatalf("missing discovery result:\n%s", output.String())
	}
}

func TestDetectDefaultHAHostWithFeedbackShowsSpinnerAfterDebounceForSlowTTYDiscovery(t *testing.T) {
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

	detectDefaultHAHostChoiceForSetup = func(cfg runtimeConfig) (string, bool) {
		time.Sleep(30 * time.Millisecond)
		return "192.168.1.124", true
	}
	writerSupportsTTYForSetup = func(out io.Writer) bool {
		return true
	}
	uiInputSupportsTTY = func() bool { return true }
	setupDiscoveryMinimumVisibleDuration = 10 * time.Millisecond

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
	if !strings.Contains(rendered, "Found Home Assistant candidate: 192.168.1.124") {
		t.Fatalf("missing discovery result:\n%s", rendered)
	}
}
