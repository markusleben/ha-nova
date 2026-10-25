package main

import (
	"os"
	"regexp"
	"slices"
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
