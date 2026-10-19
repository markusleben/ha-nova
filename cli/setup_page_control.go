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
}

type setupPagePhone struct {
	ID     string `json:"id"` // the Home Assistant device id
	Name   string `json:"name"`
	Person string `json:"person"`
}

// Test seam: Home Assistant Link remote access is on for this home.
var setupPageLinkActive = func(cfg runtimeConfig) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := discoverCloudFromLocalRelay(ctx, cfg)
	return err == nil
}

// pageControl shows step 3 until "Finish setup", then does what its
// switches say. It returns the exit code a failed Link setup leaves.
func (f *setupFlow) pageControl() int {
	p := f.page
	f.pageControlled = true
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
			f.ui.note(note)
		}
	}
	if f.pageLinkOffered() {
		ctl.Link = "offer"
	}
	show := func() {
		view := ctl
		view.Phones = slices.Clone(ctl.Phones)
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
	if !f.pairedThisRun { // as in the terminal: offered right after a pairing
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
			var line string
			result, line = sendPhoneLink(s, home, links, links[home.owner], all[i], windowWait)
			fmt.Fprintln(f.ui.termOut(), "  "+line)
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
// offer would, and its remote access is on for this home.
func (f *setupFlow) pageLinkOffered() bool {
	coordinator := cloudCoordinatorForSetup
	if f.serviceMode || f.cfg.Cloud != nil || coordinator == nil || !coordinator.Available() ||
		cloudRecoveryHoldProblem(f.cfg) != nil || !cloudSetupPromptEligible(f.page.text.out) {
		return false
	}
	if ready, err := reusableLocalDeviceForCloudSetup(f.cfg); err != nil || !ready {
		return false
	}
	return setupPageLinkActive(f.cfg)
}

// pageAddLink runs the terminal's Link offer, answered yes on the page. Its
// sign-in opens in a browser tab of its own; failures end setup as in the
// terminal, which says what's next.
func (f *setupFlow) pageAddLink() int {
	yes := func(string, string, bool) (bool, error) { return true, nil }
	cfg, attempted, code := offerCloudForCompletedSetup(yes, f.page.text.out, f.paths, f.cfg, false, f.lifecycleMarker...)
	f.cfg = cfg
	if attempted && code != 0 {
		return code
	}
	if attempted && f.cfg.Cloud.ready() {
		f.pageNotes = append(f.pageNotes, setupPageText("setup.page.home.note_link"))
	}
	return 0
}

// pageFinish ends a page run: the terminal's banner (step 4 replaces its
// first look), then step 4 until Done.
func (f *setupFlow) pageFinish() setupStepResult {
	kind := setupOutcomeComplete
	if f.cfg.Cloud.ready() && f.cfg.RoutePolicy == routePolicyAutomatic {
		kind = setupOutcomeLinkReady
	}
	f.page.text.outcome(setupOutcome{Kind: kind, Clients: f.selectedClients})
	if !f.pageQuit {
		fmt.Fprintf(f.page.text.out, "  %s\n", setupT("setup.page.terminal.home"))
		f.pageHome()
	}
	f.page.end("closed")
	return setupDone(0)
}
