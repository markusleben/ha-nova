package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func teardownTestPreflight() uninstallPreflight {
	return uninstallPreflight{
		haURL:      "http://192.168.1.5:8123",
		relayToken: "test-relay-token",
		config: runtimeConfig{
			HAURL:        "http://192.168.1.5:8123",
			RelayBaseURL: "http://192.168.1.5:8791",
		},
	}
}

type teardownRecorder struct {
	opened       []string
	relayGone    bool
	healthProbes int
}

func (r *teardownRecorder) deps() teardownDeps {
	return teardownDeps{
		openURL: func(_ io.Writer, target string) { r.opened = append(r.opened, target) },
		relayHealth: func(_, _ string) ([]byte, error) {
			r.healthProbes++
			if r.relayGone {
				return nil, errors.New("connection refused")
			}
			return []byte(`{"ok":true}`), nil
		},
		probeHA: func(string) error { return nil },
		sleep:   func(time.Duration) {},
	}
}

func runTeardownWithInput(t *testing.T, input string, preflight uninstallPreflight, rec *teardownRecorder) (teardownOutcome, string) {
	t.Helper()
	out := &bytes.Buffer{}
	outcome, err := maybeOfferGuidedTeardown(bufio.NewReader(strings.NewReader(input)), out, preflight, rec.deps())
	if err != nil {
		t.Fatalf("maybeOfferGuidedTeardown() error: %v\noutput:\n%s", err, out.String())
	}
	return outcome, out.String()
}

func TestGuidedTeardownHappyPathOpensAllThreePages(t *testing.T) {
	rec := &teardownRecorder{relayGone: true}
	// offer yes, then Enter through: open app page, app removed, open store,
	// repo removed, open profile, token revoked.
	outcome, output := runTeardownWithInput(t, "y\n\n\n\n\n\n\n", teardownTestPreflight(), rec)

	if outcome != teardownCompleted {
		t.Fatalf("outcome = %v, want teardownCompleted\noutput:\n%s", outcome, output)
	}
	wantOpened := []string{
		haRelayAppPageURL("http://192.168.1.5:8123"),
		haAppStoreURL("http://192.168.1.5:8123"),
		haProfileSecurityURL("http://192.168.1.5:8123"),
	}
	if len(rec.opened) != len(wantOpened) {
		t.Fatalf("opened %d URLs (%v), want %d", len(rec.opened), rec.opened, len(wantOpened))
	}
	for i, want := range wantOpened {
		if rec.opened[i] != want {
			t.Fatalf("opened[%d] = %q, want %q", i, rec.opened[i], want)
		}
	}
	for _, want := range []string{
		"Step 1 of 3",
		"Step 2 of 3",
		"Step 3 of 3",
		"Relay no longer answers",
		"Long-lived access tokens",
		haNovaRepositoryURL,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected teardown output %q:\n%s", want, output)
		}
	}
}

func TestGuidedTeardownDeclinedLeavesEverythingAlone(t *testing.T) {
	rec := &teardownRecorder{}
	outcome, _ := runTeardownWithInput(t, "n\n", teardownTestPreflight(), rec)
	if outcome != teardownDeclined {
		t.Fatalf("outcome = %v, want teardownDeclined", outcome)
	}
	if len(rec.opened) != 0 {
		t.Fatalf("declined teardown must not open URLs, opened %v", rec.opened)
	}
}

func TestGuidedTeardownExitCancelsWholeUninstall(t *testing.T) {
	rec := &teardownRecorder{}
	outcome, _ := runTeardownWithInput(t, "y\nexit\n", teardownTestPreflight(), rec)
	if outcome != teardownCancelled {
		t.Fatalf("outcome = %v, want teardownCancelled", outcome)
	}
}

func TestGuidedTeardownBackReturnsToPreviousStep(t *testing.T) {
	rec := &teardownRecorder{relayGone: true}
	// offer yes → app step (Enter, Enter) → repo step: back → app step again
	// (Enter, Enter) → repo (Enter, Enter) → llat (Enter, Enter).
	outcome, output := runTeardownWithInput(t, "y\n\n\nback\n\n\n\n\n\n\n", teardownTestPreflight(), rec)
	if outcome != teardownCompleted {
		t.Fatalf("outcome = %v, want teardownCompleted\noutput:\n%s", outcome, output)
	}
	if got := strings.Count(output, "Step 1 of 3"); got != 2 {
		t.Fatalf("expected the app step to render twice after back-navigation, got %d\noutput:\n%s", got, output)
	}
}

func TestGuidedTeardownNotOfferedWithoutHAURL(t *testing.T) {
	rec := &teardownRecorder{}
	outcome, output := runTeardownWithInput(t, "", uninstallPreflight{}, rec)
	if outcome != teardownNotOffered {
		t.Fatalf("outcome = %v, want teardownNotOffered", outcome)
	}
	if output != "" {
		t.Fatalf("expected silent skip without HA URL, got:\n%s", output)
	}
}

func TestGuidedTeardownNotOfferedWhenHAUnreachable(t *testing.T) {
	rec := &teardownRecorder{}
	deps := rec.deps()
	deps.probeHA = func(string) error { return errors.New("dial tcp: timeout") }

	out := &bytes.Buffer{}
	outcome, err := maybeOfferGuidedTeardown(bufio.NewReader(strings.NewReader("")), out, teardownTestPreflight(), deps)
	if err != nil {
		t.Fatalf("maybeOfferGuidedTeardown() error: %v", err)
	}
	if outcome != teardownNotOffered {
		t.Fatalf("outcome = %v, want teardownNotOffered", outcome)
	}
	if !strings.Contains(out.String(), "not reachable right now") {
		t.Fatalf("expected unreachable notice, got:\n%s", out.String())
	}
}

func TestGuidedTeardownWarnsWhenRelayStillAnswers(t *testing.T) {
	rec := &teardownRecorder{relayGone: false}
	outcome, output := runTeardownWithInput(t, "y\n\n\n\n\n\n\n", teardownTestPreflight(), rec)
	if outcome != teardownCompleted {
		t.Fatalf("outcome = %v, want teardownCompleted\noutput:\n%s", outcome, output)
	}
	if !strings.Contains(output, "still answers") {
		t.Fatalf("expected still-answering warning:\n%s", output)
	}
	if rec.healthProbes != 3 {
		t.Fatalf("healthProbes = %d, want 3 (retry loop)", rec.healthProbes)
	}
}

func TestGuidedTeardownVerifySkippedWithoutToken(t *testing.T) {
	rec := &teardownRecorder{}
	preflight := teardownTestPreflight()
	preflight.relayToken = ""
	outcome, output := runTeardownWithInput(t, "y\n\n\n\n\n\n\n", preflight, rec)
	if outcome != teardownCompleted {
		t.Fatalf("outcome = %v, want teardownCompleted\noutput:\n%s", outcome, output)
	}
	if rec.healthProbes != 0 {
		t.Fatalf("healthProbes = %d, want 0 without a stored token", rec.healthProbes)
	}
}

func TestTeardownCompletedNoteLinesByMode(t *testing.T) {
	standard := teardownCompletedNoteLines(uninstallModeStandard)
	if len(standard) != 2 || !strings.Contains(standard[1], "--purge") {
		t.Fatalf("standard mode must hint at the kept config: %#v", standard)
	}
	purge := teardownCompletedNoteLines(uninstallModePurge)
	if len(purge) != 1 {
		t.Fatalf("purge mode needs no kept-config hint: %#v", purge)
	}
}

func TestPreflightNoteLinesAlwaysListServerSideCleanup(t *testing.T) {
	// With a known HA URL the checklist carries deep links.
	withURL := preflightNoteLines(uninstallPreflight{haURL: "http://ha.local:8123"})
	joined := strings.Join(withURL, "\n")
	for _, want := range []string{
		haRelayAppPageURL("http://ha.local:8123"),
		haAppStoreURL("http://ha.local:8123"),
		haProfileSecurityURL("http://ha.local:8123"),
		haNovaRepositoryURL,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected checklist to contain %q:\n%s", want, joined)
		}
	}

	// Without any config the checklist still prints with generic panel paths —
	// an unreachable relay is no evidence the server side is gone.
	generic := strings.Join(preflightNoteLines(uninstallPreflight{}), "\n")
	for _, want := range []string{
		"Settings > Apps > NOVA Relay > Uninstall",
		"Repositories",
		"Long-lived access tokens",
	} {
		if !strings.Contains(generic, want) {
			t.Fatalf("expected generic checklist to contain %q:\n%s", want, generic)
		}
	}
}
