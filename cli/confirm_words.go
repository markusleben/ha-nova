package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Home Assistant's own words in a plan's language (docs/work/2026-10-28-
// confirmation-localization-spec.md, Design 4 and Decision 3): the name of
// a service HA NOVA has no verb for, and the state names of the domains a
// call acts on. Read when the plan is made (frontend/get_translations),
// kept in plan.Words, and sealed with the language, so apply asks in the
// same words. Keys: "service:<domain>.<service>", "state:<domain>.<state>".
type haWords map[string]string

// language is the catalog this session's confirmations speak: a stored
// plan's while it is re-planned, else Home Assistant's own.
func (s *relaySession) language() string {
	if s.lang != "" {
		return s.lang
	}
	return s.homeLanguage()
}

// homeLanguage: Home Assistant's language now (/api/config), English when
// that can't be read or has no catalog. A settings ask speaks it, never a
// plan's.
func (s *relaySession) homeLanguage() string {
	var config struct {
		Language string `json:"language"`
	}
	if status, body, err := s.coreGET("/api/config"); err == nil && status == http.StatusOK {
		_ = json.Unmarshal(body, &config)
	}
	return confirmLanguage(config.Language)
}

// haNameMax: the longest Home Assistant name for a service a confirmation
// uses, in runes, so the phone title keeps room for what it acts on.
const haNameMax = 40

// planLanguage: the language and Home Assistant words of a plan that asks.
// A raw plan speaks English until its lines have a German text (Decision
// 1). A service HA NOVA names by its label needs Home Assistant's own name
// for it, read in both languages (Home Assistant fills a missing name in
// with the English one), with a letter once defanged, and short; else the
// whole plan is English (Design 9: never mixed) and a notice says so. A
// state without a name stays as it is. A re-plan keeps the stored words.
func planLanguage(e *tierEngine, env *verbEnvelope, plan storedPlan) (string, haWords) {
	if e.level == tierOrdinary { // it asks nothing
		return "", nil
	}
	lang := e.s.language()
	switch {
	case plan.Kind == "raw" || lang == "en":
		return "en", nil
	case e.s.lang != "":
		return lang, e.s.words
	case plan.Kind != "call":
		return lang, nil
	}
	words := haWords{}
	if sayGoal(e, env, plan, "").verb == "" {
		domain, name := splitID(plan.Action.Service)
		key := "component." + domain + ".services." + name + ".name"
		own, read := haTranslations(e.s, lang, "services", []string{domain})
		english, readEnglish := haTranslations(e.s, "en", "services", []string{domain})
		if shown := sayText(own[key], false); !read || !readEnglish || !strings.ContainsFunc(shown, unicode.IsLetter) || own[key] == english[key] || utf8.RuneCountInString(shown) > haNameMax {
			env.Notices = append(env.Notices, verbNotice{Kind: "language", Text: fmt.Sprintf("HA NOVA couldn't get a short name for %s in Home Assistant's language (%s), so this confirmation is in English.", plan.Action.Service, lang)})
			return "en", nil
		}
		words["service:"+plan.Action.Service] = own[key]
	}
	var domains []string
	for _, id := range plan.Action.EntityIDs {
		domain, _ := splitID(id)
		domains = appendUnique(domains, domain)
	}
	states, _ := haTranslations(e.s, lang, "entity_component", domains)
	for key, name := range states {
		domain, rest, _ := strings.Cut(strings.TrimPrefix(key, "component."), ".")
		if state, ok := strings.CutPrefix(rest, "entity_component._.state."); ok && name != "" {
			words["state:"+domain+"."+state] = name
		}
	}
	return lang, words
}

// haTranslations: Home Assistant's texts of one category for domains in
// lang, by flat key ("component.lock.entity_component._.state.locked");
// read is false when the read failed.
func haTranslations(s *relaySession, lang, category string, domains []string) (texts map[string]string, read bool) {
	var reply struct {
		Resources map[string]string `json:"resources"`
	}
	if len(domains) == 0 || s.wsDecode(map[string]any{"type": "frontend/get_translations", "language": lang, "category": category, "integration": domains}, &reply, "translations") != nil {
		return nil, false
	}
	return reply.Resources, true
}

// valueText: how a confirmation shows an entity's current value in the
// plan's language: a state by Home Assistant's name for it, unknown and
// unavailable by HA NOVA's word, anything else as it is (sayValue). A
// value a call sends is shown as it is.
func (p storedPlan) valueText(entityID, field, value string) string {
	domain, _ := splitID(entityID)
	if word := p.Words["state:"+domain+"."+value]; field == "state" && word != "" {
		return sayValue(word)
	}
	switch value {
	case "unknown":
		return confirmT(textLang(p), "say.value.unknown")
	case "unavailable":
		return confirmT(textLang(p), "say.value.unavailable")
	}
	return sayValue(value)
}
