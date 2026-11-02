package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// Chat-app applies run as jobs (ha-nova serve; docs/work/2026-10-19-chat-
// bridge-contract.md): a chat app gives up on a tool call after about a
// minute, and a batch in a home with many automations runs for minutes.
// ha_apply waits up to the bridge's wait, then answers applying; calling
// it again waits on the same job, which never sends anything twice, and
// the outcome is handed out once. No chat-app call waits longer than
// chatLockWait for mcpVerbLock: past it, the call answers BUSY and runs
// nothing. Only what follows a phone tap waits for it as long as it takes.

// verbLock is mcpVerbLock: a mutex a waiter can give up on.
type verbLock chan struct{}

func (l verbLock) Lock() { l <- struct{}{} }

func (l verbLock) Unlock() {
	select {
	case <-l:
	default:
		panic("mcpVerbLock: unlock of an unlocked lock")
	}
}

func (l verbLock) TryLock() bool {
	select {
	case l <- struct{}{}:
		return true
	default:
		return false
	}
}

// lockWithin takes the lock, or gives up after d (false: not taken).
func (l verbLock) lockWithin(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case l <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

// chatLockWait: how long a chat-app call waits for a running change.
// Variable so tests can shrink it.
var chatLockWait = 20 * time.Second

const chatBusyWhy = "HA NOVA is still finishing another change"

// errBridgeBusy: Home Assistant's users could not be read within
// chatLockWait because a running change held mcpVerbLock.
var errBridgeBusy = errors.New(chatBusyWhy)

// policyNow: policy with the users read now, waiting for a running change
// as long as it takes: for the checks after a phone tap, which stands.
func (b *chatBridge) policyNow(user string) (*chatPolicy, int, string) {
	for {
		if p, code, why := b.policy(user, true); code != http.StatusServiceUnavailable || why != chatBusyWhy {
			return p, code, why
		}
	}
}

// lockVerb takes mcpVerbLock for a tool call: in a chat app within
// chatLockWait, else for as long as it takes.
func (srv *mcpServer) lockVerb() bool {
	if srv.chat == nil {
		mcpVerbLock.Lock()
		return true
	}
	return mcpVerbLock.lockWithin(chatLockWait)
}

// chatBusy: a tool call that ran nothing because a running change held the
// lock. An apply's plan stays valid, and its say speaks the plan's language.
func (srv *mcpServer) chatBusy(name string, args map[string]json.RawMessage) map[string]any {
	next := "This request ran nothing: HA NOVA is still finishing a change. Tell the user in their language that HA NOVA is busy and that they can ask again in a moment. Never retry on your own and never say a change failed."
	say := ""
	if id := text(args, "plan_id"); name == "ha_apply" && planIDPattern.MatchString(id) {
		if plan, err := readPlanFile(filepath.Join(planDir(srv.paths), id+".json")); err == nil {
			say = confirmT(confirmLanguage(plan.Lang), "say.busy")
			again := "and the confirm_code the user typed, if any (no new plan; the user confirms nothing again)"
			if plan.Channel == channelPhone {
				again = "(no new plan): HA NOVA then asks on their phone"
			}
			next = fmt.Sprintf("Nothing was sent for this plan, and it is still valid: HA NOVA is still finishing another change. Tell the user `say`; when they ask again, call ha_apply again with plan_id %s %s. Never retry on your own and never say it failed.", id, again)
		}
	}
	if running := srv.chat.bridge.jobOf(srv.chat.user, text(args, "plan_id"), true); running != "" {
		next += fmt.Sprintf(" Their own change (plan_id %s) is still running: call ha_apply with that plan_id for its outcome, and never say it failed or that nothing changed.", running)
	}
	env := verbEnvelope{SchemaVersion: 1, Verb: name[len("ha_"):]}
	setVerbError(&env, refusedWith("BUSY", chatBusyWhy, next))
	env.Say = say
	out, _ := json.Marshal(env)
	return toolResult(name, string(out), "", verbExitRefused, nil, false)
}

func chatJobKey(user, plan string) string { return user + "/" + plan }

// chatJoin: an ha_apply of a plan whose job this user started waits on
// that job, with no lock and no Relay read; nil when there is none.
func (srv *mcpServer) chatJoin(name string, args map[string]json.RawMessage) map[string]any {
	c := srv.chat
	id := text(args, "plan_id")
	if c == nil || name != "ha_apply" || !planIDPattern.MatchString(id) {
		return nil
	}
	key := chatJobKey(c.user, id)
	c.bridge.mu.Lock()
	job := c.bridge.jobs[key]
	c.bridge.mu.Unlock()
	if job == nil {
		return nil
	}
	return srv.awaitJob(key, job, id)
}

// awaitJob waits up to the bridge's wait for job, then hands out its
// result (once: a later apply starts afresh) or what it is doing. Only a
// user the owner's settings (chat-apps.json, read now) still let apply the
// plan's kind gets either, and the result only on Home Assistant's users
// as read within the last minute.
func (srv *mcpServer) awaitJob(key string, job *chatJob, id string) map[string]any {
	b, p := srv.chat.bridge, srv.chat
	timer := time.NewTimer(b.wait)
	defer timer.Stop()
	var done bool
	select {
	case <-job.done:
		done = true
	case <-timer.C:
	}
	if done { // the request's users may be over a minute old by now
		now, code, _ := b.policy(p.user, false)
		if now != nil && now.stale { // read while a change held the lock (homeUsers): read them now
			now, code, _ = b.policy(p.user, true)
		}
		if now == nil && code != http.StatusForbidden {
			return chatStopped("ha_apply", &verbFailure{exit: verbExitRefused, code: "ACCESS_UNKNOWN",
				message: "HA NOVA couldn't check this Home Assistant user's account just now, so it can't tell how the change ended yet",
				next:    fmt.Sprintf("Call ha_apply with plan_id %s again right away, in this same answer. If it answers ACCESS_UNKNOWN again, tell the user HA NOVA can't tell yet how their change ended and that they can ask you again in a moment. Never promise to check on your own, and never say it failed or that nothing changed.", id)})
		}
		p = now
	}
	if now := p.reread(); now == nil || !now.mayApply(job.kind) {
		code := "CHAT_LEVEL"
		if now == nil || now.level == 0 {
			code = "CHAT_APPS_REFUSED"
		}
		return chatStopped("ha_apply", &verbFailure{exit: verbExitRefused, code: code,
			message: "the owner changed this Home Assistant user's access to HA NOVA in chat apps while this change ran",
			next:    "Tell the user: " + chatJobOffList + " Never call ha_apply for it again."})
	}
	if done {
		b.mu.Lock()
		if b.jobs[key] == job {
			delete(b.jobs, key)
		}
		b.mu.Unlock()
		return maps.Clone(job.result)
	}
	if job.phone && !job.tapped.Load() {
		return waitingForPhone(id, job.lang, job.tap.Load())
	}
	return applying(id, job.lang, job.phone)
}

// chatJobOffList: what a user whose access the owner lowered or removed
// hears about a change they started (it may have run).
const chatJobOffList = "The Home Assistant owner changed your access to HA NOVA in chat apps while your change was running, so HA NOVA can't tell you how it ended: it may have run. Check in Home Assistant itself, or ask the owner."

// jobOf: the plan id of a job of user, but except's, that still runs
// (running), or that runs or ended within planTTL with its outcome not
// fetched; "" when none.
func (b *chatBridge) jobOf(user, except string, running bool) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for k, job := range b.jobs {
		if id, ok := strings.CutPrefix(k, user+"/"); ok && id != except && (job.ended.IsZero() || !running && time.Since(job.ended) < planTTL) {
			return id
		}
	}
	return ""
}

// applyJob runs ha_apply as the call would have, in the background: within
// chatLockWait, else BUSY (nothing ran), so applying means it runs.
func (srv *mcpServer) applyJob(job *chatJob, args map[string]json.RawMessage, argv []string) {
	defer func() {
		if p := recover(); p != nil {
			job.result = chatStopped("ha_apply", &verbFailure{exit: verbExitUnknown, status: "unverified", code: "OUTCOME_UNKNOWN",
				message: fmt.Sprintf("internal error while applying: %v", p),
				next:    "Tell the user HA NOVA can't tell whether the change was made: they can check in Home Assistant. Never apply this plan again."})
		}
		srv.chat.bridge.mu.Lock()
		job.ended = time.Now()
		srv.chat.bridge.mu.Unlock()
		close(job.done)
	}()
	if !srv.lockVerb() {
		job.result = srv.chatBusy("ha_apply", args)
	} else {
		job.result = srv.runLocked("ha_apply", "", false, func() int { return runApplyCommand(srv.paths, pinServer("ha_apply", argv, srv.pin)) })
	}
	srv.noteChange("ha_apply", job.what, job.result)
}

// applying: the job runs (a phone plan's after its tap): no outcome yet.
func applying(id, lang string, phone bool) map[string]any {
	say, askAgain := confirmT(lang, "say.applying"), ""
	if phone {
		say, askAgain = confirmT(lang, "say.phone_finishing"), " and that they can ask you again for the result"
	}
	next := fmt.Sprintf("HA NOVA is still making this change; its outcome is not known yet. Don't end your answer here: call ha_apply again with plan_id %s and no confirm_code right away, in this same answer, as often as it answers applying (each call waits on the same run, and nothing is sent twice). You can't act once your answer ends, so never tell the user you will check back, keep watching, or report the result on your own. If you end your answer before the outcome, tell the user `say`%s. If the user writes meanwhile, call it again before you answer. Never plan it again, and never say it failed or that nothing changed.", id, askAgain)
	envelope := map[string]any{"schema_version": 1, "verb": "apply", "status": "applying", "plan": map[string]any{"id": id}, "say": say, "next_step": next}
	text, _ := json.Marshal(envelope)
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}, "structuredContent": envelope, "isError": false}
}
