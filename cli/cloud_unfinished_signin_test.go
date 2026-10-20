package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func unfinishedSignInTestConfig(t *testing.T) runtimeConfig {
	t.Helper()
	cfg, _ := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	cfg.Cloud.Pending = nil
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

// Pending means an exchange may have run: it still needs its credential,
// whoever wrote it (an older version included).
func TestPendingWithoutCredentialStillStops(t *testing.T) {
	cfg, _ := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())

	plan, err := inspectCloudAuthorizationCleanup(context.Background(), cfg, store)
	if !errors.Is(err, errCloudAuthorizationCleanupUnverifiable) || plan.unfinishedSignIn {
		t.Fatalf("plan = %+v err = %v", plan, err)
	}
}

// A stored credential is revoked even with no Pending entry.
func TestUnfinishedSignInMarkerNeverHidesStoredCredential(t *testing.T) {
	cfg := unfinishedSignInTestConfig(t)
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())
	envelope := productionCloudTestEnvelope()
	envelope.Generation = "0123456789abcdef0123456789abcdef"
	if _, err := store.CreatePending(context.Background(), envelope, SecretStoreForbidUI); err != nil {
		t.Fatal(err)
	}

	plan, err := inspectCloudAuthorizationCleanup(context.Background(), cfg, store)
	if err != nil || !plan.hasPending {
		t.Fatalf("plan = %+v err = %v", plan, err)
	}
}

func TestUnprovenSignInStopIsPlain(t *testing.T) {
	cfg, _ := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	store := productionCloudTestStore(t, newMemoryOAuthSecretBackend())
	_, err := inspectCloudAuthorizationCleanup(context.Background(), cfg, store)
	err = cloudAuthorizationCleanupErrorWithRecoveryCommand(err, defaultServerProfileName)
	wrapped := errors.Join(err, errors.New("persist Cloud recovery safety hold: disk full"))

	text := uninstallErrorText(errorsWrap("failed to revoke Home Assistant Link authorization", wrapped))
	want := strings.Join([]string{
		"Home Assistant Link: Your sign-in on this computer didn't finish. HA NOVA can't tell if Home Assistant saved it, so it stopped.",
		"Next, in Home Assistant:",
		`1. As the owner, open NOVA. If this computer is listed with a "cloud" tag, click Revoke next to it.`,
		`2. Each person who uses HA NOVA: open Profile > Security, and under "Refresh tokens", delete every entry that ends in "/ha-nova". This also signs out HA NOVA on your other computers.`,
		"3. Run: ha-nova cloud remove --server default --yes --confirm-remote-access-revoked default",
		"4. Then run ha-nova uninstall again.",
		"(code cloud_authorization; next: inspect_security)",
		"persist Cloud recovery safety hold: disk full",
	}, "\n")
	if text != want {
		t.Fatalf("text:\n%s", text)
	}
}

// The problem's own cause (a binding error) is support detail, not a step.
func TestCloudStopTextOmitsInnerCause(t *testing.T) {
	err := cloudAuthorizationCleanupErrorWithRecoveryCommand(
		unverifiableCloudAuthorizationCleanup(errors.New("resume Cloud authorization: IDENTITY_MISMATCH")),
		defaultServerProfileName,
	)
	text := uninstallErrorText(errorsWrap("inspect Cloud credentials", err))
	if strings.Contains(text, "IDENTITY_MISMATCH") || !strings.Contains(text, `ends in "/ha-nova"`) {
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

func TestSignInWritesPendingOnlyAtCodeExchange(t *testing.T) {
	paths := writeTestConfigFile(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-marker"
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	save := func(value runtimeConfig) error { return saveConfig(paths, value) }
	_, metadata := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	reload := func() *cloudLifecycleMetadata {
		t.Helper()
		loaded, err := loadSelectedRuntimeConfigUnchecked(paths)
		if err != nil {
			t.Fatal(err)
		}
		return loaded.Cloud
	}

	request := newCloudSetupRequest(&cfg, save)
	if err := request.PersistPendingSignIn(metadata); err != nil {
		t.Fatal(err)
	}
	if lifecycle := reload(); lifecycle.Pending != nil || !lifecycle.unfinishedSignIn() {
		t.Fatalf("browser step wrote %+v", lifecycle)
	}
	if err := request.StartSignInCodeExchange(); err != nil {
		t.Fatal(err)
	}
	if lifecycle := reload(); lifecycle.Pending == nil || *lifecycle.Pending != metadata {
		t.Fatalf("exchange step wrote %+v", lifecycle)
	}
	// A Pending entry stays the record of truth on a new browser attempt.
	if err := request.PersistPendingSignIn(metadata); err != nil {
		t.Fatal(err)
	}
	if err := request.StartSignInCodeExchange(); err != nil {
		t.Fatal(err)
	}
	if lifecycle := reload(); lifecycle.Pending == nil || *lifecycle.Pending != metadata {
		t.Fatalf("retry after an exchange wrote %+v", lifecycle)
	}
}

func TestOAuthSignInSavesPendingBeforeCodeExchange(t *testing.T) {
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
		if lifecycle := onDisk(); lifecycle.Pending == nil {
			t.Fatalf("code exchange ran before Pending was saved: %+v", lifecycle)
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
					"state":"authorizing"
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
	cfg.RoutePolicy = routePolicyLocal
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
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

func TestCloudStopTextKeepsNestedJoinedFailures(t *testing.T) {
	stop := cloudAuthorizationCleanupErrorWithRecoveryCommand(
		unverifiableCloudAuthorizationCleanup(nil),
		defaultServerProfileName,
	)
	err := errors.Join(
		errors.Join(stop, errors.New("persist Cloud recovery safety hold: disk full")),
		errors.New("reset storage proof: busy"),
	)
	text, ok := cloudAuthorizationStopText(err)
	if !ok || !strings.Contains(text, "disk full") || !strings.Contains(text, "busy") ||
		strings.Count(text, "Next, in Home Assistant:") != 1 {
		t.Fatalf("text:\n%s", text)
	}
}

// A stored grant with another address (a retiring one after a reconnect)
// must not be left out of the steps.
func TestCloudStopNamesOneSignInOnlyWhenItIsTheOnlyOne(t *testing.T) {
	cfg, metadata := cloudCleanupTestConfig(t, cloudStateRetiringPrevious)
	cfg.Cloud.Current, cfg.Cloud.Pending = &metadata, nil
	plan := cloudAuthorizationCleanupPlan{}
	if got := plan.onlyLinkClient(cfg); got != metadata.OAuthClientID {
		t.Fatalf("single grant client = %q", got)
	}
	plan.retiring = productionCloudTestEnvelope()
	plan.retiring.ClientID = "http://127.0.0.1:40001/ha-nova"
	plan.hasRetiring = true
	if got := plan.onlyLinkClient(cfg); got != "" {
		t.Fatalf("two grants named one client %q", got)
	}
}

// A Pending entry without its credential may have replaced the address of an
// earlier exchange, so it never names one sign-in.
func TestCloudStopNeverNamesUnstoredPendingClient(t *testing.T) {
	cfg, _ := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	if got := (cloudAuthorizationCleanupPlan{}).onlyLinkClient(cfg); got != "" {
		t.Fatalf("unstored pending named %q", got)
	}
}

func TestCloudStopTextFindsTheStopProblem(t *testing.T) {
	other := &cloudProblem{Code: cloudProblemUnavailable, Remediation: cloudRemediationVerifyState, Detail: "relay busy"}
	stop := cloudAuthorizationCleanupErrorWithRecoveryCommand(
		unverifiableCloudAuthorizationCleanup(nil),
		defaultServerProfileName,
	)
	text, ok := cloudAuthorizationStopText(errors.Join(other, errorsWrap("inspect", stop)))
	if !ok || !strings.Contains(text, "1. As the owner") || !strings.Contains(text, "relay busy") {
		t.Fatalf("text:\n%s", text)
	}
}

func TestCodeExchangeNeedsSavedPending(t *testing.T) {
	cfg := runtimeConfig{Cloud: &cloudLifecycleMetadata{State: cloudStateAuthorizing}}
	request := newCloudSetupRequest(&cfg, func(runtimeConfig) error { return nil })
	if err := request.StartSignInCodeExchange(); err == nil {
		t.Fatal("code exchange allowed with no saved sign-in")
	}
}

func TestFailedPendingSaveLeavesNoPendingInMemory(t *testing.T) {
	cfg := runtimeConfig{Cloud: &cloudLifecycleMetadata{State: cloudStateAuthorizing}}
	_, metadata := cloudCleanupTestConfig(t, cloudStateAuthorizing)
	request := newCloudSetupRequest(&cfg, func(runtimeConfig) error { return errors.New("disk full") })
	if err := request.PersistPendingSignIn(metadata); err != nil {
		t.Fatal(err)
	}
	if err := request.StartSignInCodeExchange(); err == nil || cfg.Cloud.Pending != nil {
		t.Fatalf("err=%v pending=%+v", err, cfg.Cloud.Pending)
	}
}
