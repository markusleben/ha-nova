package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCloudRemoveManualRecoveryAcceptsMissingCurrentDevice(
	t *testing.T,
) {
	paths, store, backend, _, _ := remoteOnlyCloudRemovalFixture(t)
	if err := secretDelete(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	); err != nil {
		t.Fatal(err)
	}
	remoteCalls := 0
	installRemoteDeviceRevokeHook(
		t,
		func(context.Context, runtimeConfig, OAuthSecretStore, string) error {
			remoteCalls++
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			remoteCalls++
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{
			"--yes",
			"--confirm-remote-access-revoked",
			remoteOnlyCloudTestProfile,
		})
	})
	if exit != 0 {
		t.Fatalf("manual missing-device exit=%d output=%s", exit, output)
	}
	if remoteCalls != 0 {
		t.Fatalf("manual recovery made %d remote calls", remoteCalls)
	}
	if _, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf("current OAuth exists=%v err=%v", exists, err)
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud != nil || cfg.RoutePolicy != routePolicyLocal {
		t.Fatalf("manual recovery result = %+v", cfg)
	}
}

func TestCloudRemoveManualRecoveryRejectsCorruptCurrentDevice(
	t *testing.T,
) {
	paths, store, backend, _, _ := remoteOnlyCloudRemovalFixture(t)
	service := deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile)
	if err := secretSet(service, "corrupt"); err != nil {
		t.Fatal(err)
	}
	remoteCalls := 0
	installRemoteDeviceRevokeHook(
		t,
		func(context.Context, runtimeConfig, OAuthSecretStore, string) error {
			remoteCalls++
			return nil
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			remoteCalls++
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, []string{
			"--yes",
			"--confirm-remote-access-revoked",
			remoteOnlyCloudTestProfile,
		})
	})
	if exit != 1 {
		t.Fatalf("corrupt-device exit=%d output=%s", exit, output)
	}
	if remoteCalls != 0 {
		t.Fatalf("corrupt-device recovery made %d remote calls", remoteCalls)
	}
	value, err := secretGet(service)
	if err != nil || value != "corrupt" {
		t.Fatalf("corrupt slot value=%q err=%v", value, err)
	}
}

func TestCloudRemoveManualActivationRecoveryCheckpointsBeforeOAuthDelete(
	t *testing.T,
) {
	paths, store, backend, _, _ := remoteOnlyCloudRemovalFixture(t)
	if err := secretDelete(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	pendingEnvelope := productionCloudTestEnvelope()
	pendingEnvelope.Generation = strings.Repeat("e", 32)
	pendingEnvelope.ClientID = "http://127.0.0.1:54322/ha-nova"
	pending, err := store.CreatePending(
		context.Background(),
		pendingEnvelope,
		SecretStoreForbidUI,
	)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := cloudOriginFromCanonical(pending.CanonicalOrigin)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, pending)
	expectedCredential := validCredential(202)
	cfg.Cloud.State = cloudStateCloudVerified
	cfg.Cloud.Pending = &metadata
	cfg.Cloud.DeviceActivationStarted = true
	cfg.Cloud.DeviceActivationDeviceID = deviceIDOf(expectedCredential)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	deleteErr := errors.New("simulated pending OAuth delete failure")
	backend.fail = func(operation, service string) error {
		if operation == "delete" && service == oauthSecretPendingService {
			return deleteErr
		}
		return nil
	}
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			t.Fatal("manual recovery reached remote OAuth revocation")
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	args := []string{
		"--yes",
		"--confirm-remote-access-revoked",
		remoteOnlyCloudTestProfile,
	}
	exit, output := captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, args)
	})
	if exit != 1 {
		t.Fatalf("delete-failure exit=%d output=%s", exit, output)
	}
	checkpointed, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if checkpointed.Cloud == nil ||
		checkpointed.Cloud.DeviceRevocationCompleted == nil ||
		checkpointed.Cloud.DeviceRevocationCompleted.PendingDeviceID !=
			deviceIDOf(expectedCredential) {
		t.Fatalf("manual device checkpoint = %+v", checkpointed.Cloud)
	}

	backend.fail = nil
	exit, output = captureCommandOutput(t, func() int {
		return runCloudRemoveCommand(paths, args)
	})
	if exit != 0 {
		t.Fatalf("checkpoint retry exit=%d output=%s", exit, output)
	}
	finished, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Cloud != nil {
		t.Fatalf("checkpoint retry retained Cloud state: %+v", finished.Cloud)
	}
}
