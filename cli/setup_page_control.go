package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Step 3 of the setup page, "You stay in control"
// (docs/work/2026-10-18-onboarding-0.27-spec.md §4): the confirmation window
// is on; the phone row links one phone with one notification and a tap
// there; the Claude Code rule switch is preselected and written only on
// "Finish setup"; the Home Assistant Link row shows only while Link remote
// access is active. The terminal asks none of these on the page path.

type setupPageControl struct {
	Window bool `json:"window"` // the confirmation window can open here
	// Phone: pick (Phones can be linked), waiting (for the tap on
	// PhoneName), linked, none (no phone found), unavailable (not now).
	Phone        string           `json:"phone"`
	Phones       []setupPagePhone `json:"phones,omitempty"`
	Proposed     bool             `json:"proposed,omitempty"` // Phones[0] is the signed-in user's own phone
	PhoneName    string           `json:"phone_name,omitempty"`
	PhoneProblem *setupPageMsg    `json:"phone_problem,omitempty"`
	Rule         string           `json:"rule,omitempty"` // offer, on (already there); "" no row
	Link         string           `json:"link,omitempty"` // offer, run; "" no row
	Chat         *setupPageChat   `json:"chat,omitempty"` // nil: no row (chat_apps.go)
}

// setupPageChat is the "Use HA NOVA in Claude and ChatGPT" row (spec
// §11.6). It shows only when the App has chat apps and Home Assistant is
// 2026.10 or newer; without Link remote access or a public HTTPS address
// (chatAppsBase) it is one plain line. The page shows the restart
// consent before it posts "chat"; the click on it is that consent.
type setupPageChat struct {
	State      string         `json:"state"`                 // offer, run, on, expired (the owner session ran out), noaddress (no Link, no public HTTPS address)
	Rows       []setupPageRow `json:"rows,omitempty"`        // while run (and after): on, restart, ready
	URL        string         `json:"url,omitempty"`         // the connector address, once on
	ClaudeLink string         `json:"claude_link,omitempty"` // Add to Claude (claudeConnectorLink), once on
	Phone      string         `json:"phone,omitempty"`       // the phone that confirms for chat apps; "": a code in the chat
	Problem    *setupPageMsg  `json:"problem,omitempty"`
}

type setupPagePhone struct {
	ID     string `json:"id"` // the Home Assistant device id
	Name   string `json:"name"`
	Person string `json:"person"`
}

// Test seam: the Home Assistant Link address while its remote access is on
// for this home, else "".
var setupLinkRemoteOrigin = func(cfg runtimeConfig) string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	found, err := discoverCloudFromLocalRelay(ctx, cfg)
	if err != nil {
		return ""
	}
	return found.Origin.InputOrigin // the address Home Assistant's cloud/status names
}

// pageControl shows step 3 until "Finish setup", then does what its
// switches say. It runs once setup is complete, after the outcome banner,
// as the terminal's offers do, and returns the exit code a failed Link
// setup leaves.
func (f *setupFlow) pageControl() int {
	p := f.page
	out := f.ui.termOut()
	ctl := setupPageControl{Window: windowAvailable() && !windowOff(f.paths)}
	f.pagePhones(&ctl)
	var rulePath string
	var rules, missing []string
	if slices.Contains(f.selectedClients, "claude") {
		var note string
		rulePath, rules, missing, note = claudeRulesPending(f.paths)
		switch {
		case len(missing) > 0:
			ctl.Rule = "offer"
		case note == "":
			ctl.Rule = "on"
		default:
			fmt.Fprintln(out, "  "+note)
		}
	}
	origin, originRead := "", false
	linkOrigin := func() string { // read once, only when a row needs it
		if !originRead {
			origin, originRead = setupLinkRemoteOrigin(f.cfg), true
		}
		return origin
	}
	if f.pageLinkOffered() && linkOrigin() != "" {
		ctl.Link = "offer"
	}
	var chat chatAppsOffer
	chatOK := false
	if f.pairedThisRun && !f.cloudSetupPaused { // as the phone row: right after this run's sign-in pairing
		chat, chatOK = readChatAppsOffer(setupOwnerIngress, f.paths, linkOrigin)
	}
	switch {
	case chatOK && chat.url == "": // no internet address for Home Assistant: one plain line
		ctl.Chat = &setupPageChat{State: "noaddress"}
		if chat.state.Enabled {
			ctl.Chat.Problem = setupPageText("setup.chat.on_noaddress")
		}
	case chatOK && chat.state.Enabled:
		ctl.Chat = &setupPageChat{State: "on", URL: chat.url, ClaudeLink: claudeConnectorLink(chat.url)}
		if !chat.loaded { // on, but a restart never came: the address doesn't answer yet
			ctl.Chat.Problem = setupPageText("setup.chat.problem_slow")
		}
		if err := handOverOwnerPhone(setupOwnerIngress, f.paths, setupSignedInUser, &chat.state); err != nil {
			ctl.Chat.Problem = setupPageText("setup.chat.problem_phone")
		}
		f.pageChatNote(chat.url)
	case chatOK:
		ctl.Chat = &setupPageChat{State: "offer", Phone: chatAppsPhoneName(f.paths, setupSignedInUser, chat.state)}
	}
	show := func() {
		view := ctl
		view.Phones = slices.Clone(ctl.Phones)
		if ctl.Chat != nil { // pageChatOn keeps changing its own copy
			row := *ctl.Chat
			row.Rows = slices.Clone(row.Rows)
			view.Chat = &row
		}
		p.update(func(v *setupPageState) {
			v.Screen, v.Control, v.Problem, v.Fix, v.Links = "control", &view, nil, "", nil
		})
	}
	for {
		show()
		accepts := []string{"finish"}
		if ctl.Phone == "pick" {
			accepts = append(accepts, "phone")
		}
		if ctl.Chat != nil && ctl.Chat.State == "offer" {
			accepts = append(accepts, "chat")
		}
		action, err := p.wait(accepts...)
		if err != nil {
			f.pageQuit = true // Ctrl+C or idle: setup is done, without the extras
			return 0
		}
		switch action.Action {
		case "phone":
			if i := slices.IndexFunc(ctl.Phones, func(ph setupPagePhone) bool { return ph.ID == action.Value }); i >= 0 {
				ctl.Phone, ctl.PhoneName, ctl.PhoneProblem = "waiting", ctl.Phones[i].Name, nil
				show()
				f.pageLinkPhone(&ctl, action.Value)
				if ctl.Phone == "linked" {
					f.pageChatPhone(ctl.Chat, &chat)
				}
			}
		case "chat":
			if action.Value == "on" {
				f.pageChatOn(ctl.Chat, &chat, show)
				if f.pageQuit {
					return 0 // Ctrl+C while it ran: setup is done, without the extras
				}
			}
		case "finish":
			rule, link, ok := pageFinishChoices(action.Value)
			if !ok {
				continue // never sent by the page
			}
			if rule && ctl.Rule == "offer" {
				line, added := addClaudeRules(f.paths, rulePath, rules, missing)
				if line != "" {
					fmt.Fprintln(out, "  "+line)
				}
				note := setupPageText("setup.page.home.note_rule")
				if !added {
					note = setupPageText("setup.page.home.note_rule_failed")
				}
				f.pageNotes = append(f.pageNotes, note)
			}
			if link && ctl.Link == "offer" {
				ctl.Link = "run"
				show()
				return f.pageAddLink()
			}
			return 0
		}
	}
}

// pageFinishChoices reads "Finish setup"'s switches: "rule", "link", both
// ("rule,link"), or none ("").
func pageFinishChoices(value string) (rule, link, ok bool) {
	if value == "" {
		return false, false, true
	}
	for _, part := range strings.Split(value, ",") {
		switch {
		case part == "rule" && !rule:
			rule = true
		case part == "link" && !link:
			link = true
		default:
			return false, false, false
		}
	}
	return rule, link, true
}

// pagePhones fills the phone row from a read of this home's phones. A first
// link only: a phone linked before shows as linked; one that can't be asked
// now stays with `ha-nova setup confirm-phone`, which settles it safely.
func (f *setupFlow) pagePhones(ctl *setupPageControl) {
	ctl.Phone = "unavailable"
	if !f.pairedThisRun || f.cloudSetupPaused { // as in the terminal: offered right after a pairing
		return
	}
	home, links, ok := f.pageReadPhones()
	if !ok {
		return
	}
	current, status := home.status(links, nil)
	switch status {
	case phoneReady:
		ctl.Phone, ctl.PhoneName = "linked", terminalSafeName(current.targets[0].Name)
		return
	case phoneNone:
	default:
		return
	}
	all := home.eligible("")
	if len(all) == 0 {
		ctl.Phone = "none"
		return
	}
	if own := home.eligible(setupSignedInUser); setupSignedInUser != "" && len(own) == 1 {
		rest := slices.DeleteFunc(slices.Clone(all), func(t phoneTarget) bool { return t.DeviceID == own[0].DeviceID })
		all = append([]phoneTarget{own[0]}, rest...)
		ctl.Proposed = true
	}
	ctl.Phone = "pick"
	for _, t := range all {
		ctl.Phones = append(ctl.Phones, setupPagePhone{ID: t.DeviceID, Name: terminalSafeName(t.Name), Person: terminalSafeName(t.Person)})
	}
}

// pageReadPhones reads the phones through the paired Relay; ok=false when
// they can't be read, or the Relay signs in as the owner (a tap couldn't be
// told apart from HA NOVA itself).
func (f *setupFlow) pageReadPhones() (phoneHome, map[string]phoneEntry, bool) {
	s, err := newRelaySession(f.paths, verbOptions{})
	if err != nil {
		return phoneHome{}, nil, false
	}
	defer s.close()
	links, readable := phoneLinks(f.paths)
	home, err := lookupPhones(s, links)
	return home, links, readable && err == nil && home.relay != home.owner
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
		ctl.Phone, ctl.Phones, ctl.Proposed = "linked", nil, false
		return
	}
	ctl.Phone, ctl.PhoneProblem = "pick", pagePhoneProblem(result, ctl.PhoneName)
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

// pageLinkOffered: Home Assistant Link can be added now, as the terminal's
// offer would (pageControl also needs its remote access on for this home).
func (f *setupFlow) pageLinkOffered() bool {
	coordinator := cloudCoordinatorForSetup
	if f.serviceMode || f.cfg.Cloud != nil || coordinator == nil || !coordinator.Available() ||
		cloudRecoveryHoldProblem(f.cfg) != nil || !cloudSetupPromptEligible(f.page.text.out) {
		return false
	}
	ready, err := reusableLocalDeviceForCloudSetup(f.cfg)
	return err == nil && ready
}

// pageAddLink runs the terminal's Link offer, answered yes on the page,
// after setup completed. Like `ha-nova cloud add` it starts from the saved
// config, and it holds the generation and config setup completed with: an
// uninstall or another change meanwhile fails closed. Its sign-in opens in
// a browser tab of its own. A failure keeps the exit code the terminal
// gives it, but step 4 still shows: the local connection works.
func (f *setupFlow) pageAddLink() int {
	failed := func(code int) int {
		f.pageNotes = append(f.pageNotes, setupPageText("setup.page.home.note_link_failed"))
		return code
	}
	var markers [][]byte
	if f.pageGeneration != nil {
		uninstall, errU := readUninstallMarker(f.paths)
		snapshot, errS := readSetupConfigSnapshot(f.paths)
		if errU != nil || errS != nil {
			return failed(1)
		}
		markers = [][]byte{f.pageGeneration, uninstall, snapshot}
		if err := ensureSetupLifecycleCurrent(f.paths, markers...); err != nil {
			printHumanErr("%s", err)
			return failed(1)
		}
	}
	saved, err := loadConfig(f.paths)
	if err != nil {
		printHumanErr("%s", err)
		return failed(1)
	}
	yes := func(string, string, bool) (bool, error) { return true, nil }
	cfg, attempted, code := offerCloudForCompletedSetup(yes, f.page.text.out, f.paths, saved, false, markers...)
	f.cfg = cfg
	switch {
	case attempted && code != 0:
		return failed(code)
	case attempted && f.cfg.Cloud.ready():
		f.pageNotes = append(f.pageNotes, setupPageText("setup.page.home.note_link"))
	}
	return 0
}

// pageFinish ends a page run: the terminal's banner (step 4 replaces its
// first look; it clears the screen, so it comes first), step 3, then step
// 4 until Done.
func (f *setupFlow) pageFinish() setupStepResult {
	kind := setupOutcomeComplete
	switch {
	case f.cloudSetupPaused:
		kind = setupOutcomeLinkPaused
	case f.cfg.Cloud.ready() && f.cfg.RoutePolicy == routePolicyAutomatic:
		kind = setupOutcomeLinkReady
	}
	f.page.text.outcome(setupOutcome{Kind: kind, Clients: f.selectedClients})
	exit := f.pageControl()
	if f.pageQuit {
		f.page.end("skipped") // connected, but step 3's choices weren't applied
		return setupDone(exit)
	}
	fmt.Fprintf(f.page.text.out, "  %s\n", setupT("setup.page.terminal.home"))
	f.pageHome()
	f.page.end("closed")
	return setupDone(exit)
}
