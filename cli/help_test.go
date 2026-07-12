package main

import (
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
		{"relay (parent)", func() int { return runRelayCommand(paths, []string{"--help"}) }, "Usage: ha-nova relay <health|ws|core|files|jq|version>", "relay <subcommand> --help"},
		{"relay jq", func() int { return runRelayCommand(paths, []string{"jq", "--help"}) }, "Usage: ha-nova relay jq", "--jq-file"},
		// Must answer even on a fresh install: help may not hide behind the
		// config/token preflight of the proxy path.
		{"relay core", func() int { return runRelayCommand(paths, []string{"core", "--help"}) }, "Usage: ha-nova relay core", "-body-file"},
		{"relay ws", func() int { return runRelayCommand(paths, []string{"ws", "--help"}) }, "Usage: ha-nova relay ws", "-data-file"},
		{"relay version", func() int { return runRelayCommand(paths, []string{"version", "--help"}) }, "Usage: ha-nova relay version", "No flags"},
		{"version (root)", func() int { return dispatch(paths, "ha-nova", []string{"version", "--help"}) }, "Usage: ha-nova version", "No flags"},
		{"trace (parent)", func() int { return runTraceCommand(paths, []string{"--help"}) }, "Usage: ha-nova trace <latest|list|get>", "trace <subcommand> --help"},
		{"snapshot (parent)", func() int { return runSnapshotCommand(paths, []string{"--help"}) }, "Usage: ha-nova snapshot <save|show|verify>", "snapshot <subcommand> --help"},
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

func TestGlobalUsageMentionsPerCommandHelp(t *testing.T) {
	out := captureStdout(t, printUsage)
	for _, want := range []string{
		"ha-nova update [--version <tag>] [--force]",
		"ha-nova doctor [--auto-repair] [--quiet]",
		"Run 'ha-nova <command> --help' to see every flag of a command.",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("global usage missing %q:\n%s", want, out)
		}
	}
}
