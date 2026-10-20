package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// A re-run on the setup page (G1, spec §4): what is set up, step 3's rows on
// the same screen, Done ends it; nothing changes without a click.

// stubRerunApp: the NOVA App answers (or not) for the page's App row, and
// the chat-app read says what Home Assistant holds.
func stubRerunApp(t *testing.T, running bool, chat chatAppsHA, chatErr error) {
	t.Helper()
	previousProbe, previousAdvertises, previousChat, previousOrigin, previousLinger :=
		probeInstalledRelayForSetup, relayAdvertisesSignInForSetup, chatAppsReadHA, setupLinkRemoteOrigin, setupPageLinger
	t.Cleanup(func() {
		probeInstalledRelayForSetup, relayAdvertisesSignInForSetup, chatAppsReadHA, setupLinkRemoteOrigin, setupPageLinger =
			previousProbe, previousAdvertises, previousChat, previousOrigin, previousLinger
	})
	probeInstalledRelayForSetup = func(string) (bool, string) { return running, "0.11.0" }
	relayAdvertisesSignInForSetup = func(string) bool { return running }
	chatAppsReadHA = func(runtimePaths) (chatAppsHA, error) { return chat, chatErr }
	setupLinkRemoteOrigin = func(runtimeConfig) string { return "" }
	setupPageLinger = 0
}

// rerunFlow: the fixture's install as one paired before (a secure address
// saved); its shared token alone would keep the terminal.
func rerunFlow(paths runtimePaths, cfg runtimeConfig, state installState, marker [][]byte) *setupFlow {
	cfg.RelaySecureBaseURL = "https://127.0.0.1:9"
	return &setupFlow{paths: paths, cfg: cfg, state: state, lifecycleMarker: marker,
		connectionMode: setupConnectionLocal, pairingBackStep: setupStepRelayInstall}
}

// codexAttachedForReal: Claude Code as the fixture fakes it, Codex CLI from
// the files setup installs.
func codexAttachedForReal(t *testing.T) {
	t.Helper()
	previous := clientAttachmentPresentForStatus
	t.Cleanup(func() { clientAttachmentPresentForStatus = previous })
	clientAttachmentPresentForStatus = func(paths runtimePaths, state installState, client string) bool {
		if client == "codex" {
			return clientAttachmentPresent(paths, state, client)
		}
		return previous(paths, state, client)
	}
}

func rowStates(rows []setupPageRow) map[string]string {
	states := map[string]string{}
	for _, row := range rows {
		states[row.ID] = row.State
	}
	return states
}

// A working setup: one screen with what is set up and step 3's rows; Done
// with every switch off changes nothing and ends setup.
func TestSetupPageRerunShowsWhatIsSetUpAndEndsOnDone(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{version: "2026.10.1", appVersion: "0.11.0", present: true}, nil)
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))

	rerun := page.waitFor("the re-run screen", accepting("finish"))
	if rerun.Screen != "rerun" || rerun.Control == nil || !slices.Contains(rerun.Accepts, "server") {
		t.Fatalf("re-run = %+v", rerun)
	}
	if want := map[string]string{"ha": "ok", "app": "ok", "computer": "ok", "clients": "ok"}; !mapsEqual(rowStates(rerun.Rows), want) {
		t.Fatalf("rows = %+v", rerun.Rows)
	}
	if rerun.Rows[0].Msg.Args["host"] != "192.168.1.5" || rerun.Rows[1].Msg.Key != "setup.page.app.running_version" ||
		!slices.Equal(rerun.Rows[3].Msg.Args["clients"].([]any), []any{"Claude Code"}) {
		t.Fatalf("rows = %+v %+v %+v", rerun.Rows[0].Msg, rerun.Rows[1].Msg, rerun.Rows[3].Msg)
	}
	ctl := rerun.Control
	if ctl.Phone == "unavailable" || ctl.Rule != "offer" || ctl.Chat == nil || ctl.Chat.State != "page_on" || !strings.HasPrefix(ctl.Chat.Page, cfg.HAURL) {
		t.Fatalf("control = %+v chat %+v", ctl, ctl.Chat)
	}
	settings := claudeUserSettingsPath(paths.Home)
	before, _ := os.ReadFile(settings)
	page.click("finish", "")
	code := setupPageExit(t, done, stdout)
	if end := page.ui.snapshot(); code != 0 || end.Screen != "saved" {
		t.Fatalf("exit %d screen %s\n%s", code, end.Screen, stdout)
	}
	if after, _ := os.ReadFile(settings); string(after) != string(before) {
		t.Fatalf("Done with the switch off changed Claude Code's settings:\n%s", after)
	}
	if installLifecycleStopped(paths) {
		t.Fatal("the re-run didn't clear its matching lifecycle marker")
	}
	if !strings.Contains(stdout.String(), "Everything is already set up!") || !strings.Contains(stdout.String(), setupT("setup.page.terminal.rerun")) {
		t.Fatalf("terminal:\n%s", stdout)
	}
}

// "Add another Home Assistant server" on the re-run screen continues in the
// terminal with the add flow, as the terminal's completed-setup screen does.
func TestSetupPageRerunAddsAnotherServerInTheTerminal(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, errors.New("no chat apps"))
	page, stdout, done := startSetupPageFlowWith(t, "exit\n", rerunFlow(paths, cfg, state, marker))
	if rerun := page.waitFor("the re-run screen", accepting("server")); rerun.Control.Chat != nil {
		t.Fatalf("an App without chat apps got a row: %+v", rerun.Control.Chat)
	}
	page.click("server", "")
	code := setupPageExit(t, done, stdout)
	if code != 0 || !page.ui.inTerminal() || !strings.Contains(stdout.String(), "new server") {
		t.Fatalf("exit %d inTerminal=%v\n%s", code, page.ui.inTerminal(), stdout)
	}
}

// The NOVA App doesn't answer: the screen says so and offers Check again,
// the App page, and Fix it (step 1: another address, or the sign-in that
// installs or starts the App).
func TestSetupPageRerunWhileTheAppDoesNotAnswer(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	cfg.RelayBaseURL = dead.URL
	stubRerunApp(t, false, chatAppsHA{}, errors.New("unreachable"))
	previousIdle := setupPageIdle
	setupPageIdle = 2 * time.Second
	t.Cleanup(func() { setupPageIdle = previousIdle })
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))

	broken := page.waitFor("the re-run screen", accepting("recheck"))
	if broken.Screen != "rerun_fix" || broken.Control != nil || !slices.Contains(broken.Accepts, "fix") ||
		broken.Problem == nil || broken.Problem.Key != "setup.page.rerun.problem_app" || broken.Links["app"] != haInstallRelayAppURL(cfg.HAURL) {
		t.Fatalf("broken = %+v", broken)
	}
	if states := rowStates(broken.Rows); states["app"] != "fail" || states["computer"] != "fail" || states["ha"] != "todo" {
		t.Fatalf("rows = %+v", broken.Rows)
	}
	page.click("recheck", "")
	page.waitFor("the re-run screen again", accepting("recheck"))
	if code := setupPageExit(t, done, stdout); code != 0 || page.ui.snapshot().Screen != "idle" {
		t.Fatalf("exit %d screen %s\n%s", code, page.ui.snapshot().Screen, stdout)
	}
}

// Only the skills set up before are out of date (after an update, lane
// W3): setup refreshes them without a question, then shows the re-run screen.
func TestSetupPageRerunRefreshesTheSkillsWithoutAsking(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	withClientRuntimeAvailability(t, map[string]bool{"claude": false, "codex": true})
	codexAttachedForReal(t)
	mergeStateClients(&state, []string{"codex"}) // set up before; its files are gone
	if err := saveState(paths, state); err != nil {
		t.Fatal(err)
	}
	stubRerunApp(t, true, chatAppsHA{}, errors.New("no chat apps"))
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	ha := setupGoldenHAServer(t) // the token path checks Home Assistant itself
	cfg.HAHost, cfg.HAURL = normalizeHostInput(ha.URL), ha.URL
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))
	page.waitFor("the skills refresh", func(s setupPageState) bool { return s.Screen == "repair" })
	rerun := page.waitFor("the re-run screen", accepting("finish"))
	if rerun.Screen != "rerun" || rowStates(rerun.Rows)["clients"] != "ok" || !strings.Contains(stdout.String(), "Installing") {
		t.Fatalf("re-run = %+v\n%s", rerun, stdout)
	}
	page.click("finish", "")
	if code := setupPageExit(t, done, stdout); code != 0 || page.ui.snapshot().Screen != "saved" {
		t.Fatalf("exit %d screen %s\n%s", code, page.ui.snapshot().Screen, stdout)
	}
}

// A computer removed on the NOVA page: Fix it leads to step 1, which says
// why and signs in again (never the terminal's code prompt).
func TestSetupPageRerunRemovedComputerSignsInAgain(t *testing.T) {
	previous := verifyDeviceHealth
	t.Cleanup(func() { verifyDeviceHealth = previous })
	verifyDeviceHealth = func(runtimeConfig) deviceHealth { return deviceRevoked }
	home := t.TempDir()
	paths := runtimePaths{Home: home, ConfigDir: filepath.Join(home, "ha-nova")}
	ui := newSetupWebUI(strings.NewReader(""), &strings.Builder{})
	flow := &setupFlow{paths: paths, ui: ui, page: ui, devicePaired: true, deviceAlreadyPaired: true,
		cfg: runtimeConfig{HAHost: "ha.local", HAURL: "http://ha.local:8123"}}
	if next := flow.verifyDevice(); next.done || next.next != setupStepHost || !flow.pageRemoved || flow.deviceAlreadyPaired || flow.devicePaired {
		t.Fatalf("next %+v removed=%v paired=%v/%v", next, flow.pageRemoved, flow.deviceAlreadyPaired, flow.devicePaired)
	}
	if ui.inTerminal() {
		t.Fatal("a removed computer handed setup to the terminal")
	}
}

// An AI assistant found since the last setup is offered with a button, never
// set up without that click.
func TestSetupPageRerunOffersANewAIAssistant(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	withClientRuntimeAvailability(t, map[string]bool{"claude": true, "codex": true})
	codexAttachedForReal(t)
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	stubRerunApp(t, true, chatAppsHA{}, errors.New("no chat apps"))
	page, stdout, done := startSetupPageFlowWith(t, "", rerunFlow(paths, cfg, state, marker))
	rerun := page.waitFor("the re-run screen", accepting("clients"))
	if rerun.Screen != "rerun" || !slices.Equal(rerun.NewClients, []string{"Codex CLI"}) || rowStates(rerun.Rows)["clients"] != "ok" ||
		!slices.Equal(rerun.Rows[3].Msg.Args["clients"].([]any), []any{"Claude Code"}) {
		t.Fatalf("re-run = %+v", rerun)
	}
	if slices.Contains(loadStateOrDefault(paths).InstalledClients, "codex") {
		t.Fatal("Codex CLI was set up before the click")
	}
	page.click("clients", "")
	again := page.waitFor("the re-run screen again", func(s setupPageState) bool {
		return s.Screen == "rerun" && len(s.NewClients) == 0 && slices.Contains(s.Accepts, "finish")
	})
	if !slices.Contains(loadStateOrDefault(paths).InstalledClients, "codex") || slices.Contains(again.Accepts, "clients") ||
		!slices.Equal(again.Rows[3].Msg.Args["clients"].([]any), []any{"Claude Code", "Codex CLI"}) || rowStates(again.Rows)["clients"] != "ok" {
		t.Fatalf("after the click: %+v state %+v", again, loadStateOrDefault(paths).InstalledClients)
	}
	page.click("finish", "")
	if code := setupPageExit(t, done, stdout); code != 0 || page.ui.snapshot().Screen != "saved" {
		t.Fatalf("exit %d screen %s\n%s", code, page.ui.snapshot().Screen, stdout)
	}
}

// The shared-token install keeps the terminal: its switch to this
// computer's own credential is a terminal step.
func TestSetupPageRerunHandsASharedTokenInstallToTheTerminal(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, errors.New("no chat apps"))
	page, stdout, done := startSetupPageFlowWith(t, "", &setupFlow{paths: paths, cfg: cfg, state: state, lifecycleMarker: marker,
		connectionMode: setupConnectionLocal, pairingBackStep: setupStepRelayInstall})
	code := setupPageExit(t, done, stdout)
	if code != 0 || !page.ui.inTerminal() || !strings.Contains(stdout.String(), "legacy token") {
		t.Fatalf("exit %d inTerminal=%v\n%s", code, page.ui.inTerminal(), stdout)
	}
}
