package main

import (
	"bufio"
	"strings"
	"testing"
)

type hybridCheckpointUXCase struct {
	name        string
	state       cloudLifecycleState
	withCurrent bool
}

var hybridCheckpointUXCases = []hybridCheckpointUXCase{
	{name: "authorizing", state: cloudStateAuthorizing},
	{name: "token stored", state: cloudStateTokenStored},
	{
		name:        "reconnect pending",
		state:       cloudStateCloudVerified,
		withCurrent: true,
	},
}

func TestCompletedHybridCheckpointDecisionShowsExactRecovery(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, input := range []string{"n\n", "back\n", "exit\n"} {
			t.Run(profile+"/"+strings.TrimSpace(input), func(t *testing.T) {
				paths, cfg := saveHybridCheckpointUXProfile(
					t,
					profile,
					hybridCheckpointUXConfig(cloudStateTokenStored, false),
				)
				coordinator := successfulCloudCoordinatorForTest()
				installCloudSetupTestSeams(t, coordinator, true, true)

				var output strings.Builder
				handled, code := maybeHandleInteractiveSetupCurrentState(
					bufio.NewReader(strings.NewReader(input)),
					&output,
					paths,
					cfg,
					completeSetupStateForCloudUX(),
					false,
					false,
				)
				wantResume := "ha-nova cloud add --server " + profile
				wantRemove := "ha-nova cloud remove --server " + profile
				if !handled || code != 0 ||
					!strings.Contains(output.String(), wantResume) ||
					!strings.Contains(output.String(), wantRemove) ||
					strings.Contains(output.String(), "Everything is already set up!") ||
					coordinator.addCalls != 0 {
					t.Fatalf(
						"handled=%v code=%d calls=%d output=%s",
						handled,
						code,
						coordinator.addCalls,
						output.String(),
					)
				}
				saved, err := loadConfig(paths)
				if err != nil {
					t.Fatal(err)
				}
				if saved.Cloud == nil ||
					saved.Cloud.State != cloudStateTokenStored ||
					saved.Cloud.Current != nil {
					t.Fatalf("checkpoint changed after decline: %+v", saved.Cloud)
				}
			})
		}
	}
}

func TestHybridCheckpointRecoveryMatrix(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, checkpoint := range hybridCheckpointUXCases {
			for _, enabled := range []bool{true, false} {
				for _, interactive := range []bool{true, false} {
					name := profile + "/" + checkpoint.name
					if enabled {
						name += "/enabled"
					} else {
						name += "/disabled"
					}
					if interactive {
						name += "/interactive"
					} else {
						name += "/noninteractive"
					}
					t.Run(name, func(t *testing.T) {
						paths, cfg := saveHybridCheckpointUXProfile(
							t,
							profile,
							hybridCheckpointUXConfig(
								checkpoint.state,
								checkpoint.withCurrent,
							),
						)
						if !enabled {
							_, restore := setCloudFeatureTestIdentity(
								t,
								cloudRemoteBuildIdentity{Disabled: true},
							)
							defer restore()
						}
						coordinator := successfulCloudCoordinatorForTest()
						installCloudSetupTestSeams(t, coordinator, true, true)

						var output string
						var code int
						if interactive {
							var builder strings.Builder
							handled, result := maybeHandleInteractiveSetupCurrentState(
								bufio.NewReader(strings.NewReader("exit\n")),
								&builder,
								paths,
								cfg,
								completeSetupStateForCloudUX(),
								false,
								false,
							)
							if !handled {
								t.Fatal("interactive checkpoint was not handled")
							}
							code = result
							output = builder.String()
						} else {
							exit, captured := captureCommandOutput(t, func() int {
								if handleNonInteractiveCloudSetupRecovery(
									paths,
									cfg,
								) {
									return 1
								}
								return 0
							})
							code = exit
							output = captured
						}

						wantRemove := "ha-nova cloud remove --server " + profile
						if !strings.Contains(output, wantRemove) ||
							strings.Contains(output, "Everything is already set up!") {
							t.Fatalf("code=%d missing exact recovery: %s", code, output)
						}
						if enabled {
							action := "add"
							if checkpoint.withCurrent {
								action = "reconnect"
							}
							wantResume := "ha-nova cloud " + action +
								" --server " + profile
							if !strings.Contains(output, wantResume) {
								t.Fatalf("code=%d missing resume %q: %s", code, wantResume, output)
							}
						} else if code != 1 ||
							strings.Contains(output, "ha-nova cloud add") ||
							strings.Contains(output, "ha-nova cloud reconnect") {
							t.Fatalf("disabled recovery advertised mutation: %s", output)
						}
					})
				}
			}
		}
	}
}

func TestHybridCheckpointReachesNonInteractiveSetupRecovery(t *testing.T) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, enabled := range []bool{true, false} {
			name := profile + "/enabled"
			if !enabled {
				name = profile + "/disabled"
			}
			t.Run(name, func(t *testing.T) {
				paths, _ := saveHybridCheckpointUXProfile(
					t,
					profile,
					hybridCheckpointUXConfig(cloudStateAuthorizing, false),
				)
				withClientRuntimeAvailability(
					t,
					map[string]bool{"antigravity": true},
				)
				if !enabled {
					_, restore := setCloudFeatureTestIdentity(
						t,
						cloudRemoteBuildIdentity{Disabled: true},
					)
					defer restore()
				}
				args := []string{"antigravity", "--non-interactive"}
				if profile != defaultServerProfileName {
					args = append(args, "--server", profile)
				}

				exit, output := captureCommandOutput(t, func() int {
					return runSetup(paths, args)
				})
				wantRemove := "ha-nova cloud remove --server " + profile
				if exit != 1 ||
					!strings.Contains(output, wantRemove) ||
					strings.Contains(output, "only for Cloud-only resume") ||
					strings.Contains(output, "Everything is already set up!") {
					t.Fatalf(
						"hybrid recovery exit=%d missing %q: %s",
						exit,
						wantRemove,
						output,
					)
				}
				wantResume := "ha-nova cloud add --server " + profile
				if enabled && !strings.Contains(output, wantResume) {
					t.Fatalf("enabled recovery missing %q: %s", wantResume, output)
				}
				if !enabled && strings.Contains(output, wantResume) {
					t.Fatalf("disabled recovery advertised %q: %s", wantResume, output)
				}
			})
		}
	}
}

func TestDisabledReadyCloudStateIsNotCalledIncomplete(t *testing.T) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := hybridCheckpointUXConfig(cloudStateReady, true)
	cfg.Cloud.Pending = nil
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	_, restore := setCloudFeatureTestIdentity(
		t,
		cloudRemoteBuildIdentity{Disabled: true},
	)
	defer restore()

	var output strings.Builder
	handled, code := maybeHandleInteractiveSetupCurrentState(
		bufio.NewReader(strings.NewReader("")),
		&output,
		paths,
		cfg,
		completeSetupStateForCloudUX(),
		false,
		false,
	)
	if !handled || code != 1 ||
		!strings.Contains(
			output.String(),
			"Home Assistant Cloud access remains saved",
		) ||
		!strings.Contains(
			output.String(),
			"ha-nova cloud remove --server default",
		) ||
		strings.Contains(output.String(), "setup remains incomplete") ||
		strings.Contains(output.String(), "Everything is already set up!") {
		t.Fatalf(
			"disabled ready wording handled=%v code=%d: %s",
			handled,
			code,
			output.String(),
		)
	}
}

func completeSetupStateForCloudUX() setupState {
	return setupState{
		ConfigOK: true,
		TokenOK:  true,
		RelayOK:  true,
		WSOK:     true,
		SkillsOK: true,
	}
}

func hybridCheckpointUXConfig(
	state cloudLifecycleState,
	withCurrent bool,
) runtimeConfig {
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-hybrid-checkpoint"
	pending := cloudMetadataForTest(strings.Repeat("a", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   state,
		Pending: &pending,
	}
	if withCurrent {
		current := cloudMetadataForTest(strings.Repeat("b", 32))
		cfg.Cloud.Current = &current
		cfg.RelayInstanceID = "relay-instance-1"
		cfg.RoutePolicy = routePolicyAutomatic
	}
	return cfg
}

func saveHybridCheckpointUXProfile(
	t *testing.T,
	profile string,
	cfg runtimeConfig,
) (runtimePaths, runtimeConfig) {
	t.Helper()
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if profile != defaultServerProfileName {
		base := completedLocalCloudTestConfig()
		base.ProfileID = "profile-default-local"
		if err := saveConfig(paths, base); err != nil {
			t.Fatal(err)
		}
		setServerSelectionOverride(profile)
		setActiveServerProfile(profile)
		cfg.ProfileID = "profile-" + profile + "-hybrid"
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths, cfg
}
