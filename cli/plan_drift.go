package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"strings"
)

// The plan basis beyond configs, and drift at apply (spec §2.1, I3): when a
// recompute still is the confirmed plan, and what a new preview says when
// it is not.

// basisAttributes: what a call on an entity of the domain acts on, beyond
// its data fields. An update installs, skips, or unskips one version, so a
// new version or a running install is drift; the progress, notes, picture,
// and title a Supervisor or integration refresh rewrites are not.
var basisAttributes = map[string][]string{"update": {"installed_version", "latest_version", "skipped_version", "in_progress"}}

// featureGated: the domains whose supported_features decide a refusal or
// what the call can do. Elsewhere the bits change with what a device is
// doing (a media player's app), and that never made a plan stale.
var featureGated = map[string]bool{"lock": true, "alarm_control_panel": true, "update": true}

// basisEntities: per acted entity pl, device_class, and (feature-gated
// domains only) supported_features.
func basisEntities(e *tierEngine) map[string]any {
	entities := map[string]any{}
	for _, id := range e.acted {
		st := e.states[id]
		entity := map[string]any{"pl": e.reg[id].pl, "device_class": st.attr("device_class")}
		if domain, _ := splitID(id); featureGated[domain] {
			entity["supported_features"] = st.attr("supported_features")
		}
		entities[id] = entity
	}
	return entities
}

// basisConsumers: what the consumer scan starts from for each frozen
// target — the automations and groups Home Assistant relates to it and the
// automations related to its device. evaluate reads them before any long
// scan, so a consumer added since the preview is drift even when a cut
// check could not read it.
func basisConsumers(e *tierEngine, frozen []string) map[string]any {
	out := map[string]any{}
	for _, id := range frozen {
		items := [][2]string{{"entity", id}}
		if device := e.reg[id].di; device != "" {
			items = append(items, [2]string{"device", device})
		}
		bound := map[string]any{}
		for _, item := range items {
			related, err := e.related(item[0], item[1]) // memoized: evaluate read it first
			switch {
			case err != nil:
				bound[item[0]] = "unread"
			case item[0] == "entity":
				bound["entity"] = map[string]any{"automation": related["automation"], "group": related["group"]}
			default:
				bound[item[0]] = related["automation"]
			}
		}
		out[id] = bound
	}
	return out
}

// checkGapCodes: reasons that mean the check could not see everything the
// plan sets off; the preview says one sentence for all of them, but a check
// the call's deadline cut says it ran out of time.
var checkGapCodes = map[string]bool{
	reasonTemplatedAction: true, reasonUnresolvedTarget: true, reasonStoredSelector: true, reasonBlueprint: true,
	reasonYAMLNoID: true, reasonUnreadConfig: true, reasonListener: true, reasonScanFailed: true, reasonLimitCut: true,
}

// sayCheckGap: the one sentence for every check gap but a check the call's
// deadline cut, which says it ran out of time (sayReason).
func sayCheckGap(lang string) string { return confirmT(lang, "unseen.all") }

// cutShort: a read failed or ran out of budget, so how far a run got
// depends on timing.
func cutShort(r tierReason) bool { return r.Code == reasonLimitCut || r.Code == reasonScanFailed }

// stillConfirmed: the recompute is the plan the user confirmed. A cut
// plan's replay is so only by sameAsConfirmed, even with its hash, which
// leaves out each reason's detail; one that diverged, and the plan computed
// after it, never are (recheck).
func stillConfirmed(stored, fresh storedPlan) bool {
	return fresh.Hash == stored.Hash && !fresh.replayed && !fresh.diverged || sameAsConfirmed(stored, fresh)
}

// sameAsConfirmed: a cut preview's replay (replayReads) whose hash differs
// only in the gaps where its reads had no answer is still the confirmed
// plan. It needs a preview that was cut short and said so, a plan file
// whose fields still make its hash, a replay that asked for nothing the
// preview did not, the same action and tier, the same reasons (a read with
// no answer counts as one cut, as the budget's LIMIT_CUT and SCAN_FAILED say
// the same sentence; every other one with its detail, count and depth cuts
// too, which the replay reached as the preview did), a replay that failed
// reads only where the preview failed them, and the same basis: before
// values, configs, acted entities, consumers. Only which reads the budget
// cut (basis "lost") may differ: the replay failed the preview's unanswered
// reads as it did, without a budget of its own running out.
func sameAsConfirmed(stored, fresh storedPlan) bool {
	if !fresh.replayed || fresh.diverged ||
		!slices.ContainsFunc(stored.Reasons, cutShort) ||
		!intact(stored) ||
		stored.Kind != fresh.Kind || stored.Server != fresh.Server || stored.Tier != fresh.Tier ||
		!jsonEqual(planActionOf(stored), planActionOf(fresh)) {
		return false
	}
	cutKey := func(r tierReason) string {
		if r.Code == reasonScanFailed || r.Code == reasonLimitCut && r.Detail == readBudgetDetail {
			return "cut"
		}
		return r.Code + "|" + r.EntityID + "|" + r.Via + "|" + r.Detail
	}
	keys := func(p storedPlan) map[string]bool {
		out := map[string]bool{}
		for _, r := range p.Reasons {
			out[cutKey(r)] = true
		}
		return out
	}
	was, now := asJSON(stored.Basis), asJSON(fresh.Basis)
	failed := func(basis map[string]any, reasons []tierReason) map[string]bool { // entity|via|what, as readFailed names them; the listener scan's never enter the basis
		out := map[string]bool{}
		lost, _ := basis["lost"].([]any)
		for _, read := range lost {
			out[fmt.Sprint(read)] = true
		}
		for _, r := range reasons {
			if r.Code == reasonScanFailed && r.Via != viaListenerScan {
				out[r.EntityID+"|"+r.Via+"|"+r.Detail] = true
			}
		}
		return out
	}
	preview := failed(was, stored.Reasons)
	for read := range failed(now, fresh.Reasons) {
		if !preview[read] { // a read with no answer the walk now reaches by another way
			return false
		}
	}
	delete(was, "lost")
	delete(now, "lost")
	return maps.Equal(keys(stored), keys(fresh)) && reflect.DeepEqual(was, now)
}

// saysOnly: every reason of fresh is one the preview said (sayKey).
func saysOnly(stored, fresh storedPlan) bool {
	shown := map[string]bool{}
	for _, r := range stored.Reasons {
		shown[sayKey(r)] = true
	}
	return !slices.ContainsFunc(fresh.Reasons, func(r tierReason) bool { return !shown[sayKey(r)] })
}

// recheck recomputes a stored plan. A plan whose preview was cut short is
// then replayed on the reads that preview made (replayReads), even when
// the recompute has its hash (the listener scan's reads never enter the
// basis), unless the recompute found a reason the preview did not say:
// that is drift as it is, wherever the recompute got to. A replay
// that diverged is drift too, whatever the hash, and the plan is computed
// once more as it is now (its new preview). A plan file without its reads
// (made before they were kept) is not replayed, so only its exact hash
// passes; one whose seal does not cover them (an edited batch member) is
// drift.
func recheck(s *relaySession, env *verbEnvelope, stored storedPlan, compute func(*verbEnvelope) (storedPlan, error)) (storedPlan, error) {
	start := *env
	fresh, err := compute(env)
	if err != nil || len(stored.Reads) == 0 || !slices.ContainsFunc(stored.Reasons, cutShort) || !saysOnly(stored, fresh) {
		return fresh, err
	}
	if !planSealed(stored, s.transport.Credential) { // an edited read list (a batch member): drift, whatever the hash
		fresh.diverged = true
		return fresh, nil
	}
	*env = start
	replayed, err := s.replayReads(stored.Reads, func() (storedPlan, error) { return compute(env) })
	if err == nil && !replayed.diverged {
		return replayed, nil
	}
	*env = start
	fresh, err = compute(env)
	fresh.diverged = true
	return fresh, err
}

// forgetFailed drops the remembered reads that failed, so the next pass
// asks Home Assistant again; every answer stays.
func (s *relaySession) forgetFailed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, m := range s.memo {
		select {
		case <-m.done:
			if m.err != nil {
				delete(s.memo, key)
			}
		default: // still in flight: its caller waits for it
		}
	}
}

// intact: the plan file's fields still make its hash (and so its id and
// code), so they show what the user was shown.
func intact(p storedPlan) bool {
	return p.Hash == "sha256:"+planHash(p.Kind, p.Server, planActionOf(p), p.Tier, p.Reasons, p.Basis)
}

// planActionOf is the action a plan's hash covers (sealPlan's action).
func planActionOf(p storedPlan) any {
	switch p.Kind {
	case "call":
		return p.Action
	case "rooms":
		return p.RoomsRequest
	case "raw":
		return p.RawRequests
	case "batch":
		return batchIdentities(p.BatchMembers)
	}
	return p.ConfigAction
}

// sayKey: a reason as the preview words it; every check gap is one key,
// since whether a read got no answer or the deadline came first is timing
// (sameAsConfirmed).
func sayKey(r tierReason) string {
	if checkGapCodes[r.Code] {
		return "gap"
	}
	return r.Code + "|" + r.EntityID + "|" + r.Via
}

// asJSON: a basis as its plan file holds it, so a fresh one compares with
// a stored one.
func asJSON(basis map[string]any) map[string]any {
	raw, _ := json.Marshal(basis)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func jsonEqual(a, b any) bool {
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ra) == string(rb)
}

// driftSay opens a drifted plan's new preview: the first thing that
// changed, in plain words, in the new plan's language (a typed plan's is
// the sealed one it was made in); one space goes before the preview.
func driftSay(e *tierEngine, env *verbEnvelope, stored, fresh storedPlan) string {
	lang := textLang(fresh)
	if !intact(stored) { // an edited file never words the new preview
		return sayChanged(lang)
	}
	old, now := asJSON(stored.Basis), asJSON(fresh.Basis)
	was, _ := old["before"].(map[string]any)
	is, _ := now["before"].(map[string]any)
	for _, id := range sortedKeys(was) {
		wasFields, _ := was[id].(map[string]any)
		isFields, both := is[id].(map[string]any)
		for _, field := range sortedKeys(wasFields) {
			if after, present := isFields[field]; both && present && !reflect.DeepEqual(wasFields[field], after) {
				name := driftField(field)
				if field == "state" {
					name = confirmT(lang, "drift.state")
				}
				value := func(v any) string { // in the plan's language (valueText)
					if text, ok := v.(string); ok && text != "" {
						return fresh.valueText(id, field, text)
					}
					return driftValue(lang, field, v)
				}
				return confirmT(lang, "drift.field", "name", sayText(e.sayName(env, id), true), "field", name, "was", value(wasFields[field]), "now", value(after))
			}
		}
	}
	switch {
	case !jsonEqual(planActionOf(stored), planActionOf(fresh)):
		return confirmT(lang, "drift.targets")
	case !reflect.DeepEqual(old["current"], now["current"]):
		return confirmT(lang, "drift.current")
	case !sameWhereBoth(old["configs"], now["configs"]):
		return confirmT(lang, "drift.configs")
	case !sameWhereBoth(old["entities"], now["entities"]):
		return confirmT(lang, "drift.entities")
	case stored.Tier != fresh.Tier || !jsonEqual(stored.Reasons, fresh.Reasons) || !reflect.DeepEqual(old, now):
		return confirmT(lang, "drift.checked")
	}
	return sayChanged(lang)
}

// sameWhereBoth: every key both maps hold has the same value.
func sameWhereBoth(a, b any) bool {
	am, _ := a.(map[string]any)
	bm, _ := b.(map[string]any)
	for key, value := range am {
		if other, both := bm[key]; both && !reflect.DeepEqual(value, other) {
			return false
		}
	}
	return true
}

// driftField names a basis field; brightness_pct holds HA's brightness.
func driftField(field string) string {
	return strings.ReplaceAll(deltaAttribute(field), "_", " ")
}

func driftValue(lang, field string, v any) string {
	n, isNumber := v.(float64)
	switch t := v.(type) {
	case nil:
		return confirmT(lang, "drift.none")
	case bool:
		return map[bool]string{true: confirmT(lang, "drift.yes"), false: confirmT(lang, "drift.no")}[t]
	case string:
		if t == "" {
			return confirmT(lang, "drift.none")
		}
	}
	if isNumber && deltaAttribute(field) == "brightness" {
		return fmt.Sprintf("%d%%", int(math.Round(n/255*100)))
	}
	return sayValue(deltaValue(v)) // a long or odd value stays quoted
}
