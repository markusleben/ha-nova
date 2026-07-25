package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCloudManagementRejectsDuplicateProfileIdentitiesBeforeMutation(
	t *testing.T,
) {
	paths := setupServerCommandTest(t, `{
		"schema_version": 3,
		"default_server": "default",
		"servers": {
			"default": {
				"profile_id": "profile-shared",
				"route_policy": "local",
				"relay_base_url": "http://ha:8791"
			},
			"cabin": {
				"profile_id": "profile-shared",
				"route_policy": "local",
				"relay_base_url": "http://cabin:8791"
			}
		}
	}`)
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	storeCalls := 0
	oldStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(string) (OAuthSecretStore, error) {
		storeCalls++
		return nil, errors.New("unexpected secure-storage access")
	}
	t.Cleanup(func() { newCloudSecretStoreForCLI = oldStore })

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(
			paths,
			[]string{"--server", "cabin", "--yes"},
		)
	})
	if exit != 1 ||
		storeCalls != 0 ||
		!strings.Contains(output, "share profile_id") {
		t.Fatalf(
			"duplicate identity exit=%d store_calls=%d output=%s",
			exit,
			storeCalls,
			output,
		)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("duplicate profile identities changed configuration")
	}
}

func TestCloudConnectRejectsUnsafeSelectedProfileName(t *testing.T) {
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
	if _, err := loadCloudConnectConfig(
		paths,
		cloudCommandFlags{},
		false,
	); err == nil || !strings.Contains(err.Error(), "invalid selected server profile") {
		t.Fatalf("unsafe selected profile error = %v", err)
	}
}

func TestFreshCloudConnectRequiresExplicitServerInsteadOfEnvironment(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv(serverSelectionEnvVar, "cabin")
	dir := t.TempDir()
	paths := runtimePaths{
		ConfigDir:  dir,
		ConfigFile: dir + "/config.json",
	}
	if _, err := loadCloudConnectConfig(
		paths,
		cloudCommandFlags{url: "https://example.ui.nabu.casa"},
		false,
	); err == nil ||
		!strings.Contains(err.Error(), "requires the explicit flag --server cabin") {
		t.Fatalf("environment-only fresh Cloud selection error = %v", err)
	}
	if activeServerProfile() != defaultServerProfileName {
		t.Fatalf(
			"failed environment-only creation changed active profile to %q",
			activeServerProfile(),
		)
	}
}

func TestCloudRemoveRejectsTopLevelCloudShadowBeforeRevocation(t *testing.T) {
	paths, store, backend, current := cloudRemoveCommandFixture(t)
	top := readTestConfigTopLevel(t, paths)
	top["cloud"] = json.RawMessage(`{"state":"future"}`)
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
	if exit != 1 || !strings.Contains(output, "cannot safely remove") {
		t.Fatalf("top-level Cloud shadow exit=%d output=%s", exit, output)
	}
	if len(revoked) != 0 {
		t.Fatalf("top-level Cloud shadow was partially revoked: %v", revoked)
	}
	remaining, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists || remaining.Generation != current.Generation {
		t.Fatalf(
			"shadow refusal lost current secret: exists=%v value=%+v err=%v",
			exists,
			remaining,
			err,
		)
	}
}

func TestCloudRemoveKeepsIdentityCheckpointUntilSecretDeletionFinishes(
	t *testing.T,
) {
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
	deleteErr := newCloudError(
		CloudErrSecretStore,
		"delete current OAuth secret",
		errors.New("keyring relocked"),
	)
	backend.fail = func(op, service string) error {
		if op == "delete" && service == oauthSecretCurrentService {
			return deleteErr
		}
		return nil
	}
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 || !strings.Contains(output, string(cloudProblemSecureStorage)) {
		t.Fatalf("delete failure exit=%d output=%s", exit, output)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("secret deletion failure cleared the Cloud identity checkpoint")
	}
	remaining, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil || !exists || remaining.Generation != current.Generation {
		t.Fatalf(
			"current after delete failure: exists=%v value=%+v err=%v",
			exists,
			remaining,
			err,
		)
	}
	targets, err := collectCloudPurgeTargets(paths.ConfigFile)
	if err != nil || len(targets) != 1 ||
		targets[0].profileID != "profile-1" {
		t.Fatalf("purge targets after delete failure = %+v err=%v", targets, err)
	}

	backend.fail = nil
	exit, output = captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 0 {
		t.Fatalf("retry exit=%d output=%s", exit, output)
	}
	if len(revoked) != 2 ||
		revoked[0] != current.Generation ||
		revoked[1] != current.Generation {
		t.Fatalf("retry revocations = %v", revoked)
	}
	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud != nil || saved.RoutePolicy != routePolicyLocal {
		t.Fatalf("retry did not finish Cloud removal: %+v", saved)
	}
}
