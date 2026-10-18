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
