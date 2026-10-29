package main

import (
	"bufio"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

// The phone question (choosePhone, `ha-nova setup confirm-phone list`).
const (
	phoneWhoseQuestion = "Whose phone should confirm on this computer?"
	phoneWhoseHeader   = "Pick the phone of the person who uses this computer; it gets one notification. Only administrators' phones are listed."
	phoneWhoseMissing  = "Someone's phone missing? In Home Assistant, open Settings → People and pick that person (not there? Add them). Choose their user under “Allow person to login”, turn on “Administrator”, and pick their phone under Track device. Administrators can change everything in Home Assistant. Their Home Assistant app must be signed in to their own account. Then run ha-nova setup confirm-phone."
)

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

// choosePhone asks whose phone should confirm on this computer. Setup can't
// tell: only Home Assistant's owner signs in there, and the owner also sets
// up other people's computers. So every administrator's phone is a choice
// with its person, in name order, none proposed or first (arrow menu,
// numbered without a TTY). ok=false: skipped. Skip is the default: Enter
// never links a phone (that sends a notification).
func choosePhone(reader *bufio.Reader, out io.Writer, home phoneHome) (phoneTarget, bool) {
	all := home.eligible("")
	fmt.Fprintln(out, "  "+phoneWhoseHeader)
	for _, name := range home.shared {
		fmt.Fprintln(out, "  "+phoneSharedLine(name))
	}
	fmt.Fprintln(out, "  "+phoneWhoseMissing)
	spec := setupMenuSpec{
		Title:        phoneWhoseQuestion,
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
