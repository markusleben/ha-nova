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
	lang, text string
	phone      bool
}

// offered: the plan's window with Confirm for this ask, or nil: the App
// didn't offer the window, or no window opens here (a chat app, windows
// turned off, no display). A write held mid-apply (sendWrite) has no
// plan text: nil.
func (c *gateConfirm) offered(s *relaySession, opened gateOpened) *gateConfirm {
	if c == nil || c.text == "" || !slices.Contains(opened.Channels, "window") || s.gateChat() || windowOff(s.paths) || !windowAvailable() {
		return nil
	}
	return &gateConfirm{lang: c.lang, text: c.text, phone: slices.Contains(opened.Channels, "phone")}
}

// gateConfirmWindow: the plan's window with Confirm, its footer saying
// where else it can be confirmed; fits: shown uncut (windowFits).
func gateConfirmWindow(c *gateConfirm) (windowText, bool) {
	w := planWindow(c.lang, c.text, false, confirmT(c.lang, "button.confirm"))
	w.footer = confirmT(c.lang, "window.footer_relay")
	if c.phone {
		w.footer = confirmT(c.lang, "window.footer_relay_phone")
	}
	_, fits := windowFits(c.lang, c.text)
	return w, fits
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

// gateWindowApprove sends Confirm to the App (op window). ended: the ask
// ended, approved through the window or, meanwhile, otherwise (cancelled,
// expired; also approved on the phone or the page). Not ended: the App
// didn't take it (409: this computer may not confirm now; a 503 that
// stayed for gateWindowRetry; no readable answer), and the ask still waits
// for the phone and the page.
func (s *relaySession) gateWindowApprove(ctx context.Context, askID string) (state, grant string, ended bool) {
	until := time.Now().Add(gateWindowRetry)
	for {
		var answer struct {
			State   string `json:"state"`
			GrantID string `json:"grant_id"`
		}
		err := s.gateCall(ctx, gateAskRoute, gateWorkerStep, map[string]any{"ask_id": askID, "op": "window"}, &answer)
		var call *relayCallError
		switch {
		case err == nil && answer.State == "approved" && answer.GrantID != "",
			err == nil && answer.State != "approved" && answer.State != "waiting" && answer.State != "":
			return answer.State, answer.GrantID, true
		case errors.As(err, &call) && call.status == http.StatusServiceUnavailable && time.Until(until) > 0:
			select {
			case <-ctx.Done():
				return "", "", false
			case <-time.After(min(time.Second, time.Until(until))):
			}
			continue
		}
		return "", "", false
	}
}
