package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Applying a batch (docs/work/2026-10-29-batch-confirmation-spec.md §4):
// every member is planned again from its stored request before anything is
// sent, every delete's copy is saved before the first send, then each member
// is checked again right before its one write, in order, and the batch
// stops at the first one that is not verified. Nothing is rolled back; the
// ledger (result.items) says what went through, what stopped, and what was
// not attempted.

// batchMember is one member planned again: the plan whose preview the user
// saw, the new one, and what its texts and its write need.
type batchMember struct {
	stored, fresh storedPlan
	e             *tierEngine
	env           verbEnvelope
	ctx           configContext
}

type batchReplan struct {
	members []batchMember
	lang    string
}

// batchIdentity is what a batch's hash covers per member, in order.
type batchIdentity struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
}

func batchIdentities(members []storedPlan) []batchIdentity {
	out := []batchIdentity{}
	for _, m := range members {
		out = append(out, batchIdentity{ID: m.ID, Hash: m.Hash})
	}
	return out
}

// batchItem is one member's ledger row (plan batch: its tier; apply: its
// status and its way back).
type batchItem struct {
	Item         int                  `json:"item"`
	Plan         string               `json:"plan"`
	Op           string               `json:"op"`
	Domain       string               `json:"domain"`
	EntityID     string               `json:"entity_id,omitempty"`
	ConfigID     string               `json:"config_id"`
	Name         string               `json:"name,omitempty"`
	Tier         string               `json:"tier,omitempty"`
	Status       string               `json:"status,omitempty"`
	Verification []verifyRow          `json:"verification,omitempty"`
	Snapshot     string               `json:"snapshot,omitempty"`
	Restore      string               `json:"restore,omitempty"`
	Undo         *snapshotSaveReceipt `json:"undo,omitempty"`
	Error        string               `json:"error,omitempty"`
	quoted       string
}

type batchCounts struct {
	Verified     int `json:"verified"`
	Stopped      int `json:"stopped"`
	NotAttempted int `json:"not_attempted"`
}

type batchResult struct {
	Items  []batchItem  `json:"items"`
	Counts *batchCounts `json:"counts,omitempty"`
	lang   string
}

// computeBatchPlan plans every member again from its stored request and
// seals the batch. A member that is no longer the plan whose preview was
// shown (or no longer plans at all) changes the whole batch: PLAN_DRIFT in
// plan batch, BATCH_DRIFT in apply, never a new batch.
func computeBatchPlan(paths runtimePaths, s *relaySession, env *verbEnvelope, stored []storedPlan) (batchReplan, storedPlan, error) {
	if s.lang == "" {
		s.lang = s.language()
	}
	rp := batchReplan{lang: s.lang}
	var changed []string
	for _, m := range stored {
		member, err := replanMember(paths, s, m)
		var refusal *verbFailure
		switch {
		case errors.As(err, &refusal):
			changed = append(changed, member.name())
		case err != nil:
			return rp, storedPlan{}, err
		case !stillConfirmed(m, member.fresh) || member.fresh.Tier != m.Tier:
			changed = append(changed, member.name())
		}
		rp.members = append(rp.members, member)
	}
	if len(changed) > 0 {
		return rp, storedPlan{}, batchChanged(env, rp.lang, changed)
	}
	e := newTierEngine(s)
	for _, m := range rp.members {
		e.lift(map[bool]tierLevel{true: tierTyped, false: tierPreview}[m.fresh.Tier == tierTyped.String()])
		e.complete = e.complete && m.e.complete
	}
	plan, err := sealPlan(e, env, storedPlan{Kind: "batch", BatchMembers: stored}, batchIdentities(stored))
	if err != nil {
		return rp, plan, err
	}
	text := batchText(rp, plan.Tier == tierTyped.String())
	if plan.Tier == tierTyped.String() {
		plan.Prompt = demoPrefix(text)
	}
	env.Result, env.ask = batchPlanResult(rp), batchPhoneGoal(rp)
	env.Say = batchSay(s, plan, env.ask, text, len(rp.members))
	env.NextStep = batchNextStep(env.NextStep, plan)
	return rp, plan, nil
}

// replanMember plans one member again on its own: its channel is never
// asked (the batch's is), so it is planned as a chat plan.
func replanMember(paths runtimePaths, s *relaySession, m storedPlan) (batchMember, error) {
	defer func(saved string) { planChannel = saved }(planChannel)
	planChannel = channelChat
	s.renewBudget()
	member := batchMember{stored: m, env: verbEnvelope{SchemaVersion: 1, Verb: "apply", Status: "ok", Server: s.server}}
	var err error
	member.e, member.fresh, member.ctx, err = computeConfigPlan(paths, s, &member.env, *m.ConfigRequest)
	return member, err
}

func batchChanged(env *verbEnvelope, lang string, names []string) error {
	if env.Verb == "plan" {
		return &verbFailure{exit: verbExitDrift, status: "drift", code: "PLAN_DRIFT",
			message: sayList(names) + " changed since the preview; nothing was planned or sent",
			next:    "Plan each one named again and show its new preview, then make the batch again with its new plan id; the other plans are as they were."}
	}
	env.Say = confirmT(lang, "batch.drift", "count", len(names), "names", sayList(names))
	return &verbFailure{exit: verbExitDrift, status: "drift", code: "BATCH_DRIFT",
		message: sayList(names) + " changed since the preview; nothing was sent, and the batch is used up",
		next:    "Show `say`. Plan each one named again and show its new preview; the other plans of this batch are gone too, so plan them again as well, then make a new batch of all of them. Never apply this batch again."}
}

func batchPlanResult(rp batchReplan) batchResult {
	result := batchResult{lang: rp.lang}
	for i, m := range rp.members {
		item := m.item(i)
		item.Tier = m.fresh.Tier
		result.Items = append(result.Items, item)
	}
	return result
}

func (m batchMember) item(i int) batchItem {
	action := m.stored.ConfigAction
	return batchItem{Item: i + 1, Plan: m.stored.ID, Op: m.op(), Domain: action.Domain, EntityID: action.EntityID, ConfigID: action.ConfigID,
		Name: strings.TrimSpace(configAlias(m.ctx.current, m.stored.ConfigRequest.Draft)), quoted: m.name()}
}

// forgetReads drops the session's remembered reads: after a send, the next
// member's check must see Home Assistant as it is now.
func (s *relaySession) forgetReads() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.memo = map[string]*relayMemo{}
}

// executeBatch sends the members in order. A failure before the first send
// (a copy that could not be saved, the Relay gone) sends nothing and hands
// the batch back; after it, the batch stops and the ledger says why.
func executeBatch(paths runtimePaths, s *relaySession, env *verbEnvelope, rp batchReplan, noSnapshot bool) (err error) {
	sent := false
	defer afterSend(&sent, env, &err)
	result := batchResult{Counts: &batchCounts{}, lang: rp.lang}
	for i, m := range rp.members {
		item := m.item(i)
		item.Status = "not_attempted"
		result.Items = append(result.Items, item)
	}
	snapshots := make([]string, len(rp.members))
	for i, m := range rp.members {
		action := m.fresh.ConfigAction
		if action.Op != "delete" || noSnapshot {
			continue
		}
		file, serr := s.saveConfigSnapshot(map[string]string{"automation": "automations", "script": "scripts"}[action.Domain], snapshotName(action.EntityID), m.ctx.current)
		if serr != nil {
			return &verbFailure{exit: verbExitError, code: "SNAPSHOT_FAILED", cause: errSnapshotFailed,
				message: fmt.Sprintf("no copy of %s could be saved before deleting it (%s); nothing was sent and the batch stays valid", m.name(), serr),
				next:    "Tell the user there is no copy yet, so nothing was changed. They choose: prune the snapshot store or take a full backup (ha-nova:backup) and apply again, or go on without copies: then apply again with --no-snapshot."}
		}
		snapshots[i] = file
	}
	for i := range rp.members {
		m := rp.members[i]
		if i > 0 {
			s.forgetReads()
			again, rerr := replanMember(paths, s, m.stored)
			var refusal *verbFailure
			switch {
			case rerr != nil && !errors.As(rerr, &refusal):
				return batchStopped(env, &result, i, "not_sent", "it could not be checked again before sending: "+rerr.Error())
			case rerr != nil || !stillConfirmed(m.stored, again.fresh) || again.fresh.Tier != m.stored.Tier:
				return batchStopped(env, &result, i, "drift", "it changed since the preview, so it was not sent")
			}
			m = again
		}
		menv := verbEnvelope{SchemaVersion: 1, Verb: "apply", Status: "ok", Server: s.server, Snapshot: snapshots[i]}
		werr := executeConfig(paths, s, &menv, *m.fresh.ConfigAction, m.ctx, true)
		item := &result.Items[i]
		item.Verification, item.Snapshot, item.Restore, item.Undo = menv.Verification, menv.Snapshot, menv.Restore, menv.Undo
		if len(menv.Verification) > 0 && menv.Verification[0].EntityID != "" {
			item.EntityID = menv.Verification[0].EntityID
		}
		env.Notices = append(env.Notices, menv.Notices...)
		var failure *verbFailure
		switch {
		case werr == nil && menv.Status == "verified":
			sent, item.Status = true, "verified"
			result.Counts.Verified++
			continue
		case errors.Is(werr, errNothingSent) && !sent:
			return werr
		case errors.Is(werr, errNothingSent):
			return batchStopped(env, &result, i, "not_sent", werr.Error())
		case werr == nil:
			sent = true
			return batchStopped(env, &result, i, "unverified", "it was sent, but the check after it did not pass")
		}
		sent = true
		switch errors.As(werr, &failure); {
		case failure != nil && failure.code == "OUTCOME_UNKNOWN":
			return batchStopped(env, &result, i, "unknown", werr.Error())
		case failure != nil && failure.status == "failed":
			return batchStopped(env, &result, i, "failed", werr.Error())
		}
		return batchStopped(env, &result, i, "unverified", werr.Error())
	}
	env.Result, env.Status, env.exit = result, "verified", verbExitOK
	env.Say = batchDoneSay(rp.lang, len(rp.members), noSnapshot && slices.ContainsFunc(rp.members, func(m batchMember) bool { return m.op() == "delete" }))
	env.NextStep = "Tell the user `say`. Every item went through (result.items). Only if they ask: a changed one goes back with ha-nova plan revert --target <its config_id>, a deleted one with its result.items[].restore command; each is a new plan."
	return nil
}

// batchStopped records a batch that stopped at member i (0-based); every
// member before it was verified, every one after it was not attempted.
func batchStopped(env *verbEnvelope, result *batchResult, i int, status, why string) error {
	item := &result.Items[i]
	item.Status, item.Error = status, why
	result.Counts.Stopped = 1
	result.Counts.NotAttempted = len(result.Items) - i - 1
	env.Result, env.Status, env.exit = *result, "failed", verbExitUnverified
	env.Error = &verbError{Code: "BATCH_STOPPED", Message: fmt.Sprintf("item %d (%s) did not go through: %s; %d of %d went through, the rest were not sent", i+1, item.quoted, why, result.Counts.Verified, len(result.Items)),
		Next: "Tell the user `say`: which went through (result.items[].status verified), what stopped the next one (its error and verification), and that the rest were not sent. Never apply this batch again; the rest is a new batch with a new confirmation, only if they still want it."}
	switch status {
	case "unknown":
		env.Status, env.exit, env.OutcomeUnknown = "unverified", verbExitUnknown, true
		env.Error.Code = "OUTCOME_UNKNOWN"
		env.Error.Next = "Read whether that item took effect (ha-nova get with --config) and tell the user; never apply this batch again. The items after it were not sent."
	case "unverified":
		env.Status = "unverified"
	}
	env.Say = batchStopSay(result.lang, *result, i)
	return nil
}
