package main

import (
	"context"
	"testing"
)

func TestRemoteCloudConflictLeavesLocalPendingSetupResumable(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	pending := validCredential(26)
	if err := writePendingDeviceCredential(pending); err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{
		PendingSecureBaseURL: "https://local.example:8792",
		PendingSpkiPin:       "pin",
	}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := newSelectingCloudCoordinator()
	saveCalls := 0

	updated, err := connectRemoteToCloud(
		context.Background(),
		writeTestConfigFile(t, `{"schema_version":1}`),
		cfg,
		coordinator,
		origin,
		func(cloudRemotePairingPrompt) (string, error) {
			t.Fatal("local pending opened a Cloud pairing prompt")
			return "", nil
		},
		false,
		func(runtimeConfig) error {
			saveCalls++
			return nil
		},
	)
	if !IsCloudErrorCode(err, CloudErrDevicePendingConflict) {
		t.Fatalf("local-pending remote setup error = %v", err)
	}
	if saveCalls != 0 || coordinator.preflightCalls != 0 ||
		coordinator.remoteCalls != 0 {
		t.Fatalf(
			"blocked remote setup mutated state: saves=%d preflight=%d add=%d",
			saveCalls,
			coordinator.preflightCalls,
			coordinator.remoteCalls,
		)
	}
	if updated.Cloud != nil || updated.ProfileID != "" ||
		updated.ClientInstallID != "" {
		t.Fatalf("blocked remote setup changed config: %+v", updated)
	}

	previousActivate := activateDeviceV1ForPairing
	activateCalls := 0
	activateDeviceV1ForPairing = func(_, _, credential string) error {
		activateCalls++
		if credential != pending {
			t.Fatalf("resumed credential = %q", credential)
		}
		return nil
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = previousActivate
	})
	resumed, resumeErr := resumePendingActivation(
		&cfg,
		func(*runtimeConfig) error { return nil },
	)
	if resumeErr != nil || !resumed || activateCalls != 1 {
		t.Fatalf(
			"local pending resume=%v calls=%d err=%v",
			resumed,
			activateCalls,
			resumeErr,
		)
	}
}

func TestRemoteCloudKnownPendingRelayMismatchDoesNotMutateConfig(t *testing.T) {
	resetServerProfileSelection(t)
	t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
	if err := writePendingCloudDeviceCredential(
		validCredential(27),
		"relay-pending",
	); err != nil {
		t.Fatal(err)
	}
	cfg := runtimeConfig{RelayInstanceID: "relay-config"}
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := newSelectingCloudCoordinator()
	saveCalls := 0

	updated, err := connectRemoteToCloud(
		context.Background(),
		writeTestConfigFile(t, `{"schema_version":1}`),
		cfg,
		coordinator,
		origin,
		func(cloudRemotePairingPrompt) (string, error) {
			t.Fatal("Relay mismatch opened a Cloud pairing prompt")
			return "", nil
		},
		false,
		func(runtimeConfig) error {
			saveCalls++
			return nil
		},
	)
	if !IsCloudErrorCode(err, CloudErrRelayInstance) {
		t.Fatalf("known pending Relay mismatch error = %v", err)
	}
	if saveCalls != 0 || coordinator.preflightCalls != 0 ||
		coordinator.remoteCalls != 0 {
		t.Fatalf(
			"Relay mismatch mutated state: saves=%d preflight=%d add=%d",
			saveCalls,
			coordinator.preflightCalls,
			coordinator.remoteCalls,
		)
	}
	if updated.ProfileID != "" || updated.ClientInstallID != "" ||
		updated.RelayInstanceID != "relay-config" {
		t.Fatalf("Relay mismatch changed config: %+v", updated)
	}
}

func TestExistingDeviceCloudSetupRejectsPendingBeforeMutation(t *testing.T) {
	for _, source := range []string{
		pendingDeviceCredentialSourceLocal,
		pendingDeviceCredentialSourceCloud,
	} {
		t.Run(string(source), func(t *testing.T) {
			resetServerProfileSelection(t)
			t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
			credential := validCredential(29)
			var err error
			if source == pendingDeviceCredentialSourceCloud {
				err = writePendingCloudDeviceCredential(
					credential,
					"relay-pending",
				)
			} else {
				err = writePendingDeviceCredential(credential)
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg := completedLocalCloudTestConfig()
			coordinator := newSelectingCloudCoordinator()
			saveCalls := 0

			updated, err := connectExistingDeviceToCloud(
				context.Background(),
				writeTestConfigFile(t, `{"schema_version":1}`),
				cfg,
				coordinator,
				false,
				func(runtimeConfig) error {
					saveCalls++
					return nil
				},
			)
			if !IsCloudErrorCode(err, CloudErrDevicePendingConflict) {
				t.Fatalf("pending %s error = %v", source, err)
			}
			if saveCalls != 0 || coordinator.preflightCalls != 0 ||
				coordinator.localCalls != 0 {
				t.Fatalf(
					"pending %s mutated state: saves=%d preflight=%d add=%d",
					source,
					saveCalls,
					coordinator.preflightCalls,
					coordinator.localCalls,
				)
			}
			if updated.Cloud != nil || updated.ProfileID != cfg.ProfileID {
				t.Fatalf("pending %s changed config: %+v", source, updated)
			}
			record, exists, readErr := readPendingDeviceCredentialRecord()
			if readErr != nil || !exists ||
				record.Credential != credential ||
				record.Source != source {
				t.Fatalf(
					"pending %s changed record=%+v exists=%v err=%v",
					source,
					record,
					exists,
					readErr,
				)
			}
		})
	}
}
