package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// How long a chat-app call waits for the verb lock (mcp_serve_apply.go):
// behind other requests until its cap, behind a running change
// chatLockWait.

// slowRelay: every Relay request but the users read takes d, so each verb
// holds the lock a while.
func slowRelay(f *fakeHA, d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.extra = func(endpoint string, p map[string]any) (any, bool) {
		if endpoint != "ws" || p["type"] != "config/auth/list" {
			time.Sleep(d)
		}
		return nil, false
	}
}

// parallelTools sends one tool call per args at once, as user, and
// returns their envelopes in order (nil: no envelope).
func (s *serveHarness) parallelTools(user, name string, args []map[string]any) []map[string]any {
	envs := make([]map[string]any, len(args))
	var wg sync.WaitGroup
	for i, a := range args {
		wg.Go(func() {
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": i + 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": a}})
			req, _ := http.NewRequest(http.MethodPost, s.url, bytes.NewReader(body))
			for k, values := range s.as(user) {
				for _, v := range values {
					req.Header.Add(k, v)
				}
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			var msg struct {
				Result struct {
					StructuredContent map[string]any `json:"structuredContent"`
				} `json:"result"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&msg)
			envs[i] = msg.Result.StructuredContent
		})
	}
	wg.Wait()
	return envs
}

// Several plans sent at once (one per automation) queue for the lock
// longer than chatLockWait while no change runs: none answers BUSY.
func TestServeParallelPlansWaitForEachOther(t *testing.T) {
	shortLockWait(t)
	f := household(batchHome())
	s := newServe(t, f, levels([]string{userA}, nil))
	slowRelay(f, 15*time.Millisecond)
	var args []map[string]any
	for i := range 8 {
		args = append(args, map[string]any{"kind": "write", "domain": "script", "id": fmt.Sprintf("nova_test_%d", i+1), "draft": arrival("light.turn_off")})
	}
	start := time.Now()
	envs := s.parallelTools(userA, "ha_plan", args)
	took := time.Since(start)
	for i, env := range envs {
		if plan, _ := env["plan"].(map[string]any); plan == nil || env["error"] != nil {
			t.Errorf("plan %d: %v", i+1, env)
		}
	}
	if took < 2*chatLockWait {
		t.Fatalf("the plans took %v: too fast to queue past chatLockWait", took)
	}
	if plans := s.userPlans(userA); len(plans) != len(args) {
		t.Fatalf("plans stored: %d", len(plans))
	}
}

// shortCallWait: a request waits d for the lock in all.
func shortCallWait(t *testing.T, d time.Duration) {
	old := chatCallWait
	chatCallWait = d
	t.Cleanup(func() { chatCallWait = old })
}

// otherRequest: a request that changes nothing holds the lock until
// release (at the latest when the test ends).
func otherRequest(t *testing.T) (release func()) {
	mcpVerbLock.Lock()
	var once sync.Once
	release = func() { once.Do(mcpVerbLock.Unlock) }
	t.Cleanup(release)
	return release
}

func errMessage(env map[string]any) string {
	errInfo, _ := env["error"].(map[string]any)
	message, _ := errInfo["message"].(string)
	return message
}

// A running change (an apply job) still makes another call answer BUSY
// with the change's words after chatLockWait, long before chatCallWait.
func TestServeRunningChangeStillMakesCallsBusy(t *testing.T) {
	shortLockWait(t)
	shortCallWait(t, 5*time.Second)
	f := household(batchHome())
	s := newServe(t, f, levels([]string{userA}, nil))
	id := scriptPlan(t, s, userA)
	reached, release := holdPost(t, f, "/api/config/script/config/")
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "applying" {
		t.Fatalf("apply: %v", env)
	}
	<-reached
	start := time.Now()
	env := s.tool(userA, "ha_get", map[string]any{"entity_id": "light.kitchen"})
	if took := time.Since(start); envCode(env) != "BUSY" || !strings.Contains(errMessage(env), chatBusyWhy) ||
		!strings.Contains(nextOf(env), "Never retry on your own") || took > chatCallWait/2 {
		t.Fatalf("a read meanwhile, after %v: %v", took, env)
	}
	release()
	waitFor(t, "the outcome", func() bool { return s.tool(userA, "ha_apply", map[string]any{"plan_id": id})["status"] != "applying" })
}

// Behind other requests only, a call waits chatCallWait in all, then
// answers BUSY: nothing ran, and calling again right away is safe (never
// the change's words or say). Nothing it asked for runs later.
func TestServeQueueCapSaysCallAgain(t *testing.T) {
	shortLockWait(t)
	shortCallWait(t, 400*time.Millisecond)
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	id := scriptPlan(t, s, userA)
	release := otherRequest(t)
	for name, args := range map[string]map[string]any{
		"ha_get":   {"entity_id": "light.kitchen"},                                         // the verb itself
		"ha_call":  {"service": "light.turn_off", "entity_ids": []string{"light.kitchen"}}, // the users, read again before a change
		"ha_apply": {"plan_id": id},
	} {
		start := time.Now()
		env := s.tool(userA, name, args)
		next := nextOf(env)
		if took := time.Since(start); envCode(env) != "BUSY" || !strings.Contains(errMessage(env), chatQueuedWhy) || took < chatCallWait ||
			!strings.Contains(next, "HA NOVA was still answering other requests") || !strings.Contains(next, "Calling it again right away is safe") ||
			strings.Contains(next, "finishing") || strings.Contains(next, "Never retry") || env["say"] != nil ||
			(name == "ha_apply") != strings.Contains(next, "call ha_apply again with plan_id "+id+" and the confirm_code") {
			t.Fatalf("%s after %v: %v", name, took, env)
		}
	}
	release()
	time.Sleep(100 * time.Millisecond)
	if len(s.posts()) != 0 || len(s.userPlans(userA)) != 1 {
		t.Fatalf("ran later: posts %d plans %d", len(s.posts()), len(s.userPlans(userA)))
	}
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "verified" {
		t.Fatalf("called again: %v", env)
	}
}

// The read of Home Assistant's users before a request waits the same way:
// behind other requests past chatLockWait, until chatCallWait (then 503
// with the queue's words); behind a change chatLockWait (the change's).
func TestServeUsersReadWaitsLikeACall(t *testing.T) {
	shortLockWait(t)
	shortCallWait(t, 500*time.Millisecond)
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	for _, c := range []struct {
		name     string
		hold     func(*testing.T) func()
		release  time.Duration // 0: after the request
		status   int
		why      string
		min, max time.Duration
	}{
		{"behind another request, past chatLockWait", otherRequest, 300 * time.Millisecond, http.StatusOK, "", 300 * time.Millisecond, 500 * time.Millisecond},
		{"behind other requests until chatCallWait", otherRequest, 0, http.StatusServiceUnavailable, chatQueuedWhy, 500 * time.Millisecond, time.Second},
		{"behind a change", holdVerbLock, 0, http.StatusServiceUnavailable, chatBusyWhy, 100 * time.Millisecond, 400 * time.Millisecond},
	} {
		s.b.mu.Lock()
		s.b.usersAt = time.Now().Add(-20 * time.Minute) // too old to let the request in while the lock is held
		s.b.mu.Unlock()
		release := c.hold(t)
		if c.release != 0 {
			go func() { time.Sleep(c.release); release() }()
		}
		start := time.Now()
		status, out := s.raw(http.MethodPost, s.as(userA), initialize)
		took := time.Since(start)
		release()
		if status != c.status || !strings.Contains(out, c.why) || took < c.min || took > c.max {
			t.Fatalf("%s: HTTP %d %q after %v", c.name, status, out, took)
		}
	}
}

// Past chatLockWait, a waiter gives up once a change holds the lock, also
// one that takes it later; until then it waits behind other requests.
func TestVerbLockGivesUpOnAChangeOnly(t *testing.T) {
	shortLockWait(t)
	l := newVerbLock()
	l.Lock() // another request
	changing := make(chan struct{})
	go func() { l.Lock(); l.changing(); close(changing) }() // a change, next in line
	time.Sleep(50 * time.Millisecond)
	got := make(chan error, 1)
	go func() { got <- l.lockChat(time.Now()) }()
	time.Sleep(3 * chatLockWait)
	select {
	case err := <-got:
		t.Fatalf("gave up behind another request: %v", err)
	default:
	}
	l.Unlock()
	<-changing
	select {
	case err := <-got:
		if err != errBridgeBusy {
			t.Fatalf("while a change holds the lock: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("still waiting while a change holds the lock")
	}
	l.Unlock()
	if !l.TryLock() {
		t.Fatal("held by the waiter that gave up")
	}
}

// A waiter past chatLockWait keeps its place in line ahead of one that
// came later: first come, first served.
func TestVerbLockKeepsOrder(t *testing.T) {
	old := chatLockWait
	chatLockWait = 400 * time.Millisecond
	t.Cleanup(func() { chatLockWait = old })
	l := newVerbLock()
	l.Lock()
	order := make(chan int, 2)
	for i := range 2 {
		go func() {
			if l.lockChat(time.Now()) == nil {
				order <- i
				l.Unlock()
			}
		}()
		time.Sleep(chatLockWait / 2)
	}
	time.Sleep(chatLockWait / 4) // the first waiter is past chatLockWait, the second not yet
	l.Unlock()
	for want := range 2 {
		select {
		case got := <-order:
			if got != want {
				t.Fatalf("served %d as number %d", got, want+1)
			}
		case <-time.After(time.Second):
			t.Fatalf("waiter %d never served", want)
		}
	}
}

// An apply job in line for the lock behind other requests past the
// bridge's wait answers applying only once it holds the lock, and BUSY
// (nothing sent) when its request's cap runs out first: applying means it
// runs.
func TestServeApplyingMeansItRuns(t *testing.T) {
	shortLockWait(t)
	shortCallWait(t, 900*time.Millisecond)
	f := household(batchHome())
	s := newServe(t, f, levels([]string{userA}, nil)) // the bridge waits 300 ms
	apply := func(srv *mcpServer, id string) (map[string]any, time.Duration) {
		args := map[string]json.RawMessage{"plan_id": json.RawMessage(strconv.Quote(id))}
		argv, _ := applyArgv(args, nil)
		start := time.Now()
		return srv.chatApply("ha_apply", args, argv)["structuredContent"].(map[string]any), time.Since(start)
	}
	id, srv := scriptPlan(t, s, userA), serverFor(s, userA)
	reached, release := holdPost(t, f, "/api/config/script/config/")
	hold := otherRequest(t)
	go func() { time.Sleep(600 * time.Millisecond); hold() }()
	if env, took := apply(srv, id); env["status"] != "applying" || took < 600*time.Millisecond {
		t.Fatalf("in line, after %v: %v", took, env)
	}
	<-reached
	release()
	waitFor(t, "the outcome", func() bool { return s.tool(userA, "ha_apply", map[string]any{"plan_id": id})["status"] != "applying" })
	id, srv = scriptPlan(t, s, userA), serverFor(s, userA)
	hold = otherRequest(t)
	env, took := apply(srv, id)
	hold()
	if envCode(env) != "BUSY" || !strings.Contains(errMessage(env), chatQueuedWhy) || took < chatCallWait || len(s.writes()) != 1 {
		t.Fatalf("cap first, after %v: %v (writes %d)", took, env, len(s.writes()))
	}
}

// The body is read before the users read, which may wait up to
// chatCallWait: the server's ReadTimeout counts from the request's start.
func TestServeReadsTheBodyBeforeWaiting(t *testing.T) {
	shortLockWait(t)
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	server := httptest.NewUnstartedServer(s.b)
	server.Config.ReadTimeout = 200 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)
	s.url = server.URL + "/mcp"
	s.b.mu.Lock()
	s.b.usersAt = time.Now().Add(-20 * time.Minute) // too old to let the request in while the lock is held
	s.b.mu.Unlock()
	release := otherRequest(t)
	go func() { time.Sleep(400 * time.Millisecond); release() }()
	body := `{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"` + strings.Repeat("x", 256<<10) + `","version":"1"}}}`
	if status, out := s.raw(http.MethodPost, s.as(userA), body); status != http.StatusOK || !strings.Contains(out, `"id":7`) {
		t.Fatalf("HTTP %d %.300q", status, out)
	}
}
