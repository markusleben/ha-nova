package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCloudRecoveryHoldSurvivesRestartAndBlocksEveryResumePath(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	installCloudCommandCoordinator(
		t,
		failingRemoteCloudCommandCoordinator{
			err: newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"exchange OAuth authorization code",
				nil,
			),
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(
			paths,
			[]string{"--url", productionCloudTestOrigin},
			false,
		)
	})
	if exit != 1 || strings.Contains(output, "Resume:") {
		t.Fatalf("outcome-unknown add exit=%d output=%s", exit, output)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.State != cloudStateAuthorizing ||
		saved.Cloud.RecoveryHold == nil ||
		saved.Cloud.RecoveryHold.Code != cloudProblemAuthorization ||
		saved.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("saved recovery hold=%+v", saved.Cloud)
	}

	exit, output = captureCommandOutput(t, func() int {
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
		summary.Status != "recovery_blocked" ||
		summary.NextCommand != "" ||
		summary.VerificationError == nil ||
		summary.VerificationError.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("held status exit=%d summary=%+v", exit, summary)
	}

	blockedCoordinator := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, blockedCoordinator)
	exit, output = captureCommandOutput(t, func() int {
		return runCloudConnectCommand(
			paths,
			[]string{"--url", productionCloudTestOrigin},
			false,
		)
	})
	if exit != 1 ||
		blockedCoordinator.preflightCalls != 0 ||
		blockedCoordinator.remoteCalls != 0 ||
		strings.Contains(output, "Resume:") {
		t.Fatalf(
			"held add exit=%d calls=%d/%d output=%s",
			exit,
			blockedCoordinator.preflightCalls,
			blockedCoordinator.remoteCalls,
			output,
		)
	}

	exit, output = captureCommandOutput(t, func() int {
		return resumeInteractiveCloudOnlySetup(
			bufio.NewReader(strings.NewReader("")),
			os.Stdout,
			paths,
			saved,
			&installState{},
			"",
			nil,
			nil,
		)
	})
	if exit != 1 ||
		blockedCoordinator.remoteCalls != 0 ||
		!strings.Contains(output, "recovery safety hold") {
		t.Fatalf("held wizard exit=%d output=%s", exit, output)
	}
}

func TestCloudReconnectHoldPreservesCurrentAndSiblingIdentity(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(t, testV2TwoProfileConfig)
	setServerSelectionOverride("cabin")
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ProfileID = "profile-cabin-hold"
	cfg.RelayInstanceID = "relay-cabin-hold"
	cfg.RoutePolicy = routePolicyAutomatic
	current := cloudMetadataForTest(strings.Repeat("7", 32))
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
		failingRemoteCloudCommandCoordinator{
			err: newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"exchange reconnect authorization",
				nil,
			),
		},
	)

	exit, _ := captureCommandOutput(t, func() int {
		return runCloudConnectCommand(
			paths,
			[]string{
				"--server", "cabin",
				"--url", productionCloudTestOrigin,
			},
			true,
		)
	})
	if exit != 1 {
		t.Fatalf("uncertain reconnect exit=%d", exit)
	}
	held, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if held.Cloud == nil ||
		held.Cloud.Current == nil ||
		*held.Cloud.Current != current ||
		held.Cloud.State != cloudStateReady ||
		held.Cloud.RecoveryHold == nil {
		t.Fatalf("held reconnect overwrote current: %+v", held.Cloud)
	}
	setServerSelectionOverride(defaultServerProfileName)
	sibling, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if sibling.Cloud != nil || sibling.ClientInstallID != "inst-abc" {
		t.Fatalf("held reconnect changed default sibling: %+v", sibling)
	}
}

func TestCloudRecoveryHoldStrictRoundTripAndRemoval(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := runtimeConfig{
		ClientInstallID: "inst-hold",
		ProfileID:       "profile-hold",
		RoutePolicy:     routePolicyLocal,
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
			RecoveryHold: &cloudRecoveryHold{
				Code:        cloudProblemUnavailable,
				Remediation: cloudRemediationVerifyState,
			},
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Cloud == nil ||
		reloaded.Cloud.RecoveryHold == nil ||
		*reloaded.Cloud.RecoveryHold != *cfg.Cloud.RecoveryHold {
		t.Fatalf("reloaded hold=%+v", reloaded.Cloud)
	}
	updated := reloaded
	updated.Cloud = nil
	if _, err := prepareCloudRemovalDocument(paths, updated); err != nil {
		t.Fatalf("held profile cannot be removed: %v", err)
	}

	top := readTestConfigTopLevel(t, paths)
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	var profile map[string]json.RawMessage
	if err := json.Unmarshal(servers["default"], &profile); err != nil {
		t.Fatal(err)
	}
	profile["cloud"] = json.RawMessage(`{
		"state":"authorizing",
		"recovery_hold":{
			"code":"cloud_unavailable",
			"remediation":"verify_state_without_retry",
			"unexpected":true
		}
	}`)
	servers["default"], _ = json.Marshal(profile)
	top["servers"], _ = json.Marshal(servers)
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCloudManagementConfig(paths); err == nil {
		t.Fatal("unknown recovery-hold field was accepted")
	}
}

func TestDisabledBuildReportsHeldCleanupWithoutMutation(t *testing.T) {
	resetServerProfileSelection(t)
	_, restore := setCloudFeatureTestIdentity(
		t,
		cloudRemoteBuildIdentity{Disabled: true},
	)
	defer restore()
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := runtimeConfig{
		ProfileID:   "profile-disabled-hold",
		RoutePolicy: routePolicyLocal,
		Cloud: &cloudLifecycleMetadata{
			State: cloudStateAuthorizing,
			RecoveryHold: &cloudRecoveryHold{
				Code:        cloudProblemAuthorization,
				Remediation: cloudRemediationSecurityStop,
			},
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, "cloud remove --server default") ||
		strings.Contains(output, "cloud add") ||
		strings.Contains(output, "cloud reconnect") {
		t.Fatalf("disabled held status exit=%d output=%s", exit, output)
	}

	exit, output = captureCommandOutput(t, func() int {
		return runCloudConnectCommand(paths, nil, false)
	})
	if exit != 1 ||
		!strings.Contains(output, "cloud remove --server default") ||
		strings.Contains(output, "cloud add") ||
		strings.Contains(output, "cloud reconnect") {
		t.Fatalf("disabled held add exit=%d output=%s", exit, output)
	}

	var wizardOutput strings.Builder
	exit = resumeInteractiveCloudOnlySetup(
		bufio.NewReader(strings.NewReader("")),
		&wizardOutput,
		paths,
		cfg,
		&installState{},
		"",
		nil,
		nil,
	)
	if exit != 1 ||
		!strings.Contains(
			wizardOutput.String(),
			"cloud remove --server default",
		) ||
		strings.Contains(wizardOutput.String(), "cloud add") ||
		strings.Contains(wizardOutput.String(), "cloud reconnect") {
		t.Fatalf(
			"disabled held wizard exit=%d output=%s",
			exit,
			wizardOutput.String(),
		)
	}
}
