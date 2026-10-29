package main

// Another Home Assistant on the setup page. Pairing never replaces this
// computer's device while it has Home Assistant Link for the saved home
// (validateLocalDeviceReplacementAllowedWithPolicy), so the page says so
// before any sign-in at the other home, and its button removes Link first.

// pageLinkElsewhere: why haURL can't be used yet; nil when it can.
func (f *setupFlow) pageLinkElsewhere(haURL string) *setupPageMsg {
	if f.cfg.Cloud == nil || haURL == f.cfg.HAURL {
		return nil
	}
	return setupPageText("setup.page.problem.link_elsewhere", "host", terminalSafeName(f.cfg.HAHost))
}

// pageRemoveLink removes this computer's Link as `ha-nova cloud remove`
// does (the click is the confirmation; the terminal shows how it went) and
// reads the settings again. nil: removed.
func (f *setupFlow) pageRemoveLink() *setupPageMsg {
	failed := setupPageText("setup.page.problem.link_remove_failed")
	if ensureSetupLifecycleCurrent(f.paths, f.lifecycleMarker...) != nil {
		return failed
	}
	removeLeftoverLink(f.paths)
	if refreshSetupConfigSnapshot(f.paths, f.lifecycleMarker) != nil {
		return failed
	}
	cfg, err := loadConfig(f.paths)
	if err != nil || cfg.Cloud != nil {
		return failed
	}
	f.cfg = cfg
	return nil
}

// pageUseAddress takes the picked address; another home starts unpaired, so
// its six-digit code path asks for a code instead of checking the old home's
// device.
func (f *setupFlow) pageUseAddress(host, haURL string) {
	if haURL != f.cfg.HAURL {
		f.deviceAlreadyPaired, f.devicePaired = false, false
	}
	f.cfg = applySelectedSetupHost(f.cfg, host, haURL, f.relayURLFlag)
}
