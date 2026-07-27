package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestSetupConfigPreflightFailurePreservesSelectedCloudRecovery(
	t *testing.T,
) {
	tests := []struct {
		name       string
		config     func() runtimeConfig
		wantAction string
		forbid     []string
	}{
		{
			name: "ready",
			config: func() runtimeConfig {
				cfg := completedLocalCloudTestConfig()
				current := cloudMetadataForTest(strings.Repeat("b", 32))
				cfg.RelayInstanceID = "relay-ready"
				cfg.RoutePolicy = routePolicyAutomatic
				cfg.Cloud = &cloudLifecycleMetadata{
					State:   cloudStateReady,
					Current: &current,
				}
				return cfg
			},
			wantAction: "ha-nova cloud remove --server cabin",
			forbid: []string{
				"ha-nova cloud add --server cabin",
				"ha-nova cloud reconnect --server cabin",
				"ha-nova cloud unlock --server cabin",
			},
		},
		{
			name: "pending",
			config: func() runtimeConfig {
				return hybridCheckpointUXConfig(
					cloudStateTokenStored,
					false,
				)
			},
			wantAction: "ha-nova cloud add --server cabin",
			forbid: []string{
				"ha-nova cloud reconnect --server cabin",
				"ha-nova cloud unlock --server cabin",
			},
		},
		{
			name: "secure storage recovery hold",
			config: func() runtimeConfig {
				cfg := hybridCheckpointUXConfig(
					cloudStateTokenStored,
					false,
				)
				cfg.Cloud.RecoveryHold = &cloudRecoveryHold{
					Code:        cloudProblemSecureStorage,
					Remediation: cloudRemediationVerifyState,
				}
				return cfg
			},
			wantAction: "ha-nova cloud unlock --server cabin",
			forbid: []string{
				"ha-nova cloud add --server cabin",
				"ha-nova cloud reconnect --server cabin",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			withDeviceStorageTestHome(t)
			resetKeyringDeviceSlots(t)
			resetServerProfileSelection(t)
			paths, _ := saveHybridCheckpointUXProfile(
				t,
				"cabin",
				testCase.config(),
			)
			addInvalidCloudSibling(t, paths)
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			currentCredential := validCredential(104)
			credentialService := deviceCredentialServiceForProfile("cabin")
			if err := secretSet(
				credentialService,
				currentCredential,
			); err != nil {
				t.Fatal(err)
			}

			exit, output := captureCommandOutput(t, func() int {
				return runSetup(paths, []string{
					"hermes",
					"--server", "cabin",
					"--service",
					"--non-interactive",
					"--host", "new-ha",
					"--relay-token", "token",
				})
			})
			const failure = "cannot safely continue setup with the saved server configuration"
			actionIndex := strings.Index(output, testCase.wantAction)
			failureIndex := strings.Index(output, failure)
			const siblingFailure = "ready cloud lifecycle requires only current metadata"
			const wantCleanup = "ha-nova cloud remove --server cabin"
			cleanupIndex := strings.Index(output, wantCleanup)
			if exit != 1 ||
				actionIndex < 0 ||
				cleanupIndex < 0 ||
				failureIndex < 0 ||
				actionIndex >= failureIndex ||
				cleanupIndex >= failureIndex ||
				!strings.Contains(output, siblingFailure) {
				t.Fatalf("setup exit=%d output=%s", exit, output)
			}
			for _, forbidden := range testCase.forbid {
				if strings.Contains(output, forbidden) {
					t.Fatalf(
						"setup recovery unexpectedly contains %q: %s",
						forbidden,
						output,
					)
				}
			}
			for _, wrongProfileAction := range []string{
				"ha-nova cloud add --server default",
				"ha-nova cloud reconnect --server default",
				"ha-nova cloud unlock --server default",
				"ha-nova cloud remove --server default",
			} {
				if strings.Contains(output, wrongProfileAction) {
					t.Fatalf(
						"setup recovery targeted the wrong profile: %s",
						output,
					)
				}
			}
			if deviceFileBackendMarkerExists() {
				t.Fatal("invalid sibling changed the credential backend")
			}
			gotCredential, exists, err := readCredentialSlot(credentialService)
			if err != nil || !exists || gotCredential != currentCredential {
				t.Fatalf(
					"credential changed: got=%q exists=%v err=%v",
					gotCredential,
					exists,
					err,
				)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("rejected setup changed config.json")
			}
		})
	}
}

func addInvalidCloudSibling(t *testing.T, paths runtimePaths) {
	t.Helper()
	top := readTestConfigTopLevel(t, paths)
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(top["servers"], &servers); err != nil {
		t.Fatal(err)
	}
	servers["broken-sibling"] = json.RawMessage(
		`{"profile_id":"profile-broken","route_policy":"local","cloud":{"state":"ready"}}`,
	)
	rawServers, err := json.Marshal(servers)
	if err != nil {
		t.Fatal(err)
	}
	top["servers"] = rawServers
	if err := writeJSONFile(paths.ConfigFile, top, 0o600); err != nil {
		t.Fatal(err)
	}
}
