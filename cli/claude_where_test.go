package main

import (
	"bytes"
	"strings"
	"testing"
)

// "Claude Code" alone doesn't say which Claude got HA NOVA: the done block
// and the update line name the Code tab, and the chat tab's way in.
func TestSetupCompleteBannerSaysWhichClaudeGetsHANova(t *testing.T) {
	t.Setenv("HA_NOVA_PLAIN_UI", "1")
	where := setupT("setup.claude.where")
	for _, want := range []string{"claude command", "Code tab", "Claude Desktop", "chat tab", "chat apps", "Add to Claude"} {
		if !strings.Contains(where, want) {
			t.Fatalf("setup.claude.where %q misses %q", where, want)
		}
	}
	with := &bytes.Buffer{}
	renderSetupCompleteBanner(with, []string{"codex", "claude"}, nil)
	if !strings.Contains(with.String(), where) {
		t.Fatalf("done block for Claude Code misses which Claude:\n%s", with)
	}
	without := &bytes.Buffer{}
	renderSetupCompleteBanner(without, []string{"codex"}, nil)
	if strings.Contains(without.String(), where) {
		t.Fatalf("done block without Claude Code says which Claude:\n%s", without)
	}
}

func TestPrintUpdateDoneSaysWhichClaudeGetsHANova(t *testing.T) {
	t.Setenv("HA_NOVA_PLAIN_UI", "1")
	where := setupT("setup.claude.where")
	claude := captureStdout(t, func() {
		printUpdateDone("Updated HA NOVA to 0.27.3", postUpdateSyncResult{Synced: []string{"Claude Code", "Codex CLI"}})
	})
	if !strings.Contains(claude, "Updated HA NOVA to 0.27.3 in Claude Code and Codex CLI.") || !strings.Contains(claude, where) {
		t.Fatalf("update line for Claude Code = %q, want the summary and which Claude", claude)
	}
	codex := captureStdout(t, func() {
		printUpdateDone("Updated HA NOVA to 0.27.3", postUpdateSyncResult{Synced: []string{"Codex CLI"}})
	})
	if strings.Contains(codex, where) {
		t.Fatalf("update line without Claude Code says which Claude: %q", codex)
	}
}
