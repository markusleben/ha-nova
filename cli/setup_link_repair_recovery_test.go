package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// The re-pair's recovery paths (setup_link_repair.go): Link-only profiles
// from before, the checkpoint around the Link removal, and what setup says
// when a step fails.

func setupLifecycleOf(t *testing.T, paths runtimePaths) [][]byte {
	t.Helper()
	lifecycle := [][]byte{nil, nil, nil}
	var err error
	if lifecycle[0], err = readInstallLifecycleGeneration(paths); err != nil {
		t.Fatal(err)
	}
	if lifecycle[1], err = readUninstallMarker(paths); err != nil {
		t.Fatal(err)
	}
	if lifecycle[2], err = readSetupConfigSnapshot(paths); err != nil {
		t.Fatal(err)
	}
	return lifecycle
}

// linkSignedIn: this version's Link sign-in and key are in their slots.
func (f *relinkFixture) linkSignedIn() {
	f.t.Helper()
	if _, signedIn, _ := f.oauth.LoadCurrent(context.Background(), SecretStoreForbidUI); !signedIn {
		establishOAuthCurrent(f.t, f.oauth, "link-refresh")
	}
	if err := writeDeviceCredential(generateTestDeviceCredential(f.t)); err != nil {
		f.t.Fatal(err)
	}
}

// relinkLinkOnlyConfig: a 0.31 profile that reaches Home Assistant only
// through Link; no home address, so the re-pair always runs away.
func relinkLinkOnlyConfig(t *testing.T) runtimeConfig {
	cfg := relinkHybridConfig(t)
	cfg.RelayBaseURL, cfg.RelaySecureBaseURL, cfg.RelaySpkiPin, cfg.RoutePolicy = "", "", "", routePolicyCloud
	return cfg
}

func TestLinkRePairForALinkOnlyProfileFromBefore(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		f := newRelinkFixture(t, profile, false, relinkLinkOnlyConfig(t))
		code, output := f.run("\n")
		cfg := f.config()
		if code != 0 || len(f.coord.owns) != 1 || !cfg.Cloud.ready() || cfg.LinkRePair != "" || cfg.LinkRePairOrigin != "" || effectiveRoutePolicy(cfg.RoutePolicy) != routePolicyCloud {
			t.Fatalf("%s: code %d, Link pairings %v, config %+v\n%s", profile, code, f.coord.owns, cfg, output)
		}
		if strings.Contains(output, "fast local route") || !strings.Contains(output, setupT("setup.relink.connected")) {
			t.Fatalf("%s: no local route to add:\n%s", profile, output)
		}
		requireOneStep(t, output, "")
		f.linkSignedIn() // what the Link pairing stored
		if linkRePairNeeded(f.config()) {
			t.Fatalf("%s: a second setup would re-pair again", profile)
		}
	}
	f := newRelinkFixture(t, defaultServerProfileName, false, relinkLinkOnlyConfig(t))
	f.linkSignedIn()
	if linkRePairNeeded(f.config()) {
		t.Fatal("a Link-only profile with its sign-in and key needs no re-pair")
	}
}

// Ordinary commands on a Mac name the setup step (PAIR_AGAIN); elsewhere
// the missing sign-in stays a Link authorization problem.
func TestCloudRouteWithoutThisVersionsSignInSaysPairAgainOnAMac(t *testing.T) {
	f := newRelinkFixture(t, "cabin", false, relinkLinkOnlyConfig(t))
	defer setCloudFeatureTestBuild(t, true)()
	saved := cloudRuntimeSecureStorageContextAvailable
	t.Cleanup(func() { cloudRuntimeSecureStorageContextAvailable = saved })
	cloudRuntimeSecureStorageContextAvailable = func() bool { return true }
	_, err := resolveCloudRelayTransport(context.Background(), f.config())
	if !errors.Is(err, errDeviceNeedsRePair) || !strings.HasSuffix(err.Error(), "run: ha-nova setup --server cabin") {
		t.Fatalf("Mac: %v", err)
	}
	linkRePairApplies = func() bool { return false }
	if _, err := resolveCloudRelayTransport(context.Background(), f.config()); !IsCloudErrorCode(err, CloudErrSecretNotFound) {
		t.Fatalf("elsewhere: %v", err)
	}
}

// Setup's own removal keeps the checkpoint and runs only while the profile
// is as setup read it; `cloud remove` itself clears the checkpoint.
func TestCloudRemoveForTheRePairKeepsItsCheckpoint(t *testing.T) {
	for _, mode := range []string{"re-pair", "changed meanwhile", "cloud remove"} {
		paths, store, backend, _, credential := remoteOnlyCloudRemovalFixture(t)
		revoked := 0
		installRemoteDeviceRevokeHook(t, func(context.Context, runtimeConfig, OAuthSecretStore, string) error { revoked++; return nil })
		installCloudRemoveStore(t, store, func(context.Context, OAuthSecretEnvelope) error { return nil })
		resetProductionCloudPolicies(backend)
		cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
		if err != nil {
			t.Fatal(err)
		}
		cfg.LinkRePair, cfg.LinkRePairOrigin = linkRePairLinkOnly, productionCloudTestOrigin
		if err := saveConfig(paths, cfg); err != nil {
			t.Fatal(err)
		}
		lifecycle := setupLifecycleOf(t, paths)
		if mode == "changed meanwhile" {
			cfg.HAURL = "http://other:8123"
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}
		}
		code, output := captureCommandOutput(t, func() int {
			if mode == "cloud remove" {
				return runCloudRemoveCommand(paths, []string{"--yes"})
			}
			return linkRePairRemoveLink(paths, lifecycle)
		})
		after, err := loadSelectedRuntimeConfigUnchecked(paths)
		if err != nil {
			t.Fatal(err)
		}
		kept := after.LinkRePair == linkRePairLinkOnly && after.LinkRePairOrigin == productionCloudTestOrigin
		// The device stays for the local pairing: its activation retires this
		// same-install predecessor and carries the phone link over.
		key, _, _ := readCredentialSlot(deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile))
		switch {
		case mode == "re-pair" && (code != 0 || after.Cloud != nil || !kept || revoked != 0 || key != credential),
			mode == "re-pair" && strings.Contains(output, "Link access was removed"), // setup says what its re-pair did, once done
			mode == "cloud remove" && !strings.Contains(output, "Link access was removed"),
			mode == "cloud remove" && revoked != 1,
			mode == "changed meanwhile" && (code == 0 || after.Cloud == nil || !kept),
			mode == "cloud remove" && (code != 0 || after.Cloud != nil || after.LinkRePair != "" || after.LinkRePairOrigin != ""):
			t.Fatalf("%s: code %d, config %+v\n%s", mode, code, after, output)
		}
	}
}

// At home, setup paired but couldn't add Link; away now, the local pairing
// stays as it is and only setup at home finishes (Link can't take over that
// key: its Relay identity isn't known), with or without a saved Link address.
func TestLinkRePairAddLinkCheckpointAwayKeepsTheLocalPairing(t *testing.T) {
	for _, origin := range []string{relinkOrigin(t).InputOrigin, ""} {
		cfg := relinkHybridConfig(t)
		cfg.Cloud, cfg.RoutePolicy, cfg.LinkRePair, cfg.LinkRePairOrigin = nil, routePolicyLocal, linkRePairAddLink, origin
		f := newRelinkFixture(t, defaultServerProfileName, false, cfg)
		before, _ := os.ReadFile(f.paths.ConfigFile)
		code, output := f.run("\n")
		after, _ := os.ReadFile(f.paths.ConfigFile)
		if code != 1 || len(f.coord.owns) != 0 || string(before) != string(after) {
			t.Fatalf("origin %q: code %d, changed %v\n%s", origin, code, string(before) != string(after), output)
		}
		requireOneStep(t, output, setupT("setup.relink.home_needed", "command", "ha-nova setup"))
	}
}

func TestLinkRePairFailureWithALinkHoldNamesTheHoldsSteps(t *testing.T) {
	for _, cfg := range []runtimeConfig{relinkHybridConfig(t), relinkLinkOnlyConfig(t)} {
		testLinkRePairFailureWithALinkHold(t, cfg)
	}
}

func testLinkRePairFailureWithALinkHold(t *testing.T, cfg runtimeConfig) {
	f := newRelinkFixture(t, defaultServerProfileName, false, cfg)
	establishOAuthCurrent(t, f.oauth, "new-slot-refresh")
	linkRePairRemoveLink = func(paths runtimePaths, _ [][]byte) int {
		latest, err := loadSelectedRuntimeConfigUnchecked(paths)
		if err != nil {
			t.Fatal(err)
		}
		latest.Cloud.RecoveryHold = &cloudRecoveryHold{Code: cloudProblemAuthorization, Remediation: cloudRemediationSecurityStop}
		if err := saveConfig(paths, latest); err != nil {
			t.Fatal(err)
		}
		return 1
	}
	code, output := f.run("\n")
	if code != 1 || !strings.Contains(output, "to keep your home safe") || strings.Contains(output, setupT("setup.relink.again", "command", "ha-nova setup")) {
		t.Fatalf("code %d\n%s", code, output)
	}
}

func TestLinkRePairAwayCodeRouteSaysOneClickStaysOff(t *testing.T) {
	for input, own := range map[string]bool{"\n": true, "no\n": false} {
		f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
		f.coord.askCode = true
		_, output := f.run(input)
		if strings.Contains(output, setupT("setup.relink.own_off")) != own {
			t.Fatalf("%q:\n%s", input, output)
		}
	}
}

func TestLinkRePairAwaySaysConnectedFirstAndTheCleanupLast(t *testing.T) {
	f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
	_, output := f.run("\n")
	connected, next, cleaned := strings.Index(output, setupT("setup.relink.connected")),
		strings.Index(output, setupT("setup.relink.link_only", "command", "ha-nova setup")), strings.Index(output, setupT("setup.cleanup.done"))
	if connected < 0 || next < connected || cleaned < next {
		t.Fatalf("order:\n%s", output)
	}
	f = newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
	legacyCleanupReport = func(runtimePaths, runtimeConfig) error { return errors.New("denied") }
	code, output := f.run("\n")
	incomplete := strings.Index(output, setupT("setup.cleanup.incomplete", "reason", "the NOVA App didn't record it (denied)", "command", "ha-nova setup"))
	if code != 0 || incomplete < strings.Index(output, setupT("setup.relink.connected")) || strings.Contains(output, "fast local route") {
		t.Fatalf("cleanup incomplete: its retry is the only next step:\n%s", output)
	}
}

func TestLinkRePairLinkOnlyShortcutChecksTheLinkSignIn(t *testing.T) {
	cfg := relinkLinkOnlyConfig(t)
	cfg.LinkRePair = linkRePairLinkOnly
	f := newRelinkFixture(t, defaultServerProfileName, false, cfg)
	if code, output := f.run("\n"); code != 0 || len(f.coord.owns) != 1 {
		t.Fatalf("missing sign-in: Link pairs again; code %d, pairings %v\n%s", code, f.coord.owns, output)
	}
	f = newRelinkFixture(t, defaultServerProfileName, false, cfg)
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) {
		return nil, newCloudError(CloudErrSecretStoreLocked, "open", nil)
	}
	code, output := f.run("\n")
	if code != 1 || len(f.coord.owns) != 0 || strings.Contains(output, setupT("setup.relink.again", "command", "ha-nova setup")) {
		t.Fatalf("storage unreadable: code %d\n%s", code, output)
	}
}

func TestLinkRePairNamedPairingFailureNamesOneStep(t *testing.T) {
	f := newRelinkFixture(t, "cabin", true, relinkHybridConfig(t))
	f.pairErr = errors.New("activation failed")
	code, output := f.run("\n")
	if code != 1 || strings.Contains(output, "run ha-nova setup again") || strings.Contains(output, setupT("setup.relink.again", "command", "ha-nova setup --server cabin")) {
		t.Fatalf("code %d\n%s", code, output)
	}
	requireOneStep(t, output, "then run ha-nova setup --server cabin again")
}

func TestSetupForAnAIAppWaitsForTheRePair(t *testing.T) {
	f := newRelinkFixture(t, defaultServerProfileName, false, relinkHybridConfig(t))
	before, _ := os.ReadFile(f.paths.ConfigFile)
	code, output := captureCommandOutput(t, func() int { return runSetup(f.paths, []string{"claude"}) })
	after, _ := os.ReadFile(f.paths.ConfigFile)
	if code != 1 || string(before) != string(after) || len(f.coord.owns) != 0 {
		t.Fatalf("code %d, changed %v\n%s", code, string(before) != string(after), output)
	}
	requireOneStep(t, output, setupT("setup.relink.client_first", "command", "ha-nova setup"))
}

// PAIR_AGAIN only in a state setup's re-pair takes: a held or unfinished
// Link keeps its own steps.
func TestPairAgainOnlyWhereTheRePairTakesTheProfile(t *testing.T) {
	f := newRelinkFixture(t, defaultServerProfileName, false, relinkLinkOnlyConfig(t))
	defer setCloudFeatureTestBuild(t, true)()
	held := relinkLinkOnlyConfig(t)
	held.Cloud.RecoveryHold = &cloudRecoveryHold{Code: cloudProblemAuthorization, Remediation: cloudRemediationSecurityStop}
	reconnecting := relinkLinkOnlyConfig(t)
	reconnecting.Cloud.State = cloudStateAuthorizing
	for name, cfg := range map[string]runtimeConfig{"held": held, "reconnecting": reconnecting} {
		if linkRePairOffered(cfg) {
			t.Errorf("%s: offered", name)
		}
	}
	saved := cloudRuntimeSecureStorageContextAvailable
	t.Cleanup(func() { cloudRuntimeSecureStorageContextAvailable = saved })
	cloudRuntimeSecureStorageContextAvailable = func() bool { return true }
	if _, err := resolveCloudRelayTransport(context.Background(), held); errors.Is(err, errDeviceNeedsRePair) || !IsCloudErrorCode(err, CloudErrSecretNotFound) {
		t.Fatalf("held: the existing error, which its hold's guidance follows: %v", err)
	}
	if !linkRePairOffered(f.config()) {
		t.Fatal("a ready Link profile is offered the re-pair")
	}
}

// A named profile's client setup waits for the re-pair as well: a hybrid
// with its local key but no Link sign-in, and a Link-only profile.
func TestNamedSetupForAnAIAppWaitsForTheRePair(t *testing.T) {
	for name, cfg := range map[string]runtimeConfig{"hybrid": relinkHybridConfig(t), "Link-only": relinkLinkOnlyConfig(t)} {
		f := newRelinkFixture(t, "cabin", false, cfg)
		if name == "hybrid" {
			if err := writeDeviceCredential(generateTestDeviceCredential(t)); err != nil {
				t.Fatal(err)
			}
		}
		before, _ := os.ReadFile(f.paths.ConfigFile)
		code, output := captureCommandOutput(t, func() int { return runSetup(f.paths, []string{"claude", "--server", "cabin"}) })
		after, _ := os.ReadFile(f.paths.ConfigFile)
		if code != 1 || string(before) != string(after) || len(f.coord.owns) != 0 {
			t.Fatalf("%s: code %d, changed %v\n%s", name, code, string(before) != string(after), output)
		}
		requireOneStep(t, output, setupT("setup.relink.client_first", "command", "ha-nova setup --server cabin"))
	}
}
