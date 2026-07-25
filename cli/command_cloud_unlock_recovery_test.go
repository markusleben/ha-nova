package main

import (
	"context"
	"strings"
	"testing"
)

func installSuccessfulCloudDevicePreflight(t *testing.T) {
	t.Helper()
	oldProbe := probeCloudDeviceStorageForSetup
	oldPending := readCloudPendingDeviceForSetup
	oldCurrent := readCloudDeviceForSetup
	probeCloudDeviceStorageForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (deviceStorageProbe, error) {
		return deviceStorageProbe{mode: "keyring"}, nil
	}
	readCloudPendingDeviceForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (pendingDeviceCredentialRecord, bool, error) {
		return pendingDeviceCredentialRecord{}, false, nil
	}
	readCloudDeviceForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (string, bool, error) {
		return "", false, nil
	}
	t.Cleanup(func() {
		probeCloudDeviceStorageForSetup = oldProbe
		readCloudPendingDeviceForSetup = oldPending
		readCloudDeviceForSetup = oldCurrent
	})
}

func TestCloudUnlockSupportsExplicitProfileBeforeCheckpointExists(
	t *testing.T,
) {
	for _, test := range []struct {
		name    string
		profile string
		paths   func(*testing.T) runtimePaths
	}{
		{
			name:    "fresh default",
			profile: defaultServerProfileName,
			paths: func(t *testing.T) runtimePaths {
				paths := setupServerCommandTest(t, `{"schema_version":1}`)
				paths.ConfigFile += ".missing"
				return paths
			},
		},
		{
			name:    "missing named profile",
			profile: "cabin",
			paths: func(t *testing.T) runtimePaths {
				paths := setupServerCommandTest(t, `{"schema_version":1}`)
				if err := saveConfig(
					paths,
					completedLocalCloudTestConfig(),
				); err != nil {
					t.Fatal(err)
				}
				return paths
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			installCloudCommandPromptSession(t, true)
			installSuccessfulCloudDevicePreflight(t)
			coordinator := successfulCloudCoordinatorForTest()
			installCloudCommandCoordinator(t, coordinator)
			paths := test.paths(t)

			exit, output := captureCommandOutput(t, func() int {
				return runCloudUnlockCommand(
					paths,
					[]string{"--server", test.profile},
				)
			})
			wantAdd := "ha-nova cloud add --server " + test.profile
			if exit != 0 ||
				!strings.Contains(output, "No Cloud checkpoint was saved") ||
				!strings.Contains(
					output,
					"Device credential storage is unlocked",
				) ||
				!strings.Contains(
					output,
					"OAuth storage has no profile-scoped slot yet",
				) ||
				strings.Contains(
					output,
					"Native secure storage is unlocked",
				) ||
				!strings.Contains(output, wantAdd) ||
				strings.Contains(output, "<your-cloud-host>") ||
				strings.Contains(output, "repair config.json") ||
				coordinator.preflightCalls != 0 {
				t.Fatalf(
					"pre-profile unlock exit=%d calls=%d output=%s",
					exit,
					coordinator.preflightCalls,
					output,
				)
			}
		})
	}
}

func TestCloudUnlockPreProfileFailureUsesTypedDurableRecovery(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	paths.ConfigFile += ".missing"
	installCloudCommandPromptSession(t, true)
	oldProbe := probeCloudDeviceStorageForSetup
	probeCloudDeviceStorageForSetup = func(
		context.Context,
		SecretStoreUIPolicy,
	) (deviceStorageProbe, error) {
		return deviceStorageProbe{}, errDesktopKeyringLocked
	}
	t.Cleanup(func() {
		probeCloudDeviceStorageForSetup = oldProbe
	})

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", "cabin"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudProblemSecureStorage)) ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) ||
		!strings.Contains(output, "No Cloud checkpoint was saved") ||
		!strings.Contains(output, "ha-nova cloud unlock --server cabin") ||
		!strings.Contains(output, cloudFreshAddCommand()) ||
		strings.Contains(output, "checkpoint saved at") ||
		strings.Contains(output, "repair config.json") {
		t.Fatalf("pre-profile failure exit=%d output=%s", exit, output)
	}
}

func TestCloudUnlockHandlesLegacyLocalProfileWithoutCloudIdentity(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(
		t,
		`{
			"schema_version": 1,
			"ha_host": "ha",
			"ha_url": "http://ha:8123",
			"relay_base_url": "http://ha:8791",
			"relay_secure_base_url": "https://ha:18792",
			"relay_spki_pin": "PIN"
		}`,
	)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	coordinator := successfulCloudCoordinatorForTest()
	installCloudCommandCoordinator(t, coordinator)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", defaultServerProfileName},
		)
	})
	if exit != 0 ||
		!strings.Contains(output, "No Cloud checkpoint was saved") ||
		!strings.Contains(output, "Device credential storage is unlocked") ||
		!strings.Contains(
			output,
			"OAuth storage has no profile-scoped slot yet",
		) ||
		strings.Contains(output, "Native secure storage is unlocked") ||
		!strings.Contains(output, cloudFreshAddCommand()) ||
		coordinator.preflightCalls != 0 {
		t.Fatalf(
			"legacy unlock exit=%d calls=%d output=%s",
			exit,
			coordinator.preflightCalls,
			output,
		)
	}
}

func TestCloudUnlockPartialRemoteProfileStartsExecutableURLRecovery(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	setServerSelectionOverride("cabin")
	setActiveServerProfile("cabin")
	cfg := runtimeConfig{
		ClientInstallID: "inst-existing",
		ProfileID:       "profile-cabin",
		RoutePolicy:     routePolicyLocal,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(t, successfulCloudCoordinatorForTest())

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", "cabin"},
		)
	})
	if exit != 0 ||
		!strings.Contains(output, "ha-nova cloud add --server cabin") ||
		strings.Contains(output, "--url") ||
		strings.Contains(output, "<") {
		t.Fatalf("partial-profile unlock exit=%d output=%s", exit, output)
	}

	prompted := false
	installCloudCommandURLPrompt(t, func(
		context.Context,
	) (CloudOrigin, error) {
		prompted = true
		return CloudOrigin{}, errSetupExit
	})
	exit, output = captureCommandOutput(t, func() int {
		return runCloudConnectCommand(
			paths,
			[]string{"--server", "cabin"},
			false,
		)
	})
	if exit != 0 || !prompted ||
		!strings.Contains(
			output,
			"cancelled before an authorization checkpoint was saved",
		) {
		t.Fatalf(
			"partial-profile add exit=%d prompted=%v output=%s",
			exit,
			prompted,
			output,
		)
	}
}

func TestCloudUnlockProvidesReconnectForRecoverableReadyFailure(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{
			name: "sign in",
			err: newCloudError(
				CloudErrOAuthInvalidGrant,
				"refresh Cloud authorization",
				nil,
			),
		},
		{
			name: "pair device",
			err: newCloudError(
				CloudErrDeviceRejected,
				"verify Cloud device",
				nil,
			),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			setServerSelectionOverride("cabin")
			setActiveServerProfile("cabin")
			current := cloudMetadataForTest(strings.Repeat("e", 32))
			cfg := completedLocalCloudTestConfig()
			cfg.ProfileID = "profile-cabin"
			cfg.RelayInstanceID = "relay-cabin"
			cfg.RoutePolicy = routePolicyAutomatic
			cfg.Cloud = &cloudLifecycleMetadata{
				State:   cloudStateReady,
				Current: &current,
			}
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}
			installCloudCommandPromptSession(t, true)
			installSuccessfulCloudDevicePreflight(t)
			installCloudCommandCoordinator(
				t,
				successfulCloudCoordinatorForTest(),
			)
			installCloudCommandHealthVerifier(
				t,
				func(context.Context, runtimeConfig) error {
					return test.err
				},
			)

			exit, output := captureCommandOutput(t, func() int {
				return runCloudUnlockCommand(
					paths,
					[]string{"--server", "cabin"},
				)
			})
			if exit != 1 ||
				!strings.Contains(
					output,
					"ha-nova cloud reconnect --server cabin",
				) {
				t.Fatalf("ready unlock exit=%d output=%s", exit, output)
			}
		})
	}
}

func TestCloudUnlockReplacesVerifiedStorageHoldWithSecurityHold(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	current := cloudMetadataForTest(strings.Repeat("f", 32))
	storageHold := cloudRecoveryHold{
		Code:        cloudProblemSecureStorage,
		Remediation: cloudRemediationVerifyState,
	}
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-unlock-transition"
	cfg.RelayInstanceID = "relay-unlock-transition"
	cfg.RoutePolicy = routePolicyAutomatic
	cfg.Cloud = &cloudLifecycleMetadata{
		State:        cloudStateReady,
		Current:      &current,
		RecoveryHold: &storageHold,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(t, successfulCloudCoordinatorForTest())
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return newCloudError(
				CloudErrIdentityMismatch,
				"verify unlocked Cloud identity",
				nil,
			)
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(paths, nil)
	})
	if exit != 1 {
		t.Fatalf("unlock exit=%d output=%s", exit, output)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.RecoveryHold == nil ||
		saved.Cloud.RecoveryHold.Code != cloudProblemIdentityMismatch ||
		saved.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("unlock lost security transition: %+v", saved.Cloud)
	}
}
