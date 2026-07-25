package main

import (
	"strings"
	"testing"
)

func TestCloudUnlockKeepsHoldsWithoutReadyHealth(t *testing.T) {
	for _, test := range []struct {
		name       string
		hold       cloudRecoveryHold
		wantOutput string
	}{
		{
			name: "secure storage verification",
			hold: cloudRecoveryHold{
				Code:        cloudProblemSecureStorage,
				Remediation: cloudRemediationVerifyState,
			},
			wantOutput: "recovery safety hold remains",
		},
		{
			name: "security review",
			hold: cloudRecoveryHold{
				Code:        cloudProblemIdentityMismatch,
				Remediation: cloudRemediationSecurityStop,
			},
			wantOutput: "recovery remains paused for security review",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			cfg := runtimeConfig{
				ProfileID:   "profile-held",
				RoutePolicy: routePolicyLocal,
				Cloud: &cloudLifecycleMetadata{
					State:        cloudStateAuthorizing,
					RecoveryHold: &test.hold,
				},
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

			exit, output := captureCommandOutput(t, func() int {
				return runCloudUnlockCommand(paths, nil)
			})
			if exit != 0 || !strings.Contains(output, test.wantOutput) {
				t.Fatalf("unlock exit=%d output=%s", exit, output)
			}
			saved, err := loadSelectedRuntimeConfigUnchecked(paths)
			if err != nil {
				t.Fatal(err)
			}
			isHeld := saved.Cloud != nil &&
				saved.Cloud.RecoveryHold != nil
			if !isHeld {
				t.Fatalf("unlock cleared unverified hold: %+v", saved.Cloud)
			}
			if strings.Contains(output, "cloud add") ||
				strings.Contains(output, "cloud reconnect") {
				t.Fatalf("security hold exposed mutation guidance: %s", output)
			}
		})
	}
}
