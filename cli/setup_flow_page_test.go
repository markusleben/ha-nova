package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
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

func (p *setupPageTest) click(action, value string) {
	p.t.Helper()
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
		setupInstalledSignIn = nil
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
	verifyDeviceHealth = func(runtimeConfig) bool { return true }
	setupPageLinger = 0
	return &pairs
}

func startSetupPageFlow(t *testing.T, stdin string) (*setupPageTest, *bytes.Buffer, chan int) {
	t.Helper()
	paths, err := detectPaths()
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	ui := newSetupWebUI(strings.NewReader(stdin), &stdout)
	server, err := startSetupPageServer(ui)
	if err != nil {
		t.Fatal(err)
	}
	page := &setupPageTest{t: t, ui: ui, server: server, base: "http://127.0.0.1:" + server.port}
	flow := &setupFlow{paths: paths, state: loadStateOrDefault(paths), connectionMode: setupConnectionLocal, pairingBackStep: setupStepRelayInstall}
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
		found.Primary != "signin" || !slices.Equal(found.Clients, []string{"Codex CLI"}) {
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

	// Step 3: no phone to read, no Claude Code, no Link here; then step 4.
	control := page.waitFor("step 3", accepting("finish"))
	if control.Screen != "control" || control.Control == nil || control.Control.Phone != "unavailable" ||
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
	if teach := state.Rows[4].Msg; teach.Key != "setup.page.row.teach" || !slices.Equal(teach.Args["clients"].([]string), []string{"Claude Code", "Codex CLI"}) {
		t.Fatalf("teach row = %+v", teach)
	}
	if !slices.Contains(supervisor.writes(), "post /addons/"+HAOfficialNOVAAppSlug+"/start") {
		t.Fatalf("writes = %v", supervisor.writes())
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
	if code != 0 || end.Screen != "stopped" || !page.ui.inTerminal() || !strings.Contains(stdout.String(), "one-time six-digit code") {
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
	if found.App != "missing" {
		t.Fatalf("found = %+v", found)
	}
	setupPageExit(t, done, stdout)
}

// The page is for a plain first run at a desktop; everything else keeps the
// text wizard, and go test never gets the page.
func TestSetupPageOnlyForAPlainFirstRun(t *testing.T) {
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
	for name, change := range map[string]func(*setupFlow){
		"client named":  func(f *setupFlow) { f.target = "claude" },
		"service":       func(f *setupFlow) { f.serviceMode = true },
		"host flag":     func(f *setupFlow) { f.hostFlag = "ha.local" },
		"relay token":   func(f *setupFlow) { f.relayTokenFlag = "x" },
		"saved address": func(f *setupFlow) { f.cfg.HAURL = "http://ha.local:8123" },
		"saved relay":   func(f *setupFlow) { f.cfg.RelayBaseURL = "http://ha.local:8791" },
		"Link saved":    func(f *setupFlow) { f.cfg.Cloud = &cloudLifecycleMetadata{} },
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
	back := page.waitFor("the pairing problem", func(s setupPageState) bool { return s.Problem != nil })
	if got := pageRows(back); strings.Contains(got, "=run") {
		t.Fatalf("rows still running after the failure: %s", got)
	}
	if back.Screen != "connect" || back.Problem.Key != "setup.page.problem.pairing" || back.Fix != "retry" ||
		!slices.Contains(back.Accepts, "signin") || page.ui.inTerminal() || setupInstalledSignIn != nil {
		t.Fatalf("back = %+v inTerminal=%v", back, page.ui.inTerminal())
	}
	setupPageExit(t, done, stdout)
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
