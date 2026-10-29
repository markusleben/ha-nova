package main

import (
	"strings"
	"sync"
	"testing"
)

// Use the home (mcp_serve_levels.go): the allowlist on the service and
// every resolved target, a phone tap for high-consequence calls and never a
// chat code, and the level read again before a pending plan applies.

func TestServeUseTheHomeStaysInsideTheAllowlist(t *testing.T) {
	f := household(newFakeHA())
	f.script("evening", step("light.turn_off", "light.hall"))
	f.ent("switch.child_lock", "zwave_js", "off", map[string]any{"_ec": 0}).ent("button.restart", "esphome", "unknown", map[string]any{"_ec": 1})
	f.ent("switch.porch", "zwave_js", "off", nil).ent("input_select.mode", "input_select", "home", nil)
	f.services = f.defaultServices()
	config := map[string]map[string]any{ // listed, as Home Assistant lists them: only the allowlist refuses them
		"input_select": {"set_options": map[string]any{"target": map[string]any{}}}, "input_boolean": {"reload": map[string]any{}},
		"light": {"reload": map[string]any{}}, "scene": {"create": map[string]any{}, "apply": map[string]any{}}}
	for _, d := range f.services {
		for name, spec := range config[d["domain"].(string)] {
			d["services"].(map[string]any)[name] = spec
		}
	}
	s := newServe(t, f, levels([]string{userB}, nil))
	for code, refused := range map[string][]map[string]any{
		"CHAT_LEVEL": {
			{"service": "switch.turn_on", "entity_ids": []string{"switch.child_lock"}}, // a device's settings: configuration
			{"service": "button.press", "entity_ids": []string{"button.restart"}},      // and diagnostics
			{"service": "homeassistant.turn_on", "entity_ids": []string{"switch.porch", "switch.child_lock"}},
			{"service": "input_select.set_options", "entity_ids": []string{"input_select.mode"}, "data": map[string]any{"options": []string{"home", "away"}}},
			{"service": "input_boolean.reload"},
			{"service": "light.reload"},
			{"service": "scene.create", "data": map[string]any{"scene_id": "evening", "entities": map[string]any{"light.kitchen": "on"}}},
			{"service": "scene.apply", "data": map[string]any{"entities": map[string]any{"light.kitchen": "on"}}},
			{"service": "homeassistant.restart"},
			{"service": "homeassistant.check_config"},
			{"service": "homeassistant.update_entity", "entity_ids": []string{"light.kitchen"}},
			{"service": "group.reload"},
			{"service": "script.turn_off", "entity_ids": []string{"script.evening"}},
			{"service": "script.toggle", "entity_ids": []string{"script.evening"}},
			{"service": "homeassistant.turn_on", "entity_ids": []string{"light.kitchen", "script.evening"}},
			{"service": "light.turn_on", "entity_ids": []string{"lock.front"}},
			{"service": "zwave_js.set_value", "entity_ids": []string{"lock.front"}},
			{"service": "button.press"},
			{"service": "update.install", "skill": "ha-nova:updates"},
			{"service": "hassio.host_reboot", "skill": "ha-nova:service-call"},
			{"service": "media_player.media_pause", "skill": "ha-nova:media"},
			{"service": "media_player.media_pause"}, // owned: its skill is Manage everything's, so no detour through ha_skill
			{"service": "mqtt.publish", "data": map[string]any{"topic": "x"}},
		},
		"REFUSED": { // the tier engine's own refusals, as for anyone
			{"service": "hassio.host_reboot"},
			{"service": "script.reload"},
		},
	} {
		for _, args := range refused {
			if env := s.tool(userB, "ha_call", args); envCode(env) != code || len(s.posts()) != 0 {
				t.Errorf("%v: %v (sent %d)", args, env, len(s.posts()))
			}
		}
	}
	for _, args := range []map[string]any{
		{"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}},
		{"service": "homeassistant.turn_on", "entity_ids": []string{"light.kitchen", "light.hall"}},
		{"service": "switch.turn_on", "entity_ids": []string{"switch.porch"}},
	} {
		if env := s.tool(userB, "ha_call", args); env["status"] != "verified" {
			t.Errorf("%v: %v", args, env)
		}
	}
	env := s.tool(userB, "ha_call", map[string]any{"service": "script.turn_on", "entity_ids": []string{"script.evening"}})
	plan, _ := env["plan"].(map[string]any)
	if env["status"] != "needs_confirmation" || plan == nil {
		t.Fatalf("a script: %v", env)
	}
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": plan["id"]}); envCode(env) != "" || len(s.posts()) != 4 {
		t.Fatalf("a script, applied: %v (sent %d)", env, len(s.posts()))
	}
	env = s.tool(userB, "ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	errInfo, _ := env["error"].(map[string]any)
	next, _ := errInfo["next"].(string)
	if envCode(env) != "CONFIRM_UNAVAILABLE" || env["plan"] != nil || !strings.Contains(next, "They ask “Markus” to link their phone on the HA NOVA page") || len(s.posts()) != 4 {
		t.Fatalf("no phone: %v", env)
	}
}

// Ben links his phone: an unlock asks it, his tap applies.
func TestServeUseTheHomeConfirmsOnItsOwnPhone(t *testing.T) {
	s := newServe(t, household(newFakeHA()), levels([]string{userB}, nil))
	p := usePhones(t, "mobile_app_ben")
	p.setup.confirmer = userB
	env := s.tool(userB, "ha_call", map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	plan, _ := env["plan"].(map[string]any)
	if plan["channel"] != channelPhone || plan["code"] != nil {
		t.Fatalf("unlock: %v", env)
	}
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": plan["id"]}); env["status"] != "waiting_for_phone" || p.sentCount() != 1 {
		t.Fatalf("before the tap: %v", env)
	}
	ok, _ := p.buttons(t)
	p.events <- tap(ok, userB, "REMOTE")
	waitFor(t, "the result on the phone", func() bool { return p.sentCount() == 2 })
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": plan["id"]}); env["status"] != "verified" || len(s.posts()) != 1 {
		t.Fatalf("after the tap: %v", env)
	}
	if _, answer := s.b.controlActivity(); answer["changes"].([]activityLine)[0].Result != "phone" {
		t.Fatalf("activity: %v", answer)
	}
}

// Demoted between the ask and the tap or code: a pending call outside Use
// the home's allowlist and a pending config plan both apply nothing.
func TestServeLevelReadAgainBeforeApply(t *testing.T) {
	demote := func(f *fakeHA) {
		f.mu.Lock()
		f.users[1]["group_ids"] = []string{"system-users"}
		f.mu.Unlock()
	}
	restart := map[string]any{"kind": "call", "service": "homeassistant.restart"}
	t.Run("phone, a restart, role lowered", func(t *testing.T) {
		f := household(newFakeHA())
		s := newServe(t, f, levels([]string{userA}, nil))
		p := usePhones(t, "mobile_app_anna")
		p.setup.confirmer = userA
		plan, _ := s.tool(userA, "ha_plan", restart)["plan"].(map[string]any)
		if plan["channel"] != channelPhone {
			t.Fatalf("restart: %v", plan)
		}
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); env["status"] != "waiting_for_phone" {
			t.Fatalf("before the tap: %v", env)
		}
		demote(f)
		ok, _ := p.buttons(t)
		p.events <- tap(ok, userA, "REMOTE")
		waitFor(t, "the result on the phone", func() bool { return p.sentCount() == 2 })
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); envCode(env) != "CHAT_LEVEL" || len(s.posts()) != 0 {
			t.Fatalf("after the tap: %v (sent %d)", env, len(s.posts()))
		}
	})
	t.Run("phone, a batch, level lowered", func(t *testing.T) {
		s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
		p := usePhones(t, "mobile_app_anna")
		p.setup.confirmer = userA
		id := batchFor(t, s, userA)
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "waiting_for_phone" {
			t.Fatalf("before the tap: %v", env)
		}
		s.settings(levels([]string{userA}, map[string]string{userA: "use"}))
		ok, _ := p.buttons(t)
		p.events <- tap(ok, userA, "REMOTE")
		waitFor(t, "the result on the phone", func() bool { return p.sentCount() == 2 })
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); envCode(env) != "CHAT_LEVEL" || len(s.writes()) != 0 {
			t.Fatalf("after the tap: %v (writes %d)", env, len(s.writes()))
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if message, _ := p.sent[1]["message"].(string); !strings.Contains(message, "Your tap arrived, but HA NOVA stopped. Nothing changed. The chat says why.") {
			t.Fatalf("the phone's result: %q", message)
		}
	})
	t.Run("phone, users unreadable at the tap", func(t *testing.T) {
		f := household(newFakeHA())
		s := newServe(t, f, levels([]string{userA}, nil))
		p := usePhones(t, "mobile_app_anna")
		p.setup.confirmer = userA
		plan, _ := s.tool(userA, "ha_plan", restart)["plan"].(map[string]any)
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); env["status"] != "waiting_for_phone" {
			t.Fatalf("before the tap: %v", env)
		}
		f.mu.Lock()
		f.extra = func(endpoint string, p map[string]any) (any, bool) {
			return fakeRelayFailure{"UPSTREAM_ERROR", "boom"}, endpoint == "ws" && p["type"] == "config/auth/list"
		}
		f.mu.Unlock()
		ok, _ := p.buttons(t)
		p.events <- tap(ok, userA, "REMOTE")
		waitFor(t, "the result on the phone", func() bool { return p.sentCount() == 2 })
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); envCode(env) != "ACCESS_UNKNOWN" || len(s.posts()) != 0 {
			t.Fatalf("after the tap: %v (sent %d)", env, len(s.posts()))
		}
	})
	t.Run("code, a restart, role lowered", func(t *testing.T) {
		f := household(newFakeHA())
		s := newServe(t, f, levels([]string{userA}, nil))
		plan, _ := s.tool(userA, "ha_plan", restart)["plan"].(map[string]any)
		if plan["channel"] != channelChat || plan["code"] == nil {
			t.Fatalf("restart: %v", plan)
		}
		demote(f)
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"], "confirm_code": plan["code"]}); envCode(env) != "CONFIRM_UNAVAILABLE" || len(s.posts()) != 0 {
			t.Fatalf("applied: %v (sent %d)", env, len(s.posts()))
		}
	})
	t.Run("no code, a call outside the allowlist, role lowered", func(t *testing.T) { // apply's recompute decides
		f := household(newFakeHA())
		s := newServe(t, f, levels([]string{userA}, nil))
		plan, _ := s.tool(userA, "ha_plan", map[string]any{"kind": "call", "service": "homeassistant.check_config"})["plan"].(map[string]any)
		if plan == nil || plan["code"] != nil {
			t.Fatalf("check_config: %v", plan)
		}
		demote(f)
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); envCode(env) != "CHAT_LEVEL" || len(s.posts()) != 0 {
			t.Fatalf("applied: %v (sent %d)", env, len(s.posts()))
		}
	})
	t.Run("code, a script write, level lowered", func(t *testing.T) {
		s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
		plan, _ := s.tool(userA, "ha_plan", map[string]any{"kind": "write", "domain": "script", "id": "s1", "draft": arrival("light.turn_off")})["plan"].(map[string]any)
		if plan == nil {
			t.Fatal("no plan")
		}
		s.settings(levels([]string{userA}, map[string]string{userA: "use"}))
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": plan["id"]}); envCode(env) != "CHAT_LEVEL" || len(s.writes()) != 0 {
			t.Fatalf("applied: %v (writes %d)", env, len(s.writes()))
		}
	})
}

// A change the owner saves while a request waits (here: for Home
// Assistant's users, read under the verb lock) holds: the verb reads the
// settings again under the lock, after the request's own checks passed.
func TestServeLevelHoldsUnderTheVerbLock(t *testing.T) {
	f := household(batchHome())
	var mu sync.Mutex
	var hold chan struct{} // set: the next users read waits for it
	reached := make(chan struct{}, 1)
	f.extra = func(endpoint string, p map[string]any) (any, bool) { // runs under f.mu
		if endpoint != "ws" || p["type"] != "config/auth/list" {
			return nil, false
		}
		mu.Lock()
		h := hold
		hold = nil
		mu.Unlock()
		if h != nil {
			reached <- struct{}{}
			<-h
		}
		return f.users, true
	}
	s := newServe(t, f, levels([]string{userA}, nil))
	plan, _ := s.tool(userA, "ha_plan", map[string]any{"kind": "write", "domain": "script", "id": "s1", "draft": arrival("light.turn_off")})["plan"].(map[string]any)
	if plan == nil {
		t.Fatal("no plan")
	}
	meanwhile := func(settings, name string, args map[string]any) map[string]any {
		s.settings(levels([]string{userA}, nil))
		h := make(chan struct{})
		mu.Lock()
		hold = h
		mu.Unlock()
		var env map[string]any
		done := make(chan struct{})
		go func() { env = s.tool(userA, name, args); close(done) }()
		<-reached // the request read the settings and waits for the users
		s.settings(settings)
		close(h)
		<-done
		return env
	}
	lower := func(level string) string { return levels([]string{userA}, map[string]string{userA: level}) }
	if env := meanwhile(lower("ask"), "ha_call", map[string]any{"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}}); envCode(env) != "CHAT_LEVEL" || len(s.posts()) != 0 {
		t.Fatalf("lowered to Ask only: %v (sent %d)", env, len(s.posts()))
	}
	if env := meanwhile(lower("use"), "ha_apply", map[string]any{"plan_id": plan["id"]}); envCode(env) != "CHAT_LEVEL" || len(s.writes()) != 0 || len(s.userPlans(userA)) != 1 {
		t.Fatalf("lowered to Use the home: %v (writes %d, plans %d)", env, len(s.writes()), len(s.userPlans(userA)))
	}
	if env := meanwhile(levels([]string{userOwner}, nil), "ha_call", map[string]any{"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}}); envCode(env) != "CHAT_APPS_REFUSED" || len(s.posts()) != 0 {
		t.Fatalf("removed: %v (sent %d)", env, len(s.posts()))
	}
}

// batchFor: user's batch of a script write and an automation delete.
func batchFor(t *testing.T, s *serveHarness, user string) string {
	t.Helper()
	planID := func(args map[string]any) any {
		t.Helper()
		plan, _ := s.tool(user, "ha_plan", args)["plan"].(map[string]any)
		if plan == nil {
			t.Fatalf("no plan for %v", args)
		}
		return plan["id"]
	}
	ids := []any{planID(map[string]any{"kind": "write", "domain": "script", "id": "s1", "draft": arrival("light.turn_off")}),
		planID(map[string]any{"kind": "delete", "entity_id": "automation.kitchen_motion_old"})}
	return planID(map[string]any{"kind": "batch", "plans": ids}).(string)
}

// A batch is configuration all of it: below Manage everything it is
// neither planned nor applied, whatever its members.
func TestServeBatchNeverBelowManageEverything(t *testing.T) {
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	members := []any{}
	for _, args := range []map[string]any{{"kind": "write", "domain": "script", "id": "s2", "draft": arrival("light.turn_on")},
		{"kind": "delete", "entity_id": "automation.kitchen_motion_old"}} {
		plan, _ := s.tool(userA, "ha_plan", args)["plan"].(map[string]any)
		members = append(members, plan["id"])
	}
	id := batchFor(t, s, userA)
	for _, level := range []string{"use", "ask"} {
		s.settings(levels([]string{userA}, map[string]string{userA: level}))
		if env := s.tool(userA, "ha_plan", map[string]any{"kind": "batch", "plans": members}); envCode(env) != "CHAT_LEVEL" {
			t.Errorf("%s, planned: %v", level, env)
		}
		if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); envCode(env) != "CHAT_LEVEL" {
			t.Errorf("%s, applied: %v", level, env)
		}
	}
	if len(s.writes()) != 0 || len(s.userPlans(userA)) != 3 { // the members, and the batch that used up its own
		t.Fatalf("writes %d, plans %d", len(s.writes()), len(s.userPlans(userA)))
	}
}

// An entity registry Home Assistant doesn't answer can't tell a device's
// settings from the device: below Manage everything, no call with a target
// runs.
func TestServeUseTheHomeNeedsTheRegistry(t *testing.T) {
	f := household(newFakeHA())
	f.fail["config/entity_registry/list_for_display"] = true
	s := newServe(t, f, levels([]string{userB}, nil))
	if env := s.tool(userB, "ha_call", map[string]any{"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}}); envCode(env) != "ACCESS_UNKNOWN" || len(s.posts()) != 0 {
		t.Fatalf("registry unread: %v (sent %d)", env, len(s.posts()))
	}
}
