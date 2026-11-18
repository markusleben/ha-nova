package main

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// MAR-241: `ha-nova setup --server <name>` for an existing profile paired
// with a local Relay runs on the setup page like a plain setup, and neither UI
// asks for the AI apps again.

func namedPairedProfile(t *testing.T) runtimePaths {
	t.Helper()
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	setServerSelectionOverride("markus-home")
	setActiveServerProfile("markus-home")
	return paths
}

var namedPaired = runtimeConfig{HAHost: "ha.local", HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791",
	RelaySecureBaseURL: "https://ha.local:8792", RelaySpkiPin: "sha256/pin"}

func TestSetupPageForAnExistingNamedProfile(t *testing.T) {
	previous := setupPageAvailable
	setupPageAvailable = func() bool { return true }
	t.Cleanup(func() { setupPageAvailable = previous })
	paths := namedPairedProfile(t)
	flow := func(cfg runtimeConfig) *setupFlow { return &setupFlow{paths: paths, cfg: cfg} }
	if !flow(namedPaired).usesSetupPage() {
		t.Fatal("an existing named profile paired with a local Relay should use the page")
	}
	for name, change := range map[string]func(*setupFlow){
		"not paired locally": func(f *setupFlow) { f.cfg.RelaySecureBaseURL, f.cfg.RelaySpkiPin = "", "" },
		"Link":               func(f *setupFlow) { f.cfg.Cloud = &cloudLifecycleMetadata{} },
		"client named":       func(f *setupFlow) { f.target = "claude" },
		"service":            func(f *setupFlow) { f.serviceMode = true },
		"relay URL flag":     func(f *setupFlow) { f.relayURLFlag = "http://ha.local:8791" },
		"custom Relay":       func(f *setupFlow) { f.cfg.RelayBaseURL = "http://proxy.lan:9000" },
	} {
		f := flow(namedPaired)
		change(f)
		if f.usesSetupPage() {
			t.Errorf("%s: uses the page", name)
		}
	}
	setupPageAvailable = func() bool { return false }
	if flow(namedPaired).usesSetupPage() {
		t.Fatal("no desktop: uses the page")
	}
}

func namedClientFlow(t *testing.T, installed []string, input string) (*setupFlow, *strings.Builder) {
	t.Helper()
	paths := namedPairedProfile(t)
	var out strings.Builder
	f := &setupFlow{paths: paths, cfg: namedPaired, state: installState{InstalledClients: installed},
		ui: setupTextUI{reader: bufio.NewReader(strings.NewReader(input)), out: &out},
		choices: []setupClientChoice{
			{Value: "claude", Label: "Claude Code", Resolved: []string{"claude"}},
			{Value: "codex", Label: "Codex", Resolved: []string{"codex"}},
			{Value: "opencode", Label: "OpenCode", Resolved: []string{"opencode"}},
			{Value: "all", Label: "All", Resolved: []string{"claude", "codex", "opencode"}},
		}}
	return f, &out
}

func TestNamedProfileSetupReusesTheAIAppsOfThisComputer(t *testing.T) {
	question := setupT("setup.clients.question")
	for name, installed := range map[string][]string{"one": {"codex"}, "two": {"claude", "codex"}} {
		f, out := namedClientFlow(t, installed, "")
		if result := f.chooseClientAndMode(); result.done {
			t.Fatalf("%s: setup ended (%d): %s", name, result.code, out)
		}
		if strings.Contains(out.String(), question) || !slices.Equal(f.selectedClients, installed) || f.promptedClient {
			t.Fatalf("%s: selected %v, asked %v: %s", name, f.selectedClients, f.promptedClient, out)
		}
	}
	f, out := namedClientFlow(t, []string{"gone"}, "2\n") // none of them here any more: asked, as on a first run
	if result := f.chooseClientAndMode(); result.done || !strings.Contains(out.String(), question) || !f.promptedClient {
		t.Fatalf("an app no longer here: done %v, asked %v: %s", result.done, f.promptedClient, out)
	}
	f, out = namedClientFlow(t, nil, "2\n")
	if result := f.chooseClientAndMode(); result.done || !strings.Contains(out.String(), question) {
		t.Fatalf("no app set up yet: done %v: %s", result.done, out)
	}
	resetServerProfileSelection(t) // the default profile keeps today's question
	f, out = namedClientFlow(t, []string{"codex"}, "2\n")
	setServerSelectionOverride("")
	setActiveServerProfile(defaultServerProfileName)
	if f.chooseClientAndMode(); !strings.Contains(out.String(), question) {
		t.Fatalf("the default profile skipped the question: %s", out)
	}
}

func TestSetupPageOnlyForAnExplicitlySelectedNamedProfile(t *testing.T) {
	previous := setupPageAvailable
	setupPageAvailable = func() bool { return true }
	t.Cleanup(func() { setupPageAvailable = previous })
	f := &setupFlow{paths: namedPairedProfile(t), cfg: namedPaired}
	setServerSelectionOverride("")
	t.Setenv(serverSelectionEnvVar, "markus-home")
	if !f.usesSetupPage() {
		t.Fatal("HA_NOVA_SERVER: no page")
	}
	t.Setenv(serverSelectionEnvVar, "")
	if f.usesSetupPage() {
		t.Fatal("a named default_server uses the page; a changed default would move its actions to another home")
	}
}

func TestNamedSetupPageKeepsLinkInTheTerminalAndRetriesOnItsProfile(t *testing.T) {
	namedPairedProfile(t)
	if (&setupFlow{cfg: namedPaired}).pageLinkOffered() {
		t.Fatal("a named page offered Link, which only the terminal sets up for it")
	}
	if got := confirmPhoneCommand(activeServerProfile()); got != "ha-nova setup confirm-phone --server markus-home" {
		t.Fatalf("named retry: %q", got)
	}
	if got := confirmPhoneCommand(defaultServerProfileName); got != "ha-nova setup confirm-phone" {
		t.Fatalf("default retry: %q", got)
	}
}

func TestNamedProfileSetupWithASavedNoAppChoiceAsksNothing(t *testing.T) {
	f, out := namedClientFlow(t, nil, "")
	for i := range f.choices {
		f.choices[i].Disabled = true
	}
	if result := f.chooseClientAndMode(); result.done || !f.noClient || f.promptedClient {
		t.Fatalf("no app here, as when it was set up: done %v, noClient %v, asked %v: %s", result.done, f.noClient, f.promptedClient, out)
	}
}

func TestOpenClaudeCodeActsOnTheNamedProfile(t *testing.T) {
	home := desktopClaudeTestHome(t)
	if runtime.GOOS != "linux" {
		t.Skip("Linux terminal handoff")
	}
	claudeDesktopPlatformOS = "linux"
	resetServerProfileSelection(t)
	setActiveServerProfile("markus-home")
	local := filepath.Join(home, ".local", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(home, "terminal.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$HA_NOVA_SERVER\" > " + shellQuote(log) + "\n"
	if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "x-terminal-emulator"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := openClaudeTerminal(paths); err != nil {
		t.Fatal(err)
	}
	for start := time.Now(); time.Since(start) < 2*time.Second; time.Sleep(10 * time.Millisecond) {
		if got := mockInvocations(t, log); got == "markus-home\n" {
			return
		}
	}
	t.Fatalf("Claude Code would act on the default home: %q", mockInvocations(t, log))
}
