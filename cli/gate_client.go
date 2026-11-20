package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
	window             bool // GET /health window: 1: the App has the 0.32 window contract
	code               bool // GET /health code: 1: the App draws a chat code where its owner chose one (MAR-244)
	grants             map[[32]byte][]string
	me                 *gateWindowState // gateMe, once read
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
	gateAsking           int32 = 1 // the change's own ask
	gateLinking          int32 = 2 // a phone-link ask before it (gateMoveFirst)
	gateAskingWindow     int32 = 3 // the change's own ask, with Confirm in this computer's window
	gateAskingPhone      int32 = 4 // the change's own ask, which only the linked phone confirms
	gateAskingPage       int32 = 5 // the change's own ask, which only the NOVA page confirms
	gateAskingWindowOnly int32 = 6 // the change's own ask, which only this computer's window confirms
)

// The waiting window (nativeWindow's code with other buttons) and what its
// first button opens; tests replace both.
var (
	gateWindow = nativeButtons
	gateOpenHA = launchBrowser
)

// gateExempt: the App's chat bridge and gate worker keep their own asks
// (spec §1, §5). A chat bridge on the plain Relay token (the standalone
// container) is gated like a computer: it asks the Relay's linked phone.
func (s *relaySession) gateExempt() bool { return s.cfg.bridgeCredential != "" }

// gateChat: a chat app's call. It asks the Relay only from its apply job,
// outside mcpVerbLock (verbCall.outside), and never moves a phone.
func (s *relaySession) gateChat() bool { return s.call.chatPolicy() != nil }

// approval: what of a job's Relay ask waits for the person: ha-nova serve's
// apply job's own (verbCall.asking) on a Relay that gates it, else ha-nova
// mcp's gateApproval; nil: the CLI, whose ask ends with the command's wait,
// and the App's bridge, which keeps its waits.
func (s *relaySession) approval() *atomic.Int32 {
	if s.call != nil && s.call.asking != nil && !s.gateExempt() {
		return s.call.asking
	}
	return gateApproval
}

// sendWrite sends one checked write body exactly once (sendWriteOnce), with
// the grant an ask got for these bytes. A typed request the Relay holds
// (428: no grant covered it, or its grant expired, two minutes after the
// approval) is asked for once more and sent once more with that grant; a
// 428 after that is the answer. An ask that ends without an approval
// answers its own *verbFailure: nothing of this request was sent.
func (s *relaySession) sendWrite(endpoint string, body []byte) (json.RawMessage, error) {
	request := gateRequest{endpoint, body}
	data, err := s.sendWriteOnce(endpoint, body, s.takeGrant(request))
	var held *relayCallError
	if !errors.As(err, &held) || held.code != "GATE_APPROVAL_REQUIRED" || s.gateExempt() {
		return data, err
	}
	s.mu.Lock()
	if s.gate != nil { // an older grant for it expired with this one
		delete(s.gate.grants, gateKey(request))
	}
	s.mu.Unlock()
	if err := s.gateAsk([]gateRequest{request}, true, nil); err != nil { // no plan text for a held write: never Confirm in the window
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
// computer rated it typed (they disagree, so nothing is sent). held: a
// write the Relay held mid-apply (sendWrite), whose basis was checked just
// before and isn't checked again after the ask, so a chat app's wait keeps
// mcpVerbLock: no other change of this bridge lands under it.
func (s *relaySession) gateAsk(reqs []gateRequest, held bool, plan *gateConfirm) error {
	if len(reqs) > gateAskMax {
		return gateStop("refused", "GATE_ASK_TOO_LARGE", fmt.Sprintf("this change sends %d requests, more than one confirmation on the phone or in Home Assistant covers (%d); nothing was sent", len(reqs), gateAskMax),
			"Tell the user nothing changed. Plan it again in smaller parts.")
	}
	chat, lang := s.gateChat(), confirmLanguage(s.lang)
	if chat && (s.call == nil || s.call.outside == nil) { // not a chat app's apply job (ha_call): it can't wait for the phone
		return gateStop("refused", "CONFIRM_UNAVAILABLE", confirmT(lang, "gate.chat_held"), gateChatHeldNext)
	}
	items := make([]map[string]string, len(reqs))
	for i, r := range reqs {
		items[i] = map[string]string{"endpoint": r.endpoint, "body_b64": base64.StdEncoding.EncodeToString(r.body)}
	}
	keeps := s.approval() == nil && !chat && plan != nil && plan.id != ""
	acceptCode := keeps && !held && s.gateCodeHere() // the shell's first ask only (gate_code.go)
	open := func() (opened gateOpened, err error) {
		body := map[string]any{"requests": items}
		if acceptCode {
			body["accept_code"] = true
		}
		if presence := s.gatePresence(plan); presence != "" {
			body["presence"] = presence
		}
		err = s.gateCall(planAsks, gateAskRoute, s.gateOpenWait(len(reqs)), body, &opened)
		return opened, err
	}
	flag := s.approval()
	if flag == nil && s.askWait() <= 0 { // the check before took the time this call has to ask
		return gateStop("refused", "CONFIRM_UNAVAILABLE", "checking this change took the time this call has to ask on the linked phone or in Home Assistant; nothing was sent",
			"Tell the user nothing changed: checking this change in their home took too long to still ask them in time. They can ask again; if it keeps happening, they make the change in Home Assistant itself.")
	}
	// The CLI's ask for a stored plan may outlive the command's wait: it is
	// kept for the plan's next apply, which joins it (gate_ask_join.go).
	kept, joined := gateKeptAsk{}, false
	if keeps {
		kept, joined = gateTakeAsk(s.paths, plan.id, reqs)
	}
	var stop error
	var state, grant string
	var opened gateOpened
	var ask func() bool
	ask = func() bool { // a chat app's runs outside mcpVerbLock: it reads no process-wide state
		end := time.Now().Add(gateAskLife + 5*time.Second) // a job's ask waits its life, a move's link included
		if flag == nil {
			end = time.Now().Add(s.askWait()) // the CLI: the command's wait
		}
		var err error
		if s.gateCode != "" && !(joined && kept.Code) { // a typed code answers only the kept code ask it was shown for, never a new one
			s.gateCode = ""
			stop = gateStop("cancelled", "CONFIRM_CANCELLED", "the ask this code was shown for has ended or no longer matches the plan; nothing was sent",
				"Tell the user nothing changed. Only after they say they still want it, run the same ha-nova apply --plan command again without --confirm: HA NOVA then asks where they can confirm now. Never type a code yourself.")
			return false
		}
		if joined {
			opened = gateOpened{Needed: true, AskID: kept.AskID, Channels: kept.Channels}
			if s.gateCode != "" { // apply --confirm: the person typed the chat code back
				state, grant, stop = s.gateSendCode(plan.id, kept)
				return stop == nil && state == "approved" && grant != ""
			}
		} else {
			opened, err = open()
			if !chat && opened.Code == "" && !plan.confirmsHere(s, opened) { // a chat app never moves a phone: the bridge host's own link isn't its user's; a window here that confirms needs none
				if reopen, moved := s.gateMoveFirst(opened, err, time.Until(end)/2); moved != nil {
					stop = moved
					return false
				} else if reopen {
					opened, err = open()
				}
			}
		}
		if stop = gateOpenFailure(opened, err, chat, lang, func() error { return gateDisagrees(s.paths, s.version(), chat, plan != nil && plan.registry) }); stop != nil {
			return false
		}
		if opened.Code != "" && acceptCode { // nothing waits here: the person types it back in the chat
			stop = s.gateCodeShown(plan, opened, reqs)
			return false
		}
		state, grant = s.gateWait(opened.AskID, false, time.Until(end), plan.offered(s, opened), opened.Channels)
		if joined && (state == "expired" || state == "unknown") { // the kept ask ended unanswered, or the App forgot it: ask again, as before
			joined = false
			return ask()
		}
		return state == "approved" && grant != ""
	}
	if !chat {
		ask()
	} else if err := s.call.outside(ask, !held); err != nil { // the user's access, read again after the OK, refused it
		return err
	}
	switch {
	case stop != nil:
		return stop
	case chat && (state != "approved" || grant == ""):
		return gateChatEnded(state)
	case keeps && state == "timeout" && gateKeepAsk(s.paths, plan.id, gateKeptAsk{AskID: opened.AskID, Digest: gateAskDigest(reqs), Channels: opened.Channels, Code: joined && kept.Code}) == nil:
		return gateWaiting(plan.id, plan.lang, plan.confirmsHere(s, opened), opened.Channels) // the command's wait ended first: the ask stays for the next apply
	case state == "timeout" && flag == nil: // a held write's ask, or one that couldn't be kept: nothing joins it, so end it
		s.gateCancel(opened.AskID)
		return gateEnded(state)
	case state != "approved" || grant == "":
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

// gateOpenFailure: why an open asks nobody; nil: it asks. Every one sent
// nothing. A chat app (chat) on a container with no phone linked gets the
// clear refusal, and other refusals in the container's words; disagrees,
// when the App needs no ask for a change this computer holds.
func gateOpenFailure(opened gateOpened, err error, chat bool, lang string, disagrees func() error) error {
	var call *relayCallError
	switch {
	case chat && errors.As(err, &call) && call.code == "GATE_NO_CHANNEL":
		return gateStop("refused", "CONFIRM_UNAVAILABLE", confirmT(lang, "gate.chat_no_phone"), gateChatNoPhoneNext)
	case chat && errors.As(err, &call) && gateChatNext[call.code] != "":
		return gateStop("refused", call.code, gateRefusalMessage(call.message), gateChatNext[call.code])
	case errors.As(err, &call) && strings.HasPrefix(call.code, "GATE_"):
		return gateStop("refused", call.code, gateRefusalMessage(call.message), gateNext[call.code])
	case errors.As(err, &call) && call.status == http.StatusNotFound:
		return gateRolledBack()
	case err != nil:
		return gateStop("refused", "CONFIRM_UNAVAILABLE", "HA NOVA couldn't ask on the linked phone or in Home Assistant ("+err.Error()+"); nothing was sent", gateNext[""])
	case !opened.Needed:
		return disagrees()
	case opened.AskID == "":
		return gateStop("refused", "CONFIRM_UNAVAILABLE", "the NOVA App's answer named no ask; nothing was sent", gateNext[""])
	}
	return nil
}

// gateDisagrees: the App needs no ask for a change this computer holds for
// a person, so nothing is sent; the App keeps the last word. Only an App
// older than min_relay_version is told to update (an unknown version is
// not); registry: this computer held it because it couldn't read a Home
// Assistant registry, which it then says. A chat app's user has no
// terminal here: the change is theirs to make in Home Assistant.
func gateDisagrees(paths runtimePaths, relayVersion string, chat, registry bool) error {
	message, next := "HA NOVA's safety check in Home Assistant doesn't hold this change for a person, but this computer does", "Tell the user nothing changed. Update HA NOVA on this computer (ha-nova update), then plan it again; if it happens again, they make the change in Home Assistant itself."
	if registry {
		message += " because it couldn't read a Home Assistant registry"
		next = "Tell the user nothing changed: this computer couldn't read a Home Assistant registry, so it held the change for a person. Plan it again; if it happens again, they make the change in Home Assistant itself."
	}
	if chat {
		next = "Tell the user nothing changed: HA NOVA and its safety check in Home Assistant disagree about this change. They make it in Home Assistant itself, or ask whoever runs HA NOVA's container to update it."
	}
	if v, err := readVersionJSON(paths.VersionFile); err == nil && v.MinRelayVersion != "" && relayVersion != "" {
		if cmp, err := compareReleaseVersions(relayVersion, v.MinRelayVersion); err == nil && cmp < 0 {
			next = fmt.Sprintf("Tell the user nothing changed: the NOVA App in Home Assistant is older than this computer needs (it has %s, needs %s or newer). %s Then plan it again.", relayVersion, v.MinRelayVersion, relayAppUpdatePath)
		}
	}
	return gateStop("refused", "GATE_DISAGREES", message+"; nothing was sent", next)
}

// gateRefusalMessage: the App's refusal, which says nothing was sent on
// current Apps, and with that added where an older App's doesn't.
func gateRefusalMessage(message string) string {
	if strings.Contains(strings.ToLower(message), "nothing was sent") {
		return message
	}
	return message + "; nothing was sent"
}

// gateNext: what the agent does after the Relay refused an ask ("": no answer).
var gateNext = map[string]string{
	"":                 "Tell the user nothing changed; they can try again in a moment.",
	"GATE_REFUSED":     "Tell the user HA NOVA's safety check in Home Assistant refuses this change, so nothing changed. Never try it another way.",
	"GATE_NO_CHANNEL":  "Tell the user nothing changed: no one can confirm it now. They link their phone with `ha-nova setup confirm-phone` (in a terminal; it links it in Home Assistant too), or an administrator opens the NOVA page in Home Assistant where the App has one; `ha-nova doctor` says what is missing. Then they ask again.",
	"GATE_PAUSED":      "Tell the user nothing changed: confirmations on their phone are paused for now, after unanswered ones or many asks within an hour. They can confirm on the NOVA page in Home Assistant, or ask again later.",
	"GATE_ASK_LIMIT":   "Tell the user nothing changed: this computer asked too often in the last hour. They can ask again later.",
	"GATE_ASK_BUSY":    "Tell the user nothing changed: another ask from this computer is still being prepared. Try again in a moment.",
	"GATE_UNAVAILABLE": "Tell the user nothing changed: HA NOVA's safety check in Home Assistant isn't available. Try again in a minute; if it stays, they restart the NOVA App.",
}

// A chat app on the standalone container (gateChat): what the agent does
// after its Relay held a change outside an apply job (ha_call), and after
// an ask found no phone linked to the container; the Part variants after
// earlier steps of a raw plan went through (plan_raw_apply.go).
const (
	gateChatHeldNext        = "Tell the user nothing changed yet: this change needs an OK on the phone linked to their NOVA Relay container, which HA NOVA asks only when it applies a plan. Plan it with ha_plan, then apply that plan with ha_apply."
	gateChatHeldPartNext    = "Tell the user which steps went through; the next one needs an OK on the phone linked to their NOVA Relay container, so nothing more was sent. Plan the rest with ha_plan, then apply that plan with ha_apply."
	gateChatNoPhoneNext     = "Tell the user nothing changed: this change needs an OK on the phone linked to their NOVA Relay container, and no phone is linked there. Whoever runs the container links one from a paired computer; or they make the change in Home Assistant itself. Never ask them for a code."
	gateChatNoPhonePartNext = "Tell the user which steps went through; the next one needs an OK on the phone linked to their NOVA Relay container, and no phone is linked there, so nothing more was sent. Whoever runs the container links one from a paired computer; or they make the rest in Home Assistant itself."
	gateChatCancelledNext   = "Tell the user nothing changed: the ask was cancelled on the phone linked to their NOVA Relay container, or someone else using that container (another chat user or a computer) asked for a change meanwhile, which replaces it. Apply this plan again only if they ask for it."
)

// gateChatPart: each chat refusal's Part variant.
var gateChatPart = map[string]string{gateChatHeldNext: gateChatHeldPartNext, gateChatNoPhoneNext: gateChatNoPhonePartNext}

// gateChatNext: gateNext in the container's words for a chat app. Everyone
// holding the container's token asks as one: a new ask replaces the open
// one, and five asks in a row that go unanswered pause the phone.
var gateChatNext = map[string]string{
	"GATE_PAUSED":      "Tell the user nothing changed: asks on the phone linked to their NOVA Relay container are paused for up to 5 minutes, because five asks there in a row went unanswered (from anyone using that container, computers included). They ask again later, or make the change in Home Assistant itself.",
	"GATE_ASK_LIMIT":   "Tell the user nothing changed: everyone using their NOVA Relay container together asked too often in the last hour. They ask again later, or make the change in Home Assistant itself.",
	"GATE_ASK_BUSY":    "Tell the user nothing changed: another ask through their NOVA Relay container is still being prepared. They ask again in a moment.",
	"GATE_UNAVAILABLE": "Tell the user nothing changed: HA NOVA's safety check in their NOVA Relay container isn't available. They try again in a minute; if it stays, whoever runs the container restarts it.",
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

// gateChatEnded: gateEnded for a chat app on a standalone Relay container,
// whose linked phone is the only place to confirm.
func gateChatEnded(state string) *verbFailure {
	switch state {
	case "cancelled":
		return gateStop("declined", "CONFIRM_DECLINED", "the change was cancelled on the phone linked to the NOVA Relay container, or another ask through that container replaced it; nothing was sent", gateChatCancelledNext)
	case "expired", "timeout":
		return gateStop("cancelled", "CONFIRM_CANCELLED", "nobody confirmed it on the phone linked to the NOVA Relay container in time; nothing was sent",
			"Tell the user nothing changed. Run the same apply again (it asks that phone again) only after they say they still want it; never ask them for a code.")
	case "stopped":
		return gateEnded(state)
	}
	return gateStop("refused", "CONFIRM_UNAVAILABLE", "the phone linked to the NOVA Relay container couldn't be asked now; nothing was sent",
		"Tell the user nothing changed: the phone linked to their NOVA Relay container couldn't be asked just now. They can try again in a moment, or make this change in Home Assistant itself. Never ask them for a code.")
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
