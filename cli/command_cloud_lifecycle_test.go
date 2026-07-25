package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

type failingRemoteCloudCommandCoordinator struct {
	err error
}

func (c failingRemoteCloudCommandCoordinator) Available() bool {
	return true
}

func (c failingRemoteCloudCommandCoordinator) Preflight(
	context.Context,
	string,
) error {
	return c.err
}

func (c failingRemoteCloudCommandCoordinator) AddAwayWithExistingDevice(
	context.Context,
	cloudSetupRequest,
) (cloudSetupResult, error) {
	return cloudSetupResult{}, errors.New("unexpected local Cloud setup")
}

func (c failingRemoteCloudCommandCoordinator) AddRemoteWithPairing(
	context.Context,
	cloudRemoteSetupRequest,
) (cloudSetupResult, error) {
	return cloudSetupResult{}, errors.New("unexpected remote Cloud setup")
}

func installCloudCommandHealthVerifier(
	t *testing.T,
	verify func(context.Context, runtimeConfig) error,
) {
	t.Helper()
	old := verifyCloudDeviceHealthForCommand
	verifyCloudDeviceHealthForCommand = verify
	t.Cleanup(func() { verifyCloudDeviceHealthForCommand = old })
}

func installCloudCommandPromptSession(t *testing.T, available bool) {
	t.Helper()
	old := cloudInteractivePromptSessionForSetup
	cloudInteractivePromptSessionForSetup = func() bool { return available }
	t.Cleanup(func() { cloudInteractivePromptSessionForSetup = old })
}

func pendingCloudOnlyCommandConfig(state cloudLifecycleState) runtimeConfig {
	pending := cloudMetadataForTest(strings.Repeat("a", 32))
	if state == cloudStateAuthorizing || state == cloudStateTokenStored {
		pending.HAUserID = ""
	}
	return runtimeConfig{
		ProfileID:   "profile-pending",
		RoutePolicy: routePolicyLocal,
		Cloud: &cloudLifecycleMetadata{
			State:   state,
			Pending: &pending,
		},
	}
}

func pendingCloudReconnectCommandConfig(state cloudLifecycleState) runtimeConfig {
	cfg := completedLocalCloudTestConfig()
	cfg.ProfileID = "profile-reconnect"
	cfg.RelayInstanceID = "relay-reconnect"
	cfg.RoutePolicy = routePolicyAutomatic
	current := cloudMetadataForTest(strings.Repeat("b", 32))
	pending := cloudMetadataForTest(strings.Repeat("c", 32))
	cfg.Cloud = &cloudLifecycleMetadata{
		State:   state,
		Current: &current,
		Pending: &pending,
	}
	return cfg
}

func TestCloudStatusReportsPendingCloudOnlyTransactionWithoutOpeningStorage(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(
		paths,
		pendingCloudOnlyCommandConfig(cloudStateAuthorizing),
	); err != nil {
		t.Fatal(err)
	}
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			t.Fatal("status without a current authorization must not probe Cloud")
			return nil
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	if exit != 1 {
		t.Fatalf("pending status exit=%d output=%s", exit, output)
	}
	var summary cloudStatusSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("pending status JSON=%q: %v", output, err)
	}
	if summary.Status != "setup_pending" ||
		summary.Lifecycle != cloudStateAuthorizing ||
		summary.CurrentAvailable ||
		summary.CurrentReady ||
		!summary.Pending ||
		summary.NextCommand != "ha-nova cloud add --server default" {
		t.Fatalf("pending status summary=%+v", summary)
	}
}

func TestCloudStatusDoesNotHidePendingReconnect(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudReconnectCommandConfig(cloudStateTokenStored)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	verifyCalls := 0
	installCloudCommandHealthVerifier(
		t,
		func(_ context.Context, got runtimeConfig) error {
			verifyCalls++
			if got.Cloud == nil ||
				got.Cloud.Current == nil ||
				got.Cloud.Current.CredentialGeneration !=
					cfg.Cloud.Current.CredentialGeneration {
				t.Fatalf("status did not verify the still-current authorization: %+v", got.Cloud)
			}
			return nil
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	if exit != 1 {
		t.Fatalf("reconnect status exit=%d output=%s", exit, output)
	}
	var summary cloudStatusSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("reconnect status JSON=%q: %v", output, err)
	}
	if verifyCalls != 1 ||
		summary.Status != "reconnect_pending" ||
		summary.Lifecycle != cloudStateTokenStored ||
		!summary.CurrentAvailable ||
		!summary.CurrentReady ||
		!summary.Pending ||
		summary.NextCommand != "ha-nova cloud reconnect --server default" {
		t.Fatalf("reconnect status calls=%d summary=%+v", verifyCalls, summary)
	}
}

func TestCloudStatusRejectsCorruptPendingTransactionBeforeHealthCheck(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudOnlyCommandConfig(cloudStateTokenStored)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := strings.Replace(
		string(data),
		`"canonical_origin": "https://example.ui.nabu.casa"`,
		`"canonical_origin": ""`,
		1,
	)
	if corrupt == string(data) {
		t.Fatal("test fixture did not find pending canonical origin")
	}
	if err := os.WriteFile(paths.ConfigFile, []byte(corrupt), 0o600); err != nil {
		t.Fatal(err)
	}
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			t.Fatal("corrupt pending metadata reached health verification")
			return nil
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, "invalid pending cloud metadata") {
		t.Fatalf("corrupt pending status exit=%d output=%s", exit, output)
	}
}

func TestCloudStatusRejectsUnsafeSelectedProfileName(t *testing.T) {
	paths := setupServerCommandTest(t, `{
		"schema_version": 3,
		"default_server": "bad name",
		"servers": {
			"bad name": {
				"profile_id": "profile-bad-name",
				"route_policy": "local",
				"relay_base_url": "http://ha:8791"
			}
		}
	}`)
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			t.Fatal("unsafe selected profile reached health verification")
			return nil
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, "invalid selected server profile") {
		t.Fatalf("unsafe profile status exit=%d output=%s", exit, output)
	}
}

func TestCloudUnlockPreflightsPendingCloudOnlyTransaction(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	if err := saveConfig(
		paths,
		pendingCloudOnlyCommandConfig(cloudStateAuthorizing),
	); err != nil {
		t.Fatal(err)
	}
	coordinator := successfulCloudCoordinatorForTest()
	installCloudCommandCoordinator(t, coordinator)
	installCloudCommandPromptSession(t, true)
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			t.Fatal("pending Cloud-only unlock must not verify a nonexistent current authorization")
			return nil
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(paths, nil)
	})
	if exit != 0 ||
		coordinator.preflightCalls != 1 ||
		coordinator.preflightID != "profile-pending" ||
		!strings.Contains(output, `waiting at "authorizing"`) ||
		!strings.Contains(output, "ha-nova cloud add --server default") {
		t.Fatalf(
			"pending unlock exit=%d preflight=%d/%q output=%s",
			exit,
			coordinator.preflightCalls,
			coordinator.preflightID,
			output,
		)
	}
}

func TestCloudUnlockDoesNotTouchCurrentAfterPendingReconnectPreflight(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudReconnectCommandConfig(cloudStateCloudVerified)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	coordinator := successfulCloudCoordinatorForTest()
	installCloudCommandCoordinator(t, coordinator)
	installCloudCommandPromptSession(t, true)
	verifyCalls := 0
	installCloudCommandHealthVerifier(
		t,
		func(_ context.Context, _ runtimeConfig) error {
			verifyCalls++
			t.Fatal("pending reconnect unlock touched the current authorization")
			return errors.New("must not verify current")
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudUnlockCommand(paths, nil)
	})
	if exit != 0 ||
		coordinator.preflightCalls != 1 ||
		verifyCalls != 0 ||
		!strings.Contains(output, "a Cloud update is waiting") ||
		!strings.Contains(output, `waiting at "cloud_verified"`) ||
		!strings.Contains(output, "ha-nova cloud reconnect --server default") {
		t.Fatalf(
			"reconnect unlock exit=%d preflight=%d verify=%d output=%s",
			exit,
			coordinator.preflightCalls,
			verifyCalls,
			output,
		)
	}
}

func TestCloudStatusLockedStoragePrintsExactUnlockCommand(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudReconnectCommandConfig(cloudStateTokenStored)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return newCloudError(
				CloudErrSecretStoreLocked,
				"read current Cloud authorization",
				nil,
			)
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, nil)
	})
	if exit != 1 ||
		!strings.Contains(output, string(cloudRemediationUnlockStorage)) ||
		!strings.Contains(
			output,
			"ha-nova cloud unlock --server default",
		) {
		t.Fatalf("locked status exit=%d output=%s", exit, output)
	}
}

func TestCloudStatusJSONKeepsTypedErrorWhenStorageIsLocked(t *testing.T) {
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := pendingCloudReconnectCommandConfig(cloudStateTokenStored)
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	installCloudCommandHealthVerifier(
		t,
		func(context.Context, runtimeConfig) error {
			return newCloudError(
				CloudErrSecretStoreLocked,
				"read current Cloud authorization",
				nil,
			)
		},
	)

	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(paths, []string{"--json"})
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("locked status JSON=%q: %v", output, err)
	}
	if exit != 1 ||
		summary.VerificationError == nil ||
		summary.VerificationError.Code != cloudProblemSecureStorage ||
		summary.VerificationError.Remediation != cloudRemediationUnlockStorage ||
		summary.NextCommand != "ha-nova cloud unlock --server default" {
		t.Fatalf("locked JSON exit=%d summary=%+v", exit, summary)
	}
}

func TestCloudStatusJSONKeepsJSONForInvalidArguments(t *testing.T) {
	resetServerProfileSelection(t)
	exit, output := captureCommandOutput(t, func() int {
		return runCloudStatusCommand(
			runtimePaths{},
			[]string{"--json", "unexpected"},
		)
	})
	var summary cloudStatusSummary
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &summary); err != nil {
		t.Fatalf("invalid-argument status JSON=%q: %v", output, err)
	}
	if exit != 1 ||
		summary.Status != "error" ||
		summary.VerificationError == nil {
		t.Fatalf("invalid-argument JSON exit=%d summary=%+v", exit, summary)
	}
}
