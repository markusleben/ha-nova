package main

import "strings"

// offerLegacySwitch: a re-run on the old shared access offers the switch to
// this computer's own access as its first step, through setup's usual
// pairing: on the page when its App offers the Home Assistant sign-in, in
// the terminal otherwise (sign-in or the six-digit code). switched false:
// Not now, or nobody to ask (the done banner names it); the run goes on as
// before. The shared token stays where it is until uninstall, as after
// `ha-nova pair`.
func (f *setupFlow) offerLegacySwitch(onPage bool) (result setupStepResult, switched bool) {
	if f.serviceMode || f.overrideApplied || strings.TrimSpace(f.relayTokenFlag) != "" ||
		strings.TrimSpace(f.cfg.HAHost) == "" || strings.TrimSpace(f.cfg.HAURL) == "" ||
		!legacyAccessSwitchable(f.cfg, probePairingV1ForSetup) {
		return setupGoOn, false
	}
	var app setupPageApp
	if onPage {
		app = f.pageApp()
	}
	switch {
	case onPage && app.primary == "signin":
		rows, _ := f.pageRerunRows(app)
		rows = rows[:3] // Home Assistant, the NOVA App, this computer
		rows[2].State, rows[2].Msg = "todo", setupPageText("setup.page.rerun.computer_legacy")
		f.page.update(func(v *setupPageState) {
			v.Screen, v.Rows, v.Problem, v.Links, v.Control = "rerun_switch", rows, nil, nil, nil
		})
		action, err := f.page.wait("switch", "terminal")
		if err != nil {
			return f.cancelled(), true
		}
		if action.Action != "switch" {
			return setupGoOn, false
		}
		f.existingToken = "" // pair as on a first run
		f.keepClientsSetUpBefore()
		f.pageRepairing()
		return setupGoTo(setupStepHost), true
	case f.ui.interactive():
		f.ui.show(setupView{Blocks: []setupBlock{setupParagraph(setupText("setup.legacy.notice"))}})
		choice, err := f.ui.choose(setupAsk{
			Title: setupText("setup.legacy.ask"),
			Choices: []setupChoice{
				{Value: "switch", Label: setupText("setup.legacy.switch")},
				{Value: "later", Label: setupText("setup.legacy.later")},
			},
			Default: "switch",
		})
		if err == errSetupExit {
			return f.cancelled(), true
		}
		if err != nil || choice != "switch" {
			return setupGoOn, false
		}
		f.existingToken = ""
		return setupGoTo(f.routeKnownAddress(setupStepHost)), true
	default:
		return setupGoOn, false
	}
}
