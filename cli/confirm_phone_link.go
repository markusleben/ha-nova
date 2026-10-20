package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Linking the phone that confirms (spec §7.9). No broadcast: the link
// question goes to the one phone the user picked, and only a tap there,
// from that phone's own administrator, links it. Changing or unlinking is
// confirmed on the phone linked now, or in the window where none can answer.

// setupSignedInUser is the Home Assistant user who signed in during this
// setup run (setup_signin_pairing.go): the wizard proposes their phone.
var setupSignedInUser string

// phoneLinkBudget bounds a run without a terminal (an AI agent's command):
// both asks together stay under the common 120 s command limit.
var phoneLinkBudget = cliWindowWait

const confirmPhoneUsage = `ha-nova setup confirm-phone ["<phone name>" | list | off]`

const (
	phoneLinkButton = "Link"
	phoneNoneFound  = "No phone with the Home Assistant app was found for a Home Assistant administrator here. To confirm on your phone: install the Home Assistant app, sign in with your own Home Assistant account, then run ha-nova setup confirm-phone. If your account isn't an administrator, ask the Home Assistant owner to make it one. Until then HA NOVA asks in a window on this computer, in your AI app's form, or with a one-time code in chat."
	phoneOwnerToken = "This NOVA App signs in to Home Assistant as the owner, so a tap on a phone can't be told apart from HA NOVA itself. Phone confirmation isn't available with this App. Nothing changed."
	phoneReadFailed = "HA NOVA can't read this Home Assistant's phones right now. Nothing changed; try again in a moment."
	phoneListHeader = "Pick your own phone; it gets one notification. Only administrators' phones are listed (yours missing? ask the owner to make you an administrator)."
)

func runConfirmPhoneSetting(paths runtimePaths, args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: " + confirmPhoneUsage + "\n\nWhere no confirmation window can open, HA NOVA asks one phone you link (Home Assistant\napp) to confirm unlocks and deletes. Only that phone ever gets these confirmations.\n\n  (nothing)     pick your phone from a list, then tap “Link” on it\n  \"<name>\"      link that phone (list shows the names)\n  list          show the phones and which one is linked; sends nothing\n  off           stop asking on the phone (you confirm that on the phone)")
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
	run := phoneLinkRun{interactive: isInteractiveTTY() && stdoutIsInteractiveTTY()}
	switch {
	case len(args) == 0 || args[0] == "on":
	case args[0] == "off":
		return unlinkPhone(s, os.Stdout)
	case args[0] == "list":
		run.interactive, run.listOnly = false, true
	default:
		run.pick = args[0]
	}
	return linkPhone(s, bufio.NewReader(os.Stdin), os.Stdout, run)
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
	listOnly    bool   // list: show, never send
	pick        string // a phone's name or list number from the command line
	signedIn    string // whose phone to propose
	offer       bool   // the wizard: quiet where there is nothing to do
}

func linkPhone(s *relaySession, reader *bufio.Reader, out io.Writer, run phoneLinkRun) int {
	start := time.Now()
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
	prev := links[home.owner]
	current, status := home.status(links, nil)
	run.listOnly = run.listOnly || !run.interactive && run.pick == "" && !run.offer
	switch {
	case status == phoneReady && (run.offer || run.listOnly):
		fmt.Fprintf(out, "  Linked now: “%s” confirms HA NOVA actions from “%s”.\n", terminalSafeName(current.targets[0].Name), phoneHost())
		if run.offer {
			return 0
		}
		fmt.Fprintln(out, "  Changing it asks that phone first: the user taps “Move” there, then “Link” on the new phone.")
	case status == phoneMissing:
		fmt.Fprintln(out, "  "+home.missingLine(prev, current.why))
		if !current.why.replaceable() && !run.listOnly {
			return 1 // fixed in Home Assistant first; the phone linked now still decides
		}
	case status == phoneLegacy && run.listOnly:
		fmt.Fprintln(out, "  HA NOVA now asks on one phone you choose, and none is chosen on this computer yet.")
	case run.offer:
		renderSetupParagraph(out, "Optional: your phone can confirm unlocks and deletes when no window can open here (for example over SSH).")
	}
	all := home.eligible("")
	if len(all) == 0 {
		for _, name := range home.shared {
			fmt.Fprintln(out, "  "+phoneSharedLine(name))
		}
		if len(home.shared) == 0 {
			fmt.Fprintln(out, "  "+phoneNoneFound)
		}
		return 1
	}
	var chosen phoneTarget
	var ok bool
	switch {
	case run.pick != "":
		if chosen, ok = home.pickPhone(out, all, run.pick); !ok {
			return 1
		}
	case run.listOnly:
		fmt.Fprintln(out, "  Which phone should confirm HA NOVA actions? "+phoneListHeader)
		listPhones(out, all, home.shared)
		fmt.Fprintln(out, "  Nothing was sent. To link one: tell the user to watch that phone for “"+phoneLinkTitle+"” and to expand it and tap “Link”, then run ha-nova setup confirm-phone \"<phone name>\" (it waits up to 90 seconds).")
		return 0
	default:
		if chosen, ok = choosePhone(reader, out, home, run.signedIn); !ok {
			fmt.Fprintln(out, "  No phone linked. You can link one later: ha-nova setup confirm-phone")
			return 0
		}
	}
	name := terminalSafeName(chosen.Name)
	if status == phoneReady && current.targets[0].DeviceID == chosen.DeviceID && current.confirmer == chosen.UserID {
		fmt.Fprintf(out, "  “%s” is already the phone that confirms HA NOVA actions from “%s”.\n", name, phoneHost())
		return 0
	}
	// Without a terminal both asks share one budget; the first gets two thirds.
	stepWait, deadline := windowWait, time.Time{}
	if !run.interactive {
		deadline = start.Add(phoneLinkBudget)
		stepWait = min(windowWait, phoneLinkBudget*2/3)
	}
	if !allowPhoneChange(s, out, home, links, fmt.Sprintf("Link “%s” to confirm HA NOVA actions from “%s”?", name, phoneHost()), name, false, stepWait) {
		return 1
	}
	linkWait := windowWait
	if !deadline.IsZero() {
		linkWait = min(windowWait, time.Until(deadline))
	}
	if linkWait < windowMinWait {
		fmt.Fprintln(out, "  Nothing linked: time ran out before the new phone could be asked. Run ha-nova setup confirm-phone again.")
		return 1
	}
	if result := sendPhoneLink(s, out, home, links, prev, chosen, linkWait); result != phoneLinked {
		return 1
	}
	return 0
}

// phoneLinkResult: how a link question to a chosen phone ended.
type phoneLinkResult int

const (
	phoneLinked       phoneLinkResult = iota
	phoneLinkUnread                   // the re-check after the tap failed
	phoneLinkChanged                  // the phone or its user changed meanwhile
	phoneLinkRejected                 // the tap came from another user
	phoneLinkDeclined                 // cancelled on the phone
	phoneLinkFailed                   // the notification couldn't be sent or heard
	phoneLinkTimeout                  // no tap in time
	phoneLinkNotSaved                 // config.json couldn't take the link
)

// sendPhoneLink sends the link question to chosen alone, waits for its
// tap, re-checks the phone, and stores the link while this home's entry is
// still prev. It prints what happened to out, in the terminal's words,
// before the phone gets the result.
func sendPhoneLink(s *relaySession, out io.Writer, home phoneHome, links map[string]phoneEntry, prev phoneEntry, chosen phoneTarget, wait time.Duration) phoneLinkResult {
	name := terminalSafeName(chosen.Name)
	ask := phoneSetup{owner: home.owner, relayUser: home.relay, confirmer: chosen.UserID, targets: []phoneTarget{chosen}}
	retry := " Run ha-nova setup confirm-phone again to retry."
	answer, reply := askPhone(context.Background(), s, ask, "ha-nova-link", phoneLinkAsk(), wait, os.Stderr)
	defer func() { // only the first say counts: "Linked" below wins
		_, line := phoneNothing(answer)
		reply.say("Not linked", line)
	}()
	say := func(result phoneLinkResult, line string) phoneLinkResult {
		fmt.Fprintln(out, "  "+line)
		return result
	}
	switch answer {
	case phoneConfirmed:
		again, err := lookupPhones(s, links)
		if err != nil {
			return say(phoneLinkUnread, fmt.Sprintf("Nothing linked: HA NOVA couldn't re-check “%s” in Home Assistant after the tap.%s", name, retry))
		}
		if !slices.ContainsFunc(again.eligible(""), func(t phoneTarget) bool { return t.DeviceID == chosen.DeviceID && t.UserID == chosen.UserID }) {
			return say(phoneLinkChanged, "Nothing linked: the phone or its Home Assistant user changed while HA NOVA was asking."+retry)
		}
	case phoneRejected:
		return say(phoneLinkRejected, fmt.Sprintf("Nothing linked: the tap came from a phone signed in to Home Assistant as someone other than %s. Link a phone signed in with your own account.", terminalSafeName(chosen.Person)))
	case phoneDeclined:
		return say(phoneLinkDeclined, "Nothing linked: it was cancelled on the phone."+retry)
	case phoneFailed:
		return say(phoneLinkFailed, "Nothing linked: HA NOVA couldn't send the notification, or couldn't listen for the tap through the NOVA App. Check that the phone's Home Assistant app is signed in and can reach Home Assistant."+retry)
	default:
		return say(phoneLinkTimeout, "Nothing linked: “"+phoneLinkButton+"” was not tapped in time (expand the notification to see it)."+retry)
	}
	link := &phoneLink{Service: chosen.Service, DeviceID: chosen.DeviceID, UserID: chosen.UserID, Name: terminalSafeName(chosen.Name)}
	if err := setPhoneLink(s.paths, home.owner, prev, link); err != nil {
		return say(phoneLinkNotSaved, fmt.Sprintf("Nothing linked: %s.", err))
	}
	say(phoneLinked, fmt.Sprintf("Linked “%s”. Only “%s” gets HA NOVA confirmations from “%s”.", name, name, phoneHost()))
	reply.say("Linked to HA NOVA", "Confirmations from “"+phoneHost()+"” now come to you.")
	return phoneLinked
}

// unlinkPhone: a downgrade (the next channel is the form or chat), so it is
// confirmed on the linked phone, or in the window when that phone can never
// answer again; never without either.
func unlinkPhone(s *relaySession, out io.Writer) int {
	links, readable := phoneLinks(s.paths)
	home, err := lookupPhones(s, links)
	if !readable || err != nil {
		fmt.Fprintln(out, "  "+phoneReadFailed)
		return 1
	}
	prev := links[home.owner]
	current, status := home.status(links, nil)
	switch status {
	case phoneNone:
		fmt.Fprintln(out, "  No phone is linked for this Home Assistant on this computer. Nothing changed.")
		return 0
	case phoneLegacy:
		fmt.Fprintln(out, "  HA NOVA now asks on one phone you choose. Link yours first (ha-nova setup confirm-phone); you can then turn it off from that phone. Nothing changed.")
		return 1
	case phoneMissing:
		fmt.Fprintln(out, "  "+home.missingLine(prev, current.why))
	}
	if !allowPhoneChange(s, out, home, links, "Stop confirming HA NOVA actions on your linked phone? HA NOVA then asks in a window, in your AI app's form, or with a one-time code in chat that your AI assistant can see too.", "", true, windowWait) {
		return 1
	}
	if err := setPhoneLink(s.paths, home.owner, prev, nil); err != nil {
		fmt.Fprintf(out, "  Nothing changed: %s.\n", err)
		return 1
	}
	fmt.Fprintf(out, "  Phone confirmation is off: no phone gets HA NOVA confirmations from “%s”.\n", phoneHost())
	return 0
}

// allowPhoneChange asks whoever may approve changing this home's phone: the
// linked phone while it can be asked; the window where the linked one can
// never answer again (removed, its user no longer an administrator, or now
// the Relay's user: a model can cause each, so never by a tap alone). With
// no phone linked yet (or #719's pin) the window asks where one can open;
// where none can, the first link is its tap alone (trust on first use).
func allowPhoneChange(s *relaySession, out io.Writer, home phoneHome, links map[string]phoneEntry, question, newPhone string, unlink bool, wait time.Duration) bool {
	current, status := home.status(links, nil)
	question += "\n\n" + fmt.Sprintf("Requested on “%s” (by you, or your AI assistant). Confirm only if you want this.", phoneHost())
	switch {
	case status == phoneReady:
		old := terminalSafeName(current.targets[0].Name)
		fmt.Fprintf(out, "  First confirm on the phone linked now, “%s”.\n", old)
		answer, reply := askPhone(context.Background(), s, current, "ha-nova-setting", phoneChangeAsk(newPhone), wait, os.Stderr)
		defer func() {
			prefix, line := phoneNothing(answer)
			reply.say(prefix, line)
		}()
		if answer == phoneConfirmed {
			still, unread := phoneStillLinked(s, current)
			switch {
			case still && unlink:
				reply.say("Approved", "Confirmations from “"+phoneHost()+"” stop coming to you; the computer shows the result.")
				return true
			case still:
				reply.say("Approved", sayText(newPhone, true)+" must confirm next; the computer shows the result.")
				return true
			case unread:
				fmt.Fprintf(out, "  Nothing changed: HA NOVA couldn't re-check “%s” in Home Assistant after the tap. Run this again.\n", old)
			default:
				fmt.Fprintf(out, "  Nothing changed: “%s” or its Home Assistant user changed while HA NOVA was asking. Run this again.\n", old)
			}
			return false
		}
		switch answer {
		case phoneDeclined:
			fmt.Fprintf(out, "  Nothing changed: it was cancelled on “%s”.\n", old)
		case phoneRejected:
			fmt.Fprintf(out, "  Nothing changed: the tap came from a phone signed in to Home Assistant as someone other than the person who linked “%s”.\n", old)
		default:
			fmt.Fprintf(out, "  Nothing changed: it was not confirmed on “%s” in time, or the notification could not be sent. If that phone is lost, remove it in Home Assistant (Settings → Devices & services → Mobile App), then run this again.\n", old)
		}
		return false
	case status == phoneMissing && !current.why.replaceable():
		return false // missingLine said why and what to do
	}
	windowOnly := unlink || status == phoneMissing
	if windowOff(s.paths) || !windowAvailable() {
		if windowOnly {
			fmt.Fprintf(out, "  No confirmation window can open here (or windows are turned off), so HA NOVA can't check that this is you. Nothing changed. Run this at the computer's desktop, where a confirmation window opens, or remove \"%s\" from %s yourself.\n", phoneSettingKey, s.paths.ConfigFile)
			return false
		}
		return true // the first link: only the tap on the chosen phone links it
	}
	if newPhone != "" {
		fmt.Fprintf(out, "  A confirmation window opens on this computer first; then “%s” gets the link question.\n", newPhone)
	}
	switch askWindow(context.Background(), question, wait) {
	case windowConfirmed:
		return true
	case windowFailed:
		if !windowOnly {
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
	for _, t := range all {
		if phoneNameKey(t.Name) == phoneNameKey(pick) {
			return t, true // names are unique: a shared one is never listed
		}
	}
	if n, err := strconv.Atoi(pick); err == nil && n >= 1 && n <= len(all) {
		return all[n-1], true
	}
	for _, t := range h.phones {
		if phoneNameKey(t.Name) == phoneNameKey(pick) {
			fmt.Fprintf(out, "  “%s” belongs to %s, who isn't a Home Assistant administrator (or is HA NOVA's own user). Only an administrator's phone can confirm unlocks and deletes; the Home Assistant owner can make %s an administrator. Nothing changed.\n", terminalSafeName(t.Name), terminalSafeName(t.Person), terminalSafeName(t.Person))
			return phoneTarget{}, false
		}
	}
	for _, name := range h.shared {
		if phoneNameKey(name) == phoneNameKey(pick) {
			fmt.Fprintln(out, "  "+phoneSharedLine(name))
			return phoneTarget{}, false
		}
	}
	fmt.Fprintf(out, "  No phone is called “%s”. These phones can confirm HA NOVA actions:\n", terminalSafeName(pick))
	listPhones(out, all, h.shared)
	return phoneTarget{}, false
}

func phoneNameKey(name string) string {
	return strings.ToLower(strings.NewReplacer("’", "'", "‘", "'").Replace(strings.TrimSpace(name)))
}

func listPhones(out io.Writer, phones []phoneTarget, shared []string) {
	for i, t := range phones {
		fmt.Fprintf(out, "    %d) %s (%s)\n", i+1, terminalSafeName(t.Name), terminalSafeName(t.Person))
	}
	for _, name := range shared {
		fmt.Fprintln(out, "  "+phoneSharedLine(name))
	}
}
