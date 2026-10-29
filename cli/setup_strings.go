package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

// The setup string catalog (docs/work/2026-10-18-onboarding-0.27-spec.md
// §4.2): every setup string is a key with named {placeholders}; plural
// entries carry CLDR forms. English only for now; a new language is one
// more file. setup_strings_test.go fails on an unknown, unused, or
// mismatched key.
//
//go:embed setupweb/strings.en.json
var setupStringsEN []byte

type setupEntry struct {
	Text   string            // a plain entry
	Plural map[string]string // CLDR plural category -> text; "other" is required
}

func (e *setupEntry) UnmarshalJSON(data []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(`"`)) {
		return json.Unmarshal(data, &e.Text)
	}
	if err := json.Unmarshal(data, &e.Plural); err != nil {
		return err
	}
	for category := range e.Plural {
		// A literal: a catalog parses while package variables are still
		// being set (confirmCatalogs), so a variable here could be empty.
		if !slices.Contains([]string{"zero", "one", "two", "few", "many", "other"}, category) {
			return fmt.Errorf("unknown CLDR plural category %q", category)
		}
	}
	if e.Plural["other"] == "" {
		return fmt.Errorf("plural entry needs \"other\"")
	}
	return nil
}

func (e setupEntry) plural() bool { return e.Plural != nil }

// pluralForm picks the English (CLDR "en") form. ponytail: English rule
// only; a second language adds its rule here (or the page's
// Intl.PluralRules picks it).
func (e setupEntry) pluralForm(count int) string {
	if text, ok := e.Plural["one"]; ok && count == 1 {
		return text
	}
	return e.Plural["other"]
}

var setupCatalog = mustParseCatalog("setup string catalog", setupStringsEN)

var setupPlaceholderPattern = regexp.MustCompile(`\{([a-z_]+)\}`)

// mustParseCatalog reads one string catalog; name prefixes its panics.
func mustParseCatalog(name string, data []byte) map[string]setupEntry {
	catalog := map[string]setupEntry{}
	if err := json.Unmarshal(data, &catalog); err != nil {
		panic(name + ": " + err.Error())
	}
	return catalog
}

// setupMsg is a catalog key plus its placeholder values, as name/value
// pairs. A renderer resolves it: the terminal now, the setup page later.
// A message without a key is data shown verbatim (setupData).
type setupMsg struct {
	Key  string
	Args []any
	Data string
}

// setupData wraps data that is shown as is: names, addresses, tokens. Never
// copy: copy is a catalog key (setup_strings_test.go rejects literals here).
func setupData(text string) setupMsg {
	return setupMsg{Data: text}
}

// setupStrong marks a placeholder value the terminal shows in bold.
type setupStrong string

func setupText(key string, kv ...any) setupMsg {
	return setupMsg{Key: key, Args: kv}
}

// setupT resolves a catalog string as plain text.
func setupT(key string, kv ...any) string {
	return setupMsg{Key: key, Args: kv}.plain()
}

func (m setupMsg) empty() bool { return m.Key == "" && m.Data == "" }

func (m setupMsg) plain() string {
	return m.render(func(v any) string { return fmt.Sprint(v) })
}

func (m *setupPageMsg) plain() string {
	args := []any{}
	for key, value := range m.Args {
		args = append(args, key, value)
	}
	return setupMsg{Key: m.Key, Args: args}.plain()
}

// render fills the placeholders; value formats each argument. A missing
// key, a missing or extra argument, or a plural entry without an integer
// "count" is a programming error and panics (the catalog test keeps the
// call sites honest).
func (m setupMsg) render(value func(any) string) string {
	if m.Key == "" {
		return m.Data
	}
	return renderCatalog(setupCatalog, "setup string catalog", m.Key, m.Args, value)
}

// renderCatalog fills one catalog entry's placeholders (name: the catalog,
// for panics); kv is name/value pairs.
func renderCatalog(catalog map[string]setupEntry, name, key string, kv []any, value func(any) string) string {
	entry, ok := catalog[key]
	if !ok {
		panic(name + ": unknown key " + key)
	}
	if len(kv)%2 != 0 {
		panic(name + ": odd argument list for " + key)
	}
	args := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		placeholder, ok := kv[i].(string)
		if !ok {
			panic(name + ": non-string placeholder name for " + key)
		}
		args[placeholder] = kv[i+1]
	}
	text := entry.Text
	if entry.plural() {
		count, ok := args["count"].(int)
		if !ok {
			panic(name + ": plural " + key + " needs an int count")
		}
		text = entry.pluralForm(count)
	}
	used := map[string]bool{"count": entry.plural()}
	out := setupPlaceholderPattern.ReplaceAllStringFunc(text, func(match string) string {
		placeholder := match[1 : len(match)-1]
		v, ok := args[placeholder]
		if !ok {
			panic(name + ": " + key + " misses {" + placeholder + "}")
		}
		used[placeholder] = true
		return value(v)
	})
	for placeholder := range args {
		if !used[placeholder] {
			panic(name + ": " + key + " has no {" + placeholder + "}")
		}
	}
	return out
}

// setupStyled resolves a catalog string for the terminal: setupStrong
// values are bold in a styled session.
func setupStyled(session uiSession, key string, kv ...any) string {
	return setupMsg{Key: key, Args: kv}.render(func(v any) string {
		if strong, ok := v.(setupStrong); ok {
			return session.style("strong", string(strong))
		}
		return fmt.Sprint(v)
	})
}
