package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"slices"
)

// Step 3's chat-app row on the setup page (setupPageChat in
// setup_page_control.go; the shared turn-on is chat_apps.go).

// pageChatOn turns chat apps on after the page's consent click, through the
// owner's ingress session, and shows the restart and Home Assistant loading
// the integration. Step 3 waits meanwhile (spec §11.6: the page waits for
// Home Assistant to return before step 4). A failure keeps the switch, with
// one line; trying again runs the whole action again.
func (f *setupFlow) pageChatOn(c *setupPageChat, offer *chatAppsOffer, show func()) {
	c.State, c.Problem, c.URL, c.ClaudeLink = "run", nil, "", ""
	c.Rows = []setupPageRow{{ID: "chat_on", State: "run", Msg: setupPageText("setup.chat.row_on")}}
	show()
	set := func(id, state string) {
		for i := range c.Rows {
			if c.Rows[i].ID == id {
				c.Rows[i].State = state
			}
		}
	}
	phone := ownerChatPhone(f.paths, setupSignedInUser, offer.state)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt) // Ctrl+C: as in step 3's wait
	defer stop()
	result, err := turnOnChatApps(ctx, setupOwnerIngress, f.paths, offer.state, offer.restart, phone, func(event string) {
		switch event {
		case "on":
			set("chat_on", "ok")
			c.Rows = append(c.Rows, setupPageRow{ID: "chat_ready", State: "run", Msg: setupPageText("setup.chat.row_ready")})
		case "restart":
			c.Rows = slices.Insert(c.Rows, 1, setupPageRow{ID: "chat_restart", State: "run", Msg: setupPageText("setup.chat.row_restart")})
			set("chat_ready", "todo")
		case "back":
			set("chat_restart", "ok")
			set("chat_ready", "run")
		case "ready":
			set("chat_restart", "ok")
			set("chat_ready", "ok")
		}
		show()
	})
	if ctx.Err() != nil {
		f.pageQuit = true // the turn-on may go on in the App; setup ends without the extras
		return
	}
	if err != nil {
		fmt.Fprintf(f.ui.termOut(), "  %s\n", setupT("setup.chat.error", "error", err))
		c.State, c.Rows, c.Problem = "offer", nil, &setupPageMsg{Key: chatAppsProblem(err).Key}
		var refused *chatAppsRefused
		if errors.As(err, &refused) && refused.code == "RESTART_CONSENT_REQUIRED" { // a restart is needed now: ask with it
			offer.restart, c.NoRestart = true, false
		}
		if IsCloudErrorCode(err, CloudErrOuterSessionExpired) {
			c.State = "expired" // a retry can't work: the NOVA page can
		}
		return
	}
	c.State, c.URL, c.ClaudeLink = "on", offer.url, claudeConnectorLink(offer.url)
	offer.state.Enabled = true
	if result.phoneStored {
		name := phone.Name
		offer.state.OwnerPhone = &name
	}
	if result.phoneFailed {
		c.Problem = setupPageText("setup.chat.problem_phone")
	}
	if !result.ready {
		for i := range c.Rows {
			if c.Rows[i].State != "ok" {
				c.Rows[i].State = "fail"
			}
		}
		c.Problem = setupPageText("setup.chat.problem_slow")
	}
	f.pageChatNote(offer.url)
}

// pageChatNote: step 4 repeats the connector address, with Add to Claude.
func (f *setupFlow) pageChatNote(url string) {
	f.pageNotes = append(f.pageNotes, setupPageText("setup.page.home.note_chat", "url", url))
	f.pageChatLink = claudeConnectorLink(url)
}

// pageChatPhone follows a phone linked on step 3 after the chat row was
// built: while chat apps are off, the consent names it; once they are on,
// it goes to the App as the owner's chat-app phone (the owner taps once).
func (f *setupFlow) pageChatPhone(c *setupPageChat, offer *chatAppsOffer) {
	if c == nil {
		return
	}
	if c.State == "on" {
		offer.state.Enabled = true
		if err := handOverOwnerPhone(setupOwnerIngress, f.paths, setupSignedInUser, &offer.state); err != nil {
			fmt.Fprintf(f.ui.termOut(), "  %s\n", setupT("setup.chat.error", "error", err))
			c.Problem = setupPageText("setup.chat.problem_phone")
		}
	}
	c.Phone = chatAppsPhoneName(f.paths, setupSignedInUser, offer.state)
}

// pageChatRow is step 3's row from a read through the owner's session:
// no internet address (one plain line), on (the address and Add to Claude),
// or the switch with its consent.
func (f *setupFlow) pageChatRow(chat *chatAppsOffer) *setupPageChat {
	switch {
	case chat.url == "":
		row := &setupPageChat{State: "noaddress"}
		if chat.state.Enabled {
			row.Problem = setupPageText("setup.chat.on_noaddress")
		}
		return row
	case chat.state.Enabled:
		row := &setupPageChat{State: "on", URL: chat.url, ClaudeLink: claudeConnectorLink(chat.url)}
		if !chat.loaded { // on, but a restart never came: the address doesn't answer yet
			row.Problem = setupPageText("setup.chat.problem_slow")
		}
		if err := handOverOwnerPhone(setupOwnerIngress, f.paths, setupSignedInUser, &chat.state); err != nil {
			row.Problem = setupPageText("setup.chat.problem_phone")
		}
		f.pageChatNote(chat.url)
		return row
	}
	return &setupPageChat{State: "offer", NoRestart: !chat.restart, Phone: chatAppsPhoneName(f.paths, setupSignedInUser, chat.state)}
}

// pageRerunChatOn: Done with the chat-app switch turned on, on a re-run
// (no owner sign-in yet). As `ha-nova chat-apps on`: the owner signs in in a
// browser tab of its own, then the row asks the consent, as step 3 does,
// naming the phone and the restart from the fresh read. Nothing turns on
// before that click.
func (f *setupFlow) pageRerunChatOn(c *setupPageChat, offer *chatAppsOffer, linkOrigin func() string, show func()) {
	c.State, c.Problem, c.URL, c.ClaudeLink = "signin", nil, "", ""
	show()
	out := f.page.text.out // the sign-in's link, as the Link offer's
	ok := chatAppsSignInOK(out, f.cfg)
	if ok {
		var session haSignInSession
		if session, ok = chatAppsOwnerSession(out, f.cfg); ok {
			setupOwnerIngress, setupSignedInUser = session.ingress, session.user.ID
			*offer, ok = readChatAppsOffer(setupOwnerIngress, f.paths, linkOrigin)
		}
	}
	if !ok {
		c.State, c.Problem = "page_off", setupPageText("setup.chat.problem_signin")
		return
	}
	*c = *f.pageChatRow(offer)
	c.Ask = c.State == "offer"
}

// pageRerunChatOff: Done with the chat-app switch turned off, on a re-run:
// `ha-nova chat-apps off` itself (the owner's sign-in, then the check of
// what Home Assistant still holds); the end screen says how it went.
func (f *setupFlow) pageRerunChatOff(c *setupPageChat, show func()) {
	c.State, c.Problem, c.URL, c.ClaudeLink = "signin", nil, "", ""
	show()
	note := setupPageText("setup.page.home.note_chat_off")
	if chatAppsOff(f.page.text.out, f.paths, f.cfg) != 0 {
		note = setupPageText("setup.page.home.note_chat_off_failed")
	}
	f.pageNotes = append(f.pageNotes, note)
}
