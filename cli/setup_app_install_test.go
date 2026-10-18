package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubAppInstallSignIn replaces the browser sign-in: each call runs prepare
// against the given fake Supervisor (nil: sign-in fails with the next error).
func stubAppInstallSignIn(t *testing.T, supervisor *fakeSupervisor, results ...error) *int {
	t.Helper()
	calls := 0
	previousOpen, previousResolve, previousReady := openHASignInInstallForSetup, resolveHASignInOriginForSetup, relayAdvertisesSignInForSetup
	t.Cleanup(func() {
		openHASignInInstallForSetup, resolveHASignInOriginForSetup, relayAdvertisesSignInForSetup = previousOpen, previousResolve, previousReady
		setupInstalledSignIn = nil
	})
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) {
		return &url.URL{Scheme: "http", Host: "ha.local"}, nil
	}
	relayAdvertisesSignInForSetup = func(string) bool { return true }
	openHASignInInstallForSetup = func(_ context.Context, out io.Writer, origin string, prepare haSignInPrepare) (haSignInSession, error) {
		calls++
		if prepare == nil {
			t.Fatal("the install sign-in must carry the install step")
		}
		owner := haSignInSession{user: HACurrentUser{Name: "Markus", IsOwner: true}}
		if len(results) > 0 {
			err := results[0]
			results = results[1:]
			if err != nil {
				return owner, err
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := prepare(ctx, supervisor, owner.user); err != nil {
			return owner, err
		}
		return owner, nil
	}
	return &calls
}

func runAppInstallStep(t *testing.T, input string, update bool) (bool, error, string) {
	t.Helper()
	var out bytes.Buffer
	cfg := runtimeConfig{HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791"}
	done, err := runSignInAppInstall(bufio.NewReader(strings.NewReader(input)), &out, cfg, update, "0.9.2", "0.10.0")
	return done, err, out.String()
}

func TestAppInstallNeedsAnExplicitChoice(t *testing.T) {
	for name, test := range map[string]struct {
		input  string
		update bool
		err    error
		output string
	}{
		"manual":                      {input: "manual\n", output: "sign in and install NOVA"},
		"Enter alone is no consent":   {input: "\nmanual\n", output: "Type 'yes' or 'manual'."},
		"back":                        {input: "back\n", err: errSetupBack},
		"no answer":                   {input: "", err: io.EOF},
		"update later":                {input: "later\n", update: true, output: "older than this HA NOVA needs (v0.10.0)"},
		"update: Enter is no consent": {input: "\nno\n", update: true, output: "Type 'yes' or 'later'."},
	} {
		t.Run(name, func(t *testing.T) {
			calls := stubAppInstallSignIn(t, newFakeSupervisor(t))
			done, err, output := runAppInstallStep(t, test.input, test.update)
			if done || !errors.Is(err, test.err) || (test.err == nil && err != nil) || *calls != 0 {
				t.Fatalf("done=%v err=%v sign-ins=%d\n%s", done, err, *calls, output)
			}
			if !strings.Contains(output, test.output) {
				t.Fatalf("output lacks %q:\n%s", test.output, output)
			}
			if setupInstalledSignIn != nil {
				t.Fatal("no session may wait for pairing without an install")
			}
		})
	}
}

func TestAppInstallAfterYesInstallsAndHandsTheSignInToPairing(t *testing.T) {
	supervisor := newFakeSupervisor(t)
	calls := stubAppInstallSignIn(t, supervisor)
	done, err, output := runAppInstallStep(t, "yes\n", false)
	if !done || err != nil || *calls != 1 || setupInstalledSignIn == nil {
		t.Fatalf("done=%v err=%v sign-ins=%d session=%v\n%s", done, err, *calls, setupInstalledSignIn, output)
	}
	for _, line := range []string{"Signed in to Home Assistant as Markus (owner)", "NOVA App source added", "NOVA App installed (v0.10.0)", "NOVA App started", "NOVA App is ready"} {
		if !strings.Contains(output, line) {
			t.Fatalf("output lacks %q:\n%s", line, output)
		}
	}
}

func TestAppInstallRetriesOnlyWhileNothingChanged(t *testing.T) {
	// A wrong account changes nothing: offer the sign-in again.
	calls := stubAppInstallSignIn(t, newFakeSupervisor(t), errSignInNotOwner)
	done, err, output := runAppInstallStep(t, "yes\nyes\n", false)
	if !done || err != nil || *calls != 2 || !strings.Contains(output, "sign in again and install NOVA") ||
		!strings.Contains(output, "Nothing was changed in Home Assistant.") {
		t.Fatalf("done=%v err=%v sign-ins=%d\n%s", done, err, *calls, output)
	}

	// Half done: say what exists, then hand over to the manual steps; never
	// a silent retry and never an uninstall.
	setupInstalledSignIn = nil
	supervisor := newFakeSupervisor(t)
	supervisor.startFails = true
	calls = stubAppInstallSignIn(t, supervisor)
	done, err, output = runAppInstallStep(t, "yes\n", false)
	if done || err != nil || *calls != 1 || setupInstalledSignIn != nil {
		t.Fatalf("done=%v err=%v sign-ins=%d\n%s", done, err, *calls, output)
	}
	if !strings.Contains(output, "Home Assistant couldn't start the NOVA App.") ||
		!strings.Contains(output, "The NOVA App (v0.10.0) is installed but not running.") {
		t.Fatalf("failure must say what exists:\n%s", output)
	}
	for _, write := range supervisor.writes() {
		if strings.Contains(write, "uninstall") {
			t.Fatalf("no automatic uninstall: %v", supervisor.writes())
		}
	}
}

func TestAppInstallOnHomeAssistantWithoutAppsPointsToTheContainerGuide(t *testing.T) {
	supervisor := newFakeSupervisor(t)
	supervisor.noHassio = true
	stubAppInstallSignIn(t, supervisor)
	done, err, output := runAppInstallStep(t, "yes\n", false)
	if done || err != nil || !strings.Contains(output, relayContainerGuideURL) {
		t.Fatalf("done=%v err=%v\n%s", done, err, output)
	}
}

func TestRelayInstallStepsFallBackToTheManualPageOrContinue(t *testing.T) {
	previousOffered := haAppInstallOffered
	t.Cleanup(func() { haAppInstallOffered = previousOffered })
	haAppInstallOffered = func(io.Writer, runtimeConfig) bool { return true }
	versionFile := filepath.Join(t.TempDir(), "version.json")
	if err := os.WriteFile(versionFile, []byte(`{"min_relay_version":"0.10.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := runtimePaths{VersionFile: versionFile}
	cfg := runtimeConfig{HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791"}
	manual := "Your browser opens the NOVA App"

	for name, test := range map[string]struct {
		running   bool
		version   string
		input     string
		calls     int
		wantSteps bool
	}{
		"installed for you":      {input: "yes\n", calls: 1},
		"install it myself":      {input: "manual\n\n\n", wantSteps: true},
		"outdated, update later": {running: true, version: "0.9.2", input: "later\n"},
		"outdated, updated":      {running: true, version: "0.9.2", input: "yes\n", calls: 1},
	} {
		t.Run(name, func(t *testing.T) {
			stubInstalledRelayProbe(t, test.running, test.version)
			supervisor := newFakeSupervisor(t)
			if test.running {
				supervisor.installedApp(test.version, "started", "0.10.0")
			}
			calls := stubAppInstallSignIn(t, supervisor)
			previousOpen := openBrowserForSetup
			openBrowserForSetup = func(string) error { return nil }
			t.Cleanup(func() { openBrowserForSetup = previousOpen })
			var out bytes.Buffer
			err := runRelayInstallSteps(bufio.NewReader(strings.NewReader(test.input)), &out, paths, cfg)
			if err != nil || *calls != test.calls || strings.Contains(out.String(), manual) != test.wantSteps {
				t.Fatalf("err=%v sign-ins=%d manual steps=%v\n%s", err, *calls, strings.Contains(out.String(), manual), out.String())
			}
		})
	}
}

// One sign-in: the pairing step uses the session from the install and never
// opens a second sign-in.
func TestPairingUsesTheInstallSignInOnce(t *testing.T) {
	previousProbe, previousOpen, previousPair := probePairingV1ForSetup, openHASignInIngressForSetup, signInPairForSetup
	t.Cleanup(func() {
		probePairingV1ForSetup, openHASignInIngressForSetup, signInPairForSetup = previousProbe, previousOpen, previousPair
		setupInstalledSignIn = nil
	})
	probePairingV1ForSetup = func(string) bool { return true }
	openHASignInIngressForSetup = func(context.Context, io.Writer, string) (haSignInSession, error) {
		t.Fatal("pairing must not ask for a second sign-in")
		return haSignInSession{}, nil
	}
	pairs := 0
	signInPairForSetup = func(string, pairingProvisioner, *runtimeConfig, func(*runtimeConfig) error, pairingClientInfo, SecretStoreUIPolicy) (string, error) {
		pairs++
		return "device-1", nil
	}
	setupInstalledSignIn = &haSignInSession{user: HACurrentUser{ID: "user-1", Name: "Markus", IsOwner: true}}
	cfg := &runtimeConfig{HAURL: "http://ha.local:8123", RelayBaseURL: "http://ha.local:8791", ClientInstallID: "inst-test"}
	_, err := runSetupPairingFlow(bufio.NewReader(strings.NewReader("")), io.Discard, runtimePaths{ConfigDir: t.TempDir()}, cfg, false)
	if !errors.Is(err, errSetupDevicePaired) || pairs != 1 || setupInstalledSignIn != nil {
		t.Fatalf("err=%v pairs=%d leftover=%v", err, pairs, setupInstalledSignIn)
	}
}

func TestAppInstallIsNeverOfferedWithoutATerminal(t *testing.T) {
	cfg := applySelectedSetupHost(runtimeConfig{}, "192.168.1.5", "http://192.168.1.5:8123", "")
	if defaultHAAppInstallOffered(&bytes.Buffer{}, cfg) {
		t.Fatal("offered without a terminal")
	}
}

// The install runs inside the owner's sign-in: after the owner check, with
// its own budget (longer than the 30 s steps), before the ingress session,
// and the Home Assistant session is still revoked at the end.
func TestSignInInstallStepRunsInsideTheSignInAndStillRevokes(t *testing.T) {
	previous := haSignInStepTimeout
	haSignInStepTimeout = 300 * time.Millisecond
	t.Cleanup(func() { haSignInStepTimeout = previous })
	ha := newFakeSignInHA(t)
	ha.extra = func(data []byte) (string, bool) {
		if strings.Contains(string(data), `"type":"get_config"`) {
			return `{"components":["hassio"]}`, true
		}
		return "", false
	}
	useFakeBrowser(t, ha, signedInQuery)
	prepare := func(ctx context.Context, socket haWSCaller, user HACurrentUser) error {
		time.Sleep(2 * haSignInStepTimeout) // a long install outlives the step budget
		var config struct{ Components []string }
		return socket.Call(ctx, "get_config", nil, &config)
	}
	session, err := openHASignInIngressWith(context.Background(), io.Discard, ha.server.URL, prepare)
	if err != nil || session.ingress == nil {
		t.Fatalf("err = %v", err)
	}
	ha.mu.Lock()
	events := strings.Join(ha.events, ",")
	ha.mu.Unlock()
	want := "auth/current_user ,get_config ,supervisor/api /addons/" + ha.slug() + "/info,supervisor/api /ingress/session,revoke"
	if !strings.HasSuffix(events, want) {
		t.Fatalf("events = %s, want suffix %s", events, want)
	}

	failing := newFakeSignInHA(t)
	useFakeBrowser(t, failing, signedInQuery)
	session, err = openHASignInIngressWith(context.Background(), io.Discard, failing.server.URL,
		func(context.Context, haWSCaller, HACurrentUser) error { return errAppInstallNoSupervisor })
	failing.mu.Lock()
	defer failing.mu.Unlock()
	if !errors.Is(err, errAppInstallNoSupervisor) || session.ingress != nil || !failing.revoked {
		t.Fatalf("err=%v ingress=%v revoked=%v", err, session.ingress, failing.revoked)
	}
}
