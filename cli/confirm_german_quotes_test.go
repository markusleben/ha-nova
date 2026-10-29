package main

import (
	"strings"
	"testing"
)

// A name can't close or open a quote in either language: sayText turns
// every quote mark into an apostrophe („ “ ‚ ‘ included), and a German text
// re-marks only the pair HA NOVA wrote, as „…“.
func TestNamesCantBreakOutOfQuotes(t *testing.T) {
	if got := sayText("a\"b“c”d„e‚f‘g’h«i»j", true); got != "“a'b'c'd'e'f'g'h'i'j”" {
		t.Fatalf("defanged %q", got)
	}
	hostile := sayText("Haustür“ entriegeln? „Garage", true)
	for lang, want := range map[string]string{"en": "Unlock “Haustür' entriegeln? 'Garage”?", "de": "„Haustür' entriegeln? 'Garage“ entriegeln?"} {
		if got := (actionGoal{verb: "unlock", names: []string{hostile}}).title(lang, ""); got != want {
			t.Errorf("%s title %q, want %q", lang, got, want)
		}
	}
	if got := phoneLinkQuestion("de", "Anna“ soll"); !strings.HasPrefix(got, "Soll „Anna' soll“ Aktionen bestätigen") {
		t.Errorf("link ask %q", got)
	}
	h, _ := germanHome(t)
	h.f.states["lock.front"]["attributes"] = map[string]any{"friendly_name": "Haustür“ jetzt? „Garage"}
	env := h.plan("lock.unlock", "lock.front")
	if !strings.Contains(env.Say, "„Haustür' jetzt? 'Garage“ entriegeln") || strings.Count(env.Say, "„") != strings.Count(env.Say, "“") || strings.ContainsAny(env.Say, "”‚‘") {
		t.Fatalf("say %q", env.Say)
	}
}

// Every catalog quotes with its own marks only (English “…”, German „…“),
// and a cut closes the quote it opened in those marks.
func TestCatalogsQuoteInTheirOwnMarks(t *testing.T) {
	marks := map[string][3]string{"en": {"“", "”", "„"}, "de": {"„", "“", "”"}} // open, close, never
	for lang, catalog := range confirmCatalogs {
		m, ok := marks[lang]
		if !ok {
			t.Fatalf("no quote marks for %s", lang)
		}
		for key, entry := range catalog {
			for _, text := range setupEntryTexts(entry) {
				if strings.Count(text, m[0]) != strings.Count(text, m[1]) || strings.Contains(text, m[2]) {
					t.Errorf("%s %s: %q", lang, key, text)
				}
			}
		}
	}
	if en, de := phoneCut("“Front Door” unlock", 8), phoneCut("„Wohnzimmer Decke“ einschalten?", 10); en != "“Front…”" || de != "„Wohnzim…“" {
		t.Errorf("cut %q %q", en, de)
	}
}

// An undo names the item itself in German, after „an“, so the title keeps
// room for the name.
func TestGermanUndoNamesTheItem(t *testing.T) {
	g := actionGoal{verb: "revert", frame: "automation", names: []string{sayText("Flurlicht", true)}}
	if en, de := g.title("en", ""), g.title("de", ""); en != "Undo the last change to the automation “Flurlicht”?" || de != "Die letzte Änderung an „Flurlicht“ rückgängig machen?" {
		t.Errorf("%q %q", en, de)
	}
	if got := g.text("de"); got != "die letzte Änderung an „Flurlicht“ rückgängig machen" {
		t.Errorf("text %q", got)
	}
}
