package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// One more try for the checks before a phone is asked
// (confirm_phone_retry.go): a lookup or listening probe that failed on a
// read runs once more; the notification itself is never sent twice.

var markusLink = phoneLink{Service: "mobile_app_markuss_iphone", DeviceID: "dev-markus", UserID: "owner-id", Name: "Markus’s iPhone", Platform: phoneIOS}

func fastRetry(t *testing.T) {
	saved := phoneRetryPause
	phoneRetryPause = 10 * time.Millisecond
	t.Cleanup(func() { phoneRetryPause = saved })
}

// failReads answers the next n reads that match with a Relay timeout, as a
// slow Home Assistant does; more adds to n.
func failReads(f *fakeHA, n int, match func(endpoint string, p map[string]any) bool) (more func(int)) {
	inner := f.extra
	f.extra = func(endpoint string, p map[string]any) (any, bool) { // under f.mu
		if n > 0 && match(endpoint, p) {
			n--
			return fakeRelayFailure{"UPSTREAM_WS_TIMEOUT", "timed out"}, true
		}
		if inner != nil {
			return inner(endpoint, p)
		}
		return nil, false
	}
	return func(add int) { f.mu.Lock(); n += add; f.mu.Unlock() }
}

func wsRead(kind string) func(string, map[string]any) bool {
	return func(endpoint string, p map[string]any) bool { return endpoint == "ws" && p["type"] == kind }
}

func serviceList(endpoint string, p map[string]any) bool {
	return endpoint == "core" && p["method"] == "GET" && p["path"] == "/api/services"
}

// failProbes fails the next n listening probes; it counts every probe.
func failProbes(n int) (probes func() int) {
	var mu sync.Mutex
	seen, collect := 0, phoneCollect
	phoneCollect = func(ctx context.Context, s *relaySession, span time.Duration) ([]phoneEvent, error) {
		if span < time.Second {
			mu.Lock()
			seen++
			fail := seen <= n
			mu.Unlock()
			if fail {
				return nil, errors.New("UPSTREAM_WS_TIMEOUT")
			}
		}
		return collect(ctx, s, span)
	}
	return func() int { mu.Lock(); defer mu.Unlock(); return seen }
}

// asks: the notifications with buttons the phones got.
func (p *fakePhones) asks() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return countAsks(p.sent)
}

func countAsks(bodies []map[string]any) int {
	n := 0
	for _, body := range bodies {
		if actions, _ := body["data"].(map[string]any)["actions"].([]any); len(actions) == 2 {
			n++
		}
	}
	return n
}

func reads(h *writeHarness, kind string) int {
	n := 0
	for _, t := range h.log.wsTypes() {
		if t == kind {
			n++
		}
	}
	return n
}

func usersReadable(home *fakeHome, f *fakeHA, ok bool) {
	f.mu.Lock()
	home.usersOK = ok
	f.mu.Unlock()
}

// A check that fails once on a read runs once more, and the phone gets the
// question once: alone, and beside the window.
func TestPhoneCheckThatFailsOnceAsksOnce(t *testing.T) {
	for _, c := range []struct {
		name    string
		window  bool
		fail    func(*fakeHA) // before the apply
		probe   int           // probes that fail first
		lookups int           // lookups before the question
	}{
		{name: "lookup read", fail: func(f *fakeHA) { failReads(f, 1, wsRead("config/auth/list")) }, lookups: 2},
		{name: "lookup read, window and phone", window: true, fail: func(f *fakeHA) { failReads(f, 1, wsRead("config/auth/list")) }, lookups: 2},
		{name: "listening probe", probe: 1, lookups: 1},
		{name: "listening probe, window and phone", window: true, probe: 1, lookups: 1},
		{name: "lookup read and probe", fail: func(f *fakeHA) { failReads(f, 1, wsRead("config/auth/list")) }, probe: 1, lookups: 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, p, _ := familyLink(t)
			fastRetry(t)
			seedLink(t, h, &markusLink)
			if c.window {
				useRaceWindow(t, windowNoAnswer, make(chan struct{}), false)
			}
			plan := h.plan("lock.unlock", "lock.front").Plan
			windowWait = 10 * time.Second // the retry runs only inside the ask's wait
			before, probes := reads(h, "config/auth/list"), failProbes(c.probe)
			if c.fail != nil {
				c.fail(h.f)
			}
			var lookups, probed int
			p.tapBy = func(_, ok, _ string) []phoneEvent {
				lookups, probed = reads(h, "config/auth/list")-before, probes()
				return []phoneEvent{tap(ok, "owner-id", "REMOTE")}
			}
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			if exit != 0 || env.Status != "verified" || calls(h) != 1 || p.asks() != 1 || lookups != c.lookups || probed != 1+c.probe {
				t.Fatalf("exit %d %s %+v calls %d asks %d lookups %d probes %d", exit, env.Status, env.Error, calls(h), p.asks(), lookups, probed)
			}
		})
	}
}

// A service list that failed is read again on the second try, never taken
// from what the session remembered of the first.
func TestPhoneLookupReadsAFailedServiceListAgain(t *testing.T) {
	h, _, _ := familyLink(t)
	seedLink(t, h, &markusLink)
	failReads(h.f, 1, serviceList)
	s, err := newRelaySession(h.paths, verbOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if setup, status := discoverPhone(s, time.Now().Add(relaySessionOverallTimeout)); status != phoneMissing || setup.why != whyRead {
		t.Fatalf("first lookup: status %d why %d", status, setup.why)
	}
	if _, status := discoverPhone(s, time.Now().Add(relaySessionOverallTimeout)); status != phoneReady {
		t.Fatalf("second lookup: status %d", status)
	}
}

// Both tries fail: the phone gets nothing, and the agent hears that Home
// Assistant was slow and that nothing on the phone needs fixing.
func TestPhoneCheckThatFailsTwiceSaysHomeAssistantWasSlow(t *testing.T) {
	plain := func(t *testing.T, next string) {
		t.Helper()
		for _, want := range []string{"Home Assistant answered too slowly or a read failed, twice", "Nothing on the phone needs fixing (no notification settings, no limits)"} {
			if !strings.Contains(next, want) {
				t.Fatalf("missing %q in %q", want, next)
			}
		}
		if strings.Contains(next, "could not reach") || strings.Contains(next, "confirm-phone list") {
			t.Fatalf("blames the phone: %q", next)
		}
	}
	for _, probe := range []bool{false, true} {
		what := map[bool]string{false: "lookup", true: "probe"}[probe]
		t.Run("phone, "+what, func(t *testing.T) {
			h, p, home := familyLink(t)
			fastRetry(t)
			seedLink(t, h, &markusLink)
			plan := h.plan("lock.unlock", "lock.front").Plan
			windowWait = 10 * time.Second
			before, probes := reads(h, "config/auth/list"), failProbes(map[bool]int{false: 0, true: 9}[probe])
			usersReadable(home, h.f, probe)
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			tries := map[bool]int{false: reads(h, "config/auth/list") - before, true: probes()}[probe]
			if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(p.sent) != 0 || calls(h) != 0 || tries != 2 ||
				!strings.Contains(env.Error.Message, "answered too slowly or a read failed, twice") {
				t.Fatalf("exit %d %+v sent %d tries %d", exit, env.Error, len(p.sent), tries)
			}
			plain(t, env.Error.Next)
			if !strings.Contains(env.Error.Next, "nothing was sent to their phone") || !strings.Contains(env.Error.Next, "Never ask them for a code") {
				t.Fatalf("next %q", env.Error.Next)
			}
		})
		t.Run("window and phone, "+what, func(t *testing.T) {
			h, p, home := familyLink(t)
			fastRetry(t)
			seedLink(t, h, &markusLink)
			gate := make(chan struct{})
			useRaceWindow(t, windowNoAnswer, gate, false)
			plan := h.plan("lock.unlock", "lock.front").Plan
			windowWait = 10 * time.Second
			failProbes(map[bool]int{false: 0, true: 9}[probe])
			usersReadable(home, h.f, probe)
			go func() { time.Sleep(300 * time.Millisecond); close(gate) }() // the phone gave up first
			var exit int
			var env verbTestEnvelope
			stderr := captureStderr(t, func() { exit, env = h.run(runApplyCommand, "--plan", plan.ID) })
			if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_CANCELLED" || len(p.sent) != 0 || calls(h) != 0 ||
				!strings.Contains(stderr, "so only the window on this computer asks. Nothing on your phone needs fixing.") || strings.Contains(stderr, "can't reach your linked phone") {
				t.Fatalf("exit %d %+v sent %d stderr %q", exit, env.Error, len(p.sent), stderr)
			}
			plain(t, env.Error.Next)
			if !strings.Contains(env.Error.Next, "Nothing was sent to their phone") {
				t.Fatalf("next %q", env.Error.Next)
			}
		})
	}
	t.Run("window can't open, probe", func(t *testing.T) {
		h, p, _ := familyLink(t)
		fastRetry(t)
		seedLink(t, h, &markusLink)
		open := make(chan struct{})
		close(open)
		useRaceWindow(t, windowFailed, open, false)
		plan := h.plan("lock.unlock", "lock.front").Plan
		windowWait = 10 * time.Second
		failProbes(9)
		exit, env := h.run(runApplyCommand, "--plan", plan.ID)
		if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(p.sent) != 0 ||
			!strings.Contains(env.Error.Message, "the confirmation window couldn't open, and HA NOVA couldn't check") {
			t.Fatalf("exit %d %+v sent %d", exit, env.Error, len(p.sent))
		}
		plain(t, env.Error.Next)
	})
}

// The second try stays inside the ask's wait: its read budget ends a
// second before only windowMinWait is left, never a full budget later.
func TestPhoneCheckRetryEndsInsideTheWait(t *testing.T) {
	h, p, _ := familyLink(t)
	fastRetry(t)
	seedLink(t, h, &markusLink)
	plan := h.plan("lock.unlock", "lock.front").Plan
	previous := relaySessionOverallTimeout
	relaySessionOverallTimeout = 2 * time.Second
	t.Cleanup(func() { relaySessionOverallTimeout = previous })
	windowWait = 5 * time.Second // windowMinWait 1 s: the second try ends 3 s in
	h.f.mu.Lock()
	h.f.hang["config/auth/list"] = true
	h.f.mu.Unlock()
	before := reads(h, "config/auth/list")
	start := time.Now()
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	took, lookups := time.Since(start), reads(h, "config/auth/list")-before
	if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || !strings.Contains(env.Error.Next, "twice") || len(p.sent) != 0 ||
		lookups != 2 || took < 2900*time.Millisecond || took > 3600*time.Millisecond {
		t.Fatalf("exit %d %+v sent %d lookups %d after %v", exit, env.Error, len(p.sent), lookups, took)
	}
}

// The re-check after a tap that fails once runs once more, and the tap
// stands; failing twice, nothing runs.
func TestPhoneRecheckAfterTheTapRunsOnceMore(t *testing.T) {
	for _, fails := range []int{1, 2} {
		t.Run(map[int]string{1: "once", 2: "twice"}[fails], func(t *testing.T) {
			h, p, _ := familyLink(t)
			fastRetry(t)
			seedLink(t, h, &markusLink)
			plan := h.plan("lock.unlock", "lock.front").Plan
			windowWait = 10 * time.Second
			more := failReads(h.f, 0, wsRead("config/auth/list"))
			var atTap int
			p.tapBy = func(_, ok, _ string) []phoneEvent {
				more(fails)
				atTap = reads(h, "config/auth/list")
				return []phoneEvent{tap(ok, "owner-id", "REMOTE")}
			}
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			if rechecks := reads(h, "config/auth/list") - atTap; p.asks() != 1 || rechecks != 2 {
				t.Fatalf("asks %d re-checks %d", p.asks(), rechecks)
			}
			if fails == 1 && (exit != 0 || env.Status != "verified" || calls(h) != 1) {
				t.Fatalf("once: exit %d %s %+v", exit, env.Status, env.Error)
			}
			if fails == 2 && (exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || !strings.Contains(env.Error.Message, "couldn't re-check") || calls(h) != 0) {
				t.Fatalf("twice: exit %d %+v", exit, env.Error)
			}
		})
	}
}

// A notification Home Assistant didn't take is never sent again.
func TestPhoneSendIsNeverRetried(t *testing.T) {
	h, _, _ := familyLink(t)
	fastRetry(t)
	seedLink(t, h, &markusLink)
	plan := h.plan("lock.unlock", "lock.front").Plan
	windowWait = 10 * time.Second
	var mu sync.Mutex
	var bodies []map[string]any
	phoneNotify = func(_ *relaySession, _ string, body map[string]any) error {
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, body)
		return errors.New("Home Assistant answered HTTP 500")
	}
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	mu.Lock()
	defer mu.Unlock()
	if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || countAsks(bodies) != 1 ||
		!strings.Contains(env.Error.Message, "the confirmation could not be sent to the phone") {
		t.Fatalf("exit %d %+v questions %d", exit, env.Error, countAsks(bodies))
	}
}

// A chat app's ask: the lookup and the probe run once more there too, the
// phone gets one question, and two failed tries say Home Assistant was slow.
func TestServePhoneCheckRunsOnceMore(t *testing.T) {
	setup := func(t *testing.T) (*serveHarness, *fakePhones, string) {
		s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
		p := usePhones(t, "mobile_app_a")
		fastRetry(t)
		p.setup.confirmer = userA
		return s, p, phonePlanFor(t, s, userA)
	}
	// discoveries fails the next n lookups on a read; it counts every lookup.
	discoveries := func(p *fakePhones, n int) func() int {
		var mu sync.Mutex
		seen := 0
		phoneDiscover = func(*relaySession, time.Time) (phoneSetup, phoneStatus) {
			mu.Lock()
			defer mu.Unlock()
			if seen++; seen <= n {
				return phoneSetup{why: whyRead}, phoneMissing
			}
			return p.setup, p.status
		}
		return func() int { mu.Lock(); defer mu.Unlock(); return seen }
	}
	for _, c := range []struct {
		name          string
		lookup, probe int
	}{{"lookup", 1, 0}, {"probe", 0, 1}} {
		t.Run(c.name+" fails once", func(t *testing.T) {
			s, p, id := setup(t)
			lookups, probes := discoveries(p, c.lookup), failProbes(c.probe)
			if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "waiting_for_phone" {
				t.Fatalf("apply: %v", env)
			}
			waitFor(t, "the question", func() bool { return p.sentCount() == 1 })
			ok, _ := p.buttons(t)
			p.events <- tap(ok, userA, "REMOTE")
			waitFor(t, "the apply after the tap", func() bool { return len(s.posts()) == 1 })
			if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "verified" || p.asks() != 1 || lookups() < 2+c.lookup || probes() != 1+c.probe {
				t.Fatalf("after the tap: %v asks %d lookups %d probes %d", env, p.asks(), lookups(), probes())
			}
		})
	}
	t.Run("lookup fails twice", func(t *testing.T) {
		s, p, id := setup(t)
		lookups := discoveries(p, 9)
		env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id})
		info, _ := env["error"].(map[string]any)
		next, _ := info["next"].(string)
		if envCode(env) != "CONFIRM_UNAVAILABLE" || p.sentCount() != 0 || lookups() != 2 || next == chatPhoneMissing ||
			!strings.Contains(next, "Home Assistant answered too slowly or a read failed, twice") || !strings.Contains(next, "Nothing on the phone needs fixing") {
			t.Fatalf("apply: %v sent %d lookups %d", env, p.sentCount(), lookups())
		}
	})
}
