package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// Wording and defanging helpers of the confirmation text (confirm_say.go),
// in the plan's language (confirmT).

// sayWithData: the action and the values a call sets, once, when no
// single-target delta shows them ("turn on “Hall” with brightness 50").
func sayWithData(lang, action string, data map[string]any) string {
	var parts, flags []string
	for _, key := range sortedKeys(data) {
		if flag, ok := sayFlag(lang, key, data[key]); ok {
			flags = append(flags, ", "+flag)
			continue
		}
		parts = append(parts, confirmT(lang, "say.data_item", "field", sayValue(key), "value", sayValue(deltaValue(data[key]))))
	}
	if len(parts) > 0 {
		action = confirmT(lang, "say.with_data", "action", action, "data", strings.Join(parts, ", "))
	}
	return action + strings.Join(flags, "")
}

// sayFlag: a true/false field in plain words ("with a backup first"), never
// "backup set to true"; ok is false for any value that isn't a real bool
// (a text set to "true" stays a text).
func sayFlag(lang, field string, value any) (string, bool) {
	on, ok := value.(bool)
	words := strings.ReplaceAll(sayText(field, false), "_", " ")
	is, isField := strings.CutPrefix(words, "is ") // is_volume_muted: "volume muted: yes"
	switch {
	case !ok:
		return "", false
	case field == "backup" && on:
		return confirmT(lang, "say.flag.backup"), true
	case field == "backup":
		return confirmT(lang, "say.flag.no_backup"), true
	case isField && on:
		return confirmT(lang, "say.flag.is_yes", "field", is), true
	case isField:
		return confirmT(lang, "say.flag.is_no", "field", is), true
	case on:
		return confirmT(lang, "say.flag.on", "field", words), true
	}
	return confirmT(lang, "say.flag.off", "field", words), true
}

// sayMore counts the reasons left out (typed: those that need the user's
// confirmation).
func sayMore(lang string, n int, typed bool) string {
	if typed {
		return confirmT(lang, "say.more_typed", "count", n)
	}
	return confirmT(lang, "say.more", "count", n)
}

// sayAsk: a typed plan's phone question — the goal, and the first reason
// it needs a confirmation that the title doesn't already say (an automation
// that also unlocks, a device sibling, an unseen effect), all of them.
// Said: the one target's own action ("This unlocks a door"), and then also
// that HA NOVA can't see what that target does; never a reason via another item.
func sayAsk(e *tierEngine, env *verbEnvelope, plan storedPlan, subject string) actionGoal {
	if plan.Kind == "raw" {
		return rawAsk(e, env, plan)
	}
	lang := textLang(plan)
	goal := sayGoal(e, env, plan, subject)
	domain, _ := splitID(plan.Action.Service)
	// phrased: the title already says this effect — only "Unlock"/"Open" a
	// lock and "Disarm" the alarm; a wiping button or a garage cover keeps its line.
	own := func(r tierReason) (mine, phrased bool) {
		phrase, _, ok := sayActionPhrase(lang, e, env, r)
		target, _ := splitID(r.EntityID)
		mine = r.Via == "" && slices.Equal(plan.Action.EntityIDs, []string{r.EntityID}) && goal.ownButton(lang) != ""
		return mine, ok && domain == target && (r.Code == reasonAccessLock || r.Code == reasonAccessAlarm) && phrase.named
	}
	named := slices.ContainsFunc(env.Reasons, func(r tierReason) bool { mine, phrased := own(r); return mine && phrased })
	var why []string // every distinct risk the title doesn't say
	for _, r := range env.Reasons {
		text, typed := sayReason(lang, e, env, plan, r)
		mine, phrased := own(r)
		if typed && text != "" && !(mine && (phrased || named && sayUnseenCodes[r.Code])) && !slices.Contains(why, text) {
			why = append(why, text)
		}
	}
	goal.why = strings.Join(why, ". ")
	return goal
}

// sayCoverVerb: the phrase (sayPhraseText) of a cover action; only a
// direction the call proves is named, and a position change of unknown
// direction stays "sets the position of".
func sayCoverVerb(name string, data map[string]any, st *entityState) string {
	switch name {
	case "open_cover":
		return "opens"
	case "toggle":
		return "opens or closes"
	case "set_cover_position":
		pos, isNumber := data["position"].(float64)
		current, known := st.attr("current_position").(float64)
		switch {
		case isNumber && pos == 0:
			return "closes"
		case isNumber && known && pos > current:
			return "opens"
		}
		return "sets the position of"
	}
	return "acts on"
}

// sayPhraseText: what an action does to object, as the reason clauses say
// it ("unlocks “Front Door”"); act is the English phrase, used as an id.
func sayPhraseText(lang, act, object string) string {
	switch act {
	case "unlocks":
		return confirmT(lang, "phrase.unlocks", "object", object)
	case "opens":
		return confirmT(lang, "phrase.opens", "object", object)
	case "acts on":
		return confirmT(lang, "phrase.acts_on", "object", object)
	case "disarms":
		return confirmT(lang, "phrase.disarms", "object", object)
	case "closes":
		return confirmT(lang, "phrase.closes", "object", object)
	case "opens or closes":
		return confirmT(lang, "phrase.opens_or_closes", "object", object)
	case "sets the position of":
		return confirmT(lang, "phrase.sets_position", "object", object)
	case "presses":
		return confirmT(lang, "phrase.presses", "object", object)
	case "restarts":
		return confirmT(lang, "phrase.restarts", "object", object)
	case "stops":
		return confirmT(lang, "phrase.stops", "object", object)
	case "sounds":
		return confirmT(lang, "phrase.sounds", "object", object)
	case "silences":
		return confirmT(lang, "phrase.silences", "object", object)
	case "sounds or silences":
		return confirmT(lang, "phrase.sounds_or_silences", "object", object)
	case "sets off":
		return confirmT(lang, "phrase.sets_off", "object", object)
	}
	panic("confirm say: unknown phrase " + act)
}

// sayCoverNoun: what a cover is, as far as its device class tells.
func sayCoverNoun(lang string, st *entityState) string {
	switch st.str("device_class") {
	case "garage":
		return confirmT(lang, "noun.garage")
	case "gate":
		return confirmT(lang, "noun.gate")
	case "door":
		return confirmT(lang, "noun.door")
	}
	return confirmT(lang, "noun.cover_maybe")
}

// sayButtonNoun names what a disruptive button does only as far as its
// device class or name proves; a plain "reset" stays unspecific.
func sayButtonNoun(lang, entityID string, st *entityState) string {
	tokens := " " + strings.Join(strings.FieldsFunc(strings.ToLower(entityID+" "+st.str("friendly_name")), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ") + " "
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(tokens, " "+w+" ") {
				return true
			}
		}
		return false
	}
	switch dc := st.str("device_class"); {
	case dc == "restart" || has("restart", "reboot"):
		return confirmT(lang, "noun.restart_button")
	case dc == "update":
		return confirmT(lang, "noun.update_button")
	case has("shutdown", "poweroff", "shut down", "power off"):
		return confirmT(lang, "noun.off_button")
	}
	return confirmT(lang, "noun.reset_button")
}

// sayPlainValue: a short token (state, number, unit) needs no quotes.
var sayPlainValue = regexp.MustCompile(`^[\p{L}\p{N}_%.+-]{1,24}$`)

func sayValue(s string) string {
	if t := sayText(s, false); t != "" && sayPlainValue.MatchString(t) {
		return t
	}
	if sayText(s, false) == "" {
		return "“”"
	}
	return sayText(s, true)
}

// sayText defangs a Home Assistant string for the confirmation text: no
// control characters or line breaks, no backticks or quote marks (a name
// cannot close its own quotes), at most 60 characters; quoted when asked.
func sayText(s string, quote bool) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Cf, r): // zero-width and bidi controls
			return -1
		case unicode.IsControl(r) || unicode.IsSpace(r):
			return ' '
		case unicode.In(r, unicode.Pi, unicode.Pf) || strings.ContainsRune("`\"″＂〝〞〟", r):
			return '\''
		case r == '[' || r == '<':
			return '('
		case r == ']' || r == '>':
			return ')'
		case strings.ContainsRune("*|#~", r): // no Markdown emphasis, tables, or headings
			return ' '
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, "://", ": //") // no autolinks
	s = strings.Join(strings.Fields(s), " ")
	if runes := []rune(s); len(runes) > 60 {
		s = string(runes[:59]) + "…"
	}
	if s == "" || !quote {
		return s
	}
	return "“" + s + "”"
}

func appendUnique(list []string, s string) []string {
	for _, have := range list {
		if have == s {
			return list
		}
	}
	return append(list, s)
}

// sayOwnedAction words a Supervisor or raw-MQTT step after what it does.
func sayOwnedAction(lang string, env *verbEnvelope, r tierReason) string {
	action := ""
	if env.Action != nil && r.Via == "" {
		action = env.Action.Service
	}
	for _, m := range env.Members {
		if m.EntityID == r.EntityID && m.Via == r.Via {
			action = m.Action
		}
	}
	_, name := splitID(action)
	switch {
	case action == "mqtt.publish":
		return confirmT(lang, "owned.mqtt")
	case action == "conversation.process":
		return confirmT(lang, "owned.assist")
	case action == "recorder.purge" || action == "recorder.purge_entities":
		return confirmT(lang, "owned.purge")
	case name == "host_shutdown":
		return confirmT(lang, "owned.host_shutdown")
	case name == "host_reboot":
		return confirmT(lang, "owned.host_reboot")
	case strings.HasPrefix(name, "restore_"):
		return confirmT(lang, "owned.restore")
	case strings.HasPrefix(name, "addon_"):
		return confirmT(lang, "owned.app")
	}
	return confirmT(lang, "owned.supervisor")
}

// sayOwnedEffect words a step another skill owns after what it does
// ("sends a message"); the reason names the step's item.
func sayOwnedEffect(lang string, env *verbEnvelope, r tierReason) string {
	action := ""
	for _, m := range env.Members {
		if m.EntityID == r.EntityID && m.Via == r.Via {
			action = m.Action
		}
	}
	domain, _ := splitID(action)
	switch { // neutral per domain: one word fits every service in it
	case domain == "notify": // a phone command rides on a notify call too
		return confirmT(lang, "effect.notify")
	case domain == "persistent_notification":
		return confirmT(lang, "effect.persistent_notification")
	case action == "tts.clear_cache":
		return confirmT(lang, "effect.tts_clear")
	case domain == "tts":
		return confirmT(lang, "effect.tts")
	case domain == "media_player":
		return confirmT(lang, "effect.media")
	case domain == "camera":
		return confirmT(lang, "effect.camera")
	case domain == "calendar":
		return confirmT(lang, "effect.calendar")
	case domain == "todo":
		return confirmT(lang, "effect.todo")
	case domain == "backup":
		return confirmT(lang, "effect.backup")
	case domain == "update":
		return confirmT(lang, "effect.update")
	case action == "logger.set_level":
		return confirmT(lang, "effect.logger")
	}
	return confirmT(lang, "effect.other")
}

// directOwnedCall: the reason is the call itself, made from its owning
// skill (--skill), not a member it runs.
func directOwnedCall(env *verbEnvelope, r tierReason) bool {
	if r.Via != "" || env.Action == nil {
		return false
	}
	domain, _ := splitID(env.Action.Service)
	return ownerSkill(env.Action.Service) != "" || domain == "hassio"
}

// sayAgo: how long ago, relative ("12 minutes ago"); never a clock time,
// which an agent shows in UTC (expires_at stays RFC3339 UTC for machines).
func sayAgo(d time.Duration) string {
	switch m := int(d.Minutes()); {
	case m < 1:
		return "just now"
	case m == 1:
		return "1 minute ago"
	case m < 120:
		return fmt.Sprintf("%d minutes ago", m)
	default:
		return fmt.Sprintf("%d hours ago", m/60)
	}
}

var sayUnseenCodes = map[string]bool{reasonUnregisteredActuator: true, reasonTemplateAction: true, reasonUserButton: true, reasonUnreadCommand: true}

// sayPlainDomains: what these do is plain from the kind (a lock locks, a
// garage door opens); what HA NOVA can't check is how it does it.
var sayPlainDomains = map[string]bool{"lock": true, "cover": true, "alarm_control_panel": true, "valve": true}

// sayUnseen: "HA NOVA can't see what “X” does" ("…can't check how “X”
// works" for a lock, cover, alarm, or valve); reached through an item, that
// item leads ("This automation uses “X”, and …").
func sayUnseen(lang, name, via string, plain bool) string {
	switch {
	case name == "" && via == "":
		return confirmT(lang, "unseen.all")
	case name == "":
		return confirmT(lang, "unseen.all_via", "via", via)
	case via == "" && plain:
		return confirmT(lang, "unseen.how", "name", name)
	case via == "":
		return confirmT(lang, "unseen.what", "name", name)
	case plain:
		return confirmT(lang, "unseen.how_via", "via", upperFirst(via), "name", name)
	}
	return confirmT(lang, "unseen.what_via", "via", upperFirst(via), "name", name)
}
