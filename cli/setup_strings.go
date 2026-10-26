package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
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
		if !setupPluralCategories[category] {
			return fmt.Errorf("unknown CLDR plural category %q", category)
		}
	}
	if e.Plural["other"] == "" {
		return fmt.Errorf("plural entry needs \"other\"")
	}
	return nil
}

var setupPluralCategories = map[string]bool{"zero": true, "one": true, "two": true, "few": true, "many": true, "other": true}

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

var setupCatalog = mustParseSetupCatalog(setupStringsEN)

var setupPlaceholderPattern = regexp.MustCompile(`\{([a-z_]+)\}`)

func mustParseSetupCatalog(data []byte) map[string]setupEntry {
	catalog := map[string]setupEntry{}
	if err := json.Unmarshal(data, &catalog); err != nil {
		panic("setup string catalog: " + err.Error())
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
	entry, ok := setupCatalog[m.Key]
	if !ok {
		panic("setup string catalog: unknown key " + m.Key)
	}
	if len(m.Args)%2 != 0 {
		panic("setup string catalog: odd argument list for " + m.Key)
	}
	args := map[string]any{}
	for i := 0; i < len(m.Args); i += 2 {
		name, ok := m.Args[i].(string)
		if !ok {
			panic("setup string catalog: non-string placeholder name for " + m.Key)
		}
		args[name] = m.Args[i+1]
	}
	text := entry.Text
	if entry.plural() {
		count, ok := args["count"].(int)
		if !ok {
			panic("setup string catalog: plural " + m.Key + " needs an int count")
		}
		text = entry.pluralForm(count)
	}
	used := map[string]bool{"count": entry.plural()}
	out := setupPlaceholderPattern.ReplaceAllStringFunc(text, func(match string) string {
		name := match[1 : len(match)-1]
		v, ok := args[name]
		if !ok {
			panic("setup string catalog: " + m.Key + " misses {" + name + "}")
		}
		used[name] = true
		return value(v)
	})
	for name := range args {
		if !used[name] {
			panic("setup string catalog: " + m.Key + " has no {" + name + "}")
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
