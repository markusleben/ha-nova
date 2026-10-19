package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// Steps 1 and 2 of the setup page (docs/work/2026-10-18-onboarding-0.27-spec.md
// §4): what setup found, then the owner's sign-in in the same tab, which also
// installs, updates, or starts the NOVA App, and hands its session to the
// pairing step. "Use a six-digit code" and everything else the page does not
// cover yet continue in the terminal (setupWebUI hand-over).

// Test seams.
var (
	setupPageTry              = func(paths runtimePaths) int { return runTryCommand(paths, nil) }
	openHASignInInTabForSetup = openHASignInIngressIn
	setupPageAvailable        = defaultSetupPageAvailable
)

// setupNoBrowser: --no-browser; HA_NOVA_NO_BROWSER=1 does the same.
var setupNoBrowser bool

// setupPairWithCode: the page's "Use a six-digit code" hands the pairing
// step a code prompt without offering the sign-in again. Taken once.
var setupPairWithCode bool

// defaultSetupPageAvailable: a person sits at this computer's desktop with a
// terminal. SSH, a Linux session without a display, Windows session 0, a
// pipe, and go test keep the text path.
func defaultSetupPageAvailable() bool {
	if setupNoBrowser || testing.Testing() || os.Getenv("HA_NOVA_NO_BROWSER") == "1" ||
		!isInteractiveTTY() || !stdoutIsInteractiveTTY() {
		return false
	}
	for _, name := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		if strings.TrimSpace(os.Getenv(name)) != "" {
			return false
		}
	}
	switch runtime.GOOS {
	case "linux":
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("WSL_DISTRO_NAME") != ""
	case "darwin", "windows":
		return windowAvailable()
	}
	return false
}

// usesSetupPage: the page covers a plain first run. Re-runs, repairs, flags,
// service installs, named profiles, and saved Home Assistant Link state keep
// the text wizard.
func (f *setupFlow) usesSetupPage() bool {
	return f.target == "" && !f.serviceMode &&
		f.hostFlag == "" && f.haURLFlag == "" && f.relayURLFlag == "" && f.relayTokenFlag == "" &&
		f.cfg.HAHost == "" && f.cfg.HAURL == "" && f.cfg.RelayBaseURL == "" && f.cfg.RelaySecureBaseURL == "" &&
		f.cfg.Cloud == nil && activeServerProfile() == defaultServerProfileName && setupPageAvailable()
}

// runOnPage serves the page, opens it, and runs the engine with it. Without a
// server it runs in the terminal.
func (f *setupFlow) runOnPage() int {
	page := newSetupWebUI(os.Stdin, os.Stdout)
	server, err := startSetupPageServer(page)
	if err != nil {
		f.ui = setupTextUI{reader: bufio.NewReader(os.Stdin), out: os.Stdout}
		return f.run()
	}
	fmt.Fprintf(os.Stdout, "\n  %s\n  %s\n\n", setupT("setup.page.terminal.opening"), setupT("setup.page.terminal.link", "url", server.launchURL()))
	remove, err := openSetupPage(server.launchURL())
	defer remove()
	if err != nil { // no browser to open: the terminal it is
		server.close()
		f.ui = setupTextUI{reader: bufio.NewReader(os.Stdin), out: os.Stdout}
		return f.run()
	}
	return f.runWithPage(page)
}

// runWithPage runs the engine with a started page and closes the page at the
// end, once the open page could show how setup ended.
func (f *setupFlow) runWithPage(page *setupWebUI) int {
	defer page.server.close()
	f.ui, f.page, f.target = page, page, "all" // the page sets up every AI assistant it finds (spec §7.3)
	code := f.run()
	if !setupPageEnded(page.snapshot().Screen) {
		// Ended without an outcome (a terminal step finished it).
		end := "failed"
		if code == 0 {
			end = "finished"
		}
		page.end(end)
	}
	if page.server.opened.Load() {
		// Until the open page has shown how setup ended (a background tab
		// polls slowly), bounded.
		select {
		case <-page.server.shownEnd:
		case <-time.After(setupPageLinger):
		}
	}
	return code
}

var setupPageLinger = 15 * time.Second

func (f *setupFlow) stepPage() setupStepResult {
	p := f.page
	var candidates []setupDiscoveryCandidate
	if f.cfg.HAURL == "" { // back from a failed pairing, the address stays
		candidates = f.pageDiscover()
	}
	if len(candidates) == 1 {
		f.cfg = applySelectedSetupHost(f.cfg, candidates[0].Host, candidates[0].HAURL, f.relayURLFlag)
	}
	var found []setupPageCandidate
	if len(candidates) > 1 {
		for _, candidate := range candidates {
			found = append(found, setupPageCandidate{Host: terminalSafeName(candidate.Host), URL: candidate.HAURL})
		}
	}
	problem, screen, fix, askHA := f.pageProblem, "found", "", false
	if problem != nil {
		screen, fix, f.pageProblem = "connect", "retry", nil
	}
	links := map[string]string{}
	for {
		app := f.pageApp()
		p.update(func(v *setupPageState) {
			v.Screen, v.HA, v.Candidates, v.App, v.AppVersion, v.Primary = screen, terminalSafeName(f.cfg.HAHost), found, app.state, terminalSafeName(app.version), app.primary
			v.Clients, v.Problem, v.Fix, v.Links, v.AskHA, v.Typed = setupClientNames(f.selectedClients), problem, fix, links, askHA, f.pageTyped
			if screen == "found" {
				v.Rows = nil
			}
		})
		accepts := []string{"address", "terminal"}
		if app.primary != "" {
			accepts = append(accepts, "signin")
		}
		if f.cfg.HAURL == "" { // no Home Assistant found: the demo house (spec §8)
			accepts = append(accepts, "demo")
		}
		action, err := p.wait(accepts...)
		if err != nil {
			return f.cancelled()
		}
		problem, screen, fix, links, askHA = nil, "found", "", map[string]string{}, false
		switch action.Action {
		case "demo":
			if setupPageTry(f.paths) == 0 {
				p.end("demo")
				return setupDone(0)
			}
			problem, askHA = setupPageText("setup.page.problem.demo"), true
			continue
		case "address":
			host, haURL, ok := pageResolveAddress(action.Value)
			if !ok {
				problem, askHA = setupPageText("setup.page.problem.address", "address", terminalSafeName(action.Value)), true
				continue
			}
			f.cfg = applySelectedSetupHost(f.cfg, host, haURL, f.relayURLFlag)
			f.pageTyped = !slices.ContainsFunc(found, func(c setupPageCandidate) bool { return c.URL == action.Value })
			found = nil
		case "terminal":
			p.handover()
			// "Use a six-digit code": the terminal asks for the code, not
			// for a sign-in again.
			setupPairWithCode = app.primary != ""
			if f.cfg.HAURL == "" {
				return setupGoTo(setupStepHost)
			}
			f.detectRelayOnInstall = true
			return setupGoTo(setupStepRelayInstall)
		case "signin":
			failure, interrupted := f.pageSignIn(app)
			if interrupted {
				return f.cancelled()
			}
			if failure == nil {
				f.pairingFlow, f.pairingBackStep = true, setupStepRelayInstall
				return setupGoTo(setupStepPairing)
			}
			problem, fix, links, screen = failure.problem, failure.fix, failure.links, "connect"
			if failure.address {
				screen, askHA = "found", true
			}
		}
	}
}

// pageDiscover looks for Home Assistant. On Windows it skips the mDNS
// search, whose firewall prompt the page can't explain (spec §4.3); the
// system resolver finds homeassistant.local.
func (f *setupFlow) pageDiscover() []setupDiscoveryCandidate {
	if runtime.GOOS == "windows" {
		setupDiscoveryWithoutMDNS = true
		defer func() { setupDiscoveryWithoutMDNS = false }()
	}
	candidates, _ := discoverReachableHAHostsForSetup(f.cfg)
	return candidates
}

// pageWaitForClient waits until an AI assistant is installed (spec §4,
// step 2: "No AI app found on this computer").
func (f *setupFlow) pageWaitForClient() setupStepResult {
	for !hasAvailableSetupClientChoice(f.choices) {
		f.page.update(func(v *setupPageState) { v.Screen = "noclient" })
		if _, err := f.page.wait("recheck"); err != nil {
			return f.cancelled()
		}
		choices, err := buildSetupClientChoices(f.paths, f.state)
		if err != nil {
			return f.failErr(err)
		}
		f.choices = choices
	}
	return setupGoOn
}

func setupClientNames(clients []string) []string {
	names := make([]string, len(clients))
	for i, client := range clients {
		names[i] = setupClientLabel(client)
	}
	return names
}

func pageResolveAddress(input string) (string, string, bool) {
	host := normalizeHostInput(input)
	if host == "" {
		return "", "", false
	}
	resolved, err := resolveHAURLBaseForSetup(input)
	if err != nil {
		return "", "", false
	}
	return host, resolved, true
}

type setupPageApp struct {
	state   string // missing, old, running; "" while no address is known
	primary string // install, update, signin; "": the page can't sign in here
	version string
	minimum string
}

// pageApp: is the App running, current, and ready for sign-in pairing?
func (f *setupFlow) pageApp() setupPageApp {
	if f.cfg.HAURL == "" {
		return setupPageApp{}
	}
	_, originErr := parseLocalHAOrigin(f.cfg.HAURL)
	signIn := originErr == nil // the sign-in needs Home Assistant's own address
	installs := signIn && haAppInstallTargetsThisHA(f.cfg)
	running, version := probeInstalledRelayForSetup(f.cfg.RelayBaseURL)
	app := setupPageApp{state: "missing", version: version}
	if running {
		app.minimum = relayVersionBelowMinimum(f.paths, version)
		app.state = "running"
		if app.minimum != "" {
			app.state = "old"
		}
	}
	switch {
	case app.state == "running" && signIn && relayAdvertisesSignInForSetup(f.cfg.RelayBaseURL):
		app.primary = "signin"
	case app.state == "missing" && installs:
		app.primary = "install"
	case app.state == "old" && installs:
		app.primary = "update"
	}
	return app
}

type setupPageFailure struct {
	problem *setupPageMsg
	fix     string
	links   map[string]string
	address bool // the address is the problem: step 1 opens its field
}

// pageSignIn signs the owner in on the page's own tab; the sign-in also
// installs, updates, or starts the App. A nil failure leaves the session for
// the pairing step; interrupted means Ctrl+C in the terminal.
func (f *setupFlow) pageSignIn(app setupPageApp) (*setupPageFailure, bool) {
	p := f.page
	out := f.ui.termOut()
	origin, err := resolveHASignInOriginForSetup(context.Background(), f.cfg.HAURL)
	if err != nil {
		return pageSignInFailure(err, HACurrentUser{}, f.cfg.HAURL), false
	}
	install := app.primary != "signin"
	rows := []setupPageRow{{ID: "signed_in", State: "run", Msg: setupPageText("setup.page.row.signing_in")}}
	if install {
		add := setupPageText("setup.page.row.app_add")
		if app.primary == "update" {
			add = setupPageText("setup.page.row.app_update")
		}
		rows = append(rows,
			setupPageRow{ID: "app_add", State: "todo", Msg: add},
			setupPageRow{ID: "app_start", State: "todo", Msg: setupPageText("setup.page.row.app_start")})
	}
	rows = append(rows,
		setupPageRow{ID: "connect", State: "todo", Msg: setupPageText("setup.page.row.connect")},
		setupPageRow{ID: "teach", State: "todo", Msg: setupPageText("setup.page.row.teach", "clients", setupClientNames(f.selectedClients))})
	p.update(func(v *setupPageState) {
		v.Screen, v.Rows, v.Problem, v.Fix, v.Links, v.Accepts = "connect", rows, nil, "", nil, nil
	})

	// Closing the tab mid sign-in is no dead end: "/" works again in the same
	// browser (the session cookie stays).
	fmt.Fprintf(out, "  %s\n", setupT("setup.page.terminal.page", "url", "http://"+p.callbackHost()+"/"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stoppedOnPage := p.running(cancel)
	var state novaAppState
	prepare := func(ctx context.Context, socket haWSCaller, user HACurrentUser) error {
		signedIn := setupPageText("setup.page.row.signed_in_owner")
		if name := terminalSafeName(user.Name); name != "" {
			signedIn = setupPageText("setup.page.row.signed_in", "name", name)
		}
		p.setRow("signed_in", "ok", signedIn)
		if !install {
			return nil
		}
		supervisor := newNOVAAppSupervisor(socket)
		supervisor.phase = func(name string) {
			if name == "start" {
				p.setRow("app_add", "ok", nil)
				p.setRow("app_start", "run", nil)
				return
			}
			p.setRow("app_add", "run", nil)
		}
		if err := supervisor.ensure(ctx, out, &state, app.primary == "update", app.minimum); err != nil {
			return err
		}
		p.setRow("app_add", "ok", nil)
		p.setRow("app_start", "run", nil)
		if err := waitForNOVAAppSignIn(ctx, out, socket, f.cfg.RelayBaseURL); err != nil {
			return err
		}
		p.setRow("app_start", "ok", nil)
		return nil
	}
	session, err := openHASignInInTabForSetup(ctx, out, origin.String(), prepare, p)
	pageStop := stoppedOnPage()
	p.navigate("")
	if err == nil {
		session.fromPage = true
		setupInstalledSignIn = &session
		return nil, false
	}
	p.failRunningRows()
	if errors.Is(err, errSignInStopped) && !pageStop {
		return nil, true // Ctrl+C in the terminal
	}
	failure := pageSignInFailure(err, session.user, f.cfg.HAURL)
	if state.sourceListed || state.installed {
		failure.links["app"] = haInstallRelayAppURL(f.cfg.HAURL)
	}
	return failure, false
}

// pageSignInFailure: one plain line and the next step (spec §4, step 2).
func pageSignInFailure(err error, user HACurrentUser, haURL string) *setupPageFailure {
	var install *novaAppInstallError
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	failure := &setupPageFailure{fix: "retry", links: map[string]string{}}
	switch {
	case errors.Is(err, errSignInNotOwner):
		failure.fix = "owner"
		failure.problem = setupPageText("setup.page.problem.not_owner_unnamed")
		if name := terminalSafeName(user.Name); name != "" {
			failure.problem = setupPageText("setup.page.problem.not_owner", "name", name)
		}
	case errors.Is(err, errAppInstallNoSupervisor), errors.Is(err, errAppInstallUnsupported):
		failure.fix = ""
		failure.problem = setupPageText("setup.page.problem.no_apps")
		failure.links["guide"] = relayContainerGuideURL
	case errors.As(err, &install):
		failure.problem = setupPageText("setup.page.problem.install", "reason", install.line)
		failure.links["app"] = haInstallRelayAppURL(haURL)
	case errors.Is(err, errSignInStopped):
		failure.problem = setupPageText("setup.page.problem.stopped")
	case errors.Is(err, errSignInBrowser) && IsCloudErrorCode(err, CloudErrTimeout):
		failure.problem = setupPageText("setup.page.problem.timeout")
	case errors.Is(err, errSignInBrowser):
		failure.problem = setupPageText("setup.page.problem.canceled")
	case errors.Is(err, errSignInAppMissing), errors.Is(err, errSignInRelayNotReady), errors.Is(err, errSignInRelayUnsupported):
		failure.problem = setupPageText("setup.page.problem.app_not_ready")
	case errors.As(err, &certificate), errors.As(err, &authority):
		failure.problem, failure.address = setupPageText("setup.page.problem.certificate"), true
	case IsCloudErrorCode(err, CloudErrRedirectRejected):
		failure.problem, failure.address = setupPageText("setup.page.problem.redirect"), true
	case IsCloudErrorCode(err, CloudErrNetwork), IsCloudErrorCode(err, CloudErrTimeout), IsCloudErrorCode(err, CloudErrOAuthOutcomeUnknown):
		failure.problem, failure.address = setupPageText("setup.page.problem.unreachable"), true
	default:
		failure.problem = setupPageText("setup.page.problem.generic")
	}
	return failure
}
