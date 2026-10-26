package main

import (
	"bytes"
	"strings"
	"testing"
)

// "Claude Code" alone doesn't say which Claude got HA NOVA: setup's done
// block and doctor name the Code tab, and the chat apps guide for the chat
// tab. An update says nothing of it: it shows after every update.
func TestSetupCompleteBannerSaysWhichClaudeGetsHANova(t *testing.T) {
	t.Setenv("HA_NOVA_PLAIN_UI", "1")
	where := setupT("setup.claude.where")
	for _, want := range []string{"claude command", "Code tab", "Claude Desktop", "chat tab", "chat apps", "reachable from the internet"} {
		if !strings.Contains(where, want) {
			t.Fatalf("setup.claude.where %q misses %q", where, want)
		}
	}
	guide := "Chat apps guide: " + chatAppsGuideURL
	with := &bytes.Buffer{}
	renderSetupCompleteBanner(with, []string{"codex", "claude"}, nil)
	if !strings.Contains(with.String(), where) || !strings.Contains(with.String(), guide) {
		t.Fatalf("done block for Claude Code misses which Claude or the guide:\n%s", with)
	}
	without := &bytes.Buffer{}
	renderSetupCompleteBanner(without, []string{"codex"}, nil)
	if strings.Contains(without.String(), where) {
		t.Fatalf("done block without Claude Code says which Claude:\n%s", without)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if lines := doctorClaudeReadyLines(runtimePaths{Home: t.TempDir()}); len(lines) < 2 || lines[0] != where || lines[1] != guide {
		t.Fatalf("doctor's Claude Code lines = %q", lines)
	}
}

func TestPrintUpdateDoneLeavesOutWhichClaude(t *testing.T) {
	t.Setenv("HA_NOVA_PLAIN_UI", "1")
	out := captureStdout(t, func() {
		printUpdateDone("Updated HA NOVA to 0.27.3", postUpdateSyncResult{Synced: []string{"Claude Code", "Codex CLI"}})
	})
	if !strings.Contains(out, "Updated HA NOVA to 0.27.3 in Claude Code and Codex CLI.") || strings.Contains(out, setupT("setup.claude.where")) {
		t.Fatalf("update line = %q, want the summary only", out)
	}
}
