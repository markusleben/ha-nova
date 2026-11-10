package main

import (
	"encoding/json"
	"errors"
	"strings"
)

// doctorGate: HA NOVA's safety check in Home Assistant (gate spec §7): on,
// or why not. false: a failure (rolled back, or its worker down). The
// Relay's own phone link can't be read (no route), so doctor never claims one.
func doctorGate(paths runtimePaths, health []byte, usingCloud, deviceMode bool, info func(string, ...any)) bool {
	var h struct {
		Data struct {
			Gate int `json:"gate"`
		} `json:"data"`
	}
	_ = json.Unmarshal(health, &h)
	if h.Data.Gate != 1 && gateSeen(paths, activeServerProfile()) {
		printHumanErr("Safety check in Home Assistant: off. The NOVA App was rolled back to a version without it, so HA NOVA sends no unlocks, deletes, or other big changes. Update the NOVA App in Home Assistant (Settings → Apps → NOVA Relay).")
		return false
	}
	if h.Data.Gate != 1 {
		printHumanWarn("Safety check in Home Assistant: not on. The NOVA App is too old for it; update it in Home Assistant (Settings → Apps → NOVA Relay).")
		return true
	}
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		info("Safety check in Home Assistant: on")
		return true
	}
	defer s.close()
	links, _ := phoneLinks(paths)
	home, err := lookupPhones(s, links)
	var call *relayCallError
	if errors.As(err, &call) && call.code == "GATE_UNAVAILABLE" {
		printHumanErr("Safety check in Home Assistant: on, but not running, so no one can confirm unlocks, deletes, or other big changes. Restart the NOVA App in Home Assistant (Settings → Apps → NOVA Relay).")
		return false
	}
	info("Safety check in Home Assistant: on. Unlocks, deletes, and other big changes wait for an OK on a linked phone or in Home Assistant (NOVA).")
	var limits []string
	if err == nil && home.relay == home.owner {
		limits = append(limits, "the NOVA App signs in to Home Assistant as the owner, so only the NOVA page can confirm, never a phone (NOVA Relay in your own container: give it its own Home Assistant user's token)")
	}
	if usingCloud {
		limits = append(limits, "this computer keeps a Home Assistant sign-in for Home Assistant Link, which can act in Home Assistant without the NOVA App, so the check is no boundary for this computer")
	}
	if len(limits) > 0 {
		printHumanWarn("Safety check limits: %s.", strings.Join(limits, "; "))
	}
	if setup, status := home.status(links, err); !deviceMode && status == phoneMissing && setup.why == whyRemoved {
		printHumanWarn("The phone this computer linked for confirmations is no longer in Home Assistant. With NOVA Relay in your own container, the phone link in Home Assistant can't be replaced without that phone.")
	}
	return true
}
