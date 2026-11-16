package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"time"
)

// Waiting for the person on a relay plan's ask (gate_client.go): the long
// poll, the page link, and the window on this computer.

// gateWait long-polls the ask until it ends, wait runs out (gateAsk's
// share of the command's wait, or of ha-nova mcp's ask life; a setup link's
// own), or HA NOVA is stopped (planAsks). An end without an approval
// cancels the ask. The window confirms only a plan the App offers it for
// (confirm, gate_window_confirm.go); otherwise it only opens Home Assistant
// or cancels. Its answer is read before an approval counts, so a Cancel
// that raced the approval wins. A link ask (gateMovePhone) ends linked,
// only on the phones: no window.
func (s *relaySession) gateWait(askID string, link bool, wait time.Duration, confirm *gateConfirm) (state, grant string) {
	deadline := time.Now().Add(wait)
	if flag := s.approval(); flag != nil {
		flag.Store(map[bool]int32{false: gateAsking, true: gateLinking}[link])
		defer flag.Store(0)
	}
	ctx, cancel := context.WithDeadline(planAsks, deadline)
	defer cancel()
	type end struct{ state, grant string }
	polls := make(chan end, 1)
	go func() {
		for {
			var answer struct {
				State   string `json:"state"`
				GrantID string `json:"grant_id"`
			}
			if err := s.gateCall(ctx, gateAskRoute, gatePollTimeout, map[string]any{"ask_id": askID, "op": "wait"}, &answer); err != nil {
				var call *relayCallError
				polls <- end{state: map[bool]string{false: "lost", true: "unknown"}[errors.As(err, &call) && call.code == "GATE_ASK_UNKNOWN"]} // unknown: the App doesn't hold it (any more)
				return
			}
			if answer.State != "waiting" {
				polls <- end{answer.State, answer.GrantID}
				return
			}
		}
	}()
	page := s.gatePageURL(askID)
	waiting := confirmT(confirmLanguage(s.lang), "gate.waiting")
	window, raced, confirms := (<-chan windowAnswer)(nil), func(<-chan windowAnswer) bool { return false }, false
	phase := func() { // what a job's waiting answer names (waitingForApproval)
		if flag := s.approval(); flag != nil && confirms {
			flag.Store(gateAskingWindow)
		} else if flag != nil && !link {
			flag.Store(gateAsking)
		}
	}
	switch {
	case link:
		waiting = confirmT(confirmLanguage(s.lang), "gate.link_waiting")
	case confirm != nil:
		waiting = confirmT(confirm.lang, "gate.waiting_window")
		fallthrough
	default:
		window, raced, confirms = s.gateWindow(ctx, deadline, page, confirm, false)
		phase()
	}
	fmt.Fprintln(s.call.stderr(), waiting)
	stopped := func() (string, string) {
		if raced(window) { // a Cancel that raced the end wins: nothing keeps or joins this ask
			s.gateCancel(askID)
			return "cancelled", ""
		}
		if planAsks.Err() == nil && s.approval() == nil && !link { // the CLI's own wait ended: gateAsk keeps the ask for the plan's next apply, or ends it
			return "timeout", ""
		}
		s.gateCancel(askID)
		if planAsks.Err() != nil {
			return "stopped", ""
		}
		return "timeout", ""
	}
	for {
		select {
		case answer := <-window:
			window = nil
			switch {
			case answer == windowCancelled || answer == windowClosed:
				s.gateCancel(askID)
				return "cancelled", ""
			case answer == windowConfirmed && confirms:
				if state, grant, ended := s.gateWindowApprove(ctx, askID); ended {
					return state, grant
				}
				window, raced, confirms = s.gateWindow(ctx, deadline, page, confirm, true) // still open for the phone and the page
				phase()
			case answer == windowConfirmed && page != "": // a waiting window's first button: open Home Assistant (or hide the window), never approve
				_ = gateOpenHA(page)
			default: // closed without an answer, or it never opened: the phone and the page are left
				confirms = false
				phase()
			}
		case got := <-polls:
			switch {
			case got.state == "lost" && ctx.Err() != nil:
				return stopped()
			case got.state == "approved" && raced(window):
				s.gateCancel(askID)
				return "cancelled", ""
			case got.state != "approved" && got.state != "cancelled" && got.state != "expired" && got.state != "linked":
				s.gateCancel(askID) // lost or unavailable: end it there too
			}
			return got.state, got.grant
		case <-ctx.Done():
			return stopped()
		}
	}
}

// gatePageURL: the NOVA page from the paired profile only, never Home
// Assistant's own internal or external URL: through Link, the Link
// connection's address first; else the profile's Home Assistant URL first.
// "" when the profile holds neither. nova_ask names the ask by its ref (the
// id's hash, as on the approval page), so the NOVA page opens it there
// top-level; the ref can't wait for the grant, and approving still takes
// the person's Home Assistant session and the page's button.
func (s *relaySession) gatePageURL(askID string) string {
	local, link := "", ""
	if u, err := url.Parse(s.cfg.HAURL); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		local = s.cfg.HAURL
	}
	if s.cfg.Cloud != nil && s.cfg.Cloud.Current != nil {
		if origin, err := ParseCanonicalNabuOrigin(s.cfg.Cloud.Current.CanonicalOrigin); err == nil {
			link = origin.String()
		}
	}
	base := cmp.Or(local, link)
	if s.transport.Via == relayViaCloud {
		base = cmp.Or(link, local)
	}
	target, err := haNOVAAppPanelURL(base)
	if base == "" || err != nil {
		return ""
	}
	if !target.Direct { // no NOVA panel to name (an unstamped build): Home Assistant's start, as before
		return target.URL
	}
	return target.URL + "?nova_ask=" + fmt.Sprintf("%x", sha256.Sum256([]byte(askID)))
}

// gateWindow shows the window where one can open (as for window plans).
// With confirm, the plan's own text and Confirm (confirms: its first
// button approves); a plan too long for it, its start and where the rest
// is, above the waiting text; cannot (Confirm found the App wouldn't take
// it), the waiting text saying so. The waiting window: the worker-free
// text, a first button that opens the NOVA page (without a page: one that
// only hides the window, and the text says where the page is), and Cancel.
// Windows' stock OK/Cancel get a line saying what OK does. raced withdraws
// a window still showing and reports whether its last answer was a
// Cancel. No window: a nil channel, and raced says false.
func (s *relaySession) gateWindow(ctx context.Context, deadline time.Time, page string, confirm *gateConfirm, cannot bool) (<-chan windowAnswer, func(<-chan windowAnswer) bool, bool) {
	none := func(<-chan windowAnswer) bool { return false }
	if s.gateChat() || windowOff(s.paths) || !windowAvailable() {
		return nil, none, false
	}
	lang := confirmLanguage(s.lang)
	text, first, ok := confirmT(lang, "gate.window", "approve", confirmT(lang, "page.approve")), confirmT(lang, "gate.open"), confirmT(lang, "gate.windows_ok")
	if page == "" {
		text, first, ok = confirmT(lang, "gate.window_page"), confirmT(lang, "gate.hide"), confirmT(lang, "gate.windows_hide")
	}
	if confirm == nil { // why this window can't confirm
		if why := gateWindowWhy(lang, s.gateMe().why, s.server); why != "" {
			text = why + " " + text
		}
	}
	if runtime.GOOS == "windows" {
		text += "\n\n" + ok
	}
	show := func(ctx context.Context) windowAnswer {
		return gateWindow(ctx, text, first, confirmT(lang, "window.cancel"), time.Until(deadline))
	}
	confirms := false
	if confirm != nil {
		asked, fits := gateConfirmWindow(confirm)
		switch {
		case cannot:
			text = confirmT(lang, "gate.window_cannot") + " " + text
		case fits:
			show, confirms = func(ctx context.Context) windowAnswer { return windowRunner(ctx, asked, time.Until(deadline)) }, true
		default:
			text = gateSummary(confirm.lang, confirm.text, text)
		}
	}
	answers := make(chan windowAnswer, 1)
	windowCtx, closeWindow := context.WithCancel(ctx)
	go func() {
		defer closeWindow()
		answers <- show(windowCtx)
	}()
	return answers, func(window <-chan windowAnswer) bool {
		if window == nil {
			return false
		}
		closeWindow()
		select {
		case answer := <-window:
			return answer == windowCancelled || answer == windowClosed
		case <-time.After(3 * time.Second): // a window that won't close: no Cancel was read
			return false
		}
	}, confirms
}
