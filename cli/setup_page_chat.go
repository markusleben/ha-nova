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
