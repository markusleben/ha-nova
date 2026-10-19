package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// choosePhone: the signed-in user's only phone is proposed; otherwise every
// administrator's phone is a choice (arrow menu, numbered without a TTY).
// ok=false: skipped. Skip is the default everywhere: Enter never links a
// phone (that sends a notification).
func choosePhone(reader *bufio.Reader, out io.Writer, home phoneHome, signedIn string) (phoneTarget, bool) {
	all := home.eligible("")
	if own := home.eligible(signedIn); signedIn != "" && len(own) == 1 {
		switch promptOwnPhone(reader, out, own[0]) {
		case "use":
			return own[0], true
		case "skip":
			return phoneTarget{}, false
		}
	}
	fmt.Fprintln(out, "  "+phoneListHeader)
	for _, name := range home.shared {
		fmt.Fprintln(out, "  "+phoneSharedLine(name))
	}
	spec := setupMenuSpec{
		Title:        "Which phone should confirm HA NOVA actions?",
		Prompt:       "Use ↑/↓ or j/k, Enter to select",
		DefaultValue: "skip",
	}
	for i, t := range all {
		spec.Options = append(spec.Options, setupMenuOption{Value: strconv.Itoa(i), Label: fmt.Sprintf("%s (%s)", terminalSafeName(t.Name), terminalSafeName(t.Person))})
	}
	spec.Options = append(spec.Options, setupMenuOption{Value: "skip", Label: "Skip for now"})
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		fmt.Fprintln(out, "  "+spec.Title)
		listPhones(out, all, nil)
		for {
			answer, err := promptLineWithOptions(reader, out, "Type the number of your phone, or press Enter to skip", "", false)
			answer = strings.TrimSpace(answer)
			if err != nil || answer == "" {
				return "skip", nil
			}
			if n, err := strconv.Atoi(answer); err == nil && n >= 1 && n <= len(all) {
				return strconv.Itoa(n - 1), nil
			}
			fmt.Fprintf(out, "  Type a number from 1 to %d, or press Enter to skip.\n", len(all))
		}
	})
	if n, convErr := strconv.Atoi(answer); err == nil && convErr == nil && n >= 0 && n < len(all) {
		return all[n], true
	}
	return phoneTarget{}, false
}

// promptOwnPhone returns "use", "other", or "skip".
func promptOwnPhone(reader *bufio.Reader, out io.Writer, own phoneTarget) string {
	name, person := terminalSafeName(own.Name), terminalSafeName(own.Person)
	spec := setupMenuSpec{
		Title:  fmt.Sprintf("Use “%s” (%s) for HA NOVA confirmations?", name, person),
		Prompt: "Use ↑/↓ or j/k, Enter to select",
		Options: []setupMenuOption{
			{Value: "use", Label: fmt.Sprintf("Yes, use “%s”", name)},
			{Value: "other", Label: "Choose another phone"},
			{Value: "skip", Label: "Skip for now"},
		},
		DefaultValue: "skip", // linking sends a notification: only on an explicit yes, as on the setup page
	}
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		label := fmt.Sprintf("Use “%s” (%s) for HA NOVA confirmations? Type 'yes' to use it, 'other' for another phone, or press Enter to skip", name, person)
		answer, err := promptLineWithOptions(reader, out, label, "", false)
		switch answer = strings.ToLower(strings.TrimSpace(answer)); {
		case err != nil:
			return "skip", nil
		case answer == "y" || answer == "yes" || answer == "use":
			return "use", nil
		case answer == "other":
			return "other", nil
		}
		return "skip", nil
	})
	if err != nil {
		return "skip"
	}
	return answer
}
