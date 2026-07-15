package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

var errSetupMenuUnavailable = errors.New("setup menu unavailable")

type setupMenuOption struct {
	Value          string
	Label          string
	Disabled       bool
	DisabledReason string
}

type setupMenuSpec struct {
	Title        string
	Prompt       string
	Options      []setupMenuOption
	DefaultValue string
	AllowBack    bool
}

type setupMenuRunner interface {
	Run(out io.Writer, spec setupMenuSpec) (string, error)
}

var interactiveSetupMenuRunner setupMenuRunner = terminalSetupMenuRunner{}

type terminalSetupMenuRunner struct{}

func promptSetupClientInteractive(reader *bufio.Reader, out io.Writer, choices []setupClientChoice, defaultClient string) (string, error) {
	spec := setupMenuSpec{
		Title:        "Which AI client do you use?",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Ctrl+C to exit",
		DefaultValue: defaultClient,
	}
	for _, choice := range choices {
		spec.Options = append(spec.Options, setupMenuOption{
			Value:          choice.Value,
			Label:          choice.Label,
			Disabled:       choice.Disabled,
			DisabledReason: choice.DisabledReason,
		})
	}
	return promptSetupMenu(reader, out, spec, func() (string, error) {
		return promptSetupClientFromReader(reader, out, choices, defaultClient)
	})
}

func promptSetupTokenChoiceInteractive(reader *bufio.Reader, out io.Writer, hasExistingLocal bool) (string, error) {
	spec := setupMenuSpec{
		Title:        "Choose how to set up the Relay Auth Token:",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit",
		AllowBack:    true,
		DefaultValue: "generate",
	}
	if hasExistingLocal {
		spec.Options = []setupMenuOption{
			{Value: "keep", Label: "Keep saved token"},
			{Value: "paste", Label: "Paste existing token from another device / Home Assistant"},
			{Value: "generate", Label: "Generate a new token"},
		}
		spec.DefaultValue = "keep"
	} else {
		spec.Options = []setupMenuOption{
			{Value: "paste", Label: "Paste existing token from another device / Home Assistant"},
			{Value: "generate", Label: "Generate a new token"},
		}
	}
	return promptSetupMenu(reader, out, spec, func() (string, error) {
		return promptSetupTokenChoiceFromReader(reader, out, hasExistingLocal)
	})
}

func promptSetupRepairActionInteractive(reader *bufio.Reader, out io.Writer, mode setupRepairMode, credentialRepair setupCredentialRepairMode) (setupRepairAction, error) {
	choices, defaultChoice := setupRepairChoices(mode, credentialRepair)
	spec := setupMenuSpec{
		Title:        "Next step:",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit",
		AllowBack:    true,
		DefaultValue: defaultChoice,
	}
	for _, choice := range choices {
		spec.Options = append(spec.Options, setupMenuOption{
			Value: string(choice.Value),
			Label: choice.Label,
		})
	}
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		action, err := promptSetupRepairActionFromReader(reader, out, mode, credentialRepair)
		return string(action), err
	})
	if err != nil {
		return "", err
	}
	return setupRepairAction(answer), nil
}

func promptSetupMenu(reader *bufio.Reader, out io.Writer, spec setupMenuSpec, fallback func() (string, error)) (string, error) {
	if resolveSetupUISession(out).enhanced() && interactiveSetupMenuRunner != nil {
		answer, err := interactiveSetupMenuRunner.Run(out, spec)
		if err == nil {
			return answer, nil
		}
		if !errors.Is(err, errSetupMenuUnavailable) {
			return "", err
		}
	}
	return fallback()
}

func (terminalSetupMenuRunner) Run(out io.Writer, spec setupMenuSpec) (string, error) {
	stdout, ok := out.(*os.File)
	if !ok || stdout != os.Stdout || !uiInputSupportsTTY() || !writerSupportsTTY(stdout) {
		return "", errSetupMenuUnavailable
	}
	if !setupMenuFitsTerminalWidth(stdout, spec) {
		return "", errSetupMenuUnavailable
	}

	state, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return "", errSetupMenuUnavailable
	}
	defer term.Restore(int(os.Stdin.Fd()), state)

	session := resolveSetupUISession(out)
	index := defaultMenuIndex(spec.Options, spec.DefaultValue)
	if index < 0 {
		return "", errSetupMenuUnavailable
	}
	staleBlankDeadline := beginSetupStaleBlankInputWindow()

	lines := renderSetupMenuBlock(out, session, spec, index)
	for {
		key, err := readSetupMenuKey()
		if err != nil {
			return "", err
		}
		switch key {
		case "up":
			index = moveSetupMenuIndex(spec.Options, index, -1)
		case "down":
			index = moveSetupMenuIndex(spec.Options, index, 1)
		case "enter":
			if setupInputIsStaleBlank(staleBlankDeadline, true) {
				continue
			}
			return spec.Options[index].Value, nil
		case "back":
			if spec.AllowBack {
				return "", errSetupBack
			}
		case "exit":
			return "", errSetupExit
		default:
			continue
		}
		staleBlankDeadline = time.Time{}
		fmt.Fprintf(out, "\033[%dA\033[J", lines)
		lines = renderSetupMenuBlock(out, session, spec, index)
	}
}

func setupMenuFitsTerminalWidth(stdout *os.File, spec setupMenuSpec) bool {
	width, _, err := term.GetSize(int(stdout.Fd()))
	if err != nil || width <= 0 {
		return true
	}
	return setupMenuFitsWidth(width, spec)
}

func setupMenuFitsWidth(width int, spec setupMenuSpec) bool {
	if width <= 0 {
		return true
	}
	return setupMenuBlockWidth(spec) < width
}

func setupMenuBlockWidth(spec setupMenuSpec) int {
	maxWidth := len("  " + spec.Title)
	if promptWidth := len("  " + spec.Prompt); promptWidth > maxWidth {
		maxWidth = promptWidth
	}
	for _, option := range spec.Options {
		prefix := "    "
		if selectedWidth := len("  › " + option.Label); selectedWidth > maxWidth {
			maxWidth = selectedWidth
		}
		if width := len(prefix + option.Label); width > maxWidth {
			maxWidth = width
		}
		if option.Disabled && option.DisabledReason != "" {
			if width := len("      not available: " + option.DisabledReason); width > maxWidth {
				maxWidth = width
			}
		}
	}
	return maxWidth
}

func renderSetupMenuBlock(out io.Writer, session uiSession, spec setupMenuSpec, selected int) int {
	lines := 0
	writeSetupMenuLine(out, "")
	lines++
	writeSetupMenuLine(out, "  "+session.style("strong", spec.Title))
	lines++
	writeSetupMenuLine(out, "")
	lines++
	for idx, option := range spec.Options {
		prefix := "    "
		if idx == selected {
			prefix = "  " + session.style("accent", "› ")
		}
		label := option.Label
		if option.Disabled {
			label = session.style("muted", label)
		} else if idx == selected {
			label = session.style("strong", label)
		}
		writeSetupMenuLine(out, prefix+label)
		lines++
		if option.Disabled && option.DisabledReason != "" {
			writeSetupMenuLine(out, "      "+session.style("muted", "not available: "+option.DisabledReason))
			lines++
		}
	}
	writeSetupMenuLine(out, "")
	lines++
	writeSetupMenuLine(out, "  "+session.style("muted", spec.Prompt))
	lines++
	return lines
}

func writeSetupMenuLine(out io.Writer, line string) {
	fmt.Fprintf(out, "%s\r\n", line)
}

func defaultMenuIndex(options []setupMenuOption, defaultValue string) int {
	for idx, option := range options {
		if option.Value == defaultValue && !option.Disabled {
			return idx
		}
	}
	for idx, option := range options {
		if !option.Disabled {
			return idx
		}
	}
	return -1
}

func moveSetupMenuIndex(options []setupMenuOption, current, delta int) int {
	if len(options) == 0 {
		return current
	}
	idx := current
	for range options {
		idx = (idx + delta + len(options)) % len(options)
		if !options[idx].Disabled {
			return idx
		}
	}
	return current
}

func readSetupMenuKey() (string, error) {
	var buf [3]byte
	n, err := os.Stdin.Read(buf[:1])
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", io.EOF
	}
	switch buf[0] {
	case 3:
		return "exit", nil
	case '\r', '\n':
		return "enter", nil
	case 'j':
		return "down", nil
	case 'k':
		return "up", nil
	case 27:
		n, err := os.Stdin.Read(buf[1:2])
		if err != nil {
			return "back", nil
		}
		if n == 0 || buf[1] != '[' {
			return "back", nil
		}
		if _, err := os.Stdin.Read(buf[2:3]); err != nil {
			return "", err
		}
		switch buf[2] {
		case 'A':
			return "up", nil
		case 'B':
			return "down", nil
		default:
			return "", nil
		}
	default:
		return "", nil
	}
}
