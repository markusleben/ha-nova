package main

import "strings"

// The reason clauses of a confirmation (confirm_say.go): one plain clause
// per tier reason, in the plan's language (confirmT).

// sayReason is one plain clause per reason code; typed reports whether the
// code puts a plan on the code tier. Codes that only refuse never reach a
// plan and have no clause (TestConfirmationSayCoversEveryPlanReason).
func sayReason(lang string, e *tierEngine, env *verbEnvelope, plan storedPlan, r tierReason) (string, bool) {
	name := ""
	if r.EntityID != "" {
		name = sayText(e.sayName(env, r.EntityID), true)
	}
	via := sayVia(lang, e, env, plan, r.Via)
	if phrase, typed, ok := sayActionPhrase(lang, e, env, r); ok {
		if via != "" {
			object := phrase.noun
			if name != "" && phrase.named {
				object = name
			} else if name != "" {
				object = confirmT(lang, "reason.name_noun", "name", name, "noun", phrase.noun) // the name never hides what it is
			}
			return confirmT(lang, "reason.phrase_via", "phrase", sayPhraseText(lang, phrase.act, object), "via", via), typed
		}
		return confirmT(lang, "reason.phrase", "phrase", sayPhraseText(lang, phrase.act, phrase.noun)), typed
	}
	subject := confirmT(lang, "reason.this")
	if name != "" {
		subject = name
	}
	through := func(text string) string {
		if via == "" {
			return text
		}
		return confirmT(lang, "reason.through", "text", text, "via", via)
	}
	if sayUnseenCodes[r.Code] {
		domain, _ := splitID(r.EntityID)
		return sayUnseen(lang, name, via, sayPlainDomains[domain]), true
	}
	if checkGapCodes[r.Code] {
		return sayCheckGap(lang), true
	}
	switch r.Code {
	case reasonUserService:
		return confirmT(lang, "reason.user_service"), true
	case reasonConfirmWatch:
		return confirmT(lang, "reason.confirm_watch"), true
	case reasonSceneAccess:
		return confirmT(lang, "reason.scene_access", "name", name), true
	case reasonReload:
		return confirmT(lang, "reason.reload"), true
	case reasonDeviceAccess:
		return through(confirmT(lang, "reason.device_access", "subject", subject)), true
	case reasonDeviceAccessSibling:
		return through(sayDeviceSibling(lang, e, env, r, subject)), true
	case reasonSwitchAsX:
		if e.reg[r.EntityID].di == "" { // matched only by having no device either
			return through(confirmT(lang, "reason.switch_as_x_maybe", "subject", subject)), true
		}
		return through(confirmT(lang, "reason.switch_as_x", "subject", subject)), true
	case reasonOwnedTypedMember:
		if directOwnedCall(env, r) {
			return through(confirmT(lang, "reason.owned", "action", sayOwnedAction(lang, env, r))), true
		}
		return through(confirmT(lang, "reason.owned_also", "action", sayOwnedAction(lang, env, r))), true
	case reasonConfigDelete:
		return confirmT(lang, "reason.config_delete"), true
	case reasonOwnedMember:
		if directOwnedCall(env, r) { // its preview says it all
			return "", false
		}
		return through(confirmT(lang, "reason.owned_effect", "effect", sayOwnedEffect(lang, env, r))), false
	case reasonExpanded, reasonBatch:
		if n := len(plan.Action.EntityIDs); n != 1 {
			return confirmT(lang, "reason.many", "count", n), false
		}
		// A batch that named one entity several times says nothing; a
		// sibling of an access device carries DEVICE_ACCESS_SIBLING itself.
		switch r.Via {
		case "":
			return "", false
		case "area":
			return confirmT(lang, "reason.picked_area"), false
		case "device":
			return confirmT(lang, "reason.picked_device"), false
		case "group":
			return confirmT(lang, "reason.picked_group"), false
		}
		panic("confirm say: unknown expansion " + r.Via)
	case reasonStoredMembers:
		var names []string
		for _, m := range env.Members {
			if m.EntityID != "" {
				names = appendUnique(names, sayText(e.sayName(env, m.EntityID), true))
			}
		}
		if len(names) == 0 {
			return confirmT(lang, "reason.own_steps"), false
		}
		return confirmT(lang, "reason.also_controls", "names", sayList(lang, names)), false
	case reasonNoRegistryRow:
		return confirmT(lang, "reason.no_registry", "name", name), false
	case reasonSceneUnread:
		return confirmT(lang, "reason.scene_unread", "name", name), false
	case reasonListenerUnexpl, reasonRunListenerUnexpl, reasonManager:
		return confirmT(lang, "reason.listener"), false
	case reasonIntegrationSvc:
		return confirmT(lang, "reason.integration_service"), false
	case reasonServiceNotFound:
		return confirmT(lang, "reason.service_not_found", "service", sayText(r.Detail, true)), false
	case reasonRawTyped:
		return r.Detail, true
	case reasonHACSInstall:
		return confirmT(lang, "reason.hacs_install"), true
	case reasonConfigWrite, reasonConfigRevert, reasonConfigRestore, reasonAreaAssign, reasonRawWrite:
		return "", false
	}
	return confirmT(lang, "reason.unknown"), r.Code != "" // a code without a sentence never reads as harmless
}

// sayPhrase: what an access or disruptive action does (act, sayPhraseText)
// to what (noun); named = the act alone says what the named entity is
// (unlocks, disarms), otherwise a member keeps the noun beside its name.
type sayPhrase struct {
	act, noun string
	named     bool
}

// sayActionPhrase words the access and disruptive codes after the action
// that raised them (a member's own action, or the direct service).
func sayActionPhrase(lang string, e *tierEngine, env *verbEnvelope, r tierReason) (sayPhrase, bool, bool) {
	// Several steps can act on one entity through one item (unlock, then
	// lock): the riskiest step names the clause, never the last one.
	// Only steps that raise this very code word it; another step through the
	// same item (disarm vs. trigger) never lends its action.
	action, data, rank := "", map[string]any(nil), -1
	for _, own := range []bool{true, false} {
		for _, m := range env.Members {
			if m.EntityID != r.EntityID || m.Via != r.Via {
				continue
			}
			if _, code := classifyOnEntity(m.EntityID, m.Action, m.data, e.states[m.EntityID]); own && code != r.Code {
				continue
			}
			if mr := sayActionRank(m); mr > rank {
				action, data, rank = m.Action, m.data, mr
			}
		}
		if action != "" {
			break
		}
	}
	if action == "" && env.Action != nil {
		entityDomain, _ := splitID(r.EntityID)
		action, data = effectiveService(env.Action.Service, entityDomain), env.Action.Data
	}
	_, name := splitID(action)
	switch r.Code {
	case reasonAccessLock:
		act := map[string]string{"unlock": "unlocks", "open": "opens"}[name]
		if act == "" {
			return sayPhrase{act: "acts on", noun: confirmT(lang, "noun.door_lock")}, true, true
		}
		return sayPhrase{act: act, noun: confirmT(lang, "noun.door"), named: true}, true, true
	case reasonAccessAlarm:
		if name == "alarm_disarm" {
			return sayPhrase{act: "disarms", noun: confirmT(lang, "noun.the_alarm"), named: true}, true, true
		}
		return sayPhrase{act: "acts on", noun: confirmT(lang, "noun.the_alarm")}, true, true
	case reasonAccessCover, reasonCoverClassUnknown:
		return sayPhrase{act: sayCoverVerb(name, data, e.states[r.EntityID]), noun: sayCoverNoun(lang, e.states[r.EntityID])}, true, true
	case reasonDestructiveButton:
		return sayPhrase{act: "presses", noun: confirmT(lang, "noun.wipe_button")}, true, true
	case reasonDisruptive:
		switch {
		case action == "homeassistant.restart":
			return sayPhrase{act: "restarts", noun: "Home Assistant"}, false, true
		case action == "homeassistant.stop":
			return sayPhrase{act: "stops", noun: "Home Assistant"}, false, true
		case strings.HasPrefix(action, "siren."):
			act := "sounds"
			if name == "toggle" {
				act = map[string]string{"on": "silences", "off": "sounds"}[e.states[r.EntityID].state()]
				if act == "" {
					act = "sounds or silences"
				}
			}
			return sayPhrase{act: act, noun: confirmT(lang, "noun.siren")}, false, true
		case name == "alarm_trigger":
			return sayPhrase{act: "sets off", noun: confirmT(lang, "noun.the_alarm")}, false, true
		}
		return sayPhrase{act: "presses", noun: sayButtonNoun(lang, r.EntityID, e.states[r.EntityID])}, false, true
	}
	return sayPhrase{}, false, false
}

func sayActionRank(m tierMember) int {
	_, name := splitID(m.Action)
	switch {
	case name == "unlock" || name == "open" || name == "alarm_disarm" || coverCanOpen(name, m.data):
		return 2
	case m.Class != tierOrdinary.String():
		return 1
	}
	return 0
}

// sayVia names the item a member reason runs through ("the automation
// “Guest mode”"); "" for a direct target or an area/device expansion.
func sayVia(lang string, e *tierEngine, env *verbEnvelope, plan storedPlan, via string) string {
	root := firstSegment(via)
	switch {
	case root == "draft" && plan.ConfigAction != nil:
		switch plan.ConfigAction.Domain {
		case "automation":
			return confirmT(lang, "via.this_automation")
		case "script":
			return confirmT(lang, "via.this_script")
		case "dashboard":
			return confirmT(lang, "via.this_dashboard")
		}
		panic("confirm say: unknown config domain " + plan.ConfigAction.Domain)
	case root == "scene.apply":
		return confirmT(lang, "via.scene_applies")
	case !entityIDPattern.MatchString(root):
		return ""
	}
	domain, _ := splitID(root)
	name := sayText(e.sayName(env, root), true)
	switch domain {
	case "automation":
		return confirmT(lang, "via.automation", "name", name)
	case "script":
		return confirmT(lang, "via.script", "name", name)
	case "scene":
		return confirmT(lang, "via.scene", "name", name)
	}
	return name
}

func firstSegment(path string) string {
	root, _, _ := strings.Cut(path, "/")
	return root
}

// sayDeviceSibling: "“Gate relay” is on the same device as “Gate” (a gate):
// switching it on may open it". Detail names the access entity.
func sayDeviceSibling(lang string, e *tierEngine, env *verbEnvelope, r tierReason, subject string) string {
	action := ""
	for _, m := range env.Members {
		if m.EntityID == r.EntityID && m.Via == r.Via {
			action = m.Action
		}
	}
	if action == "" && env.Action != nil {
		domain, _ := splitID(r.EntityID)
		action = effectiveService(env.Action.Service, domain)
	}
	_, name := splitID(action)
	found := r.Detail
	domain, _ := splitID(found)
	other := confirmT(lang, "noun.lock_alarm_door")
	switch domain {
	case "lock":
		other = confirmT(lang, "noun.lock")
	case "alarm_control_panel":
		other = confirmT(lang, "noun.an_alarm")
	case "cover":
		other = sayCoverNoun(lang, e.states[found])
	}
	effect := sayDeviceEffect(lang, map[string]string{"press": "pressing", "turn_on": "switching", "toggle": "switching"}[name], domain)
	if !entityIDPattern.MatchString(found) {
		return confirmT(lang, "reason.sibling", "subject", subject, "other", other, "effect", effect)
	}
	return confirmT(lang, "reason.sibling_named", "subject", subject, "name", sayText(e.sayName(env, found), true), "other", other, "effect", effect)
}

// sayDeviceEffect: what acting on a sibling may do to the access entity
// on its device (doing: "pressing", "switching", else opening).
func sayDeviceEffect(lang, doing, domain string) string {
	switch {
	case doing == "pressing" && domain == "lock":
		return confirmT(lang, "sibling.pressing_unlock")
	case doing == "pressing" && domain == "alarm_control_panel":
		return confirmT(lang, "sibling.pressing_disarm")
	case doing == "pressing":
		return confirmT(lang, "sibling.pressing_open")
	case doing == "switching" && domain == "lock":
		return confirmT(lang, "sibling.switching_unlock")
	case doing == "switching" && domain == "alarm_control_panel":
		return confirmT(lang, "sibling.switching_disarm")
	case doing == "switching":
		return confirmT(lang, "sibling.switching_open")
	case domain == "lock":
		return confirmT(lang, "sibling.opening_unlock")
	case domain == "alarm_control_panel":
		return confirmT(lang, "sibling.opening_disarm")
	}
	return confirmT(lang, "sibling.opening_open")
}
