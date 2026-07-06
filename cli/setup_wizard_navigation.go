package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
)

var errSetupBack = errors.New("setup back")
var errSetupExit = errors.New("setup exit")
var errSetupRelayTokenStep = errors.New("setup relay token step")
var errSetupHostStep = errors.New("setup host step")

func promptWizardLineFromReader(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	fmt.Fprintln(out)
	return promptLineWithOptions(reader, out, label, defaultValue, true)
}

func promptWizardYesNoFromReader(reader *bufio.Reader, out io.Writer, label string, defaultYes bool) (bool, error) {
	return promptYesNoWithOptions(reader, out, label, defaultYes, true)
}

type setupTokenChoice struct {
	Number string
	Value  string
	Label  string
}

func promptSetupTokenChoiceFromReader(reader *bufio.Reader, out io.Writer, hasExistingLocal bool) (string, error) {
	choices := []setupTokenChoice{
		{Number: "1", Value: "paste", Label: "Paste existing token from another device / Home Assistant"},
		{Number: "2", Value: "generate", Label: "Generate a new token"},
	}
	defaultChoice := "2"
	if hasExistingLocal {
		choices = []setupTokenChoice{
			{Number: "1", Value: "keep", Label: "Keep saved token"},
			{Number: "2", Value: "paste", Label: "Paste existing token from another device / Home Assistant"},
			{Number: "3", Value: "generate", Label: "Generate a new token"},
		}
		defaultChoice = "1"
	}

	for {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  Choose how to set up the Relay Auth Token:")
		fmt.Fprintln(out)
		for _, choice := range choices {
			fmt.Fprintf(out, "    %s) %s\n", choice.Number, choice.Label)
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "  Enter [%s-%s] (default %s, or type 'back'/'exit'): ", choices[0].Number, choices[len(choices)-1].Number, defaultChoice)

		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.ToLower(strings.TrimSpace(line))
		switch line {
		case "":
			line = defaultChoice
		case "back":
			return "", errSetupBack
		case "exit":
			return "", errSetupExit
		}

		for _, choice := range choices {
			if line == choice.Number || line == choice.Value {
				return choice.Value, nil
			}
		}
		renderSetupErrorLine(out, "Invalid choice. Please enter one of the listed options.")
	}
}
