package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

func TestServerRenameBlocksEveryDeviceRetirementPhase(t *testing.T) {
	for _, phase := range []string{
		deviceCredentialRetirementPrepared,
		deviceCredentialRetirementRevoked,
	} {
		t.Run(phase, func(t *testing.T) {
			paths := setupServerCommandTest(t, testV2TwoProfileConfig)
			seedDeviceRetirementCheckpointForProfile(
				t,
				paths,
				"cabin",
				phase,
			)
			if err := secretSet(
				deviceCredentialServiceForProfile("cabin"),
				testProfileCredentialB,
			); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}

			exit, output := captureCommandOutput(t, func() int {
				return runServerCommand(
					paths,
					[]string{"rename", "cabin", "seaside"},
				)
			})
			if exit != 1 ||
				!strings.Contains(output, "pending device retirement") ||
				!strings.Contains(output, "ha-nova setup --server cabin") ||
				!strings.Contains(output, "Nothing was renamed") {
				t.Fatalf(
					"rename during %s retirement exit=%d output=%q",
					phase,
					exit,
					output,
				)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("blocked rename changed config.json")
			}
			if got, exists, err := readCredentialSlot(
				deviceCredentialServiceForProfile("cabin"),
			); err != nil || !exists || got != testProfileCredentialB {
				t.Fatalf(
					"blocked rename changed old slot: got=%q exists=%v err=%v",
					got,
					exists,
					err,
				)
			}
			if _, exists, err := readCredentialSlot(
				deviceCredentialServiceForProfile("seaside"),
			); err != nil || exists {
				t.Fatalf(
					"blocked rename created new slot: exists=%v err=%v",
					exists,
					err,
				)
			}
			checkpoint, exists, err :=
				readDeviceCredentialRetirementCheckpointForProfile(
					paths,
					"cabin",
				)
			if err != nil || !exists || checkpoint.Phase != phase {
				t.Fatalf(
					"checkpoint after blocked rename: phase=%q exists=%v err=%v",
					checkpoint.Phase,
					exists,
					err,
				)
			}
		})
	}
}

func TestServerRenameWithoutDeviceRetirementStillSucceeds(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	if exit := runServerCommand(
		paths,
		[]string{"rename", "cabin", "seaside"},
	); exit != 0 {
		t.Fatalf("rename exit = %d, want 0", exit)
	}
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if doc.hasProfile("cabin") || !doc.hasProfile("seaside") {
		t.Fatal("rename did not move the profile")
	}
}

func TestServerRenameBlocksRetirementCheckpointAtAbsentDestination(
	t *testing.T,
) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	checkpointPath, err := deviceCredentialRetirementCheckpointPathForProfile(
		paths,
		"seaside",
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSONFile(
		checkpointPath,
		deviceCredentialRetirementCheckpoint{
			SchemaVersion: deviceCredentialRetirementSchema,
			Phase:         deviceCredentialRetirementPrepared,
			Profile:       "seaside",
			ProfileID:     "profile-orphaned-destination",
		},
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		testProfileCredentialB,
	); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}

	exit, output := captureCommandOutput(t, func() int {
		return runServerCommand(
			paths,
			[]string{"rename", "cabin", "seaside"},
		)
	})
	if exit != 1 ||
		!strings.Contains(output, `server profile "seaside"`) ||
		!strings.Contains(output, "pending device retirement") ||
		!strings.Contains(output, "ha-nova setup --server seaside") {
		t.Fatalf("rename exit=%d output=%q", exit, output)
	}
	after, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("destination retirement guard changed config.json")
	}
	if got, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile("cabin"),
	); err != nil || !exists || got != testProfileCredentialB {
		t.Fatalf(
			"destination guard changed source slot: got=%q exists=%v err=%v",
			got,
			exists,
			err,
		)
	}
	if _, exists, err := readCredentialSlot(
		deviceCredentialServiceForProfile("seaside"),
	); err != nil || exists {
		t.Fatalf(
			"destination guard created target slot: exists=%v err=%v",
			exists,
			err,
		)
	}
}

func TestPairBlocksPendingDeviceRetirementBeforePairing(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	seedDeviceRetirementCheckpointForProfile(
		t,
		paths,
		"cabin",
		deviceCredentialRetirementPrepared,
	)
	originalPair := runSecurePairingForPairCmd
	pairCalled := false
	runSecurePairingForPairCmd = func(
		string,
		string,
		*runtimeConfig,
		func(*runtimeConfig) error,
		pairingClientInfo,
	) (string, error) {
		pairCalled = true
		return "", nil
	}
	t.Cleanup(func() { runSecurePairingForPairCmd = originalPair })

	exit, output := captureCommandOutput(t, func() int {
		return runPairCommand(
			paths,
			[]string{
				"--server", "cabin",
				"--relay-url", "http://cabin:8791",
				"--code", "123456",
			},
		)
	})
	if exit != 1 || !strings.Contains(output, "pending device retirement") {
		t.Fatalf("pair exit=%d output=%q", exit, output)
	}
	if pairCalled {
		t.Fatal("pairing started while device retirement was pending")
	}
}

func TestCloudConnectBlocksPendingDeviceRetirementBeforeNetwork(t *testing.T) {
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	seedDeviceRetirementCheckpointForProfile(
		t,
		paths,
		"cabin",
		deviceCredentialRetirementPrepared,
	)
	setServerSelectionOverride("cabin")
	cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
	if err != nil {
		t.Fatal(err)
	}
	_, err = connectCloudCommandLocked(
		context.Background(),
		paths,
		cfg,
		cloudCommandFlags{url: productionCloudTestOrigin},
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "pending device retirement") {
		t.Fatalf("Cloud connect retirement guard error = %v", err)
	}
}

func TestInteractiveSetupPairingBlocksEveryDeviceRetirementPhase(
	t *testing.T,
) {
	for _, phase := range []string{
		deviceCredentialRetirementPrepared,
		deviceCredentialRetirementRevoked,
	} {
		t.Run(phase, func(t *testing.T) {
			paths := setupServerCommandTest(
				t,
				`{"schema_version":1,"relay_base_url":"http://relay:8791"}`,
			)
			cfg, err := loadSelectedRuntimeConfigUnchecked(paths)
			if err != nil {
				t.Fatal(err)
			}
			seedDeviceRetirementCheckpointForProfile(
				t,
				paths,
				defaultServerProfileName,
				phase,
			)
			originalPair := securePairForSetup
			pairCalls := 0
			securePairForSetup = func(
				string,
				string,
				*runtimeConfig,
				func(*runtimeConfig) error,
				pairingClientInfo,
			) (string, error) {
				pairCalls++
				return "", nil
			}
			t.Cleanup(func() { securePairForSetup = originalPair })

			_, err = runSetupPairingFlow(
				bufio.NewReader(strings.NewReader("")),
				io.Discard,
				paths,
				&cfg,
				false,
			)
			if err == nil ||
				!strings.Contains(err.Error(), "pending device retirement") {
				t.Fatalf("pairing guard error = %v", err)
			}
			if pairCalls != 0 {
				t.Fatalf(
					"setup pairing started during %s retirement",
					phase,
				)
			}
		})
	}
}

func seedDeviceRetirementCheckpointForProfile(
	t *testing.T,
	paths runtimePaths,
	profile string,
	phase string,
) {
	t.Helper()
	doc, err := loadConfigDocument(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	cfg, exists := doc.flatProfile(profile)
	if !exists {
		t.Fatalf("profile %q missing", profile)
	}
	originalProfile := activeServerProfile()
	setActiveServerProfile(profile)
	t.Cleanup(func() { setActiveServerProfile(originalProfile) })
	if err := writeDeviceCredentialRetirementCheckpoint(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if phase == deviceCredentialRetirementRevoked {
		checkpoint, exists, err :=
			readDeviceCredentialRetirementCheckpoint(paths)
		if err != nil || !exists {
			t.Fatalf(
				"read prepared checkpoint: exists=%v err=%v",
				exists,
				err,
			)
		}
		if _, err := markDeviceCredentialRetirementRevoked(
			paths,
			checkpoint,
		); err != nil {
			t.Fatal(err)
		}
	}
	setActiveServerProfile(originalProfile)
}
