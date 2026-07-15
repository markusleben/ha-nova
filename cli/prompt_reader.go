package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

var setupNextPromptSkipsStaleBlankInput bool
var setupStaleBlankInputWindow = 150 * time.Millisecond

func armSetupNextPromptSkipsStaleBlankInput() {
	setupNextPromptSkipsStaleBlankInput = true
}

func consumeSetupNextPromptSkipsStaleBlankInput() bool {
	armed := setupNextPromptSkipsStaleBlankInput
	setupNextPromptSkipsStaleBlankInput = false
	return armed
}

func clearSetupNextPromptSkipsStaleBlankInput() {
	setupNextPromptSkipsStaleBlankInput = false
}

func beginSetupStaleBlankInputWindow() time.Time {
	if consumeSetupNextPromptSkipsStaleBlankInput() {
		return time.Now().Add(setupStaleBlankInputWindow)
	}
	return time.Time{}
}

func setupInputIsStaleBlank(deadline time.Time, blank bool) bool {
	return !deadline.IsZero() && blank && !time.Now().After(deadline)
}

func rerenderSetupPromptAfterStaleBlank(out io.Writer) {
	if resolveSetupUISession(out).enhanced() {
		fmt.Fprint(out, "\r\033[2K")
		return
	}
	fmt.Fprintln(out)
}

func promptLineWithOptions(reader *bufio.Reader, out io.Writer, label, defaultValue string, allowWizardNav bool) (string, error) {
	staleBlankDeadline := beginSetupStaleBlankInputWindow()

	for {
		fmt.Fprint(out, "  ")
		fmt.Fprint(out, label)
		if defaultValue != "" {
			fmt.Fprintf(out, " [%s]", defaultValue)
		}
		fmt.Fprint(out, ": ")

		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		if setupInputIsStaleBlank(staleBlankDeadline, line == "") {
			rerenderSetupPromptAfterStaleBlank(out)
			continue
		}
		staleBlankDeadline = time.Time{}
		if allowWizardNav {
			switch strings.ToLower(line) {
			case "back":
				return "", errSetupBack
			case "exit":
				return "", errSetupExit
			}
		}
		if line == "" {
			return defaultValue, nil
		}
		return line, nil
	}
}

func promptYesNoWithOptions(reader *bufio.Reader, out io.Writer, label string, defaultYes, allowWizardNav bool) (bool, error) {
	hint := "y/N"
	defaultAnswer := "n"
	if defaultYes {
		hint = "Y/n"
		defaultAnswer = "y"
	}
	answer, err := promptLineWithOptions(reader, out, fmt.Sprintf("%s [%s]", label, hint), "", allowWizardNav)
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		answer = defaultAnswer
	}
	return strings.HasPrefix(answer, "y"), nil
}
