package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Confirmations for chat apps (ha-nova serve; docs/work/2026-10-19-chat-
// bridge-contract.md): no
// window and no form. A typed plan asks the linked phone of the chat app's
// own Home Assistant user; with none linked, a chat code, unless the owner
// turned chat codes off. ha_apply
// waits a little for the tap, then answers waiting_for_phone while the ask
// stays open; calling it again waits on the same ask and never re-notifies.

// chatPolicy is one chat-app request: who asks, and the owner's switches.
// The MCP server sets chatCall to it for the verb it runs (runLocked, under
// mcpVerbLock); nil everywhere else.
type chatPolicy struct {
	user     string // the HA user id the integration vouched for (secret-authenticated)
	key      string // its hash: the user's data directory and log label
	links    string // the bridge's phone links: {HA user id: phoneLink}, its only store
	codes    bool   // chat codes allowed (owner switch)
	readOnly bool   // chat apps can only read (owner switch)
	bridge   *chatBridge
	// level: what this user may do now (mcp_serve_levels.go), the lower of
	// granted (the owner's choice; Manage everything when none) and their
	// role's cap. Zero: not allowed (a control job's look at another user).
	level, granted, role chatLevel
	name, ownerName      string // their name and the owner's, as Home Assistant lists them
	owner                bool   // this user is the Home Assistant owner
	stale                bool   // users read over a minute ago, while a change ran (homeUsers)
}

var chatCall *chatPolicy

// chatAskSpan: how long a chat app's phone ask stays open across its
// waiting_for_phone calls (bounded by the plan's expiry).
// ponytail: fixed; make it a flag if users need longer.
const chatAskSpan = 3 * time.Minute

const (
	chatRelayNext    = "Tell the user nothing changed: HA NOVA in Home Assistant can't reach its Relay right now. They try again in a moment; if it keeps failing, the Home Assistant owner checks the NOVA App (Settings → Apps). Never suggest a terminal command."
	chatPhoneMissing = "Tell the user nothing changed: HA NOVA can't use the phone linked to their Home Assistant user right now (the phone's Home Assistant app must be signed in with their own account, and the owner's HA NOVA page must still let them confirm on a phone). They check that phone's Home Assistant app, or make this change in Home Assistant itself. Never ask them for a code."
)

// chatCodesOffWhy and chatCodesOffNext: a typed plan, no phone linked, and
// no chat code for p's user: the owner turned chat codes off, or the
// user's level is below Manage everything (the model sees a chat code).
func chatCodesOffWhy(p *chatPolicy) string {
	if p != nil && p.level < levelManage {
		return "this change needs a tap on the user's own linked phone; none is linked, and HA NOVA never uses a chat code at this user's level"
	}
	return "this change needs a tap on the user's linked phone; no phone is linked for their Home Assistant user, and chat codes are turned off"
}

func chatCodesOffNext(p *chatPolicy) string {
	link := "They link their phone on the HA NOVA page in Home Assistant (Chat apps), then ask again"
	if p != nil && !p.owner {
		owner := "the Home Assistant owner"
		if p.ownerName != "" {
			owner = "“" + p.ownerName + "”"
		}
		link = "They ask " + owner + " to link their phone on the HA NOVA page, then ask again"
	}
	if p != nil && p.level < levelManage {
		return "Tell the user nothing changed: this needs a tap on their phone, and none is linked yet. " + link + ". Never ask them for a code."
	}
	return "Tell the user nothing changed: this change needs a tap on their phone. " + link + "; or they make this change in Home Assistant itself. Never ask them for a code."
}

// phoneMissingStep: what to do when the linked phone can't be asked.
func phoneMissingStep() string {
	if chatCall != nil {
		return chatPhoneMissing
	}
	return phoneMissingNext
}

// codeChannel: where a typed plan's code goes when no phone asks.
func codeChannel() string {
	switch {
	case chatCall != nil && !chatCall.chatCodes(): // no chat code below Manage everything
		return channelChatOff
	case chatCall != nil:
		return channelChat
	case formsShown:
		return channelForm
	}
	return channelChat
}

// discoverConfirmer: the linked phone, which in a chat app must confirm as
// the chat app's own user (chatConfirmer).
func discoverConfirmer(s *relaySession) (phoneSetup, phoneStatus) {
	return chatConfirmer(phoneDiscover(s, time.Now().Add(relaySessionOverallTimeout)))
}

// chatConfirmer: in a chat app, a linked phone that confirms as another
// user is refused, never moved to a code.
func chatConfirmer(setup phoneSetup, status phoneStatus) (phoneSetup, phoneStatus) {
	if chatCall != nil && status == phoneReady && setup.confirmer != chatCall.user {
		return phoneSetup{why: whyRemoved}, phoneMissing // not "unread": after a tap, "no longer the linked phone"
	}
	return setup, status
}

// linksForCall: the CLI's links (config.json, keyed by the HA owner), or in
// a chat app only this user's link from the bridge's own store (one Home
// Assistant, keyed by HA user), under chatOwner; never config.json.
// Missing file: none; unreadable: unknown (ok=false), never "none".
func linksForCall(paths runtimePaths) (map[string]phoneEntry, bool) {
	if chatCall == nil {
		return phoneLinks(paths)
	}
	byUser, ok := readBridgeLinks(chatCall.links)
	if !ok {
		return nil, false
	}
	return bridgeEntries(byUser, chatCall.user), true
}

// readBridgeLinks: the bridge's store, {HA user id: phoneLink or null}.
// Missing file: none; unreadable: unknown (ok=false), never "none".
func readBridgeLinks(file string) (map[string]*phoneLink, bool) {
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]*phoneLink{}, true
	}
	var byUser map[string]*phoneLink
	if err != nil || json.Unmarshal(raw, &byUser) != nil {
		return nil, false
	}
	if byUser == nil { // a file holding null
		byUser = map[string]*phoneLink{}
	}
	return byUser, true
}

// bridgeEntries: user's link as the phone code reads it, under chatOwner.
func bridgeEntries(byUser map[string]*phoneLink, user string) map[string]phoneEntry {
	links := map[string]phoneEntry{}
	if link, present := byUser[user]; present {
		if link == nil { // null: an unusable link (refused), never "no phone"
			link = &phoneLink{}
		}
		links[chatOwner] = phoneEntry{link: link}
	}
	return links
}

// chatOwner keys the bridge's one link in place of the HA owner's id.
const chatOwner = "chat-app-user"

// linkFor: the link entry for this home's owner (in a chat app, the user's).
func linkFor(links map[string]phoneEntry, owner string) phoneEntry {
	if chatCall != nil {
		return links[chatOwner]
	}
	return links[owner]
}

// chatRefusal: the owner's read-only switch refuses every change, plans
// included (a plan that can never apply is no preview).
func (srv *mcpServer) chatRefusal(tool mcpTool) map[string]any {
	if srv.chat == nil || !srv.chat.readOnly || tool.readOnly && tool.name != "ha_plan" {
		return nil
	}
	return chatStopped(tool.name, chatReadOnlyFailure())
}

func chatReadOnlyFailure() *verbFailure {
	return refusedWith("READ_ONLY", "chat apps can only read in this home (the owner's setting)",
		"Tell the user nothing changed: the Home Assistant owner set chat apps to read only on the HA NOVA page. They can make this change in Home Assistant itself.")
}

// chatReadOnly: this tool call comes from a chat app the owner set to read
// only.
func chatReadOnly() bool { return chatCall != nil && chatCall.readOnly }

// mcpPlanRoute: where a change ha_read refuses goes; never to a plan in a
// chat app set to read only.
func mcpPlanRoute() string {
	if chatReadOnly() {
		return "chat apps are set to read only in this home (the owner's setting on the HA NOVA page), so tell the user to make this change in Home Assistant itself"
	}
	return mcpPlanIt
}

func chatStopped(name string, failure *verbFailure) map[string]any {
	env := verbEnvelope{SchemaVersion: 1, Verb: name[len("ha_"):]}
	setVerbError(&env, failure)
	out, _ := json.Marshal(env)
	return toolResult(name, string(out), "", verbExitRefused, nil, false)
}

// chatJob is one phone ask of one user's plan, run once in the background.
type chatJob struct {
	done   chan struct{}
	lang   string                 // the plan's, read before its seal is checked: one this binary speaks
	result map[string]any         // never handed out itself: each response gets its own copy (reply adds fields)
	what   string                 // the plan's service or kind, for its activity line (noted once, by the job)
	ended  time.Time              // under bridge.mu
	phone  bool                   // a phone ask (phoneJob), else an apply (applyJob)
	kind   string                 // the plan's kind: its outcome only for a user who may still apply it
	locked chan struct{}          // closed once the job holds mcpVerbLock (never when it gave up on it)
	tapped atomic.Bool            // the phone answered (Confirm, Cancel, someone else's tap); apply may be running
	sent   atomic.Bool            // the question went out (askPhone): until then HA NOVA is still checking
	tap    atomic.Pointer[string] // how to answer on the phone asked (phoneTapLine), once known
	asking atomic.Bool            // ha-nova mcp: its Relay ask waits for the person; nothing was sent yet (gateApproval)
}

// chatApply: in a chat app, ha_apply of this user's plan starts (or joins)
// its job, a phone plan's ask or the apply itself, and waits up to the
// bridge's wait (awaitJob); a chat-code plan made before the owner turned
// chat codes off is refused. nil: no plan of this user, so apply says why.
func (srv *mcpServer) chatApply(name string, args map[string]json.RawMessage, argv []string) map[string]any {
	c := srv.chat
	if c == nil || name != "ha_apply" {
		return nil
	}
	id := text(args, "plan_id")
	if !planIDPattern.MatchString(id) {
		return nil
	}
	plan, err := readPlanFile(filepath.Join(planDir(srv.paths), id+".json")) // this user's plans only
	what := "plan"
	if err == nil {
		what = planWhat(plan)
	}
	refuse := func(failure *verbFailure) map[string]any { // refused before any job: noted here
		result := chatStopped(name, failure)
		srv.noteChange(name, what, result)
		return result
	}
	if err == nil && plan.Tier == tierTyped.String() && plan.Channel == channelChat && !c.chatCodes() {
		return refuse(refusedWith("CONFIRM_UNAVAILABLE", chatCodesOffWhy(c), chatCodesOffNext(c)))
	}
	b, key := c.bridge, chatJobKey(c.user, id)
	b.mu.Lock()
	job := b.jobs[key]
	if job == nil {
		if err != nil {
			b.mu.Unlock()
			return nil
		}
		phone := plan.Channel == channelPhone
		for k, old := range b.jobs {
			switch {
			case !old.ended.IsZero() && time.Since(old.ended) > planTTL: // a result nobody came back for
				delete(b.jobs, k)
			case phone && old.phone && old.ended.IsZero() && strings.HasPrefix(k, c.user+"/"): // one open ask per user: never a stack of notifications
				b.mu.Unlock()
				open, _, _ := strings.Cut(strings.TrimPrefix(k, c.user+"/"), "/")
				return refuse(refusedWith("CONFIRM_PENDING", "another change ("+open+") is still waiting for a tap on the phone",
					fmt.Sprintf("Tell the user nothing changed yet: their phone still has an open HA NOVA confirmation. Call ha_apply with plan_id %s right away, in this same answer, to wait for that one first (it sends no new notification), then apply this plan. You can't act once your answer ends, so never promise to do either on your own; if you end your answer first, tell the user to ask you again for this change.", open)))
			}
		}
		job = &chatJob{done: make(chan struct{}), locked: make(chan struct{}), lang: confirmLanguage(plan.Lang), what: what, phone: phone, kind: plan.Kind}
		b.jobs[key] = job
		if phone {
			go srv.phoneJob(job, args, argv)
		} else {
			go srv.applyJob(job, args, argv)
		}
	}
	b.mu.Unlock()
	return srv.awaitJob(key, job, id)
}

// phoneJob runs apply's phone path with the wait outside the verb lock, so
// other calls (other users, or this user's reads) run meanwhile. A tap
// applies at once, also while no call is waiting; the result waits for the
// next ha_apply.
func (srv *mcpServer) phoneJob(job *chatJob, args map[string]json.RawMessage, argv []string) {
	var a *phoneAsking
	defer func() {
		if p := recover(); p != nil {
			job.result = invalidArguments("ha_apply", fmt.Errorf("internal error while waiting for the phone: %v", p))
			if a != nil {
				a.s.close()
			}
		}
		srv.chat.bridge.mu.Lock()
		job.ended = time.Now()
		srv.chat.bridge.mu.Unlock()
		close(job.done)
	}()
	argv = pinServer("ha_apply", argv, srv.pin)
	var req applyRequest
	if err := srv.lockVerb(); err != nil { // nothing asked yet: the plan stays valid
		job.result = srv.chatBusy("ha_apply", args, err.Error())
		srv.chat.bridge.noteChange(srv.chat, "ha_apply", job.what, "phone", job.result)
		return
	}
	close(job.locked)
	result := srv.runLocked("ha_apply", "", false, func() int {
		var env verbEnvelope
		var exit int
		var ok, asked bool
		if req, exit, ok = parseApplyArgs(argv); !ok {
			return exit
		}
		if a, env, exit, asked = startPhoneAsk(srv.paths, req.opts, req.id, req.noSnapshot); a != nil {
			return 0 // asked below, unlocked
		}
		return finishApply(srv.paths, req, env, exit, asked)
	})
	if a == nil { // refused before the phone was asked (an expired plan, a phone gone)
		job.result = result
		srv.chat.bridge.noteChange(srv.chat, "ha_apply", job.what, "phone", result)
		return
	}
	tap := a.tapLine(textLang(a.plan))
	job.tap.Store(&tap)
	a.ask.sent = &job.sent
	a.wait(nil)
	job.tapped.Store(a.answer == phoneConfirmed || a.answer == phoneDeclined || a.answer == phoneRejected)
	// The owner may have removed this user, lowered their level, or set read
	// only meanwhile, or the user's Home Assistant role may have changed:
	// read again, and apply's recompute checks the call with that level.
	var now *chatPolicy
	var code int
	var why string
	var demoted *verbFailure
	if a.answer == phoneConfirmed { // the tap stands: these reads wait for a running change like the apply
		now, code, why = srv.chat.bridge.policyNow(srv.chat.user)
		id, _ := json.Marshal(req.id)
		demoted = srv.configAdminFailure("ha_apply", map[string]json.RawMessage{"plan_id": id}, chatWait(time.Now()))
		for demoted != nil && demoted.code == "BUSY" {
			demoted = srv.configAdminFailure("ha_apply", map[string]json.RawMessage{"plan_id": id}, chatWait(time.Now()))
		}
	}
	mcpVerbLock.Lock() // the tap stands: as long as it takes
	job.result = srv.runLocked("ha_apply", "", false, func() int {
		var env verbEnvelope
		var exit int
		var asked bool
		now = now.reread() // under the lock: the owner's settings as they are now
		switch {
		case a.answer != phoneConfirmed:
			env, exit, asked = a.finish()
		case now == nil && code == http.StatusServiceUnavailable:
			env, exit, asked = a.refuse("ACCESS_UNKNOWN", why, "Tell the user nothing changed: HA NOVA couldn't check their Home Assistant account just now. They can try again in a moment.")
		case now == nil:
			env, exit, asked = a.refuse("CHAT_APPS_REFUSED", "HA NOVA no longer accepts changes from this chat app ("+why+")",
				"Tell the user nothing changed: the Home Assistant owner changed who may use HA NOVA in chat apps while the phone was asking.")
		case demoted != nil:
			env, exit, asked = a.refuse(demoted.code, demoted.message, demoted.next)
		case now.readOnly:
			env, exit, asked = a.refuse("READ_ONLY", "the owner set chat apps to read only while the phone was asking",
				"Tell the user nothing changed: the Home Assistant owner set chat apps to read only on the HA NOVA page. They can make this change in Home Assistant itself.")
		case !now.mayApply(a.plan.Kind):
			env, exit, asked = a.refuse(chatLevelWords(now, false))
		default:
			chatCall = now // apply's recompute checks the call against this level (chatCallFailure)
			env, exit, asked = a.finish()
		}
		return finishApply(srv.paths, req, env, exit, asked)
	})
	srv.chat.bridge.noteChange(srv.chat, "ha_apply", job.what, "phone", job.result)
}

// waitingForPhone: the ask is still open (once the phone answered, the
// apply that follows answers applying); before its question went out
// (sent), HA NOVA is still checking the phone, so nobody watches it yet.
func waitingForPhone(id, lang string, tap *string, sent bool) map[string]any {
	how := confirmT(lang, "say.phone_waiting_how")
	if tap != nil {
		how = *tap
	}
	say := confirmT(lang, "say.phone_waiting", "tap", how) + " " + sayNotRun(lang)
	next := fmt.Sprintf("Nothing has run yet: HA NOVA is still waiting for the user's tap on their phone. Tell the user `say`, then call ha_apply again with plan_id %s and no confirm_code right away, in this same answer: it keeps waiting on the same notification and sends no new one. You can't act once your answer ends, so never tell the user you will report back on your own; if you end your answer before the outcome, tell them to ask you again once they tapped. Never ask for a code.", id)
	if !sent {
		say, next = sayNotRun(lang), fmt.Sprintf("Nothing has run yet: HA NOVA is still checking the user's linked phone in Home Assistant, and nothing was sent to it yet. Tell the user `say`, then call ha_apply again with plan_id %s and no confirm_code right away, in this same answer. Don't tell the user to watch their phone yet. Never ask for a code.", id)
	}
	envelope := map[string]any{"schema_version": 1, "verb": "apply", "status": "waiting_for_phone", "plan": map[string]any{"id": id}, "say": say, "next_step": next}
	text, _ := json.Marshal(envelope)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "structuredContent": envelope, "isError": false}
}
