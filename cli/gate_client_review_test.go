package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Review regressions for the CLI on a gated App (gate_client.go).

// held: the Relay holds every typed write that carries no grant (428).
func held(send gateSend) any {
	if send.grant == "" {
		return fakeRelayStatus{428, "GATE_APPROVAL_REQUIRED", "held"}
	}
	return nil
}

// A lazy ask (a plan made on an older App, applied after the App updated)
// that ends without an approval keeps its own words: its code and next
// step. A call plan goes back for a retry; a batch whose first member
// already ran is never handed back.
func TestGateLazyAskRefusalKeepsItsWords(t *testing.T) {
	t.Run("call", func(t *testing.T) {
		g := &fakeGate{hold: held, state: "cancelled"}
		h := gatedHarness(t, newFakeHA(), g)
		h.log.gate.Store(false) // planned on an older App: a code
		plan := h.plan("lock.unlock", "lock.front").Plan
		h.log.gate.Store(true) // the App updated before apply
		exit, env := h.run(runApplyCommand, "--plan", plan.ID, "--confirm", plan.Code)
		if _, _, sends := g.snapshot(); exit != verbExitRefused || env.Error == nil || env.Error.Code != "CONFIRM_DECLINED" || len(sends) != 1 || len(h.planFiles()) != 1 {
			t.Fatalf("exit %d %+v sends %+v plans %v", exit, env.Error, sends, h.planFiles())
		}
	})
	t.Run("batch second member", func(t *testing.T) {
		g := &fakeGate{hold: held, endFor: map[string]string{"ask2": "cancelled"}}
		h := gatedHarness(t, batchHome(), g)
		h.log.gate.Store(false)
		unlock := func(alias string) map[string]any {
			return map[string]any{"alias": alias, "sequence": []any{step("lock.unlock", "lock.front")}}
		}
		_, env := h.batch(h.writePlan("script", "s1", unlock("Arrival")), h.writePlan("script", "s2", unlock("Old arrival")))
		h.log.gate.Store(true)
		exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID, "--confirm", env.Plan.Code)
		items := batchItems(t, applied)
		kept := slices.ContainsFunc(h.planFiles(), func(f string) bool { return strings.HasPrefix(filepath.Base(f), env.Plan.ID) })
		if exit == 0 || len(items) != 2 || items[0].Status != "verified" || items[1].Status != "not_sent" || !strings.Contains(items[1].Error, "cancelled on the phone") ||
			applied.Error == nil || !strings.Contains(applied.Error.Next, gateEnded("cancelled").next) || kept {
			t.Fatalf("exit %d %+v items %+v kept %v", exit, applied.Error, items, kept)
		}
	})
}

// After the person approved, the plan is checked again right before the
// first send: a home that changed meanwhile, or a plan that expired, sends
// nothing, and the grant expires unused.
func TestGateChecksThePlanAgainAfterTheApproval(t *testing.T) {
	previous := verbNow
	t.Cleanup(func() { verbNow = previous })
	for name, tc := range map[string]struct {
		change func(f *fakeHA)
		exit   int
		code   string
	}{
		"home changed": {func(f *fakeHA) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.states["lock.front"]["state"] = "unlocked"
		}, verbExitDrift, "PLAN_DRIFT"},
		"plan expired": {func(*fakeHA) { verbNow = func() time.Time { return previous().Add(planTTL + time.Minute) } }, verbExitRefused, "PLAN_EXPIRED"},
	} {
		t.Run(name, func(t *testing.T) {
			verbNow = previous
			f := newFakeHA()
			g := &fakeGate{onWait: func() { tc.change(f) }}
			h := gatedHarness(t, f, g)
			plan := h.plan("lock.unlock", "lock.front").Plan
			exit, env := h.run(runApplyCommand, "--plan", plan.ID)
			if asks, _, sends := g.snapshot(); exit != tc.exit || env.Error == nil || env.Error.Code != tc.code || len(asks) != 1 || len(sends) != 0 {
				t.Fatalf("exit %d %+v asks %q sends %+v", exit, env.Error, asks, sends)
			}
		})
	}
}

// A typed batch one ask can't cover (more than 20 requests) is refused
// before anything is asked or sent.
func TestGateRefusesABatchTooLargeForOneAsk(t *testing.T) {
	g := &fakeGate{}
	f := batchHome()
	for i := range 11 { // a delete asks with its reload: 22 requests
		f.storeConfig(fmt.Sprint("/api/config/script/config/door_", i), map[string]any{"alias": fmt.Sprint("Door ", i), "sequence": []any{step("lock.unlock", "lock.front")}})
	}
	h := gatedHarness(t, f, g)
	var ids []string
	for i := range 11 {
		ids = append(ids, h.member("delete", fmt.Sprint("script.door_", i)))
	}
	_, env := h.batch(ids...)
	if env.Plan == nil || env.Plan.Channel != channelRelay {
		t.Fatalf("batch plan: %+v", env)
	}
	exit, applied := h.run(runApplyCommand, "--plan", env.Plan.ID)
	if asks, _, sends := g.snapshot(); exit != verbExitRefused || applied.Error == nil || applied.Error.Code != "GATE_ASK_TOO_LARGE" || len(asks)+len(sends) != 0 {
		t.Fatalf("exit %d %+v asks %d sends %d", exit, applied.Error, len(asks), len(sends))
	}
}

// Through Link: the Relay holds the write (428), the ask's grant reaches
// the Relay, and its own gate refusal on 5xx (503 GATE_UNAVAILABLE) is
// refused, nothing sent; 502 GATE_ANSWER_WITHHELD stays unknown.
func TestGateOverLink(t *testing.T) {
	for final, unknown := range map[string]bool{
		`503 {"ok":false,"error":{"code":"GATE_UNAVAILABLE","message":"down"}}`:    false,
		`502 {"ok":false,"error":{"code":"GATE_ANSWER_WITHHELD","message":"ran"}}`: true,
	} {
		status, body, _ := strings.Cut(final, " ")
		t.Run(status, func(t *testing.T) {
			g := &fakeGate{}
			var mu sync.Mutex
			var grants []string
			ingress := newProtocolTestCloudIngressClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				raw, _ := io.ReadAll(r.Body)
				var p map[string]any
				_ = json.Unmarshal(raw, &p)
				switch {
				case strings.HasSuffix(r.URL.Path, "/gate/v1/ask"):
					data, _ := json.Marshal(map[string]any{"ok": true, "data": g.ask(r.Context(), p)})
					return relayAnswer(http.StatusOK, string(data)), nil
				case strings.HasSuffix(r.URL.Path, "/core"):
					grant := r.Header.Get(gateGrantHeader)
					mu.Lock()
					grants = append(grants, grant)
					mu.Unlock()
					if grant == "" {
						return relayAnswer(http.StatusPreconditionRequired, `{"ok":false,"error":{"code":"GATE_APPROVAL_REQUIRED","message":"held"}}`), nil
					}
					code := map[string]int{"503": http.StatusServiceUnavailable, "502": http.StatusBadGateway}[status]
					return relayAnswer(code, body), nil
				}
				return nil, errors.New("unexpected " + r.URL.Path)
			}))
			transport, err := newCloudRelayTransport(ingress, validCredential(41))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			s := &relaySession{transport: transport, ctx: ctx, cancel: cancel, slots: make(chan struct{}, relaySessionMaxConcurrent), memo: map[string]*relayMemo{}}
			_, err = s.postService("lock.unlock", map[string]any{"entity_id": []string{"lock.front"}}, false)
			var call *relayCallError
			var lost *relayOutcomeUnknownError
			refused := errors.As(err, &call) && call.code == "GATE_UNAVAILABLE" && !errors.As(err, &lost)
			mu.Lock()
			defer mu.Unlock()
			if strings.Join(grants, ",") != ",grant-ask1" || errors.As(err, &lost) != unknown || !unknown && !refused {
				t.Fatalf("err %v (%T), grants %q", err, err, grants)
			}
		})
	}
}

// The CLI's ask ends with what is left of the command's own wait, not a
// fresh one: nothing is sent, and the ask is cancelled on the Relay.
func TestGateAskEndsWithTheCommandsWait(t *testing.T) {
	g := &fakeGate{release: make(chan struct{})}
	h := gatedHarness(t, newFakeHA(), g)
	plan := h.plan("lock.unlock", "lock.front").Plan
	saved := commandStart
	t.Cleanup(func() { commandStart = saved })
	commandStart = time.Now().Add(-(windowWait - time.Second))
	start := time.Now()
	exit, env := h.run(runApplyCommand, "--plan", plan.ID)
	_, ops, sends := g.snapshot()
	if exit != verbExitRefused || env.Error == nil || env.Error.Code != "CONFIRM_CANCELLED" || time.Since(start) > 15*time.Second ||
		!slices.Contains(ops, "cancel") || len(sends) != 0 || len(h.planFiles()) != 1 {
		t.Fatalf("exit %d %+v after %v; ops %v sends %+v", exit, env.Error, time.Since(start), ops, sends)
	}
}

// The waiting window's first button opens the ask on the NOVA page at an
// address from the paired profile only: through Link, its Link address;
// else its Home Assistant URL, or the Link address when it has none.
// Without either, the button only hides the window and the text says where
// the page is.
func TestGateWindowPageFromTheProfile(t *testing.T) {
	_, restore := setCloudFeatureTestIdentity(t, cloudRemoteBuildIdentity{})
	if got := (&relaySession{cfg: runtimeConfig{HAURL: "http://homeassistant.local:8123"}}).gatePageURL("ask1"); got != "http://homeassistant.local:8123" {
		t.Errorf("unstamped build: %q, want Home Assistant's start without an ask", got)
	}
	restore()
	_, restore = setCloudFeatureTestIdentity(t, cloudRemoteBuildIdentity{Development: true, AppSlug: "local_ha_nova_relay_test"})
	defer restore()
	panel := func(base string) string {
		target, _ := haNOVAAppPanelURL(base)
		return target.URL + "?nova_ask=" + fmt.Sprintf("%x", sha256.Sum256([]byte("ask1")))
	}
	link := &cloudLifecycleMetadata{Current: &cloudConnectionMetadata{CanonicalOrigin: "https://example.ui.nabu.casa"}}
	for name, tc := range map[string]struct {
		cfg  runtimeConfig
		via  relayVia
		want string
	}{
		"local":        {runtimeConfig{HAURL: "http://homeassistant.local:8123"}, relayViaLocal, panel("http://homeassistant.local:8123")},
		"link only":    {runtimeConfig{Cloud: link}, relayViaLocal, panel("https://example.ui.nabu.casa")},
		"through Link": {runtimeConfig{HAURL: "http://homeassistant.local:8123", Cloud: link}, relayViaCloud, panel("https://example.ui.nabu.casa")},
		"neither":      {runtimeConfig{HAURL: "homeassistant"}, relayViaLocal, ""},
	} {
		if got := (&relaySession{cfg: tc.cfg, transport: relayTransportSelection{Via: tc.via}}).gatePageURL("ask1"); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
	savedAvailable, savedWindow := windowAvailable, gateWindow
	t.Cleanup(func() { windowAvailable, gateWindow = savedAvailable, savedWindow })
	windowAvailable = func() bool { return true }
	var message, first string
	gateWindow = func(_ context.Context, m, f, _ string, _ time.Duration) windowAnswer {
		message, first = m, f
		return windowConfirmed
	}
	window, _ := (&relaySession{paths: runtimePaths{ConfigFile: filepath.Join(t.TempDir(), "none.json")}}).gateWindow(context.Background(), time.Now().Add(time.Second), "")
	want := confirmT("en", "gate.window_page")
	if runtime.GOOS == "windows" {
		want += "\n\n" + confirmT("en", "gate.windows_hide")
	}
	if <-window != windowConfirmed || message != want || first != confirmT("en", "gate.hide") {
		t.Fatalf("no page: %q / %q", message, first)
	}
}

// German homes read the gate's texts in German (informal du), from the
// catalog: the plan's ask, the waiting window, and the terminal line.
func TestGateGermanTexts(t *testing.T) {
	for key, want := range map[string]string{
		"gate.open":         "In Home Assistant öffnen",
		"say.relay_waiting": "Ich warte auf dein OK auf deinem Handy oder in Home Assistant; dieser Schritt wurde noch nicht gesendet.",
		"say.relay_ask":     "Bestätige auf deinem verbundenen Handy oder in Home Assistant (NOVA).",
	} {
		if got := confirmT("de", key); got != want {
			t.Errorf("%s = %q", key, got)
		}
	}
	savedAvailable, savedWindow := windowAvailable, gateWindow
	t.Cleanup(func() { windowAvailable, gateWindow = savedAvailable, savedWindow })
	windowAvailable = func() bool { return true }
	var texts []string
	gateWindow = func(ctx context.Context, message, _, _ string, _ time.Duration) windowAnswer {
		texts = append(texts, message)
		<-ctx.Done()
		return windowNoAnswer
	}
	f := newFakeHA()
	f.config = map[string]any{"language": "de"}
	h := gatedHarness(t, f, &fakeGate{})
	env := h.plan("lock.unlock", "lock.front")
	want := "Bestätige diese Änderung in Home Assistant: Öffne sie auf der NOVA-Seite in deinem Browser und drück dort „Genehmigen“. Oder bestätige sie auf deinem Handy. Dieses Fenster kann sie nicht bestätigen, und noch wurde nichts gesendet."
	if runtime.GOOS == "windows" {
		want += "\n\n" + confirmT("de", "gate.windows_ok")
	}
	if exit, _ := h.run(runApplyCommand, "--plan", env.Plan.ID); !strings.HasSuffix(env.Say, confirmT("de", "say.relay_ask")) || exit != 0 || len(texts) != 1 || texts[0] != want {
		t.Fatalf("say %q exit %d window %q", env.Say, exit, texts)
	}
}
