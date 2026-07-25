package main

import (
	"strings"
	"testing"
)

func TestRemoteOnlyCloudCheckpointPrecedesConflictingRetirement(
	t *testing.T,
) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		t.Run(profile, func(t *testing.T) {
			cfg := pendingCloudOnlyCommandConfig(cloudStateTokenStored)
			paths, cfg := saveHybridCheckpointUXProfile(t, profile, cfg)
			seedDeviceRetirementCheckpointForProfile(
				t,
				paths,
				profile,
				deviceCredentialRetirementPrepared,
			)
			makeRetirementCheckpointConflict(t, paths, profile)
			withClientRuntimeAvailability(
				t,
				map[string]bool{"codex": true},
			)
			coordinator := newSelectingCloudCoordinator()
			installCloudCommandCoordinator(t, coordinator)
			revokeCalls := installRetirementRevokeCounter(t)
			keyringCalls := installSetupKeyringPreflightCounter(t)

			stdout, stderr := captureInteractiveSetupIO(t, "", func() int {
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
			})
			output := stdout + stderr
			resume := "ha-nova cloud add --server " + profile
			remove := "ha-nova cloud remove --server " + profile
			retirementFailure := "cannot finish the interrupted device credential retirement"
			if !strings.Contains(output, resume) ||
				!strings.Contains(output, remove) ||
				!strings.Contains(output, retirementFailure) ||
				strings.Index(output, resume) >
					strings.Index(output, retirementFailure) ||
				strings.Contains(output, "unsupported client") ||
				*revokeCalls != 0 ||
				*keyringCalls != 0 ||
				coordinator.preflightCalls != 0 ||
				coordinator.remoteCalls != 0 {
				t.Fatalf(
					"conflicting ordering revoke/keyring/cloud=%d/%d/%d: %s",
					*revokeCalls,
					*keyringCalls,
					coordinator.remoteCalls,
					output,
				)
			}
			if pending, err :=
				deviceCredentialRetirementCheckpointExistsForProfile(
					paths,
					profile,
				); err != nil || !pending {
				t.Fatalf(
					"conflicting checkpoint changed: pending=%v err=%v",
					pending,
					err,
				)
			}
		})
	}
}

func TestRemoteOnlyCloudStaleRetirementSettlesBeforeClientResolution(
	t *testing.T,
) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, ready := range []bool{false, true} {
			name := profile + "/pending"
			if ready {
				name = profile + "/ready-current"
			}
			t.Run(name, func(t *testing.T) {
				cfg := pendingCloudOnlyCommandConfig(cloudStateTokenStored)
				if ready {
					current := cloudMetadataForTest(strings.Repeat("d", 32))
					cfg.Cloud = &cloudLifecycleMetadata{
						State:   cloudStateReady,
						Current: &current,
					}
					cfg.RoutePolicy = routePolicyCloud
					cfg.RelayInstanceID = "relay-ready"
				}
				paths, cfg := saveHybridCheckpointUXProfile(
					t,
					profile,
					cfg,
				)
				seedDeviceRetirementCheckpointForProfile(
					t,
					paths,
					profile,
					deviceCredentialRetirementPrepared,
				)
				withClientRuntimeAvailability(
					t,
					map[string]bool{"codex": true},
				)
				coordinator := newSelectingCloudCoordinator()
				installCloudCommandCoordinator(t, coordinator)
				revokeCalls := installRetirementRevokeCounter(t)
				keyringCalls := installSetupKeyringPreflightCounter(t)

				stdout, stderr := captureInteractiveSetupIO(
					t,
					"",
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
				if !strings.Contains(output, "unsupported client") ||
					*revokeCalls != 0 ||
					*keyringCalls != 0 ||
					coordinator.preflightCalls != 0 ||
					coordinator.remoteCalls != 0 {
					t.Fatalf(
						"stale ordering revoke/keyring/cloud=%d/%d/%d: %s",
						*revokeCalls,
						*keyringCalls,
						coordinator.remoteCalls,
						output,
					)
				}
				if !ready {
					resume := "ha-nova cloud add --server " + profile
					if !strings.Contains(output, resume) ||
						strings.Index(output, resume) >
							strings.Index(output, "unsupported client") {
						t.Fatalf("Cloud recovery rendered too late: %s", output)
					}
				}
				if pending, err :=
					deviceCredentialRetirementCheckpointExistsForProfile(
						paths,
						profile,
					); err != nil || pending {
					t.Fatalf(
						"stale checkpoint remains: pending=%v err=%v",
						pending,
						err,
					)
				}
			})
		}
	}
}

func makeRetirementCheckpointConflict(
	t *testing.T,
	paths runtimePaths,
	profile string,
) {
	t.Helper()
	checkpoint, exists, err :=
		readDeviceCredentialRetirementCheckpointForProfile(paths, profile)
	if err != nil || !exists {
		t.Fatalf("read retirement checkpoint: exists=%v err=%v", exists, err)
	}
	checkpoint.RelaySecureBaseURL = "https://retired.example:8792"
	path, err := deviceCredentialRetirementCheckpointPathForProfile(
		paths,
		profile,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(path, checkpoint, 0o600); err != nil {
		t.Fatal(err)
	}
}

func installRetirementRevokeCounter(t *testing.T) *int {
	t.Helper()
	calls := 0
	original := revokeSelfDeviceV1ForRetire
	revokeSelfDeviceV1ForRetire = func(string, string, string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { revokeSelfDeviceV1ForRetire = original })
	return &calls
}

func installSetupKeyringPreflightCounter(t *testing.T) *int {
	t.Helper()
	calls := 0
	original := relayAuthTokenSetupPreflightForSetup
	relayAuthTokenSetupPreflightForSetup = func() error {
		calls++
		return nil
	}
	t.Cleanup(func() {
		relayAuthTokenSetupPreflightForSetup = original
	})
	return &calls
}
