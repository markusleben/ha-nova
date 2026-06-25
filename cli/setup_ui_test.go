package main

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func testSetupClientChoices() []setupClientChoice {
	return []setupClientChoice{
		{Number: "1", Value: "claude", Label: "Claude Code", Resolved: []string{"claude"}},
		{Number: "2", Value: "codex", Label: "Codex CLI", Resolved: []string{"codex"}},
		{Number: "3", Value: "opencode", Label: "OpenCode", Resolved: []string{"opencode"}},
		{Number: "4", Value: "antigravity", Label: "Google Antigravity", Resolved: []string{"antigravity"}},
		{Number: "5", Value: "hermes", Label: "Hermes Agent", Resolved: []string{"hermes"}},
		{Number: "6", Value: "all", Label: "All available clients", Resolved: []string{"claude", "codex", "opencode", "antigravity", "hermes"}},
	}
}

func TestPromptSetupClientShowsLegacyListAndDefaultsToClaude(t *testing.T) {
	input := strings.NewReader("\n")
	output := &bytes.Buffer{}

	got, err := promptSetupClient(input, output, testSetupClientChoices(), "claude")
	if err != nil {
		t.Fatalf("promptSetupClient() error: %v", err)
	}
	if got != "claude" {
		t.Fatalf("promptSetupClient() = %q, want %q", got, "claude")
	}

	rendered := output.String()
	for _, want := range []string{
		"Which AI client do you use?",
		"1) Claude Code",
		"2) Codex CLI",
		"3) OpenCode",
		"4) Google Antigravity",
		"5) Hermes Agent",
		"6) All available clients",
		"Enter [1-6] (default 1, or type 'exit'): ",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("prompt output missing %q:\n%s", want, rendered)
		}
	}
}

func TestPromptSetupClientAcceptsNumberSelection(t *testing.T) {
	input := strings.NewReader("4\n")
	output := &bytes.Buffer{}

	got, err := promptSetupClient(input, output, testSetupClientChoices(), "claude")
	if err != nil {
		t.Fatalf("promptSetupClient() error: %v", err)
	}
	if got != "antigravity" {
		t.Fatalf("promptSetupClient() = %q, want %q", got, "antigravity")
	}
}

func TestPromptSetupClientAcceptsClientName(t *testing.T) {
	input := strings.NewReader("opencode\n")
	output := &bytes.Buffer{}

	got, err := promptSetupClient(input, output, testSetupClientChoices(), "claude")
	if err != nil {
		t.Fatalf("promptSetupClient() error: %v", err)
	}
	if got != "opencode" {
		t.Fatalf("promptSetupClient() = %q, want %q", got, "opencode")
	}
}

func TestPromptSetupClientRepromptsOnInvalidInput(t *testing.T) {
	input := strings.NewReader("banana\n4\n")
	output := &bytes.Buffer{}

	got, err := promptSetupClient(input, output, testSetupClientChoices(), "claude")
	if err != nil {
		t.Fatalf("promptSetupClient() error: %v", err)
	}
	if got != "antigravity" {
		t.Fatalf("promptSetupClient() = %q, want %q", got, "antigravity")
	}
	if !strings.Contains(output.String(), "Invalid choice") {
		t.Fatalf("expected invalid-choice guidance in output:\n%s", output.String())
	}
	if strings.Contains(output.String(), "✗") {
		t.Fatalf("expected plain-safe invalid-choice marker in output:\n%s", output.String())
	}
}

func TestPromptSetupClientSupportsWizardExit(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("exit\n"))
	output := &bytes.Buffer{}

	_, err := promptSetupClientFromReader(reader, output, testSetupClientChoices(), "claude")
	if err != errSetupExit {
		t.Fatalf("promptSetupClientFromReader() error = %v, want %v", err, errSetupExit)
	}
}

func TestPromptSetupClientSupportsWizardBack(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("back\n"))
	output := &bytes.Buffer{}

	_, err := promptSetupClientFromReader(reader, output, testSetupClientChoices(), "claude")
	if err != errSetupBack {
		t.Fatalf("promptSetupClientFromReader() error = %v, want %v", err, errSetupBack)
	}
}

func TestPromptReadersCanBeReusedAcrossSequentialSetupQuestions(t *testing.T) {
	input := strings.NewReader("4\n127.0.0.1:38123\n\n")
	reader := bufio.NewReader(input)
	output := &bytes.Buffer{}

	client, err := promptSetupClientFromReader(reader, output, testSetupClientChoices(), "claude")
	if err != nil {
		t.Fatalf("promptSetupClientFromReader() error: %v", err)
	}
	if client != "antigravity" {
		t.Fatalf("client = %q, want antigravity", client)
	}

	host, err := promptLineFromReader(reader, output, "Home Assistant address (IP, hostname, or URL)", "homeassistant.local")
	if err != nil {
		t.Fatalf("promptLineFromReader(host) error: %v", err)
	}
	if host != "127.0.0.1:38123" {
		t.Fatalf("host = %q, want 127.0.0.1:38123", host)
	}

	enter, err := promptLineFromReader(reader, output, "Press Enter after you saved the Relay Auth Token in NOVA Relay", "")
	if err != nil {
		t.Fatalf("promptLineFromReader(enter) error: %v", err)
	}
	if enter != "" {
		t.Fatalf("enter = %q, want empty string", enter)
	}
}

func TestPromptLineFromReaderIndentsPromptLabel(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	output := &bytes.Buffer{}

	_, err := promptLineFromReader(reader, output, "Home Assistant address", "homeassistant.local")
	if err != nil {
		t.Fatalf("promptLineFromReader() error: %v", err)
	}
	if !strings.Contains(output.String(), "  Home Assistant address [homeassistant.local]: ") {
		t.Fatalf("expected indented prompt label:\n%s", output.String())
	}
}

func TestPromptWizardLineFromReaderAddsPromptGap(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("\n"))
	output := &bytes.Buffer{}

	_, err := promptWizardLineFromReader(reader, output, "Press Enter to open your browser", "")
	if err != nil {
		t.Fatalf("promptWizardLineFromReader() error: %v", err)
	}
	if !strings.HasPrefix(output.String(), "\n  Press Enter to open your browser: ") {
		t.Fatalf("expected wizard prompt to start on its own spaced line:\n%q", output.String())
	}
}

func TestPromptLineFromReaderSkipsSingleStaleBlankAfterProtectedProgress(t *testing.T) {
	clearSetupNextPromptSkipsStaleBlankInput()
	defer clearSetupNextPromptSkipsStaleBlankInput()

	armSetupNextPromptSkipsStaleBlankInput()

	reader := bufio.NewReader(strings.NewReader("\n192.168.1.123\n"))
	output := &bytes.Buffer{}

	host, err := promptLineFromReader(reader, output, "Home Assistant address", "homeassistant.local")
	if err != nil {
		t.Fatalf("promptLineFromReader() error: %v", err)
	}
	if host != "192.168.1.123" {
		t.Fatalf("host = %q, want %q", host, "192.168.1.123")
	}
	if strings.Count(output.String(), "Home Assistant address") != 2 {
		t.Fatalf("expected prompt to be re-rendered after stale blank input:\n%s", output.String())
	}
}

func TestPromptLineFromReaderSkipsMultipleStaleBlanksAfterProtectedProgress(t *testing.T) {
	clearSetupNextPromptSkipsStaleBlankInput()
	defer clearSetupNextPromptSkipsStaleBlankInput()

	armSetupNextPromptSkipsStaleBlankInput()

	reader := bufio.NewReader(strings.NewReader("\n\n192.168.1.123\n"))
	output := &bytes.Buffer{}

	host, err := promptLineFromReader(reader, output, "Home Assistant address", "homeassistant.local")
	if err != nil {
		t.Fatalf("promptLineFromReader() error: %v", err)
	}
	if host != "192.168.1.123" {
		t.Fatalf("host = %q, want %q", host, "192.168.1.123")
	}
	if strings.Count(output.String(), "Home Assistant address") != 3 {
		t.Fatalf("expected prompt to be re-rendered for each stale blank input:\n%s", output.String())
	}
}

func TestPromptLineFromReaderAllowsDelayedBlankDefaultAfterProtectedProgress(t *testing.T) {
	clearSetupNextPromptSkipsStaleBlankInput()
	defer clearSetupNextPromptSkipsStaleBlankInput()

	originalWindow := setupStaleBlankInputWindow
	defer func() {
		setupStaleBlankInputWindow = originalWindow
	}()
	setupStaleBlankInputWindow = 25 * time.Millisecond

	armSetupNextPromptSkipsStaleBlankInput()

	inputReader, inputWriter := io.Pipe()
	defer inputReader.Close()

	go func() {
		time.Sleep(2 * setupStaleBlankInputWindow)
		_, _ = inputWriter.Write([]byte("\n"))
		_ = inputWriter.Close()
	}()

	output := &bytes.Buffer{}
	host, err := promptLineFromReader(bufio.NewReader(inputReader), output, "Home Assistant address", "homeassistant.local")
	if err != nil {
		t.Fatalf("promptLineFromReader() error: %v", err)
	}
	if host != "homeassistant.local" {
		t.Fatalf("host = %q, want %q", host, "homeassistant.local")
	}
	if strings.Count(output.String(), "Home Assistant address") != 1 {
		t.Fatalf("did not expect prompt to be re-rendered for delayed blank input:\n%s", output.String())
	}
}

func TestPromptSetupClientExplainsDisabledChoiceAndReprompts(t *testing.T) {
	choices := testSetupClientChoices()
	choices[1].Disabled = true
	choices[1].DisabledReason = "install Codex CLI first"

	input := strings.NewReader("2\n1\n")
	output := &bytes.Buffer{}

	got, err := promptSetupClient(input, output, choices, "claude")
	if err != nil {
		t.Fatalf("promptSetupClient() error: %v", err)
	}
	if got != "claude" {
		t.Fatalf("promptSetupClient() = %q, want claude", got)
	}
	if !strings.Contains(output.String(), "Codex CLI is not available yet: install Codex CLI first") {
		t.Fatalf("expected disabled-choice guidance:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "not available: install Codex CLI first") {
		t.Fatalf("expected disabled reason on its own line:\n%s", output.String())
	}
}

func TestRenderSetupHeaderMatchesWizardStyle(t *testing.T) {
	output := &bytes.Buffer{}
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalEnv := uiEnvLookup
	originalANSI := uiOutputSupportsANSI
	defer func() {
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiEnvLookup = originalEnv
		uiOutputSupportsANSI = originalANSI
	}()
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiEnvLookup = func(string) string { return "" }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	renderSetupHeader(output)

	rendered := output.String()
	for _, want := range []string{
		"HA NOVA Setup",
		"─",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("header missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderSetupHeaderFallsBackToPlainModeForNonTTY(t *testing.T) {
	output := &bytes.Buffer{}

	renderSetupHeader(output)

	rendered := output.String()
	if !strings.Contains(rendered, "HA NOVA Setup") {
		t.Fatalf("plain header missing title:\n%s", rendered)
	}
	if strings.Contains(rendered, "╭") || strings.Contains(rendered, "╰") || strings.Contains(rendered, "─") {
		t.Fatalf("plain header should not use box drawing:\n%s", rendered)
	}
}

func TestRenderSetupHeaderClearsScreenForTTY(t *testing.T) {
	output := &bytes.Buffer{}
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalEnv := uiEnvLookup
	originalANSI := uiOutputSupportsANSI
	defer func() {
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiEnvLookup = originalEnv
		uiOutputSupportsANSI = originalANSI
	}()
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiEnvLookup = func(string) string { return "" }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	renderSetupHeader(output)

	if !strings.HasPrefix(output.String(), "\x1b[2J\x1b[H") {
		t.Fatalf("expected header to clear the screen first, got %q", output.String())
	}
}

func TestRenderSetupStepMatchesWizardStyle(t *testing.T) {
	output := &bytes.Buffer{}
	originalTTY := writerSupportsTTYForSetup
	originalInput := uiInputSupportsTTY
	originalEnv := uiEnvLookup
	originalANSI := uiOutputSupportsANSI
	defer func() {
		writerSupportsTTYForSetup = originalTTY
		uiInputSupportsTTY = originalInput
		uiEnvLookup = originalEnv
		uiOutputSupportsANSI = originalANSI
	}()
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiEnvLookup = func(string) string { return "" }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	renderSetupStep(output, 2, 4, "Set up secure access")

	rendered := output.String()
	for _, want := range []string{
		"Step 2 of 4",
		"Set up secure access",
		"●",
		"○",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("step output missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "────────────────") {
		t.Fatalf("step output should stay compact without a separator rule:\n%s", rendered)
	}
}

func TestRenderSetupStepFallsBackToPlainModeForNonTTY(t *testing.T) {
	output := &bytes.Buffer{}

	renderSetupStep(output, 2, 4, "Set up secure access")

	rendered := output.String()
	for _, want := range []string{
		"[2/4]",
		"Step 2 of 4 - Set up secure access",
		"Set up secure access",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("plain step output missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "●") || strings.Contains(rendered, "○") {
		t.Fatalf("plain step should not use unicode step dots:\n%s", rendered)
	}
}

func TestRenderSetupMutedNoteBlockKeepsReminderSecondary(t *testing.T) {
	output := &bytes.Buffer{}

	renderSetupMutedNoteBlock(output, "Only if needed", "Still missing the Relay Auth Token?", "Here it is again:", "  abc123")

	rendered := output.String()
	for _, want := range []string{
		"[ Only if needed ]",
		"Still missing the Relay Auth Token?",
		"Here it is again:",
		"  abc123",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("muted note block missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderSetupCompleteBannerUsesClientLabel(t *testing.T) {
	output := &bytes.Buffer{}

	renderSetupCompleteBanner(output, []string{"claude"})

	rendered := output.String()
	for _, want := range []string{
		"Setup complete!",
		"Installed for: ",
		"Claude Code",
		"ha-nova doctor",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("complete banner missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderSetupCompleteBannerShowsInstalledClientsForMultipleTargets(t *testing.T) {
	output := &bytes.Buffer{}

	renderSetupCompleteBanner(output, []string{"claude", "antigravity"})

	rendered := output.String()
	for _, want := range []string{
		"Installed for: ",
		"Claude Code, Google Antigravity",
		"Open your installed AI assistants and try asking:",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("complete banner missing %q:\n%s", want, rendered)
		}
	}
}

func TestRenderSetupIncompleteBannerUsesIssueSpecificText(t *testing.T) {
	output := &bytes.Buffer{}

	renderSetupIncompleteBanner(output, setupIssueRelayUnreachable)

	rendered := output.String()
	for _, want := range []string{
		"Setup incomplete",
		"relay could not be verified yet",
		"Open Home Assistant",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("incomplete banner missing %q:\n%s", want, rendered)
		}
	}
}

func TestPromptSetupTokenChoiceRepromptsOnInvalidInput(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("banana\n2\n"))
	output := &bytes.Buffer{}

	got, err := promptSetupTokenChoiceFromReader(reader, output, false)
	if err != nil {
		t.Fatalf("promptSetupTokenChoiceFromReader() error: %v", err)
	}
	if got != "generate" {
		t.Fatalf("promptSetupTokenChoiceFromReader() = %q, want %q", got, "generate")
	}
	if !strings.Contains(output.String(), "Invalid choice") {
		t.Fatalf("expected invalid-choice guidance in output:\n%s", output.String())
	}
	if strings.Contains(output.String(), "✗") {
		t.Fatalf("expected plain-safe invalid-choice marker in output:\n%s", output.String())
	}
}
