package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// A chat app on a standalone NOVA Relay container (MAR-154; docs/reference/
// chat-apps.md): the bridge holds the container's plain token, so the Relay
// holds its typed changes until the one phone linked to the container
// approves them, as for a computer (gate_client.go). The apply job waits
// for that phone without mcpVerbLock, as phoneJob does for a chat user's
// own phone, so other chat users aren't BUSY for the ask's three minutes;
// ha_apply answers waiting_for_phone meanwhile.

// askOutside is an apply job's verbCall.outside. With unlock (an apply's
// first ask, gateBeforeSend, checked again after it by gateStillApproved)
// the run's process-wide state goes aside and stays clean for others while
// the lock is free; the lock comes back, as long as it takes (an approval
// stands), with that state, also when the ask panics. After an approval,
// the user's access is read again under the lock, so nothing changes it
// before the apply: a user the owner removed, lowered, or set to read only
// meanwhile, or who is no longer an administrator for a config plan, sends
// nothing, and the grant expires unused.
func (srv *mcpServer) askOutside(job *chatJob, call *verbCall) func(func() bool, bool) error {
	return func(ask func() bool, unlock bool) error {
		job.tapped.Store(false) // a later ask of this apply (an expired grant) waits for the phone again
		job.sent.Store(false)
		job.outside.Store(true)
		defer job.outside.Store(false)
		relock := func() {}
		if unlock {
			mine := takeVerbState()
			verbState{windowWait: cliWindowWait}.put()
			mcpVerbLock.Unlock()
			locked := false
			relock = func() {
				if !locked {
					mcpVerbLock.Lock()
					mcpVerbLock.changing()
					mine.put()
					locked = true
				}
			}
			defer relock() // also when the ask panicked: runLocked unlocks the lock it holds again
		}
		approved := ask()
		job.sent.Store(true) // the ask ended: until the lock is back, ha_apply still says the phone was asked
		job.tapped.Store(approved)
		relock()
		if !approved {
			return nil // gateAsk says how it ended; nothing was sent
		}
		now, code, why := srv.chat.bridge.policyNow(srv.chat.user)
		kind, _ := json.Marshal(job.kind) // the plan is claimed now: its kind decides, as when it was planned
		admin := func() *verbFailure {
			return srv.configAdminFailure("ha_plan", map[string]json.RawMessage{"kind": kind}, chatWait(time.Now()))
		}
		demoted := admin()
		for demoted != nil && demoted.code == "BUSY" {
			demoted = admin()
		}
		switch {
		case now == nil && code == http.StatusServiceUnavailable:
			return gateStop("refused", "ACCESS_UNKNOWN", why+"; nothing was sent", "Tell the user nothing changed: HA NOVA couldn't check their Home Assistant account just now. They can try again in a moment.")
		case now == nil:
			return gateStop("refused", "CHAT_APPS_REFUSED", "HA NOVA no longer accepts changes from this chat app ("+why+"); nothing was sent",
				"Tell the user nothing changed: the Home Assistant owner changed who may use HA NOVA in chat apps while the phone was asking.")
		case demoted != nil:
			return gateStop("refused", demoted.code, demoted.message+"; nothing was sent", demoted.next)
		case now.readOnly:
			return gateStop("refused", "READ_ONLY", "the owner set chat apps to read only while the phone was asking; nothing was sent",
				"Tell the user nothing changed: the Home Assistant owner set chat apps to read only on the HA NOVA page. They can make this change in Home Assistant itself.")
		case !now.mayApply(job.kind):
			code, message, next := chatLevelWords(now, false)
			return gateStop("refused", code, message, next)
		}
		chatCall, call.chat = now, now // apply's recheck checks the call against this level (chatCallFailure)
		return nil
	}
}

// waitingForRelay: the job's Relay ask is open (sent) or still being
// opened; the phone asked is the container's, maybe not the user's own.
func waitingForRelay(id, lang string, sent bool) map[string]any {
	if !sent {
		return phoneWaitEnvelope(id, sayNotRun(lang), fmt.Sprintf("Nothing has run yet: HA NOVA is still preparing the ask for the phone linked to the user's NOVA Relay container, and nothing was sent to it yet. Tell the user `say`, then call ha_apply again with plan_id %s and no confirm_code right away, in this same answer. Don't tell the user to watch a phone yet. Never ask for a code.", id))
	}
	return phoneWaitEnvelope(id, confirmT(lang, "say.relay_container_waiting"), fmt.Sprintf("Nothing has run yet: HA NOVA is waiting for an OK on the phone linked to the user's NOVA Relay container (often the phone of whoever runs it). Tell the user `say`, then call ha_apply again with plan_id %s and no confirm_code right away, in this same answer: it keeps waiting on the same ask and sends no new one. You can't act once your answer ends, so never tell the user you will report back on your own; if you end your answer before the outcome, tell them to ask you again once the phone confirmed. Never ask for a code.", id))
}
