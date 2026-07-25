package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestInteractiveCloudRecoveryPrecedesClientResolution(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, mode := range []string{"pending", "held", "disabled"} {
			t.Run(profile+"/"+mode, func(t *testing.T) {
				cfg := hybridCheckpointUXConfig(
					cloudStateTokenStored,
					false,
				)
				if mode == "held" {
					cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
						Code:        cloudProblemAuthorization,
						Remediation: cloudRemediationSecurityStop,
					}
				}
				paths, cfg := saveHybridCheckpointUXProfile(
					t,
					profile,
					cfg,
				)
				if mode == "disabled" {
					_, restore := setCloudFeatureTestIdentity(
						t,
						cloudRemoteBuildIdentity{Disabled: true},
					)
					defer restore()
				}
				coordinator := successfulCloudCoordinatorForTest()
				installCloudSetupTestSeams(t, coordinator, true, true)
				localActivationCalls := 0
				originalResume := resumePendingActivationAfterRetirementCheck
				resumePendingActivationAfterRetirementCheck = func(
					*runtimeConfig,
					func(*runtimeConfig) error,
				) (bool, error) {
					localActivationCalls++
					return true, nil
				}
				t.Cleanup(func() {
					resumePendingActivationAfterRetirementCheck =
						originalResume
				})
				localKeyringCalls := 0
				originalPreflight := relayAuthTokenSetupPreflightForSetup
				relayAuthTokenSetupPreflightForSetup = func() error {
					localKeyringCalls++
					return nil
				}
				t.Cleanup(func() {
					relayAuthTokenSetupPreflightForSetup =
						originalPreflight
				})
				input := ""
				if mode == "pending" {
					input = "n\n"
				}

				stdout, stderr := captureInteractiveSetupIO(
					t,
					input,
					func() int {
						return interactiveSetup(
							paths,
							cfg,
							installState{},
							"unsupported-client",
							"",
							"",
							"",
							"",
							false,
						)
					},
				)
				output := stdout + stderr
				wantRemove := "ha-nova cloud remove --server " + profile
				if !strings.Contains(output, wantRemove) ||
					strings.Contains(output, "unsupported client") ||
					strings.Contains(output, "Which AI client") ||
					coordinator.addCalls != 0 ||
					localActivationCalls != 0 ||
					localKeyringCalls != 0 {
					t.Fatalf(
						"recovery ordering calls=%d/%d/%d: %s",
						coordinator.addCalls,
						localActivationCalls,
						localKeyringCalls,
						output,
					)
				}
			})
		}
	}
}

func TestNonInteractiveRecoveryPrecedesNoClientFailure(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, cloudOnly := range []bool{false, true} {
			for _, enabled := range []bool{true, false} {
				for _, explicitLocal := range []bool{false, true} {
					name := profile
					if cloudOnly {
						name += "/cloud-only"
					} else {
						name += "/hybrid"
					}
					if enabled {
						name += "/enabled"
					} else {
						name += "/disabled"
					}
					if explicitLocal {
						name += "/explicit-local"
					} else {
						name += "/unconstrained"
					}
					t.Run(name, func(t *testing.T) {
						cfg := hybridCheckpointUXConfig(
							cloudStateTokenStored,
							false,
						)
						if cloudOnly {
							cfg = pendingCloudOnlyCommandConfig(
								cloudStateTokenStored,
							)
						}
						paths, _ := saveHybridCheckpointUXProfile(
							t,
							profile,
							cfg,
						)
						withClientRuntimeAvailability(t, map[string]bool{})
						if !enabled {
							_, restore := setCloudFeatureTestIdentity(
								t,
								cloudRemoteBuildIdentity{Disabled: true},
							)
							defer restore()
						}
						args := []string{"--non-interactive"}
						if profile != defaultServerProfileName {
							args = append(args, "--server", profile)
						}
						if explicitLocal {
							args = append(
								args,
								"--ha-url",
								"http://local.example:8123",
							)
						}

						exit, output := captureCommandOutput(t, func() int {
							return runSetup(paths, args)
						})
						wantRemove := "ha-nova cloud remove --server " + profile
						if exit != 1 ||
							!strings.Contains(output, wantRemove) ||
							strings.Contains(output, "no supported AI clients") {
							t.Fatalf(
								"checkpoint lost behind client failure: %s",
								output,
							)
						}
						wantResume := "ha-nova cloud add --server " + profile
						if enabled &&
							!strings.Contains(output, wantResume) {
							t.Fatalf("missing resume %q: %s", wantResume, output)
						}
						if !enabled &&
							strings.Contains(output, wantResume) {
							t.Fatalf("disabled recovery advertised resume: %s", output)
						}
					})
				}
			}
		}
	}
}

func TestPausedCloudCheckpointAlwaysShowsResumeAndCleanup(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		t.Run(profile, func(t *testing.T) {
			cfg := pendingCloudOnlyCommandConfig(cloudStateCloudVerified)
			paths, _ := saveHybridCheckpointUXProfile(t, profile, cfg)

			var output strings.Builder
			if !handlePausedCloudOwnerPairing(
				&output,
				paths,
				errSetupExit,
			) {
				t.Fatal("saved checkpoint pause was not handled")
			}
			for _, command := range []string{
				"ha-nova cloud add --server " + profile,
				"ha-nova cloud remove --server " + profile,
			} {
				if !strings.Contains(output.String(), command) {
					t.Fatalf("pause missing %q: %s", command, output.String())
				}
			}
		})
	}
}

func TestHeadlessCloudCommandShowsExactRecovery(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		t.Run(profile, func(t *testing.T) {
			cfg := pendingCloudOnlyCommandConfig(cloudStateTokenStored)
			paths, _ := saveHybridCheckpointUXProfile(t, profile, cfg)
			installCloudCommandPromptSession(t, false)
			args := []string(nil)
			if profile != defaultServerProfileName {
				args = []string{"--server", profile}
			}

			exit, output := captureCommandOutput(t, func() int {
				return runCloudConnectCommand(paths, args, false)
			})
			if exit != 1 ||
				!strings.Contains(
					output,
					"ha-nova cloud add --server "+profile,
				) ||
				!strings.Contains(
					output,
					"ha-nova cloud remove --server "+profile,
				) {
				t.Fatalf("headless recovery exit=%d: %s", exit, output)
			}
		})
	}
}

func TestCloudURLCancellationShowsExactCleanup(t *testing.T) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudOnlyCommandConfig(cloudStateAuthorizing)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	_, output := captureCommandOutput(t, func() int {
		printCloudURLPromptCancellation(paths)
		return 0
	})
	if !strings.Contains(
		output,
		"ha-nova cloud add --server default",
	) || !strings.Contains(
		output,
		"ha-nova cloud remove --server default",
	) {
		t.Fatalf("URL cancellation omitted exact recovery: %s", output)
	}
}

func TestPausedHybridOutcomeIsNeitherFailureNorFullSuccess(t *testing.T) {
	var output strings.Builder
	exit := renderSetupCompletionOutcomeWithCloudPause(
		&output,
		[]string{"codex"},
		false,
		true,
	)
	if exit != 0 ||
		!strings.Contains(output.String(), "Cloud setup paused") ||
		strings.Contains(output.String(), "Setup complete!") ||
		strings.Contains(output.String(), "Setup incomplete") {
		t.Fatalf("paused outcome exit=%d: %s", exit, output.String())
	}
}

func TestCloudOnlyRecoveryNoticePrecedesClientPrompt(t *testing.T) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudOnlyCommandConfig(cloudStateTokenStored)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	updated, handled, code := handleInteractiveCloudRecoveryBeforeClients(
		bufio.NewReader(strings.NewReader("")),
		&output,
		paths,
		cfg,
		false,
		false,
	)
	if handled || code != 0 || updated.Cloud == nil ||
		!strings.Contains(
			output.String(),
			"ha-nova cloud add --server default",
		) ||
		!strings.Contains(
			output.String(),
			"ha-nova cloud remove --server default",
		) {
		t.Fatalf(
			"Cloud-only early recovery handled=%v code=%d: %s",
			handled,
			code,
			output.String(),
		)
	}
}

func TestInteractiveNamedLocalFlagsDoNotHideCheckpoint(t *testing.T) {
	paths, cfg := saveHybridCheckpointUXProfile(
		t,
		"cabin",
		hybridCheckpointUXConfig(cloudStateTokenStored, false),
	)
	coordinator := successfulCloudCoordinatorForTest()
	installCloudSetupTestSeams(t, coordinator, true, true)

	stdout, stderr := captureInteractiveSetupIO(t, "n\n", func() int {
		return interactiveSetup(
			paths,
			cfg,
			installState{},
			"unsupported-client",
			"",
			"http://local.example:8123",
			"",
			"",
			false,
		)
	})
	output := stdout + stderr
	if !strings.Contains(
		output,
		"ha-nova cloud add --server cabin",
	) || !strings.Contains(
		output,
		"ha-nova cloud remove --server cabin",
	) || strings.Contains(
		output,
		"only for Cloud-only resume",
	) || coordinator.addCalls != 0 {
		t.Fatalf(
			"named local flags hid checkpoint calls=%d: %s",
			coordinator.addCalls,
			output,
		)
	}
}

func TestNamedRetirementOnlySetupDoesNotEnterClientWizard(t *testing.T) {
	paths := setupServerCommandTest(t, testV2ThreeProfileConfig)
	seedDeviceRetirementCheckpointForProfile(
		t,
		paths,
		"cabin",
		deviceCredentialRetirementPrepared,
	)
	withClientRuntimeAvailability(t, map[string]bool{})

	exit, output := captureCommandOutput(t, func() int {
		return runSetup(
			paths,
			[]string{"--server", "cabin", "--non-interactive"},
		)
	})
	if exit != 0 ||
		strings.Contains(output, "no supported AI clients") {
		t.Fatalf(
			"retirement-only setup entered client flow exit=%d: %s",
			exit,
			output,
		)
	}
	_, exists, err := readDeviceCredentialRetirementCheckpointForProfile(
		paths,
		"cabin",
	)
	if err != nil || exists {
		t.Fatalf(
			"retirement checkpoint remains: exists=%v err=%v",
			exists,
			err,
		)
	}
}
