package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

const remoteOnlyCloudTestProfile = "cabin"

func remoteOnlyCloudRemovalFixture(
	t *testing.T,
) (
	runtimePaths,
	*KeyringOAuthSecretStore,
	*memoryOAuthSecretBackend,
	OAuthSecretEnvelope,
	string,
) {
	t.Helper()
	paths := setupServerCommandTest(t, `{
		"schema_version":3,
		"default_server":"default",
		"servers":{
			"default":{
				"profile_id":"profile-default",
				"route_policy":"local",
				"relay_base_url":"http://ha:8791"
			}
		}
	}`)
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
	cfg := runtimeConfig{
		ProfileID:       "profile-1",
		RelayInstanceID: "relay-1",
		RoutePolicy:     routePolicyCloud,
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &metadata,
		},
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	servers, err := documentServersCopy(doc)
	if err != nil {
		t.Fatal(err)
	}
	rawProfile, err := json.Marshal(serverProfileFromRuntime(cfg))
	if err != nil {
		t.Fatal(err)
	}
	servers[remoteOnlyCloudTestProfile] = rawProfile
	if err := writeServersDocument(
		paths,
		doc,
		servers,
		defaultServerProfileName,
	); err != nil {
		t.Fatal(err)
	}
	t.Setenv(serverSelectionEnvVar, remoteOnlyCloudTestProfile)
	credential := validCredential(20)
	if err := secretSet(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
		credential,
	); err != nil {
		t.Fatal(err)
	}
	return paths, store, backend, current, credential
}

func installRemoteDeviceRevokeHook(
	t *testing.T,
	hook func(
		context.Context,
		runtimeConfig,
		OAuthSecretStore,
		string,
	) error,
) {
	t.Helper()
	previous := revokeRemoteCloudDeviceForCLI
	revokeRemoteCloudDeviceForCLI = hook
	t.Cleanup(func() {
		revokeRemoteCloudDeviceForCLI = previous
	})
}

func TestCloudRemoveRevokesRemoteOnlyDeviceBeforeOAuthAndEnablesServerRemove(
	t *testing.T,
) {
	paths, store, backend, _, credential :=
		remoteOnlyCloudRemovalFixture(t)
	var events []string
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			cfg runtimeConfig,
			gotStore OAuthSecretStore,
			gotCredential string,
		) error {
			events = append(events, "device")
			if cfg.RelayInstanceID != "relay-1" ||
				gotStore != store ||
				gotCredential != credential {
				t.Fatalf(
					"remote revoke provenance cfg=%+v store=%T credential=%q",
					cfg,
					gotStore,
					gotCredential,
				)
			}
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			events = append(events, "oauth")
			if _, exists, err := readCredentialSlot(
				deviceCredentialServiceForProfile(
					remoteOnlyCloudTestProfile,
				),
			); err != nil || exists {
				t.Fatalf(
					"device credential at OAuth revoke: exists=%v err=%v",
					exists,
					err,
				)
			}
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
	if len(events) != 2 ||
		events[0] != "device" ||
		events[1] != "oauth" {
		t.Fatalf("revocation order = %v", events)
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud != nil ||
		cfg.RelayInstanceID != "" ||
		cfg.RoutePolicy != routePolicyLocal {
		t.Fatalf("Cloud removal checkpoint = %+v", cfg)
	}

	localRevokes := stubServerRevoke(t)
	previousConfirm := readServerRemoveConfirmationForCommand
	readServerRemoveConfirmationForCommand = func(string) (string, error) {
		return remoteOnlyCloudTestProfile, nil
	}
	t.Cleanup(func() {
		readServerRemoveConfirmationForCommand = previousConfirm
	})
	exit, output = captureCommandOutput(t, func() int {
		return runServerCommand(
			paths,
			[]string{"remove", remoteOnlyCloudTestProfile},
		)
	})
	if exit != 0 {
		t.Fatalf("server remove exit=%d output=%s", exit, output)
	}
	if len(*localRevokes) != 0 {
		t.Fatalf("server remove retried a local revoke: %v", *localRevokes)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if doc.hasProfile(remoteOnlyCloudTestProfile) {
		t.Fatal("Cloud-removed server profile survived server remove")
	}
}

func TestRemoteOnlyFullPurgeRevokesDeviceBeforeOAuth(t *testing.T) {
	paths, store, backend, _, _ := remoteOnlyCloudRemovalFixture(t)
	var events []string
	installRemoteDeviceRevokeHook(
		t,
		func(
			context.Context,
			runtimeConfig,
			OAuthSecretStore,
			string,
		) error {
			events = append(events, "device")
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			events = append(events, "oauth")
			if _, exists, err := readCredentialSlot(
				deviceCredentialServiceForProfile(
					remoteOnlyCloudTestProfile,
				),
			); err != nil || exists {
				t.Fatalf(
					"device credential at OAuth purge: exists=%v err=%v",
					exists,
					err,
				)
			}
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	report := &uninstallReport{}
	if err := purgeCloudAuthorizationsForUninstall(
		paths,
		report,
	); err != nil {
		t.Fatalf("purge Cloud authorizations: %v", err)
	}
	if len(events) != 2 ||
		events[0] != "device" ||
		events[1] != "oauth" {
		t.Fatalf("purge revocation order = %v", events)
	}
}

func TestRemoteOnlyCloudRemovalKeepsOAuthAndDeviceOnAmbiguousRevoke(
	t *testing.T,
) {
	paths, store, backend, current, credential :=
		remoteOnlyCloudRemovalFixture(t)
	ambiguous := newCloudError(
		CloudErrOutcomeUnknown,
		"revoke Cloud device",
		errors.New("response lost"),
	)
	installRemoteDeviceRevokeHook(
		t,
		func(
			context.Context,
			runtimeConfig,
			OAuthSecretStore,
			string,
		) error {
			return ambiguous
		},
	)
	oauthRevokes := 0
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			oauthRevokes++
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{"--yes"})
	})
	if exit != 1 {
		t.Fatalf("ambiguous cloud remove exit=%d output=%s", exit, output)
	}
	if oauthRevokes != 0 {
		t.Fatalf("OAuth revoked after ambiguous device outcome: %d", oauthRevokes)
	}
	held, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if held.Cloud == nil ||
		held.Cloud.Current == nil ||
		held.Cloud.RecoveryHold == nil ||
		held.Cloud.RecoveryHold.Remediation != cloudRemediationVerifyState {
		t.Fatalf("ambiguous device outcome did not preserve current with hold: %+v", held.Cloud)
	}
	stored, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	)
	if err != nil || !exists || stored != credential {
		t.Fatalf(
			"device credential after ambiguity = %q, %v, %v",
			stored,
			exists,
			err,
		)
	}
	remaining, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	)
	if err != nil ||
		!exists ||
		remaining.Generation != current.Generation {
		t.Fatalf(
			"OAuth after ambiguity = %+v, %v, %v",
			remaining,
			exists,
			err,
		)
	}
}
