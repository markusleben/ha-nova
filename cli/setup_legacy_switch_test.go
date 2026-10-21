package main

import (
	"bufio"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A re-run on the old shared access: the page offers the switch first.
// Not now keeps today's terminal status screen, which names the switch too.
func TestSetupPageLegacyInstallOffersTheSwitchFirst(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, errors.New("no chat apps"))
	stubLegacySwitchProbe(t, true)
	page, stdout, done := startSetupPageFlowWith(t, "", &setupFlow{paths: paths, cfg: cfg, state: state, lifecycleMarker: marker,
		connectionMode: setupConnectionLocal, pairingBackStep: setupStepRelayInstall})

	offer := page.waitFor("the switch offer", accepting("switch"))
	if offer.Screen != "rerun_switch" || len(offer.Rows) != 3 || offer.Rows[2].ID != "computer" || offer.Rows[2].State != "todo" ||
		offer.Rows[2].Msg.Key != "setup.page.rerun.computer_legacy" || !accepting("terminal")(offer) || offer.Problem != nil {
		t.Fatalf("offer = %+v", offer)
	}
	page.click("terminal", "")
	if code := setupPageExit(t, done, stdout); code != 0 || !page.ui.inTerminal() || !strings.Contains(stdout.String(), "old shared access") {
		t.Fatalf("exit %d inTerminal=%v\n%s", code, page.ui.inTerminal(), stdout)
	}
}

// A standalone container can't pair yet: no offer, no notice; as before.
func TestSetupPageLegacyInstallWithoutPairingKeepsTheTerminal(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, errors.New("no chat apps"))
	stubLegacySwitchProbe(t, false)
	page, stdout, done := startSetupPageFlowWith(t, "", &setupFlow{paths: paths, cfg: cfg, state: state, lifecycleMarker: marker,
		connectionMode: setupConnectionLocal, pairingBackStep: setupStepRelayInstall})
	if code := setupPageExit(t, done, stdout); code != 0 || !page.ui.inTerminal() || strings.Contains(stdout.String(), "shared access") {
		t.Fatalf("exit %d inTerminal=%v\n%s", code, page.ui.inTerminal(), stdout)
	}
}

// Switch to your own access: setup's usual Home Assistant sign-in pairs
// this computer, and the run ends on the page like a first setup.
func TestSetupPageLegacySwitchSignsInAndPairs(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	withClientRuntimeAvailability(t, map[string]bool{"claude": true, "codex": true}) // Codex CLI found since: not set up without its click
	t.Setenv("HA_NOVA_DEV_ROOT", repoRootForSetupTest(t))
	t.Setenv("PATH", installClaudeMock(t, paths.Home, filepath.Join(t.TempDir(), "claude.log"))+string(os.PathListSeparator)+os.Getenv("PATH"))
	ha := newFakeSignInHA(t)
	stubRerunApp(t, true, chatAppsHA{}, errors.New("no chat apps"))
	pairs := stubSetupPageHome(t, ha.server.URL, true)
	cfg.HAHost, cfg.HAURL = normalizeHostInput(ha.server.URL), ha.server.URL
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	page, stdout, done := startSetupPageFlowWith(t, "", &setupFlow{paths: paths, cfg: cfg, state: state, lifecycleMarker: marker,
		connectionMode: setupConnectionLocal, pairingBackStep: setupStepRelayInstall})

	page.click("switch", "")
	page.click("signin", "")
	waiting := page.waitFor("the sign-in URL", func(s setupPageState) bool { return s.SignInURL != "" })
	authorize, err := url.Parse(waiting.SignInURL)
	if err != nil {
		t.Fatal(err)
	}
	callback := authorize.Query().Get("redirect_uri") + "?" + signedInQuery(authorize.Query().Get("state"))
	page.do("GET", strings.TrimPrefix(callback, page.base), "", nil)
	page.click("finish", "")
	page.click("done", "")
	if code := setupPageExit(t, done, stdout); code != 0 || *pairs != 1 || page.ui.inTerminal() || page.ui.snapshot().Screen != "closed" {
		t.Fatalf("exit %d pairs %d inTerminal=%v screen %s\n%s", code, *pairs, page.ui.inTerminal(), page.ui.snapshot().Screen, stdout)
	}
	if installed := loadStateOrDefault(paths).InstalledClients; slices.Contains(installed, "codex") || !slices.Contains(installed, "claude") {
		t.Fatalf("installed = %v, want only the AI assistant set up before", installed)
	}
}

// An App without the Home Assistant sign-in (before 0.26): the page can't
// switch, so the terminal asks (here nobody to ask: the banner names it).
func TestSetupPageLegacyInstallWithoutSignInUsesTheTerminal(t *testing.T) {
	paths, cfg, state, marker := setupCompletedInstallFixture(t)
	stubRerunApp(t, true, chatAppsHA{}, errors.New("no chat apps"))
	relayAdvertisesSignInForSetup = func(string) bool { return false }
	stubLegacySwitchProbe(t, true)
	page, stdout, done := startSetupPageFlowWith(t, "", &setupFlow{paths: paths, cfg: cfg, state: state, lifecycleMarker: marker,
		connectionMode: setupConnectionLocal, pairingBackStep: setupStepRelayInstall})
	if code := setupPageExit(t, done, stdout); code != 0 || !page.ui.inTerminal() || !strings.Contains(stdout.String(), "old shared access") {
		t.Fatalf("exit %d inTerminal=%v\n%s", code, page.ui.inTerminal(), stdout)
	}
}

func stubLegacySwitchProbe(t *testing.T, canPair bool) {
	t.Helper()
	previous := probePairingV1ForSetup
	t.Cleanup(func() { probePairingV1ForSetup = previous })
	probePairingV1ForSetup = func(string) bool { return canPair }
}

// In the terminal: the notice, then the question; Enter switches (setup's
// pairing step), Not now goes on as before. Nobody to ask: the notice only.
func TestSetupTerminalLegacyInstallAsksToSwitchFirst(t *testing.T) {
	resetServerProfileSelection(t)
	stubLegacySwitchProbe(t, true)
	run := func(input string, tty bool) (*setupFlow, setupStepResult, bool, string) {
		var out strings.Builder
		ui := setupTextUI{reader: bufio.NewReader(strings.NewReader(input)), out: &out}
		flow := &setupFlow{existingToken: "shared", ui: ui,
			cfg: runtimeConfig{HAHost: "ha.local", HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791"}}
		if tty {
			flow.ui = ttySetupTextUI{ui}
		}
		result, switched := flow.offerLegacySwitch(false)
		return flow, result, switched, out.String()
	}
	flow, result, switched, out := run("\n", true)
	if !switched || result.done || result.next != setupStepPairing || flow.existingToken != "" ||
		!strings.Contains(out, "old shared access") || !strings.Contains(out, "Switch to your own access now?") {
		t.Fatalf("switch: %+v %v token=%q\n%s", result, switched, flow.existingToken, out)
	}
	flow, _, switched, _ = run("2\n", true)
	if switched || flow.existingToken != "shared" {
		t.Fatalf("not now switched=%v token=%q", switched, flow.existingToken)
	}
	flow, _, switched, out = run("", false)
	if switched || flow.existingToken != "shared" || strings.Contains(out, "Switch to your own access now?") {
		t.Fatalf("no tty switched=%v\n%s", switched, out)
	}
}

type ttySetupTextUI struct{ setupTextUI }

func (ttySetupTextUI) interactive() bool { return true }
