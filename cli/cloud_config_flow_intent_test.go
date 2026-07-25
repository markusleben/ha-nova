package main

import (
	"context"
	"strings"
	"testing"
)

func TestCloudConnectIntentRejectsBeforeSaveOrSecretPreflight(t *testing.T) {
	origin, err := cloudOriginFromCanonical(productionCloudTestOrigin)
	if err != nil {
		t.Fatal(err)
	}
	ready := completedLocalCloudTestConfig()
	ready.ProfileID = "profile-intent"
	ready.RelayInstanceID = "relay-intent"
	metadata := cloudMetadataForTest(strings.Repeat("e", 32))
	ready.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &metadata,
	}
	ready.RoutePolicy = routePolicyAutomatic

	for _, testCase := range []struct {
		name      string
		cfg       runtimeConfig
		reconnect bool
	}{
		{
			name: "add already ready",
			cfg:  ready,
		},
		{
			name:      "reconnect not configured",
			cfg:       completedLocalCloudTestConfig(),
			reconnect: true,
		},
	} {
		t.Run(testCase.name+"/local", func(t *testing.T) {
			coordinator := newSelectingCloudCoordinator()
			saveCalls := 0
			_, err := connectExistingDeviceToCloud(
				context.Background(),
				runtimePaths{},
				testCase.cfg,
				coordinator,
				testCase.reconnect,
				func(runtimeConfig) error {
					saveCalls++
					return nil
				},
			)
			if err == nil {
				t.Fatal("invalid local Cloud intent succeeded")
			}
			if saveCalls != 0 || coordinator.preflightCalls != 0 ||
				coordinator.localCalls != 0 {
				t.Fatalf(
					"invalid local intent saves=%d preflight=%d add=%d",
					saveCalls,
					coordinator.preflightCalls,
					coordinator.localCalls,
				)
			}
		})

		t.Run(testCase.name+"/remote", func(t *testing.T) {
			resetServerProfileSelection(t)
			t.Setenv("HA_NOVA_TEST_SECRET_DIR", t.TempDir())
			coordinator := newSelectingCloudCoordinator()
			saveCalls := 0
			_, err := connectRemoteToCloud(
				context.Background(),
				runtimePaths{},
				testCase.cfg,
				coordinator,
				origin,
				func(cloudRemotePairingPrompt) (string, error) {
					t.Fatal("invalid Cloud intent opened pairing prompt")
					return "", nil
				},
				testCase.reconnect,
				func(runtimeConfig) error {
					saveCalls++
					return nil
				},
			)
			if err == nil {
				t.Fatal("invalid remote Cloud intent succeeded")
			}
			if saveCalls != 0 || coordinator.preflightCalls != 0 ||
				coordinator.remoteCalls != 0 {
				t.Fatalf(
					"invalid remote intent saves=%d preflight=%d add=%d",
					saveCalls,
					coordinator.preflightCalls,
					coordinator.remoteCalls,
				)
			}
		})
	}
}
