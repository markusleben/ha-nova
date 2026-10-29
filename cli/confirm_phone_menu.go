package main

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// Asked when setup can't tell whose computer this is (whose).
const (
	phoneWhoseQuestion = "Whose phone should confirm on this computer?"
	phoneWhoseHeader   = "Pick the phone of the person who uses this computer; it gets one notification. Only administrators' phones are listed."
	phoneWhoseMissing  = "Someone's phone missing? In Home Assistant, open Settings → People and pick that person (not there? Add them). Choose their user under “Allow person to login”, turn on “Administrator”, and pick their phone under Track device. Their Home Assistant app must be signed in to their own account. Then run ha-nova setup confirm-phone."
)

// whose: Home Assistant's owner signed in, or nobody did. The owner sets up
// HA NOVA on other people's computers too, so whose computer this is isn't
// known: no phone is the user's own, every one is listed with its person
// and none is first or preselected.
func (h phoneHome) whose(signedIn string) bool { return signedIn == "" || signedIn == h.owner }

// sharedAdmin: the first name in shared that an administrator's phone
// carries (it could be linked but for the clash); "" when none.
func (h phoneHome) sharedAdmin() string {
	for _, name := range h.shared {
		for id, names := range h.sharedOf {
			if id != h.relay && phoneAdmin(h.users[id]) && slices.Contains(names, name) {
				return name
			}
		}
	}
	return ""
}

// ownFirst lists the phones that can be linked, the user's own first (own
// counts them); with whose, in name order and none own.
func (h phoneHome) ownFirst(signedIn string) (all []phoneTarget, own int) {
	if h.whose(signedIn) {
		return h.eligible(""), 0
	}
	for _, t := range h.eligible("") {
		if t.UserID == signedIn {
			all = slices.Insert(all, own, t)
			own++
		} else {
			all = append(all, t)
		}
	}
	return all, own
}

// choosePhone: with whose, every administrator's phone is a choice with its
// person, and the question is whose phone. Otherwise the user's only phone
// is proposed, else every administrator's phone is a choice, the user's own
// first; no own phone: why first. Arrow menu, numbered without a TTY.
// ok=false: skipped. Skip is the default everywhere: Enter never links a
// phone (that sends a notification).
func choosePhone(reader *bufio.Reader, out io.Writer, home phoneHome, signedIn string) (phoneTarget, bool) {
	whose := home.whose(signedIn)
	all, own := home.ownFirst(signedIn)
	if own == 1 {
		switch promptOwnPhone(reader, out, all[0]) {
		case "use":
			return all[0], true
		case "skip":
			return phoneTarget{}, false
		}
	}
	title := "Which phone should confirm HA NOVA actions?"
	switch {
	case whose:
		title = phoneWhoseQuestion
		fmt.Fprintln(out, "  "+phoneWhoseHeader)
	case own > 0:
		fmt.Fprintln(out, "  "+phoneListHeader)
	}
	for _, name := range home.shared {
		fmt.Fprintln(out, "  "+phoneSharedLine(name))
	}
	switch {
	case whose:
		fmt.Fprintln(out, "  "+phoneWhoseMissing)
	case own == 0 && len(home.sharedOf[signedIn]) == 0:
		fmt.Fprintln(out, "  "+phoneOwnMissing)
	}
	spec := setupMenuSpec{
		Title:        title,
		Prompt:       "Use ↑/↓ or j/k, Enter to select",
		DefaultValue: "skip",
	}
	for i, t := range all {
		spec.Options = append(spec.Options, setupMenuOption{Value: strconv.Itoa(i), Label: fmt.Sprintf("%s (%s)", terminalSafeName(t.Name), terminalSafeName(t.Person))})
	}
	spec.Options = append(spec.Options, setupMenuOption{Value: "skip", Label: "Skip for now"})
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		fmt.Fprintln(out, "  "+spec.Title)
		listPhones(out, all, nil, "")
		for {
			answer, err := promptLineWithOptions(reader, out, "Type the number of the phone, or press Enter to skip", "", false)
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
