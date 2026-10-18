package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// claudeApplyRule lets Claude Code run `ha-nova apply --plan <id>` without
// its own prompt and past auto mode's classifier, so apply can open HA NOVA's
// confirmation window or ask the linked phone: there the human decides. The
// skills write the chat-code form as `ha-nova apply --confirm <code> --plan
// <id>`, which this rule does not match, so a code typed in chat stays under
// Claude Code's own review. Plugins cannot ship permission rules; only the
// user's settings file can hold it, and only after the user says yes.
const claudeApplyRule = "Bash(ha-nova apply --plan *)"

const claudeApplyRuleManual = "In Claude Code, run /permissions and add this allow rule: " + claudeApplyRule

var claudeApplyRuleJSON = []byte(`"` + claudeApplyRule + `"`)

func claudeUserSettingsPath(home string) string {
	path := filepath.Join(claudeConfigRoot(home), "settings.json")
	if abs, err := filepath.Abs(path); err == nil { // a relative CLAUDE_CONFIG_DIR: uninstall may run elsewhere
		path = abs
	}
	return path
}

// offerClaudeApplyRule is setup's optional Claude Code step: quiet when the
// rule is there, writes only after "Add rule", and never fails setup.
func offerClaudeApplyRule(reader *bufio.Reader, out io.Writer, paths runtimePaths) {
	path := claudeUserSettingsPath(paths.Home)
	present, err := claudeApplyRulePresent(path)
	if present {
		return
	}
	fmt.Fprintln(out)
	if err != nil {
		fmt.Fprintf(out, "  HA NOVA can't read %s, so it won't change it.\n", path)
		fmt.Fprintln(out, "  To let Claude Code open HA NOVA's confirmation window: "+claudeApplyRuleManual)
		return
	}
	fmt.Fprintln(out, "  Let Claude Code open HA NOVA's confirmation window?")
	fmt.Fprintln(out, "  This adds one rule to your Claude Code settings: "+claudeApplyRule)
	fmt.Fprintln(out, "  You still confirm every unlock or delete yourself.")
	fmt.Fprintln(out, "  File: "+path)
	spec := setupMenuSpec{
		Prompt:       "Use ↑/↓ or j/k, Enter to select",
		DefaultValue: "add",
		Options: []setupMenuOption{
			{Value: "add", Label: "Add rule (recommended)"},
			{Value: "skip", Label: "Skip"},
		},
	}
	answer, err := promptSetupMenu(reader, out, spec, func() (string, error) {
		// Without the menu, Enter skips: only a typed "add" consents.
		answer, err := promptLineWithOptions(reader, out, "Type 'add' to add the rule, or press Enter to skip", "", false)
		if err == nil && strings.EqualFold(strings.TrimSpace(answer), "add") {
			return "add", nil
		}
		return "skip", nil
	})
	if err != nil || answer != "add" {
		fmt.Fprintln(out, "  Skipped. To add it later, run ha-nova setup again.")
		return
	}
	added, err := changeClaudeApplyRule(path, true)
	if err != nil {
		fmt.Fprintf(out, "  Nothing was written: %s. %s\n", err, claudeApplyRuleManual)
		return
	}
	if added {
		if err := withClientMutationLock(paths, func() error {
			state, err := loadStateOrDefaultChecked(paths)
			if err != nil {
				return err
			}
			state.ClaudeApplyRuleFile = path
			return saveState(paths, state)
		}); err != nil {
			// The rule stays; uninstall just won't know to remove it.
			fmt.Fprintf(out, "  Added, but HA NOVA couldn't note it (%s): uninstall will leave it in place.\n", err)
			return
		}
	}
	fmt.Fprintln(out, "  Added. Claude Code can now open HA NOVA's confirmation window.")
}

// claudeApplyRuleHint is doctor's line while the rule is missing ("" once
// it is there).
func claudeApplyRuleHint(home string) string {
	switch present, err := claudeApplyRulePresent(claudeUserSettingsPath(home)); {
	case err != nil:
		return "Claude Code: to let auto mode open HA NOVA's confirmation window: " + claudeApplyRuleManual
	case present:
		return ""
	}
	return "Claude Code: to let auto mode open HA NOVA's confirmation window, run `ha-nova setup` and choose “Add rule”. Or: " + claudeApplyRuleManual
}

// removeClaudeApplyRuleForUninstall removes the rule only from the file setup
// recorded writing it to; a rule the user added themselves is never recorded.
func removeClaudeApplyRuleForUninstall(state installState, report *uninstallReport) {
	if state.ClaudeApplyRuleFile == "" {
		return
	}
	removed, err := changeClaudeApplyRule(state.ClaudeApplyRuleFile, false)
	switch {
	case err != nil:
		report.addNote(fmt.Sprintf("Could not remove the Claude Code rule %s from %s (%s); remove it in /permissions.", claudeApplyRule, state.ClaudeApplyRuleFile, err))
	case removed:
		report.addRemoved("Claude Code rule " + claudeApplyRule)
	}
}

func claudeApplyRulePresent(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_, changed, err := editClaudeAllow(data, true)
	return err == nil && !changed, err
}

// changeClaudeApplyRule adds or removes the rule in place: through a
// symlink, keeping the file's mode, atomically, and only if nothing else
// wrote the file in between.
func changeClaudeApplyRule(path string, add bool) (bool, error) {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	before, err := os.ReadFile(path)
	missing := errors.Is(err, os.ErrNotExist)
	mode := os.FileMode(0o644)
	switch {
	case missing && !add:
		return false, nil
	case err != nil && !missing:
		return false, err
	case !missing:
		info, err := os.Stat(path)
		if err != nil {
			return false, err
		}
		mode = info.Mode().Perm()
	}
	after, changed, err := editClaudeAllow(before, add)
	if err != nil || !changed {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	// ponytail: compare-then-rename leaves a tiny window for a concurrent
	// Claude Code write; Claude Code honors no lock HA NOVA could take.
	now, err := os.ReadFile(path)
	if missing != errors.Is(err, os.ErrNotExist) || (!missing && (err != nil || !bytes.Equal(now, before))) {
		return false, errors.New("the settings file changed while HA NOVA was reading it")
	}
	return true, writeFileAtomic(path, after, mode)
}

var errClaudeSettingsShape = errors.New("the settings file is not valid JSON in the shape Claude Code uses")

var jsonIndentPattern = regexp.MustCompile(`\n([ \t]+)\S`)

// editClaudeAllow adds (once) or removes (one byte-identical copy of) the
// rule in permissions.allow. Key order, every other key, deny and ask rules,
// and each other value's bytes stay; the file keeps its own indent.
func editClaudeAllow(data []byte, add bool) ([]byte, bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		data = []byte("{}\n")
	}
	data, err := strictJSONBytes(data, "Claude Code settings")
	if err != nil {
		return nil, false, errClaudeSettingsShape
	}
	root, err := parseOrderedJSONObject(data)
	if err != nil {
		return nil, false, err
	}
	perms := &orderedJSONObject{vals: map[string]json.RawMessage{}}
	if raw, ok := root.vals["permissions"]; ok {
		if perms, err = parseOrderedJSONObject(raw); err != nil {
			return nil, false, err
		}
	}
	var allow []json.RawMessage
	if raw, ok := perms.vals["allow"]; ok {
		if json.Unmarshal(raw, &allow) != nil || allow == nil {
			return nil, false, errClaudeSettingsShape
		}
	}
	at := slices.IndexFunc(allow, func(r json.RawMessage) bool { return bytes.Equal(r, claudeApplyRuleJSON) })
	switch {
	case add == (at >= 0):
		return data, false, nil
	case add:
		allow = append(allow, claudeApplyRuleJSON)
	default:
		allow = slices.Delete(allow, at, at+1)
	}
	list := []byte("[")
	for i, r := range allow {
		if i > 0 {
			list = append(list, ',')
		}
		list = append(list, r...)
	}
	perms.set("allow", append(list, ']'))
	root.set("permissions", perms.encode())
	indent := "  "
	if m := jsonIndentPattern.FindSubmatch(data); m != nil {
		indent = string(m[1])
	}
	var compact, out bytes.Buffer
	if err := json.Compact(&compact, root.encode()); err != nil {
		return nil, false, err
	}
	if err := json.Indent(&out, compact.Bytes(), "", indent); err != nil {
		return nil, false, err
	}
	return append(out.Bytes(), '\n'), true, nil
}

// orderedJSONObject keeps a JSON object's key order and raw values.
type orderedJSONObject struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseOrderedJSONObject(data []byte) (*orderedJSONObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, errClaudeSettingsShape
	}
	o := &orderedJSONObject{vals: map[string]json.RawMessage{}}
	for dec.More() {
		tok, err := dec.Token()
		key, isKey := tok.(string)
		var raw json.RawMessage
		if err != nil || !isKey || dec.Decode(&raw) != nil {
			return nil, errClaudeSettingsShape
		}
		if _, dup := o.vals[key]; dup { // which one Claude Code reads is not ours to guess
			return nil, errClaudeSettingsShape
		}
		o.keys, o.vals[key] = append(o.keys, key), raw
	}
	if _, err := dec.Token(); err != nil {
		return nil, errClaudeSettingsShape
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errClaudeSettingsShape
	}
	return o, nil
}

func (o *orderedJSONObject) set(key string, raw []byte) {
	if _, ok := o.vals[key]; !ok {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = raw
}

func (o *orderedJSONObject) encode() []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	b.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		_ = enc.Encode(key) // a string always encodes; Encode's newline is JSON whitespace
		b.WriteByte(':')
		b.Write(o.vals[key])
	}
	b.WriteByte('}')
	return b.Bytes()
}
