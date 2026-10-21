package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// cloudHoldOwnerCleanupTimeout bounds that read; past it, the plain advice.
var cloudHoldOwnerCleanupTimeout = 5 * time.Second

const cloudSignInHoldText = "HA NOVA stopped Home Assistant Link to keep your home safe. It can't tell if an earlier Link sign-in still works"

// cloudHoldIsSecurityStop: a safety hold that only `cloud remove` clears.
func cloudHoldIsSecurityStop(cfg runtimeConfig) bool {
	return cfg.Cloud != nil && cfg.Cloud.RecoveryHold != nil &&
		cfg.Cloud.RecoveryHold.Remediation == cloudRemediationSecurityStop
}

// cloudHoldOwnerCleanup asks what `cloud remove` would find, read without a
// prompt: owner true when it can't check the saved sign-in or device and so
// needs the owner's steps and --confirm-remote-access-revoked; client is
// the one sign-in address to delete, when known. Anything else (it checks
// out, a cleanup checkpoint, a locked store) keeps the plain `cloud remove`.
var cloudHoldOwnerCleanup = func(cfg runtimeConfig) (owner bool, client string) {
	ctx, cancel := context.WithTimeout(context.Background(), cloudHoldOwnerCleanupTimeout)
	defer cancel()
	store, err := newCloudSecretStoreForCLI(cfg.ProfileID)
	if err != nil {
		return false, ""
	}
	_, authErr := inspectCloudAuthorizationCleanup(ctx, cfg, store)
	deviceErr := validateCloudDeviceRevocationPlan(ctx, cfg, activeServerProfile())
	authMissing := errors.Is(authErr, errCloudAuthorizationCleanupUnverifiable)
	deviceMissing := errors.Is(deviceErr, errCloudDeviceRevocationCredentialMissing)
	owner = (authMissing || deviceMissing) &&
		(authErr == nil || authMissing) && (deviceErr == nil || deviceMissing)
	if problem := cloudAuthorizationStopProblem(authErr); problem != nil {
		client = problem.linkClient
	}
	return owner, client
}

// renderCloudSecurityHold says what happened in one sentence and the way
// out for profile: the plain `cloud remove` when it can finish, else the
// owner steps with links on the saved local address. terminal: a person
// reads this in their own terminal; elsewhere (doctor, status, agents) the
// last step is `cloud remove`, which prints the exact --confirm command.
func renderCloudSecurityHold(out io.Writer, cfg runtimeConfig, profile string, terminal bool) {
	if err := validateServerProfileName(profile); err != nil {
		fmt.Fprintln(out, "  HA NOVA stopped Home Assistant Link to keep your home safe.")
		renderCloudSelectionRepair(out, err)
		return
	}
	owner, client := cloudHoldOwnerCleanup(cfg)
	if !owner {
		fmt.Fprintln(out, "  HA NOVA stopped Home Assistant Link to keep your home safe.")
		fmt.Fprintf(out, "  To remove Link, run: %s\n", cloudProfileCommandFor("remove", profile))
		return
	}
	fmt.Fprintf(out, "  %s.\n", cloudSignInHoldText)
	steps := cloudAuthorizationRecoverySteps(profile, client, cfg.HAURL)
	if !terminal {
		steps = steps[:strings.LastIndex(steps, "\n3. ")] +
			"\n3. In your own terminal, run: " + cloudProfileCommandFor("remove", profile) +
			"\n   It shows the last step."
	}
	for _, line := range strings.Split(steps, "\n") {
		fmt.Fprintf(out, "  %s\n", line)
	}
}

// printCloudSecurityHold renders it on stdout for the selected server, for
// output an AI assistant may read; false when there is no such hold.
func printCloudSecurityHold(cfg runtimeConfig) bool {
	if !cloudHoldIsSecurityStop(cfg) {
		return false
	}
	renderCloudSecurityHold(os.Stdout, cfg, selectedCloudCommandProfile(), false)
	return true
}
