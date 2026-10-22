package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Drift at apply (spec §2.1, I3): when a recompute still is the confirmed
// plan, and what the new preview says when it is not.

// checkGapCodes: reasons that mean the check could not see everything the
// plan sets off. The preview says one sentence for all of them, and how many
// a run finds depends on how far it got within its read budget.
var checkGapCodes = map[string]bool{
	reasonTemplatedAction: true, reasonUnresolvedTarget: true, reasonStoredSelector: true, reasonBlueprint: true,
	reasonYAMLNoID: true, reasonUnreadConfig: true, reasonListener: true, reasonScanFailed: true, reasonLimitCut: true,
}

const sayCheckGap = "HA NOVA can't check everything this would set off"

// planActionOf is the action a plan's hash covers (sealPlan's action).
func planActionOf(p storedPlan) any {
	switch p.Kind {
	case "call":
		return p.Action
	case "rooms":
		return p.RoomsRequest
	}
	return p.ConfigAction
}

// sameAsConfirmed: a recompute whose hash differs only in how far an
// incomplete check got is still the plan the user confirmed. That needs a
// preview that already said HA NOVA can't check everything (a gap reason),
// a plan file whose fields still make its hash, and then the same action,
// tier, and before values, no reason the preview did not show (gap reasons
// count as the one sentence they say), and every config and entity both
// runs read unchanged. A run cut by the read budget reads a different set
// each time, so without this an unchanged plan could never be confirmed.
func sameAsConfirmed(stored, fresh storedPlan) bool {
	if !slices.ContainsFunc(stored.Reasons, func(r tierReason) bool { return checkGapCodes[r.Code] }) ||
		stored.Hash != "sha256:"+planHash(stored.Kind, stored.Server, planActionOf(stored), stored.Tier, stored.Reasons, stored.Basis) ||
		stored.Kind != fresh.Kind || stored.Server != fresh.Server || stored.Tier != fresh.Tier ||
		!jsonEqual(planActionOf(stored), planActionOf(fresh)) {
		return false
	}
	shown := map[string]bool{}
	for _, r := range stored.Reasons {
		shown[sayKey(r)] = true
	}
	for _, r := range fresh.Reasons {
		if !shown[sayKey(r)] {
			return false
		}
	}
	old, now := asJSON(stored.Basis), asJSON(fresh.Basis)
	if len(old) != len(now) {
		return false
	}
	for key, value := range old {
		if _, present := now[key]; !present {
			return false
		}
		switch key {
		case "configs", "entities": // read as far as each run got
			if !sameWhereBoth(value, now[key]) {
				return false
			}
		default:
			if !reflect.DeepEqual(value, now[key]) {
				return false
			}
		}
	}
	return true
}

// sayKey: a reason as the preview words it.
func sayKey(r tierReason) string {
	if checkGapCodes[r.Code] {
		return "gap"
	}
	return r.Code + "|" + r.EntityID + "|" + r.Via
}

// sameWhereBoth: every key both maps hold has the same value.
func sameWhereBoth(a, b any) bool {
	am, aIsMap := a.(map[string]any)
	bm, bIsMap := b.(map[string]any)
	if !aIsMap || !bIsMap {
		return reflect.DeepEqual(a, b)
	}
	for key, value := range am {
		if other, both := bm[key]; both && !reflect.DeepEqual(value, other) {
			return false
		}
	}
	return true
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
// changed, in plain words.
func driftSay(e *tierEngine, env *verbEnvelope, stored, fresh storedPlan) string {
	old, now := asJSON(stored.Basis), asJSON(fresh.Basis)
	was, _ := old["before"].(map[string]any)
	is, _ := now["before"].(map[string]any)
	for _, id := range sortedKeys(was) {
		wasFields, _ := was[id].(map[string]any)
		isFields, both := is[id].(map[string]any)
		for _, field := range sortedKeys(wasFields) {
			if after, present := isFields[field]; both && present && !reflect.DeepEqual(wasFields[field], after) {
				return fmt.Sprintf("Since the last preview, %s changed: %s was %s, now %s. ", sayText(e.sayName(env, id), true),
					strings.ReplaceAll(field, "_", " "), driftValue(wasFields[field]), driftValue(after))
			}
		}
	}
	switch {
	case !jsonEqual(planActionOf(stored), planActionOf(fresh)):
		return "Since the last preview, what this acts on changed. "
	case !reflect.DeepEqual(old["current"], now["current"]):
		return "Since the last preview, it was changed in Home Assistant. "
	case !sameWhereBoth(old["configs"], now["configs"]):
		return "Since the last preview, an automation or script this sets off was changed. "
	case !sameWhereBoth(old["entities"], now["entities"]):
		return "Since the last preview, a device this controls changed. "
	case stored.Tier != fresh.Tier || !jsonEqual(stored.Reasons, fresh.Reasons) || !reflect.DeepEqual(old["configs"], now["configs"]):
		return "Since the last preview, what this sets off changed. "
	}
	return sayChanged
}

func driftValue(v any) string {
	switch t := v.(type) {
	case nil:
		return "empty"
	case bool:
		return map[bool]string{true: "yes", false: "no"}[t]
	}
	return sayText(deltaValue(v), false)
}
