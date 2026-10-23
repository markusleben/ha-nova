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
// there, and moves to another phone with the linked one's OK; the Claude
// Code rule switch is preselected and written only on
// "Continue"; the Home Assistant Link row shows only while Link remote
// access is active. The terminal asks none of these on the page path.

type setupPageControl struct {
	Window bool `json:"window"` // the confirmation window can open here
	// Phone: pick (Phones can be linked), waiting (for the tap on
	// PhoneName), linked (Phones: the ones it can move to), moving (OldPhone
	// OKs the move first, then PhoneName gets the link question), none (no
	// phone found), unread (the phones couldn't be read; Check again),
	// unavailable (not now: no row).
	Phone        string           `json:"phone"`
	Phones       []setupPagePhone `json:"phones,omitempty"`
	OldPhone     string           `json:"old_phone,omitempty"`
	Proposed     bool             `json:"proposed,omitempty"` // Phones[0] is the signed-in user's own phone
	PhoneName    string           `json:"phone_name,omitempty"`
	PhoneOS      string           `json:"phone_os,omitempty"` // waiting, moving: PhoneName's platform (ios, android), for how to tap and a timeout's hint
	PhoneProblem *setupPageMsg    `json:"phone_problem,omitempty"`
	Rule         string           `json:"rule,omitempty"`       // offer, on (already there); "" no row
	RuleCount    int              `json:"rule_count,omitempty"` // the rules it adds (offer) or has (on)
	Link         string           `json:"link,omitempty"`       // offer, run; "" no row
	Chat         *setupPageChat   `json:"chat,omitempty"`       // nil: no row (chat_apps.go)
}

// setupPageChat is the "Use HA NOVA from your phone too" row (browser flow:
// "Use HA NOVA in Claude and ChatGPT"; spec §11.6). It shows only when the
// App has chat apps and Home Assistant is 2026.10 or newer; without Link
// remote access or a public HTTPS address (chatAppsBase) it says what they
// need, with Home Assistant's Link page and Check again. The page shows the restart
// consent before it posts "chat"; the click on it is that consent.
type setupPageChat struct {
	State      string         `json:"state"`                 // offer, run, on, expired (the owner session ran out), noaddress (no Link, no public HTTPS address), page_on/page_off (a re-run: no owner sign-in yet, Done applies the switch), signin (Done's owner sign-in runs)
	Ask        bool           `json:"ask,omitempty"`         // offer after Done's sign-in: the page opens the consent
	Attempt    int            `json:"attempt,omitempty"`     // page_off: Done's sign-ins that failed, so the page resets its switch even when it never saw signin
	Rows       []setupPageRow `json:"rows,omitempty"`        // while run (and after): on, restart, ready
	URL        string         `json:"url,omitempty"`         // the connector address, once on
	ClaudeLink string         `json:"claude_link,omitempty"` // Add to Claude (claudeConnectorLink), once on
	OrgLink    string         `json:"org_link,omitempty"`    // the same for a Team or Enterprise owner (claudeOrgConnectorLink)
	Phone      string         `json:"phone,omitempty"`       // the phone that confirms for chat apps; "": a code in the chat
	LinkPage   string         `json:"link_page,omitempty"`   // noaddress, off: Home Assistant's Link page (haLinkPageURL)
	Guide      string         `json:"guide,omitempty"`       // noaddress, off: the guide's own-HTTPS-address part
	Checked    string         `json:"checked,omitempty"`     // noaddress, off: the last Check again: off (read, still no address), failed (couldn't read)
	NoRestart  bool           `json:"no_restart,omitempty"`  // Home Assistant runs the integration already: turning on doesn't restart it
	Problem    *setupPageMsg  `json:"problem,omitempty"`
}

// needsLink: chat apps are off and nothing could reach them; the row
// offers Home Assistant Link and Check again.
func (c *setupPageChat) needsLink() bool {
	return c != nil && c.State == "noaddress" && c.Problem == nil
}

type setupPagePhone struct {
	ID     string `json:"id"` // the Home Assistant device id
	Name   string `json:"name"`
	Person string `json:"person"`
	OS     string `json:"os,omitempty"` // ios, android; "" unknown
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

// pageControl shows step 3 until "Continue", then does what its
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
			ctl.Rule, ctl.RuleCount = "offer", len(missing)
		case note == "":
			ctl.Rule, ctl.RuleCount = "on", len(rules)
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
	} else if f.pageRerunning && f.cfg.Cloud.ready() && f.cfg.RoutePolicy == routePolicyAutomatic {
		ctl.Link = "on" // the fallback this row offers; other routes stay with `ha-nova server route`
	}
	chatOrigin := func() string { return chatAppsLinkOrigin(f.cfg, linkOrigin()) } // as `ha-nova chat-apps`
	var chat chatAppsOffer
	chatOK := false
	readChat := func() *setupPageChat { // also on the row's Check again
		if (f.pairedThisRun || f.pageRerunning) && !f.cloudSetupPaused { // as the phone row; nil without this run's owner sign-in
			chat, chatOK = readChatAppsOffer(setupOwnerIngress, f.paths, chatOrigin)
		}
		var row *setupPageChat
		switch {
		case chatOK:
			row = f.pageChatRow(&chat)
		case f.pageRerunning:
			row = f.pageRerunChat(chatOrigin)
		}
		if f.noClient && row != nil && row.State == "offer" {
			row.Ask = true // Claude or ChatGPT in the browser: the consent opens; only its button turns them on
		}
		return row
	}
	ctl.Chat = readChat()
	screen := "control"
	if f.pageRerunOK { // the re-run screen: what is set up, then these rows
		screen = "rerun"
		f.pageRerunControl()
	}
	show := func() {
		view := ctl
		view.Phones = slices.Clone(ctl.Phones)
		if ctl.Chat != nil { // pageChatOn keeps changing its own copy
			row := *ctl.Chat
			row.Rows = slices.Clone(row.Rows)
			if row.needsLink() { // where to turn on Link remote access
				row.LinkPage, row.Guide = haLinkPageURL(f.cfg.HAURL), chatAppsGuideURL+"#what-you-need"
			}
			view.Chat = &row
		}
		p.update(func(v *setupPageState) {
			v.Screen, v.Control, v.Problem, v.Fix = screen, &view, nil, ""
			if !f.pageRerunOK {
				v.Links = nil
			}
		})
	}
	for {
		show()
		accepts := []string{"finish"}
		if ctl.Phone == "pick" || ctl.Phone == "linked" && len(ctl.Phones) > 0 {
			accepts = append(accepts, "phone")
		}
		if ctl.Phone == "unread" {
			accepts = append(accepts, "recheck")
		}
		if ctl.Chat.needsLink() {
			accepts = append(accepts, "chat_recheck")
		}
		if ctl.Chat != nil && ctl.Chat.State == "offer" {
			accepts = append(accepts, "chat")
		}
		if f.pageRerunOK {
			if name, _ := requestedServerSelection(); name == "" {
				accepts = append(accepts, "server")
			}
			if len(f.pageNewClients) > 0 {
				accepts = append(accepts, "clients")
			}
		}
		action, err := p.wait(accepts...)
		if err != nil {
			f.pageQuit = true // Ctrl+C or idle: setup is done, without the extras
			return 0
		}
		switch action.Action {
		case "recheck": // its buttons stay disabled while it reads
			f.pagePhones(&ctl)
		case "chat_recheck": // Link remote access may be on now
			originRead = false
			if row := readChat(); row == nil {
				ctl.Chat.Checked = "failed" // the old row stays: never a stale "still off"
			} else {
				ctl.Chat = row
				if ctl.Chat.needsLink() {
					ctl.Chat.Checked = "off"
				}
			}
			if ctl.Link == "" && f.pageLinkOffered() && linkOrigin() != "" {
				ctl.Link = "offer"
			}
		case "server":
			return f.pageAddServer()
		case "clients":
			return f.pageRerunAddClients()
		case "phone":
			i := slices.IndexFunc(ctl.Phones, func(ph setupPagePhone) bool { return ph.ID == action.Value })
			switch {
			case i < 0:
			case ctl.Phone == "linked": // Use another phone
				ctl.Phone, ctl.OldPhone, ctl.PhoneName, ctl.PhoneOS, ctl.PhoneProblem = "moving", ctl.PhoneName, ctl.Phones[i].Name, ctl.Phones[i].OS, nil
				show()
				if f.pageMovePhone(&ctl, action.Value) {
					f.pageChatMoved(ctl.Chat, &chat)
				}
			default:
				ctl.Phone, ctl.PhoneName, ctl.PhoneOS, ctl.PhoneProblem = "waiting", ctl.Phones[i].Name, ctl.Phones[i].OS, nil
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
			choice, ok := pageFinishChoices(action.Value)
			if !ok {
				continue // never sent by the page
			}
			f.pageChatOff = chatAppsOffOnPage(ctl.Chat)
			if ctl.Chat != nil && choice.chatOn && ctl.Chat.State == "page_off" {
				// The owner signs in, then the row asks as step 3 does; the
				// next Done applies the other switches.
				f.pageRerunChatOn(ctl.Chat, &chat, chatOrigin, show)
				continue
			}
			if ctl.Chat != nil && choice.chatOff && ctl.Chat.State == "page_on" {
				f.pageRerunChatOff(ctl.Chat, show)
			}
			if choice.rule && ctl.Rule == "offer" {
				line, added := addClaudeRules(f.paths, rulePath, rules, missing)
				if line != "" {
					fmt.Fprintln(out, "  "+line)
				}
				note := setupPageText("setup.page.home.note_rule", "count", len(missing))
				if !added {
					note = setupPageText("setup.page.home.note_rule_failed", "count", len(missing))
				}
				f.pageNotes = append(f.pageNotes, note)
			}
			if choice.link && ctl.Link == "offer" {
				ctl.Link = "run"
				show()
				return f.pageAddLink()
			}
			return 0
		}
	}
}

// pageFinish is what "Continue" (Done on a re-run) sends: the switches
// it applies.
type pageFinish struct{ rule, link, chatOn, chatOff bool }

// pageFinishChoices reads "Continue"'s switches: any of "rule", "link",
// "chat_on", "chat_off", comma-separated, each once; "" for none.
func pageFinishChoices(value string) (pageFinish, bool) {
	var choice pageFinish
	if value == "" {
		return choice, true
	}
	for _, part := range strings.Split(value, ",") {
		var flag *bool
		switch part {
		case "rule":
			flag = &choice.rule
		case "link":
			flag = &choice.link
		case "chat_on":
			flag = &choice.chatOn
		case "chat_off":
			flag = &choice.chatOff
		}
		if flag == nil || *flag {
			return pageFinish{}, false
		}
		*flag = true
	}
	return choice, !(choice.chatOn && choice.chatOff)
}

// pageLinkOffered: Home Assistant Link can be added now, as the terminal's
// offer would (pageControl also needs its remote access on for this home).
func (f *setupFlow) pageLinkOffered() bool {
	coordinator := cloudCoordinatorForSetup
	if f.serviceMode || f.noClient || f.cfg.Cloud != nil || coordinator == nil || !coordinator.Available() || // Link here is for this computer's AI app
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
	markers, err := f.pageLifecycleMarkers()
	if err != nil {
		printHumanErr("%s", err)
		return failed(1)
	}
	saved, err := loadConfig(f.paths)
	if err != nil {
		printHumanErr("%s", err)
		return failed(1)
	}
	yes := func(string, string, bool) (bool, error) { return true, nil }
	cfg, attempted, code := offerCloudForCompletedSetup(yes, f.page.text.out, f.paths, saved, false, false, markers...)
	f.cfg = cfg
	switch {
	case attempted && code != 0:
		return failed(code)
	case attempted && f.cfg.Cloud.ready():
		f.pageNotes = append(f.pageNotes, setupPageText("setup.page.home.note_link"))
	}
	return 0
}
