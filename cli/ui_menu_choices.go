package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

type uninstallChoice struct {
	mode      uninstallMode
	removeApp bool
}

// promptUninstallChoice is uninstall's one question. "app" is listed only when
// offer.offered; proceed=false means cancel (the Cancel choice or Ctrl+C).
func promptUninstallChoice(out io.Writer, defaultMode uninstallMode, offer uninstallAppOffer) (uninstallChoice, bool, error) {
	options := []setupMenuOption{{Value: "purge", Label: "Remove", Detail: "Skills, settings, and this computer's connection. Your Home Assistant stays as it is."}}
	if offer.offered {
		options = append(options, setupMenuOption{Value: "app", Label: "Remove, and remove the NOVA App from Home Assistant", Detail: "This is the last computer using it. You sign in as the owner."})
	}
	options = append(options,
		setupMenuOption{Value: "standard", Label: "Remove, but keep this computer connected", Detail: "For a reinstall later."},
		setupMenuOption{Value: "cancel", Label: "Cancel"},
	)
	spec := setupMenuSpec{
		Title:        "Remove HA NOVA from this computer?",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Ctrl+C to cancel",
		Options:      options,
		DefaultValue: string(defaultMode),
		Note:         offer.note,
	}
	answer, err := promptSetupMenu(nil, out, spec, func() (string, error) {
		if offer.note != "" {
			fmt.Fprintf(out, "  %s\n", offer.note)
		}
		return promptUninstallModeFromLine(spec.DefaultValue, offer.offered)
	})
	switch {
	case errors.Is(err, errSetupExit):
		return uninstallChoice{}, false, nil
	case err != nil:
		return uninstallChoice{}, false, err
	case answer == "purge":
		return uninstallChoice{mode: uninstallModePurge}, true, nil
	case answer == "app" && offer.offered:
		return uninstallChoice{mode: uninstallModePurge, removeApp: true}, true, nil
	case answer == "standard":
		return uninstallChoice{mode: uninstallModeStandard}, true, nil
	default:
		return uninstallChoice{}, false, nil
	}
}

// promptUninstallModeFromLine is the menu without arrow keys.
func promptUninstallModeFromLine(defaultValue string, offerApp bool) (string, error) {
	values := []string{"purge", "standard", "cancel"}
	label := "Remove HA NOVA from this computer? 1) Remove  2) Remove, but keep this computer connected  3) Cancel"
	if offerApp {
		values = []string{"purge", "app", "standard", "cancel"}
		label = "Remove HA NOVA from this computer? 1) Remove  2) Remove, and remove the NOVA App from Home Assistant  3) Remove, but keep this computer connected  4) Cancel"
	}
	answer, err := promptLine(label, strconv.Itoa(slices.Index(values, defaultValue)+1))
	if err != nil {
		return "", err
	}
	if number, err := strconv.Atoi(strings.TrimSpace(answer)); err == nil && number >= 1 && number <= len(values) {
		return values[number-1], nil
	}
	return "cancel", nil
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
