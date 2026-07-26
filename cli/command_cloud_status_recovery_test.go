package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestReadyCloudStatusProvidesProfileScopedReconnectForRecoverableIdentity(
	t *testing.T,
) {
	for _, test := range []struct {
		name        string
		err         error
		remediation cloudRemediation
	}{
		{
			name: "sign in again",
			err: newCloudError(
				CloudErrOAuthInvalidGrant,
				"refresh Cloud authorization",
				nil,
			),
			remediation: cloudRemediationSignIn,
		},
		{
			name: "pair device",
			err: newCloudError(
				CloudErrDeviceRejected,
				"load Cloud device",
				nil,
			),
			remediation: cloudRemediationPair,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			resetServerProfileSelection(t)
			paths := writeTestConfigFile(t, testV2TwoProfileConfig)
			setServerSelectionOverride("cabin")
			cfg, err := loadConfig(paths)
			if err != nil {
				t.Fatal(err)
			}
			cfg.RelayInstanceID = "relay-cabin"
			cfg.RoutePolicy = routePolicyAutomatic
			current := cloudMetadataForTest(strings.Repeat("d", 32))
			cfg.Cloud = &cloudLifecycleMetadata{
				State:   cloudStateReady,
				Current: &current,
			}
			if err := saveConfig(paths, cfg); err != nil {
				t.Fatal(err)
			}
			installCloudCommandHealthVerifier(
				t,
				func(context.Context, runtimeConfig) error {
					return test.err
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
			wantCommand := "ha-nova cloud reconnect --server cabin"
			if exit != 1 ||
				summary.VerificationError == nil ||
				summary.VerificationError.Remediation != test.remediation ||
				summary.NextCommand != wantCommand {
				t.Fatalf("status exit=%d summary=%+v", exit, summary)
			}

			exit, output = captureCommandOutput(t, func() int {
				return runCloudStatusCommand(paths, nil)
			})
			if exit != 1 || !strings.Contains(output, wantCommand) {
				t.Fatalf("human status exit=%d output=%s", exit, output)
			}
		})
	}
}

func TestCloudStatusJSONAttributesMissingNamedProfileCorrectly(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(paths, completedLocalCloudTestConfig()); err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(
			paths,
			[]string{"--server", "cabin", "--json"},
		)
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal(
		[]byte(strings.TrimSpace(output)),
		&summary,
	); err != nil {
		t.Fatalf("status JSON=%q: %v", output, err)
	}
	if exit != 1 ||
		summary.Status != "error" ||
		summary.Server != "cabin" {
		t.Fatalf("missing profile exit=%d summary=%+v", exit, summary)
	}
}

func TestCloudStatusAttributesConfiguredNamedDefaultAfterLoad(t *testing.T) {
	resetServerProfileSelection(t)
	paths := writeTestConfigFile(
		t,
		strings.Replace(
			testV2TwoProfileConfig,
			`"default_server": "default"`,
			`"default_server": "cabin"`,
			1,
		),
	)
	cfg, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ProfileID = "profile-cabin"
	cfg.Cloud = &cloudLifecycleMetadata{State: cloudStateAuthorizing}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	setServerSelectionOverride("")
	setActiveServerProfile(defaultServerProfileName)

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
		summary.Server != "cabin" ||
		summary.NextCommand != "ha-nova cloud add --server cabin" {
		t.Fatalf("configured-default status exit=%d summary=%+v", exit, summary)
	}
}

func TestCloudStatusPersistsSecurityHoldAcrossRestart(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-status-security"
	cfg.RelayInstanceID = "relay-status-security"
	current := cloudMetadataForTest(strings.Repeat("e", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	calls := 0
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			calls++
			return newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"ambiguous health authorization",
				nil,
			)
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("status JSON=%q: %v", output, err)
	}
	if exit != 1 ||
		summary.Status != "verification_failed" ||
		summary.VerificationError == nil ||
		summary.VerificationError.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("first status exit=%d summary=%+v", exit, summary)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.RecoveryHold == nil ||
		saved.Cloud.RecoveryHold.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("status did not persist hold: %+v", saved.Cloud)
	}

	exit, output = captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("restart status JSON=%q: %v", output, err)
	}
	if exit != 1 || summary.Status != "recovery_blocked" || calls != 1 {
		t.Fatalf(
			"restart status retried verification: exit=%d calls=%d summary=%+v",
			exit,
			calls,
			summary,
		)
	}

	blocked := newSelectingCloudCoordinator()
	installCloudCommandCoordinator(t, blocked)
	installCloudCommandPromptSession(t, true)
	installSuccessfulCloudDevicePreflight(t)
	exit, output = captureCommandOutput(t, func() int {
		return runCloudConnectCommand(paths, nil, true)
	})
	if exit != 1 ||
		blocked.preflightCalls != 0 ||
		blocked.remoteCalls != 0 {
		t.Fatalf(
			"held reconnect ran: exit=%d calls=%d/%d output=%s",
			exit,
			blocked.preflightCalls,
			blocked.remoteCalls,
			output,
		)
	}
}

func TestCloudStatusJSONGuidesUnlockForClearableStorageHold(
	t *testing.T,
) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-status-unlock"
	cfg.RelayInstanceID = "relay-status-unlock"
	current := cloudMetadataForTest(strings.Repeat("f", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
		RecoveryHold: &cloudRecoveryHold{
			Code:        cloudProblemSecureStorage,
			Remediation: cloudRemediationVerifyState,
		},
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(
			paths,
			[]string{"--json"},
		)
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
		summary.NextCommand != cloudUnlockCommand() {
		t.Fatalf("status exit=%d summary=%+v", exit, summary)
	}
}

func TestCloudStatusLateFailureCannotHoldConcurrentNewGeneration(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-status-race"
	cfg.RelayInstanceID = "relay-status-race"
	current := cloudMetadataForTest(strings.Repeat("1", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	replacement := readTestConfigTopLevel(t, paths)
	replacement["servers"] = []byte(strings.Replace(
		string(replacement["servers"]),
		strings.Repeat("1", 32),
		strings.Repeat("2", 32),
		1,
	))
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			written := make(chan error, 1)
			go func() {
				written <- writeJSONFile(paths.ConfigFile, replacement, 0o600)
			}()
			if err := <-written; err != nil {
				return err
			}
			return newCloudError(
				CloudErrOAuthOutcomeUnknown,
				"late generation-one health result",
				nil,
			)
		},
	)

	exit, _ := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	if exit != 1 {
		t.Fatalf("late status failure exit=%d", exit)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.Current == nil ||
		saved.Cloud.Current.CredentialGeneration != strings.Repeat("2", 32) ||
		saved.Cloud.RecoveryHold != nil {
		t.Fatalf("late status held concurrent replacement: %+v", saved.Cloud)
	}
}

func TestCloudStatusRejectsSuccessfulHealthForChangedSnapshot(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-status-success-race"
	cfg.RelayInstanceID = "relay-status-success-race"
	current := cloudMetadataForTest(strings.Repeat("3", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   cloudStateReady,
		Current: &current,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	replacement := readTestConfigTopLevel(t, paths)
	replacement["servers"] = []byte(strings.Replace(
		string(replacement["servers"]),
		strings.Repeat("3", 32),
		strings.Repeat("4", 32),
		1,
	))
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return writeJSONFile(paths.ConfigFile, replacement, 0o600)
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
		summary.Status != "verification_failed" ||
		summary.VerificationError == nil ||
		summary.VerificationError.Remediation != cloudRemediationSecurityStop {
		t.Fatalf("stale status exit=%d summary=%+v", exit, summary)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Cloud == nil ||
		saved.Cloud.Current == nil ||
		saved.Cloud.Current.CredentialGeneration != strings.Repeat("4", 32) {
		t.Fatalf("status overwrote concurrent replacement: %+v", saved.Cloud)
	}
}
