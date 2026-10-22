package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Phone confirmations for chat apps (mcp_serve_phone.go).

func (p *fakePhones) sentCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent)
}

// buttons: the Confirm and Cancel ids of the first notification.
func (p *fakePhones) buttons(t *testing.T) (ok, no string) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	actions := p.sent[0]["data"].(map[string]any)["actions"].([]any)
	return actions[0].(map[string]any)["action"].(string), actions[1].(map[string]any)["action"].(string)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func phonePlanFor(t *testing.T, s *serveHarness, user string) string {
	t.Helper()
	env := s.tool(user, "ha_plan", map[string]any{"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	plan, _ := env["plan"].(map[string]any)
	if plan["channel"] != channelPhone || plan["code"] != nil || !strings.Contains(env["next_step"].(string), "a notification on their phone") {
		t.Fatalf("phone plan: %v", env)
	}
	return plan["id"].(string)
}

// ha_apply waits briefly, then answers waiting_for_phone; calling again
// waits on the same notification. A tap applies at once, also while no
// call waits, and the next ha_apply returns that result.
func TestServeWaitsForThePhoneAndNeverRenotifies(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
	p := usePhones(t, "mobile_app_a")
	p.setup.confirmer = userA
	id := phonePlanFor(t, s, userA)
	for i := range 2 {
		start := time.Now()
		env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id})
		if env["status"] != "waiting_for_phone" || env["error"] != nil || !strings.Contains(env["next_step"].(string), "call ha_apply again with plan_id "+id) ||
			time.Since(start) > 3*time.Second || p.sentCount() != 1 || len(s.posts()) != 0 {
			t.Fatalf("call %d: %v notifications %d posts %d", i, env, p.sentCount(), len(s.posts()))
		}
	}
	p.mu.Lock()
	message, _ := p.sent[0]["message"].(string)
	p.mu.Unlock()
	if !strings.Contains(message, "Requested in your AI chat app.") {
		t.Fatalf("notification text: %q", message)
	}
	ok, _ := p.buttons(t)
	p.events <- tap(ok, userA, "REMOTE")
	waitFor(t, "the apply after the tap", func() bool { return len(s.posts()) == 1 })
	waitFor(t, "the result on the phone", func() bool { return p.sentCount() == 2 })
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "verified" {
		t.Fatalf("after the tap: %v", env)
	}
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); envCode(env) != "PLAN_NOT_FOUND" || p.sentCount() != 2 || len(s.posts()) != 1 {
		t.Fatalf("used plan: %v notifications %d", env, p.sentCount())
	}
	if strings.Contains(s.logs.String(), "HANOVA_") {
		t.Fatal("a phone id reached the log")
	}
}

// Two users at once: A's open phone ask blocks nobody, B never sees or
// applies A's plan, and B's typed plan is never asked on A's phone.
func TestServeTwoUsersConcurrently(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`","`+userB+`"]}`)
	p := usePhones(t, "mobile_app_a")
	p.setup.confirmer = userA
	id := phonePlanFor(t, s, userA)
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "waiting_for_phone" {
		t.Fatalf("A: %v", env)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 16)
	for i := range 8 {
		user := []string{userA, userB}[i%2]
		wg.Go(func() {
			start := time.Now()
			env := s.tool(user, "ha_get", map[string]any{"entity_id": "lock.front"})
			if env["status"] != "ok" || time.Since(start) > 3*time.Second {
				errs <- user + ": " + env["status"].(string)
			}
		})
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error("read during A's ask:", e)
	}
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": id}); !s.likeUnknown(userB, id, env) || p.sentCount() != 1 {
		t.Fatalf("B re-calls A's waiting plan: %v", env)
	}
	env := s.tool(userB, "ha_plan", map[string]any{"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}})
	if errInfo, _ := env["error"].(map[string]any); envCode(env) != "CONFIRM_UNAVAILABLE" || errInfo["next"] != chatPhoneMissing {
		t.Fatalf("B's plan on A's phone: %v", env)
	}
	_, no := p.buttons(t)
	p.events <- tap(no, userA, "REMOTE")
	waitFor(t, "A's ask to end", func() bool { return p.sentCount() == 2 })
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); envCode(env) != "CONFIRM_DECLINED" || len(s.posts()) != 0 || p.sentCount() != 2 {
		t.Fatalf("A declined: %v posts %d", env, len(s.posts()))
	}
}

// The bridge's link store: each user sees only their own link; an
// unreadable store is unknown, never "no phone".
func TestServePhoneLinksArePerUser(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "phone-links.json")
	defer func() { chatCall = nil }()
	chatCall = &chatPolicy{user: userA, links: file}
	if links, ok := linksForCall(runtimePaths{}); !ok || len(links) != 0 {
		t.Fatalf("missing store: %v %v", links, ok)
	}
	store, _ := json.Marshal(map[string]phoneLink{
		userA: {Service: "mobile_app_a", DeviceID: "dev-a", UserID: userA, Name: "A"},
		userB: {Service: "mobile_app_b", DeviceID: "dev-b", UserID: userB, Name: "B"}})
	if err := os.WriteFile(file, store, 0o600); err != nil {
		t.Fatal(err)
	}
	links, ok := linksForCall(runtimePaths{})
	if !ok || len(links) != 1 || linkFor(links, "any-owner").link.DeviceID != "dev-a" {
		t.Fatalf("A's links: %v", links)
	}
	chatCall.user = "cccccccccccccccccccccccccccccccc"
	if links, ok := linksForCall(runtimePaths{}); !ok || len(links) != 0 {
		t.Fatalf("unlinked user: %v", links)
	}
	if err := os.WriteFile(file, []byte(`{"`+userA+`":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := linksForCall(runtimePaths{}); ok {
		t.Fatal("a broken store read as readable")
	}
	// A linked phone whose confirmer is someone else is refused.
	saved := phoneDiscover
	defer func() { phoneDiscover = saved }()
	phoneDiscover = func(*relaySession) (phoneSetup, phoneStatus) {
		return phoneSetup{confirmer: userA, targets: []phoneTarget{{Service: "mobile_app_a"}}}, phoneReady
	}
	chatCall.user = userB
	if _, status := discoverConfirmer(nil); status != phoneMissing {
		t.Fatalf("B on A's phone: %v", status)
	}
	chatCall.user = userA
	if _, status := discoverConfirmer(nil); status != phoneReady {
		t.Fatalf("A on A's phone: %v", status)
	}
}

// Two calls waiting on one ask both get the outcome (each its own copy),
// and a tap after the owner set read only (or removed the user) sends
// nothing.
func TestServeTapAfterThePolicyChangedSendsNothing(t *testing.T) {
	for name, settings := range map[string]string{
		"read only": `{"allowed_users":["` + userA + `"],"read_only":true}`,
		"removed":   `{"allowed_users":["` + userB + `"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
			p := usePhones(t, "mobile_app_a")
			p.setup.confirmer = userA
			id := phonePlanFor(t, s, userA)
			s.b.wait = 5 * time.Second
			var wg sync.WaitGroup
			results := make([]map[string]any, 2)
			for i := range results {
				wg.Go(func() { results[i] = s.tool(userA, "ha_apply", map[string]any{"plan_id": id}) })
			}
			waitFor(t, "the notification", func() bool { return p.sentCount() == 1 })
			if err := os.WriteFile(filepath.Join(s.data, "chat-apps.json"), []byte(settings), 0o600); err != nil {
				t.Fatal(err)
			}
			ok, _ := p.buttons(t)
			p.events <- tap(ok, userA, "REMOTE")
			wg.Wait()
			for i, env := range results {
				if code := envCode(env); code != "READ_ONLY" && code != "CHAT_APPS_REFUSED" {
					t.Fatalf("call %d: %v", i, env)
				}
			}
			waitFor(t, "the result on the phone", func() bool { return p.sentCount() == 2 })
			if len(s.posts()) != 0 {
				t.Fatalf("sent after the policy changed: %d", len(s.posts()))
			}
		})
	}
}

// One open phone ask per user: a second phone plan waits for the first
// instead of stacking notifications.
func TestServeOneOpenPhoneAskPerUser(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
	p := usePhones(t, "mobile_app_a")
	p.setup.confirmer = userA
	first, second := phonePlanFor(t, s, userA), phonePlanFor(t, s, userA)
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": first}); env["status"] != "waiting_for_phone" {
		t.Fatalf("first: %v", env)
	}
	env := s.tool(userA, "ha_apply", map[string]any{"plan_id": second})
	if errInfo, _ := env["error"].(map[string]any); envCode(env) != "CONFIRM_PENDING" || !strings.Contains(errInfo["next"].(string), first) || p.sentCount() != 1 {
		t.Fatalf("second: %v notifications %d", env, p.sentCount())
	}
	_, no := p.buttons(t)
	p.events <- tap(no, userA, "REMOTE")
	waitFor(t, "the first ask to end", func() bool { return p.sentCount() == 2 })
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": first}); envCode(env) != "CONFIRM_DECLINED" {
		t.Fatalf("first after cancel: %v", env)
	}
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": second}); env["status"] != "waiting_for_phone" || p.sentCount() != 3 {
		t.Fatalf("second after the first ended: %v", env)
	}
	ok2 := ""
	p.mu.Lock()
	ok2 = p.sent[2]["data"].(map[string]any)["actions"].([]any)[1].(map[string]any)["action"].(string)
	p.mu.Unlock()
	p.events <- tap(ok2, userA, "REMOTE") // cancel, so no ask outlives the test
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": second}); envCode(env) != "CONFIRM_DECLINED" {
		t.Fatalf("second after cancel: %v", env)
	}
}

// A chat app's code note: a real home points to linking a phone; the demo
// house can't link one, so its note says what a home does instead.
func TestChatNoteDemo(t *testing.T) {
	previous := activeServerProfile()
	t.Cleanup(func() { chatCall = nil; setActiveServerProfile(previous) })
	if got := chatNote(); got != sayChatNote {
		t.Fatalf("terminal: %q", got)
	}
	chatCall = &chatPolicy{codes: true}
	if got := chatNote(); got != chatCodeNote {
		t.Fatalf("home: %q", got)
	}
	setActiveDemoProfile()
	if got := chatNote(); got != demoChatCodeNote || strings.Contains(got, "link your phone") {
		t.Fatalf("demo: %q", got)
	}
}
