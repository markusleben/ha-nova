package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
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
// answers BUSY: nothing ran, and calling again is safe (never the
// change's words or say). Nothing it asked for runs later.
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
			!strings.Contains(next, "HA NOVA was still answering other requests") || !strings.Contains(next, "Calling it again is safe: ") || !strings.HasSuffix(next, chatQueuedStop) ||
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

// readBodies serves s.b behind a handler that signals on the returned
// channel once each request's body has been read to its end.
func (s *serveHarness) readBodies(t *testing.T) <-chan struct{} {
	read := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = &signalBody{ReadCloser: r.Body, read: read}
		s.b.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	s.url = server.URL + "/mcp"
	return read
}

// signalBody signals on read once the body it wraps is read to its end.
type signalBody struct {
	io.ReadCloser
	read chan<- struct{}
	once sync.Once
}

func (b *signalBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.once.Do(func() { b.read <- struct{}{} })
	}
	return n, err
}

type reply struct {
	status int
	out    string
}

// post sends body as user in the background; the answer arrives on the
// channel (HTTP 0 and the error when there is none).
func (s *serveHarness) post(user, body string) <-chan reply {
	got, url := make(chan reply, 1), s.url
	go func() {
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
		for k, values := range s.as(user) {
			for _, v := range values {
				req.Header.Add(k, v)
			}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			got <- reply{out: err.Error()}
			return
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		got <- reply{resp.StatusCode, string(out)}
	}()
	return got
}

// within: what got delivers, or a fatal error after a generous while.
func within[V any](t *testing.T, what string, got <-chan V) V {
	t.Helper()
	select {
	case v := <-got:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: never came", what)
	}
	var none V
	return none
}

// pending fails when got already delivered.
func pending[V any](t *testing.T, what string, got <-chan V) {
	t.Helper()
	select {
	case v := <-got:
		t.Fatalf("%s: %v", what, v)
	default:
	}
}

// The read of Home Assistant's users before a request waits the same way:
// behind other requests past chatLockWait, until chatCallWait (then 503
// with the queue's words); behind a change chatLockWait (the change's).
func TestServeUsersReadWaitsLikeACall(t *testing.T) {
	shortLockWait(t)
	shortCallWait(t, 500*time.Millisecond)
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	read := s.readBodies(t)
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	for _, c := range []struct {
		name    string
		hold    func(*testing.T) func()
		release bool // once the request waits past chatLockWait
		status  int
		why     string
		min     time.Duration
	}{
		{"behind another request, past chatLockWait", otherRequest, true, http.StatusOK, `"protocolVersion"`, 0},
		{"behind other requests until chatCallWait", otherRequest, false, http.StatusServiceUnavailable, chatQueuedWhy + ", so nothing ran. Calling it again is safe.", chatCallWait},
		{"behind a change", holdVerbLock, false, http.StatusServiceUnavailable, chatBusyWhy, chatLockWait},
	} {
		s.b.mu.Lock()
		s.b.usersAt = time.Now().Add(-20 * time.Minute) // too old to let the request in while the lock is held
		s.b.mu.Unlock()
		release := c.hold(t)
		start := time.Now()
		got := s.post(userA, initialize)
		within(t, c.name+": the body read", read)
		if c.release {
			time.Sleep(2 * chatLockWait) // the request came in before its body was read
			pending(t, c.name+": answered in line", got)
			release()
		}
		r := within(t, c.name, got)
		took := time.Since(start)
		release()
		if r.status != c.status || !strings.Contains(r.out, c.why) || took < c.min {
			t.Fatalf("%s: HTTP %d %q after %v", c.name, r.status, r.out, took)
		}
	}
}

// The App's control channel waits for the users read at most chatLockWait
// in all, also behind other requests: the App gives up after 30 s.
func TestControlUsersReadWaitsLockWaitOnly(t *testing.T) {
	shortLockWait(t)
	shortCallWait(t, 10*time.Second)
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	s.b.mu.Lock()
	s.b.usersAt = time.Now().Add(-20 * time.Minute)
	s.b.mu.Unlock()
	otherRequest(t)
	start := time.Now()
	p, code, why := s.b.policyFor(userA)
	if took := time.Since(start); p != nil || code != http.StatusServiceUnavailable || why != chatQueuedWhy || took < chatLockWait || took > chatCallWait/2 {
		t.Fatalf("after %v: %v %d %q", took, p, code, why)
	}
}

// Past chatLockWait, a waiter gives up once a change holds the lock, also
// one that takes it later; until then it waits behind other requests.
func TestVerbLockGivesUpOnAChangeOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newVerbLock()
		l.Lock() // another request
		changing := make(chan struct{})
		go func() { l.Lock(); l.changing(); close(changing) }() // a change, next in line
		synctest.Wait()
		got := make(chan error, 1)
		go func() { got <- l.lockChat(chatWait(time.Now())) }()
		time.Sleep((chatLockWait + chatCallWait) / 2)
		synctest.Wait()
		pending(t, "gave up behind another request", got)
		l.Unlock()
		<-changing
		synctest.Wait()
		select {
		case err := <-got:
			if err != errBridgeBusy {
				t.Fatalf("while a change holds the lock: %v", err)
			}
		default:
			t.Fatal("still waiting while a change holds the lock")
		}
		l.Unlock()
		if !l.TryLock() {
			t.Fatal("held by the waiter that gave up")
		}
	})
}

// A waiter past chatLockWait keeps its place in line ahead of one that
// came later: first come, first served.
func TestVerbLockKeepsOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		l := newVerbLock()
		l.Lock()
		order := make(chan int, 2)
		for i := range 2 {
			go func() {
				if l.lockChat(chatWait(time.Now())) == nil {
					order <- i
					l.Unlock()
				}
			}()
			time.Sleep(chatLockWait / 2)
		}
		time.Sleep(chatLockWait / 4) // the first waiter is past chatLockWait, the second not yet
		l.Unlock()
		synctest.Wait()
		for want := range 2 {
			select {
			case got := <-order:
				if got != want {
					t.Fatalf("served %d as number %d", got, want+1)
				}
			default:
				t.Fatalf("waiter %d never served", want)
			}
		}
	})
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
	apply := func(srv *mcpServer, id string) map[string]any {
		args := map[string]json.RawMessage{"plan_id": json.RawMessage(strconv.Quote(id))}
		argv, _ := applyArgv(args, nil)
		return srv.chatApply("ha_apply", args, argv)["structuredContent"].(map[string]any)
	}
	id, srv := scriptPlan(t, s, userA), serverFor(s, userA)
	reached, release := holdPost(t, f, "/api/config/script/config/")
	hold := otherRequest(t)
	got := make(chan map[string]any, 1)
	go func() { got <- apply(srv, id) }()
	time.Sleep(time.Until(srv.since.Add((s.b.wait + chatCallWait) / 2))) // past the bridge's wait, before the cap
	pending(t, "answered in line", got)
	hold()
	if env := within(t, "the answer once it holds the lock", got); env["status"] != "applying" {
		t.Fatalf("once it holds the lock: %v", env)
	}
	<-reached
	release()
	waitFor(t, "the outcome", func() bool { return s.tool(userA, "ha_apply", map[string]any{"plan_id": id})["status"] != "applying" })
	id, srv = scriptPlan(t, s, userA), serverFor(s, userA)
	hold = otherRequest(t)
	env := apply(srv, id)
	took := time.Since(srv.since)
	hold()
	if envCode(env) != "BUSY" || !strings.Contains(errMessage(env), chatQueuedWhy) || took < chatCallWait || len(s.writes()) != 1 {
		t.Fatalf("cap first, %v after the request came in: %v (writes %d)", took, env, len(s.writes()))
	}
}

// The body is read before the users read, which may wait up to
// chatCallWait: the server's ReadTimeout counts from the request's start.
func TestServeReadsTheBodyBeforeWaiting(t *testing.T) {
	shortLockWait(t)
	s := newServe(t, household(batchHome()), levels([]string{userA}, nil))
	read := s.readBodies(t)
	s.b.mu.Lock()
	s.b.usersAt = time.Now().Add(-20 * time.Minute) // too old to let the request in while the lock is held
	s.b.mu.Unlock()
	release := otherRequest(t)
	got := s.post(userA, `{"jsonrpc":"2.0","id":7,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"`+strings.Repeat("x", 256<<10)+`","version":"1"}}}`)
	within(t, "the body read while the lock is held", read)
	pending(t, "answered while the lock is held", got)
	release()
	if r := within(t, "the answer", got); r.status != http.StatusOK || !strings.Contains(r.out, `"id":7`) {
		t.Fatalf("HTTP %d %.300q", r.status, r.out)
	}
}
