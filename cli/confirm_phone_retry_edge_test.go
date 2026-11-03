package main

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// The edges of the second try (confirm_phone_retry.go): time left to tap,
// a chat app's wait before the send, the link flows, the control job, a
// Relay refusal, and a lookup with no time for a second try.

// probesThat answers the listening probes with probe(n), n counting from 1;
// lanes still listen. It counts every probe.
func probesThat(probe func(ctx context.Context, n int) error) (probes func() int) {
	var mu sync.Mutex
	seen, collect := 0, phoneCollect
	phoneCollect = func(ctx context.Context, s *relaySession, span time.Duration) ([]phoneEvent, error) {
		if span >= time.Second {
			return collect(ctx, s, span)
		}
		mu.Lock()
		seen++
		n := seen
		mu.Unlock()
		if err := probe(ctx, n); err != nil {
			return nil, err
		}
		return collect(ctx, s, span)
	}
	return func() int { mu.Lock(); defer mu.Unlock(); return seen }
}

var errSlowRead = errors.New("UPSTREAM_WS_TIMEOUT")

// The checks before the send leave the person windowMinWait to tap: the
// second probe ends by then, and checks that leave less send nothing.
func TestPhoneAskKeepsTimeToTap(t *testing.T) {
	ask := func(t *testing.T, probe func(ctx context.Context, n int) error) (int, verbTestEnvelope, *fakePhones, time.Duration) {
		h, p, _ := familyLink(t)
		fastRetry(t)
		seedLink(t, h, &markusLink)
		plan := h.plan("lock.unlock", "lock.front").Plan
		windowWait, windowMinWait = 5*time.Second, 3*time.Second
		probesThat(probe)
		start := time.Now()
		exit, env := h.run(runApplyCommand, "--plan", plan.ID)
		return exit, env, p, time.Since(start)
	}
	t.Run("the second probe ends while there is time to tap", func(t *testing.T) {
		exit, env, p, took := ask(t, func(ctx context.Context, n int) error {
			if n > 1 {
				<-ctx.Done()
			}
			return errSlowRead
		})
		if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(p.sent) != 0 || took > 3*time.Second {
			t.Fatalf("exit %d %+v sent %d after %v", exit, env.Error, len(p.sent), took)
		}
		saysHomeAssistantWasSlow(t, env.Error.Next)
	})
	t.Run("checks that leave too little time send nothing", func(t *testing.T) {
		exit, env, p, _ := ask(t, func(context.Context, int) error {
			time.Sleep(2500 * time.Millisecond)
			return nil
		})
		if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(p.sent) != 0 {
			t.Fatalf("exit %d %+v sent %d", exit, env.Error, len(p.sent))
		}
		saysHomeAssistantWasSlow(t, env.Error.Next)
	})
	t.Run("the lanes' first moment", func(t *testing.T) {
		saved := phoneSettle
		phoneSettle = time.Second
		t.Cleanup(func() { phoneSettle = saved })
		ended, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		withdrawn, withdraw := context.WithCancel(context.Background())
		withdraw()
		refused := make(chan error, 1)
		refused <- &relayCallError{endpoint: "ws", status: 502, code: "UPSTREAM_WS_COMMAND_ERROR"}
		for want, ctx := range map[phoneAnswer]context.Context{phoneUnread: ended, phoneNoAnswer: withdrawn} {
			if answer, ok := phoneSettled(ctx, make(chan error)); ok || answer != want {
				t.Fatalf("got %d %v, want %d", answer, ok, want)
			}
		}
		if answer, ok := phoneSettled(context.Background(), refused); ok || answer != phoneFailed {
			t.Fatalf("refused lane: %d %v", answer, ok)
		}
	})
}

// A chat app's call that comes back before the question went out says HA
// NOVA is still checking the phone, never that the user should watch it.
func TestServeSaysItIsStillCheckingBeforeTheSend(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
	p := usePhones(t, "mobile_app_a")
	p.setup.confirmer = userA
	id := phonePlanFor(t, s, userA)
	s.b.wait = 300 * time.Millisecond
	gate := make(chan struct{})
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release) // a failed check never leaves the job holding the verb lock
	phoneDiscover = func(*relaySession, time.Time) (phoneSetup, phoneStatus) { <-gate; return p.setup, p.status }
	env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id})
	next, _ := env["next_step"].(string)
	if env["status"] != "waiting_for_phone" || env["say"] != sayNotRun("en") || p.sentCount() != 0 ||
		!strings.Contains(next, "still checking the user's linked phone in Home Assistant, and nothing was sent to it yet") || !strings.Contains(next, "Don't tell the user to watch their phone yet") {
		t.Fatalf("before the send: %v", env)
	}
	release()
	waitFor(t, "the question", func() bool { return p.sentCount() == 1 })
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "waiting_for_phone" || env["say"] == sayNotRun("en") {
		t.Fatalf("after the send: %v", env)
	}
	ok, _ := p.buttons(t)
	p.events <- tap(ok, userA, "REMOTE")
	waitFor(t, "the apply after the tap", func() bool { return len(s.posts()) == 1 })
	if env := s.tool(userA, "ha_apply", map[string]any{"plan_id": id}); env["status"] != "verified" {
		t.Fatalf("after the tap: %v", env)
	}
}

// A refusal before the send that waits for the verb lock is no answer
// from the phone: the call never says the change is being applied.
func TestServeRefusalWaitingForTheLockIsNotApplying(t *testing.T) {
	s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
	p := usePhones(t, "mobile_app_a")
	fastRetry(t)
	p.setup.confirmer = userA
	id := phonePlanFor(t, s, userA)
	s.b.wait = 500 * time.Millisecond
	gate, asked := make(chan struct{}), make(chan struct{})
	var once sync.Once
	probesThat(func(context.Context, int) error {
		once.Do(func() { close(asked) })
		<-gate
		return errSlowRead
	})
	result := make(chan map[string]any, 1)
	go func() { result <- s.tool(userA, "ha_apply", map[string]any{"plan_id": id}) }()
	<-asked // the checks run outside the verb lock
	mcpVerbLock.Lock()
	close(gate)
	env := <-result
	mcpVerbLock.Unlock()
	if env["status"] != "waiting_for_phone" || env["say"] != sayNotRun("en") {
		t.Fatalf("while the refusal waits for the lock: %v", env)
	}
	waitFor(t, "the refusal", func() bool {
		env = s.tool(userA, "ha_apply", map[string]any{"plan_id": id})
		return env["status"] != "waiting_for_phone"
	})
	info, _ := env["error"].(map[string]any)
	next, _ := info["next"].(string)
	if envCode(env) != "CONFIRM_UNAVAILABLE" || p.sentCount() != 0 {
		t.Fatalf("refusal: %v", env)
	}
	saysHomeAssistantWasSlow(t, next)
}

// Link, Move and Stop whose checks fail on a read: nothing changes, and the
// terminal says Home Assistant was slow, never that the phone needs fixing.
func TestPhoneLinkFlowsSayHomeAssistantWasSlow(t *testing.T) {
	const slow = "nothing was sent: HA NOVA couldn't check the phone in Home Assistant just now. Nothing on the phone needs fixing. Run this again in a moment."
	anna := phoneLink{Service: "mobile_app_annas_pixel", DeviceID: "dev-anna", UserID: "anna-id", Name: "Anna’s Pixel"}
	for _, c := range []struct {
		name, line string
		seed       bool
		run        func(*testing.T, *writeHarness) (int, string)
	}{
		{"link", "Nothing linked and " + slow, false, func(t *testing.T, h *writeHarness) (int, string) {
			return runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"})
		}},
		{"move", "Nothing changed and " + slow, true, func(t *testing.T, h *writeHarness) (int, string) {
			return runLink(t, h, "", phoneLinkRun{pick: "Markus’s iPhone"})
		}},
		{"stop", "Nothing changed and " + slow, true, runUnlink},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, p, _ := familyLink(t)
			fastRetry(t)
			if c.seed {
				seedLink(t, h, &anna)
			}
			probes := failProbes(9)
			exit, out := c.run(t, h)
			if l := linked(t, h); exit != 1 || len(p.sent) != 0 || probes() != 2 || !strings.Contains(out, c.line) || strings.Contains(out, "remove it in Home Assistant") ||
				c.seed != (l != nil && l.DeviceID == "dev-anna") {
				t.Fatalf("exit %d sent %d probes %d out %q link %+v", exit, len(p.sent), probes(), out, l)
			}
		})
	}
	if got := pagePhoneProblem(phoneLinkUnread, "P", "A", "Allow"); !reflect.DeepEqual(got, setupPageText("setup.page.control.phone_problem", "phone", "P")) {
		t.Fatalf("setup page: %+v", got)
	}
}

// The NOVA page's Move whose check fails on a read: the job fails, and
// nothing is sent.
func TestServeControlSlowPhoneCheckSendsNothing(t *testing.T) {
	c := newControl(t, "")
	fastRetry(t)
	c.seed(ownerHex, markusIPhone)
	probes := failProbes(9)
	code, body := c.post(map[string]any{"user": ownerHex, "action": "move", "phone": map[string]any{"device_id": devIpad}})
	if code != http.StatusAccepted {
		t.Fatalf("move: %d %v", code, body)
	}
	if status := c.finish(body["job"]); status != "failed" || c.phones.sentCount() != 0 || probes() != 2 || *c.links()[ownerHex] != *markusIPhone {
		t.Fatalf("status %s sent %d probes %d links %v", status, c.phones.sentCount(), probes(), c.links())
	}
}

// Only a slow read runs once more: a probe or lane the Relay refuses
// otherwise stays a failed send, with its old text.
func TestPhoneCheckTheRelayRefusesIsNotRetried(t *testing.T) {
	refused := &relayCallError{endpoint: "ws", status: 502, code: "UPSTREAM_WS_COMMAND_ERROR", message: "Home Assistant refused the subscription"}
	for _, lane := range []bool{false, true} {
		t.Run(map[bool]string{false: "probe", true: "lane"}[lane], func(t *testing.T) {
			h, p, _ := familyLink(t)
			fastRetry(t)
			seedLink(t, h, &markusLink)
			plan := h.plan("lock.unlock", "lock.front").Plan
			windowWait = 10 * time.Second
			var mu sync.Mutex
			refusals, collect := 0, phoneCollect
			phoneCollect = func(ctx context.Context, s *relaySession, span time.Duration) ([]phoneEvent, error) {
				if (span >= time.Second) != lane {
					return collect(ctx, s, span)
				}
				mu.Lock()
				refusals++
				mu.Unlock()
				return nil, refused
			}
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			mu.Lock()
			n := refusals
			mu.Unlock()
			if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(p.sent) != 0 || (!lane && n != 1) ||
				!strings.Contains(env.Error.Message, "the confirmation could not be sent to the phone") || !strings.Contains(env.Error.Next, "ha-nova setup confirm-phone list") {
				t.Fatalf("exit %d %+v sent %d refusals %d", exit, env.Error, len(p.sent), n)
			}
		})
	}
}

// A lookup that failed on a read with no time left for a second try says
// Home Assistant was slow, never that the plan expired.
func TestPhoneLookupWithNoTimeForASecondTrySaysHomeAssistantWasSlow(t *testing.T) {
	for _, window := range []bool{false, true} {
		t.Run(map[bool]string{false: "phone", true: "window and phone"}[window], func(t *testing.T) {
			h, p, _ := familyLink(t)
			fastRetry(t)
			seedLink(t, h, &markusLink)
			if window {
				useWindow(t) // no answers: a window that opens fails the test
			}
			plan := h.plan("lock.unlock", "lock.front").Plan
			previous := relaySessionOverallTimeout
			relaySessionOverallTimeout = 2 * time.Second
			t.Cleanup(func() { relaySessionOverallTimeout = previous })
			windowWait = 3 * time.Second // windowMinWait 1 s, a probe 1 s: a second try ends 1 s in, before the first ends
			h.f.mu.Lock()
			h.f.hang["config/auth/list"] = true
			h.f.mu.Unlock()
			before := reads(h, "config/auth/list")
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			if lookups := reads(h, "config/auth/list") - before; exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(p.sent) != 0 || lookups != 1 {
				t.Fatalf("exit %d %+v sent %d lookups %d", exit, env.Error, len(p.sent), lookups)
			}
			saysHomeAssistantWasSlow(t, env.Error.Next)
		})
	}
}
