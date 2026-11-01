package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Chat-app applies as jobs, and the cap on every chat-app wait for the
// verb lock (mcp_serve_apply.go).

// holdPost: the first POST to a path starting with prefix waits for
// release (at the latest when the test ends). It holds the fake's lock, so
// meanwhile a test reads only the call log, and the Relay gives up on it
// after 15 s.
func holdPost(t *testing.T, f *fakeHA, prefix string) (reached chan struct{}, release func()) {
	reached, held := make(chan struct{}), make(chan struct{})
	var once, released sync.Once
	release = func() { released.Do(func() { close(held) }) }
	t.Cleanup(release)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extra = func(_ string, p map[string]any) (any, bool) {
		if p["method"] == "POST" && strings.HasPrefix(fmt.Sprint(p["path"]), prefix) {
			once.Do(func() { close(reached); <-held })
		}
		return nil, false
	}
	return reached, release
}

// holdVerbLock: a change runs until release (at the latest when the test
// ends).
func holdVerbLock(t *testing.T) (release func()) {
	mcpVerbLock.Lock()
	var once sync.Once
	release = func() { once.Do(mcpVerbLock.Unlock) }
	t.Cleanup(release)
	return release
}

func shortLockWait(t *testing.T) {
	old := chatLockWait
	chatLockWait = 100 * time.Millisecond
	t.Cleanup(func() { chatLockWait = old })
}

func scriptPlan(t *testing.T, s *serveHarness, user string) string {
	t.Helper()
	plan, _ := s.tool(user, "ha_plan", map[string]any{"kind": "write", "domain": "script", "id": "s1", "draft": arrival("light.turn_off")})["plan"].(map[string]any)
	if plan == nil {
		t.Fatal("no plan")
	}
	return plan["id"].(string)
}

// serverFor: the tool server one request of user gets (ServeHTTP).
func serverFor(s *serveHarness, user string) *mcpServer {
	call, _, _ := s.b.policyFor(user)
	paths := s.b.paths
	paths.CacheDir = filepath.Join(s.data, "users", call.key)
	return &mcpServer{paths: paths, out: &bytes.Buffer{}, pin: s.b.pin, noForms: true, told: s.b.toldFor(user), chat: call}
}

func nextOf(env map[string]any) string {
	if next, ok := env["next_step"].(string); ok {
		return next
	}
	errInfo, _ := env["error"].(map[string]any)
	next, _ := errInfo["next"].(string)
	return next
}

// A slow apply answers applying within the bridge's wait; calling again
// joins the same run without the lock, and its outcome is handed out once.
// Meanwhile, another tool call answers BUSY and points to the running one.
func TestServeSlowApplyIsAJob(t *testing.T) {
	shortLockWait(t)
	f := household(batchHome())
	s := newServe(t, f, levels([]string{userA}, nil))
	id := scriptPlan(t, s, userA)
	reached, release := holdPost(t, f, "/api/config/script/config/")
	apply := map[string]any{"plan_id": id}
	for i := range 2 { // the job holds the lock: a call that waited for it would answer BUSY
		start := time.Now()
		env := s.tool(userA, "ha_apply", apply)
		if env["status"] != "applying" || env["say"] != confirmT("en", "say.applying") || env["error"] != nil ||
			!strings.Contains(nextOf(env), "call ha_apply again with plan_id "+id) || time.Since(start) > 3*time.Second {
			t.Fatalf("call %d: %v", i, env)
		}
		<-reached
	}
	if env := s.tool(userA, "ha_get", map[string]any{"entity_id": "light.kitchen"}); envCode(env) != "BUSY" ||
		!strings.Contains(nextOf(env), "(plan_id "+id+") is still running: call ha_apply with that plan_id") {
		t.Fatalf("a read meanwhile: %v", env)
	}
	release()
	var env map[string]any
	waitFor(t, "the outcome", func() bool { env = s.tool(userA, "ha_apply", apply); return env["status"] != "applying" })
	if env["status"] != "verified" || len(s.writes()) != 1 {
		t.Fatalf("outcome: %v (writes %v)", env, s.writes())
	}
	if env := s.tool(userA, "ha_apply", apply); envCode(env) != "PLAN_NOT_FOUND" || len(s.writes()) != 1 {
		t.Fatalf("after the outcome: %v (writes %v)", env, s.writes())
	}
	if lines := s.b.readActivity(); len(lines) != 2 || lines[0].What != "write" || lines[0].Result != "done" { // the job notes its change once
		t.Fatalf("activity: %v", lines)
	}
}

// No chat-app call waits longer than chatLockWait for a running change:
// past it, the call answers BUSY, and nothing it asked for runs later.
// Requests that change nothing use Home Assistant's users as read before.
func TestServeBusyRunsNothingLater(t *testing.T) {
	shortLockWait(t)
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	id := scriptPlan(t, s, userA)
	done := holdVerbLock(t)
	for name, args := range map[string]map[string]any{
		"ha_call":  {"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}}, // the users, read again before a change
		"ha_apply": {"plan_id": id},
		"ha_get":   {"entity_id": "light.kitchen"}, // the verb itself
	} {
		env := s.tool(userA, name, args)
		if envCode(env) != "BUSY" || !strings.Contains(nextOf(env), "Never retry on your own") || (name == "ha_apply") != (env["say"] == confirmT("en", "say.busy")) ||
			(name == "ha_apply") != strings.Contains(nextOf(env), "call ha_apply again with plan_id "+id+" and the confirm_code") {
			t.Fatalf("%s: %v", name, env)
		}
	}
	srv, args := serverFor(s, userA), map[string]json.RawMessage{"plan_id": json.RawMessage(strconv.Quote(id))}
	argv, _ := applyArgv(args, nil)
	if env := srv.chatApply("ha_apply", args, argv)["structuredContent"].(map[string]any); envCode(env) != "BUSY" { // the job, once its checks passed
		t.Fatalf("job: %v", env)
	}
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	for age, want := range map[time.Duration]int{2 * time.Minute: http.StatusOK, 20 * time.Minute: http.StatusServiceUnavailable} {
		s.b.mu.Lock()
		s.b.usersAt = time.Now().Add(-age)
		s.b.mu.Unlock()
		start := time.Now()
		if status, out := s.raw(http.MethodPost, s.as(userA), initialize); status != want || time.Since(start) > time.Second ||
			(want != http.StatusOK && !strings.Contains(out, chatBusyWhy)) {
			t.Fatalf("users read %v ago: HTTP %d %q", age, status, out)
		}
	}
	done()
	time.Sleep(300 * time.Millisecond)
	if len(s.posts()) != 0 || len(s.userPlans(userA)) != 1 || !mcpVerbLock.TryLock() {
		t.Fatalf("ran later: posts %d plans %d", len(s.posts()), len(s.userPlans(userA)))
	}
	mcpVerbLock.Unlock()
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "verified" {
		t.Fatalf("asked again: %v", env)
	}
}

// A tool call never runs with Home Assistant's users as read while a
// change held the lock (over a minute ago): it reads them again first.
func TestServeToolCallReadsStaleUsersAgain(t *testing.T) {
	f := household(batchHome())
	s := newServe(t, f, levels([]string{userA}, nil))
	config := map[string]any{"entity_id": "light.kitchen", "config": true}
	if code := s.code(userA, "ha_get", config); code == "CHAT_LEVEL" {
		t.Fatalf("administrator: %s", code)
	}
	f.mu.Lock()
	f.users[1]["group_ids"] = []string{"system-users"} // Anna is no longer an administrator
	f.mu.Unlock()
	s.b.mu.Lock()
	s.b.usersAt = time.Now().Add(-2 * time.Minute)
	s.b.mu.Unlock()
	done := holdVerbLock(t) // a change runs: the request is let in with the users read before
	go func() { time.Sleep(300 * time.Millisecond); done() }()
	if code := s.code(userA, "ha_get", config); code != "CHAT_LEVEL" {
		t.Fatalf("demoted, with the users read before: %s", code)
	}
}

// A job's outcome, too, is handed out only on users read within a minute.
func TestServeJoinReadsStaleUsersAgain(t *testing.T) {
	f := household(batchHome())
	s := newServe(t, f, levels([]string{userA}, nil))
	id := scriptPlan(t, s, userA)
	reached, release := holdPost(t, f, "/api/config/script/config/")
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "applying" {
		t.Fatalf("A: %v", env)
	}
	<-reached
	release()
	waitFor(t, "the job's end", func() bool {
		s.b.mu.Lock()
		defer s.b.mu.Unlock()
		job := s.b.jobs[chatJobKey(userA, id)]
		return job != nil && !job.ended.IsZero()
	})
	f.mu.Lock()
	f.users[1]["group_ids"] = []string{"system-users"} // Anna is no longer an administrator
	f.mu.Unlock()
	s.b.mu.Lock()
	s.b.usersAt = time.Now().Add(-2 * time.Minute)
	s.b.mu.Unlock()
	done := holdVerbLock(t) // another change runs: the join is let in with the users read before
	go func() { time.Sleep(300 * time.Millisecond); done() }()
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); envCode(env) != "CHAT_LEVEL" || !strings.Contains(nextOf(env), chatJobOffList) {
		t.Fatalf("demoted, with the users read before: %v", env)
	}
}

// A join that passed its checks on users read up to a minute before, and
// waited for the job, reads them again before it hands out the outcome.
func TestServeJoinReadsOldUsersAgain(t *testing.T) {
	f := household(batchHome())
	s := newServe(t, f, levels([]string{userA}, nil))
	id := scriptPlan(t, s, userA)
	reached, release := holdPost(t, f, "/api/config/script/config/")
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "applying" {
		t.Fatalf("A: %v", env)
	}
	<-reached
	srv := serverFor(s, userA) // a join that passed its checks while Anna was an administrator
	release()
	waitFor(t, "the write", func() bool { return len(s.writes()) == 1 })
	f.mu.Lock()
	f.users[1]["group_ids"] = []string{"system-users"} // Anna is no longer an administrator
	f.mu.Unlock()
	s.b.mu.Lock()
	s.b.usersAt = time.Now().Add(-2 * time.Minute)
	s.b.mu.Unlock()
	plan := map[string]json.RawMessage{"plan_id": json.RawMessage(strconv.Quote(id))}
	if env := srv.chatJoin("ha_apply", plan)["structuredContent"].(map[string]any); envCode(env) != "CHAT_LEVEL" || !strings.Contains(nextOf(env), chatJobOffList) {
		t.Fatalf("demoted while the join waited: %v", env)
	}
}

// A waiter that gives up never holds the lock.
func TestVerbLockGivesUpWithoutTakingIt(t *testing.T) {
	l := make(verbLock, 1)
	l.Lock()
	if l.lockWithin(10*time.Millisecond) || l.TryLock() {
		t.Fatal("taken twice")
	}
	l.Unlock()
	if !l.TryLock() {
		t.Fatal("still held by the waiter that gave up")
	}
}

// A user whose access the owner lowers below the plan's kind, or removes,
// while their change runs never gets its outcome, only that it may have
// run; nobody else ever sees the job.
func TestServeRemovedUserGetsNoOutcome(t *testing.T) {
	f := household(batchHome())
	s := newServe(t, f, levels([]string{userA, userB}, nil))
	id := scriptPlan(t, s, userA)
	reached, release := holdPost(t, f, "/api/config/script/config/")
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "applying" {
		t.Fatalf("A: %v", env)
	}
	<-reached
	plan := map[string]json.RawMessage{"plan_id": json.RawMessage(strconv.Quote(id))}
	if serverFor(s, userB).chatJoin("ha_apply", plan) != nil {
		t.Fatal("B joins A's job")
	}
	srv := serverFor(s, userA) // a call that passed its checks before the change
	join := func() map[string]any { return srv.chatJoin("ha_apply", plan)["structuredContent"].(map[string]any) }
	s.settings(levels([]string{userA, userB}, map[string]string{userA: "use"}))
	if env := join(); envCode(env) != "CHAT_LEVEL" || !strings.Contains(nextOf(env), chatJobOffList) {
		t.Fatalf("lowered A: %v", env)
	}
	s.settings(levels([]string{userB}, nil))
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ha_apply","arguments":{"plan_id":"` + id + `"}}}`
	if status, out := s.raw(http.MethodPost, s.as(userA), body); status != http.StatusForbidden || !strings.Contains(out, "it may have run") {
		t.Fatalf("A after removal: HTTP %d %q", status, out)
	}
	if env := join(); envCode(env) != "CHAT_APPS_REFUSED" || !strings.Contains(nextOf(env), chatJobOffList) {
		t.Fatalf("waiting A: %v", env)
	}
	release()
	waitFor(t, "the write", func() bool { return len(s.writes()) == 1 })
	if env := join(); envCode(env) != "CHAT_APPS_REFUSED" || env["status"] == "verified" {
		t.Fatalf("A after the outcome: %v", env)
	}
	if env := s.tool(userB, "ha_apply", map[string]any{"plan_id": id}); !s.likeUnknown(userB, id, env) {
		t.Fatalf("B on A's ended job: %v", env)
	}
}

// A phone ask waits no longer for a running change than any call (BUSY:
// nothing asked); a tap waits for it as long as it takes. After the tap, a
// phone plan's apply that is still running answers applying, never
// waiting_for_phone.
func TestServePhoneTapThenApplying(t *testing.T) {
	shortLockWait(t)
	f := newFakeHA()
	s := newServe(t, f, `{"allowed_users":["`+userA+`"]}`)
	p := usePhones(t, "mobile_app_a")
	p.setup.confirmer = userA
	id := phonePlanFor(t, s, userA)
	srv, args := serverFor(s, userA), map[string]json.RawMessage{"plan_id": json.RawMessage(strconv.Quote(id))}
	argv, _ := applyArgv(args, nil)
	done := holdVerbLock(t)
	busy := srv.chatApply("ha_apply", args, argv)["structuredContent"].(map[string]any)
	done()
	if envCode(busy) != "BUSY" || p.sentCount() != 0 {
		t.Fatalf("ask while a change runs: %v (notifications %d)", busy, p.sentCount())
	}
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "waiting_for_phone" {
		t.Fatalf("before the tap: %v", env)
	}
	reached, release := holdPost(t, f, "/api/services/lock/unlock")
	ok, _ := p.buttons(t)
	done = holdVerbLock(t) // a change runs past chatLockWait: the tap waits for it
	p.events <- tap(ok, userA, "REMOTE")
	time.Sleep(500 * time.Millisecond)
	done()
	<-reached
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "applying" || env["say"] != confirmT("en", "say.phone_finishing") {
		t.Fatalf("after the tap: %v", env)
	}
	release()
	var env map[string]any
	waitFor(t, "the outcome", func() bool {
		env = s.tool(userA, "ha_apply", map[string]any{"plan_id": id})
		return env["status"] != "applying"
	})
	if env["status"] != "verified" || len(s.posts()) != 1 {
		t.Fatalf("outcome: %v (posts %d)", env, len(s.posts()))
	}
}
