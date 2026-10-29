package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Parity (docs/work/2026-10-25-chat-parity-spec.md, item 2): every kind
// runPlanCommand dispatches and every --domain value in its usage strings
// has an ha_plan route, and the CLI accepts the argv that route builds.
func TestMCPParityPlanKindsAndDomains(t *testing.T) {
	source, err := os.ReadFile("plan_call.go")
	if err != nil {
		t.Fatal(err)
	}
	body, _, _ := strings.Cut(string(source[strings.Index(string(source), "func runPlanCommand("):]), "\n}\n")
	var kinds []string
	for _, m := range regexp.MustCompile(`args\[0\] == "([a-z]+)"`).FindAllStringSubmatch(body, -1) {
		kinds = append(kinds, m[1])
	}
	usages := map[string]string{"call": planCallUsage, "write": planWriteUsage, "delete": planDeleteUsage, "revert": planRevertUsage,
		"restore": planRestoreUsage, "rooms": planRoomsUsage, "template": planTemplateUsage, "raw": planRawUsage, "batch": planBatchUsage}
	draft := map[string]any{"alias": "A"}
	routes := map[string]map[string]any{
		"call/":              {"kind": "call", "service": "light.turn_on", "entity_ids": []string{"light.kitchen"}},
		"write/automation":   {"kind": "write", "domain": "automation", "id": "a1", "draft": draft},
		"write/script":       {"kind": "write", "domain": "script", "id": "s1", "draft": draft},
		"write/dashboard":    {"kind": "write", "domain": "dashboard", "id": "home-main", "base": homeBase(), "draft": homeDashboard()},
		"delete/":            {"kind": "delete", "entity_id": "automation.a1"},
		"delete/dashboard":   {"kind": "delete", "domain": "dashboard", "id": "home-main"},
		"revert/":            {"kind": "revert"},
		"revert/automation":  {"kind": "revert", "domain": "automation", "id": "a1"},
		"revert/script":      {"kind": "revert", "domain": "script", "id": "s1"},
		"revert/dashboard":   {"kind": "revert", "domain": "dashboard", "id": "home-main"},
		"revert/device_area": {"kind": "revert", "domain": "device_area", "id": "rooms-x"},
		"restore/":           {"kind": "restore", "snapshot": "scripts/auto-script-night-mode-20261016T120000000Z.json.gz", "id": "night_mode"},
		"rooms/":             {"kind": "rooms"},
		"template/":          {"kind": "template", "name": "battery_reminder"},
		"raw/":               {"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "input_boolean/create", "name": "A"}}}},
		"batch/":             {"kind": "batch", "plans": []string{"p_000000000001", "p_000000000002"}},
	}
	var schema map[string]any
	for _, tool := range mcpTools {
		if tool.name == "ha_plan" {
			schema = tool.schema
		}
	}
	properties := schema["properties"].(map[string]any)
	if got := properties["kind"].(map[string]any)["enum"].([]string); !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(kinds))) {
		t.Errorf("ha_plan kinds %v, runPlanCommand %v", got, kinds)
	}
	names := strings.Split(strings.TrimPrefix(planTemplateUsage, "ha-nova plan template "), "|")
	if got := properties["name"].(map[string]any)["enum"].([]string); !slices.Equal(got, names) {
		t.Errorf("ha_plan template names %v, usage %v", got, names)
	}
	h := newWriteHarness(t, dashboardFake())
	domainFlag := regexp.MustCompile(`--domain ([a-z_|]+)`)
	for _, kind := range kinds {
		usage, ok := usages[kind]
		if !ok {
			t.Errorf("plan %s: no usage string in this test", kind)
			continue
		}
		keys := []string{kind + "/"}
		if kind == "write" { // every write names its domain
			keys = nil
		}
		for _, m := range domainFlag.FindAllStringSubmatch(usage, -1) {
			for _, domain := range strings.Split(m[1], "|") {
				keys = append(keys, kind+"/"+domain)
			}
		}
		for _, key := range keys {
			args, ok := routes[key]
			if !ok {
				t.Errorf("plan %s: no ha_plan route", key)
				continue
			}
			raw := rawArgs(t, args)
			if err := checkArguments(schema, raw); err != nil {
				t.Errorf("%s: schema refuses %v: %v", key, args, err)
				continue
			}
			temp := &mcpTempFiles{}
			argv, err := planArgv(raw, temp)
			_, domain, _ := strings.Cut(key, "/")
			if err != nil || argv[0] != kind || domain != "" && !slices.Contains(argv, "--domain="+domain) {
				t.Errorf("%s: argv %v err %v", key, argv, err)
			} else if _, env := h.run(runPlanCommand, argv...); env.Error != nil && env.Error.Code == "USAGE" {
				t.Errorf("%s: the CLI refuses %v: %s", key, argv, env.Error.Message)
			}
			temp.remove()
		}
	}
}

// Parity items 3 and 4 live with the code they cover:
// TestPlanRawTierCoversEveryAllowlistedRequest (plan_raw_tier_test.go) plans
// every allowlisted raw write through kind raw and reads every allowlisted
// read through ha_read or a plan; TestMCPReadRefusesWhatRelayRefuses
// (mcp_read_test.go) refuses in ha_read what ha-nova relay refuses. The
// table itself is mcp_parity_routes_test.go.

// parityFunc returns a function's source, from its signature to its closing brace.
func parityFunc(t *testing.T, file, from string) string {
	t.Helper()
	src, err := os.ReadFile(file)
	at := strings.Index(string(src), from)
	if err != nil || at < 0 {
		t.Fatalf("%s: no %q (%v): the parity scan needs a new anchor", file, from, err)
	}
	body, _, _ := strings.Cut(string(src)[at:], "\n}\n")
	return body
}

// parityCommands: every command dispatch() runs, but internal-* and help.
func parityCommands(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, m := range regexp.MustCompile(`(?m)^\tcase ((?:"[a-z-]+",? ?)+):`).FindAllStringSubmatch(parityFunc(t, "main.go", "func dispatch("), -1) {
		for _, c := range regexp.MustCompile(`"([a-z][a-z-]*)"`).FindAllStringSubmatch(m[1], -1) {
			if !strings.HasPrefix(c[1], "internal-") && c[1] != "help" && !slices.Contains(out, c[1]) {
				out = append(out, c[1])
			}
		}
	}
	return out
}

// parityUsages: the CLI's usage strings, from the parsed Go sources: every
// string literal that starts with "ha-nova " or "Usage: ha-nova ", and the
// whole value of every name ending in Usage.
func parityUsages(t *testing.T) []string {
	t.Helper()
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	var usages []string
	literal := func(node ast.Node) string {
		lit, ok := node.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return ""
		}
		text, _ := strconv.Unquote(lit.Value)
		return text
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if spec, ok := node.(*ast.ValueSpec); ok {
				for i, id := range spec.Names {
					if !strings.HasSuffix(id.Name, "Usage") || i >= len(spec.Values) {
						continue
					}
					whole := ""
					ast.Inspect(spec.Values[i], func(part ast.Node) bool {
						whole += literal(part)
						return true
					})
					usages = append(usages, whole)
				}
			}
			if text := literal(node); strings.HasPrefix(text, "ha-nova ") || strings.HasPrefix(text, "Usage: ha-nova ") {
				usages = append(usages, text)
			}
			return true
		})
	}
	return usages
}

// parityUses maps every command key ("cmd" or "cmd sub") and flag key
// ("cmd[ sub] --flag") the skills name, and every flag key in the usage of a
// command they use, to where it was first seen; unknown maps a bare word
// after a command that takes subcommands, when it is none of them.
func parityUses(t *testing.T) (used, unknown map[string]string) {
	t.Helper()
	commands := map[string]bool{}
	for _, c := range parityCommands(t) {
		commands[c] = !slices.Contains(mcpComputerOnly, c)
	}
	subs := map[string]map[string]bool{"plan": {}}
	for _, m := range regexp.MustCompile(`args\[0\] == "([a-z]+)"`).FindAllStringSubmatch(parityFunc(t, "plan_call.go", "func runPlanCommand("), -1) {
		subs["plan"][m[1]] = true
	}
	usages := parityUsages(t)
	for _, text := range usages {
		for _, m := range regexp.MustCompile(`ha-nova ([a-z-]+) <([a-z|-]+)>`).FindAllStringSubmatch(text, -1) {
			if subs[m[1]] == nil {
				subs[m[1]] = map[string]bool{}
			}
			for _, sub := range strings.Split(m[2], "|") {
				subs[m[1]][sub] = true
			}
		}
	}
	used, unknown = map[string]string{}, map[string]string{}
	usedCommands := map[string]bool{}
	note := func(seen map[string]string, key, where string) {
		if _, ok := seen[key]; !ok {
			seen[key] = where
		}
	}
	invocation := regexp.MustCompile(`ha-nova ([a-z][a-z-]*)`)
	bareWord := regexp.MustCompile("^ ([a-z][a-z-]*)(?:[\\s`]|$)")
	flag := regexp.MustCompile(`(?:^|[\s\[(])(--?[a-z][a-z-]*)`)
	scan := func(text, where string, inSpan func(int) bool, usage bool) {
		for _, at := range invocation.FindAllStringSubmatchIndex(text, -1) {
			command, rest := text[at[2]:at[3]], text[at[1]:]
			if !commands[command] || usage && !usedCommands[command] {
				continue
			}
			if inSpan(at[0]) {
				rest, _, _ = strings.Cut(rest, "`")
			}
			if next := strings.Index(rest, "ha-nova "); next >= 0 {
				rest = rest[:next]
			}
			key := command
			if m := bareWord.FindStringSubmatch(rest); m != nil && subs[command] != nil {
				if !subs[command][m[1]] {
					note(unknown, command+" "+m[1], where)
					continue
				}
				key, rest = command+" "+m[1], rest[len(m[0]):]
			}
			if !usage {
				note(used, key, where)
				usedCommands[command] = true
			}
			for _, m := range flag.FindAllStringSubmatch(rest, -1) {
				note(used, key+" "+m[1], where)
			}
		}
	}
	err := filepath.WalkDir(filepath.Join("..", "skills"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		raw, err := os.ReadFile(path)
		for i, line := range strings.Split(string(raw), "\n") {
			scan(line, fmt.Sprintf("%s:%d", filepath.ToSlash(path)[len("../"):], i+1),
				func(at int) bool { return strings.Count(line[:at], "`")%2 == 1 }, false)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, usage := range usages {
		scan(usage, "usage "+strconv.Quote(usage), func(int) bool { return false }, true)
	}
	return used, unknown
}

// Parity item 1: a command, subcommand, or flag a skill names, or a flag in
// the usage of a command they use, has a chat route or a reason; every
// route names a tool argument that exists.
func TestMCPParityEveryCommandAndFlagHasARoute(t *testing.T) {
	used, unknown := parityUses(t)
	if len(used) < 60 {
		t.Fatalf("the scan found only %d commands and flags: %v", len(used), used)
	}
	for key, where := range unknown {
		t.Errorf("%s: ha-nova %s: %q is no subcommand its usage names; add it to the usage and to mcpParityRoutes", where, key, strings.Fields(key)[1])
	}
	resolved := map[string]bool{}
	for key, where := range used {
		fields, entry := strings.Fields(key), key
		if mcpParityRoutes[key] == "" && strings.HasPrefix(fields[len(fields)-1], "-") {
			base := strings.Join(fields[:len(fields)-1], " ")
			if reason := mcpParityRoutes[base]; reason != "" && !strings.HasPrefix(reason, "ha_") {
				resolved[base] = true
				continue
			}
			if len(fields) == 3 {
				entry = fields[0] + " " + fields[2]
			}
		}
		if mcpParityRoutes[entry] == "" {
			t.Errorf("%s: ha-nova %s has no chat route: add %q to mcpParityRoutes with its tool and argument, or the reason it has none", where, key, key)
		}
		resolved[entry] = true
	}
	properties := map[string]map[string]any{}
	for _, tool := range mcpTools {
		properties[tool.name] = tool.schema["properties"].(map[string]any)
	}
	route := regexp.MustCompile(`^(ha_[a-z]+)(?: ([a-z_]+)(?:=([a-z_]+))?)?$`)
	for key, value := range mcpParityRoutes {
		if !resolved[key] {
			t.Errorf("mcpParityRoutes[%q]: no skill or usage names it any more; remove it", key)
		}
		if slices.Contains(mcpComputerOnly, strings.Fields(key)[0]) {
			t.Errorf("mcpParityRoutes[%q]: a computer-only command needs no entry", key)
		}
		if !strings.HasPrefix(value, "ha_") {
			continue
		}
		for _, alternative := range strings.Split(value, " | ") {
			m := route.FindStringSubmatch(alternative)
			if m == nil || properties[m[1]] == nil {
				t.Errorf("%s → %q: no such tool", key, alternative)
				continue
			}
			if m[2] == "" {
				continue
			}
			schema, ok := properties[m[1]][m[2]].(map[string]any)
			if enum, _ := schema["enum"].([]string); !ok || m[3] != "" && !slices.Contains(enum, m[3]) {
				t.Errorf("%s → %q: %s has no argument %s with that value", key, alternative, m[1], m[2])
			}
		}
	}
}

// Every command is computer only, chat-routed, or exempt with a reason:
// exactly one of them.
func TestMCPParityEveryCommandIsClassified(t *testing.T) {
	commands := parityCommands(t)
	for _, command := range commands {
		routed := false
		for key := range mcpParityRoutes {
			routed = routed || strings.Fields(key)[0] == command
		}
		if n := len(slices.DeleteFunc([]bool{slices.Contains(mcpComputerOnly, command), routed, mcpParityExempt[command] != ""}, func(b bool) bool { return !b })); n != 1 {
			t.Errorf("ha-nova %s: in %d of mcpComputerOnly, mcpParityRoutes, mcpParityExempt; it belongs in exactly one", command, n)
		}
	}
	for _, command := range append(slices.Collect(maps.Keys(mcpParityExempt)), mcpComputerOnly...) {
		if !slices.Contains(commands, command) {
			t.Errorf("%s is no ha-nova command any more", command)
		}
	}
}

// Parity item 5: the header names exactly mcpComputerOnly as computer only,
// maps none of them to a tool, and maps every command that has a chat route.
// What a bullet maps is the text before its last →.
func TestMCPParityHeaderComputerOnly(t *testing.T) {
	var bullets []string
	for _, line := range strings.Split(mcpSkillHeader, "\n") {
		switch {
		case strings.HasPrefix(line, "- "):
			bullets = append(bullets, line[2:])
		case strings.HasPrefix(line, "  ") && len(bullets) > 0:
			bullets[len(bullets)-1] += " " + strings.TrimSpace(line)
		}
	}
	var computer, mapped string
	for _, bullet := range bullets {
		if strings.Contains(bullet, mcpComputerOnlySays) {
			computer = bullet
		} else if at := strings.LastIndex(bullet, "→"); at >= 0 {
			mapped += bullet[:at] + "\n"
		}
	}
	listed, _, _ := strings.Cut(computer, ":")
	if got := strings.Split(listed, ", "); !slices.Equal(got, mcpComputerOnly) {
		t.Errorf("header's computer-only list %q, want %v", got, mcpComputerOnly)
	}
	word := func(text, name string) bool {
		return regexp.MustCompile(`(?:^|[^a-z_-])` + regexp.QuoteMeta(name) + `(?:$|[^a-z_-])`).MatchString(text)
	}
	for _, name := range mcpComputerOnly {
		if word(mapped, name) {
			t.Errorf("header maps computer-only %s to a tool", name)
		}
	}
	for key, value := range mcpParityRoutes {
		command := strings.Fields(key)[0]
		if strings.HasPrefix(value, "ha_") && (word(computer, command) || !word(mapped, command)) {
			t.Errorf("header: %s has a chat route (%s), so it is mapped and never computer only", command, value)
		}
	}
}
