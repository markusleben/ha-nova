package main

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// A German home's raw plan speaks German end to end (docs/work/2026-10-28-
// confirmation-localization-spec.md, Delivery 4): each step's line, the
// typed reason, the phone's title, body, and button, and its result title;
// the plan's reasons keep the English sentence its hash covers.
func TestRawPlanInAGermanHome(t *testing.T) {
	paths, _ := rawRelay(t, map[string]func(map[string]any) any{
		"GET /api/config": func(map[string]any) any {
			return map[string]any{"status": 200, "body": map[string]any{"language": "de"}}
		},
		"input_boolean/list": func(map[string]any) any { return []any{map[string]any{"id": "guest", "name": "Gäste"}} },
	})
	s, err := newRelaySession(paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	items := []rawRequest{
		{Endpoint: "ws", Payload: json.RawMessage(`{"type":"input_boolean/delete","input_boolean_id":"guest"}`)},
		{Endpoint: "ws", Payload: json.RawMessage(`{"type":"input_boolean/create","name":"Nacht"}`)},
	}
	env := verbEnvelope{}
	e, plan, err := computeRawPlan(s, &env, items)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Ich führe gleich diese 2 Schritte in Home Assistant aus, in dieser Reihenfolge:\n1. Den Schalter-Helfer „Gäste“ löschen.\n2. Den Schalter-Helfer „Nacht“ erstellen.",
		"Das löscht etwas in Home Assistant"} {
		if !strings.Contains(env.Say, want) {
			t.Errorf("say lacks %q: %q", want, env.Say)
		}
	}
	if plan.Lang != "de" || len(plan.Reasons) != 2 || plan.Reasons[0].Detail != "This deletes something in Home Assistant" {
		t.Fatalf("plan %s %+v", plan.Lang, plan.Reasons)
	}
	ask := phonePlanAsk("de", sayAsk(e, &env, plan, ""), false)
	if ask.title != "2 Schritte in Home Assistant bestätigen?" || ask.button != "Bestätigen" ||
		!strings.HasPrefix(ask.body, "Schritt 1: Den Schalter-Helfer „Gäste“ löschen. Das löscht etwas in Home Assistant. Und 1 weiterer Schritt. ") {
		t.Fatalf("phone %+v", ask)
	}
	env = verbEnvelope{}
	if e, plan, err = computeRawPlan(s, &env, items[:1]); err != nil {
		t.Fatal(err)
	}
	goal := sayAsk(e, &env, plan, "")
	if ask := phonePlanAsk("de", goal, false); ask.title != "Den Schalter-Helfer „Gäste“ löschen?" || ask.button != "Löschen" ||
		goal.title("de", confirmT("de", "outcome.done")) != "Erledigt: Den Schalter-Helfer „Gäste“ löschen" {
		t.Fatalf("one step: %+v", ask)
	}
}

// Every raw verb keeps its words on the phone, in every catalog and behind
// every result prefix: a title cuts the object, never the verb. A long name
// keeps its start, and a list its count, wherever a language puts the
// list's lead words. The button is the verb's own word while it is one or
// two words, else Confirm, as for the verbs that read like dismissing.
func TestRawTitlesKeepTheVerb(t *testing.T) {
	name := "Flurlicht Obergeschoss Nord Ost Fenster links oben sehr lang"
	prefixes := []string{"", "outcome.done", "outcome.already_done", "outcome.sent", "outcome.failed", "outcome.check", "outcome.unclear",
		"outcome.not_done", "outcome.cancelled", "outcome.not_confirmed", "outcome.not_asked"}
	for lang, catalog := range confirmCatalogs {
		opening := string([]rune(confirmQuote(lang, "“x”"))[0])
		listed := []string{sayText(name, true), sayText(name+" 2", true), sayText(name+" 3", true), sayText(name+" 4", true)}
		targets := " " + sayList(lang, listed)
		objects := []string{confirmT(lang, "raw.ws.statistics", "targets", targets), confirmT(lang, "raw.ws.exposed", "targets", targets), confirmT(lang, "raw.ws.hacs", "targets", targets)}
		for key := range catalog {
			id, ok := strings.CutPrefix(key, "raw.verb.")
			if !ok {
				continue
			}
			word := rawVerb(lang, id)
			goals := []actionGoal{{raw: id, names: []string{confirmT(lang, "raw.object.entity", "entity", sayText(name, true))}}}
			for _, object := range objects {
				goals = append(goals, actionGoal{raw: id, names: rawNames(lang, object, listed)})
			}
			for _, g := range goals {
				list := len(g.names) > 1
				for _, key := range prefixes {
					prefix, limit := "", phoneTitleLimit
					if key != "" {
						prefix, limit = confirmT(lang, key), phoneTitleLimit-1
					}
					title := g.title(lang, prefix)
					if utf8.RuneCountInString(title) > limit || !strings.Contains(title, word) && !strings.Contains(title, upperFirst(word)) || !list && !strings.Contains(title, opening+"Flurl") ||
						list && !strings.Contains(title, confirmT(lang, "list.more", "list", "", "count", len(g.names)-1)) {
						t.Errorf("%s %s %q: %q (%d runes)", lang, id, prefix, title, utf8.RuneCountInString(title))
					}
				}
			}
			button, dismissing := (actionGoal{raw: id}).button(lang), id == "cancel" || id == "ignore" || id == "hide"
			if own := upperFirst(word); !dismissing && strings.Count(word, " ") < 2 && button != own || (dismissing || strings.Count(word, " ") > 1) && button != confirmT(lang, "button.confirm") {
				t.Errorf("%s %s: button %q", lang, id, button)
			}
		}
	}
}

// Every typed raw reason is said in the plan's language; its English text,
// which the plan's reasons keep, finds it. A reason without a text reads
// as the unknown reason, never as harmless.
func TestRawWhysSpeakThePlansLanguage(t *testing.T) {
	seen := map[string]string{}
	for id, why := range rawWhys {
		if other, twice := seen[why("en")]; twice {
			t.Errorf("%s and %s read alike: %q", id, other, why("en"))
		}
		seen[why("en")] = id
		for lang := range confirmCatalogs {
			if got, typed := sayReason(lang, newTierEngine(nil), &verbEnvelope{}, storedPlan{}, tierReason{Code: reasonRawTyped, Detail: why("en")}); got != why(lang) || !typed {
				t.Errorf("%s %s: %q %v", lang, id, got, typed)
			}
		}
	}
	for lang := range confirmCatalogs {
		if got, typed := sayReason(lang, newTierEngine(nil), &verbEnvelope{}, storedPlan{}, tierReason{Code: reasonRawTyped, Detail: "This does something new"}); got != confirmT(lang, "reason.unknown") || !typed {
			t.Errorf("%s unknown: %q %v", lang, got, typed)
		}
	}
}
