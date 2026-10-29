package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Household levels (mcp_serve_levels.go, docs/work/2026-10-24-chat-apps-
// household-spec.md): every tool, argument, and plan kind has a decided
// level, the unknown is refused at every level, and a user's level is the
// lower of the owner's choice and their Home Assistant role.

const (
	userOwner = "cccccccccccccccccccccccccccccccc"
	userRead  = "dddddddddddddddddddddddddddddddd" // system-read-only
	userNone  = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" // no group
	userOld   = "ffffffffffffffffffffffffffffffff" // an inactive administrator
	userSys   = "abababababababababababababababab" // system-generated
)

// household: Markus owns the home, Anna (userA) administers it, Ben
// (userB) is a user, Dora reads only, and Emil has no group.
func household(f *fakeHA) *fakeHA {
	f.users = []map[string]any{
		{"id": userOwner, "name": "Markus", "is_owner": true, "is_active": true, "group_ids": []string{"system-admin"}},
		{"id": userA, "name": "Anna", "is_active": true, "group_ids": []string{"system-admin"}},
		{"id": userB, "name": "Ben", "is_active": true, "group_ids": []string{"system-users"}},
		{"id": userRead, "name": "Dora", "is_active": true, "group_ids": []string{"system-read-only"}},
		{"id": userNone, "name": "Emil", "is_active": true},
		{"id": userOld, "name": "Old", "group_ids": []string{"system-admin"}},
		{"id": userSys, "name": "Supervisor", "is_active": true, "system_generated": true, "group_ids": []string{"system-admin"}},
	}
	return f
}

// levels: chat-apps.json as the App writes a list, with the owner's choices.
func levels(users []string, chosen map[string]string) string {
	raw, _ := json.Marshal(map[string]any{"allowed_users": users, "roles": 1, "levels": chosen})
	return string(raw)
}

func (s *serveHarness) settings(content string) {
	s.t.Helper()
	if err := os.WriteFile(filepath.Join(s.data, "chat-apps.json"), []byte(content), 0o600); err != nil {
		s.t.Fatal(err)
	}
}

// code: a tool call's error code, "" when none (also for a result without
// an envelope, such as ha_skill's).
func (s *serveHarness) code(user, name string, args map[string]any) string {
	s.t.Helper()
	msg := s.rpc(user, "tools/call", map[string]any{"name": name, "arguments": args})
	result, _ := msg["result"].(map[string]any)
	env, _ := result["structuredContent"].(map[string]any)
	return envCode(env)
}

func TestChatLevelsDecideEveryTool(t *testing.T) {
	tools := map[string]bool{}
	for _, tool := range mcpTools {
		tools[tool.name] = true
		need, decided := chatToolLevel[tool.name]
		if !decided {
			t.Errorf("%s: no level", tool.name)
		}
		props, _ := tool.schema["properties"].(map[string]any)
		for key := range props {
			if _, listed := chatArgLevel[tool.name][key]; need < levelManage && !listed {
				t.Errorf("%s: argument %s has no level", tool.name, key)
			}
		}
		for key := range chatArgLevel[tool.name] {
			if _, ok := props[key]; !ok {
				t.Errorf("%s: %s is not an argument", tool.name, key)
			}
		}
		if tool.name != "ha_plan" {
			continue
		}
		kind, _ := props["kind"].(map[string]any)
		kinds, _ := kind["enum"].([]string)
		for _, k := range kinds {
			if _, ok := chatPlanKinds[k]; !ok {
				t.Errorf("plan kind %s: no level", k)
			}
		}
		for k := range chatPlanKinds {
			if !slices.Contains(kinds, k) {
				t.Errorf("plan kind %s: not a kind", k)
			}
		}
	}
	for name := range chatToolLevel {
		if !tools[name] {
			t.Errorf("%s: not a tool", name)
		}
	}
	for name := range chatArgLevel {
		if chatToolLevel[name] >= levelManage {
			t.Errorf("%s: arguments decided for a tool only Manage everything has", name)
		}
	}
	for kind, k := range chatPlanKinds { // only a call applies below Manage everything; batch never does
		if k.plan != levelManage || (kind != "call" && k.apply != levelManage) {
			t.Errorf("plan kind %s: %+v", kind, k)
		}
	}
}

func TestChatLevelsRefuseTheUnknown(t *testing.T) {
	tool := func(name string) mcpTool {
		return mcpTools[slices.IndexFunc(mcpTools, func(m mcpTool) bool { return m.name == name })]
	}
	for _, level := range []chatLevel{levelAsk, levelUse, levelManage} {
		srv := &mcpServer{chat: &chatPolicy{level: level}}
		if srv.chatLevelRefusal(mcpTool{name: "ha_later", readOnly: true}, nil) == nil {
			t.Errorf("level %d: an unknown tool ran", level)
		}
		for _, kind := range []string{"later", ""} {
			if srv.chatLevelRefusal(tool("ha_plan"), rawArgs(t, map[string]any{"kind": kind})) == nil || srv.chat.mayApply(kind) {
				t.Errorf("level %d: plan kind %q", level, kind)
			}
		}
		later := srv.chatLevelRefusal(tool("ha_get"), rawArgs(t, map[string]any{"entity_id": "light.kitchen", "later": true}))
		if (later == nil) != (level == levelManage) {
			t.Errorf("level %d: an unknown argument: %v", level, later)
		}
		if refusal := srv.chatLevelRefusal(tool("ha_get"), rawArgs(t, map[string]any{"entity_id": "light.kitchen", "config": false, "related": nil})); refusal != nil {
			t.Errorf("level %d: an argument left unset counted: %v", level, refusal)
		}
	}
	var level chatLevel
	if json.Unmarshal([]byte(`"admin"`), &level) == nil || json.Unmarshal([]byte(`"use"`), &level) != nil || level != levelUse {
		t.Errorf("level names: %d", level)
	}
}

func TestChatLevelRoleCaps(t *testing.T) {
	for _, c := range []struct {
		u    haUser
		want chatLevel
	}{
		{haUser{IsOwner: true}, levelManage},
		{haUser{GroupIDs: []string{"system-users", "system-admin"}}, levelManage},
		{haUser{GroupIDs: []string{"system-users"}}, levelUse},
		{haUser{GroupIDs: []string{"system-read-only"}}, levelAsk},
		{haUser{}, levelAsk},
		{haUser{GroupIDs: []string{"family"}}, levelAsk},
	} {
		if got := roleCap(c.u); got != c.want {
			t.Errorf("%+v: %d, want %d", c.u, got, c.want)
		}
	}
}

func TestServeLevelIsTheLowerOfChoiceAndRole(t *testing.T) {
	f := household(newFakeHA())
	everyone := []string{userOwner, userA, userB, userRead, userNone, userOld, userSys}
	s := newServe(t, f, levels(everyone, map[string]string{userA: "use", userB: "manage", userNone: "use"}))
	for user, want := range map[string]chatLevel{userOwner: levelManage, userA: levelUse, userB: levelUse, userRead: levelAsk, userNone: levelAsk} {
		if p, _, why := s.b.policyFor(user); p == nil || p.level != want {
			t.Errorf("%s: %+v %s, want level %d", user, p, why, want)
		}
	}
	for _, user := range []string{userOld, userSys, "0123456789abcdef0123456789abcdef"} {
		if p, code, _ := s.b.policyFor(user); p != nil || code != http.StatusForbidden {
			t.Errorf("%s: %+v %d", user, p, code)
		}
	}
	for name, settings := range map[string]string{
		"unknown level":        `{"allowed_users":["` + userA + `"],"roles":1,"levels":{"` + userA + `":"admin"}}`,
		"levels without roles": `{"allowed_users":["` + userA + `"],"levels":{"` + userA + `":"use"}}`,
		"newer roles":          `{"allowed_users":["` + userA + `"],"roles":2}`,
	} {
		s.settings(settings)
		if p, code, _ := s.b.policyFor(userA); p != nil || code != http.StatusServiceUnavailable {
			t.Errorf("%s: %+v %d", name, p, code)
		}
	}
	s.settings(levels(everyone, nil))
	f.mu.Lock()
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		return fakeRelayFailure{"UPSTREAM_ERROR", "boom"}, endpoint == "ws" && p["type"] == "config/auth/list"
	}
	f.mu.Unlock()
	if p, code, why := s.b.policy(userA, true); p != nil || code != http.StatusServiceUnavailable || !strings.Contains(why, "can't read this home's Home Assistant users") {
		t.Errorf("unreadable users: %+v %d %s", p, code, why)
	}
}

// A 0.27 bridge decodes chat-apps.json into these fields only and refuses
// unknown ones: a list the App writes with roles turns it away.
func TestChatSettingsRollbackGuard(t *testing.T) {
	var old struct {
		AllowedUsers *[]string `json:"allowed_users"`
		ReadOnly     bool      `json:"read_only"`
		ChatCodes    *bool     `json:"chat_codes"`
	}
	dec := json.NewDecoder(strings.NewReader(`{"allowed_users":["` + userA + `"],"roles":1}`))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&old); err == nil || !strings.Contains(err.Error(), `"roles"`) {
		t.Fatalf("a 0.27 bridge would read the list as admin power for everyone on it: %v", err)
	}
}

type levelCall struct {
	name string
	args map[string]any
}

// Ask only reads devices and their state; Use the home also changes them;
// everything else needs Manage everything. Refused before anything is sent
// (config plans of a user who isn't an administrator: ADMIN_ONLY first).
func TestServeLevelsDecideEachTool(t *testing.T) {
	s := newServe(t, household(newFakeHA()), levels([]string{userA, userB, userRead}, map[string]string{userA: "ask"}))
	reads := []levelCall{{"ha_skill", map[string]any{}}, {"ha_find", map[string]any{"domain": "light"}},
		{"ha_get", map[string]any{"entity_id": "light.kitchen"}}, {"ha_history", map[string]any{"entity_id": "light.kitchen", "logbook": true}}}
	manage := []levelCall{{"ha_map", map[string]any{"section": "areas"}}, {"ha_trace", map[string]any{"action": "latest", "entity_id": "automation.x"}},
		{"ha_read", map[string]any{"endpoint": "core", "path": "/api/config"}},
		{"ha_get", map[string]any{"entity_id": "light.kitchen", "config": true}}, {"ha_get", map[string]any{"entity_id": "light.kitchen", "related": true}},
		{"ha_get", map[string]any{"entity_id": "light.kitchen", "references": true}}, {"ha_get", map[string]any{"dashboard": "lovelace"}},
		{"ha_get", map[string]any{"dashboards": true}},
		{"ha_call", map[string]any{"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}, "skill": "ha-nova:media"}},
		{"ha_apply", map[string]any{"plan_id": "p_0000000000aa", "no_snapshot": true}}}
	for kind := range chatPlanKinds {
		manage = append(manage, levelCall{"ha_plan", map[string]any{"kind": kind}})
	}
	changes := []levelCall{{"ha_call", map[string]any{"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}}},
		{"ha_apply", map[string]any{"plan_id": "p_0000000000aa"}}}
	for _, user := range []string{userA, userRead, userB} {
		ask := user != userB
		for _, c := range reads {
			if code := s.code(user, c.name, c.args); code == "CHAT_LEVEL" {
				t.Errorf("%s, %s %v: refused", user, c.name, c.args)
			}
		}
		for _, c := range manage {
			if code := s.code(user, c.name, c.args); code != "CHAT_LEVEL" && (code != "ADMIN_ONLY" || user == userA) {
				t.Errorf("%s, %s %v: %q", user, c.name, c.args, code)
			}
		}
		for _, c := range changes {
			if code := s.code(user, c.name, c.args); (code == "CHAT_LEVEL") != ask {
				t.Errorf("%s, %s %v: %q", user, c.name, c.args, code)
			}
		}
	}
	call, _, _ := s.b.policyFor(userA)
	plans := filepath.Join(s.data, "users", call.key, "plans")
	if err := os.MkdirAll(plans, 0o700); err != nil || os.WriteFile(filepath.Join(plans, "p_00000000abcd.json"), []byte("{not json"), 0o600) != nil {
		t.Fatal(err)
	}
	s.settings(levels([]string{userA, userB, userRead}, map[string]string{userA: "use"})) // an administrator: past ADMIN_ONLY
	if code := s.code(userA, "ha_apply", map[string]any{"plan_id": "p_00000000abcd"}); code != "CHAT_LEVEL" {
		t.Errorf("an unreadable plan at Use the home: %q", code)
	}
	errInfo, _ := s.tool(userB, "ha_plan", map[string]any{"kind": "call", "service": "light.turn_off"})["error"].(map[string]any)
	if next, _ := errInfo["next"].(string); !strings.HasPrefix(next, "Call ha_call with the same service and targets instead") {
		t.Errorf("a call plan at Use the home: %v", errInfo)
	}
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if len(s.posts()) != 1 || s.f.states["light.kitchen"]["state"] != "off" {
		t.Fatalf("sent %v", s.posts())
	}
}

// Markus's call (2026-10-29): at Use the home, the skill ha-nova:media on a
// media player, no other skill, and that skill on nothing else; Ask only
// refuses all of it, and apply checks the same again on both paths.
func TestServeUseTheHomeMediaSkill(t *testing.T) {
	media := func() *fakeHA {
		f := household(newFakeHA())
		f.ent("media_player.living_room", "cast", "playing", map[string]any{"friendly_name": "Living Room"})
		f.services = f.defaultServices()
		for _, d := range f.services {
			if d["domain"] == "media_player" {
				d["services"].(map[string]any)["volume_set"] = map[string]any{"target": map[string]any{}}
				d["services"].(map[string]any)["play_media"] = map[string]any{"target": map[string]any{}}
			}
		}
		return f
	}
	player := []string{"media_player.living_room"}
	volume := map[string]any{"service": "media_player.volume_set", "entity_ids": player, "data": map[string]any{"volume_level": 0.3}, "skill": "ha-nova:media"}
	play := map[string]any{"service": "media_player.play_media", "entity_ids": player, "skill": "ha-nova:media",
		"data": map[string]any{"media_content_id": "http://example.invalid/a.mp3", "media_content_type": "music"}}
	sent := func(s *serveHarness, service string) bool {
		return slices.ContainsFunc(s.posts(), func(c fakeRelayCall) bool {
			return c.payload["path"] == "/api/services/"+strings.Replace(service, ".", "/", 1)
		})
	}
	call := func(s *serveHarness, user string, args map[string]any) map[string]any { // a preview: applied as the model would
		env := s.tool(user, "ha_call", args)
		if plan, _ := env["plan"].(map[string]any); env["status"] == "needs_confirmation" && plan != nil {
			env = s.tool(user, "ha_apply", map[string]any{"plan_id": plan["id"]})
		}
		return env
	}
	s := newServe(t, media(), levels([]string{userB}, nil))
	for _, args := range []map[string]any{volume, play} {
		if env := call(s, userB, args); envCode(env) != "" || !sent(s, args["service"].(string)) {
			t.Errorf("%v at Use the home: %v", args["service"], env)
		}
	}
	for _, args := range []map[string]any{
		{"service": "media_player.volume_set", "entity_ids": player, "data": map[string]any{"volume_level": 0.3}}, // no skill: today's refusal
		{"service": "media_player.volume_set", "entity_ids": player, "data": map[string]any{"volume_level": 0.3}, "skill": "ha-nova:service-call"},
		{"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}, "skill": "ha-nova:media"},
		{"service": "tts.speak", "entity_ids": []string{"tts.home"}, "skill": "ha-nova:media"},
		{"service": "notify.notify", "skill": "ha-nova:media"},
	} {
		before := len(s.posts())
		if env := s.tool(userB, "ha_call", args); envCode(env) != "CHAT_LEVEL" || len(s.posts()) != before {
			t.Errorf("%v at Use the home: %v", args, env)
		}
	}
	ask := newServe(t, media(), levels([]string{userB}, map[string]string{userB: "ask"}))
	for _, args := range []map[string]any{volume, play} {
		if env := ask.tool(userB, "ha_call", args); envCode(env) != "CHAT_LEVEL" || len(ask.posts()) != 0 {
			t.Errorf("%v at Ask only: %v", args["service"], env)
		}
	}
	demote := func(f *fakeHA) {
		f.mu.Lock()
		f.users[1]["group_ids"] = []string{"system-users"}
		f.mu.Unlock()
	}
	planned := func(s *serveHarness, args map[string]any) any {
		withKind := map[string]any{"kind": "call"}
		for k, v := range args {
			withKind[k] = v
		}
		plan, _ := s.tool(userA, "ha_plan", withKind)["plan"].(map[string]any)
		if plan == nil {
			t.Fatalf("no plan for %v", args)
		}
		return plan["id"]
	}
	t.Run("apply, no code", func(t *testing.T) {
		f := media()
		s := newServe(t, f, levels([]string{userA}, nil))
		media, light := planned(s, volume), planned(s, map[string]any{"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}, "skill": "ha-nova:media"})
		demote(f)
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": media}); envCode(env) != "" || !sent(s, "media_player.volume_set") {
			t.Fatalf("the media player at Use the home: %v", env)
		}
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": light}); envCode(env) != "CHAT_LEVEL" || sent(s, "light.turn_off") {
			t.Fatalf("ha-nova:media on a light at Use the home: %v", env)
		}
	})
	t.Run("apply, phone", func(t *testing.T) { // an unlock Use the home may tap for, but not with a skill
		f := media()
		s := newServe(t, f, levels([]string{userA}, nil))
		p := usePhones(t, "mobile_app_anna")
		p.setup.confirmer = userA
		id := planned(s, map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}, "skill": "ha-nova:media"})
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "waiting_for_phone" {
			t.Fatalf("before the tap: %v", env)
		}
		demote(f)
		ok, _ := p.buttons(t)
		p.events <- tap(ok, userA, "REMOTE")
		waitFor(t, "the result on the phone", func() bool { return p.sentCount() == 2 })
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); envCode(env) != "CHAT_LEVEL" || len(s.posts()) != 0 {
			t.Fatalf("after the tap: %v (sent %d)", env, len(s.posts()))
		}
	})
}
