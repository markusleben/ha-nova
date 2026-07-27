package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestServerRemovalCheckpointBlocksStructuralMutations(
	t *testing.T,
) {
	paths := checkpointCabinServerRemoval(t)
	before, err := os.ReadFile(paths.ConfigFile)
	if err != nil {
		t.Fatal(err)
	}
	previousPair := runSecurePairingForPairCmd
	runSecurePairingForPairCmd = func(
		string,
		string,
		*runtimeConfig,
		func(*runtimeConfig) error,
		pairingClientInfo,
	) (string, error) {
		t.Fatal("pairing reached the one-time code exchange")
		return "", nil
	}
	t.Cleanup(func() {
		runSecurePairingForPairCmd = previousPair
	})
	for _, testCase := range []struct {
		name string
		run  func() int
	}{
		{
			name: "default",
			run: func() int {
				return runServerCommand(
					paths,
					[]string{"default", "cabin"},
				)
			},
		},
		{
			name: "rename",
			run: func() int {
				return runServerCommand(
					paths,
					[]string{"rename", "cabin", "seaside"},
				)
			},
		},
		{
			name: "route",
			run: func() int {
				return runServerCommand(
					paths,
					[]string{
						"route",
						"local",
						"--server",
						"cabin",
					},
				)
			},
		},
		{
			name: "pair",
			run: func() int {
				return runPairCommand(
					paths,
					[]string{
						"--server",
						"cabin",
						"--relay-url",
						"http://cabin:8791",
						"--code",
						"123456",
					},
				)
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			exit, output := captureCommandOutput(
				t,
				testCase.run,
			)
			if exit != 1 ||
				!strings.Contains(
					output,
					"ha-nova server remove cabin",
				) {
				t.Fatalf(
					"exit=%d output=%s",
					exit,
					output,
				)
			}
			after, err := os.ReadFile(paths.ConfigFile)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Fatal("blocked mutation changed config")
			}
		})
	}
}

func checkpointCabinServerRemoval(
	t *testing.T,
) runtimePaths {
	t.Helper()
	previousConfirmation :=
		readServerRemoveConfirmationForCommand
	previousHook := serverRemovalPhaseHook
	t.Cleanup(func() {
		readServerRemoveConfirmationForCommand =
			previousConfirmation
		serverRemovalPhaseHook = previousHook
	})
	paths := setupServerCommandTest(t, testV2TwoProfileConfig)
	if err := secretSet(
		deviceCredentialServiceForProfile("cabin"),
		testProfileCredentialB,
	); err != nil {
		t.Fatal(err)
	}
	readServerRemoveConfirmationForCommand = func(
		string,
	) (string, error) {
		return "cabin", nil
	}
	serverRemovalPhaseHook = func(phase string) error {
		if phase == "checkpoint-persisted" {
			return errors.New("simulated crash")
		}
		return nil
	}
	exit, output := captureCommandOutput(t, func() int {
		return runServerRemove(paths, []string{"cabin"})
	})
	if exit != 1 ||
		!strings.Contains(output, "simulated crash") {
		t.Fatalf("checkpoint exit=%d output=%s", exit, output)
	}
	serverRemovalPhaseHook = func(string) error { return nil }
	return paths
}
