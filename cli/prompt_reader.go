package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

func promptLineWithOptions(reader *bufio.Reader, out io.Writer, label, defaultValue string, allowWizardNav bool) (string, error) {
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
