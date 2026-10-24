package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
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
		Title:        "Which AI app do you use?",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Ctrl+C to exit",
		DefaultValue: "claude",
		Options: []setupMenuOption{
			{Value: "claude", Label: "Claude Code"},
			{Value: "antigravity", Label: "Google Antigravity"},
		},
	}, 0, 0)

	if lines == 0 {
		t.Fatal("expected rendered menu lines")
	}
	rendered := output.String()
	if !strings.Contains(rendered, "\r\n") {
		t.Fatalf("expected CRLF in rendered menu output, got %q", rendered)
	}
	if strings.Contains(rendered, "\n  Which AI app do you use?\n") {
		t.Fatalf("expected raw-menu rendering to avoid bare LF-only layout, got %q", rendered)
	}
}

func TestRenderSetupMenuBlockUsesCompactLayoutAndSecondLineDisabledReason(t *testing.T) {
	output := &bytes.Buffer{}

	renderSetupMenuBlock(output, uiSession{mode: uiModeEnhanced, color: true}, setupMenuSpec{
		Title:  "Which AI app do you use?",
		Prompt: "Use ↑/↓ or j/k, Enter to select, Ctrl+C to exit",
		Options: []setupMenuOption{
			{Value: "claude", Label: "Claude Code"},
			{Value: "codex", Label: "Codex CLI", Disabled: true, DisabledReason: "install Codex CLI first"},
		},
	}, 0, 0)

	rendered := output.String()
	if !strings.Contains(rendered, "Which AI app do you use?") || !strings.Contains(rendered, "\r\n\r\n  ") {
		t.Fatalf("expected compact menu title block with a single blank separator, got %q", rendered)
	}
	if !strings.Contains(rendered, "not available: install Codex CLI first") {
		t.Fatalf("expected disabled reason on second line, got %q", rendered)
	}
}

// A long line wraps at word breaks inside the terminal width, and the row
// count covers every wrapped row, so the redraw clears exactly the menu.
// (0.27 recheck: a long reason pushed the arrow menu to the numbered prompt.)
func TestRenderSetupMenuBlockWrapsLongLinesToTheTerminal(t *testing.T) {
	spec := setupMenuSpec{
		Title:  "Remove HA NOVA from this computer?",
		Prompt: "Use ↑/↓ or j/k, Enter to select, Ctrl+C to cancel",
		Options: []setupMenuOption{
			{Value: "purge", Label: "Remove", Disabled: true, DisabledReason: "the keyring is locked; unlock it, then run ha-nova uninstall again"},
			{Value: "cancel", Label: "Cancel"},
		},
		Note: "https://example.invalid/" + strings.Repeat("x", 100),
	}
	var out bytes.Buffer
	rows := renderSetupMenuBlock(&out, uiSession{}, spec, 1, 40)
	lines := strings.Split(strings.TrimSuffix(out.String(), "\r\n"), "\r\n")
	if rows != len(lines) {
		t.Fatalf("rows = %d, printed %d:\n%s", rows, len(lines), out.String())
	}
	for _, line := range lines {
		if n := utf8.RuneCountInString(line); n >= 40 {
			t.Fatalf("line is %d columns at width 40: %q", n, line)
		}
	}
	for _, want := range []string{"      not available: the keyring is", "      ha-nova uninstall again", "  › Cancel"} {
		if !strings.Contains(out.String(), want+"\r\n") {
			t.Fatalf("missing %q:\n%s", want, out.String())
		}
	}
	// Wide letters (a phone's name) take two columns each.
	wide := wrapSetupMenuText("電話 "+strings.Repeat("電", 30), 20)
	for _, line := range wide {
		if setupMenuTextWidth(line) > 20 {
			t.Fatalf("wide line is %d columns: %q", setupMenuTextWidth(line), wide)
		}
	}
	if flat := renderSetupMenuBlock(io.Discard, uiSession{}, spec, 1, 0); flat >= rows {
		t.Fatalf("unknown width must not wrap: %d rows vs %d", flat, rows)
	}
}

func TestPromptWizardYesNoMenu(t *testing.T) {
	originalEnv, originalTTY, originalInput, originalANSI, originalRunner := uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner
	t.Cleanup(func() {
		uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner = originalEnv, originalTTY, originalInput, originalANSI, originalRunner
	})
	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	ask := func(input string) (bool, error) {
		return promptWizardYesNoMenu(bufio.NewReader(strings.NewReader(input)), &bytes.Buffer{}, "Add another Home Assistant server?", "Yes", "No", false)
	}

	for answer, want := range map[string]bool{"yes": true, "no": false} {
		interactiveSetupMenuRunner = &stubSetupMenuRunner{answer: answer}
		if got, err := ask(""); got != want || err != nil {
			t.Fatalf("menu %q: %v, %v", answer, got, err)
		}
	}
	interactiveSetupMenuRunner = &stubSetupMenuRunner{err: errSetupBack}
	if got, err := ask(""); got || !errors.Is(err, errSetupBack) {
		t.Fatalf("Esc: %v, %v", got, err)
	}
	// Without the menu, the [y/N] line still answers.
	interactiveSetupMenuRunner = &stubSetupMenuRunner{err: errSetupMenuUnavailable}
	if got, err := ask("y\n"); !got || err != nil {
		t.Fatalf("fallback y: %v, %v", got, err)
	}
	if got, err := ask("\n"); got || err != nil {
		t.Fatalf("fallback default: %v, %v", got, err)
	}
}
