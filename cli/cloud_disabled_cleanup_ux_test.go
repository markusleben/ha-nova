package main

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestDisabledCloudStateAlwaysShowsExactCleanup(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, current := range []bool{false, true} {
			name := profile + "/pending"
			if current {
				name = profile + "/current"
			}
			t.Run(name, func(t *testing.T) {
				resetServerProfileSelection(t)
				_, restore := setCloudFeatureTestIdentity(
					t,
					cloudRemoteBuildIdentity{Disabled: true},
				)
				defer restore()
				withClientRuntimeAvailability(
					t,
					map[string]bool{"antigravity": true},
				)

				paths := setupServerCommandTest(t, `{"schema_version":1}`)
				if profile != defaultServerProfileName {
					if err := saveConfig(
						paths,
						completedLocalCloudTestConfig(),
					); err != nil {
						t.Fatal(err)
					}
					setServerSelectionOverride(profile)
					setActiveServerProfile(profile)
				}
				cfg := disabledCloudCleanupTestConfig(current)
				if err := saveConfig(paths, cfg); err != nil {
					t.Fatal(err)
				}
				wantRemove := "ha-nova cloud remove --server " + profile

				healthCalls := 0
				installCloudCommandHealthVerifier(
					t,
					func(_ context.Context, _ runtimeConfig) error {
						healthCalls++
						return nil
					},
				)
				exit, output := captureCommandOutput(t, func() int {
					return runCloudStatusCommand(paths, nil)
				})
				assertDisabledCleanupOnly(
					t,
					"status",
					exit,
					1,
					output,
					wantRemove,
				)
				if healthCalls != 0 {
					t.Fatalf("disabled status verified Cloud health %d times", healthCalls)
				}

				exit, output = captureCommandOutput(t, func() int {
					return runCloudStatusCommand(paths, []string{"--json"})
				})
				var summary cloudStatusSummary
				if err := json.Unmarshal(
					[]byte(strings.TrimSpace(output)),
					&summary,
				); err != nil {
					t.Fatalf("status JSON=%q: %v", output, err)
				}
				if exit != 1 || summary.NextCommand != "" {
					t.Fatalf(
						"disabled status JSON exit=%d summary=%+v",
						exit,
						summary,
					)
				}

				var guidance strings.Builder
				renderCloudRecoveryGuidance(
					&guidance,
					cfg,
					cloudAdapterUnavailableProblem(),
				)
				assertDisabledCleanupOnly(
					t,
					"recovery guidance",
					1,
					1,
					guidance.String(),
					wantRemove,
				)

				var resume strings.Builder
				exit = resumeInteractiveCloudOnlySetup(
					bufio.NewReader(strings.NewReader("")),
					&resume,
					paths,
					cfg,
					&installState{},
					"antigravity",
					nil,
					nil,
				)
				assertDisabledCleanupOnly(
					t,
					"interactive resume",
					exit,
					1,
					resume.String(),
					wantRemove,
				)

				exit, output = captureCommandOutput(t, func() int {
					return runCloudConnectCommand(paths, nil, current)
				})
				assertDisabledCleanupOnly(
					t,
					"Cloud command resume",
					exit,
					1,
					output,
					wantRemove,
				)

				installCloudCommandPromptSession(t, true)
				installSuccessfulCloudDevicePreflight(t)
				installCloudCommandCoordinator(
					t,
					successfulCloudCoordinatorForTest(),
				)
				unlockArgs := []string(nil)
				if profile != defaultServerProfileName {
					unlockArgs = []string{"--server", profile}
				}
				exit, output = captureCommandOutput(t, func() int {
					return runCloudUnlockCommand(paths, unlockArgs)
				})
				assertDisabledCleanupOnly(
					t,
					"unlock",
					exit,
					0,
					output,
					wantRemove,
				)

				exit, output = captureCommandOutput(t, func() int {
					return runSetup(
						paths,
						[]string{"antigravity", "--non-interactive"},
					)
				})
				assertDisabledCleanupOnly(
					t,
					"non-interactive setup",
					exit,
					1,
					output,
					wantRemove,
				)
			})
		}
	}
}

func TestDisabledLocalOnlyUnlockDoesNotOfferCloudCleanup(t *testing.T) {
	resetServerProfileSelection(t)
	_, restore := setCloudFeatureTestIdentity(
		t,
		cloudRemoteBuildIdentity{Disabled: true},
	)
	defer restore()

	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-local-only"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(t, successfulCloudCoordinatorForTest())

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(paths, nil)
	})
	if exit != 0 ||
		strings.Contains(output, "ha-nova cloud remove") ||
		strings.Contains(output, "ha-nova cloud add") ||
		strings.Contains(output, "ha-nova cloud reconnect") {
		t.Fatalf("disabled local-only unlock exit=%d output=%s", exit, output)
	}
}

func disabledCloudCleanupTestConfig(current bool) runtimeConfig {
	cfg := runtimeConfig{
		ProfileID:   "profile-disabled-cleanup",
		RoutePolicy: routePolicyLocal,
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
		},
	}
	if !current {
		return cfg
	}
	metadata := cloudMetadataForTest(strings.Repeat("d", 32))
	cfg.RelayInstanceID = "relay-disabled-cleanup"
	cfg.RoutePolicy = routePolicyCloud
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &metadata,
	}
	return cfg
}

func assertDisabledCleanupOnly(
	t *testing.T,
	path string,
	exit int,
	wantExit int,
	output string,
	wantRemove string,
) {
	t.Helper()
	if exit != wantExit ||
		!strings.Contains(output, wantRemove) ||
		strings.Contains(output, "ha-nova cloud add") ||
		strings.Contains(output, "ha-nova cloud reconnect") {
		t.Fatalf(
			"%s exit=%d, want cleanup %q only:\n%s",
			path,
			exit,
			wantRemove,
			output,
		)
	}
}
