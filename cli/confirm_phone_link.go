package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Linking the phone that confirms (spec §7.9). No broadcast: the link
// question goes to the one phone the user picked, and only a tap there,
// from that phone's own administrator, links it. Changing or unlinking is
// confirmed on the phone linked now, or in the window where none is.

// setupSignedInUser is the Home Assistant user who signed in during this
// setup run (setup_signin_pairing.go): the wizard proposes their phones.
var setupSignedInUser string

const confirmPhoneUsage = `ha-nova setup confirm-phone ["<phone name>" | off]`

const (
	phoneLinkButton = "Yes, link"
	phoneNoneFound  = "No phone with the Home Assistant app was found for a Home Assistant administrator here. To confirm on your phone: install the Home Assistant app, sign in with your own Home Assistant account (an administrator), then run ha-nova setup confirm-phone. Until then HA NOVA asks in a window on this computer, in your AI app's form, or with a one-time code in chat."
	phoneOwnerToken = "This HA NOVA Relay signs in to Home Assistant as the owner, so a tap on a phone can't be told apart from HA NOVA itself. Phone confirmation isn't available with this Relay. Nothing changed."
	phoneReadFailed = "HA NOVA can't read this Home Assistant's phones right now. Nothing changed; try again in a moment."
)

func runConfirmPhoneSetting(paths runtimePaths, args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: " + confirmPhoneUsage + "\n\nWhere no confirmation window can open, HA NOVA asks one phone you link (Home Assistant\napp) to confirm unlocks and deletes. Only that phone ever gets these confirmations.\n\n  (no name)     pick your phone from a list, then tap \"Yes, link\" on it\n  \"<name>\"      link that phone (the list shows the names)\n  off           stop asking on the phone (you confirm that on the phone)")
		return 0
	}
	if len(args) > 1 {
		printHumanErr("Usage: %s", confirmPhoneUsage)
		return 1
	}
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		printHumanErr("cannot reach HA NOVA (run ha-nova setup first): %s", err)
		return 1
	}
	defer s.close()
	if len(args) == 1 && args[0] == "off" {
		return unlinkPhone(s, os.Stdout)
	}
	pick := ""
	if len(args) == 1 && args[0] != "on" {
		pick = args[0]
	}
	interactive := isInteractiveTTY() && stdoutIsInteractiveTTY()
	return linkPhone(s, bufio.NewReader(os.Stdin), os.Stdout, phoneLinkRun{interactive: interactive, pick: pick})
}

// offerPhoneLink is the setup wizard's step after a pairing in this run.
func offerPhoneLink(reader *bufio.Reader, out io.Writer, paths runtimePaths) {
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		return
	}
	defer s.close()
	linkPhone(s, reader, out, phoneLinkRun{interactive: true, signedIn: setupSignedInUser, offer: true})
}

type phoneLinkRun struct {
	interactive bool
	pick        string // a phone's name or list number from the command line
	signedIn    string // whose phones to propose
	offer       bool   // the wizard: quiet where there is nothing to do
}

func linkPhone(s *relaySession, reader *bufio.Reader, out io.Writer, run phoneLinkRun) int {
	links, readable := phoneLinks(s.paths)
	home, err := lookupPhones(s, links)
	if !readable || err != nil {
		if !run.offer {
			fmt.Fprintln(out, "  "+phoneReadFailed)
		}
		return 1
	}
	if home.relay == home.owner {
		if !run.offer {
			fmt.Fprintln(out, "  "+phoneOwnerToken)
		}
		return 1
	}
	current, status := home.status(links, nil)
	if run.offer && status == phoneReady {
		fmt.Fprintf(out, "  “%s” confirms HA NOVA actions from this computer.\n", terminalSafeName(current.targets[0].Name))
		return 0
	}
	if run.offer {
		renderSetupParagraph(out, "Confirm on your phone (optional): where no confirmation window can open on this computer, for example over SSH, HA NOVA asks your phone before it unlocks or deletes something. Only the phone you link ever gets these confirmations.")
	}
	all := home.eligible("")
	if len(all) == 0 {
		fmt.Fprintln(out, "  "+phoneNoneFound)
		return 1
	}
	var chosen phoneTarget
	var ok bool
	switch {
	case run.pick != "":
		if chosen, ok = home.pickPhone(out, all, run.pick); !ok {
			return 1
		}
	case !run.interactive:
		fmt.Fprintln(out, "  Which phone should confirm HA NOVA actions? These phones can:")
		listPhones(out, all)
		fmt.Fprintln(out, "  Nothing was sent. Link one by name: ha-nova setup confirm-phone \"<phone name>\" — then tap \"Yes, link\" on that phone.")
		return 0
	default:
		if chosen, ok = choosePhone(reader, out, home.eligible(run.signedIn), all, run.signedIn != ""); !ok {
			fmt.Fprintln(out, "  No phone linked. You can link one later: ha-nova setup confirm-phone")
			return 0
		}
	}
	name := terminalSafeName(chosen.Name)
	if status == phoneReady && current.targets[0].DeviceID == chosen.DeviceID && current.confirmer == chosen.UserID {
		fmt.Fprintf(out, "  “%s” is already the phone that confirms HA NOVA actions from this computer.\n", name)
		return 0
	}
	if !allowPhoneChange(s, out, home, links, fmt.Sprintf("Link “%s” to confirm HA NOVA actions from this computer?", name), false) {
		return 1
	}
	ask := phoneSetup{owner: home.owner, relayUser: home.relay, confirmer: chosen.UserID, targets: []phoneTarget{chosen}}
	message := fmt.Sprintf("Link this phone to HA NOVA on “%s”? Only this phone then gets HA NOVA's confirmations for unlocks and deletes from that computer. Tap %s only if you are setting this up right now.", phoneHost(), phoneLinkButton)
	switch askPhone(s, ask, "ha-nova-link", message, phoneLinkButton, windowWait) {
	case phoneConfirmed:
	case phoneRejected:
		fmt.Fprintf(out, "  Nothing linked: the answer came from a phone signed in to Home Assistant as someone other than %s.\n", terminalSafeName(chosen.Person))
		return 1
	case phoneDeclined:
		fmt.Fprintln(out, "  Nothing linked: it was cancelled on the phone.")
		return 1
	case phoneFailed:
		fmt.Fprintln(out, "  Nothing linked: HA NOVA couldn't send the notification. Check that the phone's Home Assistant app is signed in and can reach Home Assistant, then try again.")
		return 1
	default:
		fmt.Fprintln(out, "  Nothing linked: “Yes, link” was not tapped in time. Run ha-nova setup confirm-phone to try again.")
		return 1
	}
	link := &phoneLink{Service: chosen.Service, DeviceID: chosen.DeviceID, UserID: chosen.UserID, Name: terminalSafeName(chosen.Name)}
	if err := setPhoneLink(s.paths, home.owner, link); err != nil {
		fmt.Fprintf(out, "  Nothing linked: HA NOVA couldn't save its configuration (%s).\n", err)
		return 1
	}
	fmt.Fprintf(out, "  Linked “%s”. Only this phone gets HA NOVA confirmations from this computer.\n", name)
	return 0
}

// unlinkPhone: a downgrade (the next channel is the form or chat), so it
// is confirmed on the linked phone, or in the window when that phone was
// removed from Home Assistant; never by the tap alone.
func unlinkPhone(s *relaySession, out io.Writer) int {
	links, readable := phoneLinks(s.paths)
	home, err := lookupPhones(s, links)
	if !readable || err != nil {
		fmt.Fprintln(out, "  "+phoneReadFailed)
		return 1
	}
	switch _, status := home.status(links, nil); status {
	case phoneNone:
		fmt.Fprintln(out, "  No phone is linked for this Home Assistant on this computer. Nothing changed.")
		return 0
	case phoneLegacy:
		fmt.Fprintln(out, "  HA NOVA now asks on one phone you choose. Link yours first (ha-nova setup confirm-phone); you can then turn it off from that phone. Nothing changed.")
		return 1
	}
	if !allowPhoneChange(s, out, home, links, "Stop confirming HA NOVA actions on this phone? HA NOVA then asks in a window, in your AI app's form, or with a one-time code in chat that your AI assistant can see too.", true) {
		return 1
	}
	if err := setPhoneLink(s.paths, home.owner, nil); err != nil {
		fmt.Fprintf(out, "  Nothing changed: HA NOVA couldn't save its configuration (%s).\n", err)
		return 1
	}
	fmt.Fprintln(out, "  Phone confirmation is off: no phone gets HA NOVA confirmations from this computer.")
	return 0
}

// allowPhoneChange asks whoever may approve changing this home's phone: the
// linked phone while it can be asked; the window where no phone is linked
// yet (or #719's pin, or the linked one was removed from Home Assistant).
// A window that can't open lets a link go on to its tap, never an unlink.
func allowPhoneChange(s *relaySession, out io.Writer, home phoneHome, links map[string]phoneEntry, question string, unlink bool) bool {
	current, status := home.status(links, nil)
	question += "\n\n" + fmt.Sprintf("Requested by HA NOVA on “%s”. Confirm only if you want this.", phoneHost())
	switch {
	case status == phoneReady:
		fmt.Fprintf(out, "  First confirm on the phone linked now, “%s”.\n", terminalSafeName(current.targets[0].Name))
		if askPhone(s, current, "ha-nova-setting", question, "Confirm", windowWait) == phoneConfirmed {
			return true
		}
		fmt.Fprintln(out, "  Nothing changed: Confirm was not tapped on the linked phone in time, or the notification could not be sent.")
		return false
	case status == phoneMissing && !home.gone:
		name := ""
		if link := links[home.owner].link; link != nil {
			name = " (“" + terminalSafeName(link.Name) + "”)"
		}
		fmt.Fprintf(out, "  HA NOVA can't reach the phone linked now%s, so it can't ask there. Nothing changed. If that phone is lost or replaced, remove it in Home Assistant (Settings → Devices & services → Mobile App), then run this again.\n", name)
		return false
	}
	if windowOff(s.paths) || !windowAvailable() {
		if unlink {
			fmt.Fprintf(out, "  The linked phone is no longer in Home Assistant, and no confirmation window can open here, so HA NOVA can't check that this is you. Nothing changed. Link your new phone with ha-nova setup confirm-phone, or remove \"%s\" from %s yourself.\n", phoneSettingKey, s.paths.ConfigFile)
			return false
		}
		return true // the first link: only the tap on the chosen phone links it
	}
	switch askWindow(question, windowWait) {
	case windowConfirmed:
		return true
	case windowFailed:
		if !unlink {
			return true
		}
	}
	fmt.Fprintln(out, "  Nothing changed: the confirmation window was cancelled or got no answer.")
	return false
}

// eligible: the phones that may be linked — an administrator's, never the
// Relay user's. With signedIn, only that user's (none: everyone's).
func (h phoneHome) eligible(signedIn string) []phoneTarget {
	var out []phoneTarget
	for _, t := range h.phones {
		if t.UserID != h.relay && phoneAdmin(h.users[t.UserID]) && (signedIn == "" || t.UserID == signedIn) {
			out = append(out, t)
		}
	}
	return out
}

// pickPhone finds the phone named (or numbered) on the command line.
func (h phoneHome) pickPhone(out io.Writer, all []phoneTarget, pick string) (phoneTarget, bool) {
	if n, err := strconv.Atoi(pick); err == nil && n >= 1 && n <= len(all) {
		return all[n-1], true
	}
	for _, t := range all {
		if phoneNameKey(t.Name) == phoneNameKey(pick) {
			return t, true // names are unique: a shared one is never listed
		}
	}
	for _, t := range h.phones {
		if phoneNameKey(t.Name) == phoneNameKey(pick) {
			fmt.Fprintf(out, "  “%s” belongs to %s, who isn't a Home Assistant administrator (or is HA NOVA's own user). Only an administrator's phone can confirm unlocks and deletes. Nothing changed.\n", terminalSafeName(t.Name), terminalSafeName(t.Person))
			return phoneTarget{}, false
		}
	}
	fmt.Fprintf(out, "  No phone is called “%s”. These phones can confirm HA NOVA actions:\n", terminalSafeName(pick))
	listPhones(out, all)
	return phoneTarget{}, false
}

func phoneNameKey(name string) string {
	return strings.ToLower(strings.NewReplacer("’", "'", "‘", "'").Replace(strings.TrimSpace(name)))
}

func listPhones(out io.Writer, phones []phoneTarget) {
	for i, t := range phones {
		fmt.Fprintf(out, "    %d) %s — %s\n", i+1, terminalSafeName(t.Name), terminalSafeName(t.Person))
	}
}

// choosePhone: the signed-in user's only phone is proposed; otherwise every
// administrator's phones are a numbered list. ok=false: skipped.
func choosePhone(reader *bufio.Reader, out io.Writer, own, all []phoneTarget, signedIn bool) (phoneTarget, bool) {
	if signedIn && len(own) == 1 {
		answer, err := promptLineWithOptions(reader, out, fmt.Sprintf("Use “%s” for HA NOVA confirmations? Press Enter, type 'other' for another phone, or 'skip'", terminalSafeName(own[0].Name)), "", false)
		switch answer = strings.ToLower(strings.TrimSpace(answer)); {
		case err != nil:
			return phoneTarget{}, false
		case answer == "" || answer == "y" || answer == "yes":
			return own[0], true
		case answer != "other":
			return phoneTarget{}, false
		}
	}
	fmt.Fprintln(out, "  Which phone should confirm HA NOVA actions?")
	listPhones(out, all)
	for {
		answer, err := promptLineWithOptions(reader, out, "Type the number of your phone, or press Enter to skip", "", false)
		if err != nil || strings.TrimSpace(answer) == "" {
			return phoneTarget{}, false
		}
		if n, err := strconv.Atoi(strings.TrimSpace(answer)); err == nil && n >= 1 && n <= len(all) {
			return all[n-1], true
		}
	}
}

func phoneHost() string {
	if host, err := os.Hostname(); err == nil && terminalSafeName(host) != "" {
		return terminalSafeName(host)
	}
	return "this computer"
}
