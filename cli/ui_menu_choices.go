package main

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// promptUninstallMode: proceed=false means cancel (the Cancel choice or
// Ctrl+C in the menu).
func promptUninstallMode(out io.Writer, defaultMode uninstallMode) (uninstallMode, bool, error) {
	spec := setupMenuSpec{
		Title:  "Choose uninstall mode:",
		Prompt: "Use ↑/↓ or j/k, Enter to select, Ctrl+C to cancel",
		Options: []setupMenuOption{
			{Value: "standard", Label: "Standard remove"},
			{Value: "purge", Label: "Full purge"},
			{Value: "cancel", Label: "Cancel"},
		},
		DefaultValue: "standard",
	}
	if defaultMode == uninstallModePurge {
		spec.DefaultValue = "purge"
	}
	answer, err := promptSetupMenu(nil, out, spec, func() (string, error) {
		return promptUninstallModeFromLine(spec.DefaultValue)
	})
	switch {
	case errors.Is(err, errSetupExit):
		return uninstallModeStandard, false, nil
	case err != nil:
		return uninstallModeStandard, false, err
	case answer == "standard":
		return uninstallModeStandard, true, nil
	case answer == "purge":
		return uninstallModePurge, true, nil
	default:
		return uninstallModeStandard, false, nil
	}
}

func promptUninstallModeFromLine(defaultValue string) (string, error) {
	number := "1"
	if defaultValue == "purge" {
		number = "2"
	}
	answer, err := promptLine("Choose uninstall mode: 1) Standard remove  2) Full purge  3) Cancel", number)
	if err != nil {
		return "", err
	}
	switch strings.TrimSpace(strings.ToLower(answer)) {
	case "", "1", "standard", "standard remove":
		return "standard", nil
	case "2", "purge", "full", "full purge":
		return "purge", nil
	default:
		return "cancel", nil
	}
}

// promptSetupDeviceVerifyRetry: nil retries; errSetupBack pairs again.
func promptSetupDeviceVerifyRetry(reader *bufio.Reader, out io.Writer) error {
	spec := setupMenuSpec{
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit",
		AllowBack:    true,
		DefaultValue: "retry",
		Options: []setupMenuOption{
			{Value: "retry", Label: "Retry now"},
			{Value: "back", Label: "Pair again"},
		},
	}
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		_, err := promptWizardLineFromReader(reader, out, "Press Enter to retry, or type 'back' to pair again", "")
		return "retry", err
	})
	if err == nil && answer == "back" {
		return errSetupBack
	}
	return err
}

// promptWizardYesNoMenu asks a wizard yes/no question with the arrow menu;
// without one it falls back to the [y/N] line.
func promptWizardYesNoMenu(reader *bufio.Reader, out io.Writer, title, yesLabel, noLabel string, defaultYes bool) (bool, error) {
	spec := setupMenuSpec{
		Title:        title,
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit",
		AllowBack:    true,
		DefaultValue: "no",
		Options:      []setupMenuOption{{Value: "yes", Label: yesLabel}, {Value: "no", Label: noLabel}},
	}
	if defaultYes {
		spec.DefaultValue = "yes"
	}
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		yes, err := promptWizardYesNoFromReader(reader, out, title, defaultYes)
		if yes {
			return "yes", err
		}
		return "no", err
	})
	return err == nil && answer == "yes", err
}
