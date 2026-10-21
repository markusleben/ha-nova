package main

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
)

// The setup page on a re-run (docs/work/2026-10-18-onboarding-0.27-spec.md
// §4 "Re-run on a working install"): setup ran on this computer before.
// A working setup shows what is set up, with step 3's rows on the same
// screen; Done applies their switches and ends setup. The AI assistants set
// up before are judged and, when only their skills are out of date (after
// an update), refreshed without a question; one found since is offered
// with a button. Anything else shows Fix it, which runs the usual steps on
// the page (a computer removed on the NOVA page signs in again). While the
// NOVA App doesn't answer: Check again, or Fix it on step 1, where the
// address can change and the sign-in installs or starts the App. While Home
// Assistant doesn't answer either, another one found here is offered too.

// pageRerun replaces the terminal's status screen on the page; on a first
// run (no saved address) handled is false.
func (f *setupFlow) pageRerun(marker [][]byte) (setupStepResult, bool) {
	if f.page == nil || f.page.inTerminal() || strings.TrimSpace(f.cfg.HAHost) == "" || strings.TrimSpace(f.cfg.HAURL) == "" {
		return setupGoOn, false
	}
	f.pageRerunning = true
	before := f.keepClientsSetUpBefore()
	var discovering atomic.Bool
	var unreachable []string // found addresses that didn't answer when picked; written under the page's lock
	var picked *setupPageMsg // why the last pick didn't work
	for {
		notReady := f.pageClientsNotReady()
		f.current.SkillsOK = len(notReady) == 0
		c := f.current
		// Skills set up before and now out of date: refreshed without a question.
		refresh := len(before) == 0 || !slices.ContainsFunc(notReady, func(client string) bool { return !slices.Contains(f.state.InstalledClients, client) })
		app := f.pageApp()
		// An App older than this HA NOVA needs, which the owner sign-in can
		// update: that is the one thing to do, before the switches.
		appUpdate := app.state == "old" && app.primary == "update"
		if c.ConfigOK && c.TokenOK && c.RelayOK && c.WSOK && refresh && !appUpdate {
			f.pageRerunOK = true
			if c.SkillsOK {
				if len(marker) == 0 && len(f.lifecycleMarker) > 0 {
					// Completes without its markers, as the terminal's status
					// screen; later writes hold the generation setup started
					// with, so an uninstall at any point fails them closed.
					f.pageGeneration = f.lifecycleMarker[0]
				}
				f.lifecycleMarker = marker
				return f.afterSkills(), true
			}
			f.pageRepairing("teach")
			return setupGoTo(f.routeKnownAddress(setupStepHost)), true
		}
		rows, problem := f.pageRerunRows(app)
		appDown := problem != nil
		haDown := appDown && problem.Key == "setup.page.rerun.problem_app"
		accepts, links, fix := []string{"fix"}, map[string]string{}, ""
		if appDown {
			accepts = []string{"recheck", "fix"}
		}
		if haDown { // a Home Assistant found here (pageRerunDiscover)
			accepts = append(accepts, "address")
		}
		if appDown && problem.Key == "setup.page.rerun.problem_app_only" { // the App page opens only while Home Assistant answers
			links["app"] = haInstallRelayAppURL(f.cfg.HAURL)
		}
		if appUpdate && !appDown { // its button says what it does; step 1's sign-in updates the App
			problem, fix, accepts = setupPageText("setup.page.rerun.problem_app_old"), "app_update", []string{"recheck", "fix"}
			links["app"] = haRelayAppPageURL(f.cfg.HAURL) // or update it in Home Assistant, then Check again
		}
		if haDown && picked != nil {
			problem = picked
		}
		picked = nil
		f.page.update(func(v *setupPageState) {
			v.Screen, v.Rows, v.Problem, v.Links, v.Control, v.Fix = "rerun_fix", rows, problem, links, nil, fix
			v.Candidates = slices.DeleteFunc(slices.Clone(v.Candidates), func(c setupPageCandidate) bool { return !haDown || slices.Contains(unreachable, c.URL) })
		})
		if haDown {
			f.pageRerunDiscover(&discovering, &unreachable)
		}
		action, err := f.page.wait(accepts...)
		if err != nil {
			return f.cancelled(), true
		}
		if action.Action == "address" { // one found: step 1 with it, as typed under Fix it
			candidates := f.page.snapshot().Candidates
			i := slices.IndexFunc(candidates, func(c setupPageCandidate) bool { return c.URL == action.Value })
			if i < 0 {
				continue
			}
			host, haURL, ok := pageResolveAddress(action.Value)
			if !ok { // gone since: say so, as step 1 does, and offer the rest
				f.page.update(func(*setupPageState) { unreachable = append(unreachable, action.Value) })
				picked = setupPageText("setup.page.problem.address", "address", candidates[i].Host)
				continue
			}
			f.cfg = applySelectedSetupHost(f.cfg, host, haURL, f.relayURLFlag)
			f.pageRepairing()
			return setupGoTo(setupStepHost), true
		}
		if action.Action == "recheck" {
			f.current = detectSetupStateForAssessment(f.paths, f.cfg, f.state, f.target, f.savedTokenBeforeSetup, f.hadSavedTokenBeforeSetup)
			continue
		}
		if appDown || appUpdate { // another address, or the sign-in that installs, updates, or starts the App: step 1
			f.pageRepairing()
			return setupGoTo(setupStepHost), true
		}
		next := f.routeKnownAddress(setupStepHost)
		if next == setupStepPairing { // not paired: the page signs in, as on a first run
			f.pageRepairing()
			return setupGoTo(setupStepHost), true
		}
		f.pageRepairing("connect", "teach")
		return setupGoTo(next), true
	}
}

// pageRerunDiscover looks for a Home Assistant at another address while the
// saved one doesn't answer (it moved, or this is another home), in the
// background so the screen shows at once. What it finds is only offered;
// the click goes the usual way (step 1, sign-in, pairing). One search at a
// time; Check again starts the next. skip (read under the page's lock):
// found before, then didn't answer when picked.
func (f *setupFlow) pageRerunDiscover(running *atomic.Bool, skip *[]string) {
	if !running.CompareAndSwap(false, true) {
		return
	}
	cfg := f.cfg
	go func() {
		// ponytail: pageDiscover flips a global on Windows; safe while this is
		// the only search (step 1 searches only without a saved address).
		var found []setupPageCandidate
		for _, candidate := range pageDiscover(cfg) {
			if setupDiscoveryEndpointKey(candidate.HAURL) == setupDiscoveryEndpointKey(cfg.HAURL) {
				continue
			}
			name := candidate.Host
			if via := normalizeHostInput(candidate.Via); via != "" && via != name { // its .local name too
				name = via + " (" + name + ")"
			}
			found = append(found, setupPageCandidate{Host: terminalSafeName(name), URL: candidate.HAURL})
		}
		running.Store(false) // before the result shows: Check again after it searches again
		f.page.update(func(v *setupPageState) {
			// Still that screen, and Home Assistant still away.
			if v.Screen == "rerun_fix" && slices.ContainsFunc(v.Rows, func(r setupPageRow) bool { return r.ID == "ha" && r.State == "fail" }) {
				v.Candidates = slices.DeleteFunc(found, func(c setupPageCandidate) bool { return slices.Contains(*skip, c.URL) })
			}
		})
	}()
}

// keepClientsSetUpBefore: a re-run works on the AI assistants set up before;
// one found since waits for its click (pageNewClients). None of them here
// (none set up yet, or all removed): every one found, as on a first run.
func (f *setupFlow) keepClientsSetUpBefore() (before []string) {
	before = slices.DeleteFunc(slices.Clone(f.selectedClients), func(c string) bool { return !slices.Contains(f.state.InstalledClients, c) })
	if len(before) > 0 {
		f.pageNewClients = slices.DeleteFunc(slices.Clone(f.selectedClients), func(c string) bool { return slices.Contains(before, c) })
		f.selectedClients = before
	}
	return before
}

// pageRepairing shows the re-run's progress rows while the usual steps run
// (they set the rows); none: the next screen is step 1.
func (f *setupFlow) pageRepairing(rows ...string) {
	msgs := map[string]*setupPageMsg{
		"connect": setupPageText("setup.page.row.connect"),
		"teach":   setupPageText("setup.page.row.teach", "clients", setupClientNames(f.selectedClients)),
	}
	f.page.update(func(v *setupPageState) {
		v.Screen, v.Problem, v.Links, v.Rows = "repair", nil, nil, nil
		for i, id := range rows {
			state := "todo"
			if i == 0 {
				state = "run"
			}
			v.Rows = append(v.Rows, setupPageRow{ID: id, State: state, Msg: msgs[id]})
		}
	})
}

// pageClientsNotReady: the AI assistants this re-run judges whose HA NOVA
// skills aren't in place.
func (f *setupFlow) pageClientsNotReady() []string {
	return slices.DeleteFunc(slices.Clone(f.selectedClients), func(client string) bool { return clientReadyNow(f.paths, f.state, client) })
}

// pageRerunRows: what is set up, from the same checks as the terminal's
// status screen. problem: the NOVA App doesn't answer at all (nil: it does);
// "Is this computer at home?" only when Home Assistant doesn't answer either.
func (f *setupFlow) pageRerunRows(app setupPageApp) (rows []setupPageRow, problem *setupPageMsg) {
	c := f.current
	appRow := setupPageRow{ID: "app", State: "ok", Msg: setupPageText("setup.page.app.running")}
	switch {
	case app.state == "old":
		appRow.State, appRow.Msg = "todo", setupPageText("setup.page.app.old")
	case app.state == "running" && app.version != "":
		appRow.Msg = setupPageText("setup.page.app.running_version", "version", terminalSafeName(app.version))
	case app.state == "running" || c.RelayOK:
	default:
		appRow.State, appRow.Msg, problem = "fail", setupPageText("setup.page.rerun.app_down"), setupPageText("setup.page.rerun.problem_app_only")
	}
	ha := setupPageRow{ID: "ha", State: "ok", Msg: setupPageText("setup.page.rerun.ha", "host", terminalSafeName(f.cfg.HAHost))}
	if problem != nil && probeHTTPForSetup(f.cfg.HAURL) != nil { // Home Assistant itself doesn't answer
		ha.State, problem = "fail", setupPageText("setup.page.rerun.problem_app")
	}
	computer := setupPageRow{ID: "computer", State: "ok", Msg: setupPageText("setup.page.rerun.computer_ok")}
	if !(c.TokenOK && c.RelayOK && c.WSOK) {
		computer.State, computer.Msg = "fail", setupPageText("setup.page.rerun.computer_fail")
	}
	clients := setupPageRow{ID: "clients", State: "ok", Msg: setupPageText("setup.page.rerun.clients_ok", "clients", setupClientNames(f.selectedClients))}
	if len(f.selectedClients) == 0 { // Claude or ChatGPT in the browser
		clients.Msg = setupPageText("setup.page.clients.web")
	}
	if notReady := f.pageClientsNotReady(); len(notReady) > 0 {
		clients.State, clients.Msg = "fail", setupPageText("setup.page.rerun.clients_fail", "clients", setupClientNames(notReady))
	}
	return []setupPageRow{ha, appRow, computer, clients}, problem
}

// pageRerunControl puts the status rows above step 3's on a working re-run
// (setup got this far: the connection and the skills are in place), and
// the AI assistants found since, to set up with a click.
func (f *setupFlow) pageRerunControl() {
	f.current = setupState{ConfigOK: true, TokenOK: true, RelayOK: true, WSOK: true, SkillsOK: true}
	rows, _ := f.pageRerunRows(f.pageApp())
	links := map[string]string{}
	if rows[1].State != "ok" { // an App that needs an update
		links["app"] = haRelayAppPageURL(f.cfg.HAURL)
	}
	f.page.update(func(v *setupPageState) {
		v.Rows, v.Links, v.NewClients = rows, links, setupClientNames(f.pageNewClients)
	})
}

// pageRerunAddClients sets up HA NOVA in the AI assistants found since the
// last setup (the click on the page), then shows the re-run screen again.
// It holds the generation setup completed with.
func (f *setupFlow) pageRerunAddClients() int {
	adding := f.pageNewClients
	f.selectedClients, f.pageNewClients = append(f.selectedClients, adding...), nil
	f.page.update(func(v *setupPageState) {
		v.Screen, v.Control, v.NewClients = "repair", nil, nil
		v.Rows = []setupPageRow{{ID: "teach", State: "run", Msg: setupPageText("setup.page.row.teach", "clients", setupClientNames(adding))}}
	})
	err := f.ui.working(setupText("setup.skills.installing", "client", setupClientLabel(f.target)), func() error {
		markers, err := f.pageLifecycleMarkers()
		if err != nil {
			return err
		}
		return withClientMutationLock(f.paths, func() error {
			return installClientsAndSaveStateUnlocked(f.paths, &f.state, adding, saveState, markers...)
		})
	})
	if err != nil { // the re-run's end screen says so; the terminal says why
		printHumanErr("%s", setupT("setup.error.install_clients", "error", err))
		f.pageNotes = append(f.pageNotes, setupPageText("setup.page.home.note_clients_failed", "clients", setupClientNames(adding)))
		return 1
	}
	return f.pageControl()
}

// pageRerunChat: a re-run has no owner sign-in, so the chat-app row is a
// switch that Done applies (pageRerunChatOn, pageRerunChatOff). While on it
// shows the connector address and Add to Claude, read without a sign-in,
// as `ha-nova chat-apps status`. Only for an App with chat apps and a Home
// Assistant new enough for them; nil: no row.
func (f *setupFlow) pageRerunChat(linkOrigin func() string) *setupPageChat {
	ha, err := chatAppsReadHA(f.paths)
	base, _, _ := strings.Cut(ha.appVersion, "-") // an rc of the first chat-app App counts as that App
	if err != nil || base == "" || !chatAppsHAVersionOK(ha.version) {
		return nil
	}
	if cmp, err := compareReleaseVersions(base, chatAppsMinAppVersion); err != nil || cmp < 0 {
		return nil
	}
	url := ""
	if origin := chatAppsBase(linkOrigin(), ha.externalURL); origin != "" {
		url = origin + chatAppsConnectorPath
	}
	switch {
	case !ha.present && url == "": // off, and nothing could reach them: what's missing
		return &setupPageChat{State: "noaddress"}
	case !ha.present: // Home Assistant holds the chat-app integration only while they are on
		return &setupPageChat{State: "page_off"}
	case url == "":
		return &setupPageChat{State: "page_on", Problem: setupPageText("setup.chat.on_noaddress")}
	}
	chat := &setupPageChat{State: "page_on", URL: url, ClaudeLink: claudeConnectorLink(url)}
	if !ha.loaded {
		chat.Problem = setupPageText("setup.chat.problem_slow")
	}
	return chat
}

// pageAddServer: "Add another Home Assistant server" on a working re-run
// runs the terminal's add flow, as its completed-setup screen does, held to
// the generation setup completed with.
func (f *setupFlow) pageAddServer() int {
	f.page.handover()
	setupOwnerIngress, setupSignedInUser = nil, "" // a chat-app sign-in here was for this home only
	markers, err := f.pageLifecycleMarkers()
	if err != nil {
		printHumanErr("%s", err)
		return 1
	}
	return startAddServerFlow(f.page.termIn(), f.page.text.out, f.paths, markers...)
}

// pageLifecycleMarkers: the generation setup completed with, and the
// uninstall marker and config as they are now; an error when the
// generation changed since (an uninstall rotates it). nil without one.
func (f *setupFlow) pageLifecycleMarkers() ([][]byte, error) {
	if f.pageGeneration == nil {
		return nil, nil
	}
	uninstall, err := readUninstallMarker(f.paths)
	if err != nil {
		return nil, err
	}
	snapshot, err := readSetupConfigSnapshot(f.paths)
	if err != nil {
		return nil, err
	}
	markers := [][]byte{f.pageGeneration, uninstall, snapshot}
	return markers, ensureSetupLifecycleCurrent(f.paths, markers...)
}

// pageFinish ends a page run: the terminal's banner (step 4 replaces its
// first look; it clears the screen, so it comes first), step 3, then step
// 4 until Done. A working re-run ends on its own screen instead.
func (f *setupFlow) pageFinish() setupStepResult {
	kind := setupOutcomeComplete
	switch {
	case f.pageRerunOK:
		kind = setupOutcomeAlreadyDone
	case f.cfg.Cloud.ready() && f.cfg.RoutePolicy == routePolicyAutomatic:
		kind = setupOutcomeLinkReady
	}
	if f.noClient && kind == setupOutcomeComplete { // Claude or ChatGPT in the browser: as the terminal's no-app finish
		f.page.text.show(setupView{Blocks: []setupBlock{setupSuccess(setupText("setup.done.connected_no_client"))}})
	} else {
		f.page.text.outcome(setupOutcome{Kind: kind, Clients: f.selectedClients, LegacyToken: f.pageRerunOK && legacyAccessSwitchable(f.cfg, probePairingV1ForSetup)})
	}
	if f.pageRerunOK { // the banner cleared the page's address
		fmt.Fprintf(f.page.text.out, "  %s\n", setupT("setup.page.terminal.rerun"))
	}
	exit := f.pageControl()
	if f.pageQuit && f.pageRerunOK {
		f.page.end("saved") // nothing changed
		return setupDone(exit)
	}
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
