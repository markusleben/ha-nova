package main

import (
	"strings"
	"testing"
)

func TestEveryOrdinaryCloudFailureIncludesVerifiedCleanup(
	t *testing.T,
) {
	restoreFeature := setCloudFeatureTestBuild(t, true)
	defer restoreFeature()
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		for _, ready := range []bool{false, true} {
			t.Run(profile+"/"+map[bool]string{
				false: "checkpoint",
				true:  "ready",
			}[ready], func(t *testing.T) {
				resetServerProfileSelection(t)
				setServerSelectionOverride(profile)
				setActiveServerProfile(profile)
				cfg := pendingCloudOnlyCommandConfig(
					cloudStateCloudVerified,
				)
				problem := &cloudProblem{
					Code:        cloudProblemUnavailable,
					Remediation: cloudRemediationRetry,
				}
				if ready {
					current := cloudMetadataForTest(
						strings.Repeat("d", 32),
					)
					cfg.Cloud = &cloudLifecycleMetadata{
						State:   cloudStateReady,
						Current: &current,
					}
					problem = &cloudProblem{
						Code:        cloudProblemAuthorization,
						Remediation: cloudRemediationSignIn,
					}
				}

				var output strings.Builder
				renderCloudRecoveryGuidance(
					&output,
					cfg,
					problem,
				)
				cleanup := "Verified cleanup: ha-nova cloud remove --server " +
					profile
				if !strings.Contains(output.String(), cleanup) {
					t.Fatalf("missing %q: %s", cleanup, output.String())
				}
			})
		}
	}
}

func TestCloudRecoveryHoldGuidanceIsCleanupOnly(t *testing.T) {
	restoreFeature := setCloudFeatureTestBuild(t, true)
	defer restoreFeature()
	resetServerProfileSelection(t)
	setServerSelectionOverride("cabin")
	setActiveServerProfile("cabin")
	cfg := pendingCloudOnlyCommandConfig(cloudStateAuthorizing)
	cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
		Code:        cloudProblemAuthorization,
		Remediation: cloudRemediationSecurityStop,
	}

	var output strings.Builder
	renderCloudRecoveryGuidance(
		&output,
		cfg,
		&cloudProblem{
			Code:        cloudProblemAuthorization,
			Remediation: cloudRemediationSecurityStop,
		},
	)
	if !strings.Contains(
		output.String(),
		"Verified cleanup: ha-nova cloud remove --server cabin",
	) ||
		strings.Contains(output.String(), "Resume:") ||
		strings.Contains(output.String(), "Reconnect:") {
		t.Fatalf("hold guidance=%s", output.String())
	}
}
