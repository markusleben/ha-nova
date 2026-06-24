package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type stubSetupMenuRunner struct {
	answer string
	err    error
	calls  int
}

func (s *stubSetupMenuRunner) Run(_ io.Writer, _ setupMenuSpec) (string, error) {
	s.calls++
	return s.answer, s.err
}

func TestPromptSetupClientInteractiveUsesEnhancedMenuWhenAvailable(t *testing.T) {
	originalEnv := uiEnvLookup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	originalRunner := interactiveSetupMenuRunner
	defer func() {
		uiEnvLookup = originalEnv
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
		interactiveSetupMenuRunner = originalRunner
	}()

	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	runner := &stubSetupMenuRunner{answer: "antigravity"}
	interactiveSetupMenuRunner = runner

	got, err := promptSetupClientInteractive(bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, testSetupClientChoices(), "claude")
	if err != nil {
		t.Fatalf("promptSetupClientInteractive() error: %v", err)
	}
	if got != "antigravity" {
		t.Fatalf("promptSetupClientInteractive() = %q, want antigravity", got)
	}
	if runner.calls != 1 {
		t.Fatalf("menu runner calls = %d, want 1", runner.calls)
	}
}

func TestPromptSetupClientInteractiveFallsBackWhenEnhancedMenuUnavailable(t *testing.T) {
	originalEnv := uiEnvLookup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	originalRunner := interactiveSetupMenuRunner
	defer func() {
		uiEnvLookup = originalEnv
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
		interactiveSetupMenuRunner = originalRunner
	}()

	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	runner := &stubSetupMenuRunner{err: errSetupMenuUnavailable}
	interactiveSetupMenuRunner = runner

	got, err := promptSetupClientInteractive(bufio.NewReader(strings.NewReader("1\n")), &bytes.Buffer{}, testSetupClientChoices(), "claude")
	if err != nil {
		t.Fatalf("promptSetupClientInteractive() error: %v", err)
	}
	if got != "claude" {
		t.Fatalf("promptSetupClientInteractive() = %q, want claude", got)
	}
}

func TestPromptSetupClientInteractiveReturnsRealMenuErrors(t *testing.T) {
	originalEnv := uiEnvLookup
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalANSI := uiOutputSupportsANSI
	originalRunner := interactiveSetupMenuRunner
	defer func() {
		uiEnvLookup = originalEnv
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiOutputSupportsANSI = originalANSI
		interactiveSetupMenuRunner = originalRunner
	}()

	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	wantErr := errors.New("boom")
	interactiveSetupMenuRunner = &stubSetupMenuRunner{err: wantErr}

	_, err := promptSetupClientInteractive(bufio.NewReader(strings.NewReader("")), &bytes.Buffer{}, testSetupClientChoices(), "claude")
	if !errors.Is(err, wantErr) {
		t.Fatalf("promptSetupClientInteractive() error = %v, want %v", err, wantErr)
	}
}

func TestRenderSetupMenuBlockUsesCRLFForRawTerminalLayout(t *testing.T) {
	output := &bytes.Buffer{}

	lines := renderSetupMenuBlock(output, uiSession{mode: uiModeEnhanced}, setupMenuSpec{
		Title:        "Which AI client do you use?",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Ctrl+C to exit",
		DefaultValue: "claude",
		Options: []setupMenuOption{
			{Value: "claude", Label: "Claude Code"},
			{Value: "antigravity", Label: "Google Antigravity CLI"},
		},
	}, 0)

	if lines == 0 {
		t.Fatal("expected rendered menu lines")
	}
	rendered := output.String()
	if !strings.Contains(rendered, "\r\n") {
		t.Fatalf("expected CRLF in rendered menu output, got %q", rendered)
	}
	if strings.Contains(rendered, "\n  Which AI client do you use?\n") {
		t.Fatalf("expected raw-menu rendering to avoid bare LF-only layout, got %q", rendered)
	}
}

func TestRenderSetupMenuBlockUsesCompactLayoutAndSecondLineDisabledReason(t *testing.T) {
	output := &bytes.Buffer{}

	renderSetupMenuBlock(output, uiSession{mode: uiModeEnhanced, color: true}, setupMenuSpec{
		Title:  "Which AI client do you use?",
		Prompt: "Use ↑/↓ or j/k, Enter to select, Ctrl+C to exit",
		Options: []setupMenuOption{
			{Value: "claude", Label: "Claude Code"},
			{Value: "codex", Label: "Codex CLI", Disabled: true, DisabledReason: "install Codex CLI first"},
		},
	}, 0)

	rendered := output.String()
	if !strings.Contains(rendered, "Which AI client do you use?") || !strings.Contains(rendered, "\r\n\r\n  ") {
		t.Fatalf("expected compact menu title block with a single blank separator, got %q", rendered)
	}
	if !strings.Contains(rendered, "not available: install Codex CLI first") {
		t.Fatalf("expected disabled reason on second line, got %q", rendered)
	}
}

func TestSetupMenuBlockWidthAccountsForDisabledReasonAndPrompt(t *testing.T) {
	width := setupMenuBlockWidth(setupMenuSpec{
		Title:  "Which AI client do you use?",
		Prompt: "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit",
		Options: []setupMenuOption{
			{Value: "claude", Label: "Claude Code"},
			{Value: "codex", Label: "Codex CLI", Disabled: true, DisabledReason: "install Codex CLI first"},
		},
	})
	if width < len("      not available: install Codex CLI first") {
		t.Fatalf("expected width to include disabled reason, got %d", width)
	}
}

func TestSetupMenuFitsWidthRejectsWrappedLayout(t *testing.T) {
	if setupMenuFitsWidth(40, setupMenuSpec{
		Title:  strings.Repeat("X", 200),
		Prompt: "prompt",
		Options: []setupMenuOption{
			{Value: "claude", Label: "Claude Code"},
		},
	}) {
		t.Fatal("expected oversized menu to report false for width fit check")
	}
}
