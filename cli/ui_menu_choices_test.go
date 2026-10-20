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
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// numberedChoiceMarker matches the copy of a numbered single-choice prompt
// or a [y/N] line.
var numberedChoiceMarker = regexp.MustCompile(`Enter \[|Type the number|[1-9]\) |%[ds]\) %s|y/N|Y/n`)

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
	for _, want := range []string{"renderSetupChoiceList", "promptSetupChoiceByNumber", "promptSetupConnectionModeFromReader", "promptUninstallModeFromLine", "promptYesNoWithOptions"} {
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
		{"uninstall choice", "app", func(_ *bufio.Reader, out io.Writer) error {
			choice, proceed, err := promptUninstallChoice(out, uninstallModePurge, uninstallAppOffer{offered: true}, "")
			if choice != (uninstallChoice{mode: uninstallModePurge, removeApp: true}) || !proceed {
				t.Errorf("uninstall choice = %+v proceed=%v", choice, proceed)
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
		{"no address entered", "no", func(r *bufio.Reader, out io.Writer) error {
			_, _, err := promptValidHAHostFromReader(bufio.NewReader(strings.NewReader("\n")), out, "")
			if err == nil || !strings.Contains(err.Error(), "needs a Home Assistant address") {
				t.Errorf("no address = %v", err)
			}
			return nil
		}},
		{"protected file", "yes", func(r *bufio.Reader, out io.Writer) error {
			if yes, err := promptSetupServiceCredentialsInteractive(r, out, "Claude Code"); !yes || err != nil {
				t.Errorf("protected file = %v %v", yes, err)
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
	// Ratchet: the wizard's menus fit an 80-column terminal without wrapping.
	for _, spec := range runner.specs {
		if wrapped, flat := renderSetupMenuBlock(io.Discard, uiSession{}, spec, 0, 80), renderSetupMenuBlock(io.Discard, uiSession{}, spec, 0, 0); wrapped != flat {
			t.Fatalf("menu %q wraps at 80 columns", spec.Title)
		}
	}
}

// Where "Remove" can't run (the user just declined the keyring window, or
// there is no desktop), Enter cancels in the arrow menu and in the numbered
// fallback (no TTY) alike; nothing is removed.
func TestUninstallChoiceDefaultsToCancelWhenRemoveIsBlocked(t *testing.T) {
	originalInput, originalRunner, originalStdin := uiInputSupportsTTY, interactiveSetupMenuRunner, os.Stdin
	t.Cleanup(func() {
		uiInputSupportsTTY, interactiveSetupMenuRunner, os.Stdin = originalInput, originalRunner, originalStdin
	})
	uiInputSupportsTTY = func() bool { return false }
	in, err := os.CreateTemp(t.TempDir(), "stdin-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close() })
	if _, err := in.WriteString("\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	os.Stdin = in
	var out bytes.Buffer
	var proceed bool
	_, stdout := captureCommandOutput(t, func() int {
		_, proceed, err = promptUninstallChoice(&out, uninstallModePurge, uninstallAppOffer{}, "the keyring is locked")
		return 0
	})
	if err != nil || proceed || !strings.Contains(stdout, "1) Remove, but keep the pairing for a reinstall  2) Cancel [2]") {
		t.Fatalf("proceed=%v err=%v\n%s%s", proceed, err, out.String(), stdout)
	}

	runner := &recordingSetupMenuRunner{answer: "cancel"}
	interactiveSetupMenuRunner = runner
	uiInputSupportsTTY = func() bool { return true }
	stubUninstallMenu(t, "cancel")
	interactiveSetupMenuRunner = runner
	if _, _, err := promptUninstallChoice(&out, uninstallModePurge, uninstallAppOffer{}, "the keyring is locked"); err != nil ||
		len(runner.specs) != 1 || runner.specs[0].DefaultValue != "cancel" {
		t.Fatalf("menu specs = %+v err=%v", runner.specs, err)
	}
}

// Yes/no questions keep their safe default in the arrow menu and in the
// [y/N] line without a terminal; Esc goes back only in the wizard.
func TestYesNoMenuKeepsTheDefault(t *testing.T) {
	originalEnv, originalTTY, originalInput, originalANSI, originalRunner := uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner
	t.Cleanup(func() {
		uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI, interactiveSetupMenuRunner = originalEnv, originalTTY, originalInput, originalANSI, originalRunner
	})
	uiEnvLookup = func(string) string { return "" }
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	for _, defaultYes := range []bool{false, true} {
		for _, wizard := range []bool{false, true} {
			uiInputSupportsTTY = func() bool { return true }
			runner := &recordingSetupMenuRunner{answer: "no"}
			interactiveSetupMenuRunner = runner
			var out bytes.Buffer
			if _, err := promptYesNoMenuWith(bufio.NewReader(strings.NewReader("")), &out, "Go?", "Yes", "No", defaultYes, wizard); err != nil {
				t.Fatal(err)
			}
			spec := runner.specs[0]
			if spec.DefaultValue != map[bool]string{true: "yes", false: "no"}[defaultYes] || spec.AllowBack != wizard ||
				strings.Contains(spec.Prompt, "Esc") != wizard {
				t.Fatalf("default %v wizard %v: %+v", defaultYes, wizard, spec)
			}

			uiInputSupportsTTY = func() bool { return false }
			out.Reset()
			yes, err := promptYesNoMenuWith(bufio.NewReader(strings.NewReader("\n")), &out, "Go?", "Yes", "No", defaultYes, wizard)
			if err != nil || yes != defaultYes || !strings.Contains(out.String(), map[bool]string{true: "[Y/n]", false: "[y/N]"}[defaultYes]) {
				t.Fatalf("line: default %v wizard %v: yes=%v err=%v %q", defaultYes, wizard, yes, err, out.String())
			}
		}
	}
}

// NO_COLOR turns off color, not the arrow menu; TERM=dumb (no cursor moves)
// and HA_NOVA_PLAIN_UI=1 keep the numbered prompts.
func TestNoColorKeepsTheArrowMenu(t *testing.T) {
	originalEnv, originalTTY, originalInput, originalANSI := uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI
	t.Cleanup(func() {
		uiEnvLookup, writerSupportsTTYForSetup, uiInputSupportsTTY, uiOutputSupportsANSI = originalEnv, originalTTY, originalInput, originalANSI
	})
	writerSupportsTTYForSetup = func(io.Writer) bool { return true }
	uiInputSupportsTTY = func() bool { return true }
	uiOutputSupportsANSI = func(io.Writer) bool { return true }
	for _, test := range []struct {
		env   map[string]string
		mode  uiMode
		color bool
	}{
		{map[string]string{}, uiModeEnhanced, true},
		{map[string]string{"NO_COLOR": "1"}, uiModeEnhanced, false},
		{map[string]string{"TERM": "dumb"}, uiModePlain, false},
		{map[string]string{"HA_NOVA_PLAIN_UI": "1"}, uiModePlain, false},
	} {
		uiEnvLookup = func(key string) string { return test.env[key] }
		session := resolveSetupUISession(&bytes.Buffer{})
		if session.mode != test.mode || session.color != test.color || (test.env["NO_COLOR"] != "" && session.style("accent", "x") != "x") {
			t.Fatalf("%v: %+v", test.env, session)
		}
	}
}

// y and n pick that option in a yes/no menu (Enter confirms), as the [y/N]
// line did; menus without yes/no options ignore them.
func TestSetupMenuLetterPicksYesOrNo(t *testing.T) {
	yesNo := []setupMenuOption{{Value: "yes"}, {Value: "no"}}
	if setupMenuOptionIndex(yesNo, "no") != 1 || setupMenuOptionIndex(yesNo, "yes") != 0 {
		t.Fatal("y/n don't pick their option")
	}
	if setupMenuOptionIndex([]setupMenuOption{{Value: "purge"}, {Value: "cancel"}}, "no") != -1 ||
		setupMenuOptionIndex([]setupMenuOption{{Value: "no", Disabled: true}}, "no") != -1 {
		t.Fatal("a letter picked an option it doesn't name")
	}
}
