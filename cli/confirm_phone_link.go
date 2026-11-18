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

// setupSignedInUser is Home Assistant's owner when they signed in during
// this setup run (setup_signin_pairing.go), for chat apps. The phone step
// never takes it for the person at this computer (choosePhone).
var setupSignedInUser string

// phoneLinkBudget bounds a run without a terminal (an AI agent's command):
// both asks together stay under the common 120 s command limit.
var phoneLinkBudget = cliWindowWait

const confirmPhoneUsage = `ha-nova setup confirm-phone ["<phone name>" | list | off]`

// confirmPhoneCommand names the profile a retry acts on; the bare command
// only where it resolves to the same profile (no --server or HA_NOVA_SERVER
// pick, which a different default_server would otherwise reroute).
func confirmPhoneCommand(profile string) string {
	if pick, _ := requestedServerSelection(); profile == "" || profile == defaultServerProfileName && pick == "" {
		return "ha-nova setup confirm-phone"
	}
	return "ha-nova setup confirm-phone --server " + profile
}

const (
	phoneNoneFound  = "No phone with the Home Assistant app was found for a Home Assistant administrator here. To confirm on your phone: install the Home Assistant app, sign in with your own Home Assistant account, then run ha-nova setup confirm-phone. If your account isn't an administrator, ask the Home Assistant owner to make it one. Until then, HA NOVA's answer to a risky change says where to confirm it."
	phoneOwnerToken = "This NOVA App signs in to Home Assistant as the owner, so a tap on a phone can't be told apart from HA NOVA itself. Phone confirmation isn't available with this App. Nothing changed."
	phoneReadFailed = "HA NOVA can't read this Home Assistant's phones right now. Nothing changed; try again in a moment."
	phoneLostLine   = "Lost that phone? Changing the link asks it first, so the Home Assistant owner removes this computer on the NOVA page instead. Pair it again with a new code, then run ha-nova setup confirm-phone \"<new phone>\"."
)

func runConfirmPhoneSetting(paths runtimePaths, args []string) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Println("Usage: " + confirmPhoneUsage + "\n\nWhere no confirmation window can open, HA NOVA asks one phone you link (Home Assistant\napp) to confirm unlocks and deletes. Only that phone ever gets these confirmations.\n\n  (nothing)     pick a phone from a list, then allow it on that phone\n  \"<name>\"      link that phone (list shows the names)\n  list          show the phones and which one is linked; sends nothing\n  off           stop asking on the phone from this computer (you confirm that on the phone,\n                or in a window; with the NOVA App's safety check on, the link in Home Assistant stays)")
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

// offerPhoneLink: setup's and pair's phone step. A computer whose window
// confirms, or one on the shared token, gets it after a pairing only, as in
// 0.32; a paired device that can't confirm in its window gets it on every run
// until a phone is linked (MAR-244). Without a terminal (reader nil) nothing
// is asked or read from Home Assistant: one line names the commands.
func offerPhoneLink(reader *bufio.Reader, out io.Writer, paths runtimePaths, pairedNow, device bool) {
	if !pairedNow && !device {
		return
	}
	if reader == nil {
		links, ok := phoneLinks(paths)
		linked := false
		for _, entry := range links {
			linked = linked || entry.link != nil
		}
		if device && (!ok || !linked) {
			fmt.Fprintln(out, "  "+phoneNextStep(activeServerProfile()))
		}
		return
	}
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		fmt.Fprintln(out, "  HA NOVA can't reach the NOVA App right now to offer a phone. Later: "+confirmPhoneCommand(activeServerProfile()))
		return
	}
	defer s.close()
	on, _ := s.gateOn() // as a gated send reads it: the first answer records that this App gates (gate_seen)
	confirms := false
	if on && s.gate.window && s.windowHere() { // the App's permission isn't a window here (SSH, window off)
		confirms = s.gateMe().window
	}
	if confirms && !pairedNow {
		return
	}
	linkPhone(s, reader, out, phoneLinkRun{interactive: true, offer: true, quiet: !pairedNow, pageOnly: on && !confirms})
}

// terminalReader: stdin when a person answers in this terminal, else nil
// (offerPhoneLink then only names the commands).
func terminalReader() *bufio.Reader {
	if isInteractiveTTY() && stdoutIsInteractiveTTY() {
		return bufio.NewReader(os.Stdin)
	}
	return nil
}

// phoneNextStep: the commands a bot passes on, word for word.
func phoneNextStep(profile string) string {
	return "No phone linked to this computer yet? To confirm risky changes on a phone, run " + confirmPhoneCommand(profile) + " list, then " + confirmPhoneCommand(profile) + " \"<phone name>\" and tap the button in the notification on that phone."
}

type phoneLinkRun struct {
	interactive bool
	listOnly    bool   // list: show, never send
	pick        string // a phone's name or list number from the command line
	offer       bool   // setup's or pair's offer (offerPhoneLink)
	quiet       bool   // offer: say nothing when a phone is linked already
	pageOnly    bool   // offer: without a phone, only the NOVA page confirms here
}

func linkPhone(s *relaySession, reader *bufio.Reader, out io.Writer, run phoneLinkRun) int {
	start, lang := time.Now(), s.homeLanguage()
	links, readable := phoneLinks(s.paths)
	home, err := lookupPhones(s, links)
	later := ""
	if run.offer { // never a silent skip
		later = " Later: " + confirmPhoneCommand(activeServerProfile())
	}
	if !readable || err != nil {
		fmt.Fprintln(out, "  "+phoneReadFailed+later)
		return 1
	}
	if home.relay == home.owner {
		fmt.Fprintln(out, "  "+phoneOwnerToken+later)
		return 1
	}
	prev := links[home.owner]
	current, status := home.status(links, nil)
	run.listOnly = run.listOnly || !run.interactive && run.pick == "" && !run.offer
	switch {
	case status == phoneReady && run.offer && run.quiet:
		return 0
	case status == phoneReady && (run.offer || run.listOnly):
		fmt.Fprintf(out, "  Linked now: “%s” confirms HA NOVA actions from “%s”.\n", terminalSafeName(current.targets[0].Name), phoneHost("en"))
		if run.offer {
			fmt.Fprintln(out, "  "+phoneLostLine)
			return 0
		}
		fmt.Fprintln(out, "  Changing it asks that phone first: tell the user to watch it for “"+confirmT(lang, "phone.move.title")+"”. "+phoneTapLine("en", current.targets[0].platform(), confirmT(lang, "phone.move.button"))+" Then the new phone gets “"+phoneLinkTitle(lang)+"”.")
	case status == phoneMissing:
		fmt.Fprintln(out, "  "+home.missingLine(prev, current.why))
		if !current.why.replaceable() && !run.listOnly {
			return 1 // fixed in Home Assistant first; the phone linked now still decides
		}
	case status == phoneLegacy && run.listOnly:
		fmt.Fprintln(out, "  HA NOVA now asks on one phone you choose, and none is chosen on this computer yet.")
	case run.offer:
		renderSetupParagraph(out, "Confirm risky changes (doors, alarm, deletes) on your phone? HA NOVA sends one notification to the phone you pick; tap “"+phoneLinkButton(lang)+"” there.")
	}
	all := home.eligible("")
	if len(all) == 0 {
		for _, name := range home.shared {
			fmt.Fprintln(out, "  "+phoneSharedLine(name))
		}
		if len(home.shared) == 0 {
			fmt.Fprintln(out, "  "+phoneNoneFound)
		}
		if run.pageOnly {
			phoneSkippedLines(out, s)
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
		fmt.Fprintln(out, "  "+phoneWhoseQuestion+" "+phoneWhoseHeader)
		listPhones(out, all, home.shared, phoneLinkButton(lang))
		fmt.Fprintln(out, "  "+phoneWhoseMissing)
		fmt.Fprintln(out, "  Nothing was sent. Ask the user whose phone should confirm on this computer and let them choose from this list; never pick one for them. Then tell them to watch that phone for “"+phoneLinkTitle(lang)+"” and to do what its line above says, and run ha-nova setup confirm-phone \"<phone name>\" (it waits up to 90 seconds).")
		return 0
	default:
		if chosen, ok = choosePhone(reader, out, home); !ok {
			fmt.Fprintln(out, "  No phone linked. You can link one later: "+confirmPhoneCommand(activeServerProfile()))
			if run.pageOnly {
				phoneSkippedLines(out, s)
			}
			return 0
		}
	}
	relayWait := windowWait
	if !run.interactive {
		relayWait = min(windowWait, time.Until(start.Add(phoneLinkBudget)))
	}
	if result, gated := linkPhoneAtRelay(s, out, lang, home, prev, chosen, relayWait); gated {
		return map[bool]int{true: 0, false: 1}[result == phoneLinked]
	}
	name := terminalSafeName(chosen.Name)
	if status == phoneReady && current.targets[0].DeviceID == chosen.DeviceID && current.confirmer == chosen.UserID {
		fmt.Fprintf(out, "  “%s” is already the phone that confirms HA NOVA actions from “%s”.\n", name, phoneHost("en"))
		return 0
	}
	// Without a terminal both asks share one budget; the first gets two thirds.
	stepWait, deadline := windowWait, time.Time{}
	if !run.interactive {
		deadline = start.Add(phoneLinkBudget)
		stepWait = min(windowWait, phoneLinkBudget*2/3)
	}
	if !allowPhoneChange(s, out, lang, home, links, phoneLinkQuestion(lang, name), name, false, stepWait) {
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
	if result := sendPhoneLink(s, out, lang, home, links, prev, chosen, linkWait); result != phoneLinked {
		return 1
	}
	return 0
}

// phoneLinkResult: how a link question to a chosen phone ended.
type phoneLinkResult int

const (
	phoneLinked       phoneLinkResult = iota
	phoneLinkUnread                   // a check in Home Assistant failed (before the send, or the re-check after the tap)
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
func sendPhoneLink(s *relaySession, out io.Writer, lang string, home phoneHome, links map[string]phoneEntry, prev phoneEntry, chosen phoneTarget, wait time.Duration) phoneLinkResult {
	name := terminalSafeName(chosen.Name)
	ask := phoneSetup{owner: home.owner, relayUser: home.relay, confirmer: chosen.UserID, targets: []phoneTarget{chosen}}
	retry := " Run ha-nova setup confirm-phone again to retry."
	answer, reply := askPhone(context.Background(), s, ask, "ha-nova-link", phoneLinkAsk(lang), wait, os.Stderr)
	defer func() { // only the first say counts: "Linked" below wins
		_, line := phoneNothing(lang, answer)
		reply.say(confirmT(lang, "outcome.not_linked"), line)
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
		person := terminalSafeName(chosen.Person)
		return say(phoneLinkRejected, fmt.Sprintf("Nothing linked: the tap came from a phone signed in to Home Assistant as someone other than %s. Sign in as %s in the Home Assistant app on “%s”.%s", person, person, name, retry))
	case phoneDeclined:
		return say(phoneLinkDeclined, "Nothing linked: it was cancelled on the phone."+retry)
	case phoneUnread:
		return say(phoneLinkUnread, "Nothing linked and nothing was sent: "+phoneUnreadLine)
	case phoneFailed:
		return say(phoneLinkFailed, "Nothing linked: HA NOVA couldn't send the notification, or couldn't listen for the tap through the NOVA App. Check that the phone's Home Assistant app is signed in and can reach Home Assistant."+retry)
	default:
		say(phoneLinkTimeout, "Nothing linked: “"+phoneLinkButton(lang)+"” was not tapped in time. Next time: "+phoneTapLine("en", chosen.platform(), phoneLinkButton(lang)))
		return say(phoneLinkTimeout, phoneNothingHint(chosen.platform())+retry)
	}
	link := &phoneLink{Service: chosen.Service, DeviceID: chosen.DeviceID, UserID: chosen.UserID, Name: terminalSafeName(chosen.Name), Platform: chosen.platform()}
	if err := setPhoneLink(s.paths, home.owner, prev, link); err != nil {
		return say(phoneLinkNotSaved, fmt.Sprintf("Nothing linked: %s.", err))
	}
	say(phoneLinked, fmt.Sprintf("Linked “%s”. Only “%s” gets HA NOVA confirmations from “%s”.", name, name, phoneHost("en")))
	reply.say(confirmT(lang, "outcome.linked"), confirmT(lang, "outcome.line.linked", "host", phoneHost(lang)))
	return phoneLinked
}

// allowPhoneChange asks whoever may approve changing this home's phone: the
// linked phone while it can be asked; the window where the linked one can
// never answer again (removed, its user no longer an administrator, or now
// the Relay's user: a model can cause each, so never by a tap alone). With
// no phone linked yet (or #719's pin) the window asks where one can open;
// where none can, the first link is its tap alone (trust on first use).
func allowPhoneChange(s *relaySession, out io.Writer, lang string, home phoneHome, links map[string]phoneEntry, question, newPhone string, unlink bool, wait time.Duration) bool {
	current, status := home.status(links, nil)
	question += "\n\n" + confirmT(lang, "settings.requested", "host", phoneHost(lang))
	deaf := status == phoneReady && s.phoneDeaf()
	if deaf {
		fmt.Fprintln(out, "  "+phoneDeafLine)
	}
	switch {
	case status == phoneReady && !deaf:
		old := terminalSafeName(current.targets[0].Name)
		fmt.Fprintf(out, "  First confirm on the phone linked now, “%s”.\n", old)
		answer, reply := askPhone(context.Background(), s, current, "ha-nova-setting", phoneChangeAsk(lang, newPhone), wait, os.Stderr)
		defer func() {
			prefix, line := phoneNothing(lang, answer)
			reply.say(prefix, line)
		}()
		if answer == phoneConfirmed {
			still, unread := phoneStillLinked(s, current, time.Time{})
			switch {
			case still && unlink:
				reply.say(confirmT(lang, "outcome.approved"), confirmT(lang, "outcome.line.approved_stop", "host", phoneHost(lang)))
				return true
			case still:
				reply.say(confirmT(lang, "outcome.approved"), confirmT(lang, "outcome.line.approved_move", "phone", sayText(newPhone, true)))
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
		case phoneUnread:
			fmt.Fprintln(out, "  Nothing changed and nothing was sent: "+phoneUnreadLine)
		default:
			fmt.Fprintf(out, "  Nothing changed: it was not confirmed on “%s” in time, or the notification could not be sent. If that phone is lost, remove it in Home Assistant (Settings → Devices & services → Mobile App), then run this again.\n", old)
			if answer == phoneNoAnswer {
				fmt.Fprintln(out, "  "+phoneNothingHint(current.targets[0].platform()))
			}
		}
		return false
	case status == phoneMissing && !current.why.replaceable():
		return false // missingLine said why and what to do
	}
	windowOnly := unlink || status == phoneMissing || deaf
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
	switch askWindow(context.Background(), plainWindow(lang, question), wait, os.Stderr) {
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

// eligible: the phones that may be linked — an administrator's (a chat
// app's: its user's, by level; confirmer), never the Relay user's. With
// signedIn, only that user's (none: everyone's).
func (h phoneHome) eligible(signedIn string) []phoneTarget {
	var out []phoneTarget
	for _, t := range h.phones {
		if t.UserID != h.relay && h.confirmer(h.users[t.UserID]) && (signedIn == "" || t.UserID == signedIn) {
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
	listPhones(out, all, h.shared, "")
	return phoneTarget{}, false
}

func phoneNameKey(name string) string {
	return strings.ToLower(strings.NewReplacer("’", "'", "‘", "'").Replace(strings.TrimSpace(name)))
}

// listPhones: with button, each row also says how that phone answers.
func listPhones(out io.Writer, phones []phoneTarget, shared []string, button string) {
	for i, t := range phones {
		how := ""
		if button != "" {
			how = ". On it: " + phoneTapLine("en", t.platform(), button)
		}
		fmt.Fprintf(out, "    %d) %s (%s)%s\n", i+1, terminalSafeName(t.Name), terminalSafeName(t.Person), how)
	}
	for _, name := range shared {
		fmt.Fprintln(out, "  "+phoneSharedLine(name))
	}
}

// phoneSkippedLines: where risky changes from a computer without a phone
// or a window are confirmed, the owner's chat code included (MAR-244).
func phoneSkippedLines(out io.Writer, s *relaySession) {
	fmt.Fprintln(out, "  Without a phone, risky changes from this computer wait for the NOVA page in Home Assistant.")
	if s.gate != nil && s.gate.code {
		fmt.Fprintln(out, "  Or the Home Assistant owner can let this computer confirm with a four-digit code in your AI chat, on the NOVA page. Your AI assistant sees that code too.")
	}
}
