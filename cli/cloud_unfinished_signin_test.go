package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func unfinishedSignInTestConfig(t *testing.T) runtimeConfig {
	t.Helper()
	cfg, metadata := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	marker := metadata
	cfg.Cloud.SignInAwaitingBrowser = &marker
	return cfg
}

func TestUnfinishedSignInNeedsNoRevoke(t *testing.T) {
	cfg := unfinishedSignInTestConfig(t)
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())

	plan, err := inspectCloudAuthorizationCleanup(context.Background(), cfg, store)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if !plan.unfinishedSignIn || plan.hasAuthorization() {
		t.Fatalf("plan = %+v", plan)
	}
}

// Every trace that the code may have been exchanged voids the proof.
func TestUnfinishedSignInProofFailsClosed(t *testing.T) {
	for name, mutate := range map[string]func(*runtimeConfig){
		"no marker (older version)": func(cfg *runtimeConfig) {
			cfg.Cloud.SignInAwaitingBrowser = nil
		},
		"marker from another attempt": func(cfg *runtimeConfig) {
			marker := *cfg.Cloud.SignInAwaitingBrowser
			marker.OAuthClientID = "http://127.0.0.1:43999/ha-nova"
			cfg.Cloud.SignInAwaitingBrowser = &marker
		},
		"past the code exchange": func(cfg *runtimeConfig) {
			cfg.Cloud.State = cloudStateTokenStored
		},
		"recovery hold": func(cfg *runtimeConfig) {
			cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
				Code:        cloudProblemAuthorization,
				Remediation: cloudRemediationSecurityStop,
			}
		},
		"device activation started": func(cfg *runtimeConfig) {
			cfg.Cloud.DeviceActivationStarted = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := unfinishedSignInTestConfig(t)
			mutate(&cfg)
			store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())

			plan, err := inspectCloudAuthorizationCleanup(context.Background(), cfg, store)
			if !errors.Is(err, errCloudAuthorizationCleanupUnverifiable) {
				t.Fatalf("inspect error = %v", err)
			}
			if plan.unfinishedSignIn {
				t.Fatal("unproven sign-in treated as unfinished")
			}
		})
	}
}

// A stored credential of the pending generation contradicts the marker: it is
// revoked like any other.
func TestUnfinishedSignInMarkerNeverHidesStoredCredential(t *testing.T) {
	cfg := unfinishedSignInTestConfig(t)
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())
	envelope := productionCloudTestEnvelope()
	envelope.Generation = cfg.Cloud.Pending.CredentialGeneration
	if _, err := store.CreatePending(context.Background(), envelope, SecretStoreForbidUI); err != nil {
		t.Fatal(err)
	}

	plan, err := inspectCloudAuthorizationCleanup(context.Background(), cfg, store)
	if plan.unfinishedSignIn || (err == nil && !plan.hasPending) {
		t.Fatalf("plan = %+v err = %v", plan, err)
	}
}

func TestUnprovenSignInStopIsPlain(t *testing.T) {
	cfg := unfinishedSignInTestConfig(t)
	cfg.Cloud.SignInAwaitingBrowser = nil
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())
	_, err := inspectCloudAuthorizationCleanup(context.Background(), cfg, store)
	err = cloudAuthorizationCleanupErrorWithRecoveryCommand(err, defaultServerProfileName)
	wrapped := errors.Join(err, errors.New("persist Cloud recovery safety hold: disk full"))

	text := uninstallErrorText(errorsWrap("failed to revoke Home Assistant Link authorization", wrapped))
	lines := strings.Split(text, "\n")
	want := []string{
		"Home Assistant Link: Your Home Assistant Link sign-in on this computer didn't finish, and HA NOVA can't tell if Home Assistant saved it, so it stopped. (code cloud_authorization; next: inspect_security)",
		`Next: in Home Assistant, click Revoke next to this computer on the NOVA page. Then, for each person who uses HA NOVA, open Profile > Security and delete every sign-in that ends in "/ha-nova". Then run: ha-nova cloud remove --server default --yes --confirm-remote-access-revoked default`,
		"persist Cloud recovery safety hold: disk full",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("text:\n%s", text)
	}
}

func errorsWrap(prefix string, err error) error {
	return &wrappedTestError{prefix: prefix, err: err}
}

type wrappedTestError struct {
	prefix string
	err    error
}

func (e *wrappedTestError) Error() string { return e.prefix + ": " + e.err.Error() }
func (e *wrappedTestError) Unwrap() error { return e.err }

func TestSignInMarkerOnlyForFreshOrProvenSignIn(t *testing.T) {
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-marker"
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	save := func(value runtimeConfig) error { return saveConfig(paths, value) }
	_, metadata := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	reload := func() runtimeConfig {
		t.Helper()
		loaded, err := loadSelectedRuntimeConfigUnchecked(paths)
		if err != nil {
			t.Fatal(err)
		}
		return loaded
	}

	request := newCloudSetupRequest(&cfg, save)
	if err := request.PersistPendingSignIn(metadata); err != nil {
		t.Fatal(err)
	}
	if !reload().Cloud.unfinishedSignIn() {
		t.Fatal("fresh sign-in did not save its proof")
	}
	// A retry with a new loopback client keeps the proof.
	metadata.OAuthClientID = "http://127.0.0.1:43999/ha-nova"
	if err := request.PersistPendingSignIn(metadata); err != nil {
		t.Fatal(err)
	}
	if !reload().Cloud.unfinishedSignIn() {
		t.Fatal("retried sign-in lost its proof")
	}
	if err := request.StartSignInCodeExchange(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sign_in_awaiting_browser") {
		t.Fatalf("proof survived the code exchange:\n%s", data)
	}
	// Once the exchange started, a new browser attempt can't prove anything.
	if err := request.PersistPendingSignIn(metadata); err != nil {
		t.Fatal(err)
	}
	if reload().Cloud.SignInAwaitingBrowser != nil {
		t.Fatal("sign-in after an exchange got a proof")
	}
}

func TestOAuthSignInDropsProofBeforeCodeExchange(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-oauth-proof"
	cfg.RelayInstanceID = "relay-oauth-proof"
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewOAuthSecretStore(newMemoryOAuthSecretBackend(), cfg.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	onDisk := func() *cloudLifecycleMetadata {
		loaded, err := loadSelectedRuntimeConfigUnchecked(paths)
		if err != nil {
			t.Fatal(err)
		}
		return loaded.Cloud
	}

	oldProof := verifyCloudOriginForOAuth
	oldFlow := cloudOAuthFlowForSetup
	oldBrowser := openCloudOAuthBrowserForSetup
	oldExchange := exchangeCloudAuthorizationCodeForSetup
	verifyCloudOriginForOAuth = func(context.Context, CloudOrigin) error { return nil }
	cloudOAuthFlowForSetup = func() *OAuthLoopbackFlow {
		flow := NewOAuthLoopbackFlow()
		flow.Timeout = 2 * time.Second
		return flow
	}
	browserSawProof := false
	openCloudOAuthBrowserForSetup = func(ctx context.Context, target string) error {
		browserSawProof = onDisk().unfinishedSignIn()
		return completeOAuthLoopbackForTest(ctx, target, "code")
	}
	exchangeCalled := false
	exchangeCloudAuthorizationCodeForSetup = func(
		context.Context,
		*HAOAuthClient,
		OAuthAuthorizationCode,
	) (HAOAuthToken, error) {
		exchangeCalled = true
		if onDisk().SignInAwaitingBrowser != nil {
			t.Fatal("code exchange ran while the config still proves no exchange")
		}
		return HAOAuthToken{}, errors.New("stop after the check")
	}
	t.Cleanup(func() {
		verifyCloudOriginForOAuth = oldProof
		cloudOAuthFlowForSetup = oldFlow
		openCloudOAuthBrowserForSetup = oldBrowser
		exchangeCloudAuthorizationCodeForSetup = oldExchange
	})

	_ = withPausableClientMutationLock(paths, func(mutation *pausableClientMutationLock) error {
		loaded, err := loadSelectedRuntimeConfigUnchecked(paths)
		if err != nil {
			return err
		}
		save := func(value runtimeConfig) error {
			if err := mutation.requireHeld(); err != nil {
				return err
			}
			return saveConfig(paths, value)
		}
		request := newCloudSetupRequest(&loaded, save, mutation)
		_, _, err = authorizeOrRefreshCloud(context.Background(), request, origin, store, OAuthSecretEnvelope{})
		return err
	})
	if !browserSawProof || !exchangeCalled {
		t.Fatalf("browser saw proof=%v exchange called=%v", browserSawProof, exchangeCalled)
	}
	if onDisk().unfinishedSignIn() {
		t.Fatal("a sign-in whose code was exchanged still counts as unfinished")
	}
}

func TestPurgeRemovesUnfinishedSignInWithoutRevoke(t *testing.T) {
	paths := writeTestConfigFile(t, `{
		"schema_version":3,
		"servers":{
			"default":{
				"profile_id":"profile-unfinished",
				"route_policy":"cloud",
				"cloud":{
					"state":"authorizing",
					"pending":{
						"origin":"https://unit.ui.nabu.casa",
						"canonical_origin":"https://unit.ui.nabu.casa",
						"oauth_client_id":"http://127.0.0.1:43123/ha-nova",
						"credential_generation":"0123456789abcdef0123456789abcdef"
					},
					"sign_in_awaiting_browser":{
						"origin":"https://unit.ui.nabu.casa",
						"canonical_origin":"https://unit.ui.nabu.casa",
						"oauth_client_id":"http://127.0.0.1:43123/ha-nova",
						"credential_generation":"0123456789abcdef0123456789abcdef"
					}
				}
			}
		}
	}`)
	store, err := NewOAuthSecretStore(newMemoryOAuthSecretBackend(), "profile-unfinished")
	if err != nil {
		t.Fatal(err)
	}
	oldStore := newCloudSecretStoreForCLI
	oldRevoke := revokeAndVerifyCloudAuthorizationForCLI
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) { return store, nil }
	revokeAndVerifyCloudAuthorizationForCLI = func(context.Context, OAuthSecretEnvelope) error {
		t.Fatal("nothing to revoke for an unfinished sign-in")
		return nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
		revokeAndVerifyCloudAuthorizationForCLI = oldRevoke
	})

	report := &uninstallReport{}
	if err := purgeCloudAuthorizationsForUninstall(paths, report); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if len(report.notes) != 1 || report.notes[0] != unfinishedSignInRemovedText {
		t.Fatalf("notes = %q", report.notes)
	}
}

func TestBackToStableNotice(t *testing.T) {
	got := backToStableNotice("0.27.0-rc1", "0.26.0")
	want := []string{
		"You have the test release v0.27.0-rc1. This goes back to the stable release v0.26.0.",
		"To get the test release again later, run: ha-nova update --version v0.27.0-rc1",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("notice = %q", got)
	}
	for _, pair := range [][2]string{
		{"0.27.0-rc3", "0.27.0"}, // forward to its own stable
		{"0.26.0", "0.25.0"},
		{"0.27.0-rc1", "0.27.0-rc2"},
	} {
		if lines := backToStableNotice(pair[0], pair[1]); lines != nil {
			t.Fatalf("%s -> %s notice = %q", pair[0], pair[1], lines)
		}
	}
}

func TestCloudRemoveDropsUnfinishedSignIn(t *testing.T) {
	paths, store, backend, _ := cloudRemoveCommandFixture(t)
	if err := store.DeleteCurrent(context.Background(), SecretStoreForbidUI); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	metadata := *cfg.Cloud.Current
	metadata.HAUserID = ""
	marker := metadata
	cfg.RoutePolicy = routePolicyLocal
	cfg.Cloud = &cloudLifecycleMetadata{
		State:                 cloudStateAuthorizing,
		Pending:               &metadata,
		SignInAwaitingBrowser: &marker,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudRemoveStore(t, store, func(context.Context, OAuthSecretEnvelope) error {
		t.Fatal("nothing to revoke for an unfinished sign-in")
		return nil
	})
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 || !strings.Contains(output, unfinishedSignInRemovedText) {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	after, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if after.Cloud != nil {
		t.Fatalf("unfinished sign-in left Link state: %+v", after.Cloud)
	}
}
