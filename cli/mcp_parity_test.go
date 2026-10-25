package main

import (
	"bytes"
	"fmt"
	"io/fs"
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
		"restore": planRestoreUsage, "rooms": planRoomsUsage, "template": planTemplateUsage, "raw": planRawUsage}
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
// (mcp_read_test.go) refuses in ha_read what ha-nova relay refuses.

// mcpComputerOnly (items 1 and 5): the commands that set up HA NOVA itself.
// They have no chat route, their flags need no entry, and the header names
// exactly these, in the bullet that says mcpComputerOnlySays.
var mcpComputerOnly = []string{"setup", "pair", "doctor", "update", "check-update", "version", "uninstall", "cloud", "chat-apps"}

const mcpComputerOnlySays = "this sets up HA NOVA on a computer"

// mcpParityRoutes (item 1): every command, subcommand, and flag the skills
// name, and every flag in the usage of a command they use → its chat route,
// "tool[ argument[=value]]" (alternatives joined by " | "), or the reason it
// needs none. A flag is looked up under its subcommand, then its command.
var mcpParityRoutes = map[string]string{
	"apply": "ha_apply", "apply --plan": "ha_apply plan_id", "apply --confirm": "ha_apply confirm_code", "apply --no-snapshot": "ha_apply no_snapshot",
	"call": "ha_call service", "call --match": "ha_call match", "call --area": "ha_call area", "call --device": "ha_call device",
	"call --set": "ha_call data", "call --data-file": "ha_call data", "call --response": "ha_call response", "call --skill": "ha_call skill",
	"diff": "ha_read endpoint=diff", "diff --before": "ha_read before", "diff --after": "ha_read after", "diff --out": "ha_read endpoint=diff",
	"find": "ha_find phrase", "find --domain": "ha_find domain", "find --area": "ha_find area", "find --state": "ha_find state",
	"find --limit": "ha_find limit", "find --include-disabled": "ha_find include_disabled", "find --aliases": "ha_find aliases",
	"get": "ha_get entity_id", "get --config": "ha_get config", "get --related": "ha_get related", "get --references": "ha_get references",
	"get --dashboard": "ha_get dashboard", "get --dashboards": "ha_get dashboards",
	"history": "ha_history entity_id", "history --since": "ha_history since", "history --until": "ha_history until", "history --logbook": "ha_history logbook",
	"map": "ha_map", "map --section": "ha_map section", "map --entity": "ha_map entity", "map --first-look": "ha_map first_look",
	"map --insights": "ha_map insights", "map --limit": "ha_map limit",
	"plan": "ha_plan kind", "plan --domain": "ha_plan domain", "plan --id": "ha_plan id",
	"plan call": "ha_plan kind=call", "plan call --match": "ha_plan match", "plan call --area": "ha_plan area", "plan call --device": "ha_plan device",
	"plan call --set": "ha_plan data", "plan call --data-file": "ha_plan data", "plan call --response": "ha_plan response", "plan call --skill": "ha_plan skill",
	"plan write": "ha_plan kind=write", "plan write --create": "ha_plan create", "plan write --file": "ha_plan draft", "plan write --base": "ha_plan base",
	"plan write --title": "ha_plan title", "plan write --icon": "ha_plan icon",
	"plan delete": "ha_plan kind=delete", "plan revert": "ha_plan kind=revert", "plan revert --target": "ha_plan id",
	"plan restore": "ha_plan kind=restore", "plan restore --snapshot": "ha_plan snapshot", "plan raw --file": "ha_plan requests",
	"relay": "ha_read | ha_plan kind=raw", "relay ws": "ha_read endpoint=ws | ha_plan kind=raw", "relay core": "ha_read endpoint=core | ha_plan kind=raw",
	"relay files": "ha_read endpoint=files | ha_plan kind=raw", "relay backups": "ha_read endpoint=backups | ha_plan kind=raw",
	"relay health":      "every result carries notices[]: the session bootstrap is skipped",
	"relay --data-file": "ha_read payload | ha_plan requests", "relay --body-file": "ha_read payload | ha_plan requests",
	"relay --method": "ha_read method | ha_plan requests", "relay --path": "ha_read path | ha_plan requests",
	"relay --out": "ha_read", "relay --out-binary": "ha_read endpoint=camera", "relay --strict-status": "ha_read",
	"relay --jq": "ha_read jq", "relay --jq-file": "ha_read jq", "relay jq": "ha_read jq", "relay jq --file": "ha_read jq",
	"relay jq -r": "results are JSON already: no raw-string flag", "relay jq -c": "results are JSON already: no compact flag",
	"relay jq -e": "the header tells the model to check for null or false itself",
	"snapshot":    "ha_read endpoint=snapshot", "snapshot show": "ha_read action=show", "snapshot verify": "ha_read action=verify",
	"snapshot --list": "ha_read list", "snapshot --target": "ha_read target", "snapshot --against": "ha_read against",
	"snapshot save": "ha_apply", "snapshot save --data-file": "ha_apply", // apply saves a helper update's checkpoint itself
	"trace latest": "ha_trace action=latest", "trace list": "ha_trace action=list", "trace get": "ha_trace action=get",
	"trace --json": "ha_trace", "trace --server": "a chat connection is pinned to one home",
}

// parityUses maps every command key ("cmd" or "cmd sub") and flag key
// ("cmd[ sub] --flag") the skills name, and every flag key in the usage of a
// command they use, to where it was first seen.
func parityUses(t *testing.T) map[string]string {
	t.Helper()
	read := func(name, from string) string {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		body, _, _ := strings.Cut(string(src)[strings.Index(string(src), from):], "\n}\n")
		return body
	}
	commands := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\tcase ((?:"[a-z-]+",? ?)+):`).FindAllStringSubmatch(read("main.go", "func dispatch("), -1) {
		for _, c := range regexp.MustCompile(`"([a-z-]+)"`).FindAllStringSubmatch(m[1], -1) {
			commands[c[1]] = !strings.HasPrefix(c[1], "internal-")
		}
	}
	for _, c := range mcpComputerOnly {
		if !commands[c] {
			t.Errorf("computer-only %s is no ha-nova command", c)
		}
		commands[c] = false
	}
	subs := map[string]map[string]bool{"plan": {}}
	for _, m := range regexp.MustCompile(`args\[0\] == "([a-z]+)"`).FindAllStringSubmatch(read("plan_call.go", "func runPlanCommand("), -1) {
		subs["plan"][m[1]] = true
	}
	var usages []string
	sources, _ := filepath.Glob("*.go")
	literal := regexp.MustCompile("\"(?:[^\"\\\\\\n]|\\\\.)*\"|`[^`]*`")
	assigned := regexp.MustCompile(`Usage\s*=\s*$`)
	for _, name := range sources {
		src, _ := os.ReadFile(name)
		for _, at := range literal.FindAllIndex(src, -1) {
			text := strings.ReplaceAll(string(src[at[0]+1:at[1]-1]), `\n`, "\n")
			if strings.HasSuffix(name, "_test.go") || !strings.HasPrefix(text, "Usage: ha-nova ") &&
				!(strings.HasPrefix(text, "ha-nova ") && assigned.Match(src[bytes.LastIndexByte(src[:at[0]], '\n')+1:at[0]])) {
				continue
			}
			usages = append(usages, text)
			for _, m := range regexp.MustCompile(`ha-nova ([a-z-]+) <([a-z|-]+)>`).FindAllStringSubmatch(text, -1) {
				if subs[m[1]] == nil {
					subs[m[1]] = map[string]bool{}
				}
				for _, sub := range strings.Split(m[2], "|") {
					subs[m[1]][sub] = true
				}
			}
		}
	}
	used := map[string]string{}
	usedCommands := map[string]bool{}
	note := func(key, where string) {
		if _, seen := used[key]; !seen {
			used[key] = where
		}
	}
	invocation := regexp.MustCompile(`ha-nova ([a-z][a-z-]*)`)
	subcommand := regexp.MustCompile("^ ([a-z][a-z-]*)(?:[\\s`]|$)")
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
			if m := subcommand.FindStringSubmatch(rest); m != nil && subs[command][m[1]] {
				key, rest = command+" "+m[1], rest[len(m[0]):]
			}
			if !usage {
				note(key, where)
				usedCommands[command] = true
			}
			for _, m := range flag.FindAllStringSubmatch(rest, -1) {
				note(key+" "+m[1], where)
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
	return used
}

// Parity item 1: a command or flag a skill names, or a flag in the usage of
// a command they use, has a chat route or a reason; every route names a
// tool argument that exists.
func TestMCPParityEveryCommandAndFlagHasARoute(t *testing.T) {
	used := parityUses(t)
	if len(used) < 60 {
		t.Fatalf("the scan found only %d commands and flags: %v", len(used), used)
	}
	resolved := map[string]bool{}
	for key, where := range used {
		entry := key
		if fields := strings.Fields(key); len(fields) == 3 && mcpParityRoutes[key] == "" {
			entry = fields[0] + " " + fields[2]
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

// Parity item 5: the header names exactly mcpComputerOnly as computer only,
// maps none of them to a tool, and maps every command that has a chat route.
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
		switch {
		case strings.Contains(bullet, mcpComputerOnlySays):
			computer = bullet
		case strings.Contains(bullet, "→"):
			mapped += bullet + "\n"
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
