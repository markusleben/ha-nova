package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	gatePollTimeout = 40 * time.Second // the Relay answers a wait within 25 s
	gateAskLife     = 3 * time.Minute  // the Relay ends an ask after this
)

// gateGrantKey carries a grant through a Link request's context (cloud_ingress.go).
type gateGrantKey struct{}

// relayGate: what one session knows of the Relay's safety check. grants:
// a request's digest → the grants that cover one send of it each.
type relayGate struct {
	checked, on bool
	grants      map[[32]byte][]string
}

type gateRequest struct {
	endpoint string
	body     []byte
}

func gateKey(r gateRequest) [32]byte {
	return sha256.Sum256(append(append([]byte(r.endpoint), 0), r.body...))
}

// gateApproval is set by ha-nova mcp (stdio) for its apply job
// (mcp_stdio_apply.go): an ask then waits its whole life, and the flag is
// true while it waits for the person. nil (the CLI): an ask ends after
// windowWait.
var gateApproval *atomic.Bool

// The waiting window (nativeWindow's code with other buttons) and what its
// first button opens; tests replace both.
var (
	gateWindow = func(ctx context.Context, message, lang string, wait time.Duration) windowAnswer {
		return nativeButtons(ctx, message, confirmT(lang, "gate.open"), confirmT(lang, "window.cancel"), wait)
	}
	gateOpenHA = launchBrowser
)

// gateExempt: the App's chat bridge and gate worker keep their own asks (spec §1, §5).
func (s *relaySession) gateExempt() bool { return chatCall != nil || s.cfg.bridgeCredential != "" }

// sendWrite sends one checked write body exactly once (sendWriteOnce), with
// the grant an ask got for these bytes. A typed request the Relay holds
// (428) that carried no grant is asked for now, with the reload a config
// write needs after it, and sent once more with that grant; a 428 that
// carried a grant is never asked again.
func (s *relaySession) sendWrite(endpoint string, body []byte) (json.RawMessage, error) {
	request := gateRequest{endpoint, body}
	grant := s.takeGrant(request)
	data, err := s.sendWriteOnce(endpoint, body, grant)
	var held *relayCallError
	if grant != "" || s.gateExempt() || !errors.As(err, &held) || held.code != "GATE_APPROVAL_REQUIRED" {
		return data, err
	}
	if err := s.gateAsk(append([]gateRequest{request}, gateFollowUps(request)...)); err != nil {
		return nil, errors.New(strings.TrimSuffix(err.Error(), "; nothing was sent")) // the caller says so
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
	items := make([]map[string]string, len(reqs))
	for i, r := range reqs {
		items[i] = map[string]string{"endpoint": r.endpoint, "body_b64": base64.StdEncoding.EncodeToString(r.body)}
	}
	var opened struct {
		Needed bool   `json:"needed"`
		AskID  string `json:"ask_id"`
	}
	err := s.gateCall(planAsks, map[string]any{"requests": items}, &opened)
	var call *relayCallError
	switch {
	case errors.As(err, &call) && strings.HasPrefix(call.code, "GATE_"):
		return gateStop("refused", call.code, call.message, gateNext[call.code])
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
	state, grant := s.gateWait(opened.AskID)
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

// gateWait long-polls the ask until it ends, our wait runs out (the CLI:
// windowWait; ha-nova mcp's job: the ask's life), or HA NOVA is stopped
// (planAsks). An end without an approval cancels the ask. The waiting window
// only opens Home Assistant or cancels; its answer is read before an
// approval counts, so a Cancel that raced the approval wins.
func (s *relaySession) gateWait(askID string) (state, grant string) {
	deadline := time.Now().Add(windowWait)
	if flag := gateApproval; flag != nil {
		deadline = time.Now().Add(gateAskLife + 5*time.Second)
		flag.Store(true)
		defer flag.Store(false)
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
			if err := s.gateCall(ctx, map[string]any{"ask_id": askID, "op": "wait"}, &answer); err != nil {
				polls <- end{state: "lost"}
				return
			}
			if answer.State != "waiting" {
				polls <- end{answer.State, answer.GrantID}
				return
			}
		}
	}()
	window, raced := s.gateWindow(ctx, deadline)
	fmt.Fprintln(os.Stderr, "HA NOVA asks on your linked phone or in Home Assistant (NOVA) and waits; nothing is sent before an OK there.")
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
			case windowConfirmed: // its only button besides Cancel: open Home Assistant, never approve
				if target, err := haNOVAAppPanelURL(s.cfg.HAURL); err == nil && s.cfg.HAURL != "" {
					_ = gateOpenHA(target.URL)
				}
			}
		case got := <-polls:
			switch {
			case got.state == "lost" && ctx.Err() != nil:
				return stopped()
			case got.state == "approved" && raced(window):
				s.gateCancel(askID)
				return "cancelled", ""
			case got.state != "approved" && got.state != "cancelled" && got.state != "expired":
				s.gateCancel(askID) // lost or unavailable: end it there too
			}
			return got.state, got.grant
		case <-ctx.Done():
			return stopped()
		}
	}
}

// gateWindow shows the waiting window where one can open (as for window
// plans): the worker-free text, a first button that opens Home Assistant,
// and Cancel. Windows' stock OK/Cancel get a line saying what OK does.
// raced withdraws a window still showing and reports whether its last
// answer was a Cancel. No window: a nil channel, and raced says false.
func (s *relaySession) gateWindow(ctx context.Context, deadline time.Time) (<-chan windowAnswer, func(<-chan windowAnswer) bool) {
	none := func(<-chan windowAnswer) bool { return false }
	if chatCall != nil || windowOff(s.paths) || !windowAvailable() {
		return nil, none
	}
	lang := confirmLanguage(s.lang)
	text := confirmT(lang, "gate.window")
	if runtime.GOOS == "windows" {
		text += "\n\n" + confirmT(lang, "gate.windows_ok")
	}
	answers := make(chan windowAnswer, 1)
	windowCtx, closeWindow := context.WithCancel(ctx)
	go func() {
		defer closeWindow()
		answers <- gateWindow(windowCtx, text, lang, time.Until(deadline))
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

// gateCall sends one request to the ask route, never twice (a second open
// would replace the first), and decodes its data into into.
func (s *relaySession) gateCall(parent context.Context, body map[string]any, into any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, gatePollTimeout)
	defer cancel()
	req, err := newRelayRequest(ctx, s.transport, gateAskRoute, raw)
	if err != nil {
		return err
	}
	resp, err := s.transport.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := readAllLimited(resp.Body, maxRelayResponseBytes)
	var envelope struct {
		OK    *bool           `json:"ok"`
		Data  json.RawMessage `json:"data"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	switch {
	case err != nil:
		return err
	case json.Unmarshal(data, &envelope) != nil || envelope.OK == nil:
		return &relayCallError{endpoint: "gate", status: resp.StatusCode, message: "not a result envelope"}
	case !*envelope.OK:
		return &relayCallError{endpoint: "gate", status: resp.StatusCode, code: envelope.Error.Code, message: envelope.Error.Message}
	}
	return json.Unmarshal(envelope.Data, into)
}

// gateCancel ends the ask on the Relay, best effort (planAsks may be done).
// Its answer never sends anything, an approval that came first included.
func (s *relaySession) gateCancel(askID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ended struct{}
	_ = s.gateCall(ctx, map[string]any{"ask_id": askID, "op": "cancel"}, &ended)
}

// gateNext: what the agent does after the Relay refused an ask ("": no answer).
var gateNext = map[string]string{
	"":                 "Tell the user nothing changed; they can try again in a moment.",
	"GATE_REFUSED":     "Tell the user HA NOVA's safety check in Home Assistant refuses this change, so nothing changed. Never try it another way.",
	"GATE_NO_CHANNEL":  "Tell the user nothing changed: no one can confirm it now. Their phone must be linked to HA NOVA, or the NOVA page in Home Assistant open to an administrator; then they ask again.",
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
