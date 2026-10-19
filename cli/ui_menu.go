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
	Detail         string // muted second line, e.g. what the choice means
}

type setupMenuSpec struct {
	Title        string
	Prompt       string
	Options      []setupMenuOption
	DefaultValue string
	AllowBack    bool
	Note         string // muted line under the options
}

type setupMenuRunner interface {
	Run(out io.Writer, spec setupMenuSpec) (string, error)
}

var interactiveSetupMenuRunner setupMenuRunner = terminalSetupMenuRunner{}

type terminalSetupMenuRunner struct{}

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
	for _, line := range []string{spec.Prompt, spec.Note} {
		if width := len("  " + line); width > maxWidth {
			maxWidth = width
		}
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
			if width := len("      " + setupT("setup.menu.not_available", "reason", option.DisabledReason)); width > maxWidth {
				maxWidth = width
			}
		}
		if width := len("      " + option.Detail); option.Detail != "" && width > maxWidth {
			maxWidth = width
		}
	}
	return maxWidth
}

func renderSetupMenuBlock(out io.Writer, session uiSession, spec setupMenuSpec, selected int) int {
	lines := 0
	writeSetupMenuLine(out, "")
	lines++
	if spec.Title != "" {
		writeSetupMenuLine(out, "  "+session.style("strong", spec.Title))
		writeSetupMenuLine(out, "")
		lines += 2
	}
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
			writeSetupMenuLine(out, "      "+session.style("muted", setupT("setup.menu.not_available", "reason", option.DisabledReason)))
			lines++
		}
		if option.Detail != "" {
			writeSetupMenuLine(out, "      "+session.style("muted", option.Detail))
			lines++
		}
	}
	writeSetupMenuLine(out, "")
	lines++
	if spec.Note != "" {
		writeSetupMenuLine(out, "  "+session.style("muted", spec.Note))
		lines++
	}
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

// menuEscapeWait: how long a byte after Esc counts as part of an escape
// sequence (terminals send a sequence in one write) rather than a new key.
const menuEscapeWait = 50 * time.Millisecond

func readSetupMenuKey() (string, error) {
	return readSetupMenuKeyFrom(os.Stdin)
}

func readSetupMenuKeyFrom(in *os.File) (string, error) {
	var buf [3]byte
	n, err := in.Read(buf[:1])
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
		if !menuInputPending(in, menuEscapeWait) {
			return "back", nil // a lone Esc
		}
		if _, err := in.Read(buf[1:2]); err != nil {
			return "back", nil
		}
		// CSI (ESC [) and, in application cursor mode, SS3 (ESC O).
		if buf[1] != '[' && buf[1] != 'O' {
			return "back", nil
		}
		if _, err := in.Read(buf[2:3]); err != nil {
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
