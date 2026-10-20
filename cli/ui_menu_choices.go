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
// offer.offered; "Remove" is disabled with purgeBlocked as its reason when a
// full removal can't run here, and Cancel is then the default. proceed=false
// means cancel (Cancel or Ctrl+C).
func promptUninstallChoice(out io.Writer, defaultMode uninstallMode, offer uninstallAppOffer, purgeBlocked string) (uninstallChoice, bool, error) {
	options := []setupMenuOption{{Value: "purge", Label: "Remove", Detail: "Skills, settings, and this computer's pairing. The NOVA App stays.",
		Disabled: purgeBlocked != "", DisabledReason: purgeBlocked}}
	if offer.offered && purgeBlocked == "" {
		options = append(options, setupMenuOption{Value: "app", Label: "Remove, and remove the NOVA App too",
			Detail: "No other computer uses it. You sign in as the Home Assistant owner."})
	}
	options = append(options,
		setupMenuOption{Value: "standard", Label: "Remove, but keep the pairing for a reinstall"},
		setupMenuOption{Value: "cancel", Label: "Cancel"},
	)
	spec := setupMenuSpec{
		Title:        "Remove HA NOVA from this computer?",
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Ctrl+C to cancel",
		Options:      options,
		DefaultValue: string(defaultMode),
		Note:         offer.note,
	}
	if purgeBlocked != "" {
		// The user's choice can't run (or they just said no to the keyring
		// window): Enter must change nothing.
		spec.DefaultValue = "cancel"
	}
	offerApp := offer.offered && purgeBlocked == ""
	answer, err := promptSetupMenu(nil, out, spec, func() (string, error) {
		if offer.note != "" {
			fmt.Fprintf(out, "  %s\n", offer.note)
		}
		if purgeBlocked != "" {
			fmt.Fprintf(out, "  Remove: not available here (%s).\n", purgeBlocked)
		}
		return promptUninstallModeFromLine(spec.DefaultValue, offerApp, purgeBlocked == "")
	})
	switch {
	case errors.Is(err, errSetupExit):
		return uninstallChoice{}, false, nil
	case err != nil:
		return uninstallChoice{}, false, err
	case answer == "purge" && purgeBlocked == "":
		return uninstallChoice{mode: uninstallModePurge}, true, nil
	case answer == "app" && offerApp:
		return uninstallChoice{mode: uninstallModePurge, removeApp: true}, true, nil
	case answer == "standard":
		return uninstallChoice{mode: uninstallModeStandard}, true, nil
	default:
		return uninstallChoice{}, false, nil
	}
}

// promptUninstallModeFromLine is the menu without arrow keys.
func promptUninstallModeFromLine(defaultValue string, offerApp, purgeAllowed bool) (string, error) {
	var values, labels []string
	add := func(value, label string) {
		values = append(values, value)
		labels = append(labels, fmt.Sprintf("%d) %s", len(values), label))
	}
	if purgeAllowed {
		add("purge", "Remove")
	}
	if offerApp {
		add("app", "Remove, and remove the NOVA App too")
	}
	add("standard", "Remove, but keep the pairing for a reinstall")
	add("cancel", "Cancel")
	answer, err := promptLine("Remove HA NOVA from this computer? "+strings.Join(labels, "  "), strconv.Itoa(max(slices.Index(values, defaultValue), 0)+1))
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

// promptWizardYesNoMenu asks a wizard yes/no question with the arrow menu
// (Esc goes back); without one it falls back to the [y/N] line.
func promptWizardYesNoMenu(reader *bufio.Reader, out io.Writer, title, yesLabel, noLabel string, defaultYes bool) (bool, error) {
	return promptYesNoMenuWith(reader, out, title, yesLabel, noLabel, defaultYes, true)
}

// promptYesNoMenu is the same question outside the wizard: no Esc, Ctrl+C
// cancels (errSetupExit).
func promptYesNoMenu(reader *bufio.Reader, out io.Writer, title, yesLabel, noLabel string, defaultYes bool) (bool, error) {
	return promptYesNoMenuWith(reader, out, title, yesLabel, noLabel, defaultYes, false)
}

func promptYesNoMenuWith(reader *bufio.Reader, out io.Writer, title, yesLabel, noLabel string, defaultYes, wizard bool) (bool, error) {
	spec := setupMenuSpec{
		Title:        title,
		Prompt:       "Use ↑/↓ or j/k, Enter to select, Ctrl+C to cancel",
		DefaultValue: "no",
		Options:      []setupMenuOption{{Value: "yes", Label: yesLabel}, {Value: "no", Label: noLabel}},
	}
	if wizard {
		spec.Prompt, spec.AllowBack = "Use ↑/↓ or j/k, Enter to select, Esc to go back, Ctrl+C to exit", true
	}
	if defaultYes {
		spec.DefaultValue = "yes"
	}
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		yes, err := promptYesNoWithOptions(reader, out, title, defaultYes, wizard)
		if yes {
			return "yes", err
		}
		return "no", err
	})
	return err == nil && answer == "yes", err
}
