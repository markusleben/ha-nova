package main

import (
	"fmt"
	"strings"
)

// The setup page on a re-run (docs/work/2026-10-18-onboarding-0.27-spec.md
// §4 "Re-run on a working install"): setup ran on this computer before.
// A working setup shows what is set up, with step 3's rows on the same
// screen; Done applies their switches and ends setup. Skills that only need
// a refresh (after an update) are refreshed first, without a question. Any
// other problem is shown with Fix it, which runs the usual steps on the
// page (a computer removed on the NOVA page signs in again), or, while the
// NOVA App doesn't answer, Check again. Nothing changes without a click.

// pageRerun replaces the terminal's status screen on the page; on a first
// run (no saved address) handled is false.
func (f *setupFlow) pageRerun(marker [][]byte) (setupStepResult, bool) {
	if f.page == nil || f.page.inTerminal() || strings.TrimSpace(f.cfg.HAHost) == "" || strings.TrimSpace(f.cfg.HAURL) == "" {
		return setupGoOn, false
	}
	f.pageRerunning = true
	for {
		c := f.current
		if c.IsComplete() || (c.ConfigOK && c.TokenOK && c.RelayOK && c.WSOK) {
			f.pageRerunOK = true
			if c.IsComplete() {
				f.lifecycleMarker = marker // as the terminal's status screen
				return f.afterSkills(), true
			}
			// Only the skills are out of date: setup's own job, no question.
			f.page.update(func(v *setupPageState) {
				v.Screen, v.Rows = "connect", []setupPageRow{{ID: "teach", State: "run", Msg: setupPageText("setup.page.row.teach", "clients", setupClientNames(f.selectedClients))}}
			})
			return setupGoTo(f.routeKnownAddress(setupStepHost)), true
		}
		rows, appDown := f.pageRerunRows()
		accepts, links := []string{"fix"}, map[string]string{}
		var problem *setupPageMsg
		if appDown { // nothing on this computer fixes it
			accepts, problem = []string{"recheck"}, setupPageText("setup.page.rerun.problem_app")
			links["app"] = haRelayAppPageURL(f.cfg.HAURL)
		}
		f.page.update(func(v *setupPageState) {
			v.Screen, v.Rows, v.Problem, v.Links, v.Control = "rerun", rows, problem, links, nil
		})
		action, err := f.page.wait(accepts...)
		if err != nil {
			return f.cancelled(), true
		}
		if action.Action == "recheck" {
			f.current = detectSetupStateForAssessment(f.paths, f.cfg, f.state, f.target, f.savedTokenBeforeSetup, f.hadSavedTokenBeforeSetup)
			continue
		}
		f.page.update(func(v *setupPageState) {
			v.Screen, v.Problem, v.Links = "connect", nil, nil
			v.Rows = []setupPageRow{
				{ID: "connect", State: "run", Msg: setupPageText("setup.page.row.connect")},
				{ID: "teach", State: "todo", Msg: setupPageText("setup.page.row.teach", "clients", setupClientNames(f.selectedClients))},
			}
		})
		next := f.routeKnownAddress(setupStepHost)
		if next == setupStepPairing { // not paired: the page signs in, as on a first run
			next = setupStepHost
		}
		return setupGoTo(next), true
	}
}

// pageRerunRows: what is set up, from the same checks as the terminal's
// status screen. appDown: the NOVA App doesn't answer at all.
func (f *setupFlow) pageRerunRows() (rows []setupPageRow, appDown bool) {
	c := f.current
	app := f.pageApp()
	appRow := setupPageRow{ID: "app", State: "ok", Msg: setupPageText("setup.page.app.running")}
	switch {
	case app.state == "old":
		appRow.State, appRow.Msg = "todo", setupPageText("setup.page.app.old")
	case app.state == "running" && app.version != "":
		appRow.Msg = setupPageText("setup.page.app.running_version", "version", terminalSafeName(app.version))
	case app.state == "running" || c.RelayOK:
	default:
		appRow.State, appRow.Msg, appDown = "fail", setupPageText("setup.page.rerun.app_down"), true
	}
	computer := setupPageRow{ID: "computer", State: "ok", Msg: setupPageText("setup.page.rerun.computer_ok")}
	if !(c.TokenOK && c.RelayOK && c.WSOK) {
		computer.State, computer.Msg = "fail", setupPageText("setup.page.rerun.computer_fail")
	}
	names := setupClientNames(f.selectedClients)
	clients := setupPageRow{ID: "clients", State: "ok", Msg: setupPageText("setup.page.rerun.clients_ok", "clients", names)}
	if !c.SkillsOK {
		clients.State, clients.Msg = "fail", setupPageText("setup.page.rerun.clients_fail", "clients", names)
	}
	return []setupPageRow{
		{ID: "ha", State: "ok", Msg: setupPageText("setup.page.rerun.ha", "host", terminalSafeName(f.cfg.HAHost))},
		appRow, computer, clients,
	}, appDown
}

// pageRerunControl puts the status rows above step 3's on a working re-run
// (setup got this far: the connection and the skills are in place).
func (f *setupFlow) pageRerunControl() {
	f.current = setupState{ConfigOK: true, TokenOK: true, RelayOK: true, WSOK: true, SkillsOK: true}
	rows, _ := f.pageRerunRows()
	links := map[string]string{}
	if rows[1].State != "ok" { // an App that needs an update
		links["app"] = haRelayAppPageURL(f.cfg.HAURL)
	}
	f.page.update(func(v *setupPageState) { v.Rows, v.Links = rows, links })
}

// pageRerunChat: a re-run has no owner sign-in, so the chat-app row says
// where they are switched: NOVA in Home Assistant. Only for an App with
// chat apps and a Home Assistant new enough for them; nil: no row.
func (f *setupFlow) pageRerunChat() *setupPageChat {
	ha, err := chatAppsReadHA(f.paths)
	base, _, _ := strings.Cut(ha.appVersion, "-") // an rc of the first chat-app App counts as that App
	if err != nil || base == "" || !chatAppsHAVersionOK(ha.version) {
		return nil
	}
	if cmp, err := compareReleaseVersions(base, chatAppsMinAppVersion); err != nil || cmp < 0 {
		return nil
	}
	target, err := haNOVAAppPanelURL(f.cfg.HAURL)
	if err != nil {
		return nil
	}
	chat := &setupPageChat{State: "page_off", Page: target.URL}
	if ha.present { // Home Assistant holds the chat-app integration only while they are on
		chat.State = "page_on"
	}
	return chat
}

// pageAddServer: "Add another Home Assistant server" on a working re-run
// runs the terminal's add flow, as its completed-setup screen does.
func (f *setupFlow) pageAddServer() int {
	f.page.handover()
	return startAddServerFlow(f.page.termIn(), f.page.text.out, f.paths)
}

// pageFinish ends a page run: the terminal's banner (step 4 replaces its
// first look; it clears the screen, so it comes first), step 3, then step
// 4 until Done. A working re-run ends on its own screen instead.
func (f *setupFlow) pageFinish() setupStepResult {
	kind := setupOutcomeComplete
	switch {
	case f.pageRerunOK:
		kind = setupOutcomeAlreadyDone
	case f.cloudSetupPaused:
		kind = setupOutcomeLinkPaused
	case f.cfg.Cloud.ready() && f.cfg.RoutePolicy == routePolicyAutomatic:
		kind = setupOutcomeLinkReady
	}
	f.page.text.outcome(setupOutcome{Kind: kind, Clients: f.selectedClients, LegacyToken: f.pageRerunOK && f.cfg.RelaySecureBaseURL == "" && f.cfg.RelayBaseURL != ""})
	if f.pageRerunOK { // the banner cleared the page's address
		fmt.Fprintf(f.page.text.out, "  %s\n", setupT("setup.page.terminal.rerun"))
	}
	exit := f.pageControl()
	if f.pageQuit {
		f.page.end("skipped") // connected, but step 3's choices weren't applied
		return setupDone(exit)
	}
	if f.pageRerunOK { // Done ends a re-run (spec §4), with what it did
		if !f.page.inTerminal() {
			f.page.update(func(v *setupPageState) { v.Home = &setupPageHome{Notes: f.pageNotes} })
			f.page.end("saved")
		}
		return setupDone(exit)
	}
	fmt.Fprintf(f.page.text.out, "  %s\n", setupT("setup.page.terminal.home"))
	f.pageHome()
	f.page.end("closed")
	return setupDone(exit)
}

func setupClientNames(clients []string) []string {
	names := make([]string, len(clients))
	for i, client := range clients {
		names[i] = setupClientLabel(client)
	}
	return names
}
