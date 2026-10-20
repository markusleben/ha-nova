package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

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
	width, height, err := term.GetSize(int(stdout.Fd()))
	if err != nil {
		width, height = 0, 0 // unknown: no wrapping
	}
	// The redraw moves the cursor up over the whole menu: it must fit.
	if (width > 0 && width < 20) || (height > 0 && renderSetupMenuBlock(io.Discard, session, spec, index, width) >= height) {
		return "", errSetupMenuUnavailable
	}
	staleBlankDeadline := beginSetupStaleBlankInputWindow()

	lines := renderSetupMenuBlock(out, session, spec, index, width)
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
		lines = renderSetupMenuBlock(out, session, spec, index, width)
	}
}

// renderSetupMenuBlock draws the menu and returns the rows it used. Long
// lines wrap at word breaks to the terminal width (0: unknown, no wrapping),
// so the redraw stays exact and no text forces the numbered fallback.
func renderSetupMenuBlock(out io.Writer, session uiSession, spec setupMenuSpec, selected, width int) int {
	lines := 0
	write := func(lead, hang, text, style string) {
		for idx, part := range wrapSetupMenuText(text, width-1-setupMenuTextWidth(hang)) {
			if idx > 0 {
				lead = hang
			}
			writeSetupMenuLine(out, lead+session.style(style, part))
			lines++
		}
	}
	writeSetupMenuLine(out, "")
	lines++
	if spec.Title != "" {
		write("  ", "  ", spec.Title, "strong")
		writeSetupMenuLine(out, "")
		lines++
	}
	for idx, option := range spec.Options {
		lead, style := "    ", ""
		if idx == selected {
			lead, style = "  "+session.style("accent", "› "), "strong"
		}
		if option.Disabled {
			style = "muted"
		}
		write(lead, "    ", option.Label, style)
		if option.Disabled && option.DisabledReason != "" {
			write("      ", "      ", setupT("setup.menu.not_available", "reason", option.DisabledReason), "muted")
		}
		if option.Detail != "" {
			write("      ", "      ", option.Detail, "muted")
		}
	}
	writeSetupMenuLine(out, "")
	lines++
	if spec.Note != "" {
		write("  ", "  ", spec.Note, "muted")
	}
	write("  ", "  ", spec.Prompt, "muted")
	return lines
}

// wrapSetupMenuText splits text into lines of at most room columns, at
// spaces where it can; a word longer than room (a URL) is cut. A line that
// fits, or room < 1, stays as it is.
func wrapSetupMenuText(text string, room int) []string {
	if room < 1 || setupMenuTextWidth(text) <= room {
		return []string{text}
	}
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		for setupMenuTextWidth(word) > room {
			if line != "" {
				lines, line = append(lines, line), ""
			}
			cut, used := 0, 0
			for i, r := range word {
				if used+setupMenuRuneWidth(r) > room {
					break
				}
				cut, used = i+utf8.RuneLen(r), used+setupMenuRuneWidth(r)
			}
			if cut == 0 { // a wide letter wider than room: one per line
				_, cut = utf8.DecodeRuneInString(word)
			}
			lines, word = append(lines, word[:cut]), word[cut:]
		}
		switch {
		case line == "":
			line = word
		case setupMenuTextWidth(line)+1+setupMenuTextWidth(word) > room:
			lines, line = append(lines, line), word
		default:
			line += " " + word
		}
	}
	return append(lines, line)
}

// setupMenuTextWidth is text's width in terminal columns.
func setupMenuTextWidth(text string) int {
	width := 0
	for _, r := range text {
		width += setupMenuRuneWidth(r)
	}
	return width
}

// setupMenuRuneWidth: East Asian wide letters and emoji take two columns
// (names come from the user's own phones); everything else one.
// ponytail: main wide blocks only, a full width table if menus show more.
func setupMenuRuneWidth(r rune) int {
	switch {
	case r >= 0x1100 && r <= 0x115F, r >= 0x2E80 && r <= 0xA4CF, r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF, r >= 0xFE30 && r <= 0xFE4F, r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6, r >= 0x1F300 && r <= 0x1FAFF, r >= 0x20000 && r <= 0x3FFFD:
		return 2
	}
	return 1
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
