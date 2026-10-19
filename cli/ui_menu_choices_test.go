package main

import (
	"bufio"
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// numberedChoiceMarker matches the copy of a numbered single-choice prompt.
var numberedChoiceMarker = regexp.MustCompile(`Enter \[|Type the number|[1-9]\) |%[ds]\) %s`)

// A numbered choice prompt may only run as the fallback of the arrow menu:
// every call to a function that prints one sits in a closure passed to
// promptSetupMenu (or in itself, for a retry), never on a direct path.
func TestNumberedChoicePromptsOnlyRunAsArrowMenuFallback(t *testing.T) {
	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var parsed []*ast.File
	numbered := map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		parsed = append(parsed, file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				if isPromptSetupMenuCall(n) {
					return false // copy inside the fallback closure is the fallback
				}
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && numberedChoiceMarker.MatchString(lit.Value) {
					numbered[fn.Name.Name] = true
				}
				// The copy may live in the setup string catalog.
				if call, ok := n.(*ast.CallExpr); ok && len(call.Args) > 0 {
					if ident, ok := call.Fun.(*ast.Ident); ok && (ident.Name == "setupT" || ident.Name == "setupText") {
						if lit, ok := call.Args[0].(*ast.BasicLit); ok {
							key, _ := strconv.Unquote(lit.Value)
							if numberedChoiceMarker.MatchString(setupCatalog[key].Text) {
								numbered[fn.Name.Name] = true
							}
						}
					}
				}
				return true
			})
		}
	}
	// Not prompts: listPhones prints the list for `confirm-phone list` and
	// after a mistyped name, where a number is a command-line argument.
	delete(numbered, "listPhones")
	for _, want := range []string{"renderSetupChoiceList", "promptSetupChoiceByNumber", "promptSetupConnectionModeFromReader", "promptUninstallModeFromLine"} {
		if !numbered[want] {
			t.Fatalf("guard lost track of %s; numbered=%v", want, numbered)
		}
	}
	// Not a prompt either: renderSetupChoiceList only lists the choices (the
	// numbered fallback and the no-client notice).
	delete(numbered, "renderSetupChoiceList")
	for _, file := range parsed {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			var walk func(n ast.Node, inFallback bool)
			walk = func(n ast.Node, inFallback bool) {
				ast.Inspect(n, func(child ast.Node) bool {
					call, ok := child.(*ast.CallExpr)
					if !ok {
						return true
					}
					if isPromptSetupMenuCall(call) {
						for _, arg := range call.Args {
							walk(arg, true)
						}
						return false
					}
					ident, _ := call.Fun.(*ast.Ident)
					if ident != nil && numbered[ident.Name] && !inFallback && ident.Name != fn.Name.Name {
						t.Errorf("%s: %s calls numbered prompt %s outside a promptSetupMenu fallback", fset.Position(call.Pos()), fn.Name.Name, ident.Name)
					}
					return true
				})
			}
			walk(fn.Body, false)
		}
	}
}

func isPromptSetupMenuCall(n ast.Node) bool {
	call, ok := n.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "promptSetupMenu"
}

// With an interactive terminal every choice question goes to the arrow menu
// and prints no numbered prompt.
func TestEveryChoicePromptUsesArrowMenuOnTTY(t *testing.T) {
	originalEnv, originalTTY, originalInput, originalANSI, originalRunner := uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner
	t.Cleanup(func() {
		uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner = originalEnv, originalTTY, originalInput, originalANSI, originalRunner
	})
	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }

	phone := phoneTarget{Name: "Pixel", Person: "Ana", DeviceID: "d1", UserID: "u1"}
	home := phoneHome{users: map[string]haUser{"u1": {ID: "u1", IsActive: true, IsOwner: true}}, phones: []phoneTarget{phone}}
	cases := []struct {
		name   string
		answer string
		run    func(*bufio.Reader, io.Writer) error
	}{
		{"client", "claude", func(r *bufio.Reader, out io.Writer) error {
			_, err := promptSetupClientInteractive(r, out, testSetupClientChoices(), "claude")
			return err
		}},
		{"token", "generate", func(r *bufio.Reader, out io.Writer) error {
			_, err := promptSetupTokenChoiceInteractive(r, out, true)
			return err
		}},
		{"repair", string(setupRepairActionRetry), func(r *bufio.Reader, out io.Writer) error {
			_, err := promptSetupRepairActionInteractive(r, out, setupRepairModeConnection, setupCredentialRepairPairing)
			return err
		}},
		{"discovery", "0", func(r *bufio.Reader, out io.Writer) error {
			_, _, err := promptSetupDiscoveryCandidateInteractive(r, out, []setupDiscoveryCandidate{{Host: "192.168.1.5", Source: "mDNS"}})
			return err
		}},
		{"connection mode", "hybrid", func(r *bufio.Reader, out io.Writer) error {
			mode, err := promptSetupConnectionMode(r, out)
			if mode != setupConnectionHybrid {
				t.Errorf("connection mode = %q", mode)
			}
			return err
		}},
		{"uninstall mode", "purge", func(_ *bufio.Reader, out io.Writer) error {
			mode, proceed, err := promptUninstallMode(out, uninstallModeStandard)
			if mode != uninstallModePurge || !proceed {
				t.Errorf("uninstall mode = %q proceed=%v", mode, proceed)
			}
			return err
		}},
		{"phone list", "0", func(r *bufio.Reader, out io.Writer) error {
			if got, ok := choosePhone(r, out, home, ""); !ok || got != phone {
				t.Errorf("phone = %+v ok=%v", got, ok)
			}
			return nil
		}},
		{"device verify retry", "back", func(r *bufio.Reader, out io.Writer) error {
			if err := promptSetupDeviceVerifyRetry(r, out); err != errSetupBack {
				t.Errorf("verify retry = %v, want errSetupBack", err)
			}
			return nil
		}},
		{"own phone", "use", func(r *bufio.Reader, out io.Writer) error {
			if got := promptOwnPhone(r, out, phone); got != "use" {
				t.Errorf("own phone = %q", got)
			}
			return nil
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &stubSetupMenuRunner{answer: tc.answer}
			interactiveSetupMenuRunner = runner
			var out bytes.Buffer
			if err := tc.run(bufio.NewReader(strings.NewReader("")), &out); err != nil {
				t.Fatalf("error: %v", err)
			}
			if runner.calls != 1 {
				t.Fatalf("menu runner calls = %d, want 1", runner.calls)
			}
			if numberedChoiceMarker.MatchString(out.String()) {
				t.Fatalf("numbered prompt printed on a TTY:\n%s", out.String())
			}
		})
	}
}

type recordingSetupMenuRunner struct {
	specs  []setupMenuSpec
	answer string
}

func (r *recordingSetupMenuRunner) Run(_ io.Writer, spec setupMenuSpec) (string, error) {
	r.specs = append(r.specs, spec)
	return r.answer, nil
}

// The wizard's terminal copy: one idea per line, the pairing choice as a
// menu, and the per-device pairing promise in one short line.
func TestWizardCopyPinsTheShortTerminalText(t *testing.T) {
	originalEnv, originalTTY, originalInput, originalANSI, originalRunner := uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner
	originalResolve := resolveHASignInOriginForSetup
	t.Cleanup(func() {
		uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner = originalEnv, originalTTY, originalInput, originalANSI, originalRunner
		resolveHASignInOriginForSetup = originalResolve
	})
	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) {
		return &url.URL{Scheme: "http", Host: "ha.local"}, nil
	}

	runner := &recordingSetupMenuRunner{answer: "code"}
	interactiveSetupMenuRunner = runner
	var out bytes.Buffer
	if _, _, err := runHASignInPairingStep(bufio.NewReader(strings.NewReader("")), &out, runtimePaths{}, &runtimeConfig{HAURL: "http://ha.local:8123"}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Your browser opens Home Assistant: sign in as the owner.",
		"(Home Assistant asks to give “http://127.0.0.1:…” access: that's this computer.)",
		"Each computer gets its own connection; remove it in NOVA anytime.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("sign-in step missing %q:\n%s", want, out.String())
		}
	}
	signIn := runner.specs[0]
	if signIn.Title != "" || signIn.DefaultValue != "signin" || !signIn.AllowBack ||
		signIn.Options[0].Label != "Sign in with Home Assistant" || signIn.Options[1].Label != "Use a six-digit code instead" {
		t.Fatalf("sign-in menu = %+v", signIn)
	}

	out.Reset()
	if err := promptToOpenNOVAForPairing(bufio.NewReader(strings.NewReader("\n")), &out, "http://192.168.1.10:8123"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`In NOVA (Home Assistant sidebar), click "Connect a device" for a one-time six-digit code.`,
		"Each computer gets its own connection; remove it in NOVA anytime.",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("code path missing %q:\n%s", want, out.String())
		}
	}

	runner.answer = "local"
	if _, err := promptSetupConnectionMode(bufio.NewReader(strings.NewReader("")), &out); err != nil {
		t.Fatal(err)
	}
	mode := runner.specs[len(runner.specs)-1]
	if mode.Note != "Link = Nabu Casa remote access, formerly Home Assistant Cloud." ||
		mode.Options[0].Label != "Local only (recommended)" || mode.Options[0].Detail != "No subscription needed." {
		t.Fatalf("connection mode menu = %+v", mode)
	}
	// Ratchet: the menus must fit an 80-column terminal, or they fall back.
	for _, spec := range runner.specs {
		if width := setupMenuBlockWidth(spec); width >= 80 {
			t.Fatalf("menu %q is %d columns wide", spec.Title, width)
		}
	}
}
