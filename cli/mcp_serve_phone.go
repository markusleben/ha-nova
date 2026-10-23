package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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
// The MCP server sets chatCall to it for the verb it runs (runVerb, under
// mcpVerbLock); nil everywhere else.
type chatPolicy struct {
	user     string // the HA user id the integration vouched for (secret-authenticated)
	key      string // its hash: the user's data directory and log label
	links    string // the bridge's phone links: {HA user id: phoneLink}, its only store
	codes    bool   // chat codes allowed (owner switch)
	readOnly bool   // chat apps can only read (owner switch)
	bridge   *chatBridge
}

var chatCall *chatPolicy

// chatAskSpan: how long a chat app's phone ask stays open across its
// waiting_for_phone calls (bounded by the plan's expiry).
// ponytail: fixed; make it a flag if users need longer.
const chatAskSpan = 3 * time.Minute

const (
	chatCodesOffWhy  = "this change needs a tap on the user's linked phone; no phone is linked for their Home Assistant user, and chat codes are turned off"
	chatCodesOffNext = "Tell the user nothing changed: this change needs a tap on their phone. They link a phone on their computer (`ha-nova setup confirm-phone`, then `ha-nova chat-apps on`), then ask again; or they make this change in Home Assistant itself. Never ask them for a code."
	chatRelayNext    = "Tell the user nothing changed: HA NOVA in Home Assistant can't reach its Relay right now. They try again in a moment; if it keeps failing, the Home Assistant owner checks the NOVA App (Settings → Apps). Never suggest a terminal command."
	chatPhoneMissing = "Tell the user nothing changed: HA NOVA can't use the phone linked to their Home Assistant user right now (the phone's Home Assistant app must be signed in with their own account, and that account must be a Home Assistant administrator). They check that phone's Home Assistant app, or make this change in Home Assistant itself. Never ask them for a code."
)

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
	case chatCall != nil && !chatCall.codes:
		return channelChatOff
	case chatCall != nil:
		return channelChat
	case formsShown:
		return channelForm
	}
	return channelChat
}

// discoverConfirmer: the linked phone, which in a chat app must confirm as
// the chat app's own user; any other confirmer is refused, never moved to a
// code.
func discoverConfirmer(s *relaySession) (phoneSetup, phoneStatus) {
	setup, status := phoneDiscover(s)
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
	raw, err := os.ReadFile(chatCall.links)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]phoneEntry{}, true
	}
	var byUser map[string]*phoneLink
	if err != nil || json.Unmarshal(raw, &byUser) != nil {
		return nil, false
	}
	links := map[string]phoneEntry{}
	if link, present := byUser[chatCall.user]; present {
		if link == nil { // null: an unusable link (refused), never "no phone"
			link = &phoneLink{}
		}
		links[chatOwner] = phoneEntry{link: link}
	}
	return links, true
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
	return chatStopped(tool.name, refusedWith("READ_ONLY", "chat apps can only read in this home (the owner's setting)",
		"Tell the user nothing changed: the Home Assistant owner set chat apps to read only on the HA NOVA page. They can make this change in Home Assistant itself."))
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
	result map[string]any         // never handed out itself: each response gets its own copy (reply adds fields)
	ended  time.Time              // under bridge.mu
	tapped atomic.Bool            // the phone answered; apply may be running
	tap    atomic.Pointer[string] // how to answer on the phone asked (phoneTapLine), once known
}

// chatApply: in a chat app, ha_apply on a phone plan starts (or joins) its
// ask and waits up to the bridge's wait; a chat-code plan made before the
// owner turned chat codes off is refused. nil: run ha_apply as usual.
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
	if err == nil && plan.Tier == tierTyped.String() && plan.Channel == channelChat && !c.codes {
		return chatStopped(name, channelRefusal(channelChatOff))
	}
	var noSnapshot bool
	_ = json.Unmarshal(args["no_snapshot"], &noSnapshot)
	b, key := c.bridge, fmt.Sprintf("%s/%s/%t", c.user, id, noSnapshot)
	b.mu.Lock()
	job := b.jobs[key]
	if job == nil {
		if err != nil || plan.Channel != channelPhone {
			b.mu.Unlock()
			return nil
		}
		for k, old := range b.jobs {
			switch {
			case !old.ended.IsZero() && time.Since(old.ended) > planTTL: // a result nobody came back for
				delete(b.jobs, k)
			case old.ended.IsZero() && strings.HasPrefix(k, c.user+"/"): // one open ask per user: never a stack of notifications
				b.mu.Unlock()
				open, _, _ := strings.Cut(strings.TrimPrefix(k, c.user+"/"), "/")
				return chatStopped(name, refusedWith("CONFIRM_PENDING", "another change ("+open+") is still waiting for a tap on the phone",
					fmt.Sprintf("Tell the user nothing changed yet: their phone still has an open HA NOVA confirmation. Call ha_apply with plan_id %s to wait for that one first (it sends no new notification), then apply this plan.", open)))
			}
		}
		job = &chatJob{done: make(chan struct{})}
		b.jobs[key] = job
		go srv.phoneJob(job, argv)
	}
	b.mu.Unlock()
	timer := time.NewTimer(b.wait)
	defer timer.Stop()
	select {
	case <-job.done:
		b.mu.Lock()
		if b.jobs[key] == job { // handed out once; a later apply starts afresh
			delete(b.jobs, key)
		}
		b.mu.Unlock()
		return maps.Clone(job.result)
	case <-timer.C:
		return waitingForPhone(id, job.tapped.Load(), job.tap.Load())
	}
}

// phoneJob runs apply's phone path with the wait outside the verb lock, so
// other calls (other users, or this user's reads) run meanwhile. A tap
// applies at once, also while no call is waiting; the result waits for the
// next ha_apply.
func (srv *mcpServer) phoneJob(job *chatJob, argv []string) {
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
	result := srv.runVerb("ha_apply", "", false, func() int {
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
	if a == nil {
		job.result = result
		return
	}
	tap := a.tapLine()
	job.tap.Store(&tap)
	a.wait(nil)
	job.tapped.Store(a.answer != phoneNoAnswer)
	// The owner may have removed this user or set read only meanwhile.
	now, _, why := srv.chat.bridge.policyFor(srv.chat.user)
	job.result = srv.runVerb("ha_apply", "", false, func() int {
		var env verbEnvelope
		var exit int
		var asked bool
		switch {
		case a.answer != phoneConfirmed:
			env, exit, asked = a.finish()
		case now == nil:
			env, exit, asked = a.refuse("CHAT_APPS_REFUSED", "HA NOVA no longer accepts changes from this chat app ("+why+")",
				"Tell the user nothing changed: the Home Assistant owner changed who may use HA NOVA in chat apps while the phone was asking.")
		case now.readOnly:
			env, exit, asked = a.refuse("READ_ONLY", "the owner set chat apps to read only while the phone was asking",
				"Tell the user nothing changed: the Home Assistant owner set chat apps to read only on the HA NOVA page. They can make this change in Home Assistant itself.")
		default:
			env, exit, asked = a.finish()
		}
		return finishApply(srv.paths, req, env, exit, asked)
	})
}

// waitingForPhone: the ask is still open, or the phone answered and apply
// is still running (tapped): no outcome yet either way.
func waitingForPhone(id string, tapped bool, tap *string) map[string]any {
	how := "Press and hold it on an iPhone, or pull it down on Android, then tap the button for this change."
	if tap != nil {
		how = *tap
	}
	say := "Waiting for your tap on the HA NOVA notification on your phone (it can take a few seconds to arrive). " + how + " " + sayNotRun
	next := fmt.Sprintf("Nothing has run yet: HA NOVA is still waiting for the user's tap on their phone. Tell the user `say`, then call ha_apply again with plan_id %s and no confirm_code: it keeps waiting on the same notification and sends no new one. Never ask for a code.", id)
	if tapped {
		say = "Your phone answered; HA NOVA is finishing."
		next = fmt.Sprintf("The phone answered and HA NOVA is still finishing; the outcome is not known yet. Call ha_apply again with plan_id %s and no confirm_code to get it; never plan again or ask for a code.", id)
	}
	envelope := map[string]any{"schema_version": 1, "verb": "apply", "status": "waiting_for_phone", "plan": map[string]any{"id": id}, "say": say, "next_step": next}
	text, _ := json.Marshal(envelope)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "structuredContent": envelope, "isError": false}
}
