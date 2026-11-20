package main

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Confirm in this computer's window on a relay plan's ask (0.32 spec §4,
// §6): the App offers the window for an ask (channels "window") only for
// its owner's own computer, and approves it on Confirm (op window) after it
// checks that again. The window shows the stored plan's own text, as a
// window plan's; nothing on this computer approves by itself.

// gateWindowRetry: how long Confirm tries again on a 503 before the window
// says this computer can't confirm right now; tests shorten it.
var gateWindowRetry = 5 * time.Second

// gateConfirm: a relay plan's own window text (confirmText) in its language,
// and whether the App asks the phone too.
type gateConfirm struct {
	id, lang, text string // id: the stored plan's (gate_ask_join.go)
	phone          bool
	registry       bool // the plan is typed because the entity registry couldn't be read
}

// offered: the plan's window with Confirm for this ask, or nil: the App
// didn't offer the window, or no window opens here (a chat app, windows
// turned off, no display). A write held mid-apply (sendWrite) has no
// plan text: nil.
func (c *gateConfirm) offered(s *relaySession, opened gateOpened) *gateConfirm {
	if c == nil || c.text == "" || !slices.Contains(opened.Channels, "window") || s.gateChat() || windowOff(s.paths) || !windowAvailable() {
		return nil
	}
	return &gateConfirm{id: c.id, lang: c.lang, text: c.text, phone: slices.Contains(opened.Channels, "phone")}
}

// fits: the plan's window shows it uncut (windowFits), so it can Confirm.
func (c *gateConfirm) fits() bool {
	_, fits := windowFits(c.lang, c.text)
	return fits
}

// confirmsHere: this ask's window here can Confirm (offered, and the plan
// fits it). Only then the window stands in for a phone the App lacks
// (gateMoveFirst is skipped); a summary-only window never does.
func (c *gateConfirm) confirmsHere(s *relaySession, opened gateOpened) bool {
	offered := c.offered(s, opened)
	return offered != nil && offered.fits()
}

// gateConfirmWindow: the plan's window with Confirm, its footer saying
// where else it can be confirmed; fits: shown uncut.
func gateConfirmWindow(c *gateConfirm) (windowText, bool) {
	w := planWindow(c.lang, c.text, false, confirmT(c.lang, "button.confirm"))
	w.footer = confirmT(c.lang, "window.footer_relay")
	if c.phone {
		w.footer = confirmT(c.lang, "window.footer_relay_phone")
	}
	return w, c.fits()
}

// gateSummary: a plan too long for the window, never confirmed there: its
// title and the whole lines that fit, where the rest is, then rest (the
// waiting text).
func gateSummary(lang, text, rest string) string {
	title, body, _ := strings.Cut(text, "\n\n")
	more := confirmT(lang, "gate.window_more")
	kept := title + "\n"
	for _, line := range strings.Split(body, "\n") {
		if _, fits := windowFits(lang, kept+"\n"+line+"\n"+more+"\n\n"+rest); !fits {
			break
		}
		kept += "\n" + line
	}
	return kept + "\n" + more + "\n\n" + rest
}

// gateWindowApprove sends Confirm to the App (op window), all tries within
// gateWindowRetry. ended: the ask ended, approved through the window or,
// meanwhile, otherwise (cancelled, expired; also approved on the phone or
// the page). Not ended: the App didn't take it in time (409: this computer
// may not confirm now; a 503 or a slow answer that outlasted
// gateWindowRetry; no readable answer), and the ask still waits for the
// phone and the page (ctx, the ask's own, stays alive).
func (s *relaySession) gateWindowApprove(ctx context.Context, askID string) (state, grant string, ended bool) {
	ctx, cancel := context.WithTimeout(ctx, gateWindowRetry)
	defer cancel()
	for ctx.Err() == nil {
		var answer struct {
			State   string `json:"state"`
			GrantID string `json:"grant_id"`
		}
		deadline, _ := ctx.Deadline()
		err := s.gateCall(ctx, gateAskRoute, time.Until(deadline), map[string]any{"ask_id": askID, "op": "window"}, &answer)
		var call *relayCallError
		switch {
		case err == nil && answer.State == "approved" && answer.GrantID != "",
			err == nil && answer.State != "approved" && answer.State != "waiting" && answer.State != "":
			return answer.State, answer.GrantID, true
		case errors.As(err, &call) && call.status == http.StatusServiceUnavailable:
			select {
			case <-ctx.Done():
			case <-time.After(time.Second):
			}
			continue
		}
		return "", "", false
	}
	return "", "", false
}
