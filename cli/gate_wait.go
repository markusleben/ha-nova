package main

import (
	"cmp"
	"context"
	"crypto/sha256"
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
// cancels the ask. The waiting window only opens Home Assistant or
// cancels; its answer is read before an approval counts, so a Cancel that
// raced the approval wins. A link ask (gateMovePhone) ends linked, only
// on the phones: no window.
func (s *relaySession) gateWait(askID string, link bool, wait time.Duration) (state, grant string) {
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
				polls <- end{state: "lost"}
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
	window, raced := (<-chan windowAnswer)(nil), func(<-chan windowAnswer) bool { return false }
	if link {
		waiting = confirmT(confirmLanguage(s.lang), "gate.link_waiting")
	} else {
		window, raced = s.gateWindow(ctx, deadline, page)
	}
	fmt.Fprintln(s.call.stderr(), waiting)
	stopped := func() (string, string) {
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
			switch answer {
			case windowCancelled, windowClosed:
				s.gateCancel(askID)
				return "cancelled", ""
			case windowConfirmed: // its only button besides Cancel: open Home Assistant (or hide the window), never approve
				if page != "" {
					_ = gateOpenHA(page)
				}
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

// gateWindow shows the waiting window where one can open (as for window
// plans): the worker-free text, a first button that opens the NOVA page
// (without a page: one that only hides the window, and the text says
// where the page is), and Cancel. Windows' stock OK/Cancel get a line
// saying what OK does. raced withdraws a window still showing and reports
// whether its last answer was a Cancel. No window: a nil channel, and
// raced says false.
func (s *relaySession) gateWindow(ctx context.Context, deadline time.Time, page string) (<-chan windowAnswer, func(<-chan windowAnswer) bool) {
	none := func(<-chan windowAnswer) bool { return false }
	if s.gateChat() || windowOff(s.paths) || !windowAvailable() {
		return nil, none
	}
	lang := confirmLanguage(s.lang)
	text, first, ok := confirmT(lang, "gate.window", "approve", confirmT(lang, "page.approve")), confirmT(lang, "gate.open"), confirmT(lang, "gate.windows_ok")
	if page == "" {
		text, first, ok = confirmT(lang, "gate.window_page"), confirmT(lang, "gate.hide"), confirmT(lang, "gate.windows_hide")
	}
	if runtime.GOOS == "windows" {
		text += "\n\n" + ok
	}
	answers := make(chan windowAnswer, 1)
	windowCtx, closeWindow := context.WithCancel(ctx)
	go func() {
		defer closeWindow()
		answers <- gateWindow(windowCtx, text, first, confirmT(lang, "window.cancel"), time.Until(deadline))
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
	}
}
