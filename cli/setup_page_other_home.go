package main

import "strings"

// Another Home Assistant on the setup page. Pairing never replaces this
// computer's device while it has Home Assistant Link for the saved home
// (validateLocalDeviceReplacementAllowedWithPolicy), so the page says so
// before any sign-in at the other home, and its button removes Link first.

// pageLinkElsewhere: why haURL can't be used yet; nil when it can. Only
// the saved address itself keeps Link (the same home at a new address too
// needs a new pairing, which Link blocks).
func (f *setupFlow) pageLinkElsewhere(haURL string) *setupPageMsg {
	if f.cfg.Cloud == nil || sameSetupAddress(haURL, f.cfg.HAURL) {
		return nil
	}
	return setupPageText("setup.page.problem.link_elsewhere", "host", terminalSafeName(f.cfg.HAHost))
}

// pageRemoveLink removes this computer's Link as `ha-nova cloud remove`
// does (the click is the confirmation; the terminal shows how it went) and
// takes the settings it left, also after a removal that stopped halfway.
// problem: not removed. err: the settings can't be read; setup ends.
func (f *setupFlow) pageRemoveLink() (problem *setupPageMsg, err error) {
	if err := ensureSetupLifecycleCurrent(f.paths, f.lifecycleMarker...); err != nil {
		return nil, err
	}
	removeLeftoverLink(f.paths)
	if err := refreshSetupConfigSnapshot(f.paths, f.lifecycleMarker); err != nil {
		return nil, err
	}
	if f.cfg, err = loadConfig(f.paths); err != nil {
		return nil, err
	}
	if f.cfg.Cloud != nil {
		return setupPageText("setup.page.problem.link_remove_failed"), nil
	}
	return nil, nil
}

// sameSetupAddress: one address however it is cased or ends.
func sameSetupAddress(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(a, "/"), strings.TrimRight(b, "/"))
}

// useSetupAddress takes the picked address; another home starts unpaired, so
// its six-digit code path asks for a code instead of checking the old home's
// device.
func (f *setupFlow) useSetupAddress(host, haURL string) {
	if !sameSetupAddress(haURL, f.cfg.HAURL) {
		f.deviceAlreadyPaired, f.devicePaired = false, false
	}
	f.cfg = applySelectedSetupHost(f.cfg, host, haURL, f.relayURLFlag)
}
