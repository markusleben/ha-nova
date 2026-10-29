package main

import (
	"regexp"
	"slices"
	"testing"
)

// Every confirmT key exists in English with the placeholders its call
// passes, and every English key is used.
func TestConfirmCatalogKeysMatchTheCode(t *testing.T) {
	calls := scanCatalogCalls(t, map[string]int{"confirmT": 1})
	if len(calls) == 0 {
		t.Fatal("found no confirmT calls; the scan is broken")
	}
	en := confirmCatalogs["en"]
	used := map[string]bool{}
	for _, call := range calls {
		used[call.key] = true
		entry, ok := en[call.key]
		if !ok {
			t.Errorf("%s: unknown confirm string key %q", call.pos, call.key)
			continue
		}
		got := slices.Clone(call.args)
		slices.Sort(got)
		if want := setupEntryPlaceholders(entry); !slices.Equal(got, want) {
			t.Errorf("%s: %q passes %v, catalog wants %v", call.pos, call.key, got, want)
		}
	}
	for key := range en {
		if !used[key] {
			t.Errorf("unused confirm string key %q", key)
		}
	}
}

// A confirmation never mixes languages: every catalog has exactly the
// English keys, each with the same placeholders and plural shape.
func TestConfirmCatalogsAreComplete(t *testing.T) {
	keyPattern := regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	stray := regexp.MustCompile(`[{}]`)
	en := confirmCatalogs["en"]
	for lang, catalog := range confirmCatalogs {
		if len(catalog) != len(en) {
			t.Errorf("%s has %d keys, en has %d", lang, len(catalog), len(en))
		}
		for key, entry := range catalog {
			source, ok := en[key]
			switch {
			case !ok:
				t.Errorf("%s: key %q is not in en", lang, key)
				continue
			case !keyPattern.MatchString(key):
				t.Errorf("%s: key %q does not match %s", lang, key, keyPattern)
			case entry.plural() != source.plural():
				t.Errorf("%s: %q plural %v, en %v", lang, key, entry.plural(), source.plural())
			case !slices.Equal(setupEntryPlaceholders(entry), setupEntryPlaceholders(source)):
				t.Errorf("%s: %q placeholders %v, en %v", lang, key, setupEntryPlaceholders(entry), setupEntryPlaceholders(source))
			case !entry.plural() && entry.Text == "":
				t.Errorf("%s: %q is empty", lang, key)
			}
			for _, text := range setupEntryTexts(entry) {
				if stray.MatchString(setupPlaceholderPattern.ReplaceAllString(text, "")) {
					t.Errorf("%s: %q has a malformed placeholder: %q", lang, key, text)
				}
			}
		}
		if confirmT(lang, "window.confirm") == confirmT(lang, "window.cancel") {
			t.Errorf("%s: the window's Confirm and Cancel read alike", lang)
		}
	}
}

func TestConfirmLanguageFoldsToACatalog(t *testing.T) {
	saved := confirmCatalogs
	t.Cleanup(func() { confirmCatalogs = saved })
	confirmCatalogs = map[string]map[string]setupEntry{"en": saved["en"], "de": saved["en"]}
	for in, want := range map[string]string{"de": "de", "de-CH": "de", "DE_at": "de", "en-GB": "en", "fr": "en", "": "en", "-de": "en"} {
		if got := confirmLanguage(in); got != want {
			t.Errorf("confirmLanguage(%q) = %q, want %q", in, got, want)
		}
	}
}
