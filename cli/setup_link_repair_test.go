package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The one-command Link re-pair (setup_link_repair.go), with the fake Link
// coordinator, local sign-in, and cleanup seams the other setup tests use.

const relinkProfileID = "profile-relink"

type relinkCoordinator struct {
	*selectingCloudCoordinator
	owns      []bool // the one-click answer each Link pairing carried
	remoteErr error
	localErr  error
	askCode   bool // an App without the owner's Link pairing: the code prompt
}

func (c *relinkCoordinator) AddRemoteWithPairing(ctx context.Context, request cloudRemoteSetupRequest) (cloudSetupResult, error) {
	own, _ := ctx.Value(ownComputerContextKey{}).(bool)
	c.owns = append(c.owns, own)
	if c.askCode {
		if _, err := request.PairingCode(cloudRemotePairingPrompt{AppURL: productionCloudTestOrigin + "/app/x"}); err != nil {
			return cloudSetupResult{}, err
		}
	}
	if c.remoteErr != nil {
		return cloudSetupResult{}, c.remoteErr
	}
	return c.selectingCloudCoordinator.AddRemoteWithPairing(ctx, request)
}

func (c *relinkCoordinator) AddAwayWithExistingDevice(ctx context.Context, request cloudSetupRequest) (cloudSetupResult, error) {
	if c.localErr != nil {
		return cloudSetupResult{}, c.localErr
	}
	return c.selectingCloudCoordinator.AddAwayWithExistingDevice(ctx, request)
}

type relinkFixture struct {
	t       *testing.T
	paths   runtimePaths
	home    bool
	coord   *relinkCoordinator
	oauth   *KeyringOAuthSecretStore
	cleanup *legacyCleanupFixture
	removes int
	marked  []string // the re-pair checkpoint on disk when Link was removed
	armed   []bool   // the one-click answer each local pairing carried
	pairErr error
	signErr error
}

func relinkOrigin(t *testing.T) CloudOrigin {
	t.Helper()
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

// relinkHybridConfig: Local + Link from 0.31; its key and Link sign-in are
// in the old Keychain items, which 0.32 doesn't read.
func relinkHybridConfig(t *testing.T) runtimeConfig {
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID, cfg.RelayInstanceID, cfg.RoutePolicy = relinkProfileID, "relay-selected", routePolicyAutomatic
	current := cloudMetadataForTest(strings.Repeat("1", 32))
	current.Origin, current.CanonicalOrigin = relinkOrigin(t).InputOrigin, relinkOrigin(t).CanonicalOrigin
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateReady, Current: &current}
	return cfg
}

func newRelinkFixture(t *testing.T, profile string, home bool, cfg runtimeConfig) *relinkFixture {
	t.Helper()
	f := &relinkFixture{t: t, home: home}
	f.paths = setupServerCommandTest(t, `{"schema_version":1}`)
	if profile != defaultServerProfileName {
		setServerSelectionOverride(profile)
	}
	setActiveServerProfile(profile)
	if err := saveConfig(f.paths, cfg); err != nil {
		t.Fatal(err)
	}
	saved := []any{linkRePairApplies, homeRelayAnswers, linkRePairOwnAskedAway, linkRePairPairingCode, newCloudSecretStoreForCLI,
		resolveCanonicalNabuOriginForCloudCommand, linkRePairRemoveLink, resolveHASignInOriginForSetup, openHASignInIngressForSetup,
		pairDeviceV2ArmedForSetup, signInPairForSetup}
	t.Cleanup(func() {
		linkRePairApplies = saved[0].(func() bool)
		homeRelayAnswers = saved[1].(func(context.Context, string) bool)
		linkRePairOwnAskedAway = saved[2].(func() bool)
		linkRePairPairingCode = saved[3].(cloudRemotePairingCodeProvider)
		newCloudSecretStoreForCLI = saved[4].(func(string) (OAuthSecretStore, error))
		resolveCanonicalNabuOriginForCloudCommand = saved[5].(func(context.Context, string, CloudCNAMEResolver) (CloudOrigin, error))
		linkRePairRemoveLink = saved[6].(func(runtimePaths, [][]byte) int)
		resolveHASignInOriginForSetup = saved[7].(func(context.Context, string) (*url.URL, error))
		openHASignInIngressForSetup = saved[8].(func(context.Context, io.Writer, string) (haSignInSession, error))
		pairDeviceV2ArmedForSetup = saved[9].(func(context.Context, *CloudIngressClient, deviceMetadata, bool) (*provisionedCredential, error))
		signInPairForSetup = saved[10].(func(string, pairingProvisioner, *runtimeConfig, func(*runtimeConfig) error, pairingClientInfo, SecretStoreUIPolicy) (string, error))
	})
	linkRePairApplies = func() bool { return true }
	homeRelayAnswers = func(context.Context, string) bool { return f.home }
	linkRePairOwnAskedAway = func() bool { return true }
	linkRePairPairingCode = func(cloudRemotePairingPrompt) (string, error) { return "", errSetupExit }
	stubOwnComputerAsked(t, true)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	var err error
	if f.oauth, err = NewOAuthSecretStore(newMemoryOAuthSecretBackend(), relinkProfileID); err != nil {
		t.Fatal(err)
	}
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) { return f.oauth, nil }
	f.coord = &relinkCoordinator{selectingCloudCoordinator: newSelectingCloudCoordinator()}
	f.coord.result.Current.Origin, f.coord.result.Current.CanonicalOrigin = relinkOrigin(t).InputOrigin, relinkOrigin(t).CanonicalOrigin
	installCloudCommandCoordinator(t, f.coord)
	resolveCanonicalNabuOriginForCloudCommand = func(context.Context, string, CloudCNAMEResolver) (CloudOrigin, error) {
		return relinkOrigin(t), nil
	}
	linkRePairRemoveLink = func(paths runtimePaths, lifecycle [][]byte) int { // `cloud remove`: revokes what this version stored, then drops Link
		f.removes++
		latest, err := loadConfig(paths)
		if err != nil || len(lifecycle) != 3 {
			t.Fatal(err, len(lifecycle))
		}
		f.marked = append(f.marked, string(latest.LinkRePair)+" "+latest.LinkRePairOrigin)
		latest.Cloud, latest.RoutePolicy = nil, routePolicyLocal
		if err := saveConfig(paths, latest); err != nil {
			t.Fatal(err)
		}
		return 0
	}
	resolveHASignInOriginForSetup = func(context.Context, string) (*url.URL, error) {
		return &url.URL{Scheme: "http", Host: "ha:8123"}, nil
	}
	openHASignInIngressForSetup = func(context.Context, io.Writer, string) (haSignInSession, error) {
		return haSignInSession{user: HACurrentUser{Name: "Markus", IsOwner: true}}, f.signErr
	}
	pairDeviceV2ArmedForSetup = func(_ context.Context, _ *CloudIngressClient, _ deviceMetadata, own bool) (*provisionedCredential, error) {
		f.armed = append(f.armed, own)
		return &provisionedCredential{}, nil
	}
	signInPairForSetup = func(_ string, provision pairingProvisioner, cfg *runtimeConfig, save func(*runtimeConfig) error, _ pairingClientInfo, _ SecretStoreUIPolicy) (string, error) {
		if err := validateLocalDeviceReplacementAllowed(*cfg); err != nil { // the guard, unchanged
			return "", err
		}
		if _, err := provision(deviceMetadata{CredentialStore: "keychain"}); err != nil {
			return "", err
		}
		if f.pairErr != nil {
			return "", f.pairErr
		}
		if err := writeDeviceCredential(generateTestDeviceCredential(t)); err != nil {
			t.Fatal(err)
		}
		cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cfg.RelayInstanceID = "https://ha:18793", "NEWPIN", ""
		return "device-new", save(cfg)
	}
	f.cleanup = newLegacyCleanupFixture(t)
	f.cleanup.legacy[oauthSecretCurrentService] = legacyTestEnvelope(OAuthSecretCurrent, relinkProfileID, "old-refresh")
	return f
}

// run is `ha-nova setup` for the profile with input at the terminal.
func (f *relinkFixture) run(input string) (int, string) {
	f.t.Helper()
	cfg, err := loadSelectedRuntimeConfigUnchecked(f.paths) // as setup's recovery reads it
	if err != nil {
		f.t.Fatal(err)
	}
	if !linkRePairNeeded(cfg) {
		f.t.Fatalf("setup would not re-pair %+v", cfg)
	}
	lifecycle := setupLifecycleOf(f.t, f.paths)
	return captureCommandOutput(f.t, func() int {
		return runLinkRePair(bufio.NewReader(strings.NewReader(input)), os.Stdout, f.paths, cfg, lifecycle, false)
	})
}

func (f *relinkFixture) config() runtimeConfig {
	f.t.Helper()
	cfg, err := loadSelectedRuntimeConfigUnchecked(f.paths)
	if err != nil {
		f.t.Fatalf("config after setup: %v", err)
	}
	return cfg
}

// The one step every re-pair text names, and never a Link command.
func requireOneStep(t *testing.T, output, command string) {
	t.Helper()
	if !strings.Contains(output, command) || strings.Contains(output, "cloud remove") || strings.Contains(output, "cloud add") {
		t.Fatalf("want only %q as the next step:\n%s", command, output)
	}
}

func TestLinkRePairAwayPairsThroughLinkWithTheOneClickAnswer(t *testing.T) {
	for input, own := range map[string]bool{"\n": true, "no\n": false} {
		f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
		code, output := f.run(input)
		if code != 0 || len(f.coord.owns) != 1 || f.coord.owns[0] != own || f.removes != 0 {
			t.Fatalf("%q: code %d, Link pairings %v, removes %d\n%s", input, code, f.coord.owns, f.removes, output)
		}
		cfg := f.config()
		if effectiveRoutePolicy(cfg.RoutePolicy) != routePolicyCloud || !cfg.Cloud.ready() || cfg.LinkRePair != linkRePairLinkOnly ||
			cfg.RelaySecureBaseURL+cfg.RelaySpkiPin+cfg.PendingSecureBaseURL+cfg.PendingSpkiPin != "" ||
			cfg.RelayBaseURL != "http://ha:8791" || cfg.HAURL != "http://ha:8123" || cfg.ProfileID != relinkProfileID {
			t.Fatalf("%q: config %+v", input, cfg)
		}
		if _, _, _, _, err := relayFunctionalTransport(cfg); errors.Is(err, errDeviceNeedsRePair) {
			t.Fatalf("%q: still points at the missing local key", input)
		}
		// The old version's sign-in is revoked by the cleanup, last, never copied over.
		if len(f.cleanup.revoked) != 1 || f.cleanup.revoked[0] != "old-refresh" || f.cleanup.reported != 1 {
			t.Fatalf("%q: cleanup revoked %v reported %d", input, f.cleanup.revoked, f.cleanup.reported)
		}
		for _, want := range []string{setupT("setup.relink.away"), setupT("setup.own.question"), setupT("setup.cleanup.done"),
			setupT("setup.relink.link_only", "command", "ha-nova setup")} {
			if !strings.Contains(output, want) {
				t.Fatalf("%q: missing %q\n%s", input, want, output)
			}
		}
		requireOneStep(t, output, "ha-nova setup")
	}
}

func TestLinkOwnerPairingCarriesTheOneClickAnswer(t *testing.T) {
	for _, test := range []struct {
		ctx   context.Context
		store string
		want  string
	}{
		{withOwnComputer(context.Background(), true), "keychain", `{"own_computer":true}`},
		{withOwnComputer(context.Background(), false), "keychain", "{}"},
		{withOwnComputer(context.Background(), true), "file", "{}"},
		{context.Background(), "keychain", "{}"}, // every other Link pairing, as before
	} {
		var path, sent string
		client := newProtocolTestCloudIngressClient(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(request.Body)
			path, sent = request.URL.Path, string(body)
			return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}))
		_, err := pairDeviceV2LinkOwner(test.ctx, client, deviceMetadata{CredentialStore: test.store}, "relay-1")
		if !strings.HasSuffix(path, CloudPathPairArmLink) || sent != test.want || err == nil {
			t.Fatalf("%+v: %s sent %q, %v", test, path, sent, err)
		}
	}
}

// A sign-in this version stored goes exactly as `cloud remove` takes it.
func TestLinkRePairRemovesAStoredLinkSignInLikeCloudRemove(t *testing.T) {
	f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
	establishOAuthCurrent(t, f.oauth, "new-slot-refresh")
	if code, output := f.run("\n"); code != 0 || f.removes != 1 || f.marked[0] != "link_only "+relinkOrigin(t).InputOrigin {
		t.Fatalf("code %d, removes %d, checkpoint at removal %q\n%s", code, f.removes, f.marked, output)
	}
}

func TestLinkRePairAwayFailsClosedWithOneStep(t *testing.T) {
	t.Run("Link sign-in fails, then setup again finishes", func(t *testing.T) {
		f := newRelinkFixture(t, "cabin", false, relinkHybridConfig(t))
		f.coord.remoteErr = newCloudError(CloudErrOAuthCanceled, "sign in", nil)
		code, output := f.run("\n")
		cfg := f.config()
		if code != 1 || cfg.LinkRePair != linkRePairLinkOnly || cfg.RelaySecureBaseURL != "" || cfg.Cloud.ready() {
			t.Fatalf("code %d, config %+v\n%s", code, cfg, output)
		}
		requireOneStep(t, output, setupT("setup.relink.again", "command", "ha-nova setup --server cabin"))
		f.coord.remoteErr = nil
		if code, output := f.run("\n"); code != 0 || !f.config().Cloud.ready() || f.removes != 1 {
			t.Fatalf("again: code %d, removes %d\n%s", code, f.removes, output)
		}
	})
	t.Run("Link gone before any Link state was saved: setup again retries away", func(t *testing.T) {
		f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
		f.coord.remoteErr = newCloudError(CloudErrOAuthCanceled, "sign in", nil)
		f.run("\n")
		cfg := f.config()
		if cfg.LinkRePairOrigin == "" {
			t.Fatalf("away re-pair kept no Link address: %+v", cfg)
		}
		cfg.Cloud, cfg.RoutePolicy = nil, routePolicyLocal
		if err := saveConfig(f.paths, cfg); err != nil {
			t.Fatal(err)
		}
		f.coord.remoteErr = nil
		code, output := f.run("\n")
		if after := f.config(); code != 0 || !after.Cloud.ready() || after.LinkRePairOrigin != "" {
			t.Fatalf("again: code %d, config %+v\n%s", code, after, output)
		}
	})
	t.Run("an older App asks for a code", func(t *testing.T) {
		f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
		f.coord.askCode = true
		code, output := f.run("\n")
		if code != 1 || f.config().LinkRePair != linkRePairLinkOnly || len(f.cleanup.revoked) != 0 {
			t.Fatalf("code %d, revoked %v\n%s", code, f.cleanup.revoked, output)
		}
		requireOneStep(t, output, setupT("setup.relink.again", "command", "ha-nova setup"))
	})
	t.Run("no desktop session: nothing changes", func(t *testing.T) {
		f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
		installCloudCommandPromptSession(t, false)
		before, _ := os.ReadFile(f.paths.ConfigFile)
		code, output := f.run("\n")
		after, _ := os.ReadFile(f.paths.ConfigFile)
		if code != 1 || string(before) != string(after) || len(f.coord.owns) != 0 {
			t.Fatalf("code %d, changed %v\n%s", code, string(before) != string(after), output)
		}
		requireOneStep(t, output, setupT("setup.relink.needs_desktop", "command", "ha-nova setup"))
	})
}

func TestLinkRePairAtHomeSignsInTwiceAndKeepsLink(t *testing.T) {
	f := newRelinkFixture(t, defaultServerProfileName, true, relinkHybridConfig(t))
	code, output := f.run("\n")
	cfg := f.config()
	if code != 0 || len(f.armed) != 1 || !f.armed[0] || f.removes != 0 || f.coord.localCalls != 1 || f.coord.remoteCalls != 0 {
		t.Fatalf("code %d, armed %v, removes %d, Link adds %d/%d\n%s", code, f.armed, f.removes, f.coord.localCalls, f.coord.remoteCalls, output)
	}
	if !cfg.Cloud.ready() || effectiveRoutePolicy(cfg.RoutePolicy) != routePolicyAutomatic || cfg.RelaySecureBaseURL != "https://ha:18793" || cfg.LinkRePair != "" {
		t.Fatalf("config %+v", cfg)
	}
	intro, question, signedIn := strings.Index(output, setupT("setup.relink.home")), strings.Index(output, setupT("setup.own.question")), strings.Index(output, "Signed in to Home Assistant as Markus")
	if intro < 0 || question < intro || signedIn < question || !strings.Contains(output, setupT("setup.relink.home_done")) || f.cleanup.reported != 1 {
		t.Fatalf("announce, ask, then sign in; cleanup last (reported %d):\n%s", f.cleanup.reported, output)
	}
	requireOneStep(t, output, "") // names no Link command at all
}

func TestLinkRePairAtHomeFailsClosedWithOneStep(t *testing.T) {
	t.Run("the sign-in fails: nothing changes", func(t *testing.T) {
		f := newRelinkFixture(t, defaultServerProfileName, true, relinkHybridConfig(t))
		f.signErr = errSignInStopped
		before, _ := os.ReadFile(f.paths.ConfigFile)
		code, output := f.run("\n")
		after, _ := os.ReadFile(f.paths.ConfigFile)
		if code != 1 || string(before) != string(after) {
			t.Fatalf("code %d, changed %v\n%s", code, string(before) != string(after), output)
		}
		requireOneStep(t, output, setupT("setup.relink.again", "command", "ha-nova setup"))
	})
	t.Run("pairing fails mid-way, then setup again finishes with Link", func(t *testing.T) {
		f := newRelinkFixture(t, defaultServerProfileName, true, relinkHybridConfig(t))
		f.pairErr = errors.New("activation failed")
		code, output := f.run("\n")
		if cfg := f.config(); code != 1 || cfg.Cloud != nil || cfg.LinkRePair != linkRePairAddLink || cfg.LinkRePairOrigin == "" {
			t.Fatalf("code %d, config %+v\n%s", code, cfg, output)
		}
		requireOneStep(t, output, "then run ha-nova setup again")
		if strings.Contains(output, setupT("setup.relink.again", "command", "ha-nova setup")) {
			t.Fatalf("two next steps:\n%s", output)
		}
		f.pairErr = nil
		if code, output := f.run("\n"); code != 0 || !f.config().Cloud.ready() || f.config().LinkRePair != "" {
			t.Fatalf("again: code %d\n%s", code, output)
		}
	})
	t.Run("adding Link fails, then setup again adds it without pairing twice", func(t *testing.T) {
		f := newRelinkFixture(t, defaultServerProfileName, true, relinkHybridConfig(t))
		f.coord.localErr = newCloudError(CloudErrOAuthCanceled, "sign in", nil)
		code, output := f.run("\n")
		if code != 1 || f.config().LinkRePair != linkRePairAddLink || len(f.armed) != 1 {
			t.Fatalf("code %d, armed %v\n%s", code, f.armed, output)
		}
		requireOneStep(t, output, setupT("setup.relink.again", "command", "ha-nova setup"))
		f.coord.localErr = nil
		if code, output := f.run(""); code != 0 || len(f.armed) != 1 || !f.config().Cloud.ready() {
			t.Fatalf("again: code %d, armed %v\n%s", code, f.armed, output)
		}
	})
	t.Run("the cleanup is denied: paired, with its retry", func(t *testing.T) {
		f := newRelinkFixture(t, defaultServerProfileName, true, relinkHybridConfig(t))
		legacyCleanupReport = func(runtimePaths, runtimeConfig) error { return errors.New("denied") }
		code, output := f.run("\n")
		if code != 0 || !f.config().Cloud.ready() || !strings.Contains(output, setupT("setup.cleanup.incomplete", "command", "ha-nova setup")) {
			t.Fatalf("code %d\n%s", code, output)
		}
	})
}

// A profile that paired away gets its local route back at home, and the
// hint is gone after that.
func TestLinkRePairAtHomeRestoresTheLocalRouteOfALinkOnlyProfile(t *testing.T) {
	cfg := relinkHybridConfig(t)
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cfg.RoutePolicy, cfg.LinkRePair = "", "", routePolicyCloud, linkRePairLinkOnlyHinted
	f := newRelinkFixture(t, defaultServerProfileName, true, cfg)
	establishOAuthCurrent(t, f.oauth, "away-refresh") // the Link sign-in the away pairing stored
	code, output := f.run("\n")
	cfg = f.config()
	if code != 0 || f.removes != 1 || len(f.armed) != 1 || !cfg.Cloud.ready() || effectiveRoutePolicy(cfg.RoutePolicy) != routePolicyAutomatic || cfg.LinkRePair != "" {
		t.Fatalf("code %d, removes %d, armed %v, config %+v\n%s", code, f.removes, f.armed, cfg, output)
	}
	_, hint := captureCommandOutput(t, func() int { printHomeRouteHint(f.paths, cfg); return 0 })
	if strings.Contains(hint, "fast local route") {
		t.Fatalf("hint after the local route came back: %s", hint)
	}
}

func TestLinkRePairAwayWithAReadyLinkOnlyProfileOnlyRetriesTheCleanup(t *testing.T) {
	cfg := relinkHybridConfig(t)
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cfg.RoutePolicy, cfg.LinkRePair = "", "", routePolicyCloud, linkRePairLinkOnly
	f := newRelinkFixture(t, defaultServerProfileName, false, cfg)
	f.linkSignedIn()
	code, output := f.run("")
	if code != 0 || len(f.coord.owns) != 0 || f.removes != 0 || f.cleanup.reported != 1 {
		t.Fatalf("code %d, Link pairings %v, removes %d, reported %d\n%s", code, f.coord.owns, f.removes, f.cleanup.reported, output)
	}
	requireOneStep(t, output, setupT("setup.relink.link_only", "command", "ha-nova setup"))
}

func TestHomeRouteHintOnceAtHomeAndOnlyInDoctor(t *testing.T) {
	cfg := relinkHybridConfig(t)
	cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cfg.RoutePolicy, cfg.LinkRePair = "", "", routePolicyCloud, linkRePairLinkOnly
	f := newRelinkFixture(t, "cabin", false, cfg)
	hint := setupT("setup.relink.home_hint", "command", "ha-nova setup --server cabin")
	show := func() string {
		_, output := captureCommandOutput(t, func() int { printHomeRouteHint(f.paths, f.config()); return 0 })
		return output
	}
	if output := show(); strings.Contains(output, "fast local route") || f.config().LinkRePair != linkRePairLinkOnly {
		t.Fatalf("away: %s", output)
	}
	f.home = true
	if output := show(); strings.Contains(output, "fast local route") || f.config().LinkRePair != linkRePairLinkOnly {
		t.Fatalf("Link connection not complete: %s", output)
	}
	f.linkSignedIn()
	if output := show(); !strings.Contains(output, hint) || f.config().LinkRePair != linkRePairLinkOnlyHinted {
		t.Fatalf("home: %s", output)
	}
	if output := show(); strings.Contains(output, "fast local route") {
		t.Fatalf("a second time: %s", output)
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		src, _ := os.ReadFile(name)
		if !strings.HasSuffix(name, "_test.go") && name != "setup_link_repair.go" && name != "command_doctor.go" && strings.Contains(string(src), "printHomeRouteHint(") {
			t.Fatalf("%s shows the home hint; only doctor does", name)
		}
	}
}

func TestLinkRePairOnlyForMacProfilesWithLink(t *testing.T) {
	f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
	noLink := relinkHybridConfig(t)
	noLink.Cloud, noLink.RoutePolicy = nil, routePolicyLocal
	held := relinkHybridConfig(t)
	held.Cloud.DeviceRevocationCompleted = &cloudDeviceRevocationCheckpoint{}
	pending := relinkHybridConfig(t)
	pending.PendingSecureBaseURL, pending.PendingSpkiPin = "https://ha:18793", "PIN2"
	legacy := relinkHybridConfig(t)
	legacy.RelaySecureBaseURL, legacy.RelaySpkiPin, legacy.RoutePolicy = "", "", routePolicyCloud
	marked := noLink
	marked.LinkRePair = linkRePairAddLink
	for name, test := range map[string]struct {
		cfg  runtimeConfig
		want bool
	}{
		"Local + Link, key gone":  {relinkHybridConfig(t), true},
		"shared access + Link":    {legacy, true},
		"halfway through":         {marked, true},
		"no Link":                 {noLink, false},
		"Link removal unfinished": {held, false},
		"a pairing to resume":     {pending, false},
	} {
		if got := linkRePairNeeded(test.cfg); got != test.want {
			t.Errorf("%s: %v", name, got)
		}
	}
	linkRePairApplies = func() bool { return false } // Windows, Linux: as before
	if linkRePairNeeded(relinkHybridConfig(t)) {
		t.Error("not a Mac Keychain profile")
	}
	linkRePairApplies = func() bool { return true }
	f.linkSignedIn()
	if linkRePairNeeded(relinkHybridConfig(t)) || f == nil {
		t.Error("a key that's there needs no re-pair")
	}
}

// On a Mac every re-pair text names the one setup step; elsewhere a Link
// profile keeps the remove-first recipe (pairing_predecessor_test.go,
// legacy_access_test.go).
func TestRePairTextsNameOneStepOnAMacWithLink(t *testing.T) {
	saved := linkRePairApplies
	t.Cleanup(func() { linkRePairApplies = saved })
	linkRePairApplies = func() bool { return true }
	for profile, command := range map[string]string{defaultServerProfileName: "ha-nova setup", "cabin": "ha-nova setup --server cabin"} {
		requireOneStep(t, deviceNeedsRePair(profile, true).Error(), "run: "+command)
		requireOneStep(t, sharedAccessEndedFor(runtimeConfig{RelayBaseURL: "http://ha.local:8791", Cloud: &cloudLifecycleMetadata{}}, profile), "Run "+command+" in a terminal once")
	}
	if text := deviceNeedsRePair("cabin", true).Error(); !strings.Contains(text, "one or two Home Assistant sign-ins") {
		t.Fatalf("effort: %s", text)
	}
	resetServerProfileSelection(t)
	err := validateLocalDeviceReplacementAllowed(relinkHybridConfig(t))
	if err == nil {
		t.Fatal("the guard let a pairing beside Link through")
	}
	requireOneStep(t, err.Error(), "run ha-nova setup")
}
