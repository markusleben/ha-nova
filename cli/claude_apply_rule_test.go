package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claudeRuleTestPaths isolates Claude Code's root and HA NOVA's state in
// temp dirs: these tests never touch the real ~/.claude.
func claudeRuleTestPaths(t *testing.T) (runtimePaths, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, "claude-root"))
	config := filepath.Join(home, "ha-nova")
	if err := os.MkdirAll(config, 0o700); err != nil {
		t.Fatal(err)
	}
	paths := runtimePaths{Home: home, ConfigDir: config, StateFile: filepath.Join(config, "state.json")}
	if err := saveState(paths, defaultInstallState()); err != nil { // an installed HA NOVA
		t.Fatal(err)
	}
	return paths, claudeUserSettingsPath(home)
}

func writeClaudeSettings(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readClaudeSettings(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func offerAnswer(t *testing.T, paths runtimePaths, input string) string {
	t.Helper()
	var out bytes.Buffer
	offerClaudeApplyRule(bufio.NewReader(strings.NewReader(input)), &out, paths)
	return out.String()
}

const existingClaudeSettings = `{
    "model": "opus",
    "permissions": {
        "deny": ["Bash(rm -rf *)"],
        "allow": ["Bash(npm test && npm run lint)", "Read(~/café)"],
        "ask": ["Bash(git push *)"]
    },
    "enabledPlugins": {"ha-nova@ha-nova": true}
}
`

func TestClaudeApplyRuleMergesIntoExistingSettings(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	writeClaudeSettings(t, settings, existingClaudeSettings)

	out := offerAnswer(t, paths, "add\n")
	if !strings.Contains(out, "This adds one rule to your Claude Code settings: Bash(ha-nova apply --plan *)") ||
		!strings.Contains(out, "You still confirm every unlock or delete yourself.") ||
		!strings.Contains(out, "asking before other HA NOVA changes you already approved in chat.") ||
		strings.Count(out, settings) != 1 || !strings.Contains(out, "Added.") {
		t.Fatalf("offer output:\n%s", out)
	}
	want := `{
    "model": "opus",
    "permissions": {
        "deny": [
            "Bash(rm -rf *)"
        ],
        "allow": [
            "Bash(npm test && npm run lint)",
            "Read(~/café)",
            "Bash(ha-nova apply --plan *)"
        ],
        "ask": [
            "Bash(git push *)"
        ]
    },
    "enabledPlugins": {
        "ha-nova@ha-nova": true
    }
}
`
	if got := readClaudeSettings(t, settings); got != want {
		t.Fatalf("settings after add:\n%s\nwant:\n%s", got, want)
	}
	if info, _ := os.Stat(settings); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want the file's own 0600", info.Mode().Perm())
	}
	if state := loadStateOrDefault(paths); state.ClaudeApplyRuleFile != settings {
		t.Fatalf("state records %q, want %q", state.ClaudeApplyRuleFile, settings)
	}
}

func TestClaudeApplyRuleIsIdempotent(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "Added.") {
		t.Fatalf("first offer:\n%s", out)
	}
	first := readClaudeSettings(t, settings)
	if first != "{\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ha-nova apply --plan *)\"\n    ]\n  }\n}\n" {
		t.Fatalf("new settings file:\n%s", first)
	}
	if out := offerAnswer(t, paths, "add\n"); out != "" {
		t.Fatalf("second offer should stay quiet, got:\n%s", out)
	}
	if added, err := changeClaudeApplyRule(settings, true); added || err != nil {
		t.Fatalf("second add = %v, %v", added, err)
	}
	if got := readClaudeSettings(t, settings); got != first {
		t.Fatalf("re-run changed the file:\n%s", got)
	}
}

func TestClaudeApplyRuleNeedsConsent(t *testing.T) {
	for _, input := range []string{"\n", "", "yes\n", "y\n", "skip\n"} {
		paths, settings := claudeRuleTestPaths(t)
		writeClaudeSettings(t, settings, existingClaudeSettings)
		out := offerAnswer(t, paths, input)
		if !strings.Contains(out, "Skipped.") {
			t.Fatalf("input %q: offer output:\n%s", input, out)
		}
		if got := readClaudeSettings(t, settings); got != existingClaudeSettings {
			t.Fatalf("input %q wrote the settings:\n%s", input, got)
		}
		if loadStateOrDefault(paths).ClaudeApplyRuleFile != "" {
			t.Fatalf("input %q recorded a rule", input)
		}
	}
}

func TestClaudeApplyRuleLeavesUnreadableSettingsAlone(t *testing.T) {
	for _, content := range []string{
		`{"permissions": {"allow": [}`,          // not JSON
		`["Bash(ls)"]`,                          // not an object
		`{"permissions": []}`,                   // permissions not an object
		`{"permissions": {"allow": "Bash(*)"}}`, // allow not a list
		`{"permissions": {"allow": null}}`,
		`{"a": 1, "a": 2}`, // duplicate keys
		`{} {}`,
	} {
		paths, settings := claudeRuleTestPaths(t)
		writeClaudeSettings(t, settings, content)
		out := offerAnswer(t, paths, "add\n")
		if !strings.Contains(out, "left it unchanged") || !strings.Contains(out, claudeApplyRuleManual) || strings.Contains(out, "Add rule") {
			t.Fatalf("%s: offer output:\n%s", content, out)
		}
		if _, err := changeClaudeApplyRule(settings, true); err == nil {
			t.Fatalf("%s: add succeeded", content)
		}
		if got := readClaudeSettings(t, settings); got != content {
			t.Fatalf("%s: file changed to %s", content, got)
		}
	}
}

func TestClaudeApplyRuleWritesThroughSymlink(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	target := filepath.Join(paths.Home, "dotfiles", "settings.json")
	writeClaudeSettings(t, target, "{}\n")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, settings); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if added, err := changeClaudeApplyRule(settings, true); !added || err != nil {
		t.Fatalf("add = %v, %v", added, err)
	}
	if info, err := os.Lstat(settings); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink replaced: %v", err)
	}
	if !strings.Contains(readClaudeSettings(t, target), claudeApplyRule) {
		t.Fatal("rule missing in the symlink target")
	}
}

func TestClaudeApplyRuleKeepsDanglingSymlink(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(paths.Home, "missing", "settings.json"), settings); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if added, err := changeClaudeApplyRule(settings, true); added || err == nil {
		t.Fatalf("add through a dangling link = %v, %v", added, err)
	}
	if info, err := os.Lstat(settings); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dangling link replaced: %v", err)
	}
}

func TestClaudeApplyRuleNotWrittenAfterUninstall(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	if err := os.Remove(paths.StateFile); err != nil { // uninstall finished while the menu waited
		t.Fatal(err)
	}
	if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "Nothing was written") {
		t.Fatalf("offer output:\n%s", out)
	}
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Fatalf("settings written after uninstall: %v", err)
	}
	if _, err := os.Stat(paths.StateFile); !os.IsNotExist(err) {
		t.Fatalf("state.json recreated after uninstall: %v", err)
	}
}

func TestUninstallRemovesOnlyTheRecordedRule(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	writeClaudeSettings(t, settings, existingClaudeSettings)
	if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "Added.") {
		t.Fatalf("offer:\n%s", out)
	}
	report := &uninstallReport{}
	removeClaudeApplyRuleForUninstall(loadStateOrDefault(paths), report)
	got := readClaudeSettings(t, settings)
	if strings.Contains(got, "ha-nova apply") || !strings.Contains(got, `"Bash(npm test && npm run lint)"`) ||
		!strings.Contains(got, `"Bash(rm -rf *)"`) || !strings.Contains(got, `"Bash(git push *)"`) || !strings.Contains(got, `"model": "opus"`) {
		t.Fatalf("after uninstall:\n%s", got)
	}
	if len(report.removed) != 1 || report.removed[0] != "Claude Code rule "+claudeApplyRule {
		t.Fatalf("report = %+v", report)
	}

	// Again, or with nothing recorded: the user's own identical rule stays.
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["Bash(ha-nova apply --plan *)"]}}`)
	removeClaudeApplyRuleForUninstall(installState{}, report)
	if !strings.Contains(readClaudeSettings(t, settings), claudeApplyRule) {
		t.Fatal("removed a rule HA NOVA did not record adding")
	}
	// A recorded file that is gone or unreadable never fails uninstall.
	writeClaudeSettings(t, settings, "{oops")
	removeClaudeApplyRuleForUninstall(installState{ClaudeApplyRuleFile: settings}, report)
	if len(report.notes) != 1 || !strings.Contains(report.notes[0], "/permissions") || readClaudeSettings(t, settings) != "{oops" {
		t.Fatalf("notes = %v", report.notes)
	}
}

func TestClaudeApplyRuleHintNeverWrites(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	if hint := claudeApplyRuleHint(paths.Home); !strings.Contains(hint, "ha-nova setup") || !strings.Contains(hint, claudeApplyRule) {
		t.Fatalf("hint = %q", hint)
	}
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Fatalf("hint created the settings file: %v", err)
	}
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["Bash(ha-nova apply --plan *)"]}}`)
	if hint := claudeApplyRuleHint(paths.Home); hint != "" {
		t.Fatalf("hint with the rule present = %q", hint)
	}
}

// The chat-code form the skills use must stay outside the rule: Claude Code
// matches everything before the rule's trailing " *" as a literal prefix.
func TestClaudeApplyRuleLeavesChatCodeFormOutside(t *testing.T) {
	prefix := strings.TrimSuffix(strings.TrimPrefix(claudeApplyRule, "Bash("), " *)")
	if !strings.HasPrefix("ha-nova apply --plan p_0123456789ab", prefix) ||
		strings.HasPrefix("ha-nova apply --confirm 1234 --plan p_0123456789ab", prefix) {
		t.Fatalf("rule prefix %q", prefix)
	}
}
