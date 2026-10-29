package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func rawArgs(t *testing.T, args map[string]any) map[string]json.RawMessage {
	t.Helper()
	out := map[string]json.RawMessage{}
	for key, value := range args {
		out[key] = mustJSON(value)
	}
	return out
}

// Dashboards, rooms, and templates reach ha_plan as the CLI's own flags:
// each value as --flag=value or checked, and a domain or argument the CLI's
// kind has no form for is refused before any verb runs.
func TestMCPPlanConfigRoutesArgv(t *testing.T) {
	draft := map[string]any{"views": []any{}}
	for _, tc := range []struct {
		args map[string]any
		want string // argv with the draft file as F; "!" + part of the error
	}{
		{map[string]any{"kind": "write", "domain": "dashboard", "id": "home-main", "base": "abc", "draft": draft}, "write --domain=dashboard --file=F --id=home-main --base=abc"},
		{map[string]any{"kind": "write", "domain": "dashboard", "id": "new-board", "create": true, "title": "Garden", "icon": "mdi:flower", "require_admin": true, "hide_sidebar": true, "draft": draft},
			"write --domain=dashboard --file=F --id=new-board --create --require-admin --hide-sidebar --title=Garden --icon=mdi:flower"},
		{map[string]any{"kind": "write", "domain": "dashboard", "id": "home-main", "base": "--server=x", "draft": draft}, "write --domain=dashboard --file=F --id=home-main --base=--server=x"},
		{map[string]any{"kind": "delete", "domain": "dashboard", "id": "home-main"}, "delete --domain=dashboard --id=home-main"},
		{map[string]any{"kind": "delete", "entity_id": "script.night"}, "delete script.night"},
		{map[string]any{"kind": "revert", "domain": "dashboard", "id": "home-main"}, "revert --target=home-main --domain=dashboard"},
		{map[string]any{"kind": "revert", "domain": "device_area", "id": "rooms-20261025"}, "revert --target=rooms-20261025 --domain=device_area"},
		{map[string]any{"kind": "rooms"}, "rooms"},
		{map[string]any{"kind": "template", "name": "battery_reminder"}, "template battery_reminder"},
		{map[string]any{"kind": "write", "domain": "automation", "id": "a1", "base": "abc", "draft": draft}, "!\"base\" is for domain dashboard"},
		{map[string]any{"kind": "write", "domain": "script", "id": "s1", "title": "T", "draft": draft}, "!\"title\" is for domain dashboard"},
		{map[string]any{"kind": "write", "domain": "device_area", "id": "x", "draft": draft}, "!for kind revert only"},
		{map[string]any{"kind": "revert", "domain": "automation", "id": "a1", "require_admin": true}, "!does not apply to kind revert"},
		{map[string]any{"kind": "delete", "domain": "automation", "id": "a1"}, "!or domain dashboard with id"},
		{map[string]any{"kind": "delete", "id": "home-main"}, "!or domain dashboard with id"},
		{map[string]any{"kind": "delete", "domain": "dashboard"}, "!takes id (its url_path)"},
		{map[string]any{"kind": "delete", "domain": "dashboard", "id": "home-main", "entity_id": "script.x"}, "!takes id (its url_path)"},
		{map[string]any{"kind": "delete", "domain": "device_area", "id": "x"}, "!for kind revert only"},
		{map[string]any{"kind": "rooms", "name": "battery_reminder"}, "!does not apply to kind rooms"},
		{map[string]any{"kind": "rooms", "domain": "device_area"}, "!does not apply to kind rooms"},
		{map[string]any{"kind": "template"}, "!kind template needs name"},
		{map[string]any{"kind": "template", "name": "--server=x"}, "!kind template needs name"},
		{map[string]any{"kind": "template", "name": "battery_reminder", "id": "x"}, "!does not apply to kind template"},
		{map[string]any{"kind": "call", "service": "light.turn_on", "name": "battery_reminder"}, "!does not apply to kind call"},
	} {
		temp := &mcpTempFiles{}
		argv, err := planArgv(rawArgs(t, tc.args), temp)
		temp.remove()
		got := strings.Join(argv, " ")
		if len(temp.paths) == 1 {
			got = strings.Replace(got, "--file="+temp.paths[0], "--file=F", 1)
		}
		if want, refused := strings.CutPrefix(tc.want, "!"); refused {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%v: argv %q err %v, want refusal %q", tc.args, got, err, want)
			}
		} else if err != nil || got != tc.want {
			t.Errorf("%v: argv %q err %v, want %q", tc.args, got, err, tc.want)
		}
	}
}

// ha_get takes exactly one of entity_id, dashboard, or dashboards.
func TestMCPGetModesArgv(t *testing.T) {
	for _, tc := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"entity_id": "automation.a", "config": true}, "--config -- automation.a"},
		{map[string]any{"dashboard": "home-main"}, "--dashboard=home-main"},
		{map[string]any{"dashboard": "home-main", "config": true}, "--config --dashboard=home-main"},
		{map[string]any{"dashboard": "--dashboards"}, "--dashboard=--dashboards"},
		{map[string]any{"dashboards": true}, "--dashboards"},
		{map[string]any{"dashboards": false, "entity_id": "light.a"}, "-- light.a"},
		{map[string]any{}, "!exactly one"},
		{map[string]any{"dashboard": ""}, "!exactly one"},
		{map[string]any{"dashboards": false}, "!exactly one"},
		{map[string]any{"entity_id": "light.a", "dashboard": "home-main"}, "!exactly one"},
		{map[string]any{"dashboard": "home-main", "dashboards": true}, "!exactly one"},
		{map[string]any{"dashboards": true, "config": true}, "!not dashboards"},
		{map[string]any{"dashboard": "home-main", "related": true}, "!entity_id only"},
		{map[string]any{"dashboards": true, "references": true}, "!entity_id only"},
	} {
		argv, err := getArgv(rawArgs(t, tc.args), nil)
		got := strings.Join(argv, " ")
		if want, refused := strings.CutPrefix(tc.want, "!"); refused {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%v: argv %q err %v, want refusal %q", tc.args, got, err, want)
			}
		} else if err != nil || got != tc.want {
			t.Errorf("%v: argv %q err %v, want %q", tc.args, got, err, tc.want)
		}
	}
	if argv, _ := mapArgv(rawArgs(t, map[string]any{"insights": true}), nil); strings.Join(argv, " ") != "--insights" {
		t.Errorf("ha_map insights: %v", argv)
	}
	if got := mcpNext("Read it again (`ha-nova get --dashboard home-main --config`) and plan again; --require-admin and --hide-sidebar go with --create."); !strings.Contains(got, "ha_get dashboard home-main config") || !strings.Contains(got, "require_admin and hide_sidebar") {
		t.Errorf("mcpNext: %s", got)
	}
}

func dashboardOf(t *testing.T, res map[string]any) map[string]any {
	t.Helper()
	env, code := readEnvelope(res)
	result, _ := env["result"].(map[string]any)
	boards, _ := result["dashboards"].([]any)
	if code != "" || len(boards) != 1 {
		t.Fatalf("ha_get dashboard: %v", env)
	}
	return boards[0].(map[string]any)
}

func planned(t *testing.T, res map[string]any, tier string) map[string]any {
	t.Helper()
	env, code := readEnvelope(res)
	plan, _ := env["plan"].(map[string]any)
	if code != "" || env["status"] != "needs_confirmation" || env["tier"] != tier || plan == nil {
		t.Fatalf("plan (want tier %s): %v", tier, env)
	}
	return plan
}

func appliedOK(t *testing.T, c *mcpClient, plan map[string]any) map[string]any {
	t.Helper()
	args := map[string]any{"plan_id": plan["id"]}
	if code, _ := plan["code"].(string); code != "" {
		args["confirm_code"] = code
	}
	env, code := readEnvelope(c.tool("ha_apply", args))
	if code != "" || env["status"] != "verified" {
		t.Fatalf("apply: %v", env)
	}
	return env
}

// A dashboard round trip through the tools: read, update with base,
// revert, create with the shell's flags, and a typed delete.
func TestMCPDashboardRoundTrip(t *testing.T) {
	h := newWriteHarness(t, dashboardFake())
	c := startMCP(t, h.paths)
	mcpInit(c)
	if board := dashboardOf(t, c.tool("ha_get", map[string]any{"dashboard": "home-main"})); board["config_digest"] != homeBase() || board["config"] != nil {
		t.Fatalf("ha_get dashboard: %v", board)
	}
	if env, code := readEnvelope(c.tool("ha_get", map[string]any{"dashboards": true})); code != "" || len(env["result"].(map[string]any)["dashboards"].([]any)) != 3 {
		t.Fatalf("ha_get dashboards: %v", env)
	}
	update := map[string]any{"kind": "write", "domain": "dashboard", "id": "home-main", "base": homeBase(), "draft": editedHome()}
	appliedOK(t, c, planned(t, c.tool("ha_plan", update), "preview"))
	if got := strings.Join(h.lovelaceSaves(), ","); got != "lovelace/config/save" {
		t.Fatalf("writes %s", got)
	}
	if env, code := readEnvelope(c.tool("ha_plan", update)); code != "PLAN_DRIFT" || !strings.Contains(fmt.Sprint(env["next_step"]), "ha_get dashboard home-main config") {
		t.Fatalf("stale base: %v", env)
	}
	appliedOK(t, c, planned(t, c.tool("ha_plan", map[string]any{"kind": "revert", "domain": "dashboard", "id": "home-main"}), "preview"))
	if canonicalJSON(mustJSON(h.f.lovelace.configs["home-main"])) != canonicalJSON(mustJSON(homeDashboard())) {
		t.Fatalf("revert did not restore: %v", h.f.lovelace.configs["home-main"])
	}

	create := map[string]any{"kind": "write", "domain": "dashboard", "id": "garden-board", "create": true, "title": "Garden", "icon": "mdi:flower",
		"require_admin": true, "hide_sidebar": true, "draft": map[string]any{"views": []any{map[string]any{"title": "Beds", "cards": []any{}}}}}
	appliedOK(t, c, planned(t, c.tool("ha_plan", create), "preview"))
	var shell map[string]any
	for _, p := range h.f.lovelace.sent {
		if p["type"] == "lovelace/dashboards/create" {
			shell = p
		}
	}
	if shell["url_path"] != "garden-board" || shell["title"] != "Garden" || shell["icon"] != "mdi:flower" || shell["require_admin"] != true || shell["show_in_sidebar"] != false {
		t.Fatalf("create sent %v", shell)
	}
	deleted := appliedOK(t, c, planned(t, c.tool("ha_plan", map[string]any{"kind": "delete", "domain": "dashboard", "id": "garden-board"}), "typed"))
	undo, _ := deleted["next_step"].(string)
	for _, want := range []string{"If the user asks to undo this delete", "ha_plan kind write domain dashboard id garden-board create: true title", "[icon", "require_admin hide_sidebar draft"} {
		if !strings.Contains(undo, want) || regexp.MustCompile(`--[a-z]`).MatchString(undo) {
			t.Fatalf("undo step lacks %q or names a CLI flag: %s", want, undo)
		}
	}
	for _, e := range h.f.lovelace.entries {
		if e["url_path"] == "garden-board" {
			t.Fatalf("not deleted: %v", h.f.lovelace.entries)
		}
	}
}

// ha_get bounds no dashboard config: unlike ha_read's 64 KiB, a large
// config comes back whole, in the text and in structuredContent.
func TestMCPGetLargeDashboardConfigWhole(t *testing.T) {
	f := dashboardFake()
	cards := make([]any, 1500)
	for i := range cards {
		cards[i] = map[string]any{"type": "markdown", "content": fmt.Sprintf("%s card %d", strings.Repeat("x", 60), i)}
	}
	f.lovelace.entries = append(f.lovelace.entries, map[string]any{"id": "big_board", "url_path": "big-board", "title": "Big", "mode": "storage"})
	f.lovelace.configs["big-board"] = map[string]any{"views": []any{map[string]any{"title": "All", "cards": cards}}}
	h := newWriteHarness(t, f)
	c := startMCP(t, h.paths)
	mcpInit(c)
	res := c.tool("ha_get", map[string]any{"dashboard": "big-board", "config": true})
	board := dashboardOf(t, res)
	got := board["config"].(map[string]any)["views"].([]any)[0].(map[string]any)["cards"].([]any)
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	if len(got) != 1500 || len(text) <= 64<<10 || !strings.Contains(text, "card 1499") {
		t.Fatalf("cards %d, text %d bytes", len(got), len(text))
	}
	env, _ := readEnvelope(res)
	if step, _ := env["next_step"].(string); !strings.Contains(step, "read single views with ha_read (endpoint ws, payload {type: lovelace/config, url_path: big-board}, jq .data.views[N])") || !strings.Contains(step, "never plan from a partial config") {
		t.Fatalf("large config step %q", step)
	}
	if env, _ := readEnvelope(c.tool("ha_get", map[string]any{"dashboard": "home-main", "config": true})); env["next_step"] != nil {
		t.Fatalf("small config step %v", env["next_step"])
	}
}

// Setup's insights, rooms, and their revert (domain device_area) run
// through the tools as through the CLI.
func TestMCPInsightsAndRooms(t *testing.T) {
	h := newRoomsHarness(t, newRoomsFake())
	c := startMCP(t, h.paths)
	mcpInit(c)
	env, code := readEnvelope(c.tool("ha_map", map[string]any{"insights": true}))
	result, _ := env["result"].(map[string]any)
	if code != "" || result["stats"] == nil || !strings.Contains(string(mustJSON(result["insights"])), `"action":"rooms"`) {
		t.Fatalf("ha_map insights: %v", env)
	}
	if _, code := readEnvelope(c.tool("ha_map", map[string]any{"insights": true, "first_look": true})); code != "USAGE" {
		t.Fatalf("insights with first_look: %s", code)
	}
	applied := appliedOK(t, c, planned(t, c.tool("ha_plan", map[string]any{"kind": "rooms"}), "preview"))
	h.assertAreas(map[string]string{"d_guest": "guest", "d_kitchen": "kitchen", "d_radio": "kitchen"})
	undo, _ := applied["undo"].(map[string]any)
	appliedOK(t, c, planned(t, c.tool("ha_plan", map[string]any{"kind": "revert", "domain": "device_area", "id": undo["target_id"]}), "preview"))
	h.assertAreas(roomsOriginal)
}

// A template plan is an automation create through the tools too.
func TestMCPTemplatePlan(t *testing.T) {
	f := newFakeHA()
	f.ent("person.someone", "", "home", nil)
	f.full = []map[string]any{{"entity_id": "lock.front", "platform": "zwave_js", "area_id": "hall"}, {"entity_id": "light.hall", "platform": "hue", "area_id": "hall"}}
	h := newWriteHarness(t, f)
	c := startMCP(t, h.paths)
	mcpInit(c)
	env, _ := readEnvelope(c.tool("ha_map", map[string]any{"insights": true}))
	if automation, _ := env["result"].(map[string]any)["automation"].(map[string]any); automation["template"] != templateDoorLight {
		t.Fatalf("insights offer %v", env["result"])
	}
	res := c.tool("ha_plan", map[string]any{"kind": "template", "name": templateDoorLight})
	plan := planned(t, res, "preview")
	if env, _ := readEnvelope(res); env["config"].(map[string]any)["domain"] != "automation" || len(h.writes()) != 0 {
		t.Fatalf("template plan: %v", env)
	}
	appliedOK(t, c, plan)
	if len(h.writes()) != 1 {
		t.Fatalf("writes %v", h.writes())
	}
	if _, code := readEnvelope(c.tool("ha_plan", map[string]any{"kind": "template", "name": templateBatteryReminder})); code != "TEMPLATE_NO_MATCH" {
		t.Fatalf("no match: %s", code)
	}
}

// Every plan but a call is a config plan: the owner's read only switch
// refuses it like every plan, and in a chat app only the Home Assistant
// owner or an administrator plans or applies it (checked again at apply;
// an unreadable user list refuses).
func TestServeConfigPlansNeedAnAdministrator(t *testing.T) {
	routes := []map[string]any{
		{"kind": "write", "domain": "dashboard", "id": "home-main", "base": homeBase(), "draft": editedHome()},
		{"kind": "write", "domain": "automation", "id": "a1", "draft": map[string]any{"alias": "A", "triggers": []any{}, "actions": []any{}}},
		{"kind": "delete", "domain": "dashboard", "id": "home-main"},
		{"kind": "delete", "entity_id": "automation.a1"},
		{"kind": "revert", "domain": "dashboard", "id": "home-main"},
		{"kind": "revert", "domain": "device_area", "id": "rooms-x"},
		{"kind": "restore", "snapshot": "scripts/auto-script-night-mode-20261016T120000000Z.json.gz", "id": "night_mode"},
		{"kind": "rooms"},
		{"kind": "template", "name": "battery_reminder"},
		{"kind": "raw", "requests": []any{map[string]any{"endpoint": "ws", "payload": map[string]any{"type": "input_boolean/create", "name": "A"}}}},
		{"kind": "batch", "plans": []string{"p_000000000001", "p_000000000002"}},
	}
	ro := newServe(t, dashboardFake(), `{"allowed_users":["`+userA+`"],"read_only":true}`)
	for _, args := range routes {
		if env := ro.tool(userA, "ha_plan", args); envCode(env) != "READ_ONLY" {
			t.Errorf("read only, %v: %v", args, env)
		}
	}
	if env := ro.tool(userA, "ha_get", map[string]any{"dashboards": true}); envCode(env) != "" {
		t.Errorf("read only refused a read: %v", env)
	}

	f := dashboardFake()
	var mu sync.Mutex
	admin, unreadable := true, false
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		if endpoint != "ws" || p["type"] != "config/auth/list" {
			return nil, false
		}
		mu.Lock()
		defer mu.Unlock()
		if unreadable {
			return fakeRelayFailure{"UPSTREAM_ERROR", "boom"}, true
		}
		groups := []string{"system-users"}
		if admin {
			groups = []string{"system-admin"}
		}
		return []map[string]any{{"id": userA, "is_active": true, "group_ids": groups}, {"id": userB, "is_active": true, "group_ids": []string{"system-users"}}}, true
	}
	set := func(isAdmin, isUnreadable bool) {
		mu.Lock()
		admin, unreadable = isAdmin, isUnreadable
		mu.Unlock()
	}
	s := newServe(t, f, `{"allowed_users":["`+userA+`","`+userB+`"]}`)
	for _, args := range routes {
		if env := s.tool(userB, "ha_plan", args); envCode(env) != "ADMIN_ONLY" {
			t.Errorf("not an administrator, %v: %v", args, env)
		}
	}
	if env := s.tool(userB, "ha_plan", map[string]any{"kind": "call", "service": "light.turn_off", "entity_ids": []string{"light.kitchen"}}); envCode(env) == "ADMIN_ONLY" {
		t.Errorf("a call plan needs no administrator: %v", env)
	}
	call, _, _ := s.b.policyFor(userB)
	plans := filepath.Join(s.data, "users", call.key, "plans")
	if err := os.MkdirAll(plans, 0o700); err != nil || os.WriteFile(filepath.Join(plans, "p_00000000abcd.json"), []byte("{not json"), 0o600) != nil {
		t.Fatal(err)
	}
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": "p_00000000abcd"}); envCode(env) != "ADMIN_ONLY" {
		t.Errorf("unreadable plan file: %v", env)
	}
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": "p_00000000abce"}); envCode(env) != "PLAN_NOT_FOUND" {
		t.Errorf("missing plan: %v", env)
	}
	if env := s.tool(userA, "ha_plan", map[string]any{"kind": "rooms"}); envCode(env) != "NO_ROOM_SUGGESTIONS" {
		t.Errorf("administrator, rooms: %v", env)
	}
	env := s.tool(userA, "ha_plan", routes[0])
	plan, _ := env["plan"].(map[string]any)
	if env["status"] != "needs_confirmation" || plan == nil {
		t.Fatalf("administrator, dashboard update: %v", env)
	}
	set(false, false)
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); envCode(env) != "ADMIN_ONLY" || len(s.lovelaceSaves()) != 0 {
		t.Fatalf("demoted since planning: %v (saves %v)", env, s.lovelaceSaves())
	}
	set(true, true)
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); envCode(env) != "ACCESS_UNKNOWN" || len(s.lovelaceSaves()) != 0 {
		t.Fatalf("unreadable users: %v (saves %v)", env, s.lovelaceSaves())
	}
	set(true, false)
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); env["status"] != "verified" || len(s.lovelaceSaves()) != 1 {
		t.Fatalf("administrator, apply: %v (saves %v)", env, s.lovelaceSaves())
	}
}
