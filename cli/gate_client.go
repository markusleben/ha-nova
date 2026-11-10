package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// The CLI on a gated App (docs/work/2026-11-03-relay-typed-gate-spec.md →
// CLI client): the Relay forwards a typed request only with a grant for
// exactly its bytes, which a person gave on their linked phone or on the NOVA
// page in Home Assistant. The CLI asks the Relay for those bytes, waits, and
// sends each with the grant; nothing on this computer approves.

const (
	gateGrantHeader = "X-HA-Nova-Grant"
	gateAskRoute    = "gate/v1/ask"
)

var gateAskLife = 3 * time.Minute // the Relay ends an ask after this; tests shorten it

// gateGrantKey carries a grant through a Link request's context (cloud_ingress.go).
type gateGrantKey struct{}

// relayGate: what one session knows of the Relay's safety check. grants:
// a request's digest → the grants that cover one send of it each. moved:
// a phone-link move was tried in this session (gateMoveFirst).
type relayGate struct {
	checked, on, moved bool
	grants             map[[32]byte][]string
}

type gateRequest struct {
	endpoint string
	body     []byte
}

func gateKey(r gateRequest) [32]byte {
	return sha256.Sum256(append(append([]byte(r.endpoint), 0), r.body...))
}

// gateApproval is set by ha-nova mcp (stdio) for its apply job
// (mcp_stdio_apply.go): an ask then waits its whole life, and the flag says
// what waits for the person (gateAsking, gateLinking; 0: nothing). nil (the
// CLI): an ask ends with the command's own wait (askWait).
var gateApproval *atomic.Int32

const (
	gateAsking  int32 = 1 // the change's own ask
	gateLinking int32 = 2 // a phone-link ask before it (gateMoveFirst)
)

// The waiting window (nativeWindow's code with other buttons) and what its
// first button opens; tests replace both.
var (
	gateWindow = nativeButtons
	gateOpenHA = launchBrowser
)

// gateExempt: the App's chat bridge and gate worker keep their own asks (spec §1, §5).
func (s *relaySession) gateExempt() bool { return chatCall != nil || s.cfg.bridgeCredential != "" }

// sendWrite sends one checked write body exactly once (sendWriteOnce), with
// the grant an ask got for these bytes. A typed request the Relay holds
// (428: no grant covered it, or its grant expired, two minutes after the
// approval) is asked for once more, with the reload a config delete needs
// after it, and sent once more with that grant; a 428 after that is the
// answer. An ask that ends without an approval answers its own
// *verbFailure: nothing of this request was sent.
func (s *relaySession) sendWrite(endpoint string, body []byte) (json.RawMessage, error) {
	request := gateRequest{endpoint, body}
	data, err := s.sendWriteOnce(endpoint, body, s.takeGrant(request))
	var held *relayCallError
	if s.gateExempt() || !errors.As(err, &held) || held.code != "GATE_APPROVAL_REQUIRED" {
		return data, err
	}
	reqs := append([]gateRequest{request}, gateFollowUps(request)...)
	s.mu.Lock()
	for _, r := range reqs { // an older grant for them expired with this one
		if s.gate != nil {
			delete(s.gate.grants, gateKey(r))
		}
	}
	s.mu.Unlock()
	if err := s.gateAsk(reqs); err != nil {
		return nil, err
	}
	return s.sendWriteOnce(endpoint, body, s.takeGrant(request))
}

// takeGrant: a grant for one send of r, used up; "" when none covers it.
func (s *relaySession) takeGrant(r gateRequest) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := gateKey(r)
	if s.gate == nil || len(s.gate.grants[key]) == 0 {
		return ""
	}
	grant := s.gate.grants[key][0]
	s.gate.grants[key] = s.gate.grants[key][1:]
	return grant
}

// gateAsk asks the Relay for exactly these requests and waits for the
// person (gateWait). nil: approved, and each request carries the grant
// once. Every failure sent nothing: the Relay's refusal, an App without the
// ask route (rolled back), or an ask the Relay doesn't need although this
// computer rated it typed (they disagree, so nothing is sent).
func (s *relaySession) gateAsk(reqs []gateRequest) error {
	if len(reqs) > gateAskMax {
		return gateStop("refused", "GATE_ASK_TOO_LARGE", fmt.Sprintf("this change sends %d requests, more than one confirmation on the phone or in Home Assistant covers (%d); nothing was sent", len(reqs), gateAskMax),
			"Tell the user nothing changed. Plan it again in smaller parts (a batch of at most 10 automations or scripts).")
	}
	items := make([]map[string]string, len(reqs))
	for i, r := range reqs {
		items[i] = map[string]string{"endpoint": r.endpoint, "body_b64": base64.StdEncoding.EncodeToString(r.body)}
	}
	open := func() (opened gateOpened, err error) {
		err = s.gateCall(planAsks, gateAskRoute, gateOpenWait(len(reqs)), map[string]any{"requests": items}, &opened)
		return opened, err
	}
	opened, err := open()
	end := time.Now().Add(askWait()) // the CLI: the command's wait; ha-nova mcp's job: the ask's life, a move's link included
	if gateApproval != nil {
		end = time.Now().Add(gateAskLife + 5*time.Second)
	}
	if reopen, stop := s.gateMoveFirst(opened, err, time.Until(end)/2); stop != nil {
		return stop
	} else if reopen {
		opened, err = open()
	}
	var call *relayCallError
	switch {
	case errors.As(err, &call) && strings.HasPrefix(call.code, "GATE_"):
		return gateStop("refused", call.code, call.message+"; nothing was sent", gateNext[call.code])
	case errors.As(err, &call) && call.status == http.StatusNotFound:
		return gateRolledBack()
	case err != nil:
		return gateStop("refused", "CONFIRM_UNAVAILABLE", "HA NOVA couldn't ask on the linked phone or in Home Assistant ("+err.Error()+"); nothing was sent", gateNext[""])
	case !opened.Needed:
		return gateStop("refused", "GATE_DISAGREES", "HA NOVA's safety check in Home Assistant doesn't hold this change for a person, but this computer does; nothing was sent",
			"Tell the user nothing changed. Update HA NOVA on this computer (ha-nova update) and the NOVA App in Home Assistant, then plan it again.")
	case opened.AskID == "":
		return gateStop("refused", "CONFIRM_UNAVAILABLE", "the NOVA App's answer named no ask; nothing was sent", gateNext[""])
	}
	state, grant := s.gateWait(opened.AskID, false, time.Until(end))
	if state != "approved" || grant == "" {
		return gateEnded(state)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate == nil {
		s.gate = &relayGate{}
	}
	if s.gate.grants == nil {
		s.gate.grants = map[[32]byte][]string{}
	}
	for _, r := range reqs {
		s.gate.grants[gateKey(r)] = append(s.gate.grants[gateKey(r)], grant)
	}
	return nil
}

// gateWait long-polls the ask until it ends, wait runs out (gateAsk's
// share of the command's wait, or of ha-nova mcp's ask life; a setup link's
// own), or HA NOVA is stopped (planAsks). An end without an approval
// cancels the ask. The waiting window only opens Home Assistant or
// cancels; its answer is read before an approval counts, so a Cancel that
// raced the approval wins. A link ask (gateMovePhone) ends linked, only
// on the phones: no window.
func (s *relaySession) gateWait(askID string, link bool, wait time.Duration) (state, grant string) {
	deadline := time.Now().Add(wait)
	if flag := gateApproval; flag != nil {
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
	page := s.gatePageURL()
	waiting := confirmT(confirmLanguage(s.lang), "gate.waiting")
	window, raced := (<-chan windowAnswer)(nil), func(<-chan windowAnswer) bool { return false }
	if link {
		waiting = confirmT(confirmLanguage(s.lang), "gate.link_waiting")
	} else {
		window, raced = s.gateWindow(ctx, deadline, page)
	}
	fmt.Fprintln(os.Stderr, waiting)
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
// "" when the profile holds neither.
func (s *relaySession) gatePageURL() string {
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
	return target.URL
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
	if chatCall != nil || windowOff(s.paths) || !windowAvailable() {
		return nil, none
	}
	lang := confirmLanguage(s.lang)
	text, first, ok := confirmT(lang, "gate.window"), confirmT(lang, "gate.open"), confirmT(lang, "gate.windows_ok")
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

// gateNext: what the agent does after the Relay refused an ask ("": no answer).
var gateNext = map[string]string{
	"":                 "Tell the user nothing changed; they can try again in a moment.",
	"GATE_REFUSED":     "Tell the user HA NOVA's safety check in Home Assistant refuses this change, so nothing changed. Never try it another way.",
	"GATE_NO_CHANNEL":  "Tell the user nothing changed: no one can confirm it now. They link their phone with `ha-nova setup confirm-phone` (in a terminal; it links it in Home Assistant too), or an administrator opens the NOVA page in Home Assistant where the App has one; `ha-nova doctor` says what is missing. Then they ask again.",
	"GATE_PAUSED":      "Tell the user nothing changed: confirmations on their phone are paused for a few minutes after unanswered ones. They can confirm on the NOVA page in Home Assistant, or ask again later.",
	"GATE_ASK_LIMIT":   "Tell the user nothing changed: this computer asked too often in the last hour. They can ask again later.",
	"GATE_ASK_BUSY":    "Tell the user nothing changed: another ask from this computer is still being prepared. Try again in a moment.",
	"GATE_UNAVAILABLE": "Tell the user nothing changed: HA NOVA's safety check in Home Assistant isn't available. Try again in a minute; if it stays, they restart the NOVA App.",
}

// gateEnded: an ask that ended without an approval sent nothing.
func gateEnded(state string) *verbFailure {
	switch state {
	case "cancelled":
		return gateStop("declined", "CONFIRM_DECLINED", "the change was cancelled on the phone, in Home Assistant, or in the window; nothing was sent",
			"Tell the user nothing changed. Apply this plan again only if they ask for it.")
	case "expired", "timeout":
		return gateStop("cancelled", "CONFIRM_CANCELLED", "nobody confirmed it on the phone or in Home Assistant in time; nothing was sent",
			"Tell the user nothing changed. Run the same apply again (it asks them again) only after they say they still want it; never ask them for a code.")
	case "stopped":
		return gateStop("cancelled", "CONFIRM_CANCELLED", "HA NOVA was stopped while it waited for the OK; nothing was sent", "Tell the user nothing changed.")
	}
	return gateStop("refused", "CONFIRM_UNAVAILABLE", "no one could confirm it now (the linked phone can't be asked and no other place can, or the NOVA App stopped answering); nothing was sent",
		"Tell the user nothing changed. They can confirm on the NOVA page in Home Assistant, or make this change in Home Assistant itself.")
}

// gateRolledBack: a profile that saw the gate, on an App without it.
func gateRolledBack() *verbFailure {
	return gateStop("refused", "GATE_ROLLED_BACK", "HA NOVA's App was rolled back to a version without its safety check; update it in Home Assistant. Nothing was sent",
		"Tell the user in plain words: nothing changed; they update the NOVA App in Home Assistant (Settings → Apps), then ask again.")
}

// gateStop: a refusal before anything was sent; applyPlan's release hands the claimed plan back.
func gateStop(status, code, message, next string) *verbFailure {
	return &verbFailure{exit: verbExitRefused, status: status, code: code, message: message, next: next, cause: errNothingSent}
}
