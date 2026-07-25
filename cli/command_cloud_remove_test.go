package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func cloudRemoveCommandFixture(
	t *testing.T,
) (runtimePaths, *KeyringOAuthSecretStore, *memoryOAuthSecretBackend, OAuthSecretEnvelope) {
	t.Helper()
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	backend := newMemoryOAuthSecretBackend()
	store := productionCloudTestStore(t, backend)
	pending, err := store.CreatePending(
		context.Background(),
		productionCloudTestEnvelope(),
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	current, err := store.PromotePending(
		context.Background(),
		pending.Generation,
		SecretStoreAllowUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, current)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-1"
	cfg.RelayInstanceID = "relay-1"
	cfg.RoutePolicy = routePolicyAutomatic
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &metadata,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths, store, backend, current
}

func installCloudRemoveStore(
	t *testing.T,
	store OAuthSecretStore,
	revoke OAuthSecretRevoker,
) {
	t.Helper()
	oldStore := newCloudSecretStoreForCLI
	oldRevoke := revokeAndVerifyCloudAuthorizationForCLI
	newCloudSecretStoreForCLI = func(profileID string) (OAuthSecretStore, error) {
		if profileID != "profile-1" {
			t.Fatalf("secret-store profile = %q", profileID)
		}
		return store, nil
	}
	revokeAndVerifyCloudAuthorizationForCLI = revoke
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = oldStore
		revokeAndVerifyCloudAuthorizationForCLI = oldRevoke
	})
}

func TestCloudRemoveKeepsConfigAndSecretWhenRevocationFails(t *testing.T) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	revokeFailure := newCloudError(
		CloudErrNetwork,
		"revoke test authorization",
		errors.New("offline"),
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			return revokeFailure
		},
	)
	resetProductionCloudPolicies(backend)
	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	if !strings.Contains(output, "Cloud configuration was kept") ||
		!strings.Contains(output, string(cloudProblemUnavailable)) {
		t.Fatalf("missing fail-closed removal guidance:\n%s", output)
	}
	if strings.Contains(output, current.RefreshToken) {
		t.Fatal("removal failure exposed the refresh token")
	}
	held, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if held.Cloud == nil ||
		held.Cloud.Current == nil ||
		held.Cloud.RecoveryHold != nil {
		t.Fatalf("retryable network failure changed Cloud state: %+v", held.Cloud)
	}
	remaining, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists || remaining.Generation != current.Generation {
		t.Fatalf(
			"failed revocation lost current secret: exists=%v current=%+v err=%v",
			exists,
			remaining,
			err,
		)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
}

func TestCloudRemovePreservesUnrelatedInvalidSibling(t *testing.T) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	top := readTestConfigTopLevel(t, paths)
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	brokenSibling := json.RawMessage(
		`{"profile_id":"profile-broken","route_policy":"cloud"}`,
	)
	servers["broken-sibling"] = brokenSibling
	rawServers, err := json.Marshal(servers)
	if err != nil {
		t.Fatal(err)
	}
	top["servers"] = rawServers
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}

	var revoked []string
	installCloudRemoveStore(
		t,
		store,
		func(_ context.Context, envelope OAuthSecretEnvelope) error {
			revoked = append(revoked, envelope.Generation)
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	if len(revoked) != 1 || revoked[0] != current.Generation {
		t.Fatalf("revoked generations = %v", revoked)
	}
	if _, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf("revoked current remained: exists=%v err=%v", exists, err)
	}
	after := readTestConfigTopLevel(t, paths)
	var savedServers map[string]json.RawMessage
	if err := json.Unmarshal(after["servers"], &savedServers); err != nil {
		t.Fatal(err)
	}
	var beforeSibling, afterSibling map[string]json.RawMessage
	if err := json.Unmarshal(brokenSibling, &beforeSibling); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(savedServers["broken-sibling"], &afterSibling); err != nil {
		t.Fatal(err)
	}
	if string(afterSibling["profile_id"]) != string(beforeSibling["profile_id"]) ||
		string(afterSibling["route_policy"]) != string(beforeSibling["route_policy"]) {
		t.Fatalf(
			"unrelated invalid sibling changed: before=%s after=%s",
			brokenSibling,
			savedServers["broken-sibling"],
		)
	}
}

func TestCloudRemoveRejectsUnknownCloudStateBeforeRevocation(t *testing.T) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	top := readTestConfigTopLevel(t, paths)
	var servers map[string]map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	var lifecycle map[string]json.RawMessage
	if err := json.Unmarshal(servers["default"]["cloud"], &lifecycle); err != nil {
		t.Fatal(err)
	}
	lifecycle["future_secret_slot"] = json.RawMessage(`{"generation":"future"}`)
	rawLifecycle, err := json.Marshal(lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	servers["default"]["cloud"] = rawLifecycle
	rawServers, err := json.Marshal(servers)
	if err != nil {
		t.Fatal(err)
	}
	top["servers"] = rawServers
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}

	var revoked []string
	installCloudRemoveStore(
		t,
		store,
		func(_ context.Context, envelope OAuthSecretEnvelope) error {
			revoked = append(revoked, envelope.Generation)
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 ||
		!strings.Contains(output, "cannot safely remove") ||
		!strings.Contains(output, "update HA NOVA") {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	if len(revoked) != 0 {
		t.Fatalf("unknown Cloud state was partially revoked: %v", revoked)
	}
	remaining, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists || remaining.Generation != current.Generation {
		t.Fatalf(
			"unknown-state refusal lost current secret: exists=%v current=%+v err=%v",
			exists,
			remaining,
			err,
		)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(before) ||
		!strings.Contains(string(after), `"future_secret_slot"`) ||
		!strings.Contains(string(after), `"recovery_hold"`) {
		t.Fatalf("unknown-state refusal did not preserve unknown state with hold: %s", after)
	}
}

func TestCloudRemovePrintsExactUnlockCommandWhenStorageIsLocked(t *testing.T) {
	paths, store, backend, _ := cloudRemoveCommandFixture(t)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			return newCloudError(
				CloudErrSecretStoreLocked,
				"revoke test authorization",
				errors.New("locked"),
			)
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	if !strings.Contains(
		output,
		"ha-nova cloud unlock --server default",
	) {
		t.Fatalf("missing exact unlock command:\n%s", output)
	}
}

func TestCloudRemoveRejectsUnsafeSelectedProfileName(t *testing.T) {
	paths := setupServerCommandTest(t, `{
		"schema_version": 3,
		"default_server": "bad name",
		"servers": {
			"bad name": {
				"profile_id": "profile-bad-name",
				"route_policy": "local",
				"relay_base_url": "http://ha:8791"
			}
		}
	}`)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 ||
		!strings.Contains(output, "invalid selected server profile") {
		t.Fatalf("unsafe profile remove exit=%d output=%s", exit, output)
	}
}

func TestCloudRemoveDeletesOnlyAfterVerifiedRevocation(t *testing.T) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	var revoked []string
	installCloudRemoveStore(
		t,
		store,
		func(_ context.Context, envelope OAuthSecretEnvelope) error {
			revoked = append(revoked, envelope.Generation)
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	if len(revoked) != 1 || revoked[0] != current.Generation {
		t.Fatalf("revoked generations = %v", revoked)
	}
	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud != nil || saved.RoutePolicy != routePolicyLocal {
		t.Fatalf("Cloud removal checkpoint = %+v", saved)
	}
	if saved.RelayInstanceID != "relay-1" {
		t.Fatalf("local-capable removal lost Relay identity %q", saved.RelayInstanceID)
	}
	if _, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf("revoked current remained: exists=%v err=%v", exists, err)
	}
	assertProductionCloudPolicies(t, backend, SecretStoreForbidUI)
}

func TestCloudRemoveClearsRelayIdentityWithoutLocalPairing(t *testing.T) {
	paths, store, backend, _ := cloudRemoveCommandFixture(t)
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RelaySecureBaseURL = ""
	cfg.RelaySpkiPin = ""
	cfg.RoutePolicy = routePolicyCloud
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := writeDeviceCredential(validCredential(77)); err != nil {
		t.Fatal(err)
	}
	installRemoteDeviceRevokeHook(
		t,
		func(context.Context, runtimeConfig, OAuthSecretStore, string) error {
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error { return nil },
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("cloud remove exit=%d output=%s", exit, output)
	}
	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RelayInstanceID != "" {
		t.Fatalf("Cloud-only removal retained stale Relay identity %q", saved.RelayInstanceID)
	}
}
