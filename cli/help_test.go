package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every user-facing subcommand must answer --help with its usage line and the
// registered flag defaults on stdout, exit 0 — not the former
// "ERROR: flag: help requested" (exit 1) that hid flags like update --force.
func TestSubcommandHelpFlagPrintsUsageAndFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}

	cases := []struct {
		name      string
		run       func() int
		wantUsage string
		wantFlag  string
	}{
		{"update", func() int { return runUpdate(paths, []string{"--help"}) }, "Usage: ha-nova update", "-force"},
		{"doctor", func() int { return runDoctor(paths, []string{"--help"}) }, "Usage: ha-nova doctor", "-auto-repair"},
		{"check-update", func() int { return runCheckUpdate(paths, []string{"--help"}) }, "Usage: ha-nova check-update", "-json"},
		{"uninstall", func() int { return runUninstall(paths, []string{"--help"}) }, "Usage: ha-nova uninstall", "-purge"},
		{"setup", func() int { return runSetup(paths, []string{"--help"}) }, "Usage: ha-nova setup", "-non-interactive"},
		{"status", func() int { return runStatus(paths, []string{"--help"}) }, "Usage: ha-nova status", "-json"},
		{"diff", func() int { return runDiffCommand(paths, []string{"--help"}) }, "Usage: ha-nova diff", "-before"},
		{"snapshot save", func() int { return runSnapshotCommand(paths, []string{"save", "--help"}) }, "Usage: ha-nova snapshot save", "-data-file"},
		{"snapshot show", func() int { return runSnapshotCommand(paths, []string{"show", "--help"}) }, "Usage: ha-nova snapshot show", "-list"},
		{"snapshot verify", func() int { return runSnapshotCommand(paths, []string{"verify", "--help"}) }, "Usage: ha-nova snapshot verify", "-against"},
		{"trace get", func() int { return runTraceCommand(paths, []string{"get", "--help"}) }, "Usage: ha-nova trace get", "-json"},
		{"trace latest", func() int { return runTraceCommand(paths, []string{"latest", "--help"}) }, "Usage: ha-nova trace latest", "-json"},
		{"relay health", func() int { return runRelayCommand(paths, []string{"health", "--help"}) }, "Usage: ha-nova relay health", "-connect-timeout"},
		{"relay (parent)", func() int { return runRelayCommand(paths, []string{"--help"}) }, "Usage: ha-nova relay <health|ws|core|files|backups|jq|version>", "relay <subcommand> --help"},
		{"relay jq", func() int { return runRelayCommand(paths, []string{"jq", "--help"}) }, "Usage: ha-nova relay jq", "--jq-file"},
		// Must answer even on a fresh install: help may not hide behind the
		// config/token preflight of the proxy path.
		{"relay core", func() int { return runRelayCommand(paths, []string{"core", "--help"}) }, "Usage: ha-nova relay core", "-body-file"},
		{"relay ws", func() int { return runRelayCommand(paths, []string{"ws", "--help"}) }, "Usage: ha-nova relay ws", "-data-file"},
		{"relay version", func() int { return runRelayCommand(paths, []string{"version", "--help"}) }, "Usage: ha-nova relay version", "No flags"},
		{"version (root)", func() int { return dispatch(paths, "ha-nova", []string{"version", "--help"}) }, "Usage: ha-nova version", "No flags"},
		{"trace (parent)", func() int { return runTraceCommand(paths, []string{"--help"}) }, "Usage: ha-nova trace <latest|list|get>", "trace <subcommand> --help"},
		{"snapshot (parent)", func() int { return runSnapshotCommand(paths, []string{"--help"}) }, "Usage: ha-nova snapshot <save|show|verify>", "snapshot <subcommand> --help"},
		{"census", func() int { return runCensusCommand(paths, []string{"--help"}) }, "Usage: ha-nova census <on|off|status>", "No flags"},
		{"census status", func() int { return runCensusCommand(paths, []string{"status", "--help"}) }, "Usage: ha-nova census <on|off|status>", "No flags"},
	}

	for _, tc := range cases {
		exit := 0
		out := captureStdout(t, func() {
			exit = tc.run()
		})
		if exit != 0 {
			t.Fatalf("%s --help exit = %d, want 0\noutput:\n%s", tc.name, exit, out)
		}
		if !strings.Contains(out, tc.wantUsage) {
			t.Fatalf("%s --help missing usage %q:\n%s", tc.name, tc.wantUsage, out)
		}
		if !strings.Contains(out, tc.wantFlag) {
			t.Fatalf("%s --help missing flag %q:\n%s", tc.name, tc.wantFlag, out)
		}
	}
}

// relayProxyHelp captures the --help output of one relay proxy subcommand.
func relayProxyHelp(t *testing.T, command string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := detectPaths()
	if err != nil {
		t.Fatalf("detectPaths() error: %v", err)
	}
	exit := 0
	out := captureStdout(t, func() {
		exit = runRelayCommand(paths, []string{command, "--help"})
	})
	if exit != 0 {
		t.Fatalf("relay %s --help exit = %d, want 0\noutput:\n%s", command, exit, out)
	}
	return out
}

// ws/core --help must teach the one body-and-extraction contract: inline JSON
// only for tiny read-only diagnostics, file flags for everything else, the
// response envelope with .data vs .data.body, native jq extraction, and (core)
// the strict-status exit semantics.
func TestRelayProxyHelpTeachesBodyAndEnvelopeContract(t *testing.T) {
	wsOut := relayProxyHelp(t, "ws")
	for _, want := range []string{
		"inline -d/--data JSON is acceptable only for tiny, unambiguously read-only diagnostics",
		"Mutations, complex bodies, reusable payloads, and cross-platform examples use --data-file.",
		`{"ok":true,"data":...}`,
		"the upstream payload is in .data directly",
		"--jq '.data.version' on a ws get_config result",
		"Extraction: use --jq, --jq-file, or 'ha-nova relay jq'; never call external jq.",
	} {
		if !strings.Contains(wsOut, want) {
			t.Fatalf("relay ws --help missing %q:\n%s", want, wsOut)
		}
	}

	coreOut := relayProxyHelp(t, "core")
	for _, want := range []string{
		"inline -d/--body JSON is acceptable only for tiny, unambiguously read-only diagnostics",
		"Mutations, complex bodies, reusable payloads, and cross-platform examples use --body-file.",
		`{"ok":true,"data":{"status":200,"body":...}}`,
		"the upstream payload is in .data.body",
		"the upstream HTTP status in .data.status",
		"--jq '.data.body.state' on a core GET /api/states/<entity_id> result",
		"upstream 5xx always exits nonzero",
		"--strict-status exits nonzero for any upstream error status",
		"The response envelope is still printed either way.",
		"Extraction: use --jq, --jq-file, or 'ha-nova relay jq'; never call external jq.",
	} {
		if !strings.Contains(coreOut, want) {
			t.Fatalf("relay core --help missing %q:\n%s", want, coreOut)
		}
	}
}

// The skill doc and the generated CLI help must carry the same contract
// sentences (backticks stripped, case-insensitive) so neither can drift.
func TestRelayHelpContractMatchesSkillDoc(t *testing.T) {
	docBytes, err := os.ReadFile(filepath.Join("..", "skills", "ha-nova", "relay-api.md"))
	if err != nil {
		t.Fatalf("reading relay-api.md: %v", err)
	}
	doc := strings.ToLower(strings.ReplaceAll(string(docBytes), "`", ""))
	wsOut := strings.ToLower(relayProxyHelp(t, "ws"))
	coreOut := strings.ToLower(relayProxyHelp(t, "core"))

	shared := []struct {
		span string
		help string
		name string
	}{
		{"acceptable only for tiny, unambiguously read-only diagnostics", wsOut, "ws"},
		{"acceptable only for tiny, unambiguously read-only diagnostics", coreOut, "core"},
		{"mutations, complex bodies, reusable payloads, and cross-platform examples use --data-file", wsOut, "ws"},
		{"mutations, complex bodies, reusable payloads, and cross-platform examples use --body-file", coreOut, "core"},
		{"upstream payload is in .data directly", wsOut, "ws"},
		{"--jq '.data.version' on a ws get_config result", wsOut, "ws"},
		{"upstream payload is in .data.body", coreOut, "core"},
		{"--jq '.data.body.state' on a core get /api/states/<entity_id> result", coreOut, "core"},
		{"upstream 5xx always exits nonzero", coreOut, "core"},
		{"exits nonzero for any upstream error status", coreOut, "core"},
		{"the response envelope is still printed either way", coreOut, "core"},
		{"never call external jq", wsOut, "ws"},
		{"never call external jq", coreOut, "core"},
	}
	docWS := strings.ReplaceAll(doc, " (ws) / --body-file (core)", "")
	docCore := strings.ReplaceAll(strings.ReplaceAll(doc, "--data-file (ws) / ", ""), "--body-file (core)", "--body-file")
	for _, tc := range shared {
		if !strings.Contains(doc, tc.span) && !strings.Contains(docWS, tc.span) && !strings.Contains(docCore, tc.span) {
			t.Fatalf("relay-api.md missing contract span %q", tc.span)
		}
		if !strings.Contains(tc.help, tc.span) {
			t.Fatalf("relay %s --help missing contract span %q:\n%s", tc.name, tc.span, tc.help)
		}
	}
	// The false absolutism this contract replaced must not come back.
	if strings.Contains(doc, "must use --data-file") {
		t.Fatalf("relay-api.md reintroduces the inline-JSON absolutism (%q)", "MUST use --data-file")
	}
}

// The help output renders the flag as "-json" (PrintDefaults), so the manual
// trace parsers must accept that spelling as well as "--json".
func TestTraceParsersAcceptBothJSONFlagForms(t *testing.T) {
	if _, jsonOut, err := parseTraceLatestArgs([]string{"automation.x", "-json"}); err != nil || !jsonOut {
		t.Fatalf("parseTraceLatestArgs(-json) = jsonOut %v, err %v; want true, nil", jsonOut, err)
	}
	if _, _, jsonOut, err := parseTraceGetArgs([]string{"automation.x", "run1", "-json"}); err != nil || !jsonOut {
		t.Fatalf("parseTraceGetArgs(-json) = jsonOut %v, err %v; want true, nil", jsonOut, err)
	}
}

func TestGlobalUsageMentionsPerCommandHelp(t *testing.T) {
	out := captureStdout(t, printUsage)
	for _, want := range []string{
		"ha-nova update [--version <tag>] [--force]",
		"ha-nova doctor [--auto-repair] [--quiet]",
		"ha-nova pair", // the passwordless pair command must be discoverable
		"Run 'ha-nova <command> --help' to see every flag of a command.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("global usage missing %q:\n%s", want, out)
		}
	}
}
