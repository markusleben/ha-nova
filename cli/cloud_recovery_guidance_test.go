package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestCloudConnectClassifiesNativeKeyringFailureAsUnlock(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(paths, completedLocalCloudTestConfig()); err != nil {
		t.Fatal(err)
	}
	installCloudCommandCoordinator(
		t,
		failingRemoteCloudCommandCoordinator{
			err: fmt.Errorf(
				"device credential unavailable: %w",
				errDesktopKeyringLocked,
			),
		},
	)
	installCloudCommandPromptSession(t, true)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(paths, nil, false)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudProblemSecureStorage)) ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) ||
		!strings.Contains(
			output,
			"ha-nova cloud unlock --server default",
		) ||
		strings.Contains(output, string(cloudRemediationRetry)) {
		t.Fatalf("keyring failure exit=%d output=%s", exit, output)
	}
}

func TestPausedOwnerPairingUsesDurableOAuthCheckpointWithoutUnlockClaim(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudOnlyCommandConfig(cloudStateCloudVerified)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	if !handlePausedCloudOwnerPairing(&output, paths, errSetupExit) {
		t.Fatal("owner navigation was not handled as a paused setup")
	}
	if !strings.Contains(output.String(), "OAuth authorization is saved") ||
		!strings.Contains(
			output.String(),
			"ha-nova cloud add --server default",
		) ||
		strings.Contains(output.String(), "Unlock") ||
		strings.Contains(output.String(), string(cloudRemediationRetry)) {
		t.Fatalf("paused owner guidance=%s", output.String())
	}
}

func TestPausedOwnerPairingHonorsStorageRecoveryProgression(
	t *testing.T,
) {
	for _, test := range []struct {
		name            string
		state           cloudLifecycleState
		storageVerified bool
		wantUnlock      bool
	}{
		{
			name:       "unverified ready",
			state:      cloudStateReady,
			wantUnlock: true,
		},
		{
			name:            "verified nonready",
			state:           cloudStateAuthorizing,
			storageVerified: true,
			wantUnlock:      false,
		},
		{
			name:            "verified ready health recheck",
			state:           cloudStateReady,
			storageVerified: true,
			wantUnlock:      true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			_, restore := setCloudFeatureTestIdentity(
				t,
				cloudRemoteBuildIdentity{
					Development: true,
					AppSlug:     "local_ha_nova_cloud_beta",
				},
			)
			defer restore()
			cfg := hybridCheckpointUXConfig(
				test.state,
				test.state == cloudStateReady,
			)
			if test.state == cloudStateReady {
				cfg.Cloud.Pending = nil
			}
			cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
				Code:            cloudProblemSecureStorage,
				Remediation:     cloudRemediationVerifyState,
				StorageVerified: test.storageVerified,
			}
			paths, _ := saveHybridCheckpointUXProfile(
				t,
				defaultServerProfileName,
				cfg,
			)
			var output strings.Builder
			if !handlePausedCloudOwnerPairing(
				&output,
				paths,
				errSetupExit,
			) {
				t.Fatal("storage hold pause was not handled")
			}
			hasUnlock := strings.Contains(
				output.String(),
				"ha-nova cloud unlock --server default",
			)
			if hasUnlock != test.wantUnlock ||
				!strings.Contains(
					output.String(),
					"ha-nova cloud remove --server default",
				) {
				t.Fatalf(
					"pause unlock=%v guidance=%s",
					hasUnlock,
					output.String(),
				)
			}
		})
	}
}

func TestDurableCloudRecoveryNeverClaimsAttemptedInMemoryState(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(
		paths,
		pendingCloudOnlyCommandConfig(cloudStateAuthorizing),
	); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	renderDurableCloudRecoveryGuidance(
		&output,
		paths,
		&cloudProblem{Remediation: cloudRemediationRetry},
	)
	if !strings.Contains(
		output.String(),
		`Cloud setup checkpoint saved at "authorizing"`,
	) || strings.Contains(output.String(), string(cloudStateCloudVerified)) ||
		strings.Contains(output.String(), "cloud unlock") {
		t.Fatalf("durable recovery guidance=%s", output.String())
	}

	missing := runtimePaths{ConfigFile: paths.ConfigFile + ".missing"}
	output.Reset()
	renderDurableCloudRecoveryGuidance(
		&output,
		missing,
		&cloudProblem{Remediation: cloudRemediationRetry},
	)
	if strings.Contains(output.String(), "checkpoint saved") ||
		!strings.Contains(output.String(), "No Cloud checkpoint was saved") ||
		!strings.Contains(
			output.String(),
			"ha-nova cloud add --server default",
		) ||
		strings.Contains(output.String(), "<your-cloud-host>") ||
		strings.Contains(output.String(), "repair config.json") {
		t.Fatalf("unverified recovery guidance=%s", output.String())
	}
}

func TestDurableCloudRecoveryDoesNotRecommendUnlockForNetworkFailure(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(
		paths,
		pendingCloudOnlyCommandConfig(cloudStateCloudVerified),
	); err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	renderDurableCloudRecoveryGuidance(
		&output,
		paths,
		&cloudProblem{
			Code:        cloudProblemUnavailable,
			Remediation: cloudRemediationRetry,
		},
	)
	if strings.Contains(output.String(), "cloud unlock") ||
		!strings.Contains(output.String(), "Resume:") {
		t.Fatalf("network recovery guidance=%s", output.String())
	}
}

func TestReadyRecoveryDoesNotClaimBrokenConnectionIsReady(t *testing.T) {
	resetServerProfileSelection(t)
	setServerSelectionOverride("cabin")
	setActiveServerProfile("cabin")
	current := cloudMetadataForTest(strings.Repeat("d", 32))
	cfg := runtimeConfig{
		Cloud: &cloudLifecycleMetadata{
			State:   cloudStateReady,
			Current: &current,
		},
	}

	var output strings.Builder
	renderCloudRecoveryGuidance(
		&output,
		cfg,
		&cloudProblem{
			Code:        cloudProblemAuthorization,
			Remediation: cloudRemediationSignIn,
		},
	)
	if strings.Contains(output.String(), "connection is still ready") ||
		!strings.Contains(output.String(), "configuration was not changed") ||
		!strings.Contains(
			output.String(),
			"ha-nova cloud reconnect --server cabin",
		) {
		t.Fatalf("ready recovery guidance=%s", output.String())
	}
}

func TestUnreadableRecoveryUsesProfileScopedStatusWithoutSavedClaim(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{`)
	setServerSelectionOverride("cabin")

	var output strings.Builder
	renderDurableCloudRecoveryGuidance(
		&output,
		paths,
		&cloudProblem{Remediation: cloudRemediationRetry},
	)
	if !strings.Contains(
		output.String(),
		"ha-nova cloud status --server cabin",
	) ||
		strings.Contains(output.String(), "checkpoint saved") {
		t.Fatalf("unreadable recovery guidance=%s", output.String())
	}
}

func TestCloudUnlockUnreadableProfileUsesTypedScopedStatus(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{`)
	installCloudCommandPromptSession(t, true)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			paths,
			[]string{"--server", "cabin"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudProblemUnavailable)) ||
		!strings.Contains(output, "ha-nova cloud status --server cabin") ||
		strings.Contains(output, "checkpoint saved") {
		t.Fatalf("unreadable unlock exit=%d output=%s", exit, output)
	}
}
