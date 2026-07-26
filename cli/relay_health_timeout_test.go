package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthConnectTimeoutBoundsEveryTransportSelectionPath(t *testing.T) {
	tests := []struct {
		name        string
		route       routePolicy
		override    string
		installStub func(relayTransportResolver)
	}{
		{
			name:     "explicit local",
			route:    routePolicyLocal,
			override: "--via=local",
			installStub: func(resolver relayTransportResolver) {
				resolveLocalRelayTransportForCLI = resolver
			},
		},
		{
			name:     "explicit cloud",
			route:    routePolicyCloud,
			override: "--via=cloud",
			installStub: func(resolver relayTransportResolver) {
				resolveCloudRelayTransportForCLI = resolver
			},
		},
		{
			name:  "automatic",
			route: routePolicyAutomatic,
			installStub: func(resolver relayTransportResolver) {
				resolveAutomaticRelayTransportForCLI = resolver
			},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			oldLocal := resolveLocalRelayTransportForCLI
			oldCloud := resolveCloudRelayTransportForCLI
			oldAutomatic := resolveAutomaticRelayTransportForCLI
			t.Cleanup(func() {
				resolveLocalRelayTransportForCLI = oldLocal
				resolveCloudRelayTransportForCLI = oldCloud
				resolveAutomaticRelayTransportForCLI = oldAutomatic
			})

			const connectBudget = 80 * time.Millisecond
			observed := make(chan time.Duration, 1)
			testCase.installStub(func(
				ctx context.Context,
				_ runtimeConfig,
			) (relayTransportSelection, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					return relayTransportSelection{},
						errors.New("transport selection has no deadline")
				}
				observed <- time.Until(deadline)
				<-ctx.Done()
				return relayTransportSelection{}, ctx.Err()
			})

			paths := runtimePaths{
				ConfigFile: filepath.Join(t.TempDir(), "config.json"),
			}
			cfg := runtimeConfig{
				RelayBaseURL:       "https://relay.invalid",
				RelaySecureBaseURL: "https://relay.invalid:8792",
				RelaySpkiPin:       "test-pin",
				ProfileID:          "profile-timeout-test",
				RelayInstanceID:    "relay-timeout-test",
				RoutePolicy:        testCase.route,
				Cloud:              readyCloudForTransportTest(),
			}
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatalf("save config: %v", err)
			}
			args := []string{
				"--connect-timeout",
				"0.08",
				"--max-time",
				"1",
			}
			if testCase.override != "" {
				args = append(args, testCase.override)
			}

			started := time.Now()
			exit, _ := captureCommandOutput(t, func() int {
				return runHealth(paths, args)
			})
			if exit != 1 {
				t.Fatalf("health exit = %d, want timeout failure", exit)
			}
			if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
				t.Fatalf(
					"transport selection exceeded connect budget: %s",
					elapsed,
				)
			}
			select {
			case remaining := <-observed:
				if remaining <= 0 || remaining > 150*time.Millisecond {
					t.Fatalf(
						"selection deadline remaining = %s, want about %s",
						remaining,
						connectBudget,
					)
				}
			default:
				t.Fatal("selected resolver did not observe its deadline")
			}
		})
	}
}
