package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// The phone link on a gated App (gate spec §3, §8 → layer 6b): the Relay
// asks only the phone it holds for this device, so this computer's own link
// (config.json confirm_phone) moves there through POST /gate/v1/phone, which
// the person approves with a tap on that phone (and on the phone linked
// there now, if any). A link is never an approval: the typed ask is opened
// again and needs its own OK. The local entry stays as it is.

const gatePhoneRoute = "gate/v1/phone"

// phoneDeaf: on a gated App this computer can't hear a tap (the gate
// refuses phone-event subscriptions for devices), so it asks no phone
// itself: its linked phone counts as one it can't ask, and the Relay asks
// for the typed requests. A gate state that can't be read keeps today's paths.
func (s *relaySession) phoneDeaf() bool {
	if s.gateChat() { // a chat app keeps its own asks for plans the Relay doesn't hold (gateChannel), as before
		return false
	}
	on, err := s.gateOn()
	return err == nil && on
}

const phoneDeafLine = "On this NOVA App, a tap on your phone can't reach HA NOVA on this computer, so a confirmation window here decides."

// gateMovePhone asks the Relay to hold link for this device, opening and
// waiting within wait, counted from now (never the command's start: a
// setup page may have waited long): "same" when it holds it already, else
// how the link ask ended ("linked", or one of gateWait's states; "lost":
// its answer never came, so the outcome is unknown). err: the Relay's
// refusal or no answer.
func (s *relaySession) gateMovePhone(link phoneLink, wait time.Duration) (string, error) {
	start := time.Now()
	var opened struct {
		Needed bool   `json:"needed"`
		AskID  string `json:"ask_id"`
	}
	wire := map[string]string{"service": link.Service, "device_id": link.DeviceID, "user_id": link.UserID}
	if link.Platform != "" {
		wire["platform"] = link.Platform
	}
	if err := s.gateCall(planAsks, gatePhoneRoute, min(2*gateWorkerStep+5*time.Second, wait), map[string]any{"link": wire}, &opened); err != nil {
		return "", err
	}
	if !opened.Needed {
		return "same", nil
	}
	if opened.AskID == "" {
		return "", errors.New("the NOVA App's answer named no ask")
	}
	state, _ := s.gateWait(opened.AskID, true, wait-time.Since(start))
	return state, nil
}

// gateLocalPhone: this computer's link for this Home Assistant, as the
// Relay takes it, while that phone can be asked (phoneReady). Its lookup
// has a read budget of its own: preparing the ask may have spent the session's.
func (s *relaySession) gateLocalPhone() (phoneLink, bool) {
	links, readable := phoneLinks(s.paths)
	if !readable || len(links) == 0 {
		return phoneLink{}, false
	}
	done := s.ownBudget(time.Now().Add(relaySessionOverallTimeout))
	home, err := lookupPhones(s, links)
	done()
	setup, status := home.status(links, err)
	if status != phoneReady {
		return phoneLink{}, false
	}
	phone := setup.targets[0]
	return phoneLink{Service: phone.Service, DeviceID: phone.DeviceID, UserID: setup.confirmer, Platform: phone.platform()}, true
}

// gateMoveFirst runs on evidence only, once per session (a move that ended
// without a link isn't tried again): the ask found no phone (409
// GATE_NO_CHANNEL, or channels without "phone") and this computer has a
// link. The link waits linkWait (gateAsk: half of its budget, so the
// reopened typed ask keeps the rest). reopen: open the typed ask again (the
// link ask replaced it). A move that ends without a link refuses, nothing
// sent; only an unanswered, unavailable, or paused one goes on where the
// first open offered another channel (the NOVA page), never a weaker one.
func (s *relaySession) gateMoveFirst(opened gateOpened, openErr error, linkWait time.Duration) (reopen bool, stop error) {
	var call *relayCallError
	noChannel := errors.As(openErr, &call) && call.code == "GATE_NO_CHANNEL"
	offered := openErr == nil && opened.Needed && opened.AskID != "" && len(opened.Channels) > 0
	if !noChannel && (!offered || slices.Contains(opened.Channels, "phone")) {
		return false, nil
	}
	s.mu.Lock()
	if s.gate == nil {
		s.gate = &relayGate{}
	}
	tried := s.gate.moved
	s.gate.moved = true
	s.mu.Unlock()
	if tried {
		return false, nil
	}
	link, ok := s.gateLocalPhone()
	if !ok {
		return false, nil
	}
	state, err := s.gateMovePhone(link, linkWait)
	ended := state == "expired" || state == "timeout" || state == "unavailable" || errors.As(err, &call) && call.code == "GATE_PAUSED"
	switch {
	case state == "same" || errors.As(err, &call) && call.status == http.StatusNotFound: // held already, or no link route for this device
		return false, nil
	case state == "linked", offered && ended:
		return true, nil
	}
	if offered {
		s.gateCancel(opened.AskID)
	}
	return false, gateLinkFailed(state, err)
}

// gateLinkFailed: a move that ended without a link, in its own words: the
// phone link wasn't confirmed, nothing was sent, and how to link it.
func gateLinkFailed(state string, err error) *verbFailure {
	next := "Tell the user nothing changed: their phone didn't confirm getting HA NOVA's confirmations, so the change itself wasn't asked for. They link it with `ha-nova setup confirm-phone` (in a terminal), or run `ha-nova doctor` to see what is missing; then they ask again."
	status, code, why := "refused", "CONFIRM_UNAVAILABLE", "a phone couldn't be asked: the new one, or the one linked in Home Assistant now"
	var call *relayCallError
	switch {
	case state == "stopped":
		return gateEnded(state)
	case state == "cancelled":
		status, code, why = "declined", "CONFIRM_DECLINED", "it was declined on a phone"
	case state == "expired" || state == "timeout":
		status, code, why = "cancelled", "CONFIRM_CANCELLED", "nobody allowed it on the phone in time"
	case state == "lost":
		why = "the NOVA App's answer was lost, so whether the phone is linked now is unknown"
	case errors.As(err, &call) && strings.HasPrefix(call.code, "GATE_"):
		code, why = call.code, call.message
		if n := gateNext[call.code]; n != "" && call.code != "GATE_PAUSED" {
			next = n
		}
	case err != nil:
		why = "HA NOVA couldn't ask the NOVA App (" + err.Error() + ")"
	}
	return gateStop(status, code, "the phone link to HA NOVA wasn't confirmed ("+why+"); nothing was sent", next)
}

// phoneOffGated: setup confirm-phone off on a gated App stays local.
const phoneOffGated = "Turned off on this computer only. The phone link in Home Assistant stays: the NOVA App keeps asking that phone to confirm this computer's unlocks and deletes, and HA NOVA can't remove it there."

// linkPhoneAtRelay links chosen through the Relay (setup confirm-phone and
// the setup page on a gated App, where this computer can't hear a tap;
// gated false elsewhere), waiting up to wait: the Relay's worker asks the
// phone linked there now first, if any, then chosen. Linked, this
// computer's entry follows, so a later ask moves nothing.
func linkPhoneAtRelay(s *relaySession, out io.Writer, lang string, home phoneHome, prev phoneEntry, chosen phoneTarget, wait time.Duration) (result phoneLinkResult, gated bool) {
	if on, err := s.gateOn(); err != nil || !on {
		if err != nil {
			fmt.Fprintln(out, "  "+err.Error())
		}
		return phoneLinkUnread, err != nil
	}
	name := terminalSafeName(chosen.Name)
	link := phoneLink{Service: chosen.Service, DeviceID: chosen.DeviceID, UserID: chosen.UserID, Name: name, Platform: chosen.platform()}
	fmt.Fprintf(out, "  If another phone is linked in Home Assistant now, it asks first (“%s”). Then “%s” gets “%s”. %s\n",
		confirmT(lang, "phone.move.title"), name, phoneLinkTitle(lang), phoneTapLine("en", chosen.platform(), phoneLinkButton(lang)))
	state, err := s.gateMovePhone(link, wait)
	var call *relayCallError
	result = phoneLinkFailed
	nothing := map[string]string{
		"cancelled":   "it was cancelled on a phone, the tap came from another person, or the link changed meanwhile",
		"expired":     "“" + phoneLinkButton(lang) + "” was not tapped in time",
		"timeout":     "“" + phoneLinkButton(lang) + "” was not tapped in time",
		"unavailable": "a phone couldn't be asked: the new one, or the phone linked in Home Assistant now, which must confirm the move (if that phone is gone, the link can't be replaced without it)",
	}[state]
	switch {
	case errors.As(err, &call) && call.status == http.StatusNotFound:
		nothing = "this NOVA App doesn't take a phone link from this computer"
	case err != nil:
		nothing = "HA NOVA couldn't ask its safety check in Home Assistant (" + err.Error() + ")"
	case state == "lost": // the Relay may hold the new phone already: neither side is known, so the setting stays
		fmt.Fprintf(out, "  HA NOVA lost the NOVA App's answer, so it can't tell whether “%s” is linked in Home Assistant now. This computer's setting stays as it was. Run ha-nova setup confirm-phone again and pick the phone that should confirm: that sets both right.\n", name)
		return phoneLinkUnread, true
	case state == "cancelled":
		result = phoneLinkDeclined
	case state == "expired" || state == "timeout":
		result = phoneLinkTimeout
	case state != "linked" && state != "same" && nothing == "":
		nothing = "HA NOVA stopped waiting"
	}
	if nothing != "" {
		fmt.Fprintf(out, "  Nothing linked: %s. Run ha-nova setup confirm-phone again to retry.\n", nothing)
		return result, true
	}
	if err := setPhoneLink(s.paths, home.owner, prev, &link); err != nil {
		fmt.Fprintf(out, "  Linked “%s” in Home Assistant, but this computer's setting couldn't follow (%s). Run ha-nova setup confirm-phone again.\n", name, err)
		return phoneLinkUnread, true
	}
	fmt.Fprintf(out, "  Linked “%s”. Only “%s” gets HA NOVA confirmations from “%s”.\n", name, name, phoneHost("en"))
	return phoneLinked, true
}
