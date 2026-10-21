package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The human confirms a typed plan in a window on this computer
// (docs/work/2026-10-17-human-confirmation-channels-spec.md); a fake window
// stands in for the OS one.

type fakeWindow struct {
	answers  []windowAnswer
	messages []string
	before   func() // runs while the window is open
}

func useWindow(t *testing.T, answers ...windowAnswer) *fakeWindow {
	t.Helper()
	w := &fakeWindow{answers: answers}
	savedAvailable, savedRunner := windowAvailable, windowRunner
	windowAvailable = func() bool { return true }
	windowRunner = func(_ context.Context, message string, wait time.Duration) windowAnswer {
		if wait <= 0 || wait > cliWindowWait {
			t.Errorf("window wait %v", wait)
		}
		if len(w.answers) == 0 { // runs off the test goroutine: Errorf, not Fatalf
			t.Errorf("an unexpected window: %q", message)
			return windowNoAnswer
		}
		w.messages = append(w.messages, message)
		if w.before != nil {
			w.before()
		}
		answer := w.answers[0]
		w.answers = w.answers[1:]
		return answer
	}
	t.Cleanup(func() { windowAvailable, windowRunner = savedAvailable, savedRunner })
	return w
}

func TestWindowPlanConfirmsInTheWindow(t *testing.T) {
	resetServerProfileSelection(t) // no selection: the next step adds no server note
	h := newWriteHarness(t, newFakeHA())
	w := useWindow(t, windowCancelled, windowNoAnswer, windowClosed, windowConfirmed)
	env := h.plan("lock.unlock", "lock.front")
	plan := env.Plan
	if plan.Channel != channelWindow || plan.Code != "" || !strings.HasSuffix(env.Say, sayWindowAsk) ||
		strings.Contains(env.Say, "one-time code") || strings.Contains(env.Say, sayChatNote) ||
		!strings.Contains(env.NextStep, "apply --plan "+plan.ID+" without --confirm") || !strings.Contains(env.NextStep, "Never ask for a code") ||
		strings.Contains(env.NextStep, "HA_NOVA_SERVER") {
		t.Fatalf("window plan: %+v say %q", plan, env.Say)
	}
	code := storedCode(t, h, plan.ID)
	// Cancel and no answer send nothing, use no attempt, and keep the plan;
	// a code from argv never stands in for the window.
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", code); exit != 4 || env.Error.Code != "CONFIRM_DECLINED" {
		t.Fatalf("cancel: exit %d %+v", exit, env.Error)
	}
	// A timeout gives a plain next step, never "turn the windows off" (0.27 recheck lane).
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "CONFIRM_CANCELLED" || !strings.Contains(env.Error.Next, plan.ID) ||
		!strings.Contains(env.Error.Next, "the window closed without an answer, nothing changed, and they can ask again") || strings.Contains(env.Error.Next, "confirm-window off") {
		t.Fatalf("no answer: exit %d %+v", exit, env.Error)
	}
	// Closed from outside (lane D9): said as closed, not as a timeout.
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "CONFIRM_CANCELLED" ||
		env.Error.Message != "the confirmation window was closed; nothing was sent" {
		t.Fatalf("closed: exit %d %+v", exit, env.Error)
	}
	if len(h.posts()) != 0 {
		t.Fatalf("sent %d requests without a confirmation", len(h.posts()))
	}
	exit, applied := h.run(runApplyCommand, "--plan", plan.ID)
	if exit != 0 || applied.Status != "verified" || len(h.posts()) != 1 || applied.Plan.Code != "" || applied.NextStep != "" || applied.Say != "Done: 1 device is unlocked now. I checked it." {
		t.Fatalf("confirm: exit %d %+v posts %d", exit, applied, len(h.posts()))
	}
	if !strings.HasPrefix(w.messages[0], "I'm about to unlock “lock.front”") || !strings.Contains(w.messages[0], "so I need you to confirm it. Nothing has run yet.") ||
		!strings.HasSuffix(w.messages[0], windowAsked) || strings.Contains(w.messages[0], code) {
		t.Fatalf("window text: %q", w.messages[0])
	}
}

// The model can't skip the window: a code from argv without it, an edited
// prompt, or a change while the window is open never sends.
func TestWindowCannotBeSkipped(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	w := useWindow(t, windowConfirmed, windowConfirmed)
	plan := h.plan("lock.unlock", "lock.front").Plan
	code := storedCode(t, h, plan.ID)
	if env, exit := applyPlan(h.paths, verbOptions{}, plan.ID, code, false); exit != 4 || env.Error.Code != "CONFIRM_IN_WINDOW" {
		t.Fatalf("apply with the right code but no window: exit %d %+v", exit, env.Error)
	}
	h.tamper(plan.ID, func(p map[string]any) { p["prompt"] = "Turn on the hallway light." })
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "PLAN_INVALID" || len(h.posts()) != 0 {
		t.Fatalf("edited prompt: exit %d %+v", exit, env.Error)
	}
	if len(w.messages) != 0 {
		t.Fatalf("an edited plan opened a window: %q", w.messages)
	}
	plan = h.plan("lock.unlock", "lock.front").Plan
	w.before = func() { h.f.states["lock.front"]["state"] = "jammed" } // drift while the window is open (unlocked: no_change)
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 2 || env.Status != "drift" || len(h.posts()) != 0 {
		t.Fatalf("drift during the window: exit %d %+v", exit, env)
	}
	if !strings.Contains(w.messages[0], "lock.front") {
		t.Fatalf("window text: %q", w.messages[0])
	}
	if _, err := os.Stat(filepath.Join(planDir(h.paths), plan.ID+".json")); !os.IsNotExist(err) {
		t.Fatalf("the drifted plan is still there: %v", err)
	}
}

// Too little time before expiry for a window: refused, no window.
func TestWindowRefusesNearExpiry(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	useWindow(t) // any window opening fails the test
	plan := h.plan("lock.unlock", "lock.front").Plan
	h.tamper(plan.ID, func(p map[string]any) { p["expires_at"] = verbNow().UTC().Add(10 * time.Second).Format(time.RFC3339) })
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "PLAN_EXPIRED" || len(h.posts()) != 0 {
		t.Fatalf("near expiry: exit %d %+v", exit, env.Error)
	}
}

// A window that can't open falls back to a chat plan, said in `say`; a
// window that opened and ended (killed, closed) never does.
func TestWindowOpenFailureFallsBackToChat(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	useWindow(t, windowFailed)
	old := h.plan("lock.unlock", "lock.front").Plan
	exit, env := h.run(runApplyCommand, "--plan", old.ID)
	if exit != 3 || env.Status != "needs_confirmation" || env.Plan == nil || env.Plan.Code == "" || env.Plan.Channel != channelChat ||
		!strings.HasPrefix(env.Say, "The confirmation window couldn't open on this computer. ") || !strings.HasSuffix(env.Say, sayChatNote) ||
		!strings.Contains(env.Say, "I'm about to unlock “lock.front”") || !strings.Contains(env.Say, "Type "+env.Plan.Code+" to unlock “lock.front” — a plain yes can't confirm this.") || !strings.Contains(env.NextStep, "apply --confirm <the user's text> --plan "+env.Plan.ID) || len(h.posts()) != 0 {
		t.Fatalf("fallback: exit %d %+v", exit, env)
	}
	if exit, env := h.run(runApplyCommand, "--plan", old.ID); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("the window plan survived its fallback: exit %d %+v", exit, env.Error)
	}
	if exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code); exit != 0 || applied.Status != "verified" {
		t.Fatalf("the fallback plan's code: exit %d %+v", exit, applied)
	}
}

// The fallback keeps a user's no-copy choice (a failed snapshot, then
// --no-snapshot): the chat plan still offers it.
func TestWindowFallbackKeepsNoSnapshot(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	useWindow(t, windowFailed)
	old := h.plan("lock.unlock", "lock.front").Plan
	h.tamper(old.ID, func(p map[string]any) { p["snapshot_failed"] = true }) // not in the hash
	exit, env := h.run(runApplyCommand, "--plan", old.ID, "--no-snapshot")
	stored, err := readPlanFile(filepath.Join(planDir(h.paths), env.Plan.ID+".json"))
	if exit != 3 || err != nil || !stored.SnapshotFailed || !strings.Contains(env.NextStep, "--no-snapshot") {
		t.Fatalf("fallback after a failed snapshot: exit %d %+v (%v)", exit, env, err)
	}
}

// The window comes before the form (a model with a shell can answer its own
// form, not an OS window); the form is the channel where no window opens.
// Leaving forms out, or --no-forms, never yields a code where a window can
// open.
func TestWindowAndMCPChannels(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	w := useWindow(t, windowFailed)
	unlock := map[string]any{"service": "lock.unlock", "entity_ids": []string{"lock.front"}}
	c := startMCP(t, h.paths)
	formsInit(c)
	plan := envelopeOf(c.tool("ha_call", unlock))["plan"].(map[string]any)
	if plan["channel"] != channelWindow || plan["code"] != nil {
		t.Fatalf("forms client, window available: %v", plan)
	}
	// The window can't open: the fallback is the client's form, not chat.
	fallback := c.tool("ha_apply", map[string]any{"plan_id": plan["id"]})
	env := envelopeOf(fallback)
	if next, _ := env["next_step"].(string); env["plan"].(map[string]any)["channel"] != channelForm || !strings.Contains(next, "confirmation form") || len(w.messages) != 1 {
		t.Fatalf("window failed in a forms session: %v", env)
	}
	noCode(t, "form fallback", fallback, storedCode(t, h, env["plan"].(map[string]any)["id"].(string)))

	windowAvailable = func() bool { return false }
	if plan := envelopeOf(c.tool("ha_call", unlock))["plan"].(map[string]any); plan["channel"] != channelForm {
		t.Fatalf("forms client, no window: %v", plan)
	}
	windowAvailable = func() bool { return true }

	mcpNoForms = true
	t.Cleanup(func() { mcpNoForms = false })
	plain := startMCP(t, h.paths)
	formsInit(plain)
	result := plain.tool("ha_call", unlock)
	env = envelopeOf(result)
	plan = env["plan"].(map[string]any)
	next, _ := env["next_step"].(string)
	if plan["channel"] != channelWindow || plan["code"] != nil || !strings.Contains(next, "confirmation window") || !strings.Contains(next, "no confirm_code") {
		t.Fatalf("--no-forms: %v", env)
	}
	noCode(t, "--no-forms window plan", result, storedCode(t, h, plan["id"].(string)))
}

// A window plan applied where the client shows forms still asks in its
// window: a form (which a model with a shell can answer itself) never
// confirms it.
func TestWindowPlanNeverTakesAForm(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	w := useWindow(t, windowConfirmed)
	plan := h.plan("lock.unlock", "lock.front").Plan
	c := startMCP(t, h.paths)
	formsInit(c)
	if applied := c.tool("ha_apply", map[string]any{"plan_id": plan.ID}); envelopeOf(applied)["status"] != "verified" || len(w.messages) != 1 || len(h.posts()) != 1 {
		t.Fatalf("window plan in a forms session: %v (windows %d)", applied, len(w.messages))
	}
}

// Turning windows off is confirmed in the window; a cancelled or failed
// window changes nothing; on needs no window.
func TestConfirmWindowSetting(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	useWindow(t, windowCancelled, windowFailed, windowConfirmed)
	for i, want := range []int{1, 0, 0} {
		if exit := runConfirmWindowSetting(h.paths, []string{"off"}); exit != want || windowOff(h.paths) != (i == 2) {
			t.Fatalf("off #%d: exit %d, off %v", i, exit, windowOff(h.paths))
		}
	}
	if plan := h.plan("lock.unlock", "lock.front").Plan; plan.Channel != channelChat || plan.Code == "" {
		t.Fatalf("off: %+v", plan)
	}
	if exit := runConfirmWindowSetting(h.paths, []string{"on"}); exit != 0 || windowOff(h.paths) {
		t.Fatalf("on: exit %d", exit)
	}
	if exit := runConfirmWindowSetting(h.paths, []string{"maybe"}); exit != 1 {
		t.Fatalf("bad value: exit %d", exit)
	}
}

// 0.27 recheck lane: with a server selected, the next step says to run apply
// bare, so Claude Code's `ha-nova apply --plan *` allow rule still matches.
func TestWindowPlanWithASelectedServerSaysApplyBare(t *testing.T) {
	resetServerProfileSelection(t)
	h := newWriteHarness(t, newFakeHA())
	useWindow(t)
	t.Setenv(serverSelectionEnvVar, defaultServerProfileName)
	env := h.plan("lock.unlock", "lock.front")
	if env.Plan == nil || !strings.Contains(env.NextStep, "no HA_NOVA_SERVER= or --server (apply follows its plan's server)") {
		t.Fatalf("next step must say apply runs bare: %q (%+v)", env.NextStep, env.Error)
	}
}
