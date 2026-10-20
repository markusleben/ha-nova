package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// setupCatalogCall is one setupT/setupText/setupStyled call site.
type setupCatalogCall struct {
	pos  string
	key  string
	args []string // placeholder names
}

var setupCatalogFuncs = map[string]int{"setupT": 0, "setupText": 0, "setupStyled": 1, "setupPageText": 0}

// setupPageScriptKeys are the catalog keys the setup page words itself
// (setupweb/*.js): string literals only, so every key is visible here.
func setupPageScriptKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for _, name := range []string{"app.js", "steps.js", "rerun.js"} {
		script, err := os.ReadFile(filepath.Join("setupweb", name))
		if err != nil {
			t.Fatal(err)
		}
		if regexp.MustCompile("`setup\\.").Match(script) {
			t.Fatalf("setupweb/%s builds a catalog key at runtime; use a literal key", name)
		}
		for _, match := range regexp.MustCompile(`'(setup\.[a-z0-9_.]+)'`).FindAllSubmatch(script, -1) {
			keys = append(keys, string(match[1]))
		}
	}
	return keys
}

func scanSetupCatalogCalls(t *testing.T) []setupCatalogCall {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var calls []setupCatalogCall
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		// Idents of the catalog funcs that are direct callees or their own
		// declaration; any other use is a function value that hides keys.
		allowed := map[*ast.Ident]bool{}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok {
				allowed[fn.Name] = true
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if ident, ok := call.Fun.(*ast.Ident); ok {
					allowed[ident] = true
				}
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.CompositeLit); ok && name != "setup_strings.go" && len(lit.Elts) > 0 { // setupMsg{} = no message
				if typ, ok := lit.Type.(*ast.Ident); ok && typ.Name == "setupMsg" {
					t.Errorf("%s: build setupMsg with setupText or setupData", fset.Position(lit.Pos()))
				}
			}
			if ident, ok := n.(*ast.Ident); ok && !allowed[ident] {
				if _, isCatalogFunc := setupCatalogFuncs[ident.Name]; isCatalogFunc {
					t.Errorf("%s: use %s only as a direct call", fset.Position(ident.Pos()), ident.Name)
				}
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == "setupData" && len(call.Args) == 1 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					t.Errorf("%s: setupData(%s) is copy; give it a catalog key", fset.Position(call.Pos()), lit.Value)
				}
			}
			keyIndex, ok := setupCatalogFuncs[ident.Name]
			if !ok {
				return true
			}
			pos := fset.Position(call.Pos()).String()
			if len(call.Args) <= keyIndex {
				t.Errorf("%s: %s without a key", pos, ident.Name)
				return true
			}
			found := setupCatalogCall{pos: pos}
			lit, ok := call.Args[keyIndex].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || call.Ellipsis.IsValid() {
				t.Errorf("%s: %s needs a literal key and literal placeholder names", pos, ident.Name)
				return true
			}
			found.key, _ = strconv.Unquote(lit.Value)
			rest := call.Args[keyIndex+1:]
			if len(rest)%2 != 0 {
				t.Errorf("%s: %s(%q) has an odd name/value list", pos, ident.Name, found.key)
			}
			for i := 0; i+1 < len(rest); i += 2 {
				name, ok := rest[i].(*ast.BasicLit)
				if !ok || name.Kind != token.STRING {
					t.Errorf("%s: %s(%q) placeholder names must be literals", pos, ident.Name, found.key)
					continue
				}
				unquoted, _ := strconv.Unquote(name.Value)
				found.args = append(found.args, unquoted)
			}
			calls = append(calls, found)
			return true
		})
	}
	return calls
}

func setupEntryTexts(entry setupEntry) []string {
	texts := []string{entry.Text}
	for _, text := range entry.Plural {
		texts = append(texts, text)
	}
	return texts
}

func setupEntryPlaceholders(entry setupEntry) []string {
	var names []string
	for _, text := range setupEntryTexts(entry) {
		for _, match := range setupPlaceholderPattern.FindAllStringSubmatch(text, -1) {
			if !slices.Contains(names, match[1]) {
				names = append(names, match[1])
			}
		}
	}
	if entry.plural() && !slices.Contains(names, "count") {
		names = append(names, "count")
	}
	slices.Sort(names)
	return names
}

// Every key used exists with matching placeholders, and every catalog key is
// used (spec §4.2).
func TestSetupCatalogKeysAndPlaceholdersMatchTheCode(t *testing.T) {
	calls := scanSetupCatalogCalls(t)
	if len(calls) == 0 {
		t.Fatal("found no catalog calls; the scan is broken")
	}
	used := map[string]bool{}
	for _, call := range calls {
		used[call.key] = true
		entry, ok := setupCatalog[call.key]
		if !ok {
			t.Errorf("%s: unknown setup string key %q", call.pos, call.key)
			continue
		}
		got := slices.Clone(call.args)
		slices.Sort(got)
		if want := setupEntryPlaceholders(entry); !slices.Equal(got, want) {
			t.Errorf("%s: %q passes %v, catalog wants %v", call.pos, call.key, got, want)
		}
	}
	for _, key := range setupPageScriptKeys(t) {
		used[key] = true
		if _, ok := setupCatalog[key]; !ok {
			t.Errorf("setupweb: unknown setup string key %q", key)
		}
	}
	for key := range setupCatalog {
		if !used[key] {
			t.Errorf("unused setup string key %q", key)
		}
	}
}

func TestSetupCatalogEntriesAreWellFormed(t *testing.T) {
	keyPattern := regexp.MustCompile(`^setup(\.[a-z][a-z0-9_]*)+$`)
	stray := regexp.MustCompile(`[{}]`)
	for key, entry := range setupCatalog {
		if !keyPattern.MatchString(key) {
			t.Errorf("key %q does not match %s", key, keyPattern)
		}
		for _, text := range setupEntryTexts(entry) {
			if stray.MatchString(setupPlaceholderPattern.ReplaceAllString(text, "")) {
				t.Errorf("%q has a malformed placeholder: %q", key, text)
			}
		}
		if !entry.plural() && entry.Text == "" {
			t.Errorf("%q is empty", key)
		}
	}
}

func TestSetupMsgFormatsPlaceholdersAndPlurals(t *testing.T) {
	old := setupCatalog
	t.Cleanup(func() { setupCatalog = old })
	setupCatalog = mustParseSetupCatalog([]byte(`{
		"setup.test.plain": "Hi {name}, {name}!",
		"setup.test.count": {"one": "{count} device in {room}", "other": "{count} devices in {room}"}
	}`))
	if got := setupT("setup.test.plain", "name", "Ana"); got != "Hi Ana, Ana!" {
		t.Fatalf("plain = %q", got)
	}
	for count, want := range map[int]string{0: "0 devices in Hall", 1: "1 device in Hall", 2: "2 devices in Hall"} {
		if got := setupT("setup.test.count", "count", count, "room", "Hall"); got != want {
			t.Fatalf("count %d = %q, want %q", count, got, want)
		}
	}
	styled := setupStyled(uiSession{mode: uiModeStyled, color: true}, "setup.test.plain", "name", setupStrong("Ana"))
	if !strings.Contains(styled, "\033[") || !strings.Contains(styled, "Ana") {
		t.Fatalf("styled = %q", styled)
	}
	for name, call := range map[string]func(){
		"unknown key":   func() { setupT("setup.test.nope") },
		"missing arg":   func() { setupT("setup.test.plain") },
		"extra arg":     func() { setupT("setup.test.plain", "name", "Ana", "other", 1) },
		"plural no int": func() { setupT("setup.test.count", "count", "2", "room", "Hall") },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			call()
		}()
	}
	if _, err := setupCatalogFromJSON(`{"setup.x": {"one": "a", "few": "b", "other": "c"}}`); err != nil {
		t.Fatalf("CLDR categories rejected: %v", err)
	}
	for _, bad := range []string{`{"setup.x": {"one": "a"}}`, `{"setup.x": {"one": "a", "other": "b", "several": "c"}}`} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("catalog %s parsed", bad)
				}
			}()
			mustParseSetupCatalog([]byte(bad))
		}()
	}
}

// Renderer files hold no user-facing copy: it lives in the catalog.
func TestSetupRenderersHoldNoUserFacingLiterals(t *testing.T) {
	// Input keywords and product names (proper nouns are data).
	allowed := map[string]bool{"back": true, "exit": true, "Claude Code": true, "Codex CLI": true, "OpenCode": true, "Google Antigravity": true, "Hermes Agent": true}
	words := regexp.MustCompile(`[A-Za-z]{2,} [A-Za-z]{2,}|^[A-Z][a-z]`)
	for _, name := range []string{"setup_choice.go", "setup_ui.go", "setup_intro.go", "setup_layout.go", "setup_clients.go", "setup_flow.go", "setup_flow_text.go", "setup_flow_start.go", "setup_flow_credentials.go", "setup_flow_connect.go", "setup_flow_token.go", "setup_flow_finish.go", "setup_flow_page.go", "setup_page_ui.go", "setup_page_control.go", "setup_page_home.go"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "panic" {
					return false // programming errors, never shown in a working setup
				}
			}
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value, _ := strconv.Unquote(lit.Value)
			if !allowed[value] && !strings.HasPrefix(value, "setup.") && words.MatchString(value) {
				t.Errorf("%s: user-facing literal %q belongs in setupweb/strings.en.json", fset.Position(lit.Pos()), value)
			}
			return true
		})
	}
}

func setupCatalogFromJSON(data string) (catalog map[string]setupEntry, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
	}()
	return mustParseSetupCatalog([]byte(data)), nil
}
