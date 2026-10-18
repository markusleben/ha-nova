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

// offerAnswer returns what the offer printed plus its outcome line (which
// setup prints after the banner).
func offerAnswer(t *testing.T, paths runtimePaths, input string) string {
	t.Helper()
	var out bytes.Buffer
	note := offerClaudeRules(bufio.NewReader(strings.NewReader(input)), &out, paths)
	if note != "" {
		return out.String() + "NOTE: " + note
	}
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
	read := claudeReadRule(paths)
	if read != claudeReadRuleFor(filepath.ToSlash(filepath.Join(paths.ConfigDir, "claude-marketplace"))) || !strings.HasPrefix(read, "Read(//") {
		t.Fatalf("read rule = %q", read)
	}

	out := offerAnswer(t, paths, "add\n")
	if !strings.Contains(out, "This adds to your Claude Code settings:") ||
		!strings.Contains(out, "    Bash(ha-nova apply --plan *) — so Claude Code can open HA NOVA's confirmation window") ||
		!strings.Contains(out, "    "+read+" — so Claude Code reads HA NOVA's own instructions without asking") ||
		!strings.Contains(out, "You still confirm every unlock or delete yourself.") ||
		!strings.Contains(out, "asking before other HA NOVA changes you already approved in chat.") ||
		strings.Count(out, settings) != 1 || !strings.Contains(out, "NOTE: Added to your Claude Code settings: ") {
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
            "Bash(ha-nova apply --plan *)",
            "` + read + `"
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
	state := loadStateOrDefault(paths)
	if state.ClaudeRulesFile != settings || state.ClaudeApplyRuleFile != "" || strings.Join(state.ClaudeRules, "|") != claudeApplyRule+"|"+read || state.ClaudeAllowCreated != "" {
		t.Fatalf("state records %q %q %q %q", state.ClaudeRulesFile, state.ClaudeApplyRuleFile, state.ClaudeRules, state.ClaudeAllowCreated)
	}
}

func TestClaudeReadRuleUsesClaudeCodesPathForm(t *testing.T) {
	for root, want := range map[string]string{
		"/Users/max/.config/ha-nova/claude-marketplace":                "Read(//Users/max/.config/ha-nova/claude-marketplace/**)",
		"C:/Users/Max Muster/AppData/Local/ha-nova/claude-marketplace": "Read(//c/Users/Max Muster/AppData/Local/ha-nova/claude-marketplace/**)",
		"C:/Users/Max (Work)/AppData/Local/ha-nova/claude-marketplace": "",
		"/home/a*b/.config/ha-nova/claude-marketplace":                 "",
		"//server/share/ha-nova/claude-marketplace":                    "",
		"/home/a\nb/.config/ha-nova/claude-marketplace":                "",
		"/home/a\xffb/.config/ha-nova/claude-marketplace":              "",
		"/:x/ha-nova/claude-marketplace":                               "Read(//:x/ha-nova/claude-marketplace/**)",
	} {
		if got := claudeReadRuleFor(root); got != want {
			t.Errorf("%s: %q, want %q", root, got, want)
		}
	}
}

// An install recorded before the read rule (0.26.0-rc4) adds only that rule
// and keeps the apply rule on its record.
func TestClaudeRulesAddOnlyTheMissingOne(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["Bash(ha-nova apply --plan *)"]}}`)
	state := loadStateOrDefault(paths)
	state.ClaudeApplyRuleFile = settings
	if err := saveState(paths, state); err != nil {
		t.Fatal(err)
	}
	out := offerAnswer(t, paths, "add\n")
	if strings.Contains(out, "ha-nova apply --plan *) —") || strings.Contains(out, "You still confirm") || !strings.Contains(out, "NOTE: Added to your Claude Code settings: ") {
		t.Fatalf("offer output:\n%s", out)
	}
	if got := loadStateOrDefault(paths); strings.Join(got.ClaudeRules, "|") != claudeApplyRule+"|"+claudeReadRule(paths) || got.ClaudeRulesFile != settings || got.ClaudeApplyRuleFile != "" {
		t.Fatalf("recorded %+v", got)
	}
	// The user removes the apply rule; setup adds it back without a second record.
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["`+claudeReadRule(paths)+`"]}}`)
	if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "NOTE: Added to your Claude Code settings: "+claudeApplyRule) {
		t.Fatalf("re-add:\n%s", out)
	}
	if got := loadStateOrDefault(paths).ClaudeRules; len(got) != 2 {
		t.Fatalf("recorded twice: %q", got)
	}
}

// Rules recorded for one settings file are never orphaned by a setup run
// that sees another (a different CLAUDE_CONFIG_DIR).
func TestClaudeRulesStayWithTheRecordedFile(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	state := loadStateOrDefault(paths)
	state.ClaudeRulesFile, state.ClaudeRules = filepath.Join(paths.Home, "other", "settings.json"), []string{claudeApplyRule}
	if err := saveState(paths, state); err != nil {
		t.Fatal(err)
	}
	if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "NOTE: Claude Code: HA NOVA's rules are in ") || strings.Contains(out, "Add rules") {
		t.Fatalf("offer:\n%s", out)
	}
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Fatalf("wrote the second file: %v", err)
	}
}

func TestClaudeApplyRuleIsIdempotent(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "Added to your Claude Code settings") {
		t.Fatalf("first offer:\n%s", out)
	}
	first := readClaudeSettings(t, settings)
	if first != "{\n  \"permissions\": {\n    \"allow\": [\n      \"Bash(ha-nova apply --plan *)\",\n      \""+claudeReadRule(paths)+"\"\n    ]\n  }\n}\n" {
		t.Fatalf("new settings file:\n%s", first)
	}
	if got := loadStateOrDefault(paths).ClaudeAllowCreated; got != "permissions" {
		t.Fatalf("created = %q", got)
	}
	if out := offerAnswer(t, paths, "add\n"); out != "" {
		t.Fatalf("second offer should stay quiet, got:\n%s", out)
	}
	if added, _, err := changeClaudeRules(settings, claudeRules(paths), true, ""); added != nil || err != nil {
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
		if !strings.Contains(out, "NOTE: Claude Code rules skipped.") {
			t.Fatalf("input %q: offer output:\n%s", input, out)
		}
		if got := readClaudeSettings(t, settings); got != existingClaudeSettings {
			t.Fatalf("input %q wrote the settings:\n%s", input, got)
		}
		if st := loadStateOrDefault(paths); st.ClaudeRulesFile != "" || st.ClaudeRules != nil {
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
		if !strings.HasPrefix(out, "NOTE: ") || !strings.Contains(out, "left it unchanged") || !strings.Contains(out, claudeRulesManual(claudeRules(paths))) {
			t.Fatalf("%s: offer output:\n%s", content, out)
		}
		if _, _, err := changeClaudeRules(settings, claudeRules(paths), true, ""); err == nil {
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
	if added, _, err := changeClaudeRules(settings, []string{claudeApplyRule}, true, ""); len(added) != 1 || err != nil {
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
	if added, _, err := changeClaudeRules(settings, []string{claudeApplyRule}, true, ""); added != nil || err == nil {
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
	if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "nothing was written") {
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
	if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "Added to your Claude Code settings") {
		t.Fatalf("offer:\n%s", out)
	}
	report := &uninstallReport{}
	removeClaudeRulesForUninstall(loadStateOrDefault(paths), report)
	got := readClaudeSettings(t, settings)
	if strings.Contains(got, "ha-nova apply") || strings.Contains(got, "claude-marketplace") || !strings.Contains(got, `"Bash(npm test && npm run lint)"`) ||
		!strings.Contains(got, `"Bash(rm -rf *)"`) || !strings.Contains(got, `"Bash(git push *)"`) || !strings.Contains(got, `"model": "opus"`) {
		t.Fatalf("after uninstall:\n%s", got)
	}
	if len(report.removed) != 2 || report.removed[0] != "Claude Code rule "+claudeApplyRule || report.removed[1] != "Claude Code rule "+claudeReadRule(paths) {
		t.Fatalf("report = %+v", report)
	}

	// Again, or with nothing recorded: the user's own identical rule stays.
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["Bash(ha-nova apply --plan *)"]}}`)
	removeClaudeRulesForUninstall(installState{}, report)
	if !strings.Contains(readClaudeSettings(t, settings), claudeApplyRule) {
		t.Fatal("removed a rule HA NOVA did not record adding")
	}
	// A record from before claude_rules existed removes the apply rule only.
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["Bash(ha-nova apply --plan *)","`+claudeReadRule(paths)+`"]}}`)
	removeClaudeRulesForUninstall(installState{ClaudeApplyRuleFile: settings}, report)
	if got := readClaudeSettings(t, settings); strings.Contains(got, "ha-nova apply") || !strings.Contains(got, "claude-marketplace") {
		t.Fatalf("legacy record removal:\n%s", got)
	}
	// A recorded file that is gone or unreadable never fails uninstall.
	writeClaudeSettings(t, settings, "{oops")
	removeClaudeRulesForUninstall(installState{ClaudeApplyRuleFile: settings}, report)
	if len(report.notes) != 1 || !strings.Contains(report.notes[0], "/permissions") || readClaudeSettings(t, settings) != "{oops" {
		t.Fatalf("notes = %v", report.notes)
	}
}

// Uninstall drops permissions.allow (or permissions) again only when setup
// created it; a user's own empty list stays.
func TestUninstallRemovesOnlyTheKeysSetupCreated(t *testing.T) {
	for _, tc := range []struct{ before, after string }{
		{"", "{}\n"},
		{`{"model": "x"}`, "{\n  \"model\": \"x\"\n}\n"},
		{`{"permissions": {"deny": ["Bash(rm *)"]}}`, "{\n  \"permissions\": {\n    \"deny\": [\n      \"Bash(rm *)\"\n    ]\n  }\n}\n"},
		{`{"permissions": {"allow": []}}`, "{\n  \"permissions\": {\n    \"allow\": []\n  }\n}\n"},
	} {
		paths, settings := claudeRuleTestPaths(t)
		if tc.before != "" {
			writeClaudeSettings(t, settings, tc.before)
		}
		if out := offerAnswer(t, paths, "add\n"); !strings.Contains(out, "Added to your Claude Code settings") {
			t.Fatalf("%q: offer:\n%s", tc.before, out)
		}
		removeClaudeRulesForUninstall(loadStateOrDefault(paths), &uninstallReport{})
		if got := readClaudeSettings(t, settings); got != tc.after {
			t.Errorf("%q: after uninstall:\n%s\nwant:\n%s", tc.before, got, tc.after)
		}
	}
}

func TestClaudeApplyRuleHintNeverWrites(t *testing.T) {
	paths, settings := claudeRuleTestPaths(t)
	if hint := claudeRulesHint(paths); !strings.Contains(hint, "ha-nova setup") || !strings.Contains(hint, claudeApplyRule) || !strings.Contains(hint, claudeReadRule(paths)) {
		t.Fatalf("hint = %q", hint)
	}
	if _, err := os.Stat(settings); !os.IsNotExist(err) {
		t.Fatalf("hint created the settings file: %v", err)
	}
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["Bash(ha-nova apply --plan *)"]}}`)
	if hint := claudeRulesHint(paths); strings.Contains(hint, claudeApplyRule) || !strings.Contains(hint, claudeReadRule(paths)) {
		t.Fatalf("hint with only the apply rule = %q", hint)
	}
	writeClaudeSettings(t, settings, `{"permissions":{"allow":["Bash(ha-nova apply --plan *)","`+claudeReadRule(paths)+`"]}}`)
	if hint := claudeRulesHint(paths); hint != "" {
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
