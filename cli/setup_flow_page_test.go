package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func (p *setupPageTest) state() setupPageState {
	p.t.Helper()
	request, _ := http.NewRequest(http.MethodGet, p.base+"/api/state", nil)
	request.AddCookie(p.cookie)
	response, err := setupPageNoRedirect.Do(request)
	if err != nil {
		p.t.Fatal(err)
	}
	defer response.Body.Close()
	var state setupPageState
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&state) != nil {
		p.t.Fatalf("state = %d", response.StatusCode)
	}
	return state
}

// waitFor polls the page like the browser does until ready holds.
func (p *setupPageTest) waitFor(what string, ready func(setupPageState) bool) setupPageState {
	p.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		state := p.state()
		if ready(state) {
			return state
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("page never showed %s; last state %+v", what, state)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// click clicks like the browser: only once the page offers the action again
// (a click is taken once; the engine's next wait offers it again). "stop" is
// never in Accepts.
func (p *setupPageTest) click(action, value string) {
	p.t.Helper()
	if action != "stop" {
		p.waitFor(action, accepting(action))
	}
	body, _ := json.Marshal(setupPageAction{Action: action, Value: value})
	if code := p.post(string(body), nil); code != http.StatusNoContent {
		p.t.Fatalf("click %s = %d", action, code)
	}
}

func accepting(action string) func(setupPageState) bool {
	return func(state setupPageState) bool { return slices.Contains(state.Accepts, action) }
}

// stubSetupPageHome finds one Home Assistant whose App runs and serves
// sign-in, and lets pairing and verification succeed.
func stubSetupPageHome(t *testing.T, haURL string, appRunning bool) *int {
	t.Helper()
	previousDiscover, previousProbe, previousAdvertises := discoverReachableHAHostsForSetup, probeInstalledRelayForSetup, relayAdvertisesSignInForSetup
	previousOrigin, previousPairing, previousPair, previousVerify, previousLinger := resolveHASignInOriginForSetup, probePairingV1ForSetup, signInPairForSetup, verifyDeviceHealth, setupPageLinger
	t.Cleanup(func() {
		discoverReachableHAHostsForSetup, probeInstalledRelayForSetup, relayAdvertisesSignInForSetup = previousDiscover, previousProbe, previousAdvertises
		resolveHASignInOriginForSetup, probePairingV1ForSetup, signInPairForSetup, verifyDeviceHealth, setupPageLinger = previousOrigin, previousPairing, previousPair, previousVerify, previousLinger
		setupInstalledSignIn, setupOwnerIngress, setupSignedInUser = nil, nil, ""
	})
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) {
		return []setupDiscoveryCandidate{{Host: normalizeHostInput(haURL), HAURL: haURL}}, ""
	}
	probeInstalledRelayForSetup = func(string) (bool, string) { return appRunning, "99.0.0" }
	relayAdvertisesSignInForSetup = func(string) bool { return appRunning }
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) { return url.Parse(haURL) }
	probePairingV1ForSetup = func(string) bool { return true }
	pairs := 0
	signInPairForSetup = func(string, pairingProvisioner, *runtimeConfig, func(*runtimeConfig) error, pairingClientInfo, SecretStoreUIPolicy) (string, error) {
		pairs++
		return "device-1", nil
	}
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceOK }
	setupPageLinger = 0
	return &pairs
}

func startSetupPageFlow(t *testing.T, stdin string) (*setupPageTest, *bytes.Buffer, chan int) {
	t.Helper()
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	return startSetupPageFlowWith(t, stdin, &setupFlow{paths: paths, state: loadStateOrDefault(paths), connectionMode: setupConnectionLocal, pairingBackStep: setupStepRelayInstall})
}

func startSetupPageFlowWith(t *testing.T, stdin string, flow *setupFlow) (*setupPageTest, *bytes.Buffer, chan int) {
	t.Helper()
	var stdout bytes.Buffer
	ui := newSetupWebUI(strings.NewReader(stdin), &stdout)
	server, err := startSetupPageServer(ui)
	if err != nil {
		t.Fatal(err)
	}
	page := &setupPageTest{t: t, ui: ui, server: server, base: "http://127.0.0.1:" + server.port}
	done := make(chan int, 1)
	go func() { done <- flow.runWithPage(ui) }()
	page.launch()
	return page, &stdout, done
}

func setupPageExit(t *testing.T, done chan int, stdout *bytes.Buffer) int {
	t.Helper()
	select {
	case code := <-done:
		return code
	case <-time.After(20 * time.Second):
		t.Fatalf("setup did not end\n%s", stdout)
	}
	return -1
}

// The whole page path: found → one click → Home Assistant's sign-in in the
// same tab → its redirect back (cross-site, so without the session cookie)
// → paired, verified, skills installed → done. One sign-in, revoked after.
func TestSetupPageSignsInInTheSameTabAndConnects(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	setupGoldenHome(t)
	ha := newFakeSignInHA(t)
	pairs := stubSetupPageHome(t, ha.server.URL, true)
	page, stdout, done := startSetupPageFlow(t, "")

	found := page.waitFor("the found screen", accepting("signin"))
	if found.Screen != "found" || found.HA != normalizeHostInput(ha.server.URL) || found.App != "running" ||
		found.Primary != "signin" || !slices.Equal(found.Clients, []string{"Codex"}) {
		t.Fatalf("found = %+v", found)
	}
	page.click("signin", "")
	waiting := page.waitFor("the sign-in URL", func(s setupPageState) bool { return s.SignInURL != "" })
	authorize, err := url.Parse(waiting.SignInURL)
	if err != nil || !strings.HasPrefix(waiting.SignInURL, ha.server.URL+"/auth/authorize?") ||
		authorize.Query().Get("redirect_uri") != page.base+oauthLoopbackCallbackPath ||
		authorize.Query().Get("client_id") != page.base+oauthLoopbackClientPath {
		t.Fatalf("sign-in URL = %q", waiting.SignInURL)
	}
	callback := authorize.Query().Get("redirect_uri") + "?" + signedInQuery(authorize.Query().Get("state"))
	// No cookie: SameSite=Strict stays home on a cross-site redirect.
	if target := bounceTarget(page.do(http.MethodGet, strings.TrimPrefix(callback, page.base), "", nil)); target != "/" {
		t.Fatalf("callback bounces to %q", target)
	}
	if target := bounceTarget(page.do(http.MethodGet, strings.TrimPrefix(callback, page.base), "", nil)); target != "" {
		t.Fatalf("a replayed callback was taken: %q", target)
	}

	// Step 3: the phones can't be read here (it says so), no Claude Code,
	// no Link; then step 4.
	control := page.waitFor("step 3", accepting("finish"))
	if control.Screen != "control" || control.Control == nil || control.Control.Phone != "unread" || !slices.Contains(control.Accepts, "recheck") ||
		control.Control.Rule != "" || control.Control.Link != "" || !control.Control.Window != !windowAvailable() {
		t.Fatalf("step 3 = %+v %+v", control, control.Control)
	}
	connected := control
	page.click("finish", "")
	home := page.waitFor("step 4", accepting("done"))
	if home.Screen != "home" || home.Home == nil || !home.Home.Failed || slices.Contains(home.Accepts, "claude") {
		t.Fatalf("step 4 = %+v %+v", home, home.Home)
	}
	page.click("done", "")
	code := setupPageExit(t, done, stdout) // setupPageLinger is 0: the server is gone now
	end := page.ui.snapshot()
	if code != 0 || end.Screen != "closed" {
		t.Fatalf("exit %d, screen %s\n%s", code, end.Screen, stdout)
	}
	rows := map[string]string{}
	for _, row := range connected.Rows {
		rows[row.ID] = row.State
	}
	if want := map[string]string{"signed_in": "ok", "connect": "ok", "teach": "ok"}; !mapsEqual(rows, want) {
		t.Fatalf("rows = %v, want %v", rows, want)
	}
	if connected.Rows[0].Msg.Key != "setup.page.row.signed_in" || connected.Rows[0].Msg.Args["name"] != "Markus" {
		t.Fatalf("signed-in row = %+v", connected.Rows[0].Msg)
	}
	if !strings.Contains(stdout.String(), setupT("setup.page.terminal.home")) {
		t.Fatalf("terminal never pointed to the page's step 4:\n%s", stdout)
	}
	ha.mu.Lock()
	revoked := ha.revoked
	ha.mu.Unlock()
	if *pairs != 1 || !revoked {
		t.Fatalf("pairs=%d revoked=%v", *pairs, revoked)
	}
	if out := stdout.String(); strings.Contains(out, "\x1b[") || strings.Contains(out, "good-code") || strings.Contains(out, page.server.token) {
		t.Fatalf("terminal transcript is not plain or leaks a secret:\n%s", out)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if b[key] != value {
			return false
		}
	}
	return true
}

// stubPageSignIn replaces the browser round trip: it runs prepare against the
// fake Supervisor as the given user, or fails with err.
func stubPageSignIn(t *testing.T, supervisor *fakeSupervisor, user HACurrentUser, err error) {
	t.Helper()
	previous := openHASignInInTabForSetup
	t.Cleanup(func() { openHASignInInTabForSetup = previous; setupInstalledSignIn = nil })
	openHASignInInTabForSetup = func(ctx context.Context, _ io.Writer, _ string, prepare haSignInPrepare, tab haSignInTab) (haSignInSession, error) {
		tab.navigate("http://ha.local:8123/auth/authorize?state=s")
		session := haSignInSession{user: user}
		if err != nil {
			return session, err
		}
		if !user.IsOwner {
			return session, errSignInNotOwner
		}
		return session, prepare(ctx, supervisor, user)
	}
}

func pageSignInFlow(t *testing.T) (*setupFlow, *setupPageTest) {
	t.Helper()
	page := startSetupPageTest(t)
	previousOrigin, previousAdvertises := resolveHASignInOriginForSetup, relayAdvertisesSignInForSetup
	t.Cleanup(func() {
		resolveHASignInOriginForSetup, relayAdvertisesSignInForSetup = previousOrigin, previousAdvertises
	})
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) { return url.Parse("http://ha.local:8123") }
	relayAdvertisesSignInForSetup = func(string) bool { return true }
	cfg := applySelectedSetupHost(runtimeConfig{}, "ha.local", "http://ha.local:8123", "")
	flow := &setupFlow{paths: runtimePaths{ConfigDir: t.TempDir()}, cfg: cfg, ui: page.ui, page: page.ui, selectedClients: []string{"claude", "codex"}}
	return flow, page
}

func pageRows(state setupPageState) string {
	var rows []string
	for _, row := range state.Rows {
		rows = append(rows, row.ID+"="+row.State)
	}
	return strings.Join(rows, " ")
}

// "Sign in and install NOVA": the progress rows follow the Supervisor steps,
// and the session waits for the pairing step.
func TestSetupPageInstallShowsEachStep(t *testing.T) {
	flow, page := pageSignInFlow(t)
	supervisor := newFakeSupervisor(t)
	stubPageSignIn(t, supervisor, HACurrentUser{Name: "Markus", IsOwner: true}, nil)
	failure, interrupted := flow.pageSignIn(setupPageApp{state: "missing", primary: "install"})
	state := page.ui.snapshot()
	if failure != nil || interrupted || setupInstalledSignIn == nil || state.SignInURL != "" {
		t.Fatalf("failure=%+v interrupted=%v session=%v url=%q", failure, interrupted, setupInstalledSignIn, state.SignInURL)
	}
	if got := pageRows(state); got != "signed_in=ok app_add=ok app_start=ok connect=todo teach=todo" {
		t.Fatalf("rows = %s", got)
	}
	if teach := state.Rows[4].Msg; teach.Key != "setup.page.row.teach" || !slices.Equal(teach.Args["clients"].([]string), []string{"Claude Code", "Codex"}) {
		t.Fatalf("teach row = %+v", teach)
	}
	if !slices.Contains(supervisor.writes(), "post /addons/"+HAOfficialNOVAAppSlug+"/start") {
		t.Fatalf("writes = %v", supervisor.writes())
	}
	if state.Rows[1].Msg.Key != "setup.page.row.app_add" || state.Rows[2].Msg.Key != "setup.page.row.app_start" {
		t.Fatalf("app rows = %+v %+v", state.Rows[1].Msg, state.Rows[2].Msg)
	}

	// The check before the sign-in missed the running App (lane F3): the
	// rows say it was there, not that setup added or started it.
	flow, page = pageSignInFlow(t)
	supervisor = newFakeSupervisor(t)
	supervisor.installedApp("0.10.0", "started", "")
	stubPageSignIn(t, supervisor, HACurrentUser{Name: "Markus", IsOwner: true}, nil)
	if failure, _ := flow.pageSignIn(setupPageApp{state: "missing", primary: "install"}); failure != nil {
		t.Fatalf("failure = %+v", failure)
	}
	state = page.ui.snapshot()
	if got := pageRows(state); got != "signed_in=ok app_add=ok app_start=ok connect=todo teach=todo" ||
		state.Rows[1].Msg.Key != "setup.page.row.app_there" || state.Rows[2].Msg.Key != "setup.page.row.app_running" || len(supervisor.writes()) != 0 {
		t.Fatalf("rows = %s %+v %+v writes %v", got, state.Rows[1].Msg, state.Rows[2].Msg, supervisor.writes())
	}
}

// Soft failures stay on step 2 with one line and the next step; names from
// Home Assistant arrive without control or format characters.
func TestSetupPageSignInFailuresSayWhatNext(t *testing.T) {
	t.Run("not the owner", func(t *testing.T) {
		flow, page := pageSignInFlow(t)
		stubPageSignIn(t, newFakeSupervisor(t), HACurrentUser{Name: "An‮na\x1b[31m"}, nil)
		failure, interrupted := flow.pageSignIn(setupPageApp{state: "running", primary: "signin"})
		if interrupted || failure == nil || failure.fix != "owner" || failure.problem.Key != "setup.page.problem.not_owner" ||
			failure.problem.Args["name"] != "Anna[31m" || setupInstalledSignIn != nil {
			t.Fatalf("failure = %+v %+v", failure, failure.problem)
		}
		if got := pageRows(page.ui.snapshot()); got != "signed_in=fail connect=todo teach=todo" {
			t.Fatalf("rows = %s", got)
		}
	})
	t.Run("install fails half way", func(t *testing.T) {
		flow, page := pageSignInFlow(t)
		supervisor := newFakeSupervisor(t)
		supervisor.startFails = true
		stubPageSignIn(t, supervisor, HACurrentUser{Name: "Markus", IsOwner: true}, nil)
		failure, _ := flow.pageSignIn(setupPageApp{state: "missing", primary: "install"})
		if failure == nil || failure.problem.Key != "setup.page.problem.install" || failure.problem.Args["reason"] == "" ||
			failure.links["app"] != haInstallRelayAppURL(flow.cfg.HAURL) || failure.fix != "retry" {
			t.Fatalf("failure = %+v", failure)
		}
		if got := pageRows(page.ui.snapshot()); got != "signed_in=ok app_add=ok app_start=fail connect=todo teach=todo" {
			t.Fatalf("rows = %s", got)
		}
	})
	t.Run("too slow after the sign-in", func(t *testing.T) { // lane F1: not the address field
		flow, _ := pageSignInFlow(t)
		for _, err := range []error{
			newCloudError(CloudErrTimeout, "x", context.DeadlineExceeded),
			errors.Join(errSignInAppMissing, newCloudError(CloudErrTimeout, "read the App", context.DeadlineExceeded)),
		} {
			stubPageSignIn(t, newFakeSupervisor(t), HACurrentUser{Name: "Markus", IsOwner: true}, err)
			failure, _ := flow.pageSignIn(setupPageApp{state: "running", primary: "signin"})
			if failure == nil || failure.problem.Key != "setup.page.problem.slow" || failure.address || failure.fix != "retry" {
				t.Fatalf("%v: failure = %+v", err, failure)
			}
		}
	})
	t.Run("no Apps", func(t *testing.T) {
		flow, _ := pageSignInFlow(t)
		stubPageSignIn(t, newFakeSupervisor(t), HACurrentUser{Name: "Markus", IsOwner: true}, errAppInstallNoSupervisor)
		failure, _ := flow.pageSignIn(setupPageApp{state: "missing", primary: "install"})
		if failure == nil || failure.problem.Key != "setup.page.problem.no_apps" || failure.links["guide"] != relayContainerGuideURL || failure.fix != "" {
			t.Fatalf("failure = %+v", failure)
		}
	})
}

// The page's Stop ends only the sign-in; Ctrl+C in the terminal ends setup.
func TestSetupPageStopVersusCtrlC(t *testing.T) {
	for _, fromPage := range []bool{true, false} {
		flow, page := pageSignInFlow(t)
		previous := openHASignInInTabForSetup
		openHASignInInTabForSetup = func(ctx context.Context, _ io.Writer, _ string, _ haSignInPrepare, tab haSignInTab) (haSignInSession, error) {
			if err := page.ui.act(setupPageAction{Action: "stop"}); err == nil {
				t.Error("Stop was taken before the page waits for the sign-in")
			}
			tab.navigate("http://ha.local:8123/auth/authorize?state=s")
			if !fromPage {
				return haSignInSession{}, errors.Join(errSignInStopped, context.Canceled) // the sign-in's own Ctrl+C
			}
			if err := page.ui.act(setupPageAction{Action: "stop"}); err != nil {
				t.Error(err)
			}
			<-ctx.Done()
			return haSignInSession{}, errors.Join(errSignInStopped, ctx.Err())
		}
		failure, interrupted := flow.pageSignIn(setupPageApp{state: "running", primary: "signin"})
		openHASignInInTabForSetup = previous
		if fromPage && (interrupted || failure == nil || failure.problem.Key != "setup.page.problem.stopped") {
			t.Fatalf("page stop: failure=%+v interrupted=%v", failure, interrupted)
		}
		if !fromPage && (!interrupted || failure != nil) {
			t.Fatalf("Ctrl+C: failure=%+v interrupted=%v", failure, interrupted)
		}
	}
}

// "Use a six-digit code" hands setup to the terminal; the page says so and
// the text wizard asks there.
func TestSetupPageHandsOverToTheTerminal(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	setupGoldenHome(t)
	stubSetupPageHome(t, "http://192.0.2.10:8123", true)
	page, stdout, done := startSetupPageFlow(t, "exit\n")
	page.waitFor("the found screen", accepting("terminal"))
	page.click("terminal", "")
	code := setupPageExit(t, done, stdout)
	end := page.ui.snapshot()
	if code != 0 || end.Screen != "stopped" || end.Handover != "code" || !page.ui.inTerminal() || !strings.Contains(stdout.String(), "one-time six-digit code") {
		t.Fatalf("exit %d screen %s inTerminal=%v\n%s", code, end.Screen, page.ui.inTerminal(), stdout)
	}
}

// Nobody clicks for setupPageIdle: setup ends, the page says so.
func TestSetupPageEndsWhenIdle(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	setupGoldenHome(t)
	stubSetupPageHome(t, "http://192.0.2.10:8123", false)
	previous := setupPageIdle
	setupPageIdle = 200 * time.Millisecond
	t.Cleanup(func() { setupPageIdle = previous })
	page, stdout, done := startSetupPageFlow(t, "")
	if code := setupPageExit(t, done, stdout); code != 0 || page.ui.snapshot().Screen != "idle" {
		t.Fatalf("exit %d screen %s\n%s", code, page.ui.snapshot().Screen, stdout)
	}
}

// A typed address is checked before setup uses it; an unreachable one says
// so on the page.
func TestSetupPageChecksATypedAddress(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	setupGoldenHome(t)
	stubSetupPageHome(t, "http://192.0.2.10:8123", false)
	discoverReachableHAHostsForSetup = func(runtimeConfig) ([]setupDiscoveryCandidate, string) { return nil, "" }
	previousResolve := resolveHAURLBaseForSetup
	t.Cleanup(func() { resolveHAURLBaseForSetup = previousResolve })
	resolveHAURLBaseForSetup = func(input string) (string, error) {
		if input == "192.0.2.10" {
			return "http://192.0.2.10:8123", nil
		}
		return "", errors.New("unreachable")
	}
	previousIdle := setupPageIdle
	setupPageIdle = time.Second // ends the run after the last check
	t.Cleanup(func() { setupPageIdle = previousIdle })
	page, stdout, done := startSetupPageFlow(t, "")
	ask := page.waitFor("the address question", accepting("address"))
	if ask.HA != "" || ask.App != "" || ask.Primary != "" {
		t.Fatalf("ask = %+v", ask)
	}
	page.click("address", "nope.invalid")
	wrong := page.waitFor("the address problem", func(s setupPageState) bool { return s.Problem != nil && slices.Contains(s.Accepts, "address") })
	if wrong.Problem.Key != "setup.page.problem.address" || wrong.Problem.Args["address"] != "nope.invalid" {
		t.Fatalf("problem = %+v", wrong.Problem)
	}
	page.click("address", "192.0.2.10")
	found := page.waitFor("the address", func(s setupPageState) bool { return s.HA == "192.0.2.10" && s.Problem == nil && len(s.Accepts) > 0 })
	if found.App != "missing" || !found.Typed { // lane E5: step 1 says the address was typed
		t.Fatalf("found = %+v", found)
	}
	setupPageExit(t, done, stdout)
}

// The page is for a plain `ha-nova setup` at a desktop, first run or re-run;
// everything else keeps the text wizard, and go test never gets the page.
func TestSetupPageOnlyForAPlainSetup(t *testing.T) {
	if defaultSetupPageAvailable() {
		t.Fatal("the page is available under go test")
	}
	previous := setupPageAvailable
	setupPageAvailable = func() bool { return true }
	t.Cleanup(func() { setupPageAvailable = previous })
	resetServerProfileSelection(t)
	fresh := func() *setupFlow { return &setupFlow{} }
	if !fresh().usesSetupPage() {
		t.Fatal("a fresh first run should use the page")
	}
	rerun := fresh()
	rerun.cfg = runtimeConfig{HAHost: "ha.local", HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791"}
	if !rerun.usesSetupPage() {
		t.Fatal("a re-run on a saved setup should use the page (G1)")
	}
	for name, change := range map[string]func(*setupFlow){
		"client named": func(f *setupFlow) { f.target = "claude" },
		"service":      func(f *setupFlow) { f.serviceMode = true },
		"host flag":    func(f *setupFlow) { f.hostFlag = "ha.local" },
		"relay token":  func(f *setupFlow) { f.relayTokenFlag = "x" },
		"Link only":    func(f *setupFlow) { f.cfg.Cloud = &cloudLifecycleMetadata{} },
		"custom Relay": func(f *setupFlow) {
			f.cfg = runtimeConfig{HAHost: "ha.local", HAURL: "http://ha.local:8123", RelayBaseURL: "http://proxy.lan:9000"}
		},
		"Link not ready": func(f *setupFlow) {
			f.cfg = runtimeConfig{HAHost: "ha.local", HAURL: "http://ha.local:8123", Cloud: &cloudLifecycleMetadata{}}
		},
	} {
		flow := fresh()
		change(flow)
		if flow.usesSetupPage() {
			t.Errorf("%s: uses the page", name)
		}
	}
	setupPageAvailable = func() bool { return false }
	if fresh().usesSetupPage() {
		t.Fatal("no desktop: uses the page")
	}
}

// A pairing that fails after the page's sign-in returns to step 2 with one
// line and "Try again", instead of a silent switch to the terminal.
func TestSetupPagePairingFailureReturnsToThePage(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	setupGoldenHome(t)
	ha := newFakeSignInHA(t)
	stubSetupPageHome(t, ha.server.URL, true)
	signInPairForSetup = func(string, pairingProvisioner, *runtimeConfig, func(*runtimeConfig) error, pairingClientInfo, SecretStoreUIPolicy) (string, error) {
		return "", &haSignInPairingError{err: errors.New("ingress exchange failed")}
	}
	previousIdle := setupPageIdle
	setupPageIdle = 2 * time.Second // ends the run after the check
	t.Cleanup(func() { setupPageIdle = previousIdle })
	page, stdout, done := startSetupPageFlow(t, "")
	page.waitFor("the found screen", accepting("signin"))
	page.click("signin", "")
	waiting := page.waitFor("the sign-in URL", func(s setupPageState) bool { return s.SignInURL != "" })
	authorize, _ := url.Parse(waiting.SignInURL)
	page.do(http.MethodGet, oauthLoopbackCallbackPath+"?"+signedInQuery(authorize.Query().Get("state")), "", nil)
	back := page.waitFor("the pairing problem", func(s setupPageState) bool { return s.Problem != nil && len(s.Accepts) > 0 })
	if got := pageRows(back); strings.Contains(got, "=run") {
		t.Fatalf("rows still running after the failure: %s", got)
	}
	if back.Screen != "connect" || back.Problem.Key != "setup.page.problem.pairing" || back.Fix != "retry" ||
		!slices.Contains(back.Accepts, "signin") || page.ui.inTerminal() || setupInstalledSignIn != nil {
		t.Fatalf("back = %+v inTerminal=%v", back, page.ui.inTerminal())
	}
	// Not proven to fail before the finish request: Try again signs in again.
	page.click("signin", "")
	page.waitFor("a new sign-in", func(s setupPageState) bool { return s.SignInURL != "" })
	page.click("stop", "")
	setupPageExit(t, done, stdout)
}

// A busy Home Assistant (0.27 lane F1: each ingress answer took 15–30 s):
// the pairing exchange outlasts the 30 s step budget, a timeout says why,
// and Try again reuses the sign-in instead of a new Home Assistant login.
func TestSetupPagePairingWithASlowHomeAssistant(t *testing.T) {
	withClientRuntimeAvailability(t, map[string]bool{"codex": true})
	setupGoldenHome(t)
	ha := newFakeSignInHA(t)
	ha.slowIngress = 60 * time.Millisecond
	pairs := stubSetupPageHome(t, ha.server.URL, true)
	previousStep, previousPair, previousArmed := haSignInStepTimeout, haSignInPairTimeout, pairDeviceV2ArmedForSetup
	t.Cleanup(func() {
		haSignInStepTimeout, haSignInPairTimeout, pairDeviceV2ArmedForSetup = previousStep, previousPair, previousArmed
	})
	// Scaled: the old 30 s bound, the exchange's own budget (arm + slot),
	// and 15–30 s per answer. The first try needs ten answers, the second
	// three.
	haSignInStepTimeout, haSignInPairTimeout = 100*time.Millisecond, 300*time.Millisecond
	attempts := 0
	pairDeviceV2ArmedForSetup = func(ctx context.Context, ingress *CloudIngressClient, _ deviceMetadata) (*provisionedCredential, error) {
		attempts++
		calls := 3
		if attempts == 1 {
			calls = 10
		}
		for range calls {
			var info cloudPairingV2Info
			if err := cloudPairingCall(ctx, ingress, CloudEndpointPairInfo, nil, &info); err != nil {
				return nil, &pairingBeforeFinishError{err: err}
			}
		}
		return &provisionedCredential{}, nil
	}
	signInPairForSetup = func(_ string, provision pairingProvisioner, _ *runtimeConfig, _ func(*runtimeConfig) error, _ pairingClientInfo, _ SecretStoreUIPolicy) (string, error) {
		*pairs++
		if _, err := provision(deviceMetadata{}); err != nil {
			return "", err
		}
		return "device-1", nil
	}
	previousIdle := setupPageIdle
	setupPageIdle = 2 * time.Second // ends the run in step 3
	t.Cleanup(func() { setupPageIdle = previousIdle })
	page, stdout, done := startSetupPageFlow(t, "")
	page.waitFor("the found screen", accepting("signin"))
	page.click("signin", "")
	waiting := page.waitFor("the sign-in URL", func(s setupPageState) bool { return s.SignInURL != "" })
	authorize, _ := url.Parse(waiting.SignInURL)
	page.do(http.MethodGet, oauthLoopbackCallbackPath+"?"+signedInQuery(authorize.Query().Get("state")), "", nil)
	back := page.waitFor("the pairing problem", func(s setupPageState) bool { return s.Problem != nil && len(s.Accepts) > 0 })
	if back.Screen != "connect" || back.Problem.Key != "setup.page.problem.pairing_slow" || back.Fix != "retry" ||
		!slices.Contains(back.Accepts, "signin") || !strings.Contains(stdout.String(), "Home Assistant answered too slowly") {
		t.Fatalf("back = %+v\n%s", back, stdout)
	}
	page.click("signin", "") // Try again
	control := page.waitFor("step 3", accepting("finish"))
	if control.SignInURL != "" || pageRows(control) != "signed_in=ok connect=ok teach=ok" {
		t.Fatalf("after Try again: %+v rows %s", control, pageRows(control))
	}
	setupPageExit(t, done, stdout)
	ha.mu.Lock()
	signIns := 0
	for _, event := range ha.events {
		if event == "exchange" {
			signIns++
		}
	}
	ha.mu.Unlock()
	if attempts != 2 || *pairs != 2 || signIns != 1 {
		t.Fatalf("attempts=%d pairs=%d sign-ins=%d", attempts, *pairs, signIns)
	}
}

// Try again reuses the sign-in only for a failure before the finish request
// (a finish may have left a pending device), never after a not-owner answer
// or a session Home Assistant ended.
func TestSetupPagePairingProblemSaysWhyAndWhenToSignInAgain(t *testing.T) {
	before := func(err error) error { return &haSignInPairingError{err: &pairingBeforeFinishError{err: err}} }
	for name, test := range map[string]struct {
		cause error
		key   string
		reuse bool
	}{
		"slow":           {before(newCloudError(CloudErrTimeout, "x", context.DeadlineExceeded)), "setup.page.problem.pairing_slow", true},
		"owner check":    {before(&CloudError{Code: CloudErrHAProtocol, cause: errSignInOwnerCheckSlow}), "setup.page.problem.pairing_slow", true},
		"not ready":      {before(errSignInRelayNotReady), "setup.page.problem.app_not_ready", true},
		"App stopped":    {before(newCloudError(CloudErrIngressUnavailable, "x", nil)), "setup.page.problem.app_not_ready", true},
		"App too old":    {before(errSignInRelayUnsupported), "setup.page.problem.app_old", true},
		"not owner":      {before(errSignInNotOwner), "setup.page.problem.pairing", false},
		"expired":        {before(newCloudError(CloudErrOuterSessionExpired, "x", nil)), "setup.page.problem.signin_expired", false},
		"unknown":        {&haSignInPairingError{err: newCloudError(CloudErrOutcomeUnknown, "x", context.DeadlineExceeded)}, "setup.page.problem.pairing_unknown", false},
		"unknown later":  {&haSignInPairingError{err: fmt.Errorf("%w; a later finish response was: %v", errPairingOutcomeUnknown, errors.New("401"))}, "setup.page.problem.pairing_unknown", false},
		"after finish":   {&haSignInPairingError{err: newCloudError(CloudErrRelayInstance, "validate sign-in pairing credential", nil)}, "setup.page.problem.pairing", false},
		"slow at finish": {&haSignInPairingError{err: newCloudError(CloudErrTimeout, "x", context.DeadlineExceeded)}, "setup.page.problem.slow", false},
	} {
		problem, reuse := pagePairingProblem(&setupPagePairingError{cause: test.cause})
		if problem.Key != test.key || (reuse != nil) != test.reuse {
			t.Errorf("%s: %s reuse=%v", name, problem.Key, reuse != nil)
		}
	}
}

// After a pairing that didn't finish, an App that no longer offers sign-in:
// Try again stays and reads the App again (never a code prompt the page
// doesn't show); once sign-in is back, it reuses the sign-in. With the
// reuse window over, it signs in again.
func TestSetupPagePairingRetryChecksTheAppAgain(t *testing.T) {
	for _, expired := range []bool{false, true} {
		withClientRuntimeAvailability(t, map[string]bool{"codex": true})
		setupGoldenHome(t)
		ha := newFakeSignInHA(t)
		stubSetupPageHome(t, ha.server.URL, true)
		var advertises atomic.Bool
		advertises.Store(true)
		relayAdvertisesSignInForSetup = func(string) bool { return advertises.Load() }
		var tries atomic.Int32
		signInPairForSetup = func(string, pairingProvisioner, *runtimeConfig, func(*runtimeConfig) error, pairingClientInfo, SecretStoreUIPolicy) (string, error) {
			if tries.Add(1) > 1 {
				return "device-1", nil
			}
			advertises.Store(false) // the App restarts meanwhile
			return "", &haSignInPairingError{err: &pairingBeforeFinishError{err: newCloudError(CloudErrTimeout, "x", context.DeadlineExceeded)}}
		}
		previousIdle, previousRetry := setupPageIdle, setupPageRetryFor
		setupPageIdle = 2 * time.Second
		if expired {
			setupPageRetryFor = 0
		}
		page, stdout, done := startSetupPageFlow(t, "")
		page.waitFor("the found screen", accepting("signin"))
		page.click("signin", "")
		waiting := page.waitFor("the sign-in URL", func(s setupPageState) bool { return s.SignInURL != "" })
		authorize, _ := url.Parse(waiting.SignInURL)
		page.do(http.MethodGet, oauthLoopbackCallbackPath+"?"+signedInQuery(authorize.Query().Get("state")), "", nil)
		back := page.waitFor("the pairing problem", func(s setupPageState) bool { return s.Problem != nil && len(s.Accepts) > 0 })
		if back.Problem.Key != "setup.page.problem.pairing_slow" || back.Fix != "retry" || back.Primary != "" ||
			!slices.Contains(back.Accepts, "signin") || page.ui.inTerminal() {
			t.Fatalf("back = %+v", back)
		}
		page.click("signin", "") // the App still doesn't offer sign-in: same step 2
		again := page.waitFor("step 2 again", func(s setupPageState) bool { return slices.Contains(s.Accepts, "signin") })
		if again.Problem == nil || again.Problem.Key != "setup.page.problem.pairing_slow" || again.SignInURL != "" || tries.Load() != 1 {
			t.Fatalf("again = %+v tries=%d", again, tries.Load())
		}
		advertises.Store(true)
		page.click("signin", "")
		if expired {
			page.waitFor("a new sign-in", func(s setupPageState) bool { return s.SignInURL != "" })
			page.click("stop", "")
		} else if control := page.waitFor("step 3", accepting("finish")); control.SignInURL != "" || tries.Load() != 2 {
			t.Fatalf("control = %+v tries=%d", control, tries.Load())
		}
		setupPageExit(t, done, stdout)
		setupPageIdle, setupPageRetryFor = previousIdle, previousRetry
	}
}

// The browser opens a private file, not the token URL: other users can read
// a process's arguments, not this file.
func TestSetupPageLauncherKeepsTheTokenOffTheCommandLine(t *testing.T) {
	launch := "http://127.0.0.1:1/s/" + strings.Repeat("A", 43) + `"><script>`
	file, remove, err := writeSetupPageLauncher(launch, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm() != 0o600 || strings.Contains(file, strings.Repeat("A", 43)) {
		t.Fatalf("launcher %s: %v %v", file, info.Mode(), err)
	}
	if dir, err := os.Stat(filepath.Dir(file)); err != nil || dir.Mode().Perm() != 0o700 {
		t.Fatalf("launcher dir: %v %v", dir.Mode(), err)
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), `url=http://127.0.0.1:1/s/`+strings.Repeat("A", 43)+`&#34;&gt;&lt;script&gt;"`) {
		t.Fatalf("launcher = %s", data)
	}
	remove()
	if _, err := os.Stat(filepath.Dir(file)); !os.IsNotExist(err) {
		t.Fatalf("launcher dir left behind: %v", err)
	}
}
