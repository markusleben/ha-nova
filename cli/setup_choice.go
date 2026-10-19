package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// setupChoice and setupAsk are the renderer-neutral shape of a setup
// question: the terminal shows the arrow menu (or its numbered fallback);
// the setup page will show buttons.
type setupChoice struct {
	Value    string
	Label    setupMsg
	Disabled bool
	Reason   string // why a disabled choice is unavailable (data, not copy)
}

type setupAsk struct {
	Title     setupMsg
	Choices   []setupChoice
	Default   string
	AllowBack bool // Esc goes back; typing "back" always does
}

func promptSetupChoice(reader *bufio.Reader, out io.Writer, ask setupAsk) (string, error) {
	hint := setupT("setup.menu.hint")
	if ask.AllowBack {
		hint = setupT("setup.menu.hint_back")
	}
	spec := setupMenuSpec{
		Title:        ask.Title.plain(),
		Prompt:       hint,
		DefaultValue: ask.Default,
		AllowBack:    ask.AllowBack,
	}
	for _, choice := range ask.Choices {
		spec.Options = append(spec.Options, setupMenuOption{
			Value:          choice.Value,
			Label:          choice.Label.plain(),
			Disabled:       choice.Disabled,
			DisabledReason: choice.Reason,
		})
	}
	return promptSetupMenu(reader, out, spec, func() (string, error) {
		return promptSetupChoiceByNumber(reader, out, ask)
	})
}

// promptSetupChoiceByNumber is the arrow menu's numbered fallback.
func promptSetupChoiceByNumber(reader *bufio.Reader, out io.Writer, ask setupAsk) (string, error) {
	defaultIndex := setupChoiceDefaultIndex(ask)
	if defaultIndex < 0 {
		panic("setup question without an available choice: " + ask.Title.Key)
	}
	for {
		renderSetupChoiceList(out, ask)
		fmt.Fprintln(out)
		enter := setupT("setup.menu.enter_number", "last", len(ask.Choices), "default", defaultIndex+1)
		if ask.AllowBack {
			enter = setupT("setup.menu.enter_number_back", "last", len(ask.Choices), "default", defaultIndex+1)
		}
		fmt.Fprintf(out, "  %s", enter)

		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.ToLower(strings.TrimSpace(line))
		switch line {
		case "":
			return ask.Choices[defaultIndex].Value, nil
		case "back":
			return "", errSetupBack
		case "exit":
			return "", errSetupExit
		}
		for idx, choice := range ask.Choices {
			if line == strconv.Itoa(idx+1) || line == choice.Value {
				if choice.Disabled {
					renderSetupErrorLine(out, "%s", setupT("setup.menu.choice_unavailable", "choice", choice.Label.plain(), "reason", choice.Reason))
					break
				}
				return choice.Value, nil
			}
		}
		renderSetupErrorLine(out, "%s", setupT("setup.menu.invalid_choice"))
	}
}

// setupChoiceDefaultIndex: the default when it is available, else the first
// available choice, else -1.
func setupChoiceDefaultIndex(ask setupAsk) int {
	first := -1
	for idx, choice := range ask.Choices {
		if choice.Disabled {
			continue
		}
		if choice.Value == ask.Default {
			return idx
		}
		if first < 0 {
			first = idx
		}
	}
	return first
}

func renderSetupChoiceList(out io.Writer, ask setupAsk) {
	fmt.Fprintln(out)
	fmt.Fprintf(out, "  %s\n", ask.Title.plain())
	fmt.Fprintln(out)
	for idx, choice := range ask.Choices {
		fmt.Fprintf(out, "    %d) %s\n", idx+1, choice.Label.plain())
		if choice.Disabled && choice.Reason != "" {
			fmt.Fprintf(out, "       %s\n", setupT("setup.menu.not_available", "reason", choice.Reason))
		}
	}
}

func promptSetupClientInteractive(reader *bufio.Reader, out io.Writer, choices []setupClientChoice, defaultClient string) (string, error) {
	if len(choices) == 0 {
		return "", errors.New(setupT("setup.clients.none_registered"))
	}
	ask := setupAsk{Title: setupText("setup.clients.question"), Default: defaultClient}
	for _, choice := range choices {
		ask.Choices = append(ask.Choices, setupChoice{
			Value:    choice.Value,
			Label:    setupText("setup.clients.label", "client", choice.Label),
			Disabled: choice.Disabled,
			Reason:   choice.DisabledReason,
		})
	}
	if !hasAvailableSetupClientChoice(choices) {
		renderSetupChoiceList(out, ask)
		renderSetupParagraph(out, setupT("setup.clients.none_ready"), setupT("setup.clients.install_first"))
		return "", errSetupClientPrerequisite
	}
	return promptSetupChoice(reader, out, ask)
}

func promptSetupTokenChoiceInteractive(reader *bufio.Reader, out io.Writer, hasExistingLocal bool) (string, error) {
	ask := setupAsk{Title: setupText("setup.token.question"), AllowBack: true, Default: "generate"}
	if hasExistingLocal {
		ask.Choices = append(ask.Choices, setupChoice{Value: "keep", Label: setupText("setup.token.keep")})
		ask.Default = "keep"
	}
	ask.Choices = append(ask.Choices,
		setupChoice{Value: "paste", Label: setupText("setup.token.paste")},
		setupChoice{Value: "generate", Label: setupText("setup.token.generate")},
	)
	return promptSetupChoice(reader, out, ask)
}
