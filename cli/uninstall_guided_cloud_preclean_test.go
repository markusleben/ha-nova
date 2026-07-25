package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestGuidedPurgeRevokesCloudDeviceBeforeHomeAssistantRemoval(
	t *testing.T,
) {
	paths, store, backend, current, credential :=
		remoteOnlyCloudRemovalFixture(t)
	deviceRevokes := 0
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			cfg runtimeConfig,
			gotStore OAuthSecretStore,
			gotCredential string,
		) error {
			deviceRevokes++
			if cfg.RelayInstanceID != "relay-1" ||
				gotStore != store ||
				gotCredential != credential {
				t.Fatalf(
					"Cloud device revoke provenance cfg=%+v store=%T credential=%q",
					cfg,
					gotStore,
					gotCredential,
				)
			}
			return nil
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

	if err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	); err != nil {
		t.Fatal(err)
	}
	if deviceRevokes != 1 || oauthRevokes != 0 {
		t.Fatalf(
			"pre-guided revokes: device=%d OAuth=%d",
			deviceRevokes,
			oauthRevokes,
		)
	}
	if _, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	); err != nil || exists {
		t.Fatalf(
			"pre-guided Cloud device slot: exists=%v err=%v",
			exists,
			err,
		)
	}
	if _, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || !exists {
		t.Fatalf(
			"pre-guided OAuth current: exists=%v err=%v",
			exists,
			err,
		)
	}
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cloud == nil ||
		cfg.Cloud.DeviceRevocationCompleted == nil ||
		cfg.Cloud.DeviceRevocationCompleted.CurrentDeviceID !=
			deviceIDOf(credential) {
		t.Fatalf("pre-guided Cloud device checkpoint = %+v", cfg.Cloud)
	}

	if err := purgeCloudAuthorizationsForUninstall(
		paths,
		&uninstallReport{},
	); err != nil {
		t.Fatal(err)
	}
	if deviceRevokes != 1 {
		t.Fatalf("final purge repeated Cloud device revoke: %d", deviceRevokes)
	}
	if oauthRevokes != 1 {
		t.Fatalf("final purge OAuth revokes = %d", oauthRevokes)
	}
	if _, exists, err := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); err != nil || exists {
		t.Fatalf(
			"final OAuth current: exists=%v err=%v generation=%q",
			exists,
			err,
			current.Generation,
		)
	}
}

func TestGuidedPurgeCloudDeviceFailureStopsBeforeHomeAssistantRemoval(
	t *testing.T,
) {
	paths, store, backend, _, _ := remoteOnlyCloudRemovalFixture(t)
	revokeFailure := newCloudError(
		CloudErrNetwork,
		"revoke Cloud device before guided teardown",
		errors.New("relay temporarily unavailable"),
	)
	installRemoteDeviceRevokeHook(
		t,
		func(
			context.Context,
			runtimeConfig,
			OAuthSecretStore,
			string,
		) error {
			return revokeFailure
		},
	)
	installCloudRemoveStore(
		t,
		store,
		func(context.Context, OAuthSecretEnvelope) error {
			t.Fatal("OAuth revoke ran after Cloud device failure")
			return nil
		},
	)
	resetProductionCloudPolicies(backend)

	err := prepareUninstallBeforeGuidedTeardown(
		paths,
		uninstallModePurge,
	)
	if err == nil ||
		!strings.Contains(err.Error(), "before Home Assistant removal") {
		t.Fatalf("pre-guided Cloud device error = %v", err)
	}
	if _, exists, readErr := readCredentialSlot(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	); readErr != nil || !exists {
		t.Fatalf(
			"failed pre-guided Cloud device slot: exists=%v err=%v",
			exists,
			readErr,
		)
	}
	if _, exists, readErr := store.LoadCurrent(
		context.Background(),
		SecretStoreForbidUI,
	); readErr != nil || !exists {
		t.Fatalf(
			"failed pre-guided OAuth current: exists=%v err=%v",
			exists,
			readErr,
		)
	}
	cfg, loadErr := loadSelectedRuntimeConfigUnchecked(paths)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if cfg.Cloud == nil || cfg.Cloud.Current == nil {
		t.Fatalf("failed pre-guided cleanup lost recovery state: %+v", cfg.Cloud)
	}
}
