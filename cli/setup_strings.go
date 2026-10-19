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
	Text  string // a plain entry
	One   string // plural entries: CLDR "one" and "other"
	Other string
}

func (e *setupEntry) UnmarshalJSON(data []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(`"`)) {
		return json.Unmarshal(data, &e.Text)
	}
	var forms struct {
		One   *string `json:"one"`
		Other *string `json:"other"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&forms); err != nil {
		return err
	}
	if forms.One == nil || forms.Other == nil {
		return fmt.Errorf("plural entry needs both \"one\" and \"other\"")
	}
	e.One, e.Other = *forms.One, *forms.Other
	return nil
}

func (e setupEntry) plural() bool { return e.Other != "" }

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
type setupMsg struct {
	Key  string
	Args []any
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

func (m setupMsg) plain() string {
	return m.render(func(v any) string { return fmt.Sprint(v) })
}

// render fills the placeholders; value formats each argument. A missing
// key, a missing or extra argument, or a plural entry without an integer
// "count" is a programming error and panics (the catalog test keeps the
// call sites honest).
func (m setupMsg) render(value func(any) string) string {
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
		text = entry.Other
		if count == 1 {
			text = entry.One
		}
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
