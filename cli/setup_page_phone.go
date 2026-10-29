package main

import (
	"fmt"
	"slices"
)

// Step 3's phone row on the setup page (setupPageControl in
// setup_page_control.go): a first link with one notification, and "Use
// another phone" with `ha-nova setup confirm-phone`'s move rules.

// pagePhones fills the phone row from a read of this home's phones: a first
// link, or a phone linked before with the phones it can move to (the move
// keeps `confirm-phone`'s rule: the linked phone OKs it). A link that can't
// be asked now stays with `ha-nova setup confirm-phone`, which settles it
// safely.
func (f *setupFlow) pagePhones(ctl *setupPageControl) {
	ctl.Phone, ctl.Phones, ctl.Own, ctl.PhoneWhy, ctl.OldPhone = "unavailable", nil, 0, nil, ""
	if !(f.pairedThisRun || f.pageRerunning) || f.cloudSetupPaused { // as in the terminal: right after a pairing, and on a re-run
		return
	}
	home, links, ok := f.pageReadPhones()
	if !ok {
		ctl.Phone = "unread" // a busy Home Assistant: the row says so and offers Check again
		return
	}
	if home.relay == home.owner {
		return
	}
	current, status := home.status(links, nil)
	switch status {
	case phoneReady:
		ctl.Phone, ctl.PhoneName = "linked", terminalSafeName(current.targets[0].Name)
		ctl.Phones = movePhones(home, current)
		return
	case phoneNone:
	default:
		return
	}
	all, own := home.ownFirst(setupSignedInUser)
	if own == 0 { // never someone else's phone as the obvious choice: say why theirs is missing
		ctl.PhoneWhy = setupPageText("setup.page.control.phone_missing")
		if names := home.sharedOf[home.user(setupSignedInUser)]; len(names) > 0 {
			ctl.PhoneWhy = setupPageText("setup.page.control.phone_shared", "phone", terminalSafeName(names[0]))
		}
	}
	if len(all) == 0 {
		ctl.Phone = "none"
		return
	}
	ctl.Phone, ctl.Own = "pick", own
	for _, t := range all {
		ctl.Phones = append(ctl.Phones, setupPagePhone{ID: t.DeviceID, Name: terminalSafeName(t.Name), Person: terminalSafeName(t.Person), OS: t.platform()})
	}
}

// pageReadPhones reads the phones through the paired Relay; ok=false when
// they can't be read. A Relay that signs in as the owner (home.relay ==
// home.owner) can't link a phone: a tap couldn't be told apart from HA NOVA
// itself.
func (f *setupFlow) pageReadPhones() (phoneHome, map[string]phoneEntry, bool) {
	s, err := newRelaySession(f.paths, verbOptions{})
	if err != nil {
		return phoneHome{}, nil, false
	}
	defer s.close()
	links, readable := phoneLinks(f.paths)
	home, err := lookupPhones(s, links)
	return home, links, readable && err == nil
}

// pageLinkPhone sends the link question to the phone picked on the page.
// The click on the page is the consent at this computer (the window's part
// in `ha-nova setup confirm-phone`); the tap on that phone, from its own
// administrator, still links it. A fresh read first: the page may have
// waited a while.
func (f *setupFlow) pageLinkPhone(ctl *setupPageControl, deviceID string) {
	result := phoneLinkUnread
	if s, err := newRelaySession(f.paths, verbOptions{}); err == nil {
		links, readable := phoneLinks(f.paths)
		home, err := lookupPhones(s, links)
		_, status := home.status(links, nil)
		all := home.eligible("")
		i := slices.IndexFunc(all, func(t phoneTarget) bool { return t.DeviceID == deviceID })
		if readable && err == nil && home.relay != home.owner && status == phoneNone && i >= 0 {
			result = sendPhoneLink(s, f.ui.termOut(), home, links, links[home.owner], all[i], windowWait)
		}
		s.close()
	}
	if result == phoneLinked {
		ctl.Phone, ctl.Phones, ctl.Own, ctl.PhoneWhy = "linked", nil, 0, nil
		return
	}
	ctl.Phone, ctl.PhoneProblem = "pick", pagePhoneProblem(result, ctl.PhoneName)
}

// movePhones: the phones confirmations can move to, every eligible one but
// the phone linked now (and its watch).
func movePhones(home phoneHome, current phoneSetup) []setupPagePhone {
	var out []setupPagePhone
	for _, t := range home.eligible("") {
		if !slices.ContainsFunc(current.targets, func(c phoneTarget) bool { return c.DeviceID == t.DeviceID }) {
			out = append(out, setupPagePhone{ID: t.DeviceID, Name: terminalSafeName(t.Name), Person: terminalSafeName(t.Person), OS: t.platform()})
		}
	}
	return out
}

// pageMovePhone: "Use another phone" on a linked row. The click is no
// consent here: as `ha-nova setup confirm-phone`, the change goes through
// allowPhoneChange (the phone linked now OKs it while it can answer, else
// the window), then the tap on the new phone links it. A fresh read first.
// True once the new phone is linked; else the row shows the linked one
// again, with one line.
func (f *setupFlow) pageMovePhone(ctl *setupPageControl, deviceID string) bool {
	old, name := ctl.OldPhone, ctl.PhoneName
	asked, allowed, result := false, false, phoneLinkUnread
	if s, err := newRelaySession(f.paths, verbOptions{}); err == nil {
		out := f.ui.termOut()
		links, readable := phoneLinks(f.paths)
		home, err := lookupPhones(s, links)
		current, status := home.status(links, nil)
		all := home.eligible("")
		i := slices.IndexFunc(all, func(t phoneTarget) bool { return t.DeviceID == deviceID })
		if readable && err == nil && home.relay != home.owner && status == phoneReady && i >= 0 &&
			!slices.ContainsFunc(current.targets, func(c phoneTarget) bool { return c.DeviceID == deviceID }) {
			asked, name = true, terminalSafeName(all[i].Name)
			allowed = allowPhoneChange(s, out, home, links, fmt.Sprintf("Link “%s” to confirm HA NOVA actions from “%s”?", name, phoneHost()), name, false, windowWait)
			if allowed {
				result = sendPhoneLink(s, out, home, links, links[home.owner], all[i], windowWait)
			}
		}
		s.close()
	}
	if result == phoneLinked {
		f.pagePhones(ctl) // the new phone, its watch, and where it can move now
		if ctl.Phone != "linked" {
			ctl.Phone, ctl.PhoneName, ctl.Phones = "linked", name, nil
		}
		return true
	}
	ctl.Phone, ctl.PhoneName, ctl.OldPhone = "linked", old, ""
	switch {
	case !asked:
		ctl.PhoneProblem = setupPageText("setup.page.control.phone_problem", "phone", name)
	case !allowed:
		ctl.PhoneProblem = setupPageText("setup.page.control.move_refused", "phone", old)
	default:
		ctl.PhoneProblem = pagePhoneProblem(result, name)
	}
	return false
}

// pagePhoneProblem: one plain line for a link that didn't happen.
func pagePhoneProblem(result phoneLinkResult, phone string) *setupPageMsg {
	switch result {
	case phoneLinkTimeout:
		return setupPageText("setup.page.control.phone_timeout", "phone", phone)
	case phoneLinkDeclined:
		return setupPageText("setup.page.control.phone_declined", "phone", phone)
	case phoneLinkRejected:
		return setupPageText("setup.page.control.phone_rejected", "phone", phone)
	case phoneLinkFailed:
		return setupPageText("setup.page.control.phone_unreachable", "phone", phone)
	}
	return setupPageText("setup.page.control.phone_problem", "phone", phone)
}
