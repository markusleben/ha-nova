package main

import (
	"encoding/json"
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
	BeforeConfig json.RawMessage      `json:"before_config,omitempty"` // an update whose checkpoint wasn't saved
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
	var changed, changedIDs []string
	for _, m := range stored {
		member, err := replanMember(paths, s, m)
		var refusal *verbFailure
		switch {
		case err != nil && !errors.As(err, &refusal):
			return rp, storedPlan{}, err
		case err != nil || !stillConfirmed(m, member.fresh) || member.fresh.Tier != m.Tier:
			changed, changedIDs = append(changed, member.name()), append(changedIDs, m.ID)
		}
		rp.members = append(rp.members, member)
	}
	if len(changed) > 0 {
		return rp, storedPlan{}, batchChanged(env, rp.lang, changed, changedIDs)
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
		plan.Prompt = demoPrefix(textLang(plan), text)
	}
	if plan.Channel == channelWindow { // checked again at every apply: a name in a reason may have grown
		if err := batchWindowFits(plan, env.Verb); err != nil {
			return rp, plan, err
		}
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

func batchChanged(env *verbEnvelope, lang string, names, ids []string) error {
	which := fmt.Sprintf("%s changed since the preview (plan %s)", strings.Join(names, ", "), strings.Join(ids, ", "))
	if env.Verb == "plan" {
		return &verbFailure{exit: verbExitDrift, status: "drift", code: "PLAN_DRIFT",
			message: which + "; nothing was planned or sent",
			next:    "Plan each one named again and show its new preview, then make the batch again with its new plan id; the other plans are as they were."}
	}
	env.Say = confirmT(lang, "batch.drift", "count", len(names), "names", sayList(lang, names))
	return &verbFailure{exit: verbExitDrift, status: "drift", code: "BATCH_DRIFT",
		message: which + "; nothing was sent, and the batch is used up",
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
	written := map[string]string{}            // basis.configs key → the digest of the config this apply sent and verified there
	live := func(key string) (string, bool) { // a written sibling's config as it is now, digested as a scan reads it
		domain, configID, _ := strings.Cut(key, ":")
		raw, err := readStoredConfig(s, domain, configID)
		if err != nil {
			return "", false
		}
		e := newTierEngine(s)
		if _, ok := e.parseConfig(key, domain, configID, raw, "batch"); !ok {
			return "", false
		}
		return e.basis[key], true
	}
	for i := range rp.members {
		m := rp.members[i]
		if i > 0 {
			s.forgetReads()
			again, rerr := replanMember(paths, s, m.stored)
			var refusal *verbFailure
			switch {
			case rerr != nil && !errors.As(rerr, &refusal):
				return batchStopped(env, &result, i, "not_sent", "it could not be checked again before sending: "+rerr.Error(), "")
			case rerr != nil || !excusedByBatch(m.stored, again.fresh, written, live) || again.fresh.Tier != m.stored.Tier:
				return batchStopped(env, &result, i, "drift", "it changed since the preview, so it was not sent", "")
			}
			m = again
		}
		menv := verbEnvelope{SchemaVersion: 1, Verb: "apply", Status: "ok", Server: s.server, Snapshot: snapshots[i]}
		werr := executeConfig(paths, s, &menv, *m.fresh.ConfigAction, m.ctx, true)
		item := &result.Items[i]
		item.Verification, item.Snapshot, item.Restore, item.Undo = menv.Verification, menv.Snapshot, menv.Restore, menv.Undo
		if menv.Undo == nil { // no checkpoint was saved: the replaced config is its only way back
			item.BeforeConfig = menv.BeforeConfig
		}
		if len(menv.Verification) > 0 && menv.Verification[0].EntityID != "" {
			item.EntityID = menv.Verification[0].EntityID
		}
		for _, notice := range menv.Notices {
			notice.Text = fmt.Sprintf("item %d (%s): %s", i+1, item.quoted, notice.Text)
			env.Notices = append(env.Notices, notice)
		}
		switch {
		case werr == nil && menv.Status == "verified":
			sent, item.Status = true, "verified"
			key := m.fresh.ConfigAction.Domain + ":" + m.fresh.ConfigAction.ConfigID
			if configs, _ := asJSON(m.fresh.Basis)["configs"].(map[string]any); configs[key+"#draft"] != nil { // a delete has none: never excused
				written[key] = fmt.Sprint(configs[key+"#draft"])
			}
			result.Counts.Verified++
			continue
		case errors.Is(werr, errNothingSent) && !sent:
			return werr
		case errors.Is(werr, errNothingSent):
			return batchStopped(env, &result, i, "not_sent", werr.Error(), "")
		case werr == nil:
			sent = true
			return batchStopped(env, &result, i, "unverified", "it was sent, but the check after it did not pass", menv.NextStep)
		}
		sent = true
		var failure *verbFailure
		status, itemNext := "unverified", ""
		if errors.As(werr, &failure) {
			itemNext = failure.next
		}
		switch {
		case failure != nil && failure.code == "OUTCOME_UNKNOWN":
			status = "unknown"
		case failure != nil && failure.status == "failed":
			status = "failed"
		}
		return batchStopped(env, &result, i, status, werr.Error(), itemNext)
	}
	env.Result, env.Status, env.exit = result, "verified", verbExitOK
	env.Say = batchDoneSay(rp.lang, len(rp.members), noSnapshot && slices.ContainsFunc(rp.members, func(m batchMember) bool { return m.op() == "delete" }))
	env.NextStep = "Tell the user `say`. Every item went through (result.items). " + batchWaysBack
	return nil
}

// excusedByBatch: a member planned again right before its send is still
// the confirmed one, or differs only where it read, under the key of an
// earlier member of this apply, exactly the config that member wrote and
// verified (written: its draft's digest), or no longer reads it (that write
// took it out of this member's scan) while live, read once more, is still
// that digest. Their stored digests go back and the hash must then be the
// confirmed one, so the action, the tier, the reasons (from this re-plan),
// every entity, and every other config are still what the user confirmed.
// Any other content there, or a failed read, is drift.
func excusedByBatch(stored, fresh storedPlan, written map[string]string, live func(key string) (string, bool)) bool {
	if stillConfirmed(stored, fresh) {
		return true
	}
	if len(written) == 0 || fresh.ConfigAction == nil {
		return false
	}
	basis := asJSON(fresh.Basis)
	now, _ := basis["configs"].(map[string]any)
	was, _ := asJSON(stored.Basis)["configs"].(map[string]any)
	configs := map[string]any{}
	for key, digest := range now {
		want, own := written[key]
		switch {
		case !own:
			configs[key] = digest
		case digest != want:
			return false
		}
	}
	for key, digest := range was {
		want, own := written[key]
		if !own {
			continue
		}
		if _, read := now[key]; !read {
			if got, ok := live(key); !ok || got != want {
				return false
			}
		}
		configs[key] = digest
	}
	basis["configs"] = configs
	return "sha256:"+planHash(fresh.Kind, fresh.Server, *fresh.ConfigAction, fresh.Tier, fresh.Reasons, basis) == stored.Hash
}

// batchWaysBack: how each kind of item that went through is undone.
const batchWaysBack = "Only if they ask, the ways back: a deleted item through its result.items[].restore command, a changed one through ha-nova plan revert --target <its config_id>, a created one through the delete flow; each is a new plan."

// batchRefusal: a batch that expired or took three wrong codes leaves no
// item plans behind (plan batch used them up): every item is planned again.
func batchRefusal(isBatch bool, failure *verbFailure) *verbFailure {
	switch {
	case !isBatch:
	case failure.code == "PLAN_EXPIRED":
		failure.next = "Nothing was sent, and the item plans of this batch are gone: plan every item again, show the previews, then make a new batch."
	case failure.code == "PLAN_INVALIDATED":
		failure.next = "Stop and tell the user; only on their explicit request plan every item again, show the previews, and make a new batch."
	}
	return failure
}

// batchStopped records a batch that stopped at member i (0-based); every
// member before it was verified, every one after it was not attempted.
// itemNext is what that member's own result says to do.
func batchStopped(env *verbEnvelope, result *batchResult, i int, status, why, itemNext string) error {
	if itemNext != "" {
		itemNext = " For that item: " + itemNext
	}
	item := &result.Items[i]
	item.Status, item.Error = status, why
	result.Counts.Stopped = 1
	result.Counts.NotAttempted = len(result.Items) - i - 1
	env.Result, env.Status, env.exit = *result, "failed", verbExitUnverified
	env.Error = &verbError{Code: "BATCH_STOPPED", Message: fmt.Sprintf("item %d (%s) did not go through: %s; %d of %d went through, the rest were not sent", i+1, item.quoted, why, result.Counts.Verified, len(result.Items)),
		Next: "Tell the user `say`: which went through (result.items[].status verified), what stopped the next one (its error and verification), and that the rest were not sent." + itemNext +
			" Never apply this batch again; only if they still want the rest, plan it again with a new confirmation (two or more items: a new batch). " + batchWaysBack}
	switch status {
	case "unknown":
		env.Status, env.exit, env.OutcomeUnknown = "unverified", verbExitUnknown, true
		env.Error.Code = "OUTCOME_UNKNOWN"
		env.Error.Next = "Read whether that item took effect (ha-nova get with --config) and tell the user; never apply this batch again. The items after it were not sent." + itemNext + " " + batchWaysBack
	case "unverified":
		env.Status = "unverified"
	}
	env.Say = batchStopSay(result.lang, *result, i)
	return nil
}
