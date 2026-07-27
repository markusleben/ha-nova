package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInteractiveCloudConflictFailsBeforeClientAndKeyring(
	t *testing.T,
) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		t.Run(profile, func(t *testing.T) {
			current := cloudMetadataForTest(strings.Repeat("e", 32))
			cfg := runtimeConfig{
				ProfileID:            "profile-" + profile,
				RelayInstanceID:      "relay-cloud",
				RoutePolicy:          routePolicyCloud,
				PendingSecureBaseURL: "https://pending.local:8792",
				PendingSpkiPin:       "pending-pin",
				Cloud: &cloudLifecycleMetadata{
					State:   cloudStateReady,
					Current: &current,
				},
			}
			paths, cfg := saveHybridCheckpointUXProfile(
				t,
				profile,
				cfg,
			)
			keyringCalls := installSetupKeyringPreflightCounter(t)

			stdout, stderr := captureInteractiveSetupIO(
				t,
				"",
				func() int {
					return interactiveSetup(
						paths,
						cfg,
						installState{},
						"unsupported-client",
						"",
						"",
						"",
						"",
						false,
					)
				},
			)
			output := stdout + stderr
			if !strings.Contains(
				output,
				"cannot resume the interrupted device activation",
			) ||
				!strings.Contains(
					output,
					"remove Cloud access first",
				) ||
				strings.Contains(output, "unsupported client") ||
				*keyringCalls != 0 {
				t.Fatalf(
					"Cloud activation conflict keyring=%d: %s",
					*keyringCalls,
					output,
				)
			}
		})
	}
}

func TestNonInteractiveActivationErrorPrecedesStateAndClients(
	t *testing.T,
) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	paths.StateFile = filepath.Join(paths.ConfigDir, "state.json")
	cfg := completedLocalCloudTestConfig()
	cfg.PendingSecureBaseURL = "https://pending.local:8792"
	cfg.PendingSpkiPin = "pending-pin"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StateFile, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	keyringCalls := installSetupKeyringPreflightCounter(t)
	originalResume := resumePendingActivationAfterRetirementCheck
	resumePendingActivationAfterRetirementCheck = func(
		*runtimeConfig,
		func(*runtimeConfig) error,
	) (bool, error) {
		return false, errors.New("activation retry failed")
	}
	t.Cleanup(func() {
		resumePendingActivationAfterRetirementCheck = originalResume
	})

	exit, output := captureCommandOutput(t, func() int {
		return runSetup(
			paths,
			[]string{"codex", "--non-interactive"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, "activation retry failed") ||
		strings.Contains(output, "state file is corrupt") ||
		strings.Contains(output, "Will install") ||
		*keyringCalls != 0 {
		t.Fatalf(
			"noninteractive activation ordering keyring=%d exit=%d: %s",
			*keyringCalls,
			exit,
			output,
		)
	}
}

func TestDoctorActivationErrorPrecedesClientSelfHeal(t *testing.T) {
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	paths.StateFile = filepath.Join(paths.ConfigDir, "state.json")
	cfg := completedLocalCloudTestConfig()
	cfg.PendingSecureBaseURL = "https://pending.local:8792"
	cfg.PendingSpkiPin = "pending-pin"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	state := defaultInstallState()
	state.Version = "old-version"
	state.ClientsVerifiedVersion = "old-version"
	if err := saveState(paths, state); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	originalResume := resumePendingActivationAfterRetirementCheck
	resumePendingActivationAfterRetirementCheck = func(
		*runtimeConfig,
		func(*runtimeConfig) error,
	) (bool, error) {
		return false, errors.New("doctor activation failed")
	}
	t.Cleanup(func() {
		resumePendingActivationAfterRetirementCheck = originalResume
	})

	exit, output := captureCommandOutput(t, func() int {
		return runDoctor(paths, []string{"--quiet"})
	})
	if exit != 1 ||
		!strings.Contains(output, "doctor activation failed") {
		t.Fatalf("doctor activation exit=%d: %s", exit, output)
	}
	after, err := os.ReadFile(paths.StateFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("doctor self-healed clients before activation recovery")
	}
}

func TestSetupActivationNoopAvoidsLifecycleLock(t *testing.T) {
	resumed, err := resumeSetupPendingActivation(
		runtimePaths{},
		&runtimeConfig{},
	)
	if err != nil || resumed {
		t.Fatalf("no-op activation resumed=%v err=%v", resumed, err)
	}
}
