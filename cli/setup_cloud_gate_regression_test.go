package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteOnlyCheckpointBlocksEveryExplicitLocalSetupPath(
	t *testing.T,
) {
	cases := []struct {
		name       string
		host       string
		haURL      string
		relayURL   string
		relayToken string
		service    bool
	}{
		{name: "host", host: "ha.local"},
		{name: "ha-url", haURL: "http://ha.local:8123"},
		{name: "relay-url", relayURL: "http://ha.local:8791"},
		{name: "relay-token", relayToken: "explicit-token"},
		{name: "service", service: true},
	}
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, testCase := range cases {
			t.Run(profile+"/"+testCase.name, func(t *testing.T) {
				cfg := pendingCloudOnlyCommandConfig(cloudStateTokenStored)
				paths, cfg := saveHybridCheckpointUXProfile(
					t,
					profile,
					cfg,
				)
				before, err := os.ReadFile(paths.ConfigFile)
				if err != nil {
					t.Fatal(err)
				}
				keyringCalls := installSetupKeyringPreflightCounter(t)
				activationCalls := 0
				originalResume := resumePendingActivationAfterRetirementCheck
				resumePendingActivationAfterRetirementCheck = func(
					*runtimeConfig,
					func(*runtimeConfig) error,
				) (bool, error) {
					activationCalls++
					return false, nil
				}
				t.Cleanup(func() {
					resumePendingActivationAfterRetirementCheck =
						originalResume
				})
				coordinator := newSelectingCloudCoordinator()
				installCloudCommandCoordinator(t, coordinator)

				stdout, stderr := captureInteractiveSetupIO(
					t,
					"",
					func() int {
						return interactiveSetup(
							paths,
							cfg,
							installState{},
							"unsupported-client",
							testCase.host,
							testCase.haURL,
							testCase.relayURL,
							testCase.relayToken,
							testCase.service,
						)
					},
				)
				output := stdout + stderr
				for _, command := range []string{
					"ha-nova cloud add --server " + profile,
					"ha-nova cloud remove --server " + profile,
				} {
					if !strings.Contains(output, command) {
						t.Fatalf("missing %q: %s", command, output)
					}
				}
				if !strings.Contains(
					output,
					"before changing local or service credentials",
				) ||
					strings.Contains(output, "unsupported client") ||
					*keyringCalls != 0 ||
					activationCalls != 0 ||
					coordinator.preflightCalls != 0 ||
					coordinator.remoteCalls != 0 {
					t.Fatalf(
						"explicit path escaped gate keyring/activation/cloud=%d/%d/%d: %s",
						*keyringCalls,
						activationCalls,
						coordinator.remoteCalls,
						output,
					)
				}
				after, err := os.ReadFile(paths.ConfigFile)
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != string(before) {
					t.Fatal("explicit path mutated config")
				}
			})
		}
	}
}

func TestSetupPrerequisiteFailuresRenderCloudRecoveryFirst(
	t *testing.T,
) {
	modes := []struct {
		name    string
		enabled bool
		held    bool
		ready   bool
	}{
		{name: "enabled", enabled: true},
		{name: "disabled"},
		{name: "held", enabled: true, held: true},
		{name: "ready", enabled: true, ready: true},
	}
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, mode := range modes {
			for _, source := range []string{
				"state",
				"install",
				"census",
			} {
				t.Run(profile+"/"+mode.name+"/"+source, func(t *testing.T) {
					restoreFeature := setCloudFeatureTestBuild(
						t,
						mode.enabled,
					)
					defer restoreFeature()
					cfg := pendingCloudOnlyCommandConfig(
						cloudStateTokenStored,
					)
					if mode.held {
						cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
							Code:        cloudProblemAuthorization,
							Remediation: cloudRemediationSecurityStop,
						}
					}
					if mode.ready {
						current := cloudMetadataForTest(
							strings.Repeat("f", 32),
						)
						cfg.Cloud = &cloudLifecycleMetadata{
							State:   cloudStateReady,
							Current: &current,
						}
						cfg.RelayInstanceID = "relay-instance-1"
						cfg.RoutePolicy = routePolicyCloud
					}
					paths, _ := saveHybridCheckpointUXProfile(
						t,
						profile,
						cfg,
					)
					paths.StateFile = filepath.Join(
						paths.ConfigDir,
						"state.json",
					)
					var corruptPath string
					var wantFailure string
					switch source {
					case "state":
						corruptPath = paths.StateFile
						wantFailure = "state file is corrupt"
					case "install":
						corruptPath =
							installLifecycleGenerationPath(paths)
						wantFailure = "cannot inspect install lifecycle"
					default:
						corruptPath =
							censusLifecycleMarkerPath(paths)
						wantFailure = "cannot inspect uninstall lifecycle"
					}
					if err := os.WriteFile(
						corruptPath,
						[]byte{},
						0o600,
					); err != nil {
						t.Fatal(err)
					}
					if source == "state" {
						if err := os.WriteFile(
							corruptPath,
							[]byte("{"),
							0o600,
						); err != nil {
							t.Fatal(err)
						}
					}
					args := []string{"unsupported-client"}
					if profile != defaultServerProfileName {
						args = append(args, "--server", profile)
					}

					exit, output := captureCommandOutput(t, func() int {
						return runSetup(paths, args)
					})
					resume := "ha-nova cloud add --server " + profile
					remove := "ha-nova cloud remove --server " + profile
					firstRecovery := remove
					if mode.enabled && !mode.held && !mode.ready {
						firstRecovery = resume
					}
					if exit != 1 ||
						!strings.Contains(output, remove) ||
						!strings.Contains(output, wantFailure) ||
						strings.Index(output, firstRecovery) >
							strings.Index(output, wantFailure) {
						t.Fatalf(
							"prerequisite recovery exit=%d: %s",
							exit,
							output,
						)
					}
					hasResume := strings.Contains(output, resume)
					if hasResume !=
						(mode.enabled && !mode.held && !mode.ready) {
						t.Fatalf(
							"mode %s resume=%v: %s",
							mode.name,
							hasResume,
							output,
						)
					}
				})
			}
		}
	}
}
