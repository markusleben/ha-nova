package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fastVerify shrinks the verification windows so tests do not wait seconds.
func fastVerify(t *testing.T) {
	t.Helper()
	interval, fast, slow, settle := verifyInterval, verifyWindowFast, verifyWindowSlow, reauthSettle
	verifyInterval, verifyWindowFast, verifyWindowSlow, reauthSettle = 10*time.Millisecond, 200*time.Millisecond, 200*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() {
		verifyInterval, verifyWindowFast, verifyWindowSlow, reauthSettle = interval, fast, slow, settle
	})
}

// applies sets the target state a service promises, like Home Assistant.
func applies(f *fakeHA, path string, body map[string]any) any {
	state := map[string]string{"turn_off": "off", "turn_on": "on", "unlock": "unlocked", "lock": "locked"}[path[strings.LastIndex(path, "/")+1:]]
	ids, _ := body["entity_id"].([]any)
	for _, id := range ids {
		if st := f.states[fmt.Sprint(id)]; st != nil && state != "" {
			st["state"] = state
		}
	}
	return coreReply(200, []any{})
}

type writeHarness struct {
	t     *testing.T
	f     *fakeHA
	paths runtimePaths
	log   *fakeRelayLog
}

func newWriteHarness(t *testing.T, f *fakeHA) *writeHarness {
	fastVerify(t)
	if f.service == nil {
		f.service = applies
	}
	paths, log := setupFakeRelay(t, f.reply, true)
	return &writeHarness{t: t, f: f, paths: paths, log: log}
}

func (h *writeHarness) run(command func(runtimePaths, []string) int, args ...string) (int, verbTestEnvelope) {
	h.t.Helper()
	return runVerbForTest(h.t, func() int { return command(h.paths, args) })
}

func (h *writeHarness) plan(args ...string) verbTestEnvelope {
	h.t.Helper()
	_, env := h.run(runPlanCommand, append([]string{"call"}, args...)...)
	if env.Plan == nil {
		h.t.Fatalf("no plan: %+v", env)
	}
	return env
}

// posts returns every service POST the fake Relay received.
func (h *writeHarness) posts() []fakeRelayCall {
	var out []fakeRelayCall
	for _, c := range h.log.all() {
		if c.payload["method"] == "POST" {
			out = append(out, c)
		}
	}
	return out
}

func (h *writeHarness) planFiles() []string {
	files, _ := filepath.Glob(filepath.Join(planDir(h.paths), "p_*"))
	return files
}

func TestCallOrdinaryExecutesAndVerifiesInOneInvocation(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	exit, env := h.run(runCallCommand, "light.turn_off", "light.kitchen")
	if exit != 0 || env.Status != "verified" || env.Tier != "ordinary" || len(env.Verification) != 1 || env.Verification[0].Outcome != "verified" {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	var sequence []string
	for _, c := range h.log.all() {
		sequence = append(sequence, fmt.Sprint(c.payload["method"], " ", c.payload["path"], c.payload["type"]))
	}
	tail := strings.Join(sequence[len(sequence)-3:], " | ")
	if tail != "<nil> <nil>config_entries/flow/progress | POST /api/services/light/turn_off<nil> | GET /api/states/light.kitchen<nil>" {
		t.Fatalf("request tail = %s\nfull: %v", tail, sequence)
	}
	posts := h.posts()
	body, _ := json.Marshal(posts[0].payload["body"])
	if len(posts) != 1 || string(body) != `{"entity_id":["light.kitchen"]}` {
		t.Fatalf("posts %+v", posts)
	}
	if files := h.planFiles(); len(files) != 0 {
		t.Fatalf("the fast path stored a plan: %v", files)
	}
}

func TestCallNeedingConfirmationStoresAndSendsNothing(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	exit, env := h.run(runCallCommand, "lock.unlock", "lock.front")
	if exit != 3 || env.Plan == nil || env.Plan.Code == "" || len(h.posts()) != 0 || len(h.planFiles()) != 1 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
}

func TestRefusedCallsNeverSend(t *testing.T) {
	f := newFakeHA()
	f.ent("lock.keypad", "zwave_js", "locked", map[string]any{"code_format": "^\\d{4}$"})
	h := newWriteHarness(t, f)
	for _, args := range [][]string{{"mqtt.publish", "--set", "topic=x"}, {"lock.unlock", "lock.keypad"}} {
		if exit, env := h.run(runCallCommand, args...); exit != 4 || env.Tier != "refused" {
			t.Fatalf("%v: exit %d env %+v", args, exit, env)
		}
	}
	if len(h.posts()) != 0 {
		t.Fatalf("refused call sent %+v", h.posts())
	}
}

// I2: a typed plan never runs without its exact code; three wrong codes
// invalidate it.
func TestApplyTypedPlanRequiresTheExactCode(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	plan := h.plan("lock.unlock", "lock.front").Plan
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 4 || env.Error.Code != "CODE_REQUIRED" {
		t.Fatalf("no code: exit %d env %+v", exit, env)
	}
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", "yes"); exit != 4 || env.Error.Code != "CODE_MISMATCH" {
		t.Fatalf("wrong code: exit %d env %+v", exit, env)
	}
	exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", "  "+plan.Code+"\n")
	if exit != 0 || env.Status != "verified" || len(h.posts()) != 1 || h.f.states["lock.front"]["state"] != "unlocked" {
		t.Fatalf("right code: exit %d env %+v posts %d", exit, env, len(h.posts()))
	}

	plan = h.plan("lock.lock", "lock.front", "--set", "x=1").Plan // ordinary: no code needed
	if exit, _ := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 {
		t.Fatalf("ordinary plan apply exit %d", exit)
	}
	h.f.states["lock.front"]["state"] = "locked"
	plan = h.plan("lock.unlock", "lock.front").Plan
	for i := 1; i <= 3; i++ {
		exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", "confirm:nope")
		want := map[bool]string{true: "PLAN_INVALIDATED", false: "CODE_MISMATCH"}[i == 3]
		if exit != 4 || env.Error.Code != want {
			t.Fatalf("attempt %d: exit %d env %+v", i, exit, env)
		}
	}
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("invalidated plan still applies: exit %d env %+v", exit, env)
	}
	if len(h.posts()) != 2 {
		t.Fatalf("%d posts, want 2", len(h.posts()))
	}
}

// I3: apply recomputes; any drift returns a new plan and sends nothing.
func TestApplyDriftSendsNothingAndReplacesThePlan(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	old := h.plan("lock.unlock", "lock.front").Plan
	h.f.states["lock.front"]["state"] = "jammed" // the basis changed
	exit, env := h.run(runApplyCommand, "--plan", old.ID, "--confirm", old.Code)
	if exit != 2 || env.Status != "drift" || env.Plan == nil || env.Plan.ID == old.ID || env.Plan.Code == old.Code || len(h.posts()) != 0 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
	}
	files := h.planFiles()
	if len(files) != 1 || !strings.Contains(files[0], env.Plan.ID) {
		t.Fatalf("plan files %v, want only the new plan", files)
	}
}

// I6 + preview: apply without a code executes exactly the frozen leaves.
func TestApplyPreviewPlanSendsTheFrozenIDs(t *testing.T) {
	f := newFakeHA()
	f.ent("light.hall", "hue", "on", nil)
	f.related["area:kitchen"] = []string{"light.kitchen", "light.hall", "switch.kettle"}
	h := newWriteHarness(t, f)
	planned := h.plan("light.turn_off", "--area", "kitchen", "--set", "transition=2")
	exit, env := h.run(runApplyCommand, "--plan", planned.Plan.ID)
	posts := h.posts()
	if exit != 0 || env.Status != "verified" || len(posts) != 1 {
		t.Fatalf("exit %d env %+v posts %d", exit, env, len(posts))
	}
	body, _ := json.Marshal(posts[0].payload["body"])
	frozen, _ := json.Marshal(planned.Action.EntityIDs)
	if string(body) != `{"entity_id":`+string(frozen)+`,"transition":2}` || string(frozen) != `["light.hall","light.kitchen"]` {
		t.Fatalf("payload %s, frozen %s", body, frozen)
	}
}

// I7: one POST even when the Relay times out or fails upstream; exit 7.
func TestCallOutcomeUnknownNeverRetries(t *testing.T) {
	previous := relayWriteTimeout
	relayWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { relayWriteTimeout = previous })
	for name, setup := range map[string]func(*fakeHA){
		"client timeout":         func(f *fakeHA) { f.hang["/api/services/light/turn_off"] = true },
		"relay upstream timeout": func(f *fakeHA) { f.fail["/api/services/light/turn_off"] = true },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFakeHA()
			setup(f)
			h := newWriteHarness(t, f)
			exit, env := h.run(runCallCommand, "light.turn_off", "light.kitchen")
			if exit != 7 || env.Status != "unverified" || !env.OutcomeUnknown || env.Error.Code != "OUTCOME_UNKNOWN" || len(h.posts()) != 1 {
				t.Fatalf("exit %d env %+v posts %d", exit, env, len(h.posts()))
			}
		})
	}
}

// I8: single use, expiry, and two concurrent applies send exactly once.
func TestApplyIsSingleUseExpiringAndRaceFree(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	h.f.related["area:kitchen"] = []string{"light.kitchen", "light.hall"}
	id := h.plan("light.turn_off", "--area", "kitchen").Plan.ID // preview: no code
	// Two real processes race for one plan, as two shells would.
	cmds := make([]*exec.Cmd, 2)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0], "-test.run=^TestApplyHelperProcess$", "-test.count=1")
		cmds[i].Env = append(os.Environ(), "HA_NOVA_TEST_APPLY_PLAN="+id)
		if err := cmds[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	exits := 0
	for _, cmd := range cmds {
		_ = cmd.Wait()
		exits += cmd.ProcessState.ExitCode()
	}
	if exits != verbExitRefused || len(h.posts()) != 1 {
		t.Fatalf("exit codes sum %d, posts %d: want one 0, one 4, one POST", exits, len(h.posts()))
	}
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("reuse: exit %d env %+v", exit, env)
	}

	h.f.states["light.kitchen"]["state"] = "on"
	id = h.plan("light.turn_off", "--area", "kitchen").Plan.ID
	previous := verbNow
	verbNow = func() time.Time { return previous().Add(planTTL + time.Minute) }
	t.Cleanup(func() { verbNow = previous })
	if exit, env := h.run(runApplyCommand, "--plan", id); exit != 4 || env.Error.Code != "PLAN_EXPIRED" || len(h.posts()) != 1 {
		t.Fatalf("expired: exit %d env %+v", exit, env)
	}
	if exit, env := h.run(runApplyCommand, "--plan", id, "--server", "other"); exit != 4 || env.Error.Code != "PLAN_NOT_FOUND" {
		t.Fatalf("expired plan was kept: exit %d env %+v", exit, env)
	}
}

// TestApplyHelperProcess is the child of the concurrent-apply race above.
func TestApplyHelperProcess(t *testing.T) {
	id := os.Getenv("HA_NOVA_TEST_APPLY_PLAN")
	if id == "" {
		t.Skip("child process of TestApplyIsSingleUseExpiringAndRaceFree")
	}
	paths, err := detectPaths()
	if err != nil {
		os.Exit(1)
	}
	os.Exit(runApplyCommand(paths, []string{"--plan", id}))
}

func TestApplyRefusesAPlanThatFrozeNoTarget(t *testing.T) {
	f := newFakeHA()
	f.fail["search/related"] = true // --area cannot resolve: typed UNRESOLVED_TARGET
	h := newWriteHarness(t, f)
	env := h.plan("light.turn_off", "--area", "kitchen")
	plan := env.Plan
	if strings.Contains(env.Say, "Reply") || strings.Contains(env.Say, "confirm:") {
		t.Fatalf("say asks for a plan apply refuses: %q", env.Say)
	}
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code); exit != 4 || env.Error.Code != "UNRESOLVED_TARGET" || len(h.posts()) != 0 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
}

func TestApplyServerMismatchSendsNothing(t *testing.T) {
	h := newWriteHarness(t, newFakeHA())
	plan := h.plan("lock.unlock", "lock.front").Plan
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code, "--server", "other"); exit != 4 || env.Error.Code != "SERVER_MISMATCH" || len(h.posts()) != 0 {
		t.Fatalf("exit %d env %+v", exit, env)
	}
	if _, err := os.Stat(filepath.Join(planDir(h.paths), plan.ID+".json")); err != nil {
		t.Fatalf("a mismatched apply consumed the plan: %v", err)
	}
}

func TestCallOutcomesUnverifiedFailedAndAccepted(t *testing.T) {
	f := newFakeHA()
	f.flows = []map[string]any{{"flow_id": "old", "handler": "hue", "context": map[string]any{"source": "reauth"}}}
	f.full = []map[string]any{{"entity_id": "switch.plug", "platform": "tplink", "config_entry_id": "e1"}}
	f.entries = []map[string]any{{"domain": "tplink", "entry_id": "e1"}, {"domain": "hue", "entry_id": "e2"}}
	f.service = func(f *fakeHA, path string, body map[string]any) any {
		switch path {
		case "/api/services/light/turn_off":
			return coreReply(200, []any{}) // accepted but nothing changes
		case "/api/services/switch/turn_on":
			f.flows = append(f.flows,
				map[string]any{"flow_id": "new", "handler": "tplink", "context": map[string]any{"source": "reauth", "entry_id": "e1"}},
				map[string]any{"flow_id": "other", "handler": "hue", "context": map[string]any{"source": "reauth", "entry_id": "e2"}}) // not a target's
			return coreReply(500, map[string]any{"message": "auth failed"})
		}
		return coreReply(200, []any{})
	}
	f.ent("switch.plug", "tplink", "off", nil)
	f.script("leave", step("light.turn_off", "light.hall"))
	h := newWriteHarness(t, f)
	if exit, env := h.run(runCallCommand, "light.turn_off", "light.kitchen"); exit != 6 || env.Status != "unverified" || env.Verification[0].Observed != "on" {
		t.Fatalf("unverified: exit %d env %+v", exit, env)
	}
	exit, env := h.run(runCallCommand, "switch.turn_on", "switch.plug")
	if exit != 6 || env.Status != "unverified" || env.Error.Code != "SERVICE_ERROR" || len(env.Verification) != 1 || strings.Join(env.ReauthOpened, ",") != "tplink" {
		t.Fatalf("upstream 500: exit %d env %+v", exit, env)
	}
	plan := h.plan("script.turn_on", "script.leave").Plan
	if exit, env := h.run(runApplyCommand, "--plan", plan.ID); exit != 0 || env.Status != "accepted" {
		t.Fatalf("script run: exit %d env %+v", exit, env)
	}
}
