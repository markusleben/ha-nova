package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// windowConfirmStubs: a window opens here; the plan's window answers
// confirm, and the waiting window waiting (nil: never shown).
func windowConfirmStubs(t *testing.T, confirm func(windowText) windowAnswer, waiting func(ctx context.Context, message string) windowAnswer) (confirmed *[]windowText, waited *[]string) {
	t.Helper()
	savedAvailable, savedRunner, savedWindow, savedOpen, savedRetry := windowAvailable, windowRunner, gateWindow, gateOpenHA, gateWindowRetry
	t.Cleanup(func() {
		windowAvailable, windowRunner, gateWindow, gateOpenHA, gateWindowRetry = savedAvailable, savedRunner, savedWindow, savedOpen, savedRetry
	})
	var shown []windowText
	var texts []string
	windowAvailable = func() bool { return true }
	windowRunner = func(_ context.Context, w windowText, _ time.Duration) windowAnswer {
		shown = append(shown, w)
		return confirm(w)
	}
	gateWindow = func(ctx context.Context, message, _, _ string, _ time.Duration) windowAnswer {
		texts = append(texts, message)
		if waiting == nil {
			t.Errorf("a waiting window: %q", message)
			return windowNoAnswer
		}
		return waiting(ctx, message)
	}
	gateOpenHA = func(string) error { return nil }
	return &shown, &texts
}

// The App offers this computer's window: it shows the plan's own text with
// Confirm, and Confirm approves through the App (op window); the change is
// sent once with that grant.
func TestGateWindowConfirmApprovesThroughTheApp(t *testing.T) {
	for _, phone := range []bool{false, true} {
		g := &fakeGate{release: make(chan struct{}), phones: true, channels: []string{"window"}}
		if phone {
			g.link = map[string]any{"service": "notify.mobile_app_phone"}
		}
		shown, _ := windowConfirmStubs(t, func(windowText) windowAnswer { return windowConfirmed }, nil)
		h := gatedHarness(t, newFakeHA(), g)
		plan := h.plan("lock.unlock", "lock.front").Plan
		stored, err := readPlanFile(filepath.Join(planDir(h.paths), plan.ID+".json"))
		if err != nil {
			t.Fatal(err)
		}
		exit, env := h.run(runApplyCommand, "--plan", plan.ID)
		_, ops, sends := g.snapshot()
		footer := map[bool]string{false: "window.footer_relay", true: "window.footer_relay_phone"}[phone]
		w := (*shown)
		if exit != 0 || len(sends) != 1 || sends[0].grant != "grant-ask1" || !strings.Contains(strings.Join(ops, ","), "window") || len(w) != 1 ||
			w[0].title+"\n\n"+w[0].body != stored.Prompt || w[0].footer != confirmT("en", footer) || w[0].button != confirmT("en", "button.confirm") {
			t.Fatalf("phone %v: exit %d %+v; ops %v sends %+v shown %+v", phone, exit, env.Error, ops, sends, w)
		}
	}
}

// Cancel in the plan's window cancels the ask; nothing is approved or sent.
func TestGateWindowConfirmCancel(t *testing.T) {
	g := &fakeGate{release: make(chan struct{}), phones: true, channels: []string{"window"}}
	windowConfirmStubs(t, func(windowText) windowAnswer { return windowCancelled }, nil)
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	_, env := h.run(runApplyCommand, "--plan", plan.ID)
	_, ops, sends := g.snapshot()
	if env.Error == nil || env.Error.Code != "CONFIRM_DECLINED" || len(sends) != 0 || strings.Contains(strings.Join(ops, ","), "window") || !strings.Contains(strings.Join(ops, ","), "cancel") {
		t.Fatalf("%+v; ops %v sends %+v", env.Error, ops, sends)
	}
}

// The App won't take Confirm (409: this computer may not confirm now; a 503
// that stays): the window says so and keeps waiting, and the phone's or
// the page's OK still counts. A 503 is tried again quietly first.
func TestGateWindowConfirmTheAppRefuses(t *testing.T) {
	unavailable := fakeRelayStatus{503, "GATE_UNAVAILABLE", "The Home Assistant owner could not be checked; nothing was approved"}
	for name, tc := range map[string]struct {
		answers []any
		retry   time.Duration
		windows int // op window calls
		waiting bool
	}{
		"409":            {[]any{fakeRelayStatus{409, "GATE_WINDOW_OFF", "no"}}, time.Second, 1, true},
		"503 stays":      {[]any{unavailable, unavailable, unavailable, unavailable}, 1500 * time.Millisecond, 3, true},
		"503 then works": {[]any{unavailable}, 3 * time.Second, 2, false},
	} {
		t.Run(name, func(t *testing.T) {
			g := &fakeGate{release: make(chan struct{}), phones: true, channels: []string{"window"}, windowOps: tc.answers}
			var waiting func(context.Context, string) windowAnswer
			if tc.waiting {
				waiting = func(ctx context.Context, _ string) windowAnswer {
					close(g.release) // then the phone approves
					<-ctx.Done()
					return windowNoAnswer
				}
			}
			_, texts := windowConfirmStubs(t, func(windowText) windowAnswer { return windowConfirmed }, waiting)
			gateWindowRetry = tc.retry
			h := gatedHarness(t, newFakeHA(), g)
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			_, ops, sends := g.snapshot()
			if exit != 0 || len(sends) != 1 || sends[0].grant != "grant-ask1" || strings.Count(strings.Join(ops, ","), "window") != tc.windows {
				t.Fatalf("exit %d %+v; ops %v sends %+v", exit, env.Error, ops, sends)
			}
			if tc.waiting && (len(*texts) != 1 || !strings.HasPrefix((*texts)[0], confirmT("en", "gate.window_cannot")+" "+confirmT("en", "gate.window", "approve", confirmT("en", "page.approve")))) {
				t.Fatalf("waiting window %q", *texts)
			}
		})
	}
}

// Confirm only where the App offers the window for this plan's ask, and a
// window opens here; a write held mid-apply has no plan text to show.
func TestGateWindowConfirmOffered(t *testing.T) {
	saved := windowAvailable
	t.Cleanup(func() { windowAvailable = saved })
	windowAvailable = func() bool { return true }
	s := &relaySession{paths: runtimePaths{ConfigFile: filepath.Join(t.TempDir(), "none.json")}}
	plan := &gateConfirm{lang: "en", text: "Unlock?\n\nFront door"}
	window := gateOpened{Needed: true, AskID: "ask1", Channels: []string{"window", "phone"}}
	if got := plan.offered(s, window); got == nil || !got.phone || got.text != plan.text {
		t.Fatalf("offered %+v", got)
	}
	for name, got := range map[string]*gateConfirm{
		"no window offered": plan.offered(s, gateOpened{Needed: true, AskID: "ask1", Channels: []string{"phone"}}),
		"held write":        (*gateConfirm)(nil).offered(s, window),
		"no text":           (&gateConfirm{lang: "en"}).offered(s, window),
	} {
		if got != nil {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	windowAvailable = func() bool { return false }
	if got := plan.offered(s, window); got != nil {
		t.Fatalf("no window here: %+v", got)
	}
}

// A plan too long for the window is never confirmed there: the waiting
// window shows its start, whole lines only, and where the rest is.
func TestGateWindowSummaryOfATooLongPlan(t *testing.T) {
	var lines []string
	for i := range 60 {
		lines = append(lines, "• light.room_"+strings.Repeat("x", i%7))
	}
	text := "Turn these off?\n\n" + strings.Join(lines, "\n")
	if _, fits := gateConfirmWindow(&gateConfirm{lang: "en", text: text}); fits {
		t.Fatal("a 62-line plan fits")
	}
	rest := confirmT("en", "gate.window_page")
	got := gateSummary("en", text, rest)
	if _, fits := windowFits("en", got); !fits || !strings.HasPrefix(got, "Turn these off?\n\n• light.room_\n") || !strings.HasSuffix(got, "\n"+confirmT("en", "gate.window_more")+"\n\n"+rest) {
		t.Fatalf("summary %q", got)
	}
	for _, line := range strings.Split(strings.TrimSuffix(strings.SplitN(got, confirmT("en", "gate.window_more"), 2)[0], "\n"), "\n")[2:] {
		if !strings.Contains(text, line) {
			t.Fatalf("cut line %q", line)
		}
	}
}
