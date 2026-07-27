package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestCloudRecoverySuppressesMutationForStopAndVerifyProblems(
	t *testing.T,
) {
	for _, remediation := range []cloudRemediation{
		cloudRemediationSecurityStop,
		cloudRemediationVerifyState,
	} {
		t.Run(string(remediation), func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := setupServerCommandTest(t, `{"schema_version":1}`)
			problem := &cloudProblem{Remediation: remediation}

			missing := runtimePaths{
				ConfigFile: paths.ConfigFile + ".missing",
			}
			var output strings.Builder
			renderDurableCloudRecoveryGuidance(
				&output,
				missing,
				problem,
			)
			if strings.Contains(output.String(), "cloud add") ||
				strings.Contains(output.String(), "Resume:") {
				t.Fatalf("missing recovery=%s", output.String())
			}

			if err := saveConfig(
				paths,
				pendingCloudOnlyCommandConfig(cloudStateAuthorizing),
			); err != nil {
				t.Fatal(err)
			}
			output.Reset()
			renderDurableCloudRecoveryGuidance(&output, paths, problem)
			if !strings.Contains(output.String(), "checkpoint saved") ||
				strings.Contains(output.String(), "cloud add") ||
				strings.Contains(output.String(), "Resume:") {
				t.Fatalf("pending recovery=%s", output.String())
			}
		})
	}
}

func TestGuidedCloudRecoveryCommandIsShellSyntaxSafe(t *testing.T) {
	resetServerProfileSelection(t)
	setServerSelectionOverride("cabin")
	command := cloudFreshAddCommand()
	if strings.ContainsAny(command, "<>") ||
		strings.Contains(command, "--url") {
		t.Fatalf("guided recovery command is not copy-safe: %q", command)
	}
	if output, err := exec.Command(
		"sh",
		"-n",
		"-c",
		command,
	).CombinedOutput(); err != nil {
		t.Fatalf("shell rejected %q: %v\n%s", command, err, output)
	}
}

func TestCloudStatusSuppressesResumeForSecurityStop(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(
		paths,
		pendingCloudReconnectCommandConfig(cloudStateCloudVerified),
	); err != nil {
		t.Fatal(err)
	}
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return newCloudError(
				CloudErrRelayInstance,
				"verify saved Cloud identity",
				nil,
			)
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatalf("status JSON=%q: %v", output, err)
	}
	if exit != 1 ||
		summary.VerificationError == nil ||
		summary.VerificationError.Remediation != cloudRemediationSecurityStop ||
		summary.NextCommand != "" {
		t.Fatalf("security-stop status exit=%d summary=%+v", exit, summary)
	}
	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		strings.Contains(output, "Resume after recovery") ||
		strings.Contains(output, "cloud reconnect") {
		t.Fatalf("security-stop human status exit=%d output=%s", exit, output)
	}
}

func TestDisabledCloudRecoveryNeverAdvertisesMutation(t *testing.T) {
	resetServerProfileSelection(t)
	_, restore := setCloudFeatureTestIdentity(
		t,
		cloudRemoteBuildIdentity{Disabled: true},
	)
	defer restore()

	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(
		paths,
		pendingCloudOnlyCommandConfig(cloudStateAuthorizing),
	); err != nil {
		t.Fatal(err)
	}
	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatalf("status JSON=%q: %v", output, err)
	}
	if exit != 1 || summary.NextCommand != "" {
		t.Fatalf("disabled status exit=%d summary=%+v", exit, summary)
	}
	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, "Cloud transport is disabled") ||
		!strings.Contains(output, "ha-nova cloud remove --server default") ||
		strings.Contains(output, "Resume with:") {
		t.Fatalf("disabled human status exit=%d output=%s", exit, output)
	}

	ready := completedLocalCloudTestConfig()
	ready.ProfileID = "profile-pending"
	ready.RelayInstanceID = "relay-ready"
	current := cloudMetadataForTest(strings.Repeat("e", 32))
	ready.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, ready); err != nil {
		t.Fatal(err)
	}
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return newCloudError(
				CloudErrDeviceRejected,
				"verify saved Cloud device",
				nil,
			)
		},
	)
	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatalf("ready status JSON=%q: %v", output, err)
	}
	if exit != 1 || summary.NextCommand != "" {
		t.Fatalf("disabled ready status exit=%d summary=%+v", exit, summary)
	}
	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		strings.Contains(output, "cloud reconnect") ||
		strings.Contains(output, "cloud add") {
		t.Fatalf("disabled ready human status exit=%d output=%s", exit, output)
	}

	missing := setupServerCommandTest(t, `{"schema_version":1}`)
	missing.ConfigFile += ".missing"
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	exit, output = captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(
			missing,
			[]string{"--server", "cabin"},
		)
	})
	if exit != 0 ||
		!strings.Contains(output, "Cloud transport is disabled") ||
		!strings.Contains(output, "no Cloud cleanup is needed") ||
		!strings.Contains(output, "Local-only setup remains available") ||
		strings.Contains(output, "cloud add") ||
		strings.Contains(output, "cloud reconnect") {
		t.Fatalf("disabled pre-profile unlock exit=%d output=%s", exit, output)
	}
}
