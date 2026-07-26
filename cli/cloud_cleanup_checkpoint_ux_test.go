package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCleanupCheckpointsNeverAdvertiseResume(
	t *testing.T,
) {
	for _, testCase := range []struct {
		name       string
		checkpoint func(*cloudLifecycleMetadata)
	}{
		{
			name: "device",
			checkpoint: func(cloud *cloudLifecycleMetadata) {
				cloud.DeviceRevocationCompleted =
					&cloudDeviceRevocationCheckpoint{
						CurrentDeviceID: "device-cleanup",
					}
			},
		},
		{
			name: "authorization",
			checkpoint: func(cloud *cloudLifecycleMetadata) {
				cloud.AuthorizationRevocationCompleted =
					&cloudAuthorizationRevocationCheckpoint{}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := hybridCheckpointUXConfig(
				cloudStateCloudVerified,
				true,
			)
			testCase.checkpoint(cfg.Cloud)
			var output strings.Builder
			renderCloudCheckpointActionsForProfile(
				&output,
				cfg,
				true,
				"cabin",
			)
			if strings.Contains(output.String(), "Resume:") ||
				strings.Contains(
					output.String(),
					"cloud reconnect",
				) ||
				!strings.Contains(
					output.String(),
					"ha-nova cloud remove --server cabin",
				) {
				t.Fatalf(
					"cleanup guidance = %s",
					output.String(),
				)
			}
		})
	}
}

func TestDeviceCleanupStatusSkipsCloudHealthAndNamesProfile(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	cfg := hybridCheckpointUXConfig(
		cloudStateCloudVerified,
		true,
	)
	cfg.Cloud.DeviceRevocationCompleted =
		&cloudDeviceRevocationCheckpoint{
			CurrentDeviceID: deviceIDOf(
				validCredential(190),
			),
		}
	paths, _ := saveHybridCheckpointUXProfile(
		t,
		"cabin",
		cfg,
	)
	healthCalls := 0
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			healthCalls++
			return nil
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(
			paths,
			[]string{
				"--server",
				"cabin",
				"--json",
			},
		)
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatalf("status JSON=%q: %v", output, err)
	}
	if exit != 1 ||
		summary.Status != "cleanup_pending" ||
		summary.NextCommand !=
			"ha-nova cloud remove --server cabin" ||
		healthCalls != 0 {
		t.Fatalf(
			"status exit=%d calls=%d summary=%+v",
			exit,
			healthCalls,
			summary,
		)
	}
}

func TestCleanupUnlockUsesWritableProbeWithoutSecretResume(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	cfg := hybridCheckpointUXConfig(
		cloudStateReady,
		true,
	)
	cfg.Cloud.Pending = nil
	cfg.Cloud.AuthorizationRevocationCompleted =
		&cloudAuthorizationRevocationCheckpoint{
			OwnerConfirmedAllRemoteAccessRevoked: true,
		}
	paths, cfg := saveHybridCheckpointUXProfile(
		t,
		"cabin",
		cfg,
	)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	coordinator := successfulCloudCoordinatorForTest()
	installCloudCommandCoordinator(t, coordinator)
	backend := newMemoryOAuthSecretBackend()
	store, err := NewOAuthSecretStore(
		backend,
		cfg.ProfileID,
	)
	if err != nil {
		t.Fatal(err)
	}
	previousStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(
		profileID string,
	) (OAuthSecretStore, error) {
		if profileID != cfg.ProfileID {
			t.Fatalf(
				"secret-store profile=%q want=%q",
				profileID,
				cfg.ProfileID,
			)
		}
		return store, nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = previousStore
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", "cabin"},
		)
	})
	if exit != 0 ||
		!strings.Contains(
			output,
			"ha-nova cloud remove --server cabin",
		) ||
		strings.Contains(output, "cloud reconnect") ||
		coordinator.preflightCalls != 0 {
		t.Fatalf(
			"unlock exit=%d preflight=%d output=%s",
			exit,
			coordinator.preflightCalls,
			output,
		)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if strings.Join(backend.operations, ",") !=
		"set,get,delete" {
		t.Fatalf(
			"cleanup unlock operations=%v",
			backend.operations,
		)
	}
}

func TestDeviceOnlyCleanupUnlockPreflightsBoundOAuthSlot(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	cfg := hybridCheckpointUXConfig(cloudStateReady, true)
	cfg.Cloud.Pending = nil
	cfg.Cloud.DeviceRevocationCompleted =
		&cloudDeviceRevocationCheckpoint{
			CurrentDeviceID: deviceIDOf(
				validCredential(192),
			),
		}
	paths, cfg := saveHybridCheckpointUXProfile(t, "cabin", cfg)
	backend := newMemoryOAuthSecretBackend()
	store, err := NewOAuthSecretStore(backend, cfg.ProfileID)
	if err != nil {
		t.Fatal(err)
	}
	envelope := productionCloudTestEnvelope()
	envelope.SchemaVersion = oauthSecretSchema
	envelope.State = OAuthSecretCurrent
	envelope.ProfileID = cfg.ProfileID
	envelope.RelayInstanceID = cfg.RelayInstanceID
	envelope.CreatedAt = time.Now().UTC()
	envelope.UpdatedAt = envelope.CreatedAt
	origin, err := cloudOriginFromCanonical(
		envelope.CanonicalOrigin,
	)
	if err != nil {
		t.Fatal(err)
	}
	metadata := cloudMetadataFromEnvelope(origin, envelope)
	cfg.Cloud.Current = &metadata
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := store.write(
		context.Background(),
		oauthSecretCurrentService,
		envelope,
		SecretStoreForbidUI,
	); err != nil {
		t.Fatal(err)
	}
	resetProductionCloudPolicies(backend)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	previousStore := newCloudSecretStoreForCLI
	newCloudSecretStoreForCLI = func(
		string,
	) (OAuthSecretStore, error) {
		return store, nil
	}
	t.Cleanup(func() {
		newCloudSecretStoreForCLI = previousStore
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", "cabin"},
		)
	})
	if exit != 0 ||
		!strings.Contains(output, "OAuth authorization revocation") ||
		!strings.Contains(
			output,
			"ha-nova cloud remove --server cabin",
		) {
		t.Fatalf("unlock exit=%d output=%s", exit, output)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if strings.Join(backend.operations, ",") != "get" {
		t.Fatalf(
			"device-only unlock operations=%v",
			backend.operations,
		)
	}
}
