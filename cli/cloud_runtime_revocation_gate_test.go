package main

import (
	"context"
	"errors"
	"testing"
)

func TestProductionCloudResolversRejectRevocationCheckpointBeforeIO(
	t *testing.T,
) {
	restoreBuild := setCloudFeatureTestBuild(t, true)
	defer restoreBuild()
	configureCloudRemoteFeature(runtimePaths{})

	tests := []struct {
		name       string
		checkpoint func(*cloudLifecycleMetadata)
	}{
		{
			name: "device revocation",
			checkpoint: func(cloud *cloudLifecycleMetadata) {
				cloud.DeviceRevocationCompleted =
					&cloudDeviceRevocationCheckpoint{
						CurrentDeviceID: "revoked-device",
					}
			},
		},
		{
			name: "authorization revocation",
			checkpoint: func(cloud *cloudLifecycleMetadata) {
				cloud.AuthorizationRevocationCompleted =
					&cloudAuthorizationRevocationCheckpoint{}
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			cloud := readyCloudForTransportTest()
			testCase.checkpoint(cloud)
			cfg := runtimeConfig{
				RoutePolicy: routePolicyAutomatic,
				Cloud:       cloud,
			}

			oldLocal := resolveLocalRelayTransportForCLI
			oldStore := newCloudSecretStoreForCLI
			localCalls := 0
			resolveLocalRelayTransportForCLI = func(
				context.Context,
				runtimeConfig,
			) (relayTransportSelection, error) {
				localCalls++
				return relayTransportSelection{}, errors.New(
					"local transport must not run",
				)
			}
			newCloudSecretStoreForCLI = func(
				string,
			) (OAuthSecretStore, error) {
				t.Fatal("revoked profile opened OAuth secure storage")
				return nil, nil
			}
			t.Cleanup(func() {
				resolveLocalRelayTransportForCLI = oldLocal
				newCloudSecretStoreForCLI = oldStore
			})

			if _, err := resolveAutomaticRelayTransport(
				context.Background(),
				cfg,
			); err == nil {
				t.Fatal("automatic resolver accepted revoked profile")
			}
			if localCalls != 0 {
				t.Fatalf("automatic resolver performed %d local calls", localCalls)
			}
			if _, err := resolveCloudRelayTransport(
				context.Background(),
				cfg,
			); err == nil {
				t.Fatal("Cloud resolver accepted revoked profile")
			}
		})
	}
}
