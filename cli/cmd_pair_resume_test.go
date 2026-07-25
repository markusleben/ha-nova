package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRunPairCommandResumeErrorPrecedesMigrationAndCode(
	t *testing.T,
) {
	for _, profile := range []string{defaultServerProfileName, "cabin"} {
		t.Run(profile, func(t *testing.T) {
			cfg := completedLocalCloudTestConfig()
			cfg.PendingSecureBaseURL = "https://pending:8792"
			cfg.PendingSpkiPin = "pending-pin"
			paths, _ := saveHybridCheckpointUXProfile(t, profile, cfg)
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}

			resumeErr := errors.New("activation retry failed")
			originalResume := resumePendingActivationAfterRetirementCheck
			resumePendingActivationAfterRetirementCheck = func(
				*runtimeConfig,
				func(*runtimeConfig) error,
			) (bool, error) {
				return false, resumeErr
			}
			originalPair := runSecurePairingForPairCmd
			pairCalls := 0
			runSecurePairingForPairCmd = func(
				_, _ string,
				_ *runtimeConfig,
				_ func(*runtimeConfig) error,
				_ pairingClientInfo,
			) (string, error) {
				pairCalls++
				return "", errors.New("must not pair")
			}
			t.Cleanup(func() {
				resumePendingActivationAfterRetirementCheck =
					originalResume
				runSecurePairingForPairCmd = originalPair
			})

			args := []string{"--credential-store", "file"}
			if profile != defaultServerProfileName {
				args = append(args, "--server", profile)
			}
			exit, output := captureCommandOutput(t, func() int {
				return runPairCommand(paths, args)
			})
			if exit != 1 ||
				!strings.Contains(output, resumeErr.Error()) ||
				strings.Contains(output, "Six-digit code") ||
				pairCalls != 0 ||
				deviceFileBackendMarkerExists() {
				t.Fatalf(
					"resume failure pairCalls=%d exit=%d: %s",
					pairCalls,
					exit,
					output,
				)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("resume failure mutated config")
			}
		})
	}
}

func TestRunPairCommandResumesBeforeFileMigrationAndRelayOverride(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := completedLocalCloudTestConfig()
	cfg.PendingSecureBaseURL = "https://pending:8792"
	cfg.PendingSpkiPin = "pending-pin"
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	credential := validCredential(91)
	if err := writePendingDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}

	originalActivate := activateDeviceV1ForPairing
	activationCalls := 0
	activateDeviceV1ForPairing = func(base, pin, got string) error {
		activationCalls++
		if base != cfg.PendingSecureBaseURL ||
			pin != cfg.PendingSpkiPin ||
			got != credential {
			t.Fatalf(
				"activation base/pin/credential=%q/%q/%q",
				base,
				pin,
				got,
			)
		}
		return nil
	}
	originalPair := runSecurePairingForPairCmd
	pairCalls := 0
	runSecurePairingForPairCmd = func(
		_, _ string,
		_ *runtimeConfig,
		_ func(*runtimeConfig) error,
		_ pairingClientInfo,
	) (string, error) {
		pairCalls++
		return "", errors.New("must not pair")
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = originalActivate
		runSecurePairingForPairCmd = originalPair
	})

	exit, output := captureCommandOutput(t, func() int {
		return runPairCommand(paths, []string{
			"--relay-url", "http://override:8791",
			"--code", "123456",
			"--credential-store", "file",
		})
	})
	if exit != 0 ||
		!strings.Contains(
			output,
			"Resumed the interrupted secure device activation",
		) ||
		activationCalls != 1 ||
		pairCalls != 0 ||
		!deviceFileBackendMarkerExists() {
		t.Fatalf(
			"resume activation/pair=%d/%d exit=%d: %s",
			activationCalls,
			pairCalls,
			exit,
			output,
		)
	}
	saved, err := loadConfig(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RelayBaseURL != cfg.RelayBaseURL ||
		saved.PendingSecureBaseURL != "" ||
		saved.PendingSpkiPin != "" {
		t.Fatalf("resumed config=%+v", saved)
	}
	got, exists, err := readDeviceCredential()
	if err != nil || !exists || got != credential {
		t.Fatalf(
			"migrated credential=%q exists=%v err=%v",
			got,
			exists,
			err,
		)
	}
}

func TestRunPairCommandResumesPendingActivationFromValidIncompleteProfile(
	t *testing.T,
) {
	withDeviceStorageTestHome(t)
	resetKeyringDeviceSlots(t)
	resetServerProfileSelection(t)
	paths := setupServerCommandTest(t, `{"schema_version":1}`)
	cfg := runtimeConfig{
		HAHost:               "ha",
		HAURL:                "http://ha:8123",
		PendingSecureBaseURL: "https://pending:8792",
		PendingSpkiPin:       "pending-pin",
		RoutePolicy:          routePolicyLocal,
	}
	if err := saveConfig(paths, cfg); err != nil {
		t.Fatal(err)
	}
	credential := validCredential(93)
	if err := writePendingDeviceCredential(credential); err != nil {
		t.Fatal(err)
	}

	originalActivate := activateDeviceV1ForPairing
	activationCalls := 0
	activateDeviceV1ForPairing = func(base, pin, got string) error {
		activationCalls++
		if base != cfg.PendingSecureBaseURL ||
			pin != cfg.PendingSpkiPin ||
			got != credential {
			t.Fatalf(
				"activation base/pin/credential=%q/%q/%q",
				base,
				pin,
				got,
			)
		}
		return nil
	}
	originalPair := runSecurePairingForPairCmd
	pairCalls := 0
	runSecurePairingForPairCmd = func(
		_, _ string,
		_ *runtimeConfig,
		_ func(*runtimeConfig) error,
		_ pairingClientInfo,
	) (string, error) {
		pairCalls++
		return "", errors.New("must not pair")
	}
	t.Cleanup(func() {
		activateDeviceV1ForPairing = originalActivate
		runSecurePairingForPairCmd = originalPair
	})

	exit, output := captureCommandOutput(t, func() int {
		return runPairCommand(paths, []string{
			"--relay-url", "http://override:8791",
			"--code", "123456",
		})
	})
	if exit != 0 ||
		!strings.Contains(
			output,
			"Resumed the interrupted secure device activation",
		) ||
		activationCalls != 1 ||
		pairCalls != 0 {
		t.Fatalf(
			"incomplete resume activation/pair=%d/%d exit=%d: %s",
			activationCalls,
			pairCalls,
			exit,
			output,
		)
	}
	saved, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RelayBaseURL != "" ||
		saved.RelaySecureBaseURL != cfg.PendingSecureBaseURL ||
		saved.RelaySpkiPin != cfg.PendingSpkiPin ||
		saved.PendingSecureBaseURL != "" ||
		saved.PendingSpkiPin != "" {
		t.Fatalf("resumed incomplete config=%+v", saved)
	}
}
