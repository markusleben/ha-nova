package main

import (
	"encoding/json"
	"errors"
	"strings"
)

// doctorGate: HA NOVA's safety check in Home Assistant (gate spec §7): on,
// or why not, and who can confirm. 1: a failure (rolled back, or its worker
// down); skip: doctor checked no Relay. linkSignIn: this computer keeps a
// Home Assistant Link sign-in, whatever route this run took. Without a
// device key (deviceMode false) the Relay runs on its own: no NOVA page.
// The Relay's own phone link can't be read (no route), so doctor never
// claims one.
func doctorGate(paths runtimePaths, skip bool, health []byte, linkSignIn, deviceMode bool, info func(string, ...any)) int {
	var h struct {
		Data struct {
			Gate int `json:"gate"`
		} `json:"data"`
	}
	_ = json.Unmarshal(health, &h)
	switch {
	case skip:
		return 0
	case h.Data.Gate != 1 && gateSeen(paths, activeServerProfile()):
		printHumanErr("Safety check in Home Assistant: off. The NOVA App was rolled back to a version without it, so HA NOVA sends no unlocks, deletes, or other big changes. Update the NOVA App in Home Assistant (Settings → Apps → NOVA Relay).")
		return 1
	case h.Data.Gate != 1:
		printHumanWarn("Safety check in Home Assistant: not on. The NOVA App is too old for it; update it in Home Assistant (Settings → Apps → NOVA Relay).")
		return 0
	}
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		printHumanWarn("Safety check in Home Assistant: on, but HA NOVA couldn't check who can confirm (%s).", err)
		return 0
	}
	defer s.close()
	links, _ := phoneLinks(paths)
	home, err := lookupPhones(s, links)
	var call *relayCallError
	switch {
	case errors.As(err, &call) && call.code == "GATE_UNAVAILABLE":
		printHumanErr("Safety check in Home Assistant: on, but not running, so no one can confirm unlocks, deletes, or other big changes. Restart the NOVA App in Home Assistant (Settings → Apps → NOVA Relay).")
		return 1
	case err != nil:
		printHumanWarn("Safety check in Home Assistant: on, but HA NOVA couldn't check that it runs and who can confirm (%s). Run ha-nova doctor again in a moment.", err)
	default:
		info("Safety check in Home Assistant: on. Unlocks, deletes, and other big changes wait for an OK on a linked phone or in Home Assistant (NOVA).")
	}
	var limits []string
	owner := home.relay != "" && home.relay == home.owner
	switch {
	case owner && deviceMode:
		limits = append(limits, "the NOVA App signs in to Home Assistant as the owner, so only the NOVA page can confirm, never a phone")
	case owner:
		limits = append(limits, "your NOVA Relay runs on its own, without a NOVA page, and signs in to Home Assistant as the owner, so nothing can confirm unlocks, deletes, or other big changes: give it the token (HA_LLAT) of a Home Assistant user who isn't the owner")
	}
	if linkSignIn {
		limits = append(limits, "this computer keeps a Home Assistant sign-in for Home Assistant Link, which can act in Home Assistant without the NOVA App, so the check is no boundary for this computer")
	}
	if len(limits) > 0 {
		printHumanWarn("Safety check limits: %s.", strings.Join(limits, "; "))
	}
	setup, status := home.status(links, err)
	switch {
	case deviceMode || owner || err != nil:
	case status == phoneMissing && setup.why == whyRemoved:
		printHumanWarn("The phone this computer linked for confirmations is no longer in Home Assistant. Link another one: ha-nova setup confirm-phone. Your NOVA Relay runs on its own, without a NOVA page; if it still asks the old phone, delete the file gate-phone.json in its data folder first.")
	case status == phoneNone:
		printHumanWarn("No phone is linked for confirmations on this computer. Your NOVA Relay runs on its own, without a NOVA page, so until a phone is linked nothing can confirm unlocks, deletes, or other big changes. Link one: ha-nova setup confirm-phone.")
	}
	return 0
}

func fetchRelayHealth(relayBaseURL, token string) ([]byte, error) {
	return fetchRelayHealthWith(httpClient, relayBaseURL, token)
}
