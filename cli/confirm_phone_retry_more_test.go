package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// The second try where no ask has started yet (confirm_phone_retry.go): a
// plan's phone lookup, the window's fallback, and a listening lane that
// fails in its first moment.

// failLanes fails the next n listening lanes' subscriptions at once.
func failLanes(n int) {
	var mu sync.Mutex
	collect := phoneCollect
	phoneCollect = func(ctx context.Context, s *relaySession, span time.Duration) ([]phoneEvent, error) {
		if span >= time.Second {
			mu.Lock()
			fail := n > 0
			n--
			mu.Unlock()
			if fail {
				return nil, errors.New("UPSTREAM_WS_TIMEOUT")
			}
		}
		return collect(ctx, s, span)
	}
}

// A phone plan (no window) whose lookup fails once on a read is still made
// for the phone; failing twice, it is refused with the plain texts; the
// second try's reads end phonePlanRetryBudget after the pause.
func TestPhonePlanLookupRunsOnceMore(t *testing.T) {
	t.Run("fails once", func(t *testing.T) {
		h, _, _ := familyLink(t)
		fastRetry(t)
		seedLink(t, h, &markusLink)
		failReads(h.f, 1, wsRead("config/auth/list"))
		exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
		if env.Plan == nil || env.Plan.Channel != channelPhone || reads(h, "config/auth/list") != 2 {
			t.Fatalf("exit %d %+v %+v lookups %d", exit, env.Plan, env.Error, reads(h, "config/auth/list"))
		}
	})
	t.Run("fails twice", func(t *testing.T) {
		h, _, home := familyLink(t)
		fastRetry(t)
		seedLink(t, h, &markusLink)
		usersReadable(home, h.f, false)
		exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
		if exit != 4 || env.Plan != nil || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || reads(h, "config/auth/list") != 2 ||
			!strings.Contains(env.Error.Message, "answered too slowly or a read failed, twice") {
			t.Fatalf("exit %d %+v lookups %d", exit, env.Error, reads(h, "config/auth/list"))
		}
		saysHomeAssistantWasSlow(t, env.Error.Next)
	})
	t.Run("the second try is capped", func(t *testing.T) {
		h, _, _ := familyLink(t)
		fastRetry(t)
		seedLink(t, h, &markusLink)
		previous, cap := relaySessionOverallTimeout, phonePlanRetryBudget
		relaySessionOverallTimeout, phonePlanRetryBudget = 2*time.Second, 400*time.Millisecond
		t.Cleanup(func() { relaySessionOverallTimeout, phonePlanRetryBudget = previous, cap })
		h.f.mu.Lock()
		h.f.hang["config/auth/list"] = true
		h.f.mu.Unlock()
		start := time.Now()
		exit, env := h.run(runPlanCommand, "call", "lock.unlock", "lock.front")
		took := time.Since(start)
		if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || reads(h, "config/auth/list") != 2 || took < 2300*time.Millisecond || took > 3500*time.Millisecond {
			t.Fatalf("exit %d %+v lookups %d after %v", exit, env.Error, reads(h, "config/auth/list"), took)
		}
		saysHomeAssistantWasSlow(t, env.Error.Next)
	})
	t.Run("no second try past the plan's own budget", func(t *testing.T) {
		fastRetry(t)
		discover := phoneDiscover
		t.Cleanup(func() { phoneDiscover = discover })
		lookups := 0
		phoneDiscover = func(*relaySession, time.Time) (phoneSetup, phoneStatus) {
			lookups++
			return phoneSetup{why: whyRead}, phoneMissing
		}
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-phonePlanRetryBudget-time.Second))
		defer cancel()
		if channel := detectAfterWindow(&relaySession{ctx: ctx}, time.Time{}); channel != channelPhoneUnread || lookups != 1 {
			t.Fatalf("channel %s lookups %d", channel, lookups)
		}
	})
}

// A chat app's plan: the lookup runs once more there too.
func TestServePhonePlanLookupRunsOnceMore(t *testing.T) {
	for _, fails := range []int{1, 2} {
		t.Run(map[int]string{1: "once", 2: "twice"}[fails], func(t *testing.T) {
			s := newServe(t, newFakeHA(), `{"allowed_users":["`+userA+`"]}`)
			p := usePhones(t, "mobile_app_a")
			fastRetry(t)
			p.setup.confirmer = userA
			var mu sync.Mutex
			lookups := 0
			phoneDiscover = func(*relaySession, time.Time) (phoneSetup, phoneStatus) {
				mu.Lock()
				defer mu.Unlock()
				if lookups++; lookups <= fails {
					return phoneSetup{why: whyRead}, phoneMissing
				}
				return p.setup, p.status
			}
			env := s.tool(userA, "ha_plan", map[string]any{"kind": "call", "service": "lock.unlock", "entity_ids": []string{"lock.front"}})
			plan, _ := env["plan"].(map[string]any)
			info, _ := env["error"].(map[string]any)
			next, _ := info["next"].(string)
			mu.Lock()
			defer mu.Unlock()
			if fails == 1 && (plan["channel"] != channelPhone || lookups != 2) {
				t.Fatalf("once: %v lookups %d", env, lookups)
			}
			if fails == 2 {
				if envCode(env) != "CONFIRM_UNAVAILABLE" || plan != nil || lookups != 2 || next == chatPhoneMissing {
					t.Fatalf("twice: %v lookups %d", env, lookups)
				}
				saysHomeAssistantWasSlow(t, next)
			}
		})
	}
}

// A window that can't open after a lookup that failed twice: its fallback
// looks the phone up once more by the window's deadline, and says Home
// Assistant was slow where that fails too.
func TestWindowFallbackLooksThePhoneUpOnceMore(t *testing.T) {
	for _, found := range []bool{true, false} {
		t.Run(map[bool]string{true: "found on its second try", false: "fails throughout"}[found], func(t *testing.T) {
			h, p, home := familyLink(t)
			fastRetry(t)
			seedLink(t, h, &markusLink)
			useWindow(t, windowFailed)
			plan := h.plan("lock.unlock", "lock.front").Plan
			if plan.Channel != channelWindow {
				t.Fatalf("plan %+v", plan)
			}
			windowWait = 10 * time.Second
			before := reads(h, "config/auth/list")
			if found {
				failReads(h.f, 3, wsRead("config/auth/list")) // the apply's two, then the fallback's first
			} else {
				usersReadable(home, h.f, false)
			}
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			lookups := reads(h, "config/auth/list") - before
			if len(p.sent) != 0 || calls(h) != 0 || lookups != 4 {
				t.Fatalf("sent %d calls %d lookups %d", len(p.sent), calls(h), lookups)
			}
			if found && (exit != 3 || env.Plan == nil || env.Plan.Channel != channelPhone || env.Plan.Code != "") {
				t.Fatalf("found: exit %d %+v %+v", exit, env.Plan, env.Error)
			}
			if !found {
				if exit != 4 || env.Plan != nil || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" ||
					!strings.Contains(env.Error.Message, "the confirmation window couldn't open, and HA NOVA couldn't check the linked phone") {
					t.Fatalf("fails: exit %d %+v", exit, env.Error)
				}
				saysHomeAssistantWasSlow(t, env.Error.Next)
			}
		})
	}
}

// A listening lane that fails right after a good probe subscribes again
// before anything is sent: once, the phone gets one question; twice, none,
// and the agent hears Home Assistant was slow.
func TestPhoneListeningLaneThatFailsOnceAsksOnce(t *testing.T) {
	for _, fails := range []int{1, 2} {
		t.Run(map[int]string{1: "once", 2: "twice"}[fails], func(t *testing.T) {
			h, p, _ := familyLink(t)
			fastRetry(t)
			seedLink(t, h, &markusLink)
			plan := h.plan("lock.unlock", "lock.front").Plan
			windowWait, phoneSettle = 10*time.Second, 300*time.Millisecond
			failLanes(fails)
			p.tapBy = tapFrom("mobile_app_markuss_iphone", "owner-id", true)
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			if fails == 1 && (exit != 0 || env.Status != "verified" || calls(h) != 1 || p.asks() != 1) {
				t.Fatalf("once: exit %d %s %+v asks %d", exit, env.Status, env.Error, p.asks())
			}
			if fails == 2 {
				if exit != 4 || env.Error == nil || env.Error.Code != "CONFIRM_UNAVAILABLE" || len(p.sent) != 0 || calls(h) != 0 ||
					!strings.Contains(env.Error.Message, "answered too slowly or a read failed, twice") {
					t.Fatalf("twice: exit %d %+v sent %d", exit, env.Error, len(p.sent))
				}
				saysHomeAssistantWasSlow(t, env.Error.Next)
			}
		})
	}
}

// The listening probe before a window opens beside the linked phone ends
// while windowMinWait is still left: a probe that hangs leaves the window
// asking alone, with that time to answer.
func TestWindowKeepsItsTimeWhileThePhoneProbeHangs(t *testing.T) {
	h, p, _ := familyLink(t)
	fastRetry(t)
	seedLink(t, h, &markusLink)
	w := useWindow(t, windowConfirmed)
	plan := h.plan("lock.unlock", "lock.front").Plan
	windowWait, windowMinWait = 5*time.Second, 3*time.Second
	collect := phoneCollect
	phoneCollect = func(ctx context.Context, s *relaySession, span time.Duration) ([]phoneEvent, error) {
		if span < time.Second {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return collect(ctx, s, span)
	}
	var left time.Duration
	runner := windowRunner
	windowRunner = func(ctx context.Context, message, lang string, wait time.Duration) windowAnswer {
		left = wait
		return runner(ctx, message, lang, wait)
	}
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	if exit != 0 || calls(h) != 1 || p.asks() != 0 || left < windowMinWait-500*time.Millisecond ||
		len(w.messages) != 1 || strings.Contains(w.messages[0], windowBoth("en")) || !strings.HasSuffix(w.messages[0], windowAsked("en")) {
		t.Fatalf("exit %d %+v asks %d window left %v messages %q", exit, env.Error, p.asks(), left, w.messages)
	}
}
