package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// claudeApplyRule lets Claude Code run `ha-nova apply --plan <id>` without
// its own prompt and past auto mode's classifier, so apply can open HA NOVA's
// confirmation window or ask the linked phone: there the human decides. The
// skills write the chat-code form as `ha-nova apply --confirm <code> --plan
// <id>`, which this rule does not match, so a code typed in chat stays under
// Claude Code's own review. Plugins cannot ship permission rules; only the
// user's settings file can hold it, and only after the user says yes.
const claudeApplyRule = "Bash(ha-nova apply --plan *)"

// claudeReadRule lets Claude Code read HA NOVA's own skill files, which the
// plugin loads in place from claude-marketplace — outside every project, so
// each session otherwise asks "Read outside the working directories" (auto
// mode) or before every file (default mode). Read-only and only that folder;
// "" when its path holds a character that would widen the pattern.
func claudeReadRule(paths runtimePaths) string {
	root, err := filepath.Abs(claudeMarketplaceBaseRoot(paths))
	if err != nil {
		return ""
	}
	return claudeReadRuleFor(filepath.ToSlash(root))
}

// claudeReadRuleFor writes an absolute slash path in Claude Code's own rule
// form: /x → //x, C:/x → //c/x.
func claudeReadRuleFor(root string) string {
	if !utf8.ValidString(root) || strings.ContainsFunc(root, func(r rune) bool { return r < 0x20 || r == 0x7f }) ||
		strings.ContainsAny(root, "*?[]{}()!#\\") || strings.HasPrefix(root, "//") { // a pattern character, or a network path
		return ""
	}
	if len(root) > 2 && root[1] == ':' && root[2] == '/' && ('a' <= root[0]|0x20 && root[0]|0x20 <= 'z') {
		root = "/" + strings.ToLower(root[:1]) + root[2:]
	}
	return "Read(/" + root + "/**)"
}

func claudeRules(paths runtimePaths) []string {
	if read := claudeReadRule(paths); read != "" {
		return []string{claudeApplyRule, read}
	}
	return []string{claudeApplyRule}
}

func claudeRulesManual(rules []string) string {
	return "In Claude Code, run /permissions and add these allow rules: " + strings.Join(rules, ", ")
}

func claudeUserSettingsPath(home string) string {
	path := filepath.Join(claudeConfigRoot(home), "settings.json")
	if abs, err := filepath.Abs(path); err == nil { // a relative CLAUDE_CONFIG_DIR: uninstall may run elsewhere
		path = abs
	}
	return path
}

// offerClaudeRules is setup's optional Claude Code step: quiet when the rules
// are there, writes only after "Add rules", and never fails setup. It runs
// before the final banner, which clears the screen, so it returns its outcome
// line for the caller to print after the banner ("" when quiet).
func offerClaudeRules(reader *bufio.Reader, out io.Writer, paths runtimePaths) string {
	path, rules, missing, note := claudeRulesPending(paths)
	if len(missing) == 0 {
		return note
	}
	fmt.Fprintln(out)
	if len(rules) > 1 {
		fmt.Fprintln(out, "  Let Claude Code work with HA NOVA without extra questions?")
	} else {
		fmt.Fprintln(out, "  Let Claude Code open HA NOVA's confirmation window?")
	}
	fmt.Fprintln(out, "  This adds to your Claude Code settings:")
	for _, rule := range missing {
		why := "so Claude Code can open HA NOVA's confirmation window"
		if rule != claudeApplyRule {
			why = "so Claude Code reads HA NOVA's own instructions without asking"
		}
		fmt.Fprintf(out, "    %s — %s\n", rule, why)
	}
	if slices.Contains(missing, claudeApplyRule) {
		fmt.Fprintln(out, "  You still confirm every unlock or delete yourself. Claude Code also stops")
		fmt.Fprintln(out, "  asking before other HA NOVA changes you already approved in chat.")
	}
	fmt.Fprintln(out, "  File: "+path)
	spec := setupMenuSpec{
		Prompt:       "Use ↑/↓ or j/k, Enter to select",
		DefaultValue: "add",
		Options: []setupMenuOption{
			{Value: "add", Label: "Add rules (recommended)"},
			{Value: "skip", Label: "Skip for now"},
		},
	}
	armSetupNextPromptSkipsStaleBlankInput() // an Enter typed during the install never answers this
	defer clearSetupNextPromptSkipsStaleBlankInput()
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		// Without the menu, Enter skips: only a typed "add" consents.
		answer, err := promptLineWithOptions(reader, out, "Type 'add' to add the rules, or press Enter to skip", "", false)
		if err == nil && strings.EqualFold(strings.TrimSpace(answer), "add") {
			return "add", nil
		}
		return "skip", nil
	})
	if err != nil || answer != "add" {
		return "Claude Code rules skipped. To add them later, run ha-nova setup again."
	}
	line, _ := addClaudeRules(paths, path, rules, missing)
	return line
}

// claudeRulesPending: the rules setup may offer to add to path. No missing
// rules: nothing to offer, and note says why ("" when they are all there).
func claudeRulesPending(paths runtimePaths) (path string, rules, missing []string, note string) {
	path = claudeUserSettingsPath(paths.Home)
	rules = claudeRules(paths)
	missing, err := claudeRulesMissing(path, rules)
	if err == nil && len(missing) == 0 {
		return path, rules, nil, ""
	}
	if recorded := claudeRulesRecordFile(loadStateOrDefault(paths)); recorded != "" && !sameFile(recorded, path) {
		// One record per install: never orphan the rules in the other file.
		return path, rules, nil, fmt.Sprintf("Claude Code: HA NOVA's rules are in %s; it left %s unchanged.", recorded, path)
	}
	if err != nil {
		return path, rules, nil, fmt.Sprintf("Claude Code: HA NOVA can't safely edit %s, so it left it unchanged. %s", path, claudeRulesManual(rules))
	}
	return path, rules, missing, ""
}

// addClaudeRules writes the missing rules after the user said yes and
// records them for uninstall; it returns the outcome line and whether the
// rules are in the file now.
func addClaudeRules(paths runtimePaths, path string, rules, missing []string) (string, bool) {
	// Under the lock and only while HA NOVA is installed (an uninstall in
	// another terminal removes state.json), so the rules and their record
	// land together.
	var added []string
	var unrecorded error
	err := withClientMutationLock(paths, func() error {
		state, err := loadState(paths)
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("HA NOVA was uninstalled meanwhile")
		}
		if err != nil {
			return err
		}
		if recorded := claudeRulesRecordFile(state); recorded != "" && !sameFile(recorded, path) { // another setup recorded meanwhile
			return fmt.Errorf("HA NOVA's rules are recorded in %s", recorded)
		}
		var created string
		added, created, err = changeClaudeRules(path, missing, true, "")
		if err != nil || len(added) == 0 {
			return err
		}
		state.ClaudeRules = recordedClaudeRules(state)
		for _, rule := range added {
			if !slices.Contains(state.ClaudeRules, rule) {
				state.ClaudeRules = append(state.ClaudeRules, rule)
			}
		}
		state.ClaudeRulesFile, state.ClaudeApplyRuleFile = path, ""
		if created != "" {
			state.ClaudeAllowCreated = created
		}
		unrecorded = saveState(paths, state)
		return nil
	})
	switch {
	case err != nil:
		return fmt.Sprintf("Claude Code rules: nothing was written (%s). %s", err, claudeRulesManual(missing)), false
	case len(added) == 0:
		return "", true
	case unrecorded != nil:
		return fmt.Sprintf("Claude Code rules added, but HA NOVA couldn't note them (%s): uninstall will leave them in place.", unrecorded), true
	}
	count := "1 rule"
	if len(added) > 1 {
		count = fmt.Sprintf("%d rules", len(added))
	}
	return "Added " + count + " for HA NOVA to your Claude Code settings." + claudeNoReadRuleNote(rules), true
}

// claudeNoReadRuleNote: without a read rule, Claude Code keeps asking before
// reading HA NOVA's files; say so instead of promising otherwise.
func claudeNoReadRuleNote(rules []string) string {
	if len(rules) > 1 {
		return ""
	}
	return " Claude Code may still ask before it reads HA NOVA's files. That's normal; just allow it."
}

// claudeRulesRecordFile is the settings file setup recorded adding rules to.
func claudeRulesRecordFile(state installState) string {
	if state.ClaudeRulesFile != "" {
		return state.ClaudeRulesFile
	}
	return state.ClaudeApplyRuleFile
}

// recordedClaudeRules: the rules setup added; a release-candidate record
// (claude_apply_rule_file) is the apply rule only.
func recordedClaudeRules(state installState) []string {
	switch {
	case state.ClaudeRulesFile != "":
		return state.ClaudeRules
	case state.ClaudeApplyRuleFile != "":
		return []string{claudeApplyRule}
	}
	return nil
}

func sameFile(a, b string) bool {
	if a == b {
		return true
	}
	ia, errA := os.Stat(a)
	ib, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

// claudeRulesHint is doctor's line while a rule is missing ("" once all
// are there).
func claudeRulesHint(paths runtimePaths) string {
	const why = "Optional: if Claude Code asks before reading HA NOVA's files, or its auto mode blocks HA NOVA's confirmation window, "
	rules := claudeRules(paths)
	switch missing, err := claudeRulesMissing(claudeUserSettingsPath(paths.Home), rules); {
	case err != nil:
		return why + "add the rules yourself. " + claudeRulesManual(rules)
	case len(missing) == 0 && len(rules) > 1:
		return ""
	case len(missing) == 0:
		return strings.TrimSpace(claudeNoReadRuleNote(rules))
	default:
		return why + "run `ha-nova setup` and accept its rules. " + claudeRulesManual(missing)
	}
}

// removeClaudeRulesForUninstall removes only the rules setup recorded adding,
// from the file it recorded; a rule the user added themselves is never
// recorded. permissions.allow (or permissions) goes too once empty, but only
// when setup created it.
func removeClaudeRulesForUninstall(state installState, report *uninstallReport) {
	file, rules := claudeRulesRecordFile(state), recordedClaudeRules(state)
	if file == "" || len(rules) == 0 {
		return
	}
	prune := ""
	if state.ClaudeRulesFile != "" {
		prune = state.ClaudeAllowCreated
	}
	removed, _, err := changeClaudeRules(file, rules, false, prune)
	if err != nil {
		report.addNote(fmt.Sprintf("Could not remove the Claude Code rules %s from %s (%s); remove them in /permissions.", strings.Join(rules, ", "), file, err))
		return
	}
	for _, rule := range removed {
		report.addRemoved("Claude Code rule " + rule)
	}
}

// claudeRulesMissing returns the rules the settings file lacks.
func claudeRulesMissing(path string, rules []string) ([]string, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return rules, nil
	}
	if err != nil {
		return nil, err
	}
	_, missing, _, err := editClaudeAllow(data, rules, true, "")
	return missing, err
}

// changeClaudeRules adds or removes the rules in place: through a symlink,
// keeping the file's mode, atomically, and only if nothing else wrote the
// file in between. It returns the rules it changed and, on add, which key it
// created ("permissions", "allow", or "").
func changeClaudeRules(path string, rules []string, add bool, prune string) ([]string, string, error) {
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil { // a dangling dotfiles link: never replace it with a file
			return nil, "", fmt.Errorf("%s is a link HA NOVA can't follow", path)
		}
		path = target
	}
	before, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	mode := os.FileMode(0o644)
	switch {
	case missing && !add:
		return nil, "", nil
	case err != nil && !missing:
		return nil, "", err
	case !missing:
		info, err := os.Stat(path)
		if err != nil {
			return nil, "", err
		}
		mode = info.Mode().Perm()
	}
	after, changed, created, err := editClaudeAllow(before, rules, add, prune)
	if err != nil || len(changed) == 0 {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", err
	}
	// ponytail: compare-then-rename leaves a tiny window for a concurrent
	// Claude Code write; Claude Code honors no lock HA NOVA could take.
	now, err := os.ReadFile(path)
	if missing != errors.Is(err, os.ErrNotExist) || (!missing && (err != nil || !bytes.Equal(now, before))) {
		return nil, "", errors.New("the settings file changed while HA NOVA was reading it")
	}
	return changed, created, writeFileAtomic(path, after, mode)
}
