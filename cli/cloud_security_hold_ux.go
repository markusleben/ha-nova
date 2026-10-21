package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const cloudSignInHoldText = "HA NOVA stopped Home Assistant Link to keep your home safe. It can't tell if an earlier Link sign-in still works"

// cloudHoldNeedsOwnerSteps: a safety hold on a Link sign-in. A plain
// `cloud remove` can't check that sign-in either and stops again, so the
// owner steps and the manual cleanup command are the way out.
func cloudHoldNeedsOwnerSteps(cfg runtimeConfig) bool {
	return cfg.Cloud != nil && cfg.Cloud.RecoveryHold != nil &&
		cfg.Cloud.RecoveryHold.Remediation == cloudRemediationSecurityStop &&
		cfg.Cloud.RecoveryHold.Code == cloudProblemAuthorization
}

// renderCloudHoldOwnerSteps says what happened in one sentence, then the
// owner steps with links on the saved local address, ending with the exact
// manual cleanup command for profile.
func renderCloudHoldOwnerSteps(out io.Writer, cfg runtimeConfig, profile string) {
	fmt.Fprintf(out, "  %s.\n", cloudSignInHoldText)
	if err := validateServerProfileName(profile); err != nil {
		renderCloudSelectionRepair(out, err)
		return
	}
	if cfg.Cloud.Current != nil {
		fmt.Fprintln(out, "  Your saved Link connection was not changed.")
	}
	for _, line := range strings.Split(cloudAuthorizationRecoverySteps(profile, "", cfg.HAURL), "\n") {
		fmt.Fprintf(out, "  %s\n", line)
	}
}

// printCloudHoldOwnerSteps renders them on stdout for the selected server
// when the hold needs them; false otherwise.
func printCloudHoldOwnerSteps(cfg runtimeConfig) bool {
	if !cloudHoldNeedsOwnerSteps(cfg) {
		return false
	}
	renderCloudHoldOwnerSteps(os.Stdout, cfg, selectedCloudCommandProfile())
	return true
}
