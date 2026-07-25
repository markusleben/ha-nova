package main

import (
	"context"
	"errors"
	"testing"
)

func TestDeviceRevocationCheckpointSaveFailureKeepsEverySecret(
	t *testing.T,
) {
	cfg, current, pending := activationUncertainCloudDeviceFixture(t)
	revokes := 0
	installRemoteDeviceRevokeHook(
		t,
		func(
			context.Context,
			runtimeConfig,
			OAuthSecretStore,
			string,
		) error {
			revokes++
			return nil
		},
	)
	saveErr := errors.New("simulated revocation checkpoint save failure")
	removed, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		context.Background(),
		cfg,
		remoteOnlyCloudTestProfile,
		nil,
		nil,
		func(cloudDeviceRevocationCheckpoint) error {
			return saveErr
		},
	)
	if removed || !errors.Is(err, saveErr) || revokes != 2 {
		t.Fatalf(
			"checkpoint failure removed=%v revokes=%d err=%v",
			removed,
			revokes,
			err,
		)
	}
	assertCloudDeviceCredentialSlot(
		t,
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
		current,
	)
	record, exists, readErr := readPendingDeviceCredentialRecord()
	if readErr != nil || !exists || record.Credential != pending {
		t.Fatalf(
			"checkpoint failure pending=%+v exists=%v err=%v",
			record,
			exists,
			readErr,
		)
	}
}

func TestDeviceRevocationRetrySkipsRemoteAndToleratesDeletedSlot(
	t *testing.T,
) {
	cfg, current, pending := activationUncertainCloudDeviceFixture(t)
	cfg.Cloud.DeviceRevocationCompleted =
		&cloudDeviceRevocationCheckpoint{
			CurrentDeviceID: deviceIDOf(current),
			PendingDeviceID: deviceIDOf(pending),
		}
	if err := secretDelete(
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
	); err != nil {
		t.Fatal(err)
	}
	installRemoteDeviceRevokeHook(
		t,
		func(
			context.Context,
			runtimeConfig,
			OAuthSecretStore,
			string,
		) error {
			t.Fatal("checkpoint retry repeated remote revocation")
			return nil
		},
	)
	removed, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		context.Background(),
		cfg,
		remoteOnlyCloudTestProfile,
		nil,
		nil,
		func(cloudDeviceRevocationCheckpoint) error {
			t.Fatal("checkpoint retry rewrote durable progress")
			return nil
		},
	)
	if err != nil || !removed {
		t.Fatalf("checkpoint retry removed=%v err=%v", removed, err)
	}
	if _, exists, err := readPendingDeviceCredentialRecord(); err != nil ||
		exists {
		t.Fatalf("checkpoint retry pending exists=%v err=%v", exists, err)
	}
}

func TestDeviceRevocationRetryRejectsReplacedExpectedSlot(
	t *testing.T,
) {
	cfg, current, pending := activationUncertainCloudDeviceFixture(t)
	cfg.Cloud.DeviceRevocationCompleted =
		&cloudDeviceRevocationCheckpoint{
			CurrentDeviceID: deviceIDOf(current),
			PendingDeviceID: deviceIDOf(pending),
		}
	replacement := validCredential(118)
	if err := writePendingCloudDeviceCredential(
		replacement,
		cfg.RelayInstanceID,
	); err != nil {
		t.Fatal(err)
	}
	removed, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		context.Background(),
		cfg,
		remoteOnlyCloudTestProfile,
		nil,
		nil,
		acceptCloudDeviceRevocationCheckpoint,
	)
	if removed || !IsCloudErrorCode(err, CloudErrIdentityMismatch) {
		t.Fatalf("replaced retry removed=%v err=%v", removed, err)
	}
	assertCloudDeviceCredentialSlot(
		t,
		deviceCredentialServiceForProfile(remoteOnlyCloudTestProfile),
		current,
	)
	record, exists, readErr := readPendingDeviceCredentialRecord()
	if readErr != nil || !exists || record.Credential != replacement {
		t.Fatalf(
			"replaced retry pending=%+v exists=%v err=%v",
			record,
			exists,
			readErr,
		)
	}
}

func TestDeviceBoundCleanupUsesExactPromotedCurrentWithoutPending(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	setActiveServerProfile(remoteOnlyCloudTestProfile)
	credential := validCredential(119)
	deviceID := deviceIDOf(credential)
	if err := writeDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}
	cfg := remotePromotedDeviceRecoveryConfig(t, deviceID)
	cfg.RoutePolicy = routePolicyCloud
	revokes := 0
	installRemoteDeviceRevokeHook(
		t,
		func(
			_ context.Context,
			got runtimeConfig,
			_ OAuthSecretStore,
			value string,
		) error {
			revokes++
			if value != credential ||
				got.Cloud == nil ||
				!got.Cloud.DeviceActivationStarted {
				t.Fatalf("promoted revoke cfg=%+v value=%q", got, value)
			}
			return nil
		},
	)
	var checkpoint cloudDeviceRevocationCheckpoint
	removed, err := revokeRemoteOnlyCloudDeviceBeforeOAuth(
		context.Background(),
		cfg,
		remoteOnlyCloudTestProfile,
		nil,
		nil,
		func(value cloudDeviceRevocationCheckpoint) error {
			checkpoint = value
			return nil
		},
	)
	if err != nil || !removed || revokes != 1 ||
		checkpoint.CurrentDeviceID != deviceID ||
		checkpoint.PendingDeviceID != "" {
		t.Fatalf(
			"promoted cleanup removed=%v revokes=%d checkpoint=%+v err=%v",
			removed,
			revokes,
			checkpoint,
			err,
		)
	}
	if _, exists, err := readDeviceCredential(); err != nil || exists {
		t.Fatalf("promoted cleanup current exists=%v err=%v", exists, err)
	}
}

func assertCloudDeviceCredentialSlot(
	t *testing.T,
	service string,
	want string,
) {
	t.Helper()
	got, exists, err := readCredentialSlot(service)
	if err != nil || !exists || got != want {
		t.Fatalf(
			"credential slot %q=%q exists=%v err=%v",
			service,
			got,
			exists,
			err,
		)
	}
}
