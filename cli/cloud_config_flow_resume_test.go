package main

import (
	"context"
	"strings"
	"testing"
)

func TestReconnectStopsAfterResumingCommittedLifecycle(t *testing.T) {
	for _, state := range []cloudLifecycleState{
		cloudStateCommitted,
		cloudStateRetiringPrevious,
	} {
		t.Run("local/"+string(state), func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(t, `{"schema_version":1}`)
			cfg := resumableReconnectConfig(state, true)
			coordinator := newSelectingCloudCoordinator()
			updated, err := connectExistingDeviceToCloud(
				context.Background(),
				paths,
				cfg,
				coordinator,
				true,
				func(value runtimeConfig) error {
					cfg = value
					return nil
				},
			)
			if err != nil {
				t.Fatalf("resume local reconnect: %v", err)
			}
			if coordinator.preflightCalls != 1 ||
				coordinator.localCalls != 0 ||
				coordinator.remoteCalls != 0 ||
				!updated.Cloud.ready() ||
				updated.RoutePolicy != routePolicyAutomatic {
				t.Fatalf(
					"local resume calls=%d/%d/%d result=%+v",
					coordinator.preflightCalls,
					coordinator.localCalls,
					coordinator.remoteCalls,
					updated,
				)
			}
		})

		t.Run("remote/"+string(state), func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(t, `{"schema_version":1}`)
			cfg := resumableReconnectConfig(state, false)
			coordinator := newSelectingCloudCoordinator()
			origin, err := cloudOriginFromCanonical(
				productionCloudTestOrigin,
			)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := connectRemoteToCloud(
				context.Background(),
				paths,
				cfg,
				coordinator,
				origin,
				func(cloudRemotePairingPrompt) (string, error) {
					t.Fatal("resumed reconnect requested another pairing code")
					return "", nil
				},
				true,
				func(value runtimeConfig) error {
					cfg = value
					return nil
				},
			)
			if err != nil {
				t.Fatalf("resume remote reconnect: %v", err)
			}
			if coordinator.preflightCalls != 1 ||
				coordinator.localCalls != 0 ||
				coordinator.remoteCalls != 0 ||
				!updated.Cloud.ready() ||
				updated.RoutePolicy != routePolicyCloud {
				t.Fatalf(
					"remote resume calls=%d/%d/%d result=%+v",
					coordinator.preflightCalls,
					coordinator.localCalls,
					coordinator.remoteCalls,
					updated,
				)
			}
		})
	}
}

func resumableReconnectConfig(
	state cloudLifecycleState,
	local bool,
) runtimeConfig {
	metadata := cloudMetadataForTest(strings.Repeat("d", 32))
	cloud := &cloudLifecycleMetadata{
		State:   state,
		Current: &metadata,
	}
	if state == cloudStateCommitted {
		pending := metadata
		cloud.Pending = &pending
	}
	cfg := runtimeConfig{
		ProfileID:       "profile-resume-reconnect",
		RelayInstanceID: "relay-resume-reconnect",
		Cloud:           cloud,
		ClientInstallID: "install-resume-reconnect",
		RoutePolicy:     routePolicyCloud,
	}
	if local {
		cfg.HAHost = "ha.local"
		cfg.HAURL = "http://ha.local:8123"
		cfg.RelayBaseURL = "http://ha.local:8791"
		cfg.RelaySecureBaseURL = "https://ha.local:18792"
		cfg.RelaySpkiPin = "PIN"
		cfg.RoutePolicy = routePolicyAutomatic
	}
	return cfg
}
