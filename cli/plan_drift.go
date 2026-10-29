package main

import (
	"encoding/json"
	"fmt"
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
// plan sets off; the preview says one sentence for all of them.
var checkGapCodes = map[string]bool{
	reasonTemplatedAction: true, reasonUnresolvedTarget: true, reasonStoredSelector: true, reasonBlueprint: true,
	reasonYAMLNoID: true, reasonUnreadConfig: true, reasonListener: true, reasonScanFailed: true, reasonLimitCut: true,
}

const sayCheckGap = "HA NOVA can't check everything this would set off"

// cutShort: a read failed or ran out of budget, so how far a run got
// depends on timing.
func cutShort(r tierReason) bool { return r.Code == reasonLimitCut || r.Code == reasonScanFailed }

// stillConfirmed: the recompute is the plan the user confirmed.
func stillConfirmed(stored, fresh storedPlan) bool {
	return fresh.Hash == stored.Hash || sameAsConfirmed(stored, fresh)
}

// sameAsConfirmed: a recompute whose hash differs only in the gaps a check
// cut short ran into (a 30 s budget over a slow link, a timed-out read) is
// still the confirmed plan. It needs a preview that was cut short and said
// so, a plan file whose fields still make its hash, the same action, tier,
// and basis (before values, acted entities, every config the walk read),
// and no reason the preview did not show. Timing-made gaps (the budget cut,
// the listener scan's per-automation gaps) count as the one sentence they
// all say; a failed read in the walk keeps its own entity. The listener
// scan's own reads never enter the basis (tierEngine.census), so how far it
// got cannot make a plan stale.
func sameAsConfirmed(stored, fresh storedPlan) bool {
	if !slices.ContainsFunc(stored.Reasons, cutShort) ||
		!intact(stored) ||
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
	return reflect.DeepEqual(asJSON(stored.Basis), asJSON(fresh.Basis))
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

// sayKey: a reason as the preview words it.
func sayKey(r tierReason) string {
	if checkGapCodes[r.Code] && (r.Via == viaListenerScan || r.Code == reasonLimitCut && r.EntityID == "" && r.Via == "") {
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
// changed, in plain words.
func driftSay(e *tierEngine, env *verbEnvelope, stored, fresh storedPlan) string {
	if !intact(stored) { // an edited file never words the new preview
		return sayChanged
	}
	old, now := asJSON(stored.Basis), asJSON(fresh.Basis)
	was, _ := old["before"].(map[string]any)
	is, _ := now["before"].(map[string]any)
	for _, id := range sortedKeys(was) {
		wasFields, _ := was[id].(map[string]any)
		isFields, both := is[id].(map[string]any)
		for _, field := range sortedKeys(wasFields) {
			if after, present := isFields[field]; both && present && !reflect.DeepEqual(wasFields[field], after) {
				return fmt.Sprintf("Since the last preview, %s changed: %s was %s, now %s. ", sayText(e.sayName(env, id), true),
					driftField(field), driftValue(field, wasFields[field]), driftValue(field, after))
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
		return "Since the last preview, something this controls changed. "
	case stored.Tier != fresh.Tier || !jsonEqual(stored.Reasons, fresh.Reasons) || !reflect.DeepEqual(old, now):
		return "HA NOVA checked again, and what this sets off looks different now. "
	}
	return sayChanged
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

func driftValue(field string, v any) string {
	n, isNumber := v.(float64)
	switch t := v.(type) {
	case nil:
		return "none"
	case bool:
		return map[bool]string{true: "yes", false: "no"}[t]
	case string:
		if t == "" {
			return "none"
		}
	}
	if isNumber && deltaAttribute(field) == "brightness" {
		return fmt.Sprintf("%d%%", int(math.Round(n/255*100)))
	}
	return sayValue(deltaValue(v)) // a long or odd value stays quoted
}
