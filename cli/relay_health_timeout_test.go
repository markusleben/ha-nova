package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// --connect-timeout bounds each connection a selection opens (the budget the
// resolver carries); --max-time bounds the whole selection, which on Link is
// several round trips (docs/work/2026-11-03-link-route-timeouts-spec.md).
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
			budgets := make(chan time.Duration, 1)
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
				budgets <- relayConnectBudget(ctx, time.Hour)
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
			if elapsed := time.Since(started); elapsed > 1500*time.Millisecond {
				t.Fatalf(
					"transport selection exceeded the --max-time budget: %s",
					elapsed,
				)
			}
			select {
			case remaining := <-observed:
				if remaining <= 500*time.Millisecond || remaining > time.Second {
					t.Fatalf(
						"selection deadline remaining = %s, want about the 1s --max-time",
						remaining,
					)
				}
			default:
				t.Fatal("selected resolver did not observe its deadline")
			}
			if budget := <-budgets; budget != connectBudget {
				t.Fatalf("connect budget carried to the resolver = %s, want %s", budget, connectBudget)
			}
		})
	}
}
