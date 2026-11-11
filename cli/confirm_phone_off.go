package main

import (
	"fmt"
	"io"
)

// unlinkPhone: a downgrade (the next channel is the form or chat), so it is
// confirmed on the linked phone, or in the window when that phone can never
// answer again (or this computer can't hear it: a gated App); never without
// either. On a gated App it stays local: the Relay has no unlink, and the
// NOVA page no control for it.
func unlinkPhone(s *relaySession, out io.Writer) int {
	lang := s.homeLanguage()
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
	gated, gateErr := s.gateOn()
	question := confirmT(lang, "settings.stop_ask")
	if gated {
		question = confirmT(lang, "settings.stop_ask_gated")
	}
	if !allowPhoneChange(s, out, lang, home, links, question, "", true, windowWait) {
		return 1
	}
	if err := setPhoneLink(s.paths, home.owner, prev, nil); err != nil {
		fmt.Fprintf(out, "  Nothing changed: %s.\n", err)
		return 1
	}
	switch {
	case gated:
		fmt.Fprintln(out, "  "+phoneOffGated)
	case gateErr != nil:
		fmt.Fprintf(out, "  Turned off on this computer. HA NOVA couldn't check whether its safety check in Home Assistant is on (%s); if it is, the phone link in Home Assistant stays.\n", gateErr)
	default:
		fmt.Fprintf(out, "  Phone confirmation is off: no phone gets HA NOVA confirmations from “%s”.\n", phoneHost("en"))
	}
	return 0
}
